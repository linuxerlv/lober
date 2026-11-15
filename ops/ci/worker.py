"""Run one frozen dev or test candidate on the designated Linux machine."""

import argparse
from datetime import datetime, timezone
import hashlib
import json
import os
from pathlib import Path
import platform
import re
import shlex
import shutil
import socket
import stat
import subprocess
import sys
import tarfile
import tempfile
import time
import uuid


SHA = r"[0-9a-f]{40}"
GATE = Path(__file__).resolve().parents[2] / "tests/lobertest/enterprise/gate.py"
SUITES = ("gate", "control", "flows", "events", "recovery")
MEDIA = ("LOBER_TEST_ESXI7_ISO", "LOBER_TEST_ESXI8_ISO", "LOBER_TEST_ESXI9_ISO",
         "LOBER_TEST_REAL_ISO")
MEDIA_TESTS = {
    ("github.com/lober/lober/internal/application/media", "TestImportRealESXi/" + version)
    for version in ("7", "8", "9")
} | {
    ("github.com/lober/lober/internal/application/media", "TestIdentifyRealISO"),
    ("github.com/lober/lober/internal/infrastructure/media/iso9660", "TestRealISO"),
}
BUILD_ENV = ("PATH", "HOME", "LANG", "LC_ALL", "LC_CTYPE", "XDG_CACHE_HOME",
             "XDG_CONFIG_HOME", "XDG_DATA_HOME", "GOCACHE", "GOMODCACHE", "GOPATH",
             "PLAYWRIGHT_BROWSERS_PATH")


def utc_now():
    return datetime.now(timezone.utc).isoformat(timespec="seconds")


def valid_repository(value):
    return re.fullmatch(r"https://github\.com/[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9_][A-Za-z0-9_.-]*", value) is not None


def parse_ref(output, branch):
    match = re.fullmatch(f"({SHA})\trefs/heads/{re.escape(branch)}\n?", output)
    if not match:
        raise ValueError("GitHub 没有返回唯一且完整的目标分支提交")
    return match[1]


def digest(path):
    with path.open("rb") as stream:
        return hashlib.file_digest(stream, "sha256").hexdigest()


def write_json(path, value):
    temporary = path.with_suffix(".tmp")
    temporary.write_text(json.dumps(value, ensure_ascii=False, indent=2) + "\n", encoding="utf-8")
    temporary.replace(path)


def read_json(path):
    return json.loads(path.read_text(encoding="utf-8"))


def extract_source(archive, source):
    with tarfile.open(archive, "r:") as stream:
        files = set()
        for member in stream.getmembers():
            path = Path(member.name)
            if (not (member.isdir() or member.isfile()) or path.is_absolute()
                    or ".." in path.parts or not path.parts or path.parts[0] == ".git"
                    or "\\" in member.name or path in files):
                raise ValueError("源码归档包含不安全的路径或文件类型")
            files.add(path)
        source.mkdir()
        stream.extractall(source, filter="data")
    return {path.as_posix(): digest(source / path) for path in sorted(files)
            if (source / path).is_file()}


def go_summary(output, *, optional_media=False):
    result = {**empty_counts(), "package_failures": 0, "optional_skips": []}
    for line in output.splitlines():
        if not line.startswith("{"):
            continue
        event = json.loads(line)
        action = event.get("Action")
        case = event.get("Test")
        if case and action in ("pass", "skip", "fail"):
            result["count"] += 1
            if action == "skip":
                key = event.get("Package"), case
                if optional_media and key in MEDIA_TESTS:
                    result["optional_skips"].append(":".join(key))
                else:
                    result["skipped"] += 1
        if action == "fail":
            result["failed" if case else "package_failures"] += 1
        if action == "build-fail":
            result["package_failures"] += 1
    return result


def frontend_summary(output):
    output = re.sub(r"\x1b\[[0-9;]*[a-zA-Z]", "", output)
    vitest = re.findall(r"^\s*Tests +([^\n]+) \((\d+)\)\s*$", output, re.MULTILINE)
    node = dict(re.findall(r"^# (tests|pass|fail|cancelled|skipped|todo) (\d+)\s*$",
                           output, re.MULTILINE))
    if len(vitest) != 2 or set(node) != {"tests", "pass", "fail", "cancelled", "skipped", "todo"}:
        return None
    result = {
        "count": int(node["tests"]),
        "failed": int(node["fail"]) + int(node["cancelled"]),
        "skipped": int(node["skipped"]) + int(node["todo"]),
    }
    if result["count"] <= 0 or int(node["pass"]) + result["failed"] + result["skipped"] != result["count"]:
        return None
    for counts, total in vitest:
        parts = {name: int(number) for number, name in re.findall(r"(\d+) (passed|failed|skipped|todo)", counts)}
        if int(total) <= 0 or sum(parts.values()) != int(total):
            return None
        result["count"] += int(total)
        result["failed"] += parts.get("failed", 0)
        result["skipped"] += parts.get("skipped", 0) + parts.get("todo", 0)
    return result


