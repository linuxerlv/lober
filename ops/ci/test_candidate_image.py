import subprocess
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

import worker


class CandidateImageTests(unittest.TestCase):
    def test_retries_and_promotions_share_one_image_but_keep_each_result(self):
        with tempfile.TemporaryDirectory() as tmp:
            root = Path(tmp)
            repo = root / "repo"
            repo.mkdir()

            def git(*args):
                return subprocess.check_output(["git", "-C", str(repo), *args], text=True).strip()

            git("init", "-q")
            git("config", "user.email", "ci@example.invalid")
            git("config", "user.name", "CI")
            (repo / "main.go").write_text("package main\n")
            git("add", ".")
            git("commit", "-qm", "first")
            first = git("rev-parse", "HEAD")
            git("commit", "--allow-empty", "-qm", "promote")
            promoted = git("rev-parse", "HEAD")
            (repo / "main.go").write_text("package main\nfunc main() {}\n")
            git("commit", "-qam", "Go change")
            changed = git("rev-parse", "HEAD")
            store = root / "store"
            store.mkdir()
            built = {}
            checks = []

            def engine(args, **kwargs):
                if args[:3] == ["docker", "image", "inspect"]:
                    reference = args[-1]
                    image = built.get(reference, reference if reference in built.values() else "")
                    return subprocess.CompletedProcess(args, 0 if image else 1, image)
                if args[:3] == ["docker", "buildx", "build"]:
                    tag = args[args.index("--tag") + 1]
                    self.assertNotIn(tag, built)
                    built[tag] = "sha256:" + str(len(built) + 1) * 64
                elif args[:2] == ["docker", "run"]:
                    checks.append(args[-2])
                else:
                    return actual_run(args, **kwargs)
                return subprocess.CompletedProcess(args, 0, "")

            actual_run = subprocess.run
            with patch.object(worker.subprocess, "run", side_effect=engine):
                for commit, source in ((first, None), (first, None), (promoted, first), (changed, None)):
                    self.assertEqual(worker.image_run(store, repo, commit, source), 0)
                self.assertEqual(worker.image_run(store, repo, changed, first), 1)

            self.assertEqual(len(built), 2)
            self.assertEqual(checks[:3], [checks[0]] * 3)
            self.assertNotEqual(checks[0], checks[3])
            results = [worker.read_json(path) for path in store.glob("runs/*/result.json")]
            self.assertCountEqual([row["status"] for row in results], ["PASS"] * 4 + ["FAIL"])
            self.assertEqual(len({row["run_id"] for row in results}), 5)
