"""从冻结镜像和同一候选的测试原件组装发布清单。"""

import argparse
import fcntl
import grp
import hashlib
import json
import os
from pathlib import Path
import re
import stat
import subprocess
import sys
import tempfile
import uuid

sys.path.insert(0, str(Path(__file__).resolve().parent))
from verify import require_batch_image, require_capacity_image, verified_worker, verify_suite_image
from worker import build_environment, current_head, digest, extract_source, image_id, read_json, source_tree, write_json


REPOSITORY = "https://github.com/linuxerlv/lober.git"
SUITES = ("control", "flows", "events", "recovery")
CHECKS = (*SUITES, "kvm_batch", "upgrade")


def replace_manifest(path, data):
    directory = path.parent.stat()
    with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as stream:
        target = Path(stream.name)
        try:
            stream.write(data)
            stream.close()
            os.chown(target, directory.st_uid, directory.st_gid)
            target.chmod(0o640)
            target.replace(path)
        finally:
            target.unlink(missing_ok=True)


def freeze(root, store, repo, commit, group):
    if current_head(REPOSITORY, "test", build_environment(os.environ)) != commit:
        raise ValueError("指定提交不是 GitHub 当前的 test 候选")
    final = root / commit
    if final.exists() or final.is_symlink():
        raise ValueError("候选目录已经存在，请保留原有记录")
    tree = source_tree(repo, commit)
    index = read_json(store / "images" / (tree + ".json"))
    image = index["image_id"]
    if (index["tree"] != tree or not re.fullmatch(r"sha256:[0-9a-f]{64}", image)
            or image_id(image) != image):
        raise ValueError("候选镜像索引与镜像不符")

    with tempfile.TemporaryDirectory(dir=root, prefix=".freeze-") as temp:
        staging = Path(temp) / "candidate"
        staging.mkdir()
        binary = staging / "lober"
        archive = staging / "source.tar"
        subprocess.run(["git", "-C", str(repo), "archive", "--format=tar", "--output", archive, commit],
                       check=True, timeout=180)
        files = extract_source(archive, staging / "source")
        cid = subprocess.check_output(["docker", "create", "--network", "none", image],
                                      text=True, timeout=30).strip()
        if not re.fullmatch(r"[0-9a-f]{64}", cid):
            raise ValueError("无法识别提取候选程序的容器")
        try:
            subprocess.run(["docker", "cp", f"{cid}:/usr/local/bin/lober", binary],
                           check=True, timeout=60)
            if not stat.S_ISREG(binary.lstat().st_mode) or not binary.stat().st_size:
                raise ValueError("候选镜像内的程序文件无效")
        finally:
            subprocess.run(["docker", "rm", "-v", cid], check=True, timeout=30, stdout=subprocess.DEVNULL)

        write_json(staging / "candidate.json", {
            "product_sha": commit, "test_sha": commit, "run_id": uuid.uuid4().hex,
            "image_id": image, "binary": str(final / binary.name), "binary_sha256": digest(binary),
            "source_archive": str(final / archive.name), "source_archive_sha256": digest(archive),
            "files": files,
        })
        logs = staging / "logs"
        logs.mkdir()
        uid = root.stat().st_uid
        for path in (staging, *staging.rglob("*")):
            os.chown(path, uid, group)
            path.chmod(0o750 if path.is_dir() or path == binary else 0o640)
        logs.chmod(0o2770)
        staging.rename(final)


def bind(root, store, commit, runs):
    path = root / commit / "candidate.json"
    manifest = read_json(path)
    if manifest["product_sha"] != commit or manifest["test_sha"] != commit or "capacity" in manifest:
        raise ValueError("候选提交不符，或清单已经固定")
    if set(runs) != set(CHECKS) or len(set(runs.values())) != len(CHECKS):
        raise ValueError("每个测试需要不同的运行编号")
    image = manifest["image_id"]
    for suite in SUITES:
        if verify_suite_image(store, commit, runs[suite], suite) != image:
            raise ValueError(f"{suite} 镜像不一致")
    _, upgrade = verified_worker(store, commit, runs["upgrade"], "upgrade")
    if upgrade["image_id"] != image:
        raise ValueError("升级镜像不一致")
    combined = {**manifest, **{f"container_{key}_run": value for key, value in runs.items()}}
    require_batch_image(store, commit, combined)
    replace_manifest(path, (json.dumps(combined, ensure_ascii=False, indent=2) + "\n").encode())


def finish(root, store, commit, capacity_id):
    directory = root / commit
    path = directory / "candidate.json"
    initial = read_json(path)
    if not re.fullmatch(r"kvm-capacity\.[A-Za-z0-9]{8}", capacity_id):
        raise ValueError("容量编号无效")
    receipt = directory / "qualification" / capacity_id / "capacity-candidate.json"
    if receipt.resolve(strict=True) != receipt:
        raise ValueError("容量清单路径无效")
    # 验证与写入使用同一份字节。保留原校验值，供容量和后续验收复核。
    fd = os.open(receipt, os.O_RDONLY | os.O_NOFOLLOW | os.O_NONBLOCK)
    with os.fdopen(fd, "rb") as stream:
        if not stat.S_ISREG(os.fstat(stream.fileno()).st_mode):
            raise ValueError("容量清单不是普通文件")
        raw = stream.read((32 << 20) + 1)
    if len(raw) > 32 << 20:
        raise ValueError("容量清单超出大小限制")
    complete = json.loads(raw)
    base = {k: v for k, v in complete.items() if k not in ("capacity", "container_capacity_run")}
    if (initial != base or complete["container_capacity_run"] != capacity_id
            or Path(complete["capacity"]["evidence_root"]) != receipt.parent / "logs"):
        raise ValueError("容量清单与冻结候选不一致")
    require_capacity_image(store, commit, complete)
    original = read_json(store / "capacity" / (capacity_id + ".json"))
    if original["manifest_sha256"] != hashlib.sha256(raw).hexdigest():
        raise ValueError("容量原件与最终清单的校验值不符")
    replace_manifest(path, raw)