def build_environment(environ):
    result = {key: environ[key] for key in BUILD_ENV if key in environ}
    result.update(CI="true", NO_COLOR="1", GOENV="off", GOWORK="off",
                  GIT_CONFIG_NOSYSTEM="1", GIT_CONFIG_GLOBAL="/dev/null",
                  GIT_TERMINAL_PROMPT="0", GIT_ALLOW_PROTOCOL="https")
    return result


def isolated_recovery_namespace(environ):
    expected = Path(environ.get("LOBER_CI_RECOVERY_NETNS", ""))
    if expected.parent != Path("/run/netns"):
        return False
    try:
        return os.path.samestat(os.stat("/proc/self/ns/net"), expected.stat())
    except OSError:
        return False


def current_head(repository, branch, env):
    ref = "refs/heads/" + branch
    output = subprocess.run(["git", "-c", "http.followRedirects=false", "ls-remote",
                             "--exit-code", repository, ref],
                            cwd="/", env=env, check=True, capture_output=True, text=True, timeout=90)
    return parse_ref(output.stdout, branch)


class Candidate:
    def __init__(self, branch, root, sha, repository, environ):
        self.branch = branch
        self.root = root
        self.sha = sha
        self.repository = repository
        self.environ = environ
        self.path = root / sha
        self.logs = self.path / "logs"
        self.source = self.path / "source"
        self.build = self.path / "build"
        self.binary = self.path / "lober"
        self.archive = self.path / "source.tar"
        self.manifest = self.path / "candidate.json"
        self.execution = self.path / "execution.json"
        self.ready = self.path / "dev-ready.json"
        self.env = build_environment(environ)
        self.env["TMPDIR"] = str(self.path / "scratch")
        self.test_env = dict(self.env)
        for key in ("LOBER_TEST_PG_DSN", "LOBER_ENTERPRISE_VM_IP"):
            if environ.get(key):
                self.test_env[key] = environ[key]
        for variable, name in (("LOBER_TEST_BASH", "bash"), ("LOBER_TEST_BUSYBOX", "busybox")):
            path = shutil.which(name, path=self.env.get("PATH"))
            if path:
                self.test_env[variable] = path

    def command(self, name, argv, *, cwd=None, env=None, timeout=2700):
        argv = [str(value) for value in argv]
        start = time.monotonic()
        log_path = self.logs / (name + ".log")
        command_cwd = self.build if cwd is None else cwd
        metadata = {"command": argv, "cwd": str(command_cwd), "started": utc_now()}
        with log_path.open("wb") as log:
            log.write(("$ " + shlex.join(argv) + "\n").encode())
            log.flush()
            try:
                proc = subprocess.run(["timeout", "--kill-after=15s", str(timeout)] + argv,
                                      cwd=command_cwd, env=self.env if env is None else env,
                                      stdout=log, stderr=subprocess.STDOUT, check=False)
                code = proc.returncode
            except OSError as error:
                log.write((str(error) + "\n").encode())
                code = 127
        write_json(self.logs / (name + ".command.json"), {
            **metadata, "exit_code": code, "duration_seconds": round(time.monotonic() - start, 3),
        })
        return code

    def require(self, name, argv, **kwargs):
        if self.command(name, argv, **kwargs):
            raise RuntimeError(f"{name} 执行失败，详情见 {name}.log")

    def restore_missing_ui_files(self):
        # 架构测试要能读取 Git 列出的文件。保留 Vite 已生成的同名文件。
        dist = "internal/interfaces/console/ui/dist"
        tracked = subprocess.check_output(["git", "ls-files", "-z", "--", dist],
                                          cwd=self.build, env=self.env)
        paths = [os.fsdecode(path) for path in tracked.split(b"\0") if path]
        missing = [path for path in paths if not (self.build / path).exists()]
        if missing:
            self.require("restore-ui-placeholders", ["git", "restore", "--source", self.sha, "--", *missing])

    def prepare(self):
        self.path.mkdir(mode=0o700)
        self.logs.mkdir()
        (self.path / "scratch").mkdir()
        self.require("git-init", ["git", "init", "--template=", self.build], cwd=self.path)
        self.require("git-fetch", ["git", "-c", "http.followRedirects=false", "fetch", "--no-tags",
                                   "--depth=1", self.repository, self.sha], timeout=180)
        actual = subprocess.check_output(["git", "rev-parse", "FETCH_HEAD"],
                                         cwd=self.build, env=self.env, text=True).strip()
        if actual != self.sha:
            raise RuntimeError("下载的提交与冻结提交不一致")
        self.require("git-archive", ["git", "archive", "--format=tar", "--output", self.archive, self.sha])
        files = extract_source(self.archive, self.source)
        self.require("git-checkout", ["git", "-c", "core.hooksPath=/dev/null",
                                      "checkout", "--detach", self.sha])
        self.require("frontend-install", ["pnpm", "install", "--frozen-lockfile"], cwd=self.build / "web")
        self.require("frontend", ["make", "webcheck"],
                     env={**self.env, "NODE_OPTIONS": "--test-reporter=tap"})
        self.require("build", ["go", "build", "-trimpath", "-o", self.binary, "./cmd/lober"])
        self.restore_missing_ui_files()
        data = {"product_sha": self.sha, "test_sha": self.sha, "run_id": uuid.uuid4().hex,
                "binary": str(self.binary), "binary_sha256": digest(self.binary),
                "source_archive": str(self.archive), "source_archive_sha256": digest(self.archive),
                "files": files}
        write_json(self.manifest, data)

    def attach_qualification(self):
        config = self.environ.get("LOBER_CI_CAPACITY_MANIFEST")
        if self.branch != "test" or not config:
            return
        path = Path(config)
        if not path.is_absolute():
            raise ValueError("LOBER_CI_CAPACITY_MANIFEST 必须是绝对路径")
        qualification = read_json(path)
        if not isinstance(qualification, dict):
            raise ValueError("容量配置必须是 JSON 对象")
        candidate = read_json(self.manifest)
        identity_path = Path(qualification["run_identity_file"]).resolve(strict=True)
        if (not identity_path.is_relative_to(self.path / "qualification")
                or digest(identity_path) != qualification["run_identity_sha256"]):
            raise ValueError("装机原件身份文件必须来自本轮私有目录且校验值一致")
        identity = read_json(identity_path)
        keys = ("run_id", "product_sha", "test_sha", "binary_sha256")
        if any(identity.get(key) != candidate[key] for key in keys):
            raise ValueError("装机原件属于另一轮候选")
        if "capacity" in candidate:
            if candidate["capacity"] != qualification:
                raise ValueError("本轮正式容量配置已经冻结")
            return
        candidate["capacity"] = qualification
        write_json(self.manifest, candidate)


    def evidence(self, name, code, counts, *, problems=()):
        manifest = read_json(self.manifest)
        log = self.logs / (name + ".log")
        status = "PASS" if (code == 0 and counts["count"] > 0
                            and not counts["failed"] and not counts["skipped"] and not problems) else "FAIL"
        write_json(self.logs / (name + ".json"), {
            "status": status, **counts, "problems": list(problems), "exit_code": code,
            "run_id": manifest["run_id"], "manifest_sha256": digest(self.manifest),
            "product_sha": self.sha, "test_sha": self.sha, "binary_sha256": manifest["binary_sha256"],
            "evidence": log.name, "evidence_sha256": digest(log),
        })
        return status == "PASS"

    def frontend(self):
        counts = frontend_summary((self.logs / "frontend.log").read_text(encoding="utf-8", errors="replace"))
        code = read_json(self.logs / "frontend.command.json")["exit_code"]
        return self.evidence("frontend", code, counts or empty_counts(),
                             problems=() if counts else ("缺少两套 Vitest 或 Node 的实际测试计数",))

    def backend(self):
        if not self.test_env.get("LOBER_TEST_PG_DSN"):
            raise ValueError("必须为本阶段配置专用测试数据库 LOBER_TEST_PG_DSN")
        # 安装脚本测试会替换 /var/lib/lober，临时路径必须避开这个前缀。
        web = self.build / "web"
        backend_env = {**self.test_env, "TMPDIR": "/tmp", "LOBER_TEST_BATCH_WEB": str(web)}
        # 控制台用例会多次运行完整口令校验。并发检测下 20 分钟不够。
        checks = [
            ("backend-unit", ["go", "test", "-json", "-count=1", "./..."]),
            ("backend-race", ["go", "test", "-json", "-count=1", "-race", "-timeout=40m", "./..."]),
        ]
        static = self.command("backend-static", ["make", "fmt", "vet", "cross", "gen", "archcheck"],
                              env=backend_env)
        browser = self.command("browser", ["pnpm", "exec", "playwright", "install", "chromium", "--only-shell"],
                               cwd=web)
        codes = {"backend-static": static, "browser": browser}
        counts = {**empty_counts(), "package_failures": 0, "optional_skips": []}
        problems = ["浏览器准备失败，见 browser.log"] if browser else []
        with (self.logs / "backend.log").open("wb") as combined:
            for name, code in codes.items():
                combined.write((self.logs / (name + ".log")).read_bytes())
                combined.write(f"\n{name} exit_code={code}\n".encode())
            for name, argv in checks:
                codes[name] = self.command(name, argv, env=backend_env)
                raw = (self.logs / (name + ".log")).read_bytes()
                combined.write(raw)
                combined.write(f"\n{name} exit_code={codes[name]}\n".encode())
                summary = go_summary(raw.decode("utf-8", errors="replace"), optional_media=True)
                for key, value in summary.items():
                    counts[key] += value
                if not summary["count"]:
                    problems.append(f"{name} 缺少测试结果")
                if summary["package_failures"]:
                    problems.append(f"{name} 有构建或包级失败")
        return self.evidence("backend", next((code for code in codes.values() if code), 0),
                             counts, problems=problems)

    def common_args(self):
        return ["--output", self.logs, "--manifest", self.manifest, "--product-sha", self.sha]

    def enterprise(self):
        scripts = self.source / "tests/lobertest/enterprise"
        fixture = self.path / "synthetic-rocky.iso"
        fixture.unlink(missing_ok=True)
        fixture_code = self.command("fixture", ["bash", scripts / "fixturegen.sh", fixture], cwd=self.source)
        node = shutil.which("node", path=self.env.get("PATH"))
        enterprise_env = dict(self.test_env, LOBER_ENTERPRISE_BINARY=str(self.binary),
                              LOBER_ENTERPRISE_FIXTURE=str(fixture), LOBER_ENTERPRISE_NODE=node or "",
                              LOBER_ENTERPRISE_UI_SCRIPT=str(scripts / "ui.mjs"),
                              LOBER_ENTERPRISE_PLAYWRIGHT=str(self.build / "web/node_modules/playwright/index.mjs"))
        passed = fixture_code == 0
        recovery_launcher = self.environ.get("LOBER_CI_RECOVERY_LAUNCHER")
        for suite in SUITES:
            if suite == "recovery" and recovery_launcher:
                launcher = admin_executable(recovery_launcher, self.root)
                code = self.command(suite, ["/usr/bin/sudo", "-n", launcher, self.sha],
                                    env=self.env, timeout=540)
                passed = passed and code == 0
                continue
            if suite == "recovery" and not isolated_recovery_namespace(self.environ):
                reason = "故障演练需要本阶段的隔离网络命名空间"
                (self.logs / "recovery.log").write_text(reason + "\n", encoding="utf-8")
                self.evidence("recovery", None, empty_counts(), problems=(reason,))
                passed = False
                continue
            code = self.command(suite, ["python3", "-E", "-s", "-B", scripts / "run.py", suite,
                                        *self.common_args(), "--test-sha", self.sha],
                                cwd=self.source, env=enterprise_env)
            passed = passed and code == 0
        return passed

    def gate(self, profile):
        return self.command("gate-" + profile, ["python3", "-I",
                            GATE, "--source-root", self.source,
                            "--qualification-root", self.path / "qualification",
                            "--profile", profile, "--results", self.logs,
                            "--product-sha", self.sha, "--manifest", self.manifest], cwd=self.source)

    def fuzz(self, name, package, target):
        code = self.command(name, ["go", "test", "-json", package, "-run=^$", "-fuzz=^" + target + "$",
                                   "-fuzztime=60s", "-parallel=2"])
        output = (self.logs / (name + ".log")).read_text(encoding="utf-8")
        executions = re.findall(r"fuzz: elapsed: [^,]+, execs: (\d+)", output)
        summary = go_summary(output)
        summary["count"] = int(executions[-1]) if executions else 0
        problems = ("有构建或包级失败",) if summary["package_failures"] else ()
        return self.evidence(name, code, summary, problems=problems)

    def scan(self):
        code = self.command("security_scan", ["govulncheck", "./..."])
        output = (self.logs / "security_scan.log").read_text(encoding="utf-8")
        completed = "No vulnerabilities found" in output
        return self.evidence("security_scan", code, {
            "count": int(completed), "failed": int(code != 0), "skipped": 0,
        })

    def audit(self):
        code = self.command("frontend_audit", ["pnpm", "audit", "--prod", "--json",
                            "--registry=https://registry.npmjs.org"], cwd=self.build / "web")
        lines = (self.logs / "frontend_audit.log").read_text(encoding="utf-8").splitlines()
        try:
            report = json.loads("\n".join(lines[1:]))
            totals = report["metadata"]["vulnerabilities"]
            levels = ("info", "low", "moderate", "high", "critical")
            if not all(type(totals.get(level)) is int and totals[level] >= 0 for level in levels):
                raise ValueError("bad audit totals")
            advisories = sum(totals[level] for level in levels)
            counts = {"count": 1, "failed": int(advisories > 0), "skipped": 0, "advisories": advisories}
        except (AttributeError, KeyError, TypeError, ValueError):
            counts = empty_counts()
        return self.evidence("frontend_audit", code, counts)

    def real_media(self):
        config = self.environ.get("LOBER_CI_MEDIA_CHECKSUMS")
        env = {key: self.environ.get(key, "") for key in MEDIA}
        if config and Path(config).is_absolute() and all(Path(value).is_file() for value in env.values()):
            return self.command("real-media-command", ["python3", "-E", "-s", "-B",
                                self.source / "tests/lobertest/enterprise/real_media_evidence.py",
                                *self.common_args(), "--checksums", config],
                                cwd=self.source, env={**self.test_env, **env}) == 0
        reason = "真实镜像或可信校验值未配置，未执行读取与导入测试"
        (self.logs / "real_media.log").write_text(reason + "\n", encoding="utf-8")
        self.evidence("real_media", None, empty_counts(), problems=(reason,))
        return None

    def deployment(self):
        hooks = [admin_executable(self.environ.get(key, ""), self.root) for key in
                 ("LOBER_CI_DEV_DEPLOY", "LOBER_CI_DEV_SMOKE")]
        binary_sha = read_json(self.manifest)["binary_sha256"]
        manifest_sha = digest(self.manifest)
        if digest(self.binary) != binary_sha:
            raise RuntimeError("部署前二进制与冻结清单不一致")
        if current_head(self.repository, self.branch, self.env) != self.sha:
            raise RuntimeError("dev 已有更新提交，跳过旧候选的部署")
        for name, hook in zip(("deploy", "smoke"), hooks):
            self.require(name, [hook, self.binary, self.sha], cwd=hook.parent,
                         env=build_environment(self.environ), timeout=600)
        if digest(self.binary) != binary_sha or digest(self.manifest) != manifest_sha:
            raise RuntimeError("部署期间二进制或冻结清单已更改")
        write_json(self.ready, {"sha": self.sha, "binary_sha256": binary_sha,
                                "manifest_sha256": manifest_sha})

    def run(self, retry):
        previous = read_json(self.execution) if self.execution.is_file() else {}
        if previous.get("status") in ("PASS", "FAIL", "BLOCKED") and not retry:
            return 0
        if self.path.exists() and not self.manifest.is_file():
            attempts = self.root / "attempts"
            attempts.mkdir(exist_ok=True)
            self.path.rename(attempts / (self.sha + "-" + uuid.uuid4().hex))
        if self.path.is_dir():
            self.ready.unlink(missing_ok=True)
            write_json(self.execution, {"status": "RUNNING", "branch": self.branch, "sha": self.sha})
        started = utc_now()
        status = "FAIL"
        problem = ""
        try:
            if not self.manifest.is_file():
                self.prepare()
            for name in ("backend", "frontend", *SUITES, "fuzz_iso", "fuzz_wim",
                         "security_scan", "frontend_audit", "real_media"):
                (self.logs / (name + ".json")).unlink(missing_ok=True)
            self.attach_qualification()
            frontend = self.frontend()
            backend = self.backend()
            control = self.enterprise()
            if self.branch == "dev":
                if self.gate("control-complete") or not all((frontend, backend, control)):
                    raise RuntimeError("完整控制面检查失败")
                self.deployment()
                status = "PASS"
            else:
                automated = (
                    frontend, backend, control,
                    self.fuzz("fuzz_iso", "./internal/infrastructure/media/iso9660", "FuzzISO"),
                    self.fuzz("fuzz_wim", "./internal/infrastructure/media", "FuzzWIMImages"),
                    self.scan(), self.audit(),
                )
                media = self.real_media()
                release = self.gate("release")
                if all(automated) and media is not False:
                    status = "BLOCKED" if release or media is None else "PASS"
                else:
                    problem = "自动检查失败，详情见 logs"
        except (OSError, KeyError, RuntimeError, ValueError, subprocess.SubprocessError) as error:
            problem = str(error)
        binding = {}
        if self.manifest.is_file():
            candidate = read_json(self.manifest)
            binding = {"manifest_sha256": digest(self.manifest),
                       "binary_sha256": candidate["binary_sha256"], "run_id": candidate["run_id"]}
        if self.path.is_dir():
            write_json(self.execution, {**binding, "status": status, "branch": self.branch, "sha": self.sha,
                                       "machine": socket.gethostname(), "platform": platform.platform(),
                                       "started": started, "finished": utc_now(), "problem": problem})
        print(f"{self.branch} {self.sha} {status}: {problem or self.logs}", flush=True)
        return 0 if status in ("PASS", "BLOCKED") else 1


