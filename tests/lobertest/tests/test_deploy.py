"""部署闸门的五类输入：编译相关本机可验（1-2、4），推送相关需 VM（3、5）。

`cross_compile` 是整个工程的第一道闸门——它失败时必须让 session 立刻停止，
不跑任何用例，所以异常消息必须带 go 的完整 stderr，否则定位不到根因（见
`5089a86`：文件名撞上 Go 的 GOOS 隐式约束，当时就是靠这段输出才定位到的）。

就绪判据是三条组合（`lober -check` 退出 0、`systemctl is-active` 为
active、未认证登录拿 4xx 而非连接错误），`wait_ready` 超时时必须说清是
哪一条没过——只看 `systemctl is-active` 不够，`Type=simple` 下进程起来
不代表端口在听。
"""

from __future__ import annotations

import shutil
import subprocess

import pytest

from lobertest import deploy

# ----------------------------------------------------------------------
# 类别 1-2：cross_compile，本机可验，不需要 VM。
# ----------------------------------------------------------------------


def test_cross_compile_produces_linux_amd64_elf_binary(tmp_path):
    """类别 1：干净仓库上交叉编译，产出文件存在且是 ELF 64-bit（用 `file`
    命令核实，与团队已独立验证过的结论一致：GOOS=linux GOARCH=amd64 go
    build ./cmd/lober 退出 0，产物是 statically linked 的 ELF 64-bit
    LSB executable, x86-64）。"""
    repo_root = _find_repo_root()
    out = tmp_path / "lober-linux-amd64"

    result = deploy.cross_compile(repo_root, out)

    assert result == out
    assert out.exists()

    file_bin = shutil.which("file")
    if file_bin is None:
        pytest.skip("本机没有 file 命令，无法核实 ELF 格式")
    proc = subprocess.run([file_bin, str(out)], capture_output=True, text=True)
    assert "ELF 64-bit" in proc.stdout
    assert "x86-64" in proc.stdout


def test_cross_compile_failure_raises_with_full_go_stderr(tmp_path):
    """类别 2：在故意引入 Linux 不兼容代码的临时副本上编译失败，必须抛
    异常且消息含 go 的错误文本——这段输出是唯一能定位根因的信息（`5089a86`
    那类 GOOS 隐式约束错误就是靠它定位的）。用完必须清理临时副本，不留
    痕迹。"""
    real_repo_root = _find_repo_root()
    broken_repo_root = _make_broken_copy(tmp_path, real_repo_root)
    out = tmp_path / "lober-broken"

    try:
        with pytest.raises(Exception) as exc_info:
            deploy.cross_compile(broken_repo_root, out)

        message = str(exc_info.value)
        # 编译失败时不产出可用的二进制。
        assert not out.exists()
        # 消息里必须能看到 go 编译器报的错误文本，不能只是一句「编译失败」。
        assert "syntax error" in message or "undefined" in message or "expected" in message
    finally:
        shutil.rmtree(broken_repo_root, ignore_errors=True)


# ----------------------------------------------------------------------
# 类别 4：wait_ready 超时路径，本机可验（用假的 SSH/Console 双假对象）。
# ----------------------------------------------------------------------


class _FakeCheckResult:
    def __init__(self, rc: int):
        self.rc = rc
        self.ok = rc == 0

    def check(self, what: str):
        if self.rc != 0:
            raise RuntimeError(f"{what} 失败（rc={self.rc}）")
        return self


class _FakeSSH:
    """假 SSH：`-check` 与 `systemctl is-active` 的返回值可控。"""

    def __init__(self, *, check_rc: int, is_active_output: str):
        self._check_rc = check_rc
        self._is_active_output = is_active_output
        self.commands: list[str] = []

    def run(self, cmd: str, timeout: int = 120):
        self.commands.append(cmd)
        if "-check" in cmd:
            return _FakeCheckResult(self._check_rc)
        if "is-active" in cmd:
            return _FakeStdoutResult(self._is_active_output)
        raise AssertionError(f"未预期的命令: {cmd!r}")

    def sudo(self, cmd: str, timeout: int = 120):
        """实测订正：`_check_readiness` 判据 1 改成了 `ssh.sudo(...)`（provision
        任务发现 `/etc/lober/lober.yaml` 是 root:lober 640，非 sudo 身份读不到，
        `-check` 会在读配置这一步就被拒绝而不是真的验证配置内容）。真实 SSH.sudo
        只是 `self.run(f"sudo {cmd}")` 的薄包装（见 ssh.py），本假对象同样委托给
        run，保持与真实契约同一个行为形状，不需要另起一套判断逻辑。"""
        return self.run(f"sudo {cmd}", timeout=timeout)


