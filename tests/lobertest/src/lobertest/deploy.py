"""部署闸门：一条命令把当前源码变成 VM-S 上跑着的 lober。

交叉编译 + 建 schema + 推二进制与配置 + 重启 + 等就绪。`cross_compile` 是
**第一道闸门**：它失败时调用方必须让整个 session 立刻停止，不跑任何用例
——本模块只负责在失败时把 go 的完整 stderr 带进异常消息，「session 立刻
停止」是 conftest.py 里 `deployed` fixture（任务 8）的职责，不是本模块的。

没有健康检查端点，`wait_ready` 用三条判据组合判断就绪：
`lober -check` 退出 0、`systemctl is-active` 为 active、未认证登录拿到
4xx 而非连接错误。三条全过才算就绪，只看 `systemctl is-active` 不够
——`Type=simple` 下进程起来不代表端口在听。
"""

from __future__ import annotations

import os
import subprocess
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from lobertest.ssh import SSH

# 固定测试密码，与 api.py 的 _TEST_PASSWORD 取相同的值——task 6 生成的
# 测试账号与本模块渡出的 lober.yaml 必须用同一份密码，否则 Console.token()
# 登录不了自己刚建好的账号。两处独立记录同一个事实是刻意的（api.py 顶部
# 注释已解释过：不让 api.py 反向依赖 deploy.py）。
TEST_PASSWORD = "LoberTest#2026Automation"


@dataclass(frozen=True)
class TestUser:
    """一个测试账号的最小描述，喂给 render_config 产出 `auth.users` 段。

    任务 6 会正式定义账号生成逻辑（含 bcrypt 哈希计算）；本任务只需要一个
    足够表达「name + 哈希 + 角色」的形状，让 render_config 的契约今天就能
    独立测试。任务 6 落地时如果需要更多字段，直接扩展这个类型即可——它是
    叶子数据结构，不被其它模块的公开 API 依赖。
    """

    name: str
    bcrypt_hash: str
    roles: list[str]


class _ConsoleLoginProbe(Protocol):
    """wait_ready 第三条判据需要的最小接口：探测未认证登录的响应。

    真实调用点传入 lobertest.api.Console 的一个方法（或适配包装），测试
    用假对象。用 Protocol 而非直接依赖 Console，避免 deploy.py 反过来
    import api.py——两者目前没有依赖关系，不该因为这一个用法引入。
    """

    def unauthenticated_login_status(self) -> int: ...


def cross_compile(repo_root: Path, out: Path) -> Path:
    """交叉编译 `./cmd/lober` 到 linux/amd64，产物写到 `out`。

    这是整个工程的第一道闸门：失败时抛异常，消息带 go 的完整 stderr——
    `5089a86` 那类 GOOS 隐式约束错误就是靠这段输出定位的，不能只说
    「编译失败」。
    """
    out.parent.mkdir(parents=True, exist_ok=True)
    proc = subprocess.run(
        ["go", "build", "-o", str(out), "./cmd/lober"],
        cwd=str(repo_root),
        env=_cross_compile_env(),
        capture_output=True,
        text=True,
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"交叉编译失败（GOOS=linux GOARCH=amd64，退出码 {proc.returncode}）\n"
            f"go stderr:\n{proc.stderr}"
        )
    return out


def _cross_compile_env() -> dict[str, str]:
    """构造交叉编译需要的环境变量：继承当前环境（保留 PATH 等），叠加
    GOOS/GOARCH。不能用空字典重开一个环境——那会丢失 PATH，导致找不到
    go 本身。"""
    env = dict(os.environ)
    env["GOOS"] = "linux"
    env["GOARCH"] = "amd64"
    return env


def create_schema(ssh: SSH, name: str) -> None:
    """在 VM-S 的 Postgres 里建 schema。lober 不会自动创建
    `search_path` 指向的 schema（`internal/config/config.go` 的
    `Postgres.SearchPath` 字段注释明确写了这一点），所以本函数必须自己建。

    用 `CREATE SCHEMA IF NOT EXISTS` 做到幂等：同名调用两次，第二次不
    报错。
    """
    ssh.sudo(
        f'psql -U postgres -c "CREATE SCHEMA IF NOT EXISTS {_quote_ident(name)};"'
    ).check(f"建 schema {name}")


def teardown_schema(ssh: SSH, name: str) -> None:
    """删除 create_schema 建的 schema，级联删除其中全部对象。"""
    ssh.sudo(
        f'psql -U postgres -c "DROP SCHEMA IF EXISTS {_quote_ident(name)} CASCADE;"'
    ).check(f"删 schema {name}")


def _quote_ident(name: str) -> str:
    """给 SQL 标识符加双引号转义，防止 schema 名里含特殊字符时被解析
    错误或注入。测试工程内的 schema 名都是硬编码字面量，这里转义是纵深
    防御而非应对不可信输入。"""
    return '"' + name.replace('"', '""') + '"'


