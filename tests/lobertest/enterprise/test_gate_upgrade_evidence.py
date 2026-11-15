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
            manifest = {"image_id": image, "product_sha": "a" * 40, "test_sha": "b" * 40,
                        "files": {"ops/ci/upgrade_probe.py": "7" * 64}}
            run_id = "c" * 32
            manifest["container_upgrade_run"] = run_id
            image_root = root / "store"
            original = image_root / "runs" / run_id
            original.mkdir(parents=True)
            proof = {"baseline_sha": BASELINE_SHA, "probe_sha256": "7" * 64, "new_task_sha256": "4" * 64,
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
                (original / "check.log").write_bytes(log.read_bytes())
                (original / "result.json").write_bytes(worker.read_bytes())
                return {"image_id": image, "evidence": log.name, "evidence_sha256": digest(log),
                        "worker_sha256": digest(worker)}

            self.assertEqual(verify_upgrade(root, manifest, row_for(proof), image_root), [])
            for modified in (
                    {**proof, "baseline_sha": "f" * 40},
                    {**proof, "probe_sha256": "f" * 64},
                    {**proof, "rolled_back_status": 200},
                    {**proof, "stages": [proof["stages"][0], {**proof["stages"][1], "image_id": BASELINE_IMAGE}, proof["stages"][2]]},
                    {**proof, "stages": [*proof["stages"][:2], {**proof["stages"][2], "snapshot_sha256": "0" * 64}]}):
                with self.subTest(proof=modified):
                    self.assertTrue(verify_upgrade(root, manifest, row_for(modified), image_root))
            row = row_for(proof)
            self.assertTrue(verify_upgrade(root, manifest, row, None))
            log.write_bytes(log.read_bytes() + b"replaced")
            worker_record = json.loads(worker.read_text())
            worker_record["check_log_sha256"] = digest(log)
            worker.write_text(json.dumps(worker_record))
            rewritten = {**row, "evidence_sha256": digest(log), "worker_sha256": digest(worker)}
            self.assertTrue(verify_upgrade(root, manifest, rewritten, image_root))
            row = row_for(proof)
            (original / "result.json").unlink()
            self.assertTrue(verify_upgrade(root, manifest, row, image_root))
            worker.unlink()
            self.assertTrue(verify_upgrade(root, manifest, row, image_root))
