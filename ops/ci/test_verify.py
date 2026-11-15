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

    def test_image_promotion_rejects_changed_log_and_mismatched_commit(self):
        image = "sha256:" + "a" * 64
        control, debian = "1" * 32, "2" * 32

        def write(path, data):
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(json.dumps(data), encoding="utf-8")

        for run_id, suite in ((control, "control"), (debian, "debian")):
            run = self.root / "runs" / run_id
            run.mkdir(parents=True)
            (run / "check.log").write_text("checked")
            write(run / "result.json", {"status": "PASS", "run_id": run_id, "commit": SHA,
                "check_exit_code": 0, "check_command": [f"/opt/image-{suite}"],
                "check_log_sha256": verify.digest(run / "check.log"), "image_id": image})
        write(self.root / "runs" / control / "control.json",
              {"status": "PASS", "count": 88, "failures": 0, "skipped": 0, "runtime_images": [image]})
        kvm = self.root / "runs" / debian / "kvm"
        terminal = {"state": "done", "events": [{"kind": kind} for kind in
                    ("machine.installing", "machine.init_start", "machine.ready")]}
        write(kvm / "terminal.json", terminal)
        write(kvm / "repeat-terminal.json", terminal)
        guest = {"id": "debian", "version": "13", "machine": "same", "root": "/dev/vda1"}
        write(kvm / "guest-identity.json", {"first": {**guest, "boot": "first"},
                                         "repeat": {**guest, "boot": "repeat"}})
        (kvm / "runtime-image.txt").write_text(image)
        (kvm / "repeat.pcap").write_bytes(bytes(40))
        self.assertEqual(verify.verify_image(self.root, SHA, control, debian), image)
        with self.assertRaises(ValueError):
            verify.verify_image(self.root, "2" * 40, control, debian)
        (self.root / "runs" / control / "check.log").write_text("changed")
        with self.assertRaises(ValueError):
            verify.verify_image(self.root, SHA, control, debian)

    def complete(self, stage, status="PASS"):
        (self.run / "execution.json").write_text(json.dumps({
            "sha": SHA, "branch": stage, "status": status, "run_id": "round-1",
            "binary_sha256": verify.digest(self.binary),
            "manifest_sha256": verify.digest(self.manifest),
        }), encoding="utf-8")

    def test_capacity_report_requires_original_container_and_full_window(self):
        image = "sha256:" + "a" * 64
        run = "kvm-capacity.ABCDEFGH"
        record = self.root / "capacity" / (run + ".json")
        record.parent.mkdir()
        instance = {"image_id": image, "container_id": "b" * 64, "pid": 42, "start_ticks": 100}
        expected = {"run_id": "frozen-run", "image_id": image, "container_capacity_run": run}
        proof = {"status": "PASS", "commit": SHA, "run_id": expected["run_id"],
                 "before": instance, "after": instance, "soak_seconds": 86400, "observed_seconds": 90000}
        record.write_text(json.dumps(proof))
        verify.require_capacity_image(self.root, SHA, expected)
        phase_records = {}
        for phase in ("seed", "normal", "burst", "soak", "evaluate"):
            path = self.root / "results" / phase / "result.json"
            path.parent.mkdir(parents=True)
            path.write_text(phase)
            phase_records[phase] = verify.digest(path)
        expected["capacity"] = {"evidence_root": str(self.root / "results")}
        record.write_text(json.dumps({**proof, "manifest_sha256": "manifest", "phase_records": phase_records}))
        verify.require_capacity_image(self.root, SHA, expected, "manifest")
        (self.root / "results" / "soak" / "result.json").write_text("replaced")
        with self.assertRaises(ValueError):
            verify.require_capacity_image(self.root, SHA, expected, "manifest")
        for invalid in ({**proof, "soak_seconds": 3600}, {**proof, "soak_seconds": float("nan")},
                        {**proof, "observed_seconds": 86399},
                        {**proof, "after": {**instance, "image_id": "sha256:" + "c" * 64}},
                        {**proof, "commit": "2" * 40}):
            record.write_text(json.dumps(invalid))
            with self.assertRaises(ValueError):
                verify.require_capacity_image(self.root, SHA, expected)
        record.unlink()
        with self.assertRaises(OSError):
            verify.require_capacity_image(self.root, SHA, expected)

    @patch("verify.require_batch_image")
    @patch("verify.require_capacity_image")
    @patch("verify.verify_suite_image")
    @patch("verify.verify_control_image")
    def test_container_release_rejects_missing_and_mismatched_suite_before_reporting(self, paired, suite_image, capacity, batch):
        image = "sha256:" + "a" * 64
        paired.return_value = image
        suite_image.return_value = image
        manifest = {"image_id": image, "container_control_run": "1" * 32, "container_debian_run": "2" * 32}
        manifest.update(container_flows_run="3" * 32, container_events_run="4" * 32, container_recovery_run="5" * 32)
        suites = ("control", "flows", "events", "recovery",
                  "pxe_state", "real_install", "capacity_soak", "supported_upgrade")
        for suite in suites:
            (self.run / "logs" / (suite + ".json")).write_text(json.dumps({"image_id": image}))
        verify.require_release_image(self.root, SHA, manifest, self.run / "logs")
        batch.assert_called_once()
        capacity.assert_called_once()
        self.assertEqual(suite_image.call_count, 3)
        (self.run / "logs" / "flows.json").write_text(json.dumps({"image_id": "sha256:" + "b" * 64}))
        with self.assertRaisesRegex(ValueError, "flows"):
            verify.require_release_image(self.root, SHA, manifest, self.run / "logs")
        (self.run / "logs" / "flows.json").unlink()
        with self.assertRaisesRegex(ValueError, "flows"):
            verify.require_release_image(self.root, SHA, manifest, self.run / "logs")

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
