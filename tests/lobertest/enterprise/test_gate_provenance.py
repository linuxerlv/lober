import hashlib
import json
from pathlib import Path
import tempfile
import unittest

import gate


class ProvenanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.source = self.root / "source"
        self.source.mkdir()
        (self.source / "fixture.py").write_bytes(b"expected test source")
        self.binary = self.root / "binary"
        self.binary.write_bytes(b"expected binary")
        self.archive = self.root / "source.tar"
        self.archive.write_bytes(b"expected source archive")
        self.manifest = {"binary_sha256": self.digest(self.binary),
                         "files": {"fixture.py": self.digest(self.source / "fixture.py")},
                         "source_archive": str(self.archive),
                         "source_archive_sha256": self.digest(self.archive)}

    @staticmethod
    def digest(path):
        return hashlib.sha256(path.read_bytes()).hexdigest()

    def validate(self):
        return getattr(gate, "validate_candidate", lambda *_: [])(self.manifest, self.source, self.binary)

    def test_matching_artifacts_pass(self):
        self.assertEqual(self.validate(), [])

    def test_wrong_binary_is_rejected(self):
        self.binary.write_bytes(b"old binary")
        self.assertTrue(self.validate())

    def test_changed_test_source_is_rejected(self):
        (self.source / "fixture.py").write_bytes(b"unverified code")
        self.assertTrue(self.validate())

    def test_changed_source_archive_is_rejected(self):
        self.archive.write_bytes(b"different source archive")
        self.assertTrue(self.validate())

    def test_interrupted_rerun_invalidates_previous_pass(self):
        path = self.root / "control.json"
        path.write_text(json.dumps({"status": "PASS", "count": 1}))
        getattr(gate, "begin_run", lambda *_: None)(path, "run-new", "product", "tests")
        self.assertEqual(json.loads(path.read_text())["status"], "RUNNING")

    def test_changed_evidence_is_rejected(self):
        log = self.root / "control.log"
        log.write_bytes(b"completed output")
        row = {"evidence": "control.log", "evidence_sha256": self.digest(log)}
        log.write_bytes(b"interrupted rerun")
        validate = getattr(gate, "validate_evidence", lambda *_: [])
        self.assertTrue(validate(row, self.root))

    def test_evidence_outside_result_root_is_rejected(self):
        validate = getattr(gate, "validate_evidence", lambda *_: [])
        self.assertTrue(validate({"evidence": "../outside.log", "evidence_sha256": "a" * 64}, self.root))
