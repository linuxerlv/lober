import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

import verify


SHA = "1" * 40


class VerifyTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.run = self.root / SHA
        self.run.mkdir()
        (self.run / "source").mkdir()
        (self.run / "logs").mkdir()
        self.binary = self.run / "lober"
        self.binary.write_bytes(b"candidate")
        self.manifest = self.run / "candidate.json"
        self.manifest.write_text(json.dumps({
            "product_sha": SHA, "test_sha": SHA, "binary": str(self.binary),
            "binary_sha256": verify.digest(self.binary), "run_id": "round-1",
        }), encoding="utf-8")

    def complete(self, stage, status="PASS"):
        (self.run / "execution.json").write_text(json.dumps({
            "sha": SHA, "branch": stage, "status": status, "run_id": "round-1",
            "binary_sha256": verify.digest(self.binary),
            "manifest_sha256": verify.digest(self.manifest),
        }), encoding="utf-8")

    def ready(self):
        (self.run / "dev-ready.json").write_text(json.dumps({
            "sha": SHA, "binary_sha256": verify.digest(self.binary),
            "manifest_sha256": verify.digest(self.manifest),
        }), encoding="utf-8")

    @patch("verify.subprocess.run")
    def test_test_verification_runs_only_release_and_propagates_failure(self, command):
        self.complete("test", "BLOCKED")
        command.return_value.returncode = 1
        self.assertFalse(verify.verify("test", SHA, self.root))
        argv = command.call_args.args[0]
        self.assertEqual(argv[argv.index("--profile") + 1], "release")
        self.assertEqual(Path(argv[argv.index("--source-root") + 1]), self.run / "source")

    @patch("verify.subprocess.run")
    def test_dev_requires_matching_deployment_and_control_suite(self, command):
        self.complete("dev")
        command.return_value.returncode = 0
        self.assertFalse(verify.verify("dev", SHA, self.root))
        command.assert_not_called()
        self.ready()
        command.return_value.stdout = json.dumps({
            "profile": "control-complete", "product_sha": SHA, "status": "PASS", "problems": [],
        })
        self.assertTrue(verify.verify("dev", SHA, self.root))
        self.assertIn("control-complete", command.call_args.args[0])
        self.binary.write_bytes(b"different")
        self.assertFalse(verify.verify("dev", SHA, self.root))

    @patch("verify.subprocess.run")
    def test_mismatched_candidate_never_runs_gate(self, command):
        doc = json.loads(self.manifest.read_text(encoding="utf-8"))
        doc["test_sha"] = "2" * 40
        self.manifest.write_text(json.dumps(doc), encoding="utf-8")
        self.assertFalse(verify.verify("test", SHA, self.root))
        self.assertFalse(verify.verify("test", "../outside", self.root))
        command.assert_not_called()

    def test_installed_release_gate_rejects_all_missing_evidence(self):
        import shutil
        import subprocess
        import sys

        enterprise = self.run / "source/tests/lobertest/enterprise"
        enterprise.mkdir(parents=True)
        for name in ("gate.py", "profiles.json"):
            shutil.copyfile(verify.GATE.with_name(name), enterprise / name)
        marker = self.run / "candidate-code-executed"
        (enterprise / "production_capacity.py").write_text(
            f"from pathlib import Path\nPath({str(marker)!r}).touch()\nraise SystemExit(0)\n",
            encoding="utf-8",
        )
        (self.run / "logs/pxe_state.json").write_text('{"status":"PASS"}', encoding="utf-8")
        archive = self.run / "source.tar"
        archive.write_bytes(b"fixture archive")
        candidate = json.loads(self.manifest.read_text(encoding="utf-8"))
        candidate.update(source_archive=str(archive), source_archive_sha256=verify.digest(archive),
                         files={f"tests/lobertest/enterprise/{p.name}": verify.digest(p)
                                for p in enterprise.iterdir()})
        self.manifest.write_text(json.dumps(candidate), encoding="utf-8")
        result = subprocess.run([
            sys.executable, "-I", str(verify.GATE), "--profile", "release",
            "--source-root", str(self.run / "source"), "--results", str(self.run / "logs"),
            "--product-sha", SHA, "--manifest", str(self.manifest),
        ], capture_output=True, check=False)
        report = json.loads(result.stdout)
        profiles = json.loads((enterprise / "profiles.json").read_text(encoding="utf-8"))
        self.assertEqual(len(profiles["release"]), 16)
        self.assertEqual(result.returncode, 1)
        self.assertFalse(marker.exists())
        for suite in set(profiles["release"]) - {"pxe_state"}:
            self.assertIn(f"{suite}: required result missing", report["problems"])
        self.assertFalse(verify.verify("test", SHA, self.root))

    @patch("verify.subprocess.run")
    def test_failed_worker_is_blocked_even_with_suite_reports(self, command):
        self.complete("test", "FAIL")
        self.assertFalse(verify.verify("test", SHA, self.root))
        command.assert_not_called()