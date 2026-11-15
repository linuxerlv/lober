#!/usr/bin/env python3
"""读取 GitHub 上待合并的申请，并把测试结果写到试合并提交。"""

import argparse
import json
import os
import re
import subprocess
import sys
from datetime import datetime, timedelta, timezone
from itertools import count
from typing import NamedTuple
from urllib.error import HTTPError
from urllib.parse import urlencode
from urllib.request import HTTPRedirectHandler, Request, build_opener

ROUTES = {
    "preview": ("test", "enterprise-release"),
    "test": ("dev", "dev-ready"),
}
PAGE_SIZE = 100
STATUS_REFRESH = timedelta(days=6)


class GitHubError(Exception):
    pass


class RejectRedirect(HTTPRedirectHandler):
    def redirect_request(self, req, fp, code, msg, headers, newurl):
        # 避免把带凭据的请求转发给其他地址。
        return None


class GitHub:
    def __init__(self, token):
        self.token = token
        self.opener = build_opener(RejectRedirect())

    def call(self, path, payload=None):
        headers = {
            "Accept": "application/vnd.github+json",
            "Authorization": f"Bearer {self.token}",
            "User-Agent": "lober-promotion-reporter",
            # 此版本的合并请求接口仍提供试合并提交号。
            "X-GitHub-Api-Version": "2022-11-28",
        }
        body = None
        if payload is not None:
            headers["Content-Type"] = "application/json"
            body = json.dumps(payload).encode("utf-8")
        request = Request(f"https://api.github.com{path}", data=body, headers=headers)
        try:
            with self.opener.open(request, timeout=30) as response:
                return json.load(response)
        except HTTPError as exc:
            exc.close()
            raise GitHubError(f"GitHub HTTP {exc.code}") from None
        except (OSError, ValueError):
            raise GitHubError("GitHub 请求失败或返回格式错误") from None


class Trial(NamedTuple):
    base_sha: str
    head_sha: str
    merge_sha: str


def valid_sha(value):
    return isinstance(value, str) and re.fullmatch(r"[0-9a-f]{40}", value) is not None


def pages(github, path, **filters):
    for page in count(1):
        query = urlencode(dict(filters, per_page=PAGE_SIZE, page=page))
        results = github.call(f"{path}?{query}")
        if not isinstance(results, list):
            raise GitHubError("GitHub 返回的列表格式错误")
        yield from results
        if len(results) < PAGE_SIZE:
            return


def current_trial(github, repository, prefix, number, base, source):
    pull = github.call(f"{prefix}/pulls/{number}")
    try:
        if (
            pull["number"] != number
            or pull["state"] != "open"
            or pull["draft"] is not False
            or pull["merged"] is not False
            or pull["mergeable"] is not True
        ):
            return None
        trial = Trial(pull["base"]["sha"], pull["head"]["sha"], pull["merge_commit_sha"])
        if not all(map(valid_sha, trial)) or len(set(trial)) != 3:
            return None
        for key, branch, sha in (("base", base, trial.base_sha), ("head", source, trial.head_sha)):
            side = pull[key]
            if side["ref"] != branch or side["repo"]["full_name"].casefold() != repository.casefold():
                return None
            live = github.call(f"{prefix}/branches/{branch}")
            if live["name"] != branch or live["protected"] is not True or live["commit"]["sha"] != sha:
                return None
    except (AttributeError, KeyError, TypeError):
        return None
    return trial


def matching_tree(github, prefix, trial):
    source = github.call(f"{prefix}/git/commits/{trial.head_sha}")
    merged = github.call(f"{prefix}/git/commits/{trial.merge_sha}")
    try:
        return (
            source["sha"] == trial.head_sha
            and merged["sha"] == trial.merge_sha
            and [parent["sha"] for parent in merged["parents"]] == [trial.base_sha, trial.head_sha]
            and valid_sha(source["tree"]["sha"])
            and merged["tree"]["sha"] == source["tree"]["sha"]
        )
    except (KeyError, TypeError):
        return False


def unchanged_status(github, prefix, merge_sha, context, creator_id, state, now):
    for status in pages(github, f"{prefix}/commits/{merge_sha}/statuses"):
        if status.get("context", "").casefold() != context or (status.get("creator") or {}).get("id") != creator_id:
            continue
        if status.get("state") != state:
            return False
        try:
            created = datetime.fromisoformat(status["created_at"].replace("Z", "+00:00"))
            return timedelta() <= now - created < STATUS_REFRESH
        except (AttributeError, KeyError, TypeError, ValueError):
            return False
    return False


