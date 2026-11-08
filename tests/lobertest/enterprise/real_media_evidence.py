"""运行真实镜像读取检查，并生成绑定候选的证据；不启动设备或安装。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import subprocess

from gate import atomic_json, begin_run, digest, validate_candidate

APP = "github.com/lober/lober/internal/application/media"
ISO = "github.com/lober/lober/internal/infrastructure/media/iso9660"
REQUIRED = {(APP, "TestImportRealESXi/" + v) for v in ("7", "8", "9")} | {
    (APP, "TestIdentifyRealISO"), (ISO, "TestRealISO")}
MEDIA = ("LOBER_TEST_ESXI7_ISO", "LOBER_TEST_ESXI8_ISO", "LOBER_TEST_ESXI9_ISO", "LOBER_TEST_REAL_ISO")


def media_digest(path):
    checksum = hashlib.sha256()
    with path.open("rb") as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b""):
            checksum.update(block)
    return checksum.hexdigest()


def summarize(rows, exit_code):
    outcomes = {key: [] for key in REQUIRED}
    failures = 0
    for row in rows:
        action = row.get("Action")
        key = row.get("Package"), row.get("Test")
        if key in outcomes and action in ("pass", "fail", "skip"):
            outcomes[key].append(action)
        if action == "fail":
            failures += 1
    passed = sum(value == ["pass"] for value in outcomes.values())
    skipped = sum("skip" in value for value in outcomes.values())
    missing = len(REQUIRED) - passed
    return {"status": "PASS" if exit_code == 0 and not failures and not missing else "FAIL",
            "count": len(REQUIRED), "failed": failures + missing, "skipped": skipped}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--product-sha", required=True)
    parser.add_argument("--checksums", type=Path, required=True,
                        help="可信来源确认的镜像 SHA256 映射，键为四个环境变量名")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.product_sha):
        parser.error("product-sha must be a full Git SHA")
    args.output.mkdir(parents=True, exist_ok=True)
    result = args.output / "real_media.json"
    begin_run(result, "initializing", args.product_sha, "")
    manifest = json.loads(args.manifest.read_text())
    begin_run(result, manifest["run_id"], args.product_sha, manifest["test_sha"])
    root = Path(__file__).resolve().parents[3]
    problems = validate_candidate(manifest, root, manifest.get("binary", ""))
    for name in ("tests/lobertest/enterprise/real_media_evidence.py", "tests/lobertest/enterprise/gate.py"):
        if name not in manifest.get("files", {}):
            problems.append("Evidence tool missing from frozen manifest: " + name)
    if args.product_sha != manifest.get("product_sha"):
        problems.append("Candidate identity mismatch")
    expected = json.loads(args.checksums.read_text())
    actual = {}
    for key in MEDIA:
        path = Path(os.environ.get(key, ""))
        if not path.is_file() or not re.fullmatch(r"[0-9a-f]{64}", str(expected.get(key, ""))):
            problems.append(key + ": file or expected checksum missing")
            continue
        actual[key] = media_digest(path)
        if actual[key] != expected[key]:
            problems.append(key + ": checksum mismatch")
    command = ["go", "test", "-json", "-count=1", "./internal/application/media",
               "./internal/infrastructure/media/iso9660", "-run",
               "^(TestImportRealESXi|TestIdentifyRealISO|TestRealISO)$"]
    log = args.output / "real_media.log"
    rows = []
    rc = 1
    if not problems:
        proc = subprocess.run(command, cwd=root, stdout=subprocess.PIPE, stderr=subprocess.STDOUT,
                              text=True, encoding="utf-8", errors="replace", check=False)
        rc = proc.returncode
        metadata = {"product_sha": args.product_sha, "run_id": manifest["run_id"],
                    "manifest_sha256": digest(args.manifest), "media_sha256": actual,
                    "command": command, "exit_code": rc}
        log.write_text(json.dumps(metadata, ensure_ascii=False) + "\n" + proc.stdout, encoding="utf-8")
        for line in proc.stdout.splitlines():
            try:
                value = json.loads(line)
                if isinstance(value, dict):
                    rows.append(value)
                else:
                    problems.append("Malformed test event")
            except ValueError:
                problems.append("Malformed test event")
    else:
        log.write_text("Preflight failed: " + "; ".join(problems) + "\n", encoding="utf-8")
    summary = summarize(rows, rc)
    if problems:
        summary["status"] = "FAIL"
        summary["failed"] += len(problems)
    report = {**summary, "product_sha": args.product_sha, "test_sha": manifest["test_sha"],
              "run_id": manifest["run_id"], "manifest_sha256": digest(args.manifest),
              "binary_sha256": manifest["binary_sha256"], "media_sha256": actual,
              "evidence": log.name, "evidence_sha256": digest(log), "problems": problems,
              "scope": "镜像识别、读取和 ESXi 导入；不包含设备启动或安装"}
    atomic_json(result, report)
    print(json.dumps(report, ensure_ascii=False, indent=2))
    return 0 if report["status"] == "PASS" else 1


if __name__ == "__main__":
    raise SystemExit(main())
