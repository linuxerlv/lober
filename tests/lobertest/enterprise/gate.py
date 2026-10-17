"""Fail-closed gate for explicit suite profiles; uses only the standard library."""
import argparse
import json
from pathlib import Path
import re


def evaluate(required, results, product_sha):
    problems = []
    if not required or len(set(required)) != len(required) or not product_sha:
        return ["Invalid or empty required-suite profile"]
    for name in required:
        row = results.get(name)
        if not isinstance(row, dict):
            problems.append(f"{name}: required result missing")
            continue
        if row.get("status") != "PASS":
            problems.append(f"{name}: {row.get('status', 'missing status')}")
        for key in ("count", "failed", "skipped"):
            value = row.get(key)
            if type(value) is not int or value < (1 if key == "count" else 0):
                problems.append(f"{name}: invalid {key}")
        if row.get("failed") != 0 or row.get("skipped") != 0:
            problems.append(f"{name}: failed or skipped required cases")
        if row.get("product_sha") != product_sha:
            problems.append(f"{name}: candidate identity mismatch")
        if not isinstance(row.get("evidence"), str) or not row["evidence"]:
            problems.append(f"{name}: evidence missing")
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=("control-smoke", "release"), required=True)
    parser.add_argument("--results", type=Path, required=True)
    parser.add_argument("--product-sha", required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.product_sha):
        parser.error("product-sha must be the full frozen Git SHA")
    required = json.loads(Path(__file__).with_name("profiles.json").read_text())[args.profile]
    results = {}
    root = args.results.resolve(strict=True)
    for name in required:
        path = root / (name + ".json")
        if path.is_file():
            try:
                results[name] = json.loads(path.read_text())
            except (ValueError, OSError):
                pass
    problems = evaluate(required, results, args.product_sha)
    for name, row in results.items():
        if not isinstance(row, dict) or not isinstance(row.get("evidence"), str):
            continue
        path = (root / row["evidence"]).resolve()
        if not path.is_relative_to(root) or not path.is_file():
            problems.append(f"{name}: evidence file absent or outside result directory")
    print(json.dumps({"profile": args.profile, "product_sha": args.product_sha,
                      "status": "FAIL" if problems else "PASS", "problems": problems}, indent=2))
    return 1 if problems else 0


if __name__ == "__main__":
    raise SystemExit(main())
