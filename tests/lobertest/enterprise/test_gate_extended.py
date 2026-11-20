"""New control suites must be mandatory and bound to executed test artifacts."""
import json
from pathlib import Path
import unittest

from gate import evaluate


class ExtendedGateTests(unittest.TestCase):
    names = ("flows", "events", "recovery")

    def setUp(self):
        self.manifest = {"run_id": "fresh-run", "product_sha": "product",
                         "test_sha": "current-tests", "binary_sha256": "current-binary",
                         "_sha256": "manifest-digest"}
        self.row = {"status": "PASS", "count": 1, "failed": 0, "skipped": 0,
                    "product_sha": "product", "test_sha": "current-tests",
                    "binary_sha256": "current-binary", "run_id": "fresh-run",
                    "manifest_sha256": "manifest-digest", "evidence": "completed.log"}

    def test_extended_suite_cannot_reuse_different_test_source(self):
        for name in self.names:
            with self.subTest(suite=name):
                row = {**self.row, "test_sha": "old-tests"}
                self.assertTrue(evaluate([name], {name: row}, "product", self.manifest))

    def test_extended_suite_cannot_reuse_different_binary(self):
        for name in self.names:
            with self.subTest(suite=name):
                row = {**self.row, "binary_sha256": "old-binary"}
                self.assertTrue(evaluate([name], {name: row}, "product", self.manifest))

    def test_extended_matching_suite_can_pass(self):
        for name in self.names:
            with self.subTest(suite=name):
                self.assertEqual(evaluate([name], {name: self.row}, "product", self.manifest), [])

    def test_complete_control_and_release_require_all_extended_suites(self):
        profiles = json.loads(Path(__file__).with_name("profiles.json").read_text())
        for profile in ("control-complete", "release"):
            with self.subTest(profile=profile):
                self.assertTrue(set(self.names).issubset(profiles.get(profile, [])))
