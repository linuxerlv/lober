import copy
import io
import json
import os
import subprocess
import unittest
from contextlib import redirect_stdout
from datetime import datetime, timedelta, timezone
from importlib.util import module_from_spec, spec_from_file_location
from pathlib import Path
from unittest.mock import patch
from urllib.error import HTTPError
from urllib.parse import parse_qs, urlsplit

SPEC = spec_from_file_location("report", Path(__file__).with_name("report.py"))
report = module_from_spec(SPEC)

REPO = "example/lober"
BASE = "1" * 40
HEAD = "2" * 40
MERGE = "3" * 40
TREE = "4" * 40
OTHER = "5" * 40
NOW = datetime(2026, 11, 11, tzinfo=timezone.utc)
BOT_ID = 100
PREFIX = f"/repos/{REPO}"


def pull(base="preview", head="test", **fields):
    return {
        "number": 21,
        "state": "open",
        "draft": False,
        "merged": False,
        "mergeable": True,
        "merge_commit_sha": MERGE,
        "base": {"ref": base, "sha": BASE, "repo": {"full_name": REPO}},
        "head": {"ref": head, "sha": HEAD, "repo": {"full_name": REPO}},
        **fields,
    }


def status(state, creator=BOT_ID, age=1, context="enterprise-release"):
    return {
        "state": state,
        "context": context,
        "creator": {"id": creator},
        "created_at": (NOW - timedelta(days=age)).strftime("%Y-%m-%dT%H:%M:%SZ"),
    }


class GitHub:
    def __init__(self, base="preview", head="test"):
        self.base = base
        self.head = head
        self.pull = pull(base, head)
        self.parents = [BASE, HEAD]
        self.head_tree = TREE
        self.trial_tree = TREE
        self.branch_protected = True
        self.head_sha = HEAD
        self.statuses = []
        self.writes = []
        self.verified = []
        self.verdict = 0
        self.change_after_verify = None
        self.extra_list_page = False

    def call(self, path, payload=None):
        route = urlsplit(path)
        if route.path == f"{PREFIX}/pulls":
            query = parse_qs(route.query)
            if query["state"] != ["open"] or query["base"] != [self.base]:
                raise AssertionError(f"unexpected pull list: {path}")
            if self.extra_list_page:
                if query["page"] == ["1"]:
                    return [{"number": 123}] * 100
                if query["page"] != ["2"]:
                    raise AssertionError(f"unexpected page: {path}")
            return [{"number": 21}]
        if route.path == f"{PREFIX}/pulls/21":
            return copy.deepcopy(self.pull)
        if self.extra_list_page and route.path == f"{PREFIX}/pulls/123":
            return pull(number=123, draft=True)
        if route.path == f"{PREFIX}/branches/{self.base}":
            return {"name": self.base, "protected": True, "commit": {"sha": BASE}}
        if route.path == f"{PREFIX}/branches/{self.head}":
            return {
                "name": self.head,
                "protected": self.branch_protected,
                "commit": {"sha": self.head_sha},
            }
        if route.path == f"{PREFIX}/git/commits/{HEAD}":
            return {"sha": HEAD, "tree": {"sha": self.head_tree}}
        if route.path == f"{PREFIX}/git/commits/{MERGE}":
            return {
                "sha": MERGE,
                "tree": {"sha": self.trial_tree},
                "parents": [{"sha": parent} for parent in self.parents],
            }
        if route.path == f"{PREFIX}/commits/{MERGE}/statuses":
            return self.statuses
        if payload is not None:
            self.writes.append((path, payload["context"], payload["state"]))
            return {}
        raise AssertionError(f"unexpected request: {path}")

    def verify(self, stage, sha):
        self.verified.append((stage, sha))
        if self.change_after_verify:
            self.change_after_verify()
        if isinstance(self.verdict, Exception):
            raise self.verdict
        return self.verdict

    def run(self):
        report.poll_branch(self, REPO, self.base, self.verify, BOT_ID, NOW)