class _FakeStdoutResult:
    def __init__(self, stdout: str):
        self.stdout = stdout
        self.rc = 0
        self.ok = True

    def check(self, what: str):
        return self


class _FakeConsole:
    """假 Console：未认证登录请求可控为抛异常（模拟连接错误）或返回 4xx。"""

    def __init__(self, *, raises: Exception | None):
        self._raises = raises
        self.called = False

    def unauthenticated_login_status(self) -> int:
        self.called = True
        if self._raises is not None:
            raise self._raises
        return 401


def test_wait_ready_timeout_names_which_criterion_failed_check():
    """类别 4a：`-check` 一直非零时超时，消息必须点名是 check 这一条没过，
    不能只说「超时」。"""
    ssh = _FakeSSH(check_rc=1, is_active_output="active\n")
    console = _FakeConsole(raises=None)

    with pytest.raises(Exception) as exc_info:
        deploy.wait_ready(ssh, console, timeout=1)

    message = str(exc_info.value)
    assert "-check" in message or "check" in message


def test_wait_ready_timeout_names_which_criterion_failed_is_active():
    """类别 4b：`-check` 过了但 `systemctl is-active` 一直不是 active，
    消息必须点名是这一条没过——这是「进程起来但端口没监听」的典型信号。"""
    ssh = _FakeSSH(check_rc=0, is_active_output="activating\n")
    console = _FakeConsole(raises=None)

    with pytest.raises(Exception) as exc_info:
        deploy.wait_ready(ssh, console, timeout=1)

    message = str(exc_info.value)
    assert "is-active" in message or "active" in message


def test_wait_ready_timeout_names_which_criterion_failed_login_probe():
    """类别 4c：前两条都过了，但未认证登录探测一直连接失败（不是拿到
    4xx，是拿不到响应），消息必须点名是登录探测这一条没过。"""
    ssh = _FakeSSH(check_rc=0, is_active_output="active\n")
    console = _FakeConsole(raises=ConnectionError("connection refused"))

    with pytest.raises(Exception) as exc_info:
        deploy.wait_ready(ssh, console, timeout=1)

    message = str(exc_info.value)
    assert "login" in message or "登录" in message or "auth" in message


def test_wait_ready_succeeds_when_all_three_criteria_pass():
    """三条判据全过时 wait_ready 正常返回，不抛异常。"""
    ssh = _FakeSSH(check_rc=0, is_active_output="active\n")
    console = _FakeConsole(raises=None)

    deploy.wait_ready(ssh, console, timeout=1)

    assert console.called is True


# ----------------------------------------------------------------------
# 类别 3、5：render_config 产出被 -check 接受、create_schema 幂等。
# 都需要真实 VM-S（render_config 的产出要喂给远程 lober -check；
# create_schema 要真的连库执行 DDL），标 needs_vm。
# ----------------------------------------------------------------------


@pytest.mark.needs_vm
def test_render_config_output_accepted_by_lober_check(tmp_path):
    """类别 3：render_config 产出的 YAML 推到 VM-S 后能被
    `lober -config ... -check` 接受（退出 0）。需要一台已推好二进制的
    VM-S，标 needs_vm。

    实测订正（provision 任务第一次真连 VM-S 跑此测试时发现）：本用例原来
    没有先建 schema，直接把指向 `lobertest_render_check` 这个 schema 名的
    YAML 推上去跑 `-check`。`render_config` 的职责只是产出 YAML 正文，
    不建 schema——`create_schema` 是独立的一步（`deploy.create_schema` 的
    文档已经写明"lober 不会自动创建 search_path 指向的 schema"），调用方
    必须自己先建。原用例漏了这一步，实测报
    `ERROR: no schema has been selected to create in (SQLSTATE 3F000)`
    ——AutoMigrate 阶段找不到任何有权限的 schema，`-check` 因此 rc=1，
    但这个失败原因与"render_config 产出的字段形状对不对"这条本用例真正
    要验证的东西无关，是用例自己漏了前置步骤导致的假红。补上
    `create_schema`/`teardown_schema` 后，本用例才是在验证类别 3 本来
    要验证的内容。
    """
    from lobertest.config import load
    from lobertest.ssh import SSH

    settings = load(env={})
    schema_name = "lobertest_render_check"
    yaml_body = deploy.render_config(schema=schema_name, users=[])

    with SSH(settings) as ssh:
        try:
            deploy.create_schema(ssh, schema_name)
            remote_config = "/tmp/lobertest-render-check.yaml"
            _push_text(ssh, yaml_body, remote_config)
            result = ssh.sudo(f"/opt/lober/bin/lober -config {remote_config} -check")
        finally:
            deploy.teardown_schema(ssh, schema_name)

    assert result.rc == 0


