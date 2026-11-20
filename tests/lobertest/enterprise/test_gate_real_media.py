import unittest

from real_media_evidence import summarize
from gate import evaluate


class RealMediaEvidenceTests(unittest.TestCase):
    def rows(self):
        return [
            {"Action": "pass", "Package": package, "Test": test}
            for package, test in (
                ("github.com/lober/lober/internal/application/media", "TestImportRealESXi/7"),
                ("github.com/lober/lober/internal/application/media", "TestImportRealESXi/8"),
                ("github.com/lober/lober/internal/application/media", "TestImportRealESXi/9"),
                ("github.com/lober/lober/internal/application/media", "TestIdentifyRealISO"),
                ("github.com/lober/lober/internal/infrastructure/media/iso9660", "TestRealISO"),
            )
        ]

    def test_all_required_real_media_cases_pass(self):
        self.assertEqual(summarize(self.rows(), 0)["status"], "PASS")

    def test_missing_case_fails(self):
        self.assertEqual(summarize(self.rows()[:-1], 0)["status"], "FAIL")

    def test_skipped_case_fails(self):
        rows = self.rows()
        rows[0]["Action"] = "skip"
        self.assertEqual(summarize(rows, 0)["status"], "FAIL")

    def test_process_or_package_failure_fails(self):
        self.assertEqual(summarize(self.rows(), 1)["status"], "FAIL")
        self.assertEqual(summarize(self.rows() + [{"Action": "fail", "Package": "p"}], 0)["status"], "FAIL")

    def test_duplicate_case_cannot_mask_failure(self):
        rows = self.rows()
        rows.append({**rows[0], "Action": "fail"})
        self.assertEqual(summarize(rows, 0)["status"], "FAIL")

    def test_gate_rejects_wrong_media_test_source_or_binary(self):
        manifest = {"run_id": "run", "_sha256": "manifest", "test_sha": "tests", "binary_sha256": "binary"}
        row = {"status": "PASS", "count": 5, "failed": 0, "skipped": 0,
               "product_sha": "product", "evidence": "real_media.log", "run_id": "run",
               "manifest_sha256": "manifest", "test_sha": "tests", "binary_sha256": "binary"}
        self.assertEqual(evaluate(["real_media"], {"real_media": row}, "product", manifest), [])
        for field in ("test_sha", "binary_sha256"):
            self.assertTrue(evaluate(["real_media"], {"real_media": {**row, field: "old"}}, "product", manifest))