def empty_counts():
    return {"count": 0, "failed": 0, "skipped": 0}


def admin_executable(value, root):
    path = Path(value)
    if not path.is_absolute():
        raise ValueError("部署和冒烟命令必须配置管理员维护的绝对路径")
    path = path.resolve(strict=True)
    if path.is_relative_to(root) or not path.is_file() or not os.access(path, os.X_OK):
        raise ValueError("部署和冒烟命令必须是候选目录之外的可执行文件")
    for component in (path, *path.parents):
        info = component.stat()
        if info.st_uid != 0 or info.st_mode & (stat.S_IWGRP | stat.S_IWOTH):
            raise ValueError("部署和冒烟命令及其父目录须由管理员拥有且普通用户不可写")
    return path


def image_id(reference):
    result = subprocess.run(["docker", "image", "inspect", "--format", "{{.Id}}", reference],
                            capture_output=True, text=True, timeout=30, check=False)
    if result.returncode:
        return ""
    value = result.stdout.strip()
    if not re.fullmatch(r"sha256:[0-9a-f]{64}", value):
        raise ValueError("Docker 返回了无效的镜像校验值")
    return value


def source_tree(repo, sha):
    if not re.fullmatch(SHA, sha):
        raise ValueError("必须使用完整的 Git 提交号")
    return subprocess.check_output(["git", "-C", str(repo), "rev-parse", "--verify",
                                    sha + "^{commit}^{tree}"], text=True, timeout=30).strip()


