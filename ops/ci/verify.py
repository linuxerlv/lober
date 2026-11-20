"""在指定阶段的测试账号下核对候选和发布证据。"""

import argparse
import hashlib
import json
import math
from pathlib import Path
import re
import subprocess
import sys


GATE = Path(__file__).resolve().parents[2] / "tests/lobertest/enterprise/gate.py"
PROFILES = {"dev": "control-complete", "test": "release"}
IMAGE_RUNTIME_SUITES = ("control", "flows", "events", "recovery",
                        "pxe_state", "real_install", "capacity_soak", "supported_upgrade")


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verified_worker(root, sha, run_id, suite):
    if not re.fullmatch(r"[0-9a-f]{32}", run_id):
        raise ValueError("运行编号无效")
    run = root / "runs" / run_id
    if run.resolve() != root.resolve() / "runs" / run_id or run.is_symlink():
        raise ValueError("运行目录无效")
    report = json.loads((run / "result.json").read_text())
    if (report["status"] != "PASS" or report["run_id"] != run_id or report["commit"] != sha
            or report["check_exit_code"] != 0
            or Path(report["check_command"][0]).name != f"image-{suite}"
            or report["check_log_sha256"] != digest(run / "check.log")):
        raise ValueError(f"{suite} 检查结果与指定候选不匹配")
    return run, report


def verify_control_image(root, sha, run_id):
    return verify_suite_image(root, sha, run_id, "control")


def verify_suite_image(root, sha, run_id, suite):
    control, worker = verified_worker(root, sha, run_id, suite)
    image = worker["image_id"]
    result = json.loads((control / (suite + ".json")).read_text())
    if (not re.fullmatch(r"sha256:[0-9a-f]{64}", image)
            or result["status"] != "PASS" or result["count"] <= 0 or result["failures"] or result["skipped"]
            or result["runtime_images"] != [image]):
        raise ValueError(f"{suite} 未完整通过")
    return image


def verify_image(root, sha, control_id, debian_id):
    """返回两项检查实际使用的镜像。缺少证据时拒绝晋级。"""
    if not re.fullmatch(r"[0-9a-f]{40}", sha) or control_id == debian_id:
        raise ValueError("候选提交或运行编号无效")
    image = verify_control_image(root, sha, control_id)
    debian, dr = verified_worker(root, sha, debian_id, "debian")
    if image != dr["image_id"]:
        raise ValueError("两项检查使用的镜像不同")
    kvm = debian / "kvm"
    first = json.loads((kvm / "terminal.json").read_text())
    repeat = json.loads((kvm / "repeat-terminal.json").read_text())
    guest = json.loads((kvm / "guest-identity.json").read_text())
    if ((kvm / "runtime-image.txt").read_text().strip() != image or first["state"] != "done"
            or first != repeat or (kvm / "repeat.pcap").stat().st_size <= 24
            or not {"machine.installing", "machine.init_start", "machine.ready"}
                <= {event["kind"] for event in first["events"]}
            or guest["first"]["boot"] == guest["repeat"]["boot"]
            or any(guest["first"][key] != guest["repeat"][key] for key in ("machine", "root", "id", "version"))
            or guest["repeat"]["id"] != "debian" or guest["repeat"]["version"] != "13"):
        raise ValueError("Debian 装机或重复引导证据不完整")
    return image


def require_capacity_image(image_root, sha, manifest, manifest_sha=None):
    run_id = manifest["container_capacity_run"]
    if not re.fullmatch(r"kvm-capacity\.[A-Za-z0-9]{8}", run_id):
        raise ValueError("容量运行编号无效")
    path = image_root / "capacity" / (run_id + ".json")
    if path.resolve() != image_root.resolve() / "capacity" / path.name or path.is_symlink():
        raise ValueError("容量原始记录路径无效")
    proof = json.loads(path.read_text())
    before = proof["before"]
    duration = proof["soak_seconds"]
    observed = proof["observed_seconds"]
    if (proof["status"] != "PASS" or proof["commit"] != sha or proof["run_id"] != manifest["run_id"]
            or before != proof["after"] or before["image_id"] != manifest["image_id"]
            or not re.fullmatch(r"[0-9a-f]{64}", before["container_id"])
            or type(before["pid"]) is not int or before["pid"] <= 1
            or type(before["start_ticks"]) is not int or before["start_ticks"] <= 0
            or type(duration) not in (int, float) or not math.isfinite(duration) or duration < 86400 or
            type(observed) not in (int, float) or not math.isfinite(observed) or observed < 86400 or
            (manifest_sha is not None and proof["manifest_sha256"] != manifest_sha)):
        raise ValueError("容量原始记录与候选镜像或 24 小时要求不符")
    if manifest_sha is not None:
        record_root = Path(manifest["capacity"]["evidence_root"])
        phase_records = {}
        for phase in ("seed", "normal", "burst", "soak", "evaluate"):
            record = record_root / phase / "result.json"
            if record.resolve(strict=True) != record:
                raise ValueError("容量阶段原件路径不符")
            phase_records[phase] = digest(record)
        if phase_records != proof["phase_records"]:
            raise ValueError("容量阶段原件已被更换")


