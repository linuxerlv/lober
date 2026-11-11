"""伪造缓存只用于测试检查器本身，不代表任何真实验收。"""
import hashlib
import json
import os
from pathlib import Path
import py_compile
import tempfile
import unittest
import subprocess
import sys

import cloud_install
import production_capacity
import gate


class VerifiedLoaderTests(unittest.TestCase):
    def test_same_timestamp_bytecode_cannot_replace_frozen_source(self):
        for module in (cloud_install, production_capacity, gate):
            with self.subTest(module=module.__name__), tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp) / "checker.py"
                path.write_text("VALUE = 'evil'\n")
                os.utime(path, (1700000000, 1700000000))
                py_compile.compile(str(path), doraise=True)
                path.write_text("VALUE = 'good'\n")
                os.utime(path, (1700000000, 1700000000))
                expected = hashlib.sha256(path.read_bytes()).hexdigest()
                loaded = module.load_verified_module(path, expected, "test_frozen_checker")
                self.assertEqual(loaded.VALUE, "good")

    def test_wrong_source_digest_is_rejected(self):
        for module in (cloud_install, production_capacity, gate):
            with self.subTest(module=module.__name__), tempfile.TemporaryDirectory() as tmp:
                path = Path(tmp) / "checker.py"
                path.write_text("VALUE = 'good'\n")
                with self.assertRaises(ValueError):
                    module.load_verified_module(path, "0" * 64, "test_frozen_checker")

    def test_entrypoints_do_not_execute_poisoned_module_caches(self):
        for target in ("gate.py", "installation_evidence.py", "production_capacity.py"):
            with self.subTest(target=target):
                self.check_entrypoints_ignore_cache(target)

    def test_entrypoints_reject_nonisolated_python_before_local_module_import(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            marker = root / "injected"
            for name in ("gate.py", "installation_evidence.py"):
                (root / name).write_bytes((Path(gate.__file__).parent / name).read_bytes())
            (root / "argparse.py").write_text("from pathlib import Path\nPath(" + repr(str(marker)) + ").write_text('bad')\n")
            for name in ("gate.py", "installation_evidence.py"):
                proc = subprocess.run([sys.executable, str(root / name)], capture_output=True, text=True)
                self.assertNotEqual(proc.returncode, 0)
                self.assertFalse(marker.exists(), name + " imported an untrusted local module")

    def check_entrypoints_ignore_cache(self, target):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            source = root / "source"
            tools = source / "tests/lobertest/enterprise"
            tools.mkdir(parents=True)
            current = Path(gate.__file__).parent
            for name in ("gate.py", "installation_evidence.py", "production_capacity.py", "profiles.json"):
                (tools / name).write_bytes((current / name).read_bytes())
            marker = root / "cache-executed"
            path = tools / target
            original = path.read_bytes()
            poison = ("from pathlib import Path\nPath(" + repr(str(marker)) + ").write_text('bad')\n").encode()
            self.assertLess(len(poison), len(original))
            path.write_bytes(poison + b" " * (len(original) - len(poison)))
            os.utime(path, (1700000000, 1700000000))
            py_compile.compile(str(path), doraise=True)
            path.write_bytes(original)
            os.utime(path, (1700000000, 1700000000))
            binary, archive = root / "binary", root / "archive"
            binary.write_bytes(b"binary")
            archive.write_bytes(b"archive")
            digest = lambda p: hashlib.sha256(p.read_bytes()).hexdigest()
            files = {str(p.relative_to(source)): digest(p) for p in tools.glob("*.py")}
            files[str((tools / "profiles.json").relative_to(source))] = digest(tools / "profiles.json")
            manifest = {"run_id": "test", "product_sha": "a" * 40, "test_sha": "b" * 40,
                        "binary": str(binary), "binary_sha256": digest(binary), "files": files,
                        "source_archive": str(archive), "source_archive_sha256": digest(archive)}
            manifest_path = root / "candidate.json"
            manifest_path.write_text(json.dumps(manifest))
            results = root / "results"
            results.mkdir()
            row = {"status": "PASS", "count": 1, "failed": 0, "skipped": 0,
                   "product_sha": manifest["product_sha"], "test_sha": manifest["test_sha"],
                   "binary_sha256": manifest["binary_sha256"], "run_id": "test",
                   "manifest_sha256": digest(manifest_path), "evidence": "dummy.log"}
            (results / "dummy.log").write_text("not a real installation")
            row["evidence_sha256"] = digest(results / "dummy.log")
            (results / "pxe_state.json").write_text(json.dumps(row))
            for script, extra in (("installation_evidence.py", ["--kind", "installation"]),
                                  ("gate.py", ["--profile", "release", "--results", str(results)])):
                command = [sys.executable, "-I", str(tools / script), "--manifest", str(manifest_path),
                           "--product-sha", manifest["product_sha"]] + extra
                if script == "installation_evidence.py": command += ["--output", str(root / "output")]
                subprocess.run(command, capture_output=True, text=True, check=False)
                self.assertFalse(marker.exists(), script + " executed unverified bytecode")
