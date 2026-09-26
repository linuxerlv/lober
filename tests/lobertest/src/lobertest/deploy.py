"""部署闸门：一条命令把当前源码变成 VM-S 上跑着的 lober。

交叉编译 + 建 schema + 推二进制与配置 + 重启 + 等就绪。`cross_compile` 是
**第一道闸门**：它失败时调用方必须让整个 session 立刻停止，不跑任何用例
——本模块只负责在失败时把 go 的完整 stderr 带进异常消息，「session 立刻
停止」是 conftest.py 里 `deployed` fixture（任务 8）的职责，不是本模块的。

没有健康检查端点，`wait_ready` 用三条判据组合判断就绪：
`lober -check` 退出 0、`systemctl is-active` 为 active、未认证登录拿到
4xx 而非连接错误。三条全过才算就绪，只看 `systemctl is-active` 不够
——`Type=simple` 下进程起来不代表端口在听。

测试账号（`TestUser`/`build_test_users`）的用户名前缀与明文密码从
`lobertest.config` import（`TEST_USER_PREFIX`、`TEST_PASSWORD`），不在本
文件内联字面量——`api.py` 登录时用的是同一份常量。哈希经
`go run ./scripts/mkuser` 生成，不在 Python 侧装 bcrypt 库计算：哈希必须
由 lober 自己的工具产出，否则「测试账号能登录」证明不了「运维按文档建的
账号能登录」（任务 6）。
"""

from __future__ import annotations

import os
import subprocess
import tempfile
import time
from dataclasses import dataclass
from pathlib import Path
from typing import Protocol

from lobertest.config import (
    TEST_PASSWORD,
    TEST_PG_PASSWORD,
    TEST_ROOT_SSH_PUBLIC_KEY,
    TEST_USER_PREFIX,
    TEST_ROLES,
)
from lobertest.ssh import SSH


@dataclass(frozen=True)
class TestUser:
    """一个测试账号的最小描述，喂给 render_config 产出 `auth.users` 段。

    保留 `bcrypt_hash` 而非明文 `password`：写进配置文件的是哈希，明文只
    在调 `mkuser` 时用一次，不该存进每条用户记录里（任务 6）。
    """

    name: str
    bcrypt_hash: str
    roles: list[str]


def bcrypt_hash(repo_root: Path, password: str) -> str:
    """调 `go run ./scripts/mkuser -user <name> -stdin` 产出密码的 bcrypt
    哈希。不在 Python 侧装 bcrypt 库实现——哈希必须由 lober 自己的工具
    生成，否则「测试账号能登录」证明不了「运维按文档建的账号能登录」。

    密码经 `stdin=PIPE` 喂给子进程，绝不出现在命令行参数列表里——那会
    进 `ps` 输出（本函数风险清单明确要求）。`-user` 的值只影响 mkuser
    打到 stderr 的提示文本，不影响哈希本身，这里固定传 "lobertest"，
    调用方不需要为了算一个哈希而先决定账号名。

    显式指定 `encoding="utf-8"`（不能让 `text=True` 走平台默认编码）：
    mkuser 的 stderr 提示信息是中文，本机默认区域设置是 GBK，`subprocess`
    用平台默认编码解码子进程输出会在读 stderr 的后台线程里直接
    `UnicodeDecodeError`——这不是子进程真的失败，而是 Python 侧解码
    环节先炸了，且发生在后台线程，主线程看到的是一个误导性的空 stderr。
    """
    proc = subprocess.run(
        ["go", "run", "./scripts/mkuser", "-user", "lobertest", "-stdin"],
        cwd=str(repo_root),
        input=f"{password}\n",
        capture_output=True,
        text=True,
        encoding="utf-8",
    )
    if proc.returncode != 0:
        raise RuntimeError(
            f"mkuser 生成哈希失败（退出码 {proc.returncode}）\nstderr:\n{proc.stderr}"
        )
    return proc.stdout.strip()


