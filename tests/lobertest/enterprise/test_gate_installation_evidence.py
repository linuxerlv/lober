"""合成记录仅测试接入规则，不代表真实网络启动或安装。"""
import json
import subprocess
import sys
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import installation_evidence as evidence
import test_production_capacity as capacity_fixtures


class InstallationEvidenceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.source.mkdir()
        self.files = {}
        for name in evidence.TRUSTED_FILES:
            path = self.source / name
            path.parent.mkdir(parents=True, exist_ok=True)
            path.write_text(name)
            self.files[name] = evidence.digest(path)
        self.identity = {"run_id": "run", "product_sha": "a" * 40,
                         "test_sha": "b" * 40, "binary_sha256": "c" * 64}
        self.checker = self.root / "cloud_install.py"
        self.checker.write_text((self.source / evidence.CHECKER).read_text())
        self.report = self.write("install.json", {"status": "PASS", "synthetic": False})
        helpers = {}
        for name in evidence.HELPERS:
            helper = self.root / Path(name).name
            helper.write_text((self.source / name).read_text())
            helpers[str(helper)] = self.files[name]
        common = {**self.identity, "installation_checkers": {str(self.checker): self.files[evidence.CHECKER]},
                  "installation_helpers": helpers, "installation_targets": {"target": {}}}
        self.common = self.write("common.json", common)
        self.manifest = {**self.identity, "files": self.files, "capacity": {
            "run_identity_file": str(self.common), "run_identity_sha256": evidence.digest(self.common),
            "concurrent_installation": {"path": str(self.report), "sha256": evidence.digest(self.report)}}}

    def write(self, name, value):
        path = self.root / name
        path.write_text(json.dumps(value))
        path.chmod(0o600)
        return path

    def verify(self):
        return evidence.verify_installation(self.manifest, self.source, self.root)

    def test_only_accepts_existing_checker_verdict(self):
        with patch.object(evidence.capacity, "installation_problems", return_value=[]) as checker:
            self.assertEqual(self.verify(), [])
            checker.assert_called_once()
        with patch.object(evidence.capacity, "installation_problems", return_value=["missing capture"]):
            self.assertTrue(self.verify())

    def test_rejects_changed_raw_report_and_missing_contract(self):
        self.report.write_text('{"status":"PASS"}')
        self.assertTrue(self.verify())
        del self.manifest["capacity"]["run_identity_file"]
        self.assertTrue(self.verify())

    def test_rejects_changed_frozen_checker(self):
        self.checker.write_text("different checker")
        self.assertTrue(self.verify())

    def test_cli_missing_real_report_fails_both_results(self):
        source = Path(evidence.__file__).resolve().parents[3]
        binary = self.root / "binary"
        archive = self.root / "source.tar"
        binary.write_bytes(b"binary")
        archive.write_bytes(b"archive")
        files = {name: evidence.digest(source / name) for name in (
            "tests/lobertest/enterprise/gate.py", "tests/lobertest/enterprise/production_capacity.py",
            "tests/lobertest/enterprise/installation_evidence.py")}
        manifest = {**self.identity, "binary": str(binary), "binary_sha256": evidence.digest(binary),
                    "source_archive": str(archive), "source_archive_sha256": evidence.digest(archive),
                    "files": files}
        path = self.write("candidate.json", manifest)
        for kind, names in (("installation", ("pxe_state", "real_install")),
                            ("capacity", ("capacity_soak",))):
            with self.subTest(kind=kind):
                out = self.root / kind
                proc = subprocess.run([sys.executable, "-I", evidence.__file__, "--manifest", str(path),
                                       "--output", str(out), "--kind", kind,
                                       "--product-sha", manifest["product_sha"]],
                                      capture_output=True, text=True)
                self.assertEqual(proc.returncode, 1)
                for name in names:
                    self.assertEqual(json.loads((out / (name + ".json")).read_text())["status"], "FAIL")

    def test_synthetic_original_chain_is_checked_end_to_end_and_tampering_is_rejected(self):
        # 夹具中的检查器和抓包都是合成的；只验证接入兼容性，不能作为验收证据。
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            identity, contract, report = capacity_fixtures.ReviewBoundaryTests().install_fixture(root, explicit=True)
            source = root / "source"
            source.mkdir()
            checker_text = (root / "mock-checker-a.py").read_text()
            files = {}
            for name in evidence.TRUSTED_FILES:
                path = source / name
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_text(checker_text if name == evidence.CHECKER else name)
                files[name] = evidence.digest(path)
            checkers = {}
            for suffix in ("a", "b"):
                path = root / ("checker-" + suffix) / "cloud_install.py"
                path.parent.mkdir()
                path.write_text(checker_text)
                checkers[suffix] = path
            contract["installation_checkers"] = {str(path): files[evidence.CHECKER] for path in checkers.values()}
            helpers = {}
            for name in evidence.HELPERS:
                path = root / Path(name).name
                path.write_text(name)
                helpers[str(path)] = files[name]
            contract["installation_helpers"] = helpers
            common = {name: identity[name] for name in ("run_id", "product_sha", "test_sha", "binary_sha256")}
            common.update(contract)
            common_file = root / "common.json"
            common_file.write_text(json.dumps(common))
            common_file.chmod(0o600)
            identity["run_identity_sha256"] = evidence.digest(common_file)
            report["run_identity_sha256"] = identity["run_identity_sha256"]
            for mode in ("parallel", "serial"):
                for case in report[mode]:
                    suffix = case["target_uuid"][-1]
                    checker = checkers[suffix]
                    case["checker"] = {"path": str(checker.relative_to(root)), "sha256": evidence.digest(checker)}
                    case["run_identity_sha256"] = identity["run_identity_sha256"]
                    refs = case["files"]
                    manifest_file = root / refs["manifest"]["path"]
                    data = json.loads(manifest_file.read_text())
                    data["run_identity_sha256"] = identity["run_identity_sha256"]
                    manifest_file.write_text(json.dumps(data))
                    refs["manifest"]["sha256"] = evidence.digest(manifest_file)
                    result_file = root / refs["result"]["path"]
                    data = json.loads(result_file.read_text())
                    data.update(run_identity_sha256=identity["run_identity_sha256"],
                                executor_sha256=evidence.digest(checker),
                                manifest_sha256=refs["manifest"]["sha256"])
                    data["files"][manifest_file.name] = refs["manifest"]["sha256"]
                    result_file.write_text(json.dumps(data))
                    refs["result"]["sha256"] = evidence.digest(result_file)
            report_file = root / "report.json"
            report_file.write_text(json.dumps(report))
            report_file.chmod(0o600)
            manifest = {**{k: identity[k] for k in ("run_id", "product_sha", "test_sha", "binary_sha256")},
                        "files": files, "capacity": {"run_identity_file": str(common_file),
                            "run_identity_sha256": evidence.digest(common_file),
                            "concurrent_installation": {"path": str(report_file), "sha256": evidence.digest(report_file)}}}
            with patch.object(evidence.capacity, "BASE", root):
                self.assertEqual(evidence.verify_installation(manifest, source, root), [])
                original = root / report["parallel"][0]["files"]["qualification"]["path"]
                original.write_text(original.read_text() + " ")
                self.assertTrue(evidence.verify_installation(manifest, source, root))

    def test_cli_invalid_source_invalidates_old_pass(self):
        manifest = self.write("bad-candidate.json", {"files": {
            "tests/lobertest/enterprise/installation_evidence.py": "0" * 64}})
        out = self.root / "stale"
        out.mkdir()
        for name in ("pxe_state", "real_install"):
            (out / (name + ".json")).write_text('{"status":"PASS"}')
        proc = subprocess.run([sys.executable, "-I", evidence.__file__, "--manifest", str(manifest),
                               "--output", str(out), "--product-sha", "a" * 40],
                              capture_output=True, text=True)
        self.assertNotEqual(proc.returncode, 0)
        for name in ("pxe_state", "real_install"):
            self.assertEqual(json.loads((out / (name + ".json")).read_text())["status"], "RUNNING")