@pytest.mark.needs_vm
def test_create_schema_called_twice_is_idempotent():
    """类别 5：create_schema 对同一个 schema 名调用两次，第二次不报错
    （`CREATE SCHEMA IF NOT EXISTS` 语义）。需要真实连库，标 needs_vm。"""
    from lobertest.config import load
    from lobertest.ssh import SSH

    settings = load(env={})
    schema_name = "lobertest_idempotent_check"

    with SSH(settings) as ssh:
        try:
            deploy.create_schema(ssh, schema_name)
            deploy.create_schema(ssh, schema_name)  # 第二次不应报错
        finally:
            deploy.teardown_schema(ssh, schema_name)


# ----------------------------------------------------------------------
# 辅助函数
# ----------------------------------------------------------------------


def _find_repo_root():
    """从本文件出发向上找到仓库根（`cmd/lober` 存在的那一层）。"""
    from pathlib import Path

    here = Path(__file__).resolve()
    for parent in here.parents:
        if (parent / "cmd" / "lober").is_dir() and (parent / "go.mod").is_file():
            return parent
    raise RuntimeError(f"从 {here} 向上找不到仓库根（cmd/lober + go.mod）")


def _make_broken_copy(tmp_path, real_repo_root):
    """把 `cmd/lober` 复制到临时目录，塞一个编译不过的 .go 文件进去。

    只复制 `cmd/lober` 与 `go.mod`/`go.sum`（不复制整个仓库——那会很慢，
    且 cross_compile 只编译 `./cmd/lober` 这一个 package，不需要其余源码
    在磁盘上物理存在于同一份副本里，因为 go.mod 里的 module 路径与
    GOFLAGS=-mod=mod 会让 go 走 module cache 里的其余依赖）。

    这个函数不改动 real_repo_root 本身，只在 tmp_path 下产出一份独立
    副本；调用方负责在用完后 shutil.rmtree 它。
    """
    import shutil as _shutil
    from pathlib import Path

    broken_root = tmp_path / "broken-repo"
    broken_root.mkdir()

    _shutil.copy2(real_repo_root / "go.mod", broken_root / "go.mod")
    if (real_repo_root / "go.sum").is_file():
        _shutil.copy2(real_repo_root / "go.sum", broken_root / "go.sum")

    # cross_compile 只编译 ./cmd/lober，但 go build 仍需能解析整个 module
    # 图——最简单可靠的做法是整仓复制，用 ignore 排除大目录，避免复制
    # .git/ 与已有的 tests/lobertest（含 .venv）拖慢速度。
    def _ignore(dirpath, names):
        ignored = set()
        for name in names:
            if name in (".git", ".venv", "__pycache__", ".pytest_cache", "node_modules"):
                ignored.add(name)
        return ignored

    _shutil.rmtree(broken_root)
    _shutil.copytree(real_repo_root, broken_root, ignore=_ignore)

    # 塞一个编译不过的文件进 cmd/lober：故意的语法错误，任何 Go 版本、
    # 任何 GOOS 都编译不过，不依赖 GOOS=linux 特有的行为——这里只需要
    # 验证「cross_compile 失败时消息带 go 的完整 stderr」，不需要复现
    # GOOS 专属的那类错误。
    #
    # 文件名不能以 `_test.go` 结尾：那是 Go 工具链保留的测试文件后缀，
    # `go build`（非 `go test`）会把这类文件从包的 GoFiles 里静默排除
    # ——实测验证过：塞一个 `zz_broken_for_test.go` 进去，`go build` 仍
    # 退出 0，`go list -f '{{.GoFiles}}'` 确认该文件根本不在编译清单里。
    # 这正是本仓库既有教训的同一类陷阱（AutoMigrate 索引漏字段静默降级
    # 那条备忘录）：错误的假设不报错，只是悄悄什么都没做。
    broken_file = broken_root / "cmd" / "lober" / "zz_broken_build.go"
    broken_file.write_text(
        "package main\n\nfunc thisDoesNotCompile( {\n",
        encoding="utf-8",
    )

    return broken_root


def _push_text(ssh, text: str, remote_path: str) -> None:
    """把一段文本写到本地临时文件再 push 到远端——SSH.push 只接受本地
    文件路径，没有直接推字符串的方法。"""
    import tempfile
    from pathlib import Path

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False, encoding="utf-8") as f:
        f.write(text)
        local_path = Path(f.name)
    try:
        ssh.push(local_path, remote_path)
    finally:
        local_path.unlink(missing_ok=True)
