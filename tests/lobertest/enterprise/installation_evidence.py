"""只读取已冻结的安装原件，不操作设备或启动安装。"""
import sys

if __name__ == "__main__" and not sys.flags.isolated:
    raise SystemExit("请使用 python3 -I 运行证据检查")

import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import re
from types import SimpleNamespace

if __name__ == "__main__":
    # 先将旧结果置为未完成；核验源码前不导入本仓库的模块或其字节码缓存。
    early = argparse.ArgumentParser(add_help=False)
    early.add_argument("--manifest", type=Path, required=True)
    early.add_argument("--output", type=Path, required=True)
    early.add_argument("--product-sha", required=True)
    early.add_argument("--kind", choices=("installation", "capacity"), default="installation")
    args, _ = early.parse_known_args()
    args.output.mkdir(parents=True, exist_ok=True)
    for name in (("pxe_state", "real_install") if args.kind == "installation" else ("capacity_soak",)):
        target = args.output / (name + ".json")
        temporary = target.with_suffix(".json.tmp")
        temporary.write_text(json.dumps({"status": "RUNNING", "product_sha": args.product_sha}) + "\n")
        temporary.replace(target)
    try:
        frozen = json.loads(args.manifest.read_text())
        root = Path(__file__).resolve().parents[3]
        own_name = "tests/lobertest/enterprise/installation_evidence.py"
        if hashlib.sha256(Path(__file__).read_bytes()).hexdigest() != frozen["files"][own_name]:
            raise ValueError("verifier source changed")
        gate_name = "tests/lobertest/enterprise/gate.py"
        path = root / gate_name
        source = path.read_bytes()
        if hashlib.sha256(source).hexdigest() != frozen["files"][gate_name]:
            raise ValueError("gate source changed")
        spec = importlib.util.spec_from_file_location("gate", path)
        trusted_gate = importlib.util.module_from_spec(spec)
        sys.modules["gate"] = trusted_gate
        exec(compile(source, str(path), "exec"), trusted_gate.__dict__)
        cap_name = "tests/lobertest/enterprise/production_capacity.py"
        capacity = trusted_gate.load_verified_module(root / cap_name, frozen["files"][cap_name], "production_capacity")
        atomic_json, begin_run, digest, validate_candidate = (trusted_gate.atomic_json, trusted_gate.begin_run,
                                                               trusted_gate.digest, trusted_gate.validate_candidate)
    except (KeyError, ValueError, TypeError, OSError, json.JSONDecodeError, SyntaxError):
        raise SystemExit(1)
else:
    from gate import atomic_json, begin_run, digest, validate_candidate
    import production_capacity as capacity

CHECKER = "tests/lobertest/enterprise/cloud_install.py"
HELPERS = tuple("tests/lobertest/enterprise/" + x + ".py" for x in
                ("cloud_packet_capture", "cloud_capture_session", "cloud_guest_identity", "cloud_no_task_probe"))
TRUSTED_FILES = (CHECKER,) + HELPERS + (
    "tests/lobertest/enterprise/production_capacity.py",
    "tests/lobertest/enterprise/installation_evidence.py",
    "tests/lobertest/enterprise/gate.py",
)


def verify_installation(manifest, source, root):
    try:
        source, root = Path(source).resolve(strict=True), Path(root).resolve(strict=True)
        identity = {name: manifest[name] for name in ("run_id", "product_sha", "test_sha", "binary_sha256")}
        for name in TRUSTED_FILES:
            path = (source / name).resolve(strict=True)
            if not path.is_relative_to(source) or digest(path) != manifest["files"][name]:
                raise ValueError("frozen checker source mismatch")
        config = manifest["capacity"]
        common_file = capacity.private_file(config["run_identity_file"], root)
        if digest(common_file) != config["run_identity_sha256"]:
            raise ValueError("common identity changed")
        common = json.loads(common_file.read_text())
        if any(common.get(name) != value for name, value in identity.items()):
            raise ValueError("common identity mismatch")
        expected = {Path(name).name: manifest["files"][name] for name in TRUSTED_FILES}
        for mapping, required in ((common["installation_checkers"], {Path(CHECKER).name}),
                                  (common["installation_helpers"], {Path(name).name for name in HELPERS})):
            if not isinstance(mapping, dict) or {Path(name).name for name in mapping} != required:
                raise ValueError("checker contract incomplete")
            for name, value in mapping.items():
                path = Path(name)
                resolved = path.resolve(strict=True)
                if (path.is_symlink() or not resolved.is_relative_to(root) or path.name not in expected
                        or value != expected[path.name] or digest(path) != value):
                    raise ValueError("checker contract mismatch")
        ref = config["concurrent_installation"]
        report_file = capacity.private_file(ref["path"], root)
        if digest(report_file) != ref["sha256"]:
            raise ValueError("installation report changed")
        report = json.loads(report_file.read_text())
        identity["run_identity_sha256"] = config["run_identity_sha256"]
        return capacity.installation_problems(report, identity, report_file.parent, common)
    except (KeyError, TypeError, ValueError, OSError, json.JSONDecodeError):
        return ["installation evidence missing, changed, or untrusted"]


