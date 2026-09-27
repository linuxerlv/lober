"""L2 装机层专属 fixture：独立 schema 的部署、真实目标机、跨跳 SSH。

L1（`tests/l1_api/`）与 L2 共用顶层 `conftest.py` 的 `deployed`/`console`，
两者都指向同一个 `lobertest` schema。L2 不能复用它——真实装机会在
`idx_tasks_sn_active`（同一 SN 只能有一个活跃任务）上与 L1 的任务互相
干扰，且 L2 每个家族要在**磁盘上真实清空重装**，与 L1 纯 API 断言的
生命周期不是同一件事。所以本文件建立一套独立的 L2 部署：同一台
VM-S、同一个 lober 二进制，但换一个 schema、重新渲染配置、重启服务。

这是「每个家族一个独立 schema」这条计划要求在只做一个家族（RHEL BIOS，
最小成功线）时的落地——contract 留了 schema 参数化的余地，但本任务只
实例化一次。

目标机网络位置的关键事实（provision 阶段确认，写在这里因为下面的
`ssh_into_target` 直接依赖它）：装机 VLAN（10.20.20.0/24）只有 VM-S
自己的 enp0s3 网卡在这个二层域里，Windows 宿主与这个网段之间没有路由
（SLIRP 是用户态 NAT，只转发 hostfwd 声明过的具体端口，不路由整个子网）。
因此目标机装完后不能被 Windows 宿主直接 SSH——必须先 SSH 到 VM-S，
再从 VM-S 内部 SSH 到目标机（跳板，两跳）。这与 VM-S 本身怎么连
（tests/lobertest/src/lobertest/ssh.py 的 `SSH` 类，固定连
`settings.ssh_host:ssh_port`）是两件独立的事，不合并成一个类——`SSH`
类的契约（任务 4）是「VM-S 的 SSH 通道」，不该为了适配跳板场景改变
语义或新增分支参数。
"""

from __future__ import annotations

import subprocess
import time
from contextlib import contextmanager
from dataclasses import dataclass
from pathlib import Path

import pytest

from lobertest import qemu
from lobertest.api import Console
from lobertest.config import Settings, load
from lobertest.deploy import (
    build_test_users,
    cross_compile,
    create_schema,
    deploy,
    render_config,
    wait_ready,
)
from lobertest.ssh import SSH

# L2 专用 schema，与 L1 的 `lobertest` 分开（见文件头文档）。命名带
# `_l2_` 前缀，未来加其它家族时各自建一个同前缀的 schema，不需要改本
# 常量本身。
L2_SCHEMA = "lobertest_l2_rhel_bios"

# 目标机条目 id 与 MAC。MAC 前缀分段沿用 L1 三个文件已建立的惯例
# （test_lifecycle.py 用 01-06、test_admission.py 用 20-29、
# test_rbac.py 用 30-39），L2 取 40-49，避免与任何 L1 文件的 MAC 撞上
# ——`UpsertHost` 按 mac 列做写入冲突判定（不是 sn），撞了会导致同一
# SN 名下出现两行不同 MAC 的脏数据（test_lifecycle.py 的 `_family_mac`
# 文档记录了这条约束的实测根因）。
TARGET_ENTRY_ID = "l2-rhel-bios"
TARGET_MAC = "52:54:00:aa:bb:40"
TARGET_SN = "lobertest-l2-rhel-bios-sn"
TARGET_DISK_SERIAL = "l2-rhel-bios-disk0"
TARGET_DISK_BY_ID = f"/dev/disk/by-id/virtio-{TARGET_DISK_SERIAL}"


@pytest.fixture(scope="session")
def l2_settings() -> Settings:
    """独立于顶层 `settings` fixture 的一份配置副本，`schema` 换成 L2 专属值。

    不复用顶层 `conftest.py` 的 `settings` fixture 再在本文件内改
    `schema` 字段——`Settings` 是 `frozen=True`（任务 1 的设计：配置在
    session 开始时定死，用例不得改），改字段会直接抛
    `FrozenInstanceError`。用 dataclasses.replace 产出一份新实例是唯一
    合法的方式，语义上也更清楚：这是"同一环境事实，换一个 schema"，
    不是"运行期修改配置"。
    """
    import dataclasses

    return dataclasses.replace(load(), schema=L2_SCHEMA)


