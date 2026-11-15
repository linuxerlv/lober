"""核对固定起点、实际运行镜像及升级和恢复记录。"""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import tempfile


BASELINE_SHA = "a7655acef724a5e5a3830517bf5ee13060a285de"
BASELINE_IMAGE = "sha256:2576f3ff8cd97a1595d8aa269b9d29abd28e0fe77c0bc67c323be6bd640f22ff"
PROOF_PREFIX = "UPGRADE_PROOF "
LOG = "supported_upgrade.log"
WORKER = "supported_upgrade.worker.json"


def read_file(path):
    if path.is_symlink():
        raise ValueError(f"证据不能是符号链接：{path.name}")
    return path.read_bytes()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def validate(manifest, log, worker):
    image, commit = manifest["image_id"], manifest["product_sha"]
    if (not re.fullmatch(r"sha256:[0-9a-f]{64}", image) or image == BASELINE_IMAGE
            or not re.fullmatch(r"[0-9a-f]{40}", commit) or commit == BASELINE_SHA):
        raise ValueError("候选与升级起点必须是不同的固定版本")
    if (worker["status"] != "PASS" or worker["commit"] != commit or worker["image_id"] != image
            or worker["check_exit_code"] != 0 or worker["check_log_sha256"] != digest(log)):
        raise ValueError("执行记录与候选或检查日志不符")
    cmd = worker["check_command"]
    if (len(cmd) != 3 or Path(cmd[0]).name != "image-upgrade" or cmd[1] != image
            or not re.fullmatch(r"[0-9a-f]{32}", worker["run_id"]) or Path(cmd[2]).name != worker["run_id"]):
        raise ValueError("检查入口或执行编号不符")
    proofs = [json.loads(line[len(PROOF_PREFIX):]) for line in log.decode().splitlines()
              if line.startswith(PROOF_PREFIX)]
    if len(proofs) != 1:
        raise ValueError("必须有且只有一份升级原始记录")
    proof = proofs[0]
    probe_sha = proof["probe_sha256"]
    if (not re.fullmatch(r"[0-9a-f]{64}", probe_sha) or probe_sha != manifest["files"]["ops/ci/upgrade_probe.py"]
            or proof["baseline_sha"] != BASELINE_SHA or proof["rolled_back_status"] != 404
            or not re.fullmatch(r"[0-9a-f]{64}", proof["new_task_sha256"])):
        raise ValueError("升级起点、升级后新任务或恢复记录不符")
    stages = proof["stages"]
    if [(s["phase"], s["image_id"]) for s in stages] != [
            ("before", BASELINE_IMAGE), ("upgraded", image), ("restored", BASELINE_IMAGE)]:
        raise ValueError("三个阶段实际运行的镜像不符")
    cids = [s["container_id"] for s in stages]
    if len(set(cids)) != 3 or any(not re.fullmatch(r"[0-9a-f]{64}", cid) for cid in cids):
        raise ValueError("未记录三个独立的容器启动")
    for key in ("snapshot_sha256", "artifacts_sha256"):
        values = [s[key] for s in stages]
        if len(set(values)) != 1 or not re.fullmatch(r"[0-9a-f]{64}", values[0]):
            raise ValueError("升级或恢复改变了原数据或制品")
    counts = [s["artifact_count"] for s in stages]
    if any(type(count) is not int or count <= 0 for count in counts) or len(set(counts)) != 1:
        raise ValueError("原制品缺失或数量不符")


def verify_upgrade(root, manifest, row, image_root):
    try:
        log, worker = read_file(root / LOG), read_file(root / WORKER)
        if (row["evidence"] != LOG or row["evidence_sha256"] != digest(log)
                or row["worker_sha256"] != digest(worker) or row["image_id"] != manifest["image_id"]):
            raise ValueError("原始记录的校验值或镜像不符")
        original = json.loads(worker)
        run_id = manifest["container_upgrade_run"]
        if image_root is None or run_id != original["run_id"] or not re.fullmatch(r"[0-9a-f]{32}", run_id):
            raise ValueError("发布清单没有指定原始升级执行")
        original_run = image_root / "runs" / run_id
        if (original_run.is_symlink()
                or original_run.resolve() != image_root.resolve() / "runs" / run_id
                or read_file(original_run / "check.log") != log
                or read_file(original_run / "result.json") != worker):
            raise ValueError("汇总证据与执行器原件不符")
        validate(manifest, log, original)
        return []
    except (OSError, ValueError, KeyError, TypeError, AttributeError):
        return ["升级原始记录缺失、不完整或与固定候选不符"]


def main():
    os.umask(0o077)
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--run", type=Path, required=True)
    parser.add_argument("--results", type=Path, required=True)
    args = parser.parse_args()
    output = args.results.resolve(strict=True)
    result = output / "supported_upgrade.json"
    # 每次先撤销旧结论，导入完成后才写新的通过记录。
    result.unlink(missing_ok=True)
    try:
        manifest_data = read_file(args.manifest)
        manifest = json.loads(manifest_data)
        log, worker = read_file(args.run / "check.log"), read_file(args.run / "result.json")
        record = json.loads(worker)
        if record["run_id"] != args.run.resolve().name or record["run_id"] != manifest["container_upgrade_run"]:
            raise ValueError("执行编号与原件目录不符")
        validate(manifest, log, record)
        row = {key: manifest[key] for key in ("run_id", "product_sha", "test_sha", "binary_sha256", "image_id")}
        row.update(status="PASS", count=1, failed=0, skipped=0, manifest_sha256=digest(manifest_data),
                   evidence=LOG, evidence_sha256=digest(log), worker_sha256=digest(worker))
        for name, data in ((LOG, log), (WORKER, worker), (result.name, json.dumps(row).encode() + b"\n")):
            with tempfile.NamedTemporaryFile(dir=output, delete=False) as stream:
                temporary = Path(stream.name)
                try:
                    stream.write(data)
                    stream.close()
                    temporary.replace(output / name)
                finally:
                    temporary.unlink(missing_ok=True)
    except (OSError, ValueError, KeyError, TypeError, AttributeError) as error:
        parser.exit(1, f"升级证据导入失败：{error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
