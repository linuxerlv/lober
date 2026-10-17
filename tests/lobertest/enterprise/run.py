"""Run only the current-rewrite suite, never legacy pytest fixtures or filters."""
import argparse
import json
import os
from pathlib import Path
import platform
import re
import sys
import time
import unittest

from gate import atomic_json, begin_run, digest, validate_candidate


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("suite", choices=("control", "gate"))
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--product-sha", required=True)
    parser.add_argument("--test-sha", required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    args = parser.parse_args()
    for sha in (args.product_sha, args.test_sha):
        if not re.fullmatch(r"[0-9a-f]{40}", sha):
            parser.error("Candidate identities must be full frozen Git SHAs")
    args.output.mkdir(parents=True, exist_ok=True)
    result_path = args.output / (args.suite + ".json")
    begin_run(result_path, "initializing", args.product_sha, args.test_sha)
    manifest = json.loads(args.manifest.read_text())
    begin_run(result_path, manifest["run_id"], args.product_sha, args.test_sha)
    binary = os.environ.get("LOBER_ENTERPRISE_BINARY", manifest["binary"])
    problems = validate_candidate(manifest, Path(__file__).resolve().parents[3], binary)
    if args.product_sha != manifest.get("product_sha") or args.test_sha != manifest.get("test_sha"):
        problems.append("Requested source identities do not match the frozen build manifest")
    common = {"run_id": manifest["run_id"], "manifest_sha256": digest(args.manifest),
              "product_sha": args.product_sha, "test_sha": args.test_sha,
              "binary_sha256": digest(binary) if Path(binary).is_file() else "",
              "evidence": args.suite + ".log"}
    if problems:
        print("Candidate preflight failed: " + "; ".join(problems), file=sys.stderr, flush=True)
        log = args.output / common["evidence"]
        atomic_json(result_path, {**common, "status": "FAIL", "count": 0, "failed": 1, "skipped": 0,
                                  "problems": problems, "evidence_sha256": digest(log) if log.is_file() else ""})
        return 1
    pattern = "test_gate*.py" if args.suite == "gate" else "test_control.py"
    suite = unittest.defaultTestLoader.discover(str(Path(__file__).parent), pattern=pattern)
    started = time.monotonic()
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    successful = result.wasSuccessful() and result.testsRun > 0 and not result.skipped
    report = {**common, "status": "PASS" if successful else "FAIL", "count": result.testsRun,
              "failed": len(result.failures) + len(result.errors), "skipped": len(result.skipped),
              "duration_seconds": round(time.monotonic() - started, 3),
              "product_sha": args.product_sha, "test_sha": args.test_sha,
              "evidence": args.suite + ".log", "runtime": platform.platform(),
              "python": platform.python_version(),
              "failed_cases": [test.id() for test, _ in result.failures + result.errors]}
    sys.stdout.flush()
    sys.stderr.flush()
    log = args.output / common["evidence"]
    if not log.is_file():
        report["status"] = "FAIL"
        successful = False
    report["evidence_sha256"] = digest(log) if log.is_file() else ""
    atomic_json(result_path, report)
    return 0 if successful else 1


if __name__ == "__main__":
    raise SystemExit(main())