@pytest.fixture(scope="session")
def l2_deployed(l2_settings: Settings, repo_root: Path, tmp_path_factory: pytest.TempPathFactory) -> Settings:
    """把 L2 专属 schema 部署上去。

    与顶层 `deployed` fixture（任务 8）职责相同但不共用实现——顶层版本
    的"跳过部署"缓存键（`_deployment_already_matches`）比较的是二进制
    sha256 + 配置正文；L2 与 L1 的配置正文天然不同（schema 字段），
    所以即使二进制完全一样，L2 第一次跑一定会触发真实的
    stop/push/restart，这正是期望行为，不需要额外的缓存逃逸逻辑。

    不判断"是否已经是 L2 部署"就跳过——L2 只有一个家族、一次性跑，
    没有 L1 那种"同一 session 内被反复调用"的场景，条件部署带来的复杂度
    在这里不值得。
    """
    binary = cross_compile(repo_root, tmp_path_factory.mktemp("deploy-l2") / "lober-linux-amd64")
    users = build_test_users(repo_root)
    config_text = render_config(schema=l2_settings.schema, users=users)

    with SSH(l2_settings) as ssh:
        create_schema(ssh, l2_settings.schema)
        deploy(ssh, binary, config_text)
        probe_console = Console(l2_settings.console_base_url)
        wait_ready(ssh, probe_console, timeout=60)

    return l2_settings


@pytest.fixture(scope="session")
def l2_console(l2_deployed: Settings) -> Console:
    return Console(l2_deployed.console_base_url, gateway_base_url=l2_deployed.gateway_base_url)


@pytest.fixture(scope="session")
def l2_artifact_reaper(l2_deployed: Settings):
    """L2 版本的制品集磁盘回收，逐字节对应顶层 `conftest.py` 的
    `artifact_reaper`（同样的理由：DELETE 端点对 ready+被引用的制品集
    一律 409，只能在基础设施层删；同样不做孤儿探测，只删自己登记过的
    id）。**不能直接复用顶层那个 fixture**：它内部硬编码连
    `deployed`（L1 的 schema）执行 `DELETE FROM {deployed.schema}...`，
    L2 的制品集记录落在 `l2_deployed.schema`（不同的 schema），指错
    schema 的 DELETE 语句不会报错但什么也删不掉（WHERE id IN (...) 在
    错误的 schema 里天然查不到这些 id），是一种沉默的失效模式。
    """
    collected: list[str] = []
    yield collected.append

    if not collected:
        return
    ids = " ".join(f"'{i}'" for i in collected)
    dirs = " ".join(f"/srv/lober/artifacts/{i}" for i in collected)
    try:
        with SSH(l2_deployed) as ssh:
            ssh.sudo(f"rm -rf {dirs}")
            ssh.run(
                "sudo -u postgres psql -d lober_test -Atc "
                f'"DELETE FROM {l2_deployed.schema}.artifact_sets WHERE id IN ({ids});"'
            )
    except Exception as exc:  # noqa: BLE001 —— teardown 不该掩盖用例的真实结果
        print(f"\n[警告] 回收 {len(collected)} 个 L2 制品集失败，guest 上会留下几 GB：{exc}")


_RHEL_ISO_HOST_PATH = Path("D:/OpenCloudOS-9.6-x86_64-minimal.iso")
_GUEST_ISO_DIR = "/srv/lober/sources"


