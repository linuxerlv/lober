"""Run only the current-rewrite suite, never legacy pytest fixtures or filters."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import time
import unittest


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("suite", choices=("control", "gate"))
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--product-sha", required=True)
    parser.add_argument("--test-sha", required=True)
    args = parser.parse_args()
    for sha in (args.product_sha, args.test_sha):
        if not re.fullmatch(r"[0-9a-f]{40}", sha):
            parser.error("Candidate identities must be full frozen Git SHAs")
    args.output.mkdir(parents=True, exist_ok=True)
    suite = unittest.defaultTestLoader.discover(str(Path(__file__).parent), pattern=f"test_{args.suite}.py")
    started = time.monotonic()
    result = unittest.TextTestRunner(verbosity=2).run(suite)
    successful = result.wasSuccessful() and result.testsRun > 0 and not result.skipped
    report = {"status": "PASS" if successful else "FAIL", "count": result.testsRun,
              "failed": len(result.failures) + len(result.errors), "skipped": len(result.skipped),
              "duration_seconds": round(time.monotonic() - started, 3),
              "product_sha": args.product_sha, "test_sha": args.test_sha,
              "evidence": args.suite + ".log", "runtime": platform.platform(),
              "python": platform.python_version(),
              "failed_cases": [test.id() for test, _ in result.failures + result.errors]}
    if args.suite == "control" and os.environ.get("LOBER_ENTERPRISE_BINARY"):
        report["binary_sha256"] = hashlib.sha256(Path(os.environ["LOBER_ENTERPRISE_BINARY"]).read_bytes()).hexdigest()
    (args.output / (args.suite + ".json")).write_text(json.dumps(report, indent=2) + "\n")
    return 0 if successful else 1


if __name__ == "__main__":
    raise SystemExit(main())