def report_pull(github, repository, prefix, number, base, source, context, verify, creator_id, now, *, dry_run=False):
    trial = current_trial(github, repository, prefix, number, base, source)
    if trial is None or not matching_tree(github, prefix, trial):
        return
    try:
        result = verify(source, trial.head_sha)
    except (OSError, subprocess.SubprocessError):
        result = None
    passed = type(result) is int and result == 0
    state = "success" if passed else "failure"
    if not dry_run and unchanged_status(github, prefix, trial.merge_sha, context, creator_id, state, now):
        return
    # 检查可能耗时。发状态前，再确认申请和两个分支仍指向同一批提交。
    if current_trial(github, repository, prefix, number, base, source) != trial:
        return
    if dry_run:
        print(f"PR #{number}: {context} {state} on {trial.merge_sha} (dry-run)")
        return
    github.call(
        f"{prefix}/statuses/{trial.merge_sha}",
        {
            "context": context,
            "state": state,
            "description": "Verified test evidence" if passed else "Missing or failing test evidence",
        },
    )
    print(f"PR #{number}: {context} {state}")


def poll_branch(github, repository, base, verify, creator_id, now, *, dry_run=False):
    prefix = f"/repos/{repository}"
    source, context = ROUTES[base]
    owner, _ = repository.split("/")
    healthy = True
    for summary in pages(github, f"{prefix}/pulls", state="open", base=base, head=f"{owner}:{source}"):
        if not isinstance(summary, dict) or type(summary.get("number")) is not int or summary["number"] <= 0:
            continue
        number = summary["number"]
        try:
            report_pull(github, repository, prefix, number, base, source, context, verify, creator_id, now,
                        dry_run=dry_run)
        except GitHubError as exc:
            print(f"PR #{number}: {exc}", file=sys.stderr)
            healthy = False
    return healthy


def absolute_path(path):
    if not os.path.isabs(path):
        raise argparse.ArgumentTypeError("请提供可执行文件的绝对路径")
    return path


def installation_token(command):
    try:
        result = subprocess.run(
            [command],
            stdin=subprocess.DEVNULL,
            stdout=subprocess.PIPE,
            stderr=subprocess.DEVNULL,
            timeout=30,
            check=True,
        )
        token = result.stdout.strip().decode("ascii")
    except (OSError, UnicodeError, subprocess.SubprocessError):
        raise GitHubError("获取 GitHub App 安装令牌失败") from None
    if re.fullmatch(r"[A-Za-z0-9_.-]+", token) is None:
        raise GitHubError("GitHub App 安装令牌格式错误")
    return token


def run_verifier(command, stage, sha):
    return subprocess.run(
        [command, stage, sha],
        stdin=subprocess.DEVNULL,
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
        env={"PATH": os.defpath},
        timeout=120,
        check=False,
    ).returncode


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--token-command", required=True, type=absolute_path)
    parser.add_argument("--verify-command", required=True, type=absolute_path)
    parser.add_argument("--status-creator-id", required=True, type=int)
    parser.add_argument("--dry-run", action="store_true", help="核对试合并并显示结论，不发 GitHub 状态")
    args = parser.parse_args()
    if (
        re.fullmatch(r"[A-Za-z0-9-]+/[A-Za-z0-9_.-]+", args.repository) is None
        or args.repository.split("/")[1] in (".", "..")
    ):
        parser.error("--repository 格式应为 owner/repo")
    if args.status_creator_id <= 0:
        parser.error("--status-creator-id 必须为正整数")
    try:
        github = GitHub(installation_token(args.token_command))
    except GitHubError as exc:
        print(exc, file=sys.stderr)
        return 1
    healthy = True
    for base in ROUTES:
        try:
            if not poll_branch(
                github,
                args.repository,
                base,
                lambda stage, sha: run_verifier(args.verify_command, stage, sha),
                args.status_creator_id,
                datetime.now(timezone.utc),
                dry_run=args.dry_run,
            ):
                healthy = False
        except GitHubError as exc:
            print(f"{base}: {exc}", file=sys.stderr)
            healthy = False
    return 0 if healthy else 1


if __name__ == "__main__":
    sys.exit(main())
