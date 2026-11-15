import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from upgrade_evidence import BASELINE_IMAGE, BASELINE_SHA, verify_upgrade


class UpgradeEvidenceTests(unittest.TestCase):
    def test_bound_proof_rejects_wrong_origin_image_and_restore(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            image = "sha256:" + "9" * 64
            manifest = {"image_id": image, "product_sha": "a" * 40, "test_sha": "b" * 40}
            run_id = "c" * 32
            proof = {"baseline_sha": BASELINE_SHA, "new_task_sha256": "4" * 64,
                     "rolled_back_status": 404, "stages": [
                         {"phase": phase, "image_id": running, "container_id": key * 64,
                          "snapshot_sha256": "5" * 64, "artifacts_sha256": "6" * 64, "artifact_count": 2}
                         for phase, running, key in (("before", BASELINE_IMAGE, "1"),
                             ("upgraded", image, "2"), ("restored", BASELINE_IMAGE, "3"))]}
            log = root / "supported_upgrade.log"
            worker = root / "supported_upgrade.worker.json"
            digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()

            def row_for(value):
                log.write_text("UPGRADE_PROOF " + json.dumps(value) + "\n", encoding="utf-8")
                worker.write_text(json.dumps({"status": "PASS", "run_id": run_id,
                    "commit": manifest["product_sha"], "image_id": image, "check_exit_code": 0,
                    "check_log_sha256": digest(log),
                    "check_command": ["/opt/lober/image-upgrade", image, "/runs/" + run_id]}))
                return {"image_id": image, "evidence": log.name, "evidence_sha256": digest(log),
                        "worker_sha256": digest(worker)}

            self.assertEqual(verify_upgrade(root, manifest, row_for(proof)), [])
            for modified in (
                    {**proof, "baseline_sha": "f" * 40},
                    {**proof, "rolled_back_status": 200},
                    {**proof, "stages": [proof["stages"][0], {**proof["stages"][1], "image_id": BASELINE_IMAGE}, proof["stages"][2]]},
                    {**proof, "stages": [*proof["stages"][:2], {**proof["stages"][2], "snapshot_sha256": "0" * 64}]}):
                with self.subTest(proof=modified):
                    self.assertTrue(verify_upgrade(root, manifest, row_for(modified)))
            row = row_for(proof)
            worker.unlink()
            self.assertTrue(verify_upgrade(root, manifest, row))
