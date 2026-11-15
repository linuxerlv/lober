import os
import subprocess
import tarfile
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import container_candidate


class ContainerCandidateTests(unittest.TestCase):
    def test_freeze_copies_program_from_image_and_preserves_source(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            commit, tree, cid = "a" * 40, "b" * 40, "c" * 64
            image = "sha256:" + "d" * 64
            store = root / "images"
            store.mkdir()
            container_candidate.write_json(store / (tree + ".json"), dict(tree=tree, image_id=image))
            fixture = root / "fixture.txt"
            fixture.write_text("fixed source")

            def run(argv, **kwargs):
                if argv[0] == "git":
                    with tarfile.open(argv[-2], "w") as archive:
                        archive.add(fixture, arcname=fixture.name)
                elif argv[:2] == ["docker", "cp"]:
                    self.assertEqual(argv[2], cid + ":/usr/local/bin/lober")
                    argv[3].write_bytes(b"the image's program")
                else:
                    self.assertEqual(argv, ["docker", "rm", "-v", cid])
                return subprocess.CompletedProcess(argv, 0)

            with (patch.object(container_candidate, "current_head", return_value=commit),
                  patch.object(container_candidate, "source_tree", return_value=tree),
                  patch.object(container_candidate, "image_id", return_value=image),
                  patch.object(container_candidate.subprocess, "check_output", return_value=cid) as docker,
                  patch.object(container_candidate.subprocess, "run", side_effect=run)):
                container_candidate.freeze(root, root, root, commit, os.getgid())
            docker.assert_called_once_with(["docker", "create", "--network", "none", image],
                                           text=True, timeout=30)
            candidate = root / commit
            manifest = container_candidate.read_json(candidate / "candidate.json")
            self.assertEqual(manifest["image_id"], image)
            self.assertEqual(manifest["binary_sha256"], container_candidate.digest(candidate / "lober"))
            self.assertEqual((candidate / "source" / fixture.name).read_text(), "fixed source")

    def test_freeze_requires_current_test_head_and_registered_image(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            commit = "a" * 40
            with patch.object(container_candidate, "current_head", return_value="b" * 40):
                with self.assertRaisesRegex(ValueError, "test"):
                    container_candidate.freeze(root, root, root, commit, 0)
            self.assertFalse((root / commit).exists())
            with (patch.object(container_candidate, "current_head", return_value=commit),
                  patch.object(container_candidate, "source_tree", return_value="c" * 40)):
                with self.assertRaises(FileNotFoundError):
                    container_candidate.freeze(root, root, root, commit, 0)
            self.assertFalse((root / commit).exists())

    def test_binding_rejects_another_image_without_changing_manifest(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            commit = "a" * 40
            run = root / commit
            run.mkdir()
            manifest = run / "candidate.json"
            original = dict(product_sha=commit, test_sha=commit, image_id="sha256:" + "b" * 64)
            container_candidate.write_json(manifest, original)
            before = manifest.read_bytes()
            ids = {key: f"{index:032x}" for index, key in enumerate(container_candidate.CHECKS, 1)}
            with patch.object(container_candidate, "verify_suite_image", return_value="sha256:" + "d" * 64):
                with self.assertRaisesRegex(ValueError, "镜像"):
                    container_candidate.bind(root, root, commit, ids)
            self.assertEqual(manifest.read_bytes(), before)

    def test_selection_binds_all_suites_including_batch_and_upgrade(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            commit = "a" * 40
            run = root / commit
            run.mkdir()
            manifest = run / "candidate.json"
            image = "sha256:" + "b" * 64
            container_candidate.write_json(manifest, dict(product_sha=commit, test_sha=commit, image_id=image))
            ids = {key: f"{index:032x}" for index, key in enumerate(container_candidate.CHECKS, 1)}
            with (patch.object(container_candidate, "verify_suite_image", return_value=image),
                  patch.object(container_candidate, "require_batch_image") as batch,
                  patch.object(container_candidate, "verified_worker", return_value=(run, {"image_id": image}))):
                container_candidate.bind(root, root, commit, ids)
            for key, value in ids.items():
                self.assertEqual(container_candidate.read_json(manifest)[f"container_{key}_run"], value)
            batch.assert_called_once()

    def test_finish_keeps_exact_bytes_and_rejects_replaced_receipt(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            commit, capacity_id = "a" * 40, "kvm-capacity.Aa12Bb34"
            run = root / commit
            qualification = run / "qualification" / capacity_id
            qualification.mkdir(parents=True)
            manifest = run / "candidate.json"
            initial = dict(product_sha=commit, test_sha=commit, image_id="sha256:" + "b" * 64)
            container_candidate.write_json(manifest, initial)
            before = manifest.read_bytes()
            capacity_manifest = qualification / "capacity-candidate.json"
            container_candidate.write_json(capacity_manifest, {
                **initial, "container_capacity_run": capacity_id,
                "capacity": {"evidence_root": str(qualification / "logs")},
            })
            receipts = root / "capacity"
            receipts.mkdir()
            receipt = receipts / (capacity_id + ".json")
            container_candidate.write_json(receipt, {"manifest_sha256": "c" * 64})
            with patch.object(container_candidate, "require_capacity_image"):
                with self.assertRaisesRegex(ValueError, "校验"):
                    container_candidate.finish(root, root, commit, capacity_id)
                self.assertEqual(manifest.read_bytes(), before)
                checksum = container_candidate.digest(capacity_manifest)
                container_candidate.write_json(receipt, {"manifest_sha256": checksum})
                container_candidate.finish(root, root, commit, capacity_id)
            self.assertEqual(manifest.read_bytes(), capacity_manifest.read_bytes())
