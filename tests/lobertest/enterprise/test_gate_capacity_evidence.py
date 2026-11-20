"""合成资料只验证长稳记录的检查器，不是实际长稳或安装。"""
import tempfile
import json
from pathlib import Path
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import production_capacity as capacity
import installation_evidence as evidence


class CapacityEvidenceTests(unittest.TestCase):
    def test_missing_phases_and_real_install_are_blocked_without_writes(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            context = SimpleNamespace(output_root=root, identity={}, profile={}, c={}, common_identity={})
            problems, results, digests = capacity.assess_capacity(context)
            self.assertTrue(any("soak: result absent" in item for item in problems))
            self.assertTrue(any("real concurrent installation" in item for item in problems))
            self.assertEqual(results, {})
            self.assertEqual(digests, {})
            self.assertEqual(list(root.iterdir()), [])

    def test_capacity_adapter_refuses_empty_stage_records(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            profile = root / "profile.json"
            profile.write_text(json.dumps(capacity.FORMAL_PROFILE))
            profile.chmod(0o600)
            common = root / "identity.json"
            common.write_text("{}")
            common.chmod(0o600)
            manifest_file = root / "candidate.json"
            manifest_file.write_text("{}")
            manifest = {"run_id": "run", "product_sha": "a"*40, "test_sha": "b"*40,
                        "binary_sha256": "c"*64, "files": {"tests/lobertest/enterprise/production_capacity.py": "d"*64},
                        "capacity": {"profile_file": str(profile), "profile_sha256": evidence.digest(profile),
                                     "evidence_root": str(root), "run_identity_file": str(common),
                                     "run_identity_sha256": evidence.digest(common)}}
            with patch.object(evidence, "verify_installation", return_value=[]):
                problems = evidence.verify_capacity(manifest, root, root, manifest_file)
            self.assertIn("soak: result absent", problems)
            self.assertIn("real concurrent installation evidence absent", problems)
