"""合成输入只测试门禁拒绝规则，不代表真实安装或容量验收。"""
import unittest
import hashlib
import json
from pathlib import Path
import subprocess
import sys
import tempfile

from gate import evaluate, evaluate_release_profile


class QualificationIdentityTests(unittest.TestCase):
    def test_release_cannot_drop_required_suites_or_unfreeze_profile(self):
        self.assertTrue(evaluate_release_profile(["backend"], {}, Path(".")))

    def test_release_profile_accepts_frozen_complete_requirements(self):
        root = Path(__file__).resolve().parents[3]
        names = ("tests/lobertest/enterprise/gate.py", "tests/lobertest/enterprise/profiles.json")
        files = {name: hashlib.sha256((root / name).read_bytes()).hexdigest() for name in names}
        required = json.loads((root / names[1]).read_text())["release"]
        self.assertEqual(evaluate_release_profile(required, {"files": files}, root), [])
        self.assertTrue(evaluate_release_profile(required, {"files": {names[0]: files[names[0]]}}, root))

    def test_cli_rejects_profile_omitted_from_manifest(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            tools = source / "tests/lobertest/enterprise"
            tools.mkdir(parents=True)
            (tools / "gate.py").write_bytes(Path(__file__).with_name("gate.py").read_bytes())
            (tools / "profiles.json").write_text(json.dumps({"release": ["backend"]}))
            binary, archive = root / "binary", root / "archive"
            binary.write_bytes(b"binary"); archive.write_bytes(b"archive")
            digest = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
            manifest = {"run_id": "run", "product_sha": "a"*40, "test_sha": "b"*40,
                        "binary": str(binary), "binary_sha256": digest(binary),
                        "source_archive": str(archive), "source_archive_sha256": digest(archive),
                        "files": {"tests/lobertest/enterprise/gate.py": digest(tools / "gate.py")}}
            mp = root / "manifest.json"; mp.write_text(json.dumps(manifest))
            results = root / "results"; results.mkdir()
            log = results / "backend.log"; log.write_text("synthetic")
            row = {"status": "PASS", "count": 1, "failed": 0, "skipped": 0,
                   "run_id": "run", "product_sha": "a"*40, "manifest_sha256": digest(mp),
                   "evidence": log.name, "evidence_sha256": digest(log)}
            (results / "backend.json").write_text(json.dumps(row))
            proc = subprocess.run([sys.executable, "-I", str(tools / "gate.py"), "--profile", "release",
                                   "--manifest", str(mp), "--results", str(results), "--product-sha", "a"*40],
                                  capture_output=True, text=True)
            self.assertNotEqual(proc.returncode, 0, proc.stdout)
    def test_qualification_results_are_bound_to_tests_and_binary(self):
        manifest = {"run_id": "run", "_sha256": "manifest", "test_sha": "tests", "binary_sha256": "binary"}
        row = {"status": "PASS", "count": 1, "failed": 0, "skipped": 0,
               "product_sha": "product", "evidence": "evidence.log", "run_id": "run",
               "manifest_sha256": "manifest", "test_sha": "tests", "binary_sha256": "binary"}
        for name in ("pxe_state", "real_install", "capacity_soak", "supported_upgrade"):
            for field in ("test_sha", "binary_sha256"):
                with self.subTest(name=name, field=field):
                    self.assertTrue(evaluate([name], {name: {**row, field: "old"}}, "product", manifest))

    def test_upgrade_pass_label_without_supported_upgrade_verifier_is_refused(self):
        manifest = {"run_id": "run", "_sha256": "manifest", "test_sha": "tests", "binary_sha256": "binary"}
        row = {"status": "PASS", "count": 1, "failed": 0, "skipped": 0,
               "product_sha": "product", "evidence": "evidence.log", "run_id": "run",
               "manifest_sha256": "manifest", "test_sha": "tests", "binary_sha256": "binary"}
        self.assertTrue(evaluate(["supported_upgrade"], {"supported_upgrade": row}, "product", manifest))


if __name__ == "__main__":
    unittest.main()
