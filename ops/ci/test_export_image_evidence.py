from pathlib import Path
import runpy
import unittest


selected_files = runpy.run_path(str(Path(__file__).with_name("export-image-evidence.py")))["selected_files"]


class ExportEvidenceTests(unittest.TestCase):
    def test_exports_only_the_selected_worker_records_and_summaries(self):
        suites = ("control", "flows", "events", "recovery", "upgrade", "kvm_batch")
        manifest = {f"container_{name}_run": str(n) * 32 for n, name in enumerate(suites, 1)}
        manifest["container_capacity_run"] = "kvm-capacity.ABCDEFGH"
        files = set(selected_files(manifest))
        self.assertEqual(len(files), 19)
        self.assertIn(Path("capacity/kvm-capacity.ABCDEFGH.json"), files)
        self.assertIn(Path("runs", "6" * 32, "kvm/shared-service-start.json"), files)
        self.assertNotIn(Path("runs", "6" * 32, "kvm/boot.pcap"), files)
        manifest["container_upgrade_run"] = "../../" + "3" * 32
        with self.assertRaises(ValueError):
            selected_files(manifest)