def build_test_users(repo_root: Path) -> list[TestUser]:
    """为五个角色各生成一个测试账号，name 为 `f"{TEST_USER_PREFIX}{role}"`。

    五个角色取自 `config.TEST_ROLES`（权威定义见 `internal/console/rbac.go
    :19-24`），全部账号共用同一份 `config.TEST_PASSWORD`，哈希各自单独
    调一次 `bcrypt_hash`（bcrypt 加盐，同一密码每次调用产出不同哈希，这不
    是缺陷）。

    `TestUser` 是 role -> 单角色的映射（每个账号恰好一个角色），不是
    role 的任意组合——这保证了 `hook-author` 与 `operator`（互不重叠的
    两个角色，rbac.go:45-47）不会被同一个测试账号同时持有，RBAC 边界
    测试才测得出来。

    没有做成模块级静态列表（计划契约原文的 `TEST_USERS: list[TestUser]`）
    是因为哈希计算需要 `repo_root`，而 `deploy.py` 里其它需要 repo_root
    的函数（`cross_compile`、`build_web`）都是「调用方传入」而非「模块内
    自行发现」，本函数遵循同一约定，不额外引入一种发现方式（详见任务
    报告的偏离说明）。
    """
    return [
        TestUser(
            name=f"{TEST_USER_PREFIX}{role}",
            bcrypt_hash=bcrypt_hash(repo_root, TEST_PASSWORD),
            roles=[role],
        )
        for role in TEST_ROLES
    ]


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

    实测订正（provision 任务的 provision.sh 第一次真的传相对路径进来时
    发现）：`out` 必须先 `.resolve()` 成绝对路径再交给 `go build -o`。
    `go build` 子进程的 `cwd` 是 `repo_root`，不是调用方 Python 进程自己
    的当前工作目录——若 `out` 是相对路径（比如 `.tmp/lober-linux-amd64`），
    `-o` 参数会相对 `repo_root` 解析，产物落在 `{repo_root}/.tmp/...`，
    而不是调用方以为的"相对我自己 cwd 的 .tmp/..."。这在只用绝对路径
    调用时（任务 5 原来的测试用例、本任务前面的手工验证）不会暴露——
    两者都恰好传了绝对路径——直到 provision.sh 传入相对路径
    `.tmp/lober-linux-amd64` 才第一次真正触发：脚本自己已经 `cd` 到
    `tests/lobertest`，产物却出现在仓库根的 `.tmp/` 下，函数不报错、
    返回值看起来正常，只是产物在一个调用方没预料到的位置——这正是最
    隐蔽的一类 bug："看起来成功了，只是结果去了别的地方"。`resolve()`
    让 `out` 的含义不再依赖子进程的 cwd 是什么，调用方传绝对路径还是
    相对路径都产出同一个结果。
    """
    out = out.resolve()
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

    实测订正一（provision 任务发现，原实现从未连过真实 VM-S）：命令必须
    是 `sudo -u postgres psql ...`，不能是 `sudo psql -U postgres ...`。
    这台 VM-S 的 pg_hba.conf 对本机 unix socket 连接用的是 `peer` 认证
    ——peer 校验的是**发起连接的操作系统用户名**，与 psql 的 `-U` 参数
    无关。`sudo psql -U postgres` 下，实际发起连接的进程属主是 root（sudo
    默认切到 root，不是 postgres），`-U postgres` 只是告诉 psql "以
    postgres 这个数据库角色身份连"，两者不是一回事——peer 认证看到的是
    "谁在 connect()"，不是"你声称是谁"。报错现场是 `FATAL: Peer
    authentication failed for user "postgres"`，容易误读成"postgres
    这个角色密码错了"，但这条 peer 规则根本不检查密码，检查的是 OS 用户
    与数据库角色名是否一致——root != postgres，无论 -U 怎么写都会失败。
    `sudo -u postgres psql` 才会让发起连接的进程属主真的是 postgres，与
    peer 规则要求的用户名一致。

    实测订正二：显式指定 `-d lober_test`（固定数据库名，不是 `name`
    本身）。`name` 是 schema 名，随每轮测试变化（"每轮一个独立 schema"，
    设计文档 §3）；`render_config` 里的 `store.postgres.dbname` 是硬编码
    字面量 `"lober_test"`，与 schema 名是两个独立的概念，不能假设两者
    相同——第一次实现时误以为 `name` 同时是 db 名与 schema 名，实测直接
    暴露：`psql -d lobertest` 报 `database "lobertest" does not exist`，
    因为真正建出来的数据库叫 lober_test（下划线），根本没有一个叫
    lobertest 的数据库。`CREATE SCHEMA` 建的 schema 归属于"当前连接的
    数据库"，必须连到 lober.yaml 的 store.postgres.dbname 指向的那个
    数据库，lober 进程才能看到这个 schema。

    实测订正三：必须带 `AUTHORIZATION lober_test`，不能建成默认属主
    （执行者 postgres）的 schema。裸 `CREATE SCHEMA IF NOT EXISTS lobertest`
    建出的 schema 属主是 postgres，`\\dn+` 核实其 Access privileges 一栏
    为空——即"除属主外任何角色零权限"，lober 进程实际连接用的角色是
    lober_test（render_config 的 store.postgres.user），它对这个 schema
    连 USAGE 都没有。表现是 AutoMigrate 阶段 `CREATE TABLE "tasks" (...)`
    报 `ERROR: no schema has been selected to create in (SQLSTATE 3F000)`
    ——这条报错字面意思是"没有可用于创建对象的 schema"，根源是
    search_path 里唯一配置的 schema（lobertest）因为连接角色没有权限
    访问，被 Postgres 从有效 search_path 中静默剔除，剔除后 search_path
    变成空，"CREATE TABLE" 不知道该建在哪。`AUTHORIZATION lober_test`
    让 lober_test 直接成为该 schema 的属主，属主对自己名下的 schema
    天然有完整权限，不需要额外一条 GRANT。
    """
    ssh.sudo(
        f'-u postgres psql -d lober_test '
        f'-c "CREATE SCHEMA IF NOT EXISTS {_quote_ident(name)} AUTHORIZATION lober_test;"'
    ).check(f"建 schema {name}")


