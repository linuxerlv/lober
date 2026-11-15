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
IMAGE_RUNTIME_SUITES = ("control", "flows", "events", "recovery", "real_media",
                        "pxe_state", "real_install", "capacity_soak", "supported_upgrade")


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def verify_image(root, sha, control_id, debian_id):
    """返回两项检查实际使用的镜像。缺少证据时拒绝晋级。"""
    if not re.fullmatch(r"[0-9a-f]{40}", sha) or control_id == debian_id:
        raise ValueError("候选提交或运行编号无效")
    records = []
    for suite, run_id in (("control", control_id), ("debian", debian_id)):
        if not re.fullmatch(r"[0-9a-f]{32}", run_id):
            raise ValueError("运行编号无效")
        run = root / "runs" / run_id
        report = json.loads((run / "result.json").read_text())
        if (report["status"] != "PASS" or report["run_id"] != run_id or report["commit"] != sha
                or report["check_exit_code"] != 0
                or Path(report["check_command"][0]).name != f"image-{suite}"
                or report["check_log_sha256"] != digest(run / "check.log")):
            raise ValueError(f"{suite} 检查结果与指定候选不匹配")
        records.append((run, report))
    (control, cr), (debian, dr) = records
    image = cr["image_id"]
    if image != dr["image_id"] or not re.fullmatch(r"sha256:[0-9a-f]{64}", image):
        raise ValueError("两项检查使用的镜像不同")
    result = json.loads((control / "control.json").read_text())
    if (result["status"] != "PASS" or result["count"] <= 0 or result["failures"] or result["skipped"]
            or result["runtime_images"] != [image]):
        raise ValueError("control 未完整通过")
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


def require_release_image(image_root, sha, manifest, logs):
    image = verify_image(image_root, sha, manifest["container_control_run"], manifest["container_debian_run"])
    if manifest["image_id"] != image:
        raise ValueError("发布候选与已验证镜像不符")
    for suite in IMAGE_RUNTIME_SUITES:
        path = logs / (suite + ".json")
        if not path.is_file() or path.is_symlink():
            raise ValueError(f"{suite}: 缺少对应镜像的验收记录")
        if json.loads(path.read_text())["image_id"] != image:
            raise ValueError(f"{suite}: 验收镜像与候选不符")


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
        if stage == "test" and image_root is not None:
            require_release_image(image_root, sha, candidate, logs)
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
