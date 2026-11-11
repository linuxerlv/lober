import io
from pathlib import Path
import tarfile
import tempfile
import unittest
from unittest.mock import patch

import worker


class WorkerTests(unittest.TestCase):
    def test_remote_ref_requires_one_full_sha_for_requested_branch(self):
        sha = "1" * 40
        self.assertEqual(worker.parse_ref(f"{sha}\trefs/heads/dev\n", "dev"), sha)
        for text in ("", f"{sha}\trefs/heads/test\n", f"{sha}\trefs/heads/dev\n" * 2):
            with self.subTest(text=text), self.assertRaises(ValueError):
                worker.parse_ref(text, "dev")

    def test_github_url_does_not_allow_credentials_or_extra_arguments(self):
        self.assertTrue(worker.valid_repository("https://github.com/linuxerlv/lober.git"))
        for url in ("file:///etc", "https://x@github.com/a/b", "https://github.com/a/b?x=1",
                    "https://github.com/a/b/extra", "https://github.com.evil/a/b"):
            with self.subTest(url=url):
                self.assertFalse(worker.valid_repository(url))

    def test_archive_rejects_links_before_extraction(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive = root / "candidate.tar"
            with tarfile.open(archive, "w") as stream:
                safe = tarfile.TarInfo("go.mod")
                safe.size = 1
                stream.addfile(safe, io.BytesIO(b"x"))
                link = tarfile.TarInfo("escape")
                link.type = tarfile.SYMTYPE
                link.linkname = "../outside"
                stream.addfile(link)
            with self.assertRaises(ValueError):
                worker.extract_source(archive, root / "source")
            self.assertFalse((root / "source/go.mod").exists())

    def test_go_summary_counts_failures_and_distinguishes_opt_in_media(self):
        output = '''
{"Action":"pass","Package":"example","Test":"TestOne"}
{"Action":"skip","Package":"example","Test":"TestNeedsDatabase"}
{"Action":"skip","Package":"github.com/lober/lober/internal/application/media","Test":"TestImportRealESXi/7"}
{"Action":"fail","Package":"example/broken","Test":"TestFails"}
{"Action":"fail","Package":"example/broken"}
{"Action":"skip","Package":"example/no-tests"}
'''
        self.assertEqual(worker.go_summary(output, optional_media=True), {
            "count": 4, "failed": 1, "skipped": 1, "package_failures": 1, "optional_skips": [
                "github.com/lober/lober/internal/application/media:TestImportRealESXi/7"],
        })
        self.assertEqual(worker.go_summary(output)["skipped"], 2)

    def test_frontend_counts_both_vitest_suites_and_node_results(self):
        output = """
 Tests  13 passed | 1 skipped (14)
 Tests  6 passed (6)
# tests 9
# pass 8
# fail 1
# cancelled 0
# skipped 0
# todo 0
"""
        self.assertEqual(worker.frontend_summary(output), {
            "count": 29, "failed": 1, "skipped": 1,
        })
        self.assertIsNone(worker.frontend_summary(output.split("# tests")[0]))

    def test_build_environment_drops_host_credentials_and_test_env(self):
        env = worker.build_environment({
            "PATH": "/usr/bin", "HOME": "/var/lib/lober-ci-test",
            "LOBER_TEST_PG_DSN": "private", "GITHUB_TOKEN": "private",
            "LOBER_CI_DEV_DEPLOY": "/root/helper", "GOFLAGS": "-run=^$",
        })
        for key in ("GITHUB_TOKEN", "LOBER_TEST_PG_DSN", "LOBER_CI_DEV_DEPLOY", "GOFLAGS"):
            self.assertNotIn(key, env)
        self.assertEqual(env["HOME"], "/var/lib/lober-ci-test")

    def test_backend_commands_use_service_private_tmp(self):
        with tempfile.TemporaryDirectory() as temporary:
            candidate = worker.Candidate("test", Path(temporary), "1" * 40, "", {
                "LOBER_TEST_PG_DSN": "fixture",
            })
            candidate.logs.mkdir(parents=True)
            with patch.object(candidate, "command", side_effect=RuntimeError("stop after first command")) as command:
                with self.assertRaisesRegex(RuntimeError, "stop after first command"):
                    candidate.backend()
            self.assertEqual(command.call_args.kwargs["env"]["TMPDIR"], "/tmp")
            self.assertEqual(candidate.test_env["TMPDIR"], str(candidate.path / "scratch"))

    def test_test_retry_preserves_handoff_and_stays_blocked_on_release_gate(self):
        with tempfile.TemporaryDirectory() as temporary:
            candidate = worker.Candidate("test", Path(temporary), "1" * 40, "", {})
            candidate.logs.mkdir(parents=True)
            candidate.manifest.write_text('{"run_id":"frozen","binary_sha256":"checksum"}')
            worker.write_json(candidate.execution, {"status": "BLOCKED"})
            handoff = candidate.logs / "real_install.json"
            handoff.write_bytes(b"manual evidence")
            with (patch.object(candidate, "frontend", return_value=True),
                  patch.object(candidate, "backend", return_value=True),
                  patch.object(candidate, "enterprise", return_value=True),
                  patch.object(candidate, "fuzz", return_value=True),
                  patch.object(candidate, "scan", return_value=True),
                  patch.object(candidate, "audit", return_value=True),
                  patch.object(candidate, "real_media", return_value=None),
                  patch.object(candidate, "gate", return_value=1),
                  patch.object(candidate, "deployment", side_effect=AssertionError("test attempted deploy"))):
                self.assertEqual(candidate.run(retry=True), 0)
            self.assertEqual(worker.read_json(candidate.execution)["status"], "BLOCKED")
            self.assertEqual(handoff.read_bytes(), b"manual evidence")
            self.assertEqual(candidate.manifest.read_text(), '{"run_id":"frozen","binary_sha256":"checksum"}')

    def test_smoke_failure_leaves_no_dev_ready_marker(self):
        with tempfile.TemporaryDirectory() as temporary:
            candidate = worker.Candidate("dev", Path(temporary), "1" * 40, "", {})
            candidate.logs.mkdir(parents=True)
            candidate.binary.write_bytes(b"lober")
            worker.write_json(candidate.manifest, {"binary_sha256": worker.digest(candidate.binary)})
            calls = []

            def execute(name, argv, **kwargs):
                calls.append((name, argv[1:]))
                return int(name == "smoke")

            with (patch.object(worker, "admin_executable", return_value=Path("/admin/helper")),
                  patch.object(worker, "current_head", return_value=candidate.sha),
                  patch.object(candidate, "command", side_effect=execute),
                  self.assertRaisesRegex(RuntimeError, "smoke")):
                candidate.deployment()
            self.assertEqual(calls, [(name, [candidate.binary, candidate.sha])
                                     for name in ("deploy", "smoke")])
            self.assertFalse(candidate.ready.exists())


if __name__ == "__main__":
    unittest.main()

class RecoveryIsolationTests(unittest.TestCase):
    def test_faults_require_a_configured_namespace(self):
        self.assertFalse(worker.isolated_recovery_namespace({}))
        self.assertFalse(worker.isolated_recovery_namespace({
            "LOBER_CI_RECOVERY_NETNS": "/tmp/not-a-network-namespace",
        }))

class QualificationHandoffTests(unittest.TestCase):
    def test_attach_requires_same_run_and_refuses_replacement(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary).resolve()
            config = root / "qualification-config.json"
            candidate = worker.Candidate("test", root, "1" * 40, "", {
                "LOBER_CI_CAPACITY_MANIFEST": str(config),
            })
            evidence = candidate.path / "qualification"
            evidence.mkdir(parents=True)
            identity = evidence / "identity.json"
            frozen = {"run_id": "round", "product_sha": candidate.sha,
                      "test_sha": candidate.sha, "binary_sha256": "checksum"}
            worker.write_json(candidate.manifest, frozen)
            worker.write_json(identity, {**frozen, "run_id": "old"})
            qualification = {"run_identity_file": str(identity),
                             "run_identity_sha256": worker.digest(identity)}
            worker.write_json(config, qualification)
            with self.assertRaisesRegex(ValueError, "另一轮"):
                candidate.attach_qualification()
            worker.write_json(identity, frozen)
            qualification["run_identity_sha256"] = worker.digest(identity)
            worker.write_json(config, qualification)
            candidate.attach_qualification()
            self.assertEqual(worker.read_json(candidate.manifest),
                             {**frozen, "capacity": qualification})
            worker.write_json(config, {**qualification, "changed": True})
            with self.assertRaisesRegex(ValueError, "已经冻结"):
                candidate.attach_qualification()
