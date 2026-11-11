"""在指定阶段的测试账号下核对候选和发布证据。"""

import argparse
import hashlib
import json
from pathlib import Path
import re
import subprocess
import sys


GATE = Path(__file__).resolve().parents[2] / "tests/lobertest/enterprise/gate.py"
PROFILES = {"dev": "control-complete", "test": "release"}


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify(stage, sha, root):
    if stage not in PROFILES or not re.fullmatch(r"[0-9a-f]{40}", sha):
        return False

    run = root.resolve() / sha
    source = run / "source"
    logs = run / "logs"
    manifest = run / "candidate.json"
    binary = run / "lober"
    qualification = run / "qualification"
    execution = run / "execution.json"
    if any(path.is_symlink() for path in (run, source, logs, manifest, binary, qualification, execution)):
        return False
    try:
        candidate = json.loads(manifest.read_text(encoding="utf-8"))
        if (candidate["product_sha"] != sha or candidate["test_sha"] != sha
                or candidate["binary"] != str(binary)
                or candidate["binary_sha256"] != digest(binary)):
            return False

        manifest_sha = digest(manifest)
        completed = json.loads(execution.read_text(encoding="utf-8"))
        accepted = ("PASS", "BLOCKED") if stage == "test" else ("PASS",)
        if (completed["status"] not in accepted or completed["branch"] != stage
                or completed["sha"] != sha or completed["run_id"] != candidate["run_id"]
                or completed["binary_sha256"] != candidate["binary_sha256"]
                or completed["manifest_sha256"] != manifest_sha):
            return False

        if stage == "dev":
            marker = run / "dev-ready.json"
            if marker.is_symlink():
                return False
            ready = json.loads(marker.read_text(encoding="utf-8"))
            if (ready["sha"] != sha or ready["binary_sha256"] != candidate["binary_sha256"]
                    or ready["manifest_sha256"] != manifest_sha):
                return False

        result = subprocess.run([
            sys.executable, "-I", str(GATE), "--profile", PROFILES[stage],
            "--source-root", str(source), "--results", str(logs),
            "--product-sha", sha, "--manifest", str(manifest),
            "--qualification-root", str(qualification),
        ], capture_output=True, check=False, timeout=100)
        if result.returncode:
            return False
        report = json.loads(result.stdout)
        return report == {"profile": PROFILES[stage], "product_sha": sha,
                          "status": "PASS", "problems": []}
    except (OSError, ValueError, TypeError, KeyError, subprocess.TimeoutExpired):
        return False


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", choices=PROFILES)
    parser.add_argument("sha")
    parser.add_argument("--root", type=Path)
    args = parser.parse_args()
    root = args.root or Path(f"/var/lib/lober-ci-{args.stage}")
    import fcntl

    try:
        with (root / ".worker.lock").open("r") as lock:
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
            return 0 if verify(args.stage, args.sha, root) else 1
    except OSError:
        return 1


if __name__ == "__main__":
    sys.exit(main())