@pytest.fixture(scope="session")
def l2_rhel_iso_guest_path(l2_deployed: Settings) -> str:
    """RHEL 系 ISO（OpenCloudOS）在 guest 上的路径，只依赖 `l2_deployed`。

    **不复用顶层 `conftest.py` 的 `family_iso_paths` fixture**：那个
    fixture 依赖 `deployed`（L1 的 `lobertest` schema），请求它会连带
    触发 L1 整套部署（交叉编译+推送+重启）先跑一次，即使本次只想跑
    L2——这与三层设计的独立性相悖（L2 应该只被"L1 是否已失败"这个运行
    期事实闸住，不该在 fixture 依赖图上硬耦合到 L1 的部署流程）。guest
    上的推送目标路径与幂等判断逻辑（文件已存在且同大小则跳过）与顶层
    版本一致，是刻意的小段重复而不是共享——两处都只有十几行，共享需要
    抽一个新模块或改变现有模块的依赖方向，为一次推送逻辑不值得。
    """
    if not _RHEL_ISO_HOST_PATH.is_file():
        pytest.skip(f"本机找不到 RHEL 系 ISO：{_RHEL_ISO_HOST_PATH}")

    guest_path = f"{_GUEST_ISO_DIR}/{_RHEL_ISO_HOST_PATH.name}"
    local_size = _RHEL_ISO_HOST_PATH.stat().st_size

    with SSH(l2_deployed) as ssh:
        check = ssh.run(f"stat -c %s {guest_path} 2>/dev/null || echo MISSING")
        if check.stdout.strip() == str(local_size):
            return guest_path

        staging_path = f"/home/debian/lobertest-iso-upload-{_RHEL_ISO_HOST_PATH.name}"
        try:
            ssh.push(_RHEL_ISO_HOST_PATH, staging_path)
        except Exception:
            ssh.run(f"rm -f {staging_path}")
            raise
        ssh.sudo(f"mv {staging_path} {guest_path}").check(f"移动 ISO 到 {guest_path}")
        ssh.sudo(f"chmod 644 {guest_path}").check("设置 ISO 可读权限")
        return guest_path


@pytest.fixture(scope="session")
def vms_ssh(l2_settings: Settings):
    """本文件模块内一条常驻的 VM-S SSH 连接，供 `find_target_ip` 与
    `ssh_into_target` 两跳登录目标机时复用，避免每次都重新握手。

    `l2_settings` 与顶层 `settings` 的 ssh_host/port/user/key_path
    完全相同（只有 schema 字段不同），用哪一份连 VM-S 都一样，这里选
    `l2_settings` 只是让本文件的 fixture 依赖图不再牵出顶层的
    `settings`/`deployed` 链。
    """
    with SSH(l2_settings) as ssh:
        yield ssh


@dataclass(frozen=True)
class TargetVM:
    """一台已启动目标机的把手：entry_id、MAC、串口日志路径、磁盘标识、
    以及装完后发现/登录它用的辅助方法。

    不直接暴露 qemu.Popen 句柄给用例体——用例体只需要"这台机器在跑"、
    "串口日志在这里"、"完事后怎么连上去"三件事，进程句柄本身的生命周期
    由 fixture 的 teardown 管，暴露出去只会诱使用例体自己去 poll/kill，
    绕开 `stop_target` 的幂等与超时保护。

    **常量与辅助函数挂在这个类上而不是作为模块级名字导出**：`tests/`
    目录没有 `__init__.py`（不是一个 Python package），`test_install.py`
    没有合法的 import 路径可以拿到本文件里的模块级函数/常量（`from
    tests.l2_install.conftest import X` 这类写法在这个工程的 pytest
    配置下无法解析——`pyproject.toml` 没有配 `pythonpath`，也没有任何
    既有测试文件这样做过）。pytest 的标准解法是：conftest.py 只通过
    fixture 向测试文件暴露东西，测试文件不 import conftest.py 本身。
    把 `disk_by_id`/`disk_serial` 收进这个类、把 `find_target_ip`/
    `ssh_into_target` 做成它的方法，`test_install.py` 只需要请求
    `target`（已经是 fixture）与 `vms_ssh`（也是 fixture），不需要任何
    跨文件 import。
    """

    entry_id: str
    mac: str
    sn: str
    disk_by_id: str
    disk_serial: str
    serial_log_path: Path
    settings: Settings

    def find_ip(self, vms_ssh: "SSH", *, timeout: int = 180) -> str:
        """见模块级 `_find_target_ip` 的文档；本方法只是绑定
        `self.mac`，避免调用点重复传参。"""
        return _find_target_ip(vms_ssh, self.mac, timeout=timeout)

    def ssh(self, vms_ssh: "SSH", target_ip: str, *, timeout: int = 30):
        """见模块级 `_ssh_into_target` 的文档；本方法只是绑定
        `self.settings.target_root_key_path`，避免调用点重复传参。"""
        return _ssh_into_target(
            vms_ssh,
            target_ip,
            key_path_on_host=self.settings.target_root_key_path,
            timeout=timeout,
        )