def require_batch_image(image_root, sha, manifest):
    run_id = manifest["container_kvm_batch_run"]
    run, result = verified_worker(image_root, sha, run_id, "debian-batch")
    before = json.loads((run / "kvm" / "shared-service-start.json").read_text())
    after = json.loads((run / "kvm" / "shared-service-end.json").read_text())
    if (result["image_id"] != manifest["image_id"]
            or before != after or before["image_id"] != manifest["image_id"]
            or before["run_id"] != manifest["run_id"] or before["binary_sha256"] != manifest["binary_sha256"]):
        raise ValueError("四次装机的原件与候选镜像不符")


def require_release_image(image_root, sha, manifest, logs, manifest_sha=None):
    image = verify_control_image(image_root, sha, manifest["container_control_run"])
    if manifest["image_id"] != image:
        raise ValueError("发布候选与已验证镜像不符")
    for suite in IMAGE_RUNTIME_SUITES:
        path = logs / (suite + ".json")
        if not path.is_file() or path.is_symlink():
            raise ValueError(f"{suite}: 缺少对应镜像的验收记录")
        if json.loads(path.read_text())["image_id"] != image:
            raise ValueError(f"{suite}: 验收镜像与候选不符")
    require_batch_image(image_root, sha, manifest)
    require_capacity_image(image_root, sha, manifest, manifest_sha)
    for suite in ("flows", "events", "recovery"):
        if verify_suite_image(image_root, sha, manifest[f"container_{suite}_run"], suite) != image:
            raise ValueError(f"{suite}: 原件镜像与候选不符")


def verify(stage, sha, root, image_root=None):
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
        if stage == "test" and image_root is not None:
            require_release_image(image_root, sha, candidate, logs, manifest_sha)

        command = [
            sys.executable, "-I", str(GATE), "--profile", PROFILES[stage],
            "--source-root", str(source), "--results", str(logs),
            "--product-sha", sha, "--manifest", str(manifest),
            "--qualification-root", str(qualification),
        ]
        if stage == "test" and image_root is not None:
            command += ["--image-root", str(image_root)]
        result = subprocess.run(command, capture_output=True, check=False, timeout=100)
        if result.returncode:
            return False
        report = json.loads(result.stdout)
        return report == {"profile": PROFILES[stage], "product_sha": sha,
                          "status": "PASS", "problems": []}
    except (OSError, ValueError, TypeError, KeyError, IndexError, subprocess.TimeoutExpired):
        return False


def main():
    if sys.argv[1:2] == ["image"]:
        parser = argparse.ArgumentParser(description="核对候选镜像的控制面和装机证据")
        parser.add_argument("sha")
        parser.add_argument("--root", type=Path, required=True)
        parser.add_argument("--control-run", required=True)
        parser.add_argument("--debian-run", required=True)
        args = parser.parse_args(sys.argv[2:])
        try:
            print(verify_image(args.root, args.sha, args.control_run, args.debian_run))
            return 0
        except (OSError, ValueError, KeyError, TypeError, IndexError) as error:
            print(f"镜像晋级检查失败：{error}", file=sys.stderr)
            return 1
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("stage", choices=PROFILES)
    parser.add_argument("sha")
    parser.add_argument("--root", type=Path)
    parser.add_argument("--image-root", type=Path)
    args = parser.parse_args()
    root = args.root or Path(f"/var/lib/lober-ci-{args.stage}")
    import fcntl

    try:
        with (root / ".worker.lock").open("r") as lock:
            fcntl.flock(lock, fcntl.LOCK_SH | fcntl.LOCK_NB)
            return 0 if verify(args.stage, args.sha, root, args.image_root) else 1
    except OSError:
        return 1


if __name__ == "__main__":
    sys.exit(main())