def image_run(root, repo, commit, promote_from=None, check=None):
    import fcntl

    run_id = uuid.uuid4().hex
    run = root / "runs" / run_id
    run.mkdir(mode=0o700, parents=True)
    result = {"run_id": run_id, "commit": commit, "promote_from": promote_from,
              "status": "RUNNING", "started": utc_now(), "machine": socket.gethostname()}
    write_json(run / "result.json", result)
    try:
        tree = source_tree(repo, commit)
        result["tree"] = tree
        if promote_from:
            if source_tree(repo, promote_from) != tree:
                raise ValueError("晋级提交的文件内容和原候选不一致")
            subprocess.run(["git", "-C", str(repo), "merge-base", "--is-ancestor",
                            promote_from, commit], timeout=30, check=True)
        store = root / "images"
        store.mkdir(mode=0o700, exist_ok=True)
        tag = "lober-candidate:" + tree
        record = store / (tree + ".json")
        with (store / (tree + ".lock")).open("a") as lock:
            fcntl.flock(lock, fcntl.LOCK_EX)
            if record.is_file():
                image = read_json(record)["image_id"]
                if image_id(image) != image:
                    raise ValueError("共享制品记录对应的镜像已丢失")
                result["image_action"] = "reused"
            elif promote_from:
                raise ValueError("晋级所需的原候选镜像记录不存在")
            else:
                with tempfile.TemporaryFile() as archive, (run / "build.log").open("wb") as log:
                    subprocess.run(["git", "-C", str(repo), "archive", "--format=tar", tree],
                                   stdout=archive, timeout=180, check=True)
                    archive.seek(0)
                    subprocess.run(["docker", "buildx", "build", "--builder", "default",
                                    "--load", "--progress=plain", "--build-arg", "GOPROXY",
                                    "--platform", "linux/amd64", "--tag", tag,
                                    "--file", "ops/ci/Dockerfile", "-"],
                                   stdin=archive, stdout=log, stderr=subprocess.STDOUT,
                                   timeout=3600, check=True)
                image = image_id(tag)
                if not image:
                    raise ValueError("构建完成后找不到本地镜像")
                write_json(record, {"tree": tree, "built_from": commit, "image_id": image})
                result["image_action"] = "built"
        result["image_id"] = image
        argv = ([str(check), image, str(run)] if check else
                ["docker", "run", "--rm", "--network", "none", image, "-h"])
        result["check_command"] = argv
        with (run / "check.log").open("wb") as log:
            outcome = subprocess.run(argv, stdout=log, stderr=subprocess.STDOUT,
                                     timeout=3600, check=False)
        result["check_exit_code"] = outcome.returncode
        result["check_log_sha256"] = digest(run / "check.log")
        result["status"] = "PASS" if outcome.returncode == 0 else "FAIL"
    except (OSError, KeyError, ValueError, subprocess.SubprocessError) as error:
        result.update(status="FAIL", problem=str(error))
    result["finished"] = utc_now()
    write_json(run / "result.json", result)
    print(f"{result['status']} {run / 'result.json'}", flush=True)
    return 0 if result["status"] == "PASS" else 1