class ReportTests(unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        SPEC.loader.exec_module(report)

    def setUp(self):
        output = redirect_stdout(io.StringIO())
        output.__enter__()
        self.addCleanup(output.__exit__, None, None, None)

    def test_verified_candidate_passes_on_the_trial_commit(self):
        github = GitHub()
        github.run()
        self.assertEqual(github.verified, [("test", HEAD)])
        self.assertEqual(github.writes, [(f"{PREFIX}/statuses/{MERGE}", "enterprise-release", "success")])

    def test_dev_promotion_uses_its_own_evidence_and_status(self):
        github = GitHub("test", "dev")
        github.run()
        self.assertEqual(github.verified, [("dev", HEAD)])
        self.assertEqual(github.writes, [(f"{PREFIX}/statuses/{MERGE}", "dev-ready", "success")])

    def test_failed_or_unavailable_verification_blocks_the_trial_commit(self):
        for verdict in (1, 127, -15, None, False, OSError(), subprocess.TimeoutExpired("verify", 120)):
            with self.subTest(verdict=verdict):
                github = GitHub()
                github.verdict = verdict
                github.run()
                self.assertEqual(github.writes, [(f"{PREFIX}/statuses/{MERGE}", "enterprise-release", "failure")])

    def test_dry_run_checks_the_trial_without_posting_a_status(self):
        for verdict in (0, 1):
            with self.subTest(verdict=verdict):
                github = GitHub()
                github.verdict = verdict
                self.assertTrue(report.poll_branch(github, REPO, github.base, github.verify, BOT_ID, NOW,
                                                   dry_run=True))
                self.assertEqual(github.verified, [("test", HEAD)])
                self.assertEqual(github.writes, [])

    def test_ineligible_or_unavailable_pull_is_not_reported(self):
        for reason, mutate in {
            "fork": lambda g: g.pull["head"]["repo"].update(full_name="other/lober"),
            "unknown repo": lambda g: g.pull["head"].update(repo=None),
            "untrusted source": lambda g: g.pull["head"].update(ref="feat/other"),
            "draft": lambda g: g.pull.update(draft=True),
            "closed": lambda g: g.pull.update(state="closed"),
            "merged": lambda g: g.pull.update(merged=True),
            "conflict": lambda g: g.pull.update(mergeable=False),
            "calculating": lambda g: g.pull.update(mergeable=None),
            "missing merge": lambda g: g.pull.update(merge_commit_sha=None),
            "untrusted sha": lambda g: g.pull.update(merge_commit_sha="../../../other"),
            "missing protection": lambda g: setattr(g, "branch_protected", False),
            "outdated head": lambda g: setattr(g, "head_sha", OTHER),
            "wrong base parent": lambda g: setattr(g, "parents", [OTHER, HEAD]),
            "wrong head parent": lambda g: setattr(g, "parents", [BASE, OTHER]),
            "wrong tree": lambda g: setattr(g, "trial_tree", OTHER),
        }.items():
            with self.subTest(reason=reason):
                github = GitHub()
                mutate(github)
                github.run()
                self.assertEqual(github.verified, [])
                self.assertEqual(github.writes, [])

    def test_result_is_discarded_when_the_pull_or_branch_changes_during_verification(self):
        for change in ("branch", "pull"):
            with self.subTest(change=change):
                github = GitHub()
                if change == "branch":
                    github.change_after_verify = lambda: setattr(github, "head_sha", OTHER)
                else:
                    github.change_after_verify = lambda: github.pull.update(merge_commit_sha=OTHER)
                github.run()
                self.assertEqual(github.verified, [("test", HEAD)])
                self.assertEqual(github.writes, [])

    def test_fresh_unchanged_status_from_the_app_is_not_repeated(self):
        github = GitHub()
        github.statuses = [status("success")]
        github.run()
        self.assertEqual(github.writes, [])

    def test_status_from_another_author_does_not_suppress_our_status(self):
        github = GitHub()
        github.statuses = [status("success", creator=BOT_ID + 1)]
        github.run()
        self.assertEqual(len(github.writes), 1)

    def test_changed_verdict_replaces_earlier_success_even_if_old_failure_exists(self):
        github = GitHub()
        github.statuses = [status("success"), status("failure", age=2)]
        github.verdict = 1
        github.run()
        self.assertEqual(github.writes, [(f"{PREFIX}/statuses/{MERGE}", "enterprise-release", "failure")])

    def test_success_is_refreshed_before_github_expires_it(self):
        github = GitHub()
        github.statuses = [status("success", age=6)]
        github.run()
        self.assertEqual(len(github.writes), 1)

    def test_open_pull_list_uses_additional_pages(self):
        github = GitHub()
        github.extra_list_page = True
        github.run()
        self.assertEqual(len(github.writes), 1)

    def test_verifier_receives_only_stage_and_source_with_no_github_token(self):
        with (
            patch.dict(os.environ, {"GH_TOKEN": "secret", "LOBER_CI_APP_KEY": "/private/key"}),
            patch.object(report.subprocess, "run", return_value=subprocess.CompletedProcess([], 127)) as command,
        ):
            self.assertEqual(report.run_verifier("/trusted/verify", "test", HEAD), 127)
        self.assertEqual(command.call_args.args[0], ["/trusted/verify", "test", HEAD])
        self.assertEqual(command.call_args.kwargs["env"], {"PATH": os.defpath})

    def test_token_helper_accepts_stateless_installation_token(self):
        result = subprocess.CompletedProcess([], 0, stdout=b"ghs_42_header.payload.signature\n")
        with patch.object(report.subprocess, "run", return_value=result) as command:
            self.assertEqual(report.installation_token("/trusted/token"), "ghs_42_header.payload.signature")
        self.assertEqual(command.call_args.args[0], ["/trusted/token"])

    def test_token_helper_error_cannot_leak_command_output(self):
        error = subprocess.CalledProcessError(1, ["/trusted/token"], output=b"secret", stderr=b"private error")
        with patch.object(report.subprocess, "run", side_effect=error):
            with self.assertRaisesRegex(report.GitHubError, "^获取 GitHub App 安装令牌失败$"):
                report.installation_token("/trusted/token")

    def test_http_error_does_not_expose_credentials_or_response(self):
        client = report.GitHub("token-secret")
        failure = HTTPError("https://api.github.com", 403, "hidden error", {}, io.BytesIO(b"private body"))
        with patch.object(client.opener, "open", side_effect=failure):
            with self.assertRaisesRegex(report.GitHubError, "^GitHub HTTP 403$"):
                client.call(f"{PREFIX}/pulls")

    def test_http_request_uses_fixed_host_and_sends_status_as_json(self):
        client = report.GitHub("token-secret")
        response = io.BytesIO(b'{"id": 1}')
        with patch.object(client.opener, "open", return_value=response) as request:
            self.assertEqual(client.call(f"{PREFIX}/statuses/{MERGE}", {"state": "success"}), {"id": 1})
        submitted = request.call_args.args[0]
        self.assertEqual(submitted.full_url, f"https://api.github.com{PREFIX}/statuses/{MERGE}")
        self.assertEqual(submitted.get_method(), "POST")
        self.assertEqual(submitted.get_header("Authorization"), "Bearer token-secret")
        self.assertEqual(json.loads(submitted.data), {"state": "success"})


if __name__ == "__main__":
    unittest.main()