def verify_capacity(manifest, source, root, manifest_path):
    problems = verify_installation(manifest, source, root)
    if problems:
        return problems
    try:
        root = Path(root).resolve(strict=True)
        config = manifest["capacity"]
        profile_file = capacity.private_file(config["profile_file"], root)
        if digest(profile_file) != config["profile_sha256"]:
            raise ValueError("profile changed")
        profile = json.loads(profile_file.read_text())
        if capacity.profile_problems(profile):
            raise ValueError("profile is not the complete qualification")
        output = Path(config["evidence_root"])
        resolved = output.resolve(strict=True)
        if output.is_symlink() or not resolved.is_relative_to(root) or not resolved.is_dir():
            raise ValueError("capacity evidence outside owned root")
        if resolved.stat().st_uid != os.getuid() or resolved.stat().st_mode & 0o077:
            raise ValueError("capacity evidence is not private")
        common_file = capacity.private_file(config["run_identity_file"], root)
        common = json.loads(common_file.read_text())
        identity = {name: manifest[name] for name in ("run_id", "product_sha", "test_sha", "binary_sha256")}
        identity.update(manifest_sha256=digest(manifest_path), profile_sha256=config["profile_sha256"],
                        executor_sha256=manifest["files"]["tests/lobertest/enterprise/production_capacity.py"],
                        run_identity_sha256=config["run_identity_sha256"])
        context = SimpleNamespace(output_root=resolved, identity=identity, profile=profile,
                                  c=config, common_identity=common)
        return capacity.assess_capacity(context)[0]
    except (KeyError, TypeError, ValueError, OSError, json.JSONDecodeError):
        return ["capacity evidence missing, changed, or untrusted"]


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--manifest", type=Path, required=True)
    parser.add_argument("--output", type=Path, required=True)
    parser.add_argument("--product-sha", required=True)
    parser.add_argument("--kind", choices=("installation", "capacity"), default="installation")
    args = parser.parse_args()
    if not re.fullmatch(r"[0-9a-f]{40}", args.product_sha):
        parser.error("product-sha must be a full Git SHA")
    args.output.mkdir(parents=True, exist_ok=True)
    names = ("pxe_state", "real_install") if args.kind == "installation" else ("capacity_soak",)
    for name in names:
        begin_run(args.output / (name + ".json"), "initializing", args.product_sha, "")
    try:
        manifest = json.loads(args.manifest.read_text())
        for name in names:
            begin_run(args.output / (name + ".json"), manifest["run_id"], args.product_sha, manifest["test_sha"])
        source = Path(__file__).resolve().parents[3]
        problems = validate_candidate(manifest, source, manifest["binary"])
        if manifest.get("product_sha") != args.product_sha:
            problems.append("candidate identity mismatch")
        if not problems:
            if args.kind == "installation":
                problems += verify_installation(manifest, source, capacity.BASE)
            else:
                problems += verify_capacity(manifest, source, capacity.BASE, args.manifest)
        log = args.output / (args.kind + "-verification.log")
        log.write_text(json.dumps({"product_sha": args.product_sha, "run_id": manifest["run_id"],
                                   "manifest_sha256": digest(args.manifest), "problems": problems},
                                  ensure_ascii=False, indent=2) + "\n")
        for name in names:
            row = {"status": "FAIL" if problems else "PASS", "count": 4,
                   "failed": len(problems), "skipped": 0, "product_sha": args.product_sha,
                   "test_sha": manifest["test_sha"], "binary_sha256": manifest["binary_sha256"],
                   "run_id": manifest["run_id"], "manifest_sha256": digest(args.manifest),
                   "evidence": log.name, "evidence_sha256": digest(log),
                   "scope": ("已冻结的两台并行及两台串行安装原件" if args.kind == "installation"
                             else "完整规模、24 小时长稳及真实并发安装原件")}
            atomic_json(args.output / (name + ".json"), row)
        return 1 if problems else 0
    except (KeyError, ValueError, OSError, TypeError, json.JSONDecodeError):
        return 1


if __name__ == "__main__":
    raise SystemExit(main())