def teardown_schema(ssh: SSH, name: str) -> None:
    """删除 create_schema 建的 schema，级联删除其中全部对象。

    与 create_schema 同两个实测订正：`sudo -u postgres`（peer 认证）+
    连到固定数据库 `lober_test`（不是 name 本身，见 create_schema 的
    详细说明）。
    """
    ssh.sudo(
        f'-u postgres psql -d lober_test '
        f'-c "DROP SCHEMA IF EXISTS {_quote_ident(name)} CASCADE;"'
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
  root_ssh_public_keys:
    - "{TEST_ROOT_SSH_PUBLIC_KEY}"
console:
  listen: "127.0.0.1:8190"
  token_secret: "lobertest-token-secret"
store:
  postgres:
    host: "127.0.0.1"
    port: 5432
    user: "lober_test"
    password: "{TEST_PG_PASSWORD}"
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
    wait_ready 的职责，调用方按需组合。

    实测订正（provision 任务发现，原实现从未连过真实 VM-S）：不能直接
    `ssh.push` 到 `/opt/lober/bin/lober` 或 `/etc/lober/lober.yaml`。
    `ssh.push` 走 SFTP，以 SSH 登录用户（`debian`）的身份写文件，而这两个
    目标目录都是 `root:root 755`——`debian` 用户对它们既无写权限也不在
    能写的组里。实测报 `PermissionError: [Errno 13] Permission denied`。

    没有改成"给 debian 加权限"（比如 chown /opt/lober、/etc/lober 给
    debian）：`ssh.push` 的文档约定是"目标目录不存在时不自动创建——静默
    建目录会掩盖路径写错"，这条约定背后的意图是 push 应该是一个诚实反映
    真实权限边界的操作，放宽系统目录权限会让"测试环境能直接写系统路径"
    这件事本身变得不真实（生产部署里运维也不会给 SSH 登录用户开
    /opt、/etc 的写权限）。

    改成"先推到 debian 可写的临时路径，再 sudo mv 到位"：这是标准的
    "非特权用户推送到特权路径"模式（chmod/chown 无法隔空作用于 SFTP
    正在写的临时文件，必须落盘后再用 sudo 挪动）。mv 而不是 cp+rm：mv
    在同一文件系统内是原子的，避免"文件已存在但内容只写了一半"这种
    半推送状态被其它进程（比如刚好在这一刻探测 is-active 的另一个测试）
    看到。
    """
    ssh.sudo("systemctl stop lober || true").check("停止 lober")

    staging_binary = "/tmp/lobertest-deploy-lober-binary"
    ssh.push(binary, staging_binary)
    ssh.sudo(f"mv {staging_binary} /opt/lober/bin/lober").check("移动二进制到位")
    ssh.sudo("chmod +x /opt/lober/bin/lober").check("赋予二进制可执行权限")

    with tempfile.NamedTemporaryFile(mode="w", suffix=".yaml", delete=False, encoding="utf-8") as f:
        f.write(config)
        local_config_path = Path(f.name)
    try:
        staging_config = "/tmp/lobertest-deploy-lober-config.yaml"
        ssh.push(local_config_path, staging_config)
        ssh.sudo(f"mv {staging_config} /etc/lober/lober.yaml").check("移动配置到位")
        # 配置文件权限沿用既有约定（root:lober 640，见 /etc/lober/lober.yaml
        # 实测的既有权限）：lober 进程以 lober 用户运行，需要能读但不需要
        # 世界可读（配置里含数据库密码与 token_secret）。mv 之后文件的属主
        # 变成 root（sudo 执行者），组保持临时文件创建时的属组，显式
        # chown/chmod 一次以保证结果确定，不依赖临时文件当时恰好是什么权限。
        ssh.sudo("chown root:lober /etc/lober/lober.yaml").check("设置配置文件属组")
        ssh.sudo("chmod 640 /etc/lober/lober.yaml").check("设置配置文件权限")
    finally:
        local_config_path.unlink(missing_ok=True)

    ssh.sudo("systemctl daemon-reload").check("重载 systemd unit")

    # 实测订正：先 reset-failed 再 restart。lober.service 配了
    # StartLimitIntervalSec=300/StartLimitBurst=5（deploy/systemd/lober.service
    # 的 [Unit] 段），若上一轮部署的配置有问题导致服务在 5 分钟内失败重启
    # 超过 5 次，systemd 会记下"失败次数超限"的状态并拒绝再次启动，报
    # `Start request repeated too quickly`——即使这一轮推的新配置已经修好了
    # 根因，systemd 也不会自动清除这个计数器，`systemctl restart` 会直接
    # 卡在这个拒绝上，看起来像新配置仍然有问题（实测正是这样撞上的：
    # `-check` 手动跑已经 exit 0，但 restart 仍然失败，直到手动
    # reset-failed 才恢复）。reset-failed 幂等——没有失败计数时执行它不是
    # 错误，是空操作，因此可以无条件放在每次 deploy 里，不需要先判断是否
    # 真的命中了限流。
    ssh.sudo("systemctl reset-failed lober || true")
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
    # 实测订正：必须 sudo，不能是 ssh.run（普通用户身份）。
    # /etc/lober/lober.yaml 的权限是 root:lober 640（deploy() 里显式设置，
    # 与生产 systemd 单元里 lober 进程以 lober 用户运行这条约定一致）——
    # SSH 登录用户 debian 不在 lober 组里，普通身份跑 -check 会在读配置
    # 文件这一步就被拒绝，报的 rc 不是"配置有问题"，是"这个身份读不到
    # 配置文件"，两种失败混在同一个 rc!=0 里会误导排查方向。用 sudo 才能
    # 复现 systemd ExecStartPre 实际的读取身份。
    check_result = ssh.sudo("/opt/lober/bin/lober -config /etc/lober/lober.yaml -check")
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