def render_config(*, schema: str, users: list[TestUser]) -> str:
    """产出 lober.yaml 正文。

    `gateway.listen` 与 `console.listen` 绑 `127.0.0.1`——nginx 是唯一
    对外入口，两个 listener 不直接暴露给装机 VLAN 或公网。`search_path`
    填本轮的 schema 名，做到「每轮测试一个独立 schema」的隔离。

    字段形状对齐 `cmd/lober/main_test.go` 里 `writeMinimalValidConfigFile`
    产出的样例（那是本仓库唯一一份「已被 -check 验证能通过」的最小配置），
    而不是自己猜一份——猜的字段名对不上会让 render_config 的产出被
    `-check` 拒绝，而这条契约本身要到有 VM 才能验证（类别 3，标
    needs_vm），本机跑不出这种字段名打错的红灯。
    """
    users_yaml = _render_users(users)
    return f"""\
gateway:
  listen: "127.0.0.1:8180"
  gateway_base: "http://127.0.0.1:8180"
  artifact_base: "http://127.0.0.1:8181"
  syslog_listen: "127.0.0.1:8614"
  root_ssh_public_keys: []
console:
  listen: "127.0.0.1:8190"
  token_secret: "lobertest-token-secret"
store:
  postgres:
    host: "127.0.0.1"
    port: 5432
    user: "lober_test"
    password: ""
    dbname: "lober_test"
    sslmode: "disable"
    search_path: "{schema}"
  artifact_path: "/srv/lober/artifacts"
  log_path: "/var/lib/lober/log"
auth:
  users:
{users_yaml}
provider:
  ipam: "static"
  dns: "noop"
  resolver: "rule-based"
  disk_policy: "largest"
  heartbeat_mode: "off"
"""


def _render_users(users: list[TestUser]) -> str:
    """把 TestUser 列表渡成 `auth.users` 段的缩进 YAML 文本。空列表时
    产出一个占位 admin 账号——lober 启动校验大概率要求至少一个用户，
    渡出空列表会让 `-check` 因为「无法登录」这个不相关的原因失败，
    掩盖了本函数真正要验证的东西（schema 隔离与字段形状）。
    """
    if not users:
        users = [
            TestUser(
                name="lobertest-admin",
                bcrypt_hash="$2a$10$7EqJtq98hPqEX7fNZaFWoOhi6vjuLpk3wJ0Qe0KcJvJ8G7ZbtG9Ia",
                roles=["admin"],
            )
        ]

    lines = []
    for user in users:
        roles = ", ".join(f'"{role}"' for role in user.roles)
        lines.append(f'    - name: "{user.name}"')
        lines.append(f'      bcrypt_hash: "{user.bcrypt_hash}"')
        lines.append(f"      roles: [{roles}]")
    return "\n".join(lines)


def deploy(ssh: SSH, binary: Path, config: str) -> None:
    """推二进制 + 推配置 + 重启服务。不在本函数内等就绪——那是
    wait_ready 的职责，调用方按需组合。"""
    ssh.sudo("systemctl stop lober || true").check("停止 lober")
    ssh.push(binary, "/opt/lober/bin/lober")
    ssh.sudo("chmod +x /opt/lober/bin/lober").check("赋予二进制可执行权限")

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False, encoding="utf-8") as f:
        f.write(config)
        local_config_path = Path(f.name)
    try:
        ssh.push(local_config_path, "/etc/lober/lober.yaml")
    finally:
        local_config_path.unlink(missing_ok=True)

    ssh.sudo("systemctl daemon-reload").check("重载 systemd unit")
    ssh.sudo("systemctl restart lober").check("重启 lober")


def wait_ready(ssh: SSH, console: _ConsoleLoginProbe, timeout: int = 60) -> None:
    """轮询三条就绪判据直到全部通过，或超时。

    三条判据缺一不可：
      1. `lober -config /etc/lober/lober.yaml -check` 退出 0
      2. `systemctl is-active lober` 输出 `active`
      3. 未认证 `POST /console/v1/auth/login` 拿到 4xx（不是连接错误）

    超时时异常消息必须点名是哪一条没过，不能只说「超时」——三条判据
    分别对应「配置能被解析」「进程活着」「端口在听且认证生效」三种不同
    的失败原因，笼统的超时消息定位不到该往哪个方向查。
    """
    deadline = time.monotonic() + timeout
    last_failure = "未知"

    while True:
        last_failure = _check_readiness(ssh, console)
        if last_failure is None:
            return  # 三条全过

        if time.monotonic() >= deadline:
            raise TimeoutError(
                f"等待 lober 就绪超时（{timeout}s）。最后一次失败原因：{last_failure}"
            )
        time.sleep(1)


def _check_readiness(ssh: SSH, console: _ConsoleLoginProbe) -> str | None:
    """跑一轮三条判据检查，全过返回 None，否则返回点名哪一条没过的
    描述。判据 2、3 依赖判据 1、2 先过——配置解析不了或进程没起，去查
    端口监听状态没有意义，因此三条按顺序短路检查而非并行跑三次远程
    命令。
    """
    check_result = ssh.run("/opt/lober/bin/lober -config /etc/lober/lober.yaml -check")
    if check_result.rc != 0:
        return f"判据 1（lober -check）未过：rc={check_result.rc}"

    is_active_output = ssh.run("systemctl is-active lober").stdout.strip()
    if is_active_output != "active":
        return f"判据 2（systemctl is-active）未过：当前状态 {is_active_output!r}"

    try:
        status = console.unauthenticated_login_status()
    except Exception as exc:
        return f"判据 3（未认证登录探测）未过：连接失败 {exc}"
    if not (400 <= status < 500):
        return f"判据 3（未认证登录探测）未过：期望 4xx，实际 {status}"

    return None


def build_web(ssh: SSH, repo_root: Path) -> Path:
    """构建前端 dist。任务 10 才用，本任务不实现。"""
    raise NotImplementedError("build_web 属于任务 10（L3 前端层），本任务未实现")