@dataclass(frozen=True)
class TargetIdentity:
    """目标机的静态身份：sn/mac/磁盘标识，不含任何运行期状态（没有
    serial_log_path，没有进程句柄）。

    独立于 `TargetVM` 存在的理由：`test_install.py` 的 `create_host`
    调用需要在**目标机启动之前**（`target_launcher()` 被调用之前）就
    知道这些值——sn/mac/byIdPath 必须先出现在服务端的主机清单里，
    目标机的 iPXE 才有东西可以匹配。若这些值只存在于 `TargetVM`
    （由 `launch()` 返回，语义上"机器已经在跑"），调用方就没有办法在
    "机器还没起"这个阶段拿到它们——会重新导致本文件之前踩过的那个
    时序坑（复制字面量到 test_install.py 里，两处独立维护，随时可能
    漂移出不一致的值）。

    本对象作为 fixture 暴露（`target_identity`），供 `test_install.py`
    的 `create_host` 调用与 `target_launcher` 的 `launch()` 内部共用
    同一份取值来源。
    """

    sn: str
    mac: str
    disk_by_id: str
    disk_serial: str


@pytest.fixture(scope="session")
def target_identity() -> TargetIdentity:
    return TargetIdentity(
        sn=TARGET_SN,
        mac=TARGET_MAC,
        disk_by_id=TARGET_DISK_BY_ID,
        disk_serial=TARGET_DISK_SERIAL,
    )


@pytest.fixture
def target_launcher(l2_settings: Settings, target_identity: TargetIdentity):
    """产出一个"起目标机"的动作（可调用对象），而不是一台已经在跑的
    目标机——这是本任务第一次真实装机才暴露的时序缺陷的修复。

    **踩过的坑（本任务实测）**：若直接把"起目标机"做成一个普通 fixture
    （请求方在函数签名里写 `def test_x(target): ...`），pytest 会在
    测试函数体开始执行之前就完成这个 fixture 的 setup——也就是说
    QEMU 目标机会在 `create_host`/`create_task` 还没被调用之前就已经
    启动并开始 PXE 引导。真机的 iPXE 在几秒内就会打到
    `/v1/boot`，而此时任务根本还不存在，服务端正确返回"no task"
    （网关侧行为完全符合 SPEC：查不到任务的机器请求 `/v1/boot`
    "不建任务、不写任何记录"），iPXE 的默认 fallback 是尝试从本地
    SAN 设备 0x80 引导，失败后停在 `iPXE>` 提示符——机器不会自己重试
    `/v1/boot`。第一次真跑观察到的现象正是这个：串口日志里
    `lober: no task for` → sanboot I/O error → 卡死。这不是后端缺陷
    （建任务即授权、查不到任务不建记录都是 SPEC §3.2 明确要求的行为），
    是我自己的 fixture 时序错误。

    修法：**不把"起目标机"挂在函数签名的参数位置**，而是让用例体在
    真正建完任务、确认服务端已经能响应这台 MAC 对应的引导请求之后，
    自己决定"现在开始起 QEMU"这一刻——本 fixture 只提供一个
    `launch()` 可调用对象，用例体在正确的时间点显式调用它。
    teardown（`stop_target`）仍然由本 fixture 的 `finally` 负责，
    不因为改成"延迟启动"就丢失"无论成功与否都要停止目标机"这条
    保证——`finally` 块里用一个可变容器记录"是否真的启动过"，只有
    启动过才需要调 `stop_target`。
    """
    started: dict[str, subprocess.Popen] = {}

    def launch() -> "TargetVM":
        qemu.reset_overlay(TARGET_ENTRY_ID, "20G", settings=l2_settings)

        # 必须在 start_target 之前算好日志路径，且之后不能再调
        # serial_log_path 第二次——start_target 内部已经调用一次决定
        # 往哪个文件重定向 QEMU 输出（qemu.py 第 254 行），若调用方在
        # start_target 返回之后再调一次，此时 QEMU 进程已经打开并在
        # 持续写入这个文件，Windows 下对被其它进程以写模式打开的文件
        # rename 会直接报 PermissionError [WinError 32]（本任务第一轮
        # 真实装机实测踩到，与 POSIX 允许 rename 一个正被打开的文件
        # 不同）。"计算路径"与"清理旧文件"只做这一次，且必须在
        # start_target 启动新进程之前完成。
        log_path = qemu.serial_log_path(TARGET_ENTRY_ID, settings=l2_settings)

        proc = qemu.start_target(
            TARGET_ENTRY_ID,
            firmware="bios",
            mem="3072",
            disk_serial=TARGET_DISK_SERIAL,
            mac=TARGET_MAC,
            settings=l2_settings,
        )
        started["proc"] = proc

        return TargetVM(
            entry_id=TARGET_ENTRY_ID,
            mac=TARGET_MAC,
            sn=TARGET_SN,
            disk_by_id=TARGET_DISK_BY_ID,
            disk_serial=TARGET_DISK_SERIAL,
            serial_log_path=log_path,
            settings=l2_settings,
        )

    try:
        yield launch
    finally:
        if "proc" in started:
            qemu.stop_target(TARGET_ENTRY_ID, proc=started["proc"], settings=l2_settings)