def image_main():
    parser = argparse.ArgumentParser(description="构建或复用一个候选镜像，并独立记录本轮检查")
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--repo", type=Path, required=True)
    parser.add_argument("--commit", required=True)
    parser.add_argument("--promote-from")
    parser.add_argument("--check", type=Path)
    args = parser.parse_args(sys.argv[2:])
    if not sys.platform.startswith("linux") or not sys.flags.isolated:
        parser.error("请在 Linux 上使用 python3 -I 运行")
    os.umask(0o077)
    if not args.root.is_absolute() or not args.repo.is_absolute():
        parser.error("--root 和 --repo 必须是绝对路径")
    root = args.root.resolve(strict=True)
    info = root.stat()
    if not root.is_dir() or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        parser.error("镜像库目录须由当前执行账号拥有，权限为 0700")
    check = args.check
    if check is not None:
        check = admin_executable(check, root)
    return image_run(root, args.repo, args.commit, args.promote_from, check)


def main():
    if sys.argv[1:2] == ["image"]:
        return image_main()
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("branch", choices=("dev", "test"))
    parser.add_argument("--root", type=Path, required=True)
    parser.add_argument("--repository", required=True)
    parser.add_argument("--retry", action="store_true", help="重跑同一提交的自动检查")
    args = parser.parse_args()
    if not sys.flags.isolated or not sys.platform.startswith("linux"):
        parser.error("请在指定 Linux 测试机上使用 python3 -I 运行")
    if not valid_repository(args.repository):
        parser.error("--repository 必须是没有凭据和查询参数的 GitHub HTTPS 仓库地址")
    if not args.root.is_absolute():
        parser.error("--root 必须是绝对路径")

    import fcntl

    os.umask(0o077)
    root = args.root.resolve(strict=True)
    info = root.stat()
    if not root.is_dir() or os.getuid() == 0 or info.st_uid != os.getuid() or stat.S_IMODE(info.st_mode) != 0o700:
        parser.error("运行目录须由当前专用账号拥有，权限为 0700")
    with (root / ".worker.lock").open("a") as lock:
        try:
            fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        except BlockingIOError:
            return 0
        try:
            env = build_environment(os.environ)
            sha = current_head(args.repository, args.branch, env)
            return Candidate(args.branch, root, sha, args.repository, os.environ).run(args.retry)
        except (OSError, ValueError, subprocess.SubprocessError) as error:
            print(f"{args.branch} worker: {error}", file=sys.stderr)
            return 1


if __name__ == "__main__":
    raise SystemExit(main())
