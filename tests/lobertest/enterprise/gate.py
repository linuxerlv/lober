"""Fail-closed gate for explicit suite profiles; uses only the standard library."""
import argparse
import hashlib
import json
from pathlib import Path
import re


def digest(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def atomic_json(path, value):
    path = Path(path)
    temporary = path.with_suffix(path.suffix + ".tmp")
    temporary.write_text(json.dumps(value, indent=2) + "\n")
    temporary.replace(path)


def begin_run(path, run_id, product_sha, test_sha):
    atomic_json(path, {"status": "RUNNING", "run_id": run_id,
                      "product_sha": product_sha, "test_sha": test_sha,
                      "count": 0, "failed": 0, "skipped": 0})


def validate_candidate(manifest, source_root, binary):
    problems = []
    root = Path(source_root).resolve()
    binary = Path(binary)
    if not binary.is_file() or digest(binary) != manifest.get("binary_sha256"):
        problems.append("Built binary does not match frozen manifest")
    archive = Path(manifest.get("source_archive", ""))
    if not archive.is_file() or digest(archive) != manifest.get("source_archive_sha256"):
        problems.append("Test source archive does not match frozen manifest")
    files = manifest.get("files")
    if not isinstance(files, dict) or not files:
        return problems + ["Frozen source file map is absent"]
    for name, expected in files.items():
        path = (root / name).resolve()
        if not path.is_relative_to(root) or not path.is_file() or digest(path) != expected:
            problems.append("Frozen source mismatch: " + name)
    return problems


def validate_evidence(row, root):
    name = row.get("evidence")
    if not isinstance(name, str) or not name:
        return ["Evidence path missing"]
    root = Path(root).resolve()
    path = (root / name).resolve()
    if not path.is_relative_to(root) or not path.is_file() or not path.stat().st_size:
        return ["Evidence absent, empty, or outside result root"]
    if digest(path) != row.get("evidence_sha256"):
        return ["Evidence digest mismatch (incomplete or replaced log)"]
    return []


def evaluate(required, results, product_sha, manifest=None):
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
        if manifest is not None:
            if row.get("run_id") != manifest.get("run_id"):
                problems.append(f"{name}: run identity mismatch")
            if row.get("manifest_sha256") != manifest.get("_sha256"):
                problems.append(f"{name}: build manifest mismatch")
            if name in ("control", "gate", "flows", "events", "recovery"):
                if row.get("test_sha") != manifest.get("test_sha"):
                    problems.append(f"{name}: test source identity mismatch")
                if row.get("binary_sha256") != manifest.get("binary_sha256"):
                    problems.append(f"{name}: executed binary mismatch")
    return problems


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--profile", choices=("control-smoke", "control-complete", "release"), required=True)
    parser.add_argument("--results", type=Path, required=True)
    parser.add_argument("--product-sha", required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.product_sha):
        parser.error("product-sha must be the full frozen Git SHA")
    required = json.loads(Path(__file__).with_name("profiles.json").read_text())[args.profile]
    results = {}
    root = args.results.resolve(strict=True)
    manifest = json.loads(args.manifest.read_text())
    manifest["_sha256"] = digest(args.manifest)
    for name in required:
        path = root / (name + ".json")
        if path.is_file():
            try:
                results[name] = json.loads(path.read_text())
            except (ValueError, OSError):
                pass
    problems = evaluate(required, results, args.product_sha, manifest)
    if manifest.get("product_sha") != args.product_sha or not manifest.get("run_id"):
        problems.append("Frozen product/run identity mismatch")
    problems += validate_candidate(manifest, Path(__file__).resolve().parents[3], manifest.get("binary", ""))
    for name, row in results.items():
        if not isinstance(row, dict) or not isinstance(row.get("evidence"), str):
            continue
        problems += [f"{name}: {problem}" for problem in validate_evidence(row, root)]
    print(json.dumps({"profile": args.profile, "product_sha": args.product_sha,
                      "status": "FAIL" if problems else "PASS", "problems": problems}, indent=2))
    return 1 if problems else 0


if __name__ == "__main__":
    raise SystemExit(main())
