"""The release gate must fail closed; no service or database is needed."""
import unittest

from gate import evaluate


class GateTests(unittest.TestCase):
    def result(self, **changes):
        value = {"status": "PASS", "count": 2, "failed": 0, "skipped": 0,
                 "product_sha": "abc", "evidence": "api.json"}
        value.update(changes)
        return value

    def test_complete_required_suite_passes(self):
        self.assertEqual(evaluate(["api"], {"api": self.result()}, "abc"), [])

    def test_missing_required_suite_fails(self):
        self.assertTrue(evaluate(["api"], {}, "abc"))

    def test_zero_cases_fails(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(count=0)}, "abc"))

    def test_skipped_required_cases_fail(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(skipped=1)}, "abc"))

    def test_failed_cases_override_pass_label(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(failed=1)}, "abc"))

    def test_wrong_product_identity_fails(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(product_sha="old")}, "abc"))

    def test_blocked_cannot_pass(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(status="BLOCKED")}, "abc"))

    def test_missing_evidence_fails(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(evidence="")}, "abc"))

    def test_empty_profile_fails(self):
        self.assertTrue(evaluate([], {}, "abc"))

    def test_non_numeric_count_fails_closed(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(count="2")}, "abc"))

    def test_negative_failure_count_fails_closed(self):
        self.assertTrue(evaluate(["api"], {"api": self.result(failed=-1)}, "abc"))

    def test_unselected_blocked_suite_does_not_hide_selected_success(self):
        self.assertEqual(evaluate(["api"], {"api": self.result(), "install": self.result(status="BLOCKED")}, "abc"), [])


if __name__ == "__main__":
    unittest.main()