def _find_target_ip(vms_ssh: SSH, mac: str, *, timeout: int = 120) -> str:
    """在 VM-S 的 dnsmasq 租约文件里按 MAC 找目标机的 DHCP 分配地址。

    轮询而非一次性读取：目标机从 QEMU 进程启动到真正发出 DHCP
    request 之间有 BIOS/iPXE 自身的初始化耗时（实测经验：几秒到十几秒
    量级），一次读取拿到的很可能是空结果而不是"确定没有"。

    租约文件路径与格式（`<expiry> <mac> <ip> <hostname> <client-id>`）
    是 dnsmasq 的标准行为，provision 阶段实测确认过路径
    （`/var/lib/misc/dnsmasq.leases`），不是猜测。
    """
    deadline = time.monotonic() + timeout
    mac_lower = mac.lower()
    while time.monotonic() < deadline:
        result = vms_ssh.sudo("cat /var/lib/misc/dnsmasq.leases 2>/dev/null || true")
        for line in result.stdout.splitlines():
            fields = line.split()
            if len(fields) >= 3 and fields[1].lower() == mac_lower:
                return fields[2]
        time.sleep(3)
    raise TimeoutError(
        f"等待 MAC {mac} 出现在 dnsmasq 租约文件超时（{timeout}s）——"
        f"目标机可能还没发出 DHCP 请求，或 PXE 引导链路本身卡住了，"
        f"先查串口日志"
    )