def collect(root, store, commit):
    """用原始日志生成门禁输入。只有完整门禁通过才写执行结论。"""
    directory = root / commit
    path = directory / "candidate.json"
    execution = directory / "execution.json"
    manifest = read_json(path)
    manifest_sha = digest(path)
    account = "lober-ci-test"
    logs = directory / "logs"
    qualification = directory / "qualification"
    enterprise = Path(__file__).resolve().parents[2] / "tests/lobertest/enterprise"
    if "capacity" not in manifest or logs.resolve(strict=True) != logs:
        raise ValueError("缺少最终容量清单或日志目录无效")
    execution.unlink(missing_ok=True)
    require_capacity_image(store, commit, manifest)
    common = {key: manifest[key] for key in ("run_id", "product_sha", "test_sha", "binary_sha256", "image_id")}
    for suite in SUITES:
        run_id = manifest[f"container_{suite}_run"]
        if verify_suite_image(store, commit, run_id, suite) != manifest["image_id"]:
            raise ValueError(f"{suite} 镜像不一致")
        original = store / "runs" / run_id
        report = read_json(original / f"{suite}.json")
        evidence = logs / f"{suite}.log"
        replace_manifest(evidence, (original / "check.log").read_bytes())
        row = dict(common, status="PASS", count=report["count"], failed=0, skipped=0,
                   manifest_sha256=manifest_sha, evidence=evidence.name, evidence_sha256=digest(evidence))
        replace_manifest(logs / f"{suite}.json", (json.dumps(row) + "\n").encode())

    with tempfile.TemporaryDirectory(dir=directory) as temporary:
        staging = Path(temporary)
        subprocess.run([sys.executable, "-I", enterprise / "upgrade_evidence.py",
                        "--manifest", path, "--run", store / "runs" / manifest["container_upgrade_run"],
                        "--results", staging], check=True, timeout=120)
        for output in staging.iterdir():
            replace_manifest(logs / output.name, output.read_bytes())
    for kind in ("installation", "capacity"):
        subprocess.run(["runuser", "-u", account, "--", sys.executable, "-I",
                        directory / "source/tests/lobertest/enterprise/installation_evidence.py", "--kind", kind,
                        "--manifest", path, "--output", logs, "--product-sha", commit,
                        "--qualification-root", qualification], check=True, timeout=600)
    subprocess.run([sys.executable, "-I", str(Path(__file__).with_name("export-image-evidence.py")), path],
                   check=True, timeout=120)
    subprocess.run(["runuser", "-u", account, "--", sys.executable, "-I", enterprise / "gate.py",
                    "--profile", "release", "--source-root", directory / "source", "--results", logs,
                    "--product-sha", commit, "--manifest", path, "--qualification-root", qualification,
                    "--image-root", "/var/lib/lober-ci-container-evidence"], check=True, timeout=120)
    receipt = dict(status="PASS", branch="test", sha=commit, manifest_sha256=manifest_sha,
                   run_id=manifest["run_id"], binary_sha256=manifest["binary_sha256"])
    replace_manifest(execution, (json.dumps(receipt) + "\n").encode())


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=Path, default=Path("/var/lib/lober-ci-test"))
    parser.add_argument("--store", type=Path, default=Path("/var/lib/lober-container-ci"))
    parser.add_argument("commit")
    actions = parser.add_subparsers(dest="action", required=True)
    prepare = actions.add_parser("freeze")
    prepare.add_argument("repo", type=Path)
    select = actions.add_parser("bind")
    for suite in CHECKS:
        select.add_argument("--" + suite.replace("_", "-"), required=True)
    promote = actions.add_parser("finish")
    promote.add_argument("capacity_id")
    actions.add_parser("collect")
    args = parser.parse_args()
    if os.geteuid() != 0 or not re.fullmatch(r"[0-9a-f]{40}", args.commit):
        parser.error("请由宿主机管理员传入完整提交号")
    os.umask(0o077)
    try:
        for directory in (args.root, args.store):
            info = directory.lstat()
            if (directory.resolve(strict=True) != directory or not stat.S_ISDIR(info.st_mode)
                    or info.st_uid != 0 or info.st_mode & 0o022):
                raise ValueError("候选目录和镜像库须由管理员控制")
        if args.store.stat().st_mode & 0o077:
            raise ValueError("镜像库必须是管理员私有目录")
        with (args.root / (args.commit + ".container.lock")).open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            if args.action == "freeze":
                freeze(args.root, args.store, args.repo, args.commit, grp.getgrnam("lober-ci-test").gr_gid)
            else:
                candidate = args.root / args.commit
                for path in (candidate, candidate / "candidate.json"):
                    details = path.lstat()
                    if path.is_symlink() or details.st_uid != 0 or details.st_mode & 0o022:
                        raise ValueError("候选清单须由管理员控制")
                if args.action == "bind":
                    bind(args.root, args.store, args.commit, {key: getattr(args, key) for key in CHECKS})
                elif args.action == "finish":
                    finish(args.root, args.store, args.commit, args.capacity_id)
                else:
                    if args.store != Path("/var/lib/lober-container-ci"):
                        raise ValueError("发布回报必须使用宿主机的固定镜像库")
                    collect(args.root, args.store, args.commit)
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        parser.exit(1, f"发布清单组装失败：{error}\n")


if __name__ == "__main__":
    main()