@contextmanager
def _ssh_into_target(
    vms_ssh: SSH,
    target_ip: str,
    *,
    key_path_on_host: Path,
    timeout: int = 120,
):
    """经 VM-S 跳板登进目标机，`with` 块内可用返回的对象跑命令。

    **两跳的必要性见本文件头部文档**：装机 VLAN 从 Windows 宿主不可达。
    这里不新建一个通用的"跳板 SSH"抽象——`lobertest.ssh.SSH` 类的契约
    是"连到 `Settings` 描述的那台主机"，跳板场景的连接目标（目标机
    IP）是运行期才知道的动态值，语义上与"三层都要连的那台固定 VM-S"
    不是同一件事，用独立的轻量实现更清楚，避免为了塞进一个用例就扩大
    `SSH` 类本身的职责边界。

    做法：把目标机私钥的内容通过已连接的 VM-S SSH 会话写到 VM-S 上的
    临时文件（VM-S 本身有 OpenSSH 客户端，是 Debian 系统的标准配置），
    再从 VM-S 内部发起对目标机的 SSH 调用。**做成上下文管理器**（而不是
    直接返回包装对象）是为了保证临时私钥文件一定会被删除——即使调用方
    在 `with` 块内的断言抛出异常，`finally` 仍会执行 `rm`，不会把目标机
    私钥的明文残留在 VM-S 的 `/tmp` 里。

    返回值不是 `lobertest.ssh.SSH` 实例（那个类硬编码连接
    `Settings.ssh_host/port/user/key_path`，语义是"连 VM-S"），而是一个
    在 VM-S 会话内部执行远程命令的轻量包装：调用方拿到的 `.run(cmd)`
    实际上是 `vms_ssh.run("ssh ... target_ip cmd")`，两次网络跳转对
    调用方透明。
    """
    remote_key_path = "/tmp/lobertest-target-key"
    key_content = key_path_on_host.read_text(encoding="utf-8")
    # 用 heredoc 写入而非 sftp.put：私钥内容已经在 Python 进程内存里
    # （read_text 的返回值），走一次现有的 `vms_ssh.run` 命令通道即可，
    # 不需要为了写一个几百字节的文件专门开一次 SFTP 会话。
    write_cmd = (
        f"cat > {remote_key_path} <<'LOBERTEST_KEY_EOF'\n{key_content}\nLOBERTEST_KEY_EOF\n"
        f"chmod 600 {remote_key_path}"
    )
    vms_ssh.run(write_cmd).check("写入目标机私钥到 VM-S 临时文件")

    try:
        yield _JumpSSH(vms_ssh, target_ip, remote_key_path, timeout=timeout)
    finally:
        vms_ssh.run(f"rm -f {remote_key_path}")


class _JumpSSH:
    """经 VM-S 跳板连目标机的最小包装，只暴露 `run`（够用例体验证
    `/etc/os-release` 与 `systemctl is-system-running` 用）。"""

    def __init__(self, vms_ssh: SSH, target_ip: str, remote_key_path: str, *, timeout: int) -> None:
        self._vms_ssh = vms_ssh
        self._target_ip = target_ip
        self._remote_key_path = remote_key_path
        self._timeout = timeout

    def run(self, cmd: str, timeout: int | None = None):
        """在目标机上跑一条命令，经 VM-S 转发。

        `StrictHostKeyChecking=no` + `UserKnownHostsFile=/dev/null`：
        目标机每次 `reset_overlay` 都是全新系统，host key 每轮都不同，
        与顶层 `SSH` 类对 VM-S 用 `AutoAddPolicy` 是同一个理由（一次性
        测试机，不是需要维护身份的生产连接）。`-o ConnectTimeout` 给
        一个明确上限，避免目标机网络配置有问题时这条 ssh 命令本身挂到
        `run()` 的外层超时才失败——那样定位不到"是 SSH 本身连不上"还是
        "命令跑得慢"。
        """
        inner = (
            f"ssh -i {self._remote_key_path} "
            f"-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "
            f"-o ConnectTimeout=10 "
            f"root@{self._target_ip} {_shell_quote(cmd)}"
        )
        return self._vms_ssh.run(inner, timeout=timeout or self._timeout)


def _shell_quote(s: str) -> str:
    """给要在远端 shell 里当一个整体参数的字符串加单引号转义。

    与 `lobertest.ssh` 模块内部的同名私有函数逐字节一致的最小实现
    （只处理单引号转义），不 import 它——那是 ssh.py 的模块内部细节
    （下划线前缀），跨模块依赖私有符号会让本文件在 ssh.py 重构内部
    实现时莫名其妙跟着坏。这里的用法足够简单（把整条远程命令字符串
    当一个参数传给外层 ssh），不需要更完整的 shell 转义库。
    """
    return "'" + s.replace("'", "'\"'\"'") + "'"
