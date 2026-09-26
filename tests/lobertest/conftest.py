"""顶层 conftest：target 解析、session 级部署、层间闸门。

只放三层共用的 fixture（settings/deployed/console/repo_root），层特有的
fixture（L2 的 target、L3 的 build_web+playwright）留在各自目录下的
conftest.py（任务 9/10 的事）。

层间闸门（本文件的核心职责，任务 8）：L1（`needs_vm` marker）有任何失败
时，L2/L3（`slow` marker）的用例在运行期被跳过而非等它们真的跑起来再
失败——接口层断了，装机与 UI 必然跟着断，再跑一小时没有信息量。用
`pytest_runtest_setup` 运行期钩子实现（`--ignore` 是启动时的静态路径
过滤，做不到「等 L1 跑完看结果再决定」这种运行期条件；`pytest_
collection_modifyitems` 也做不到——它只在收集阶段跑一次，发生在任何
用例真正执行之前，见文件下方「层间闸门」一节的详细记录，那里记录了
第一版实现为什么错、怎么发现、怎么改对）。
"""

from __future__ import annotations

from pathlib import Path

import pytest

from lobertest.api import Console
from lobertest.config import Settings, load
from lobertest.deploy import (
    build_test_users,
    cross_compile,
    create_schema,
    deploy,
    render_config,
    teardown_schema,
    wait_ready,
)
from lobertest.ssh import SSH


@pytest.fixture(scope="session")
def repo_root() -> Path:
    """从本文件出发向上找到仓库根（`cmd/lober` + `go.mod` 存在的那一层）。

    本文件与 test_users.py/test_deploy.py 各自的同名 fixture 逐字节一致
    （向上找 cmd/lober 目录 + go.mod 文件），任务 6 的 test_users.py 已经
    在注释里预留了这个收敛点（"两个测试文件目前没有共享的 conftest.py
    承载会话级 fixture（那是任务 8 才建立的顶层 conftest.py 的职责）"）。
    没有反过来改 test_users.py/test_deploy.py 去 import 本文件的
    repo_root——那两个文件属于任务 5/6 已完成并 commit 的范围，本任务
    只做「只做任务 8」这条边界要求的事，不去动前面任务已经绿灯的文件。
    """
    here = Path(__file__).resolve()
    for parent in [here, *here.parents]:
        if (parent / "cmd" / "lober").is_dir() and (parent / "go.mod").is_file():
            return parent
    raise RuntimeError(f"从 {here} 向上找不到仓库根（cmd/lober + go.mod）")


@pytest.fixture(scope="session")
def settings() -> Settings:
    return load()


@pytest.fixture(scope="session")
def deployed(settings: Settings, repo_root: Path, tmp_path_factory: pytest.TempPathFactory) -> Settings:
    """session 级部署闸门：交叉编译 + 建 schema + 推二进制/配置 + 等就绪。

    每轮 pytest 一次，不是每个用例一次——`deployed` 是 session scope。
    返回值就是 settings 本身（`DeployInfo` 这个契约里提到的类型名在本次
    实现里没有另建一个结构体：调用点唯一需要从"已部署"这件事上取到的
    信息只有 `settings`——console_base_url、schema 名——而这些本来就是
    settings 的字段，另建一个只转发同一批字段的 DeployInfo 类是没有实质
    内容的包装，`console` fixture 直接依赖 settings 即可）。

    `cross_compile` 失败时异常自然从本 fixture 抛出并向上传播——pytest
    的 session 级 fixture 一旦抛异常，整个 session 立刻停止收集/执行后续
    用例（这正是「session 立刻停止」这条要求在 pytest 里的落地方式，
    不需要额外写终止逻辑）。
    """
    binary = cross_compile(repo_root, tmp_path_factory.mktemp("deploy") / "lober-linux-amd64")
    users = build_test_users(repo_root)

    with SSH(settings) as ssh:
        create_schema(ssh, settings.schema)
        config_text = render_config(schema=settings.schema, users=users)
        deploy(ssh, binary, config_text)

        console = Console(settings.console_base_url)
        wait_ready(ssh, console, timeout=60)

    return settings


# 六家族里两个本机有真实 ISO 的家族，及其在 Windows 宿主上的路径。
#
# **本任务实测确认，不是计划原文的假设：** 计划原文说 `D:/qemu-vm/isos`
# 下有 OpenCloudOS 与 Debian 的 ISO，实测该目录根本不存在
# （`config.iso_dir` 的默认值从未被真正验证过）。真实存在的 ISO 在
# `D:/` 与 `D:/lober-isos/` 下。没有 ubuntu/suse/windows/esxi 的 ISO
# ——这四个家族在 test_lifecycle.py 的参数表里标 pytest.mark.skip。
_HOST_ISO_PATHS = {
    "rhel": Path("D:/OpenCloudOS-9.6-x86_64-minimal.iso"),
    "debian": Path("D:/lober-isos/debian-netinst.iso"),
}

# guest 上 ISO 落地的目录。用 /srv/lober/sources（root:root 755，
# provision 阶段已存在，且已经放着 debian-13-netboot.tar.gz），不是
# /tmp——ISO 上百 MB 到几 GB，/tmp 是 tmpfs（内存盘），guest 只有约 750M
# 内存盘配额，塞不下；落到磁盘路径也让「重跑 session 不必重推」这件事
# 有意义（推送前先检查目标文件是否已存在且大小一致，见 _ensure_iso_pushed）。
_GUEST_ISO_DIR = "/srv/lober/sources"


def _ensure_iso_pushed(ssh: SSH, local_path: Path, guest_filename: str) -> str:
    """把本地 ISO 推到 guest 的 `_GUEST_ISO_DIR` 下，返回 guest 侧绝对路径。

    **本任务第一次真跑 L1 才发现的事实**：`import_artifact` 的
    `sourcePath` 由 lober 进程自己在 guest 内 `open()`（导入 job 跑在
    guest 内的 lober 进程里，不是测试进程），Windows 宿主路径
    （`D:/OpenCloudOS-9.6-x86_64-minimal.iso`）对 guest 而言毫无意义——
    实测直接报 `open D:/...: no such file or directory`。计划契约的
    `import_artifact(source_path, name)` 签名本身没错，错的是调用方
    （本文件）传入的 source_path 必须先是一个 guest 路径，这条约束在
    计划任务 3/8 都没有写明，是本任务实测暴露的缺口。

    推送前先检查 guest 上是否已有同名同大小的文件——避免每次 session
    都重推几 GB（ISO 内容不会变，SFTP 传输本身要几十秒到几分钟，
    session 级 fixture 不应该重复付出这个代价）。用文件大小而非哈希
    判断「已经推过」：算 sha256 需要在 guest 上跑一次全文件读取，对
    几 GB 文件同样不便宜，文件大小足够区分「同一个文件」与「推送
    中断留下的半截文件」（后者大小几乎不可能恰好相等）。
    """
    guest_path = f"{_GUEST_ISO_DIR}/{guest_filename}"
    local_size = local_path.stat().st_size

    check = ssh.run(f"stat -c %s {guest_path} 2>/dev/null || echo MISSING")
    remote_size_str = check.stdout.strip()
    if remote_size_str == str(local_size):
        return guest_path

    # 实测订正：中转路径不能是 /tmp——那是 tmpfs（内存盘），本机只有约
    # 736M 配额，装不下一整个 ISO（实测 OpenCloudOS 1.7G 推到一半后
    # sftp.put 报 `SFTP_FAILURE`，`df -h /tmp` 确认写满到 100%）。改用
    # `debian` 用户自己的 home 目录（`/home/debian`，落在真实磁盘
    # /dev/vda1 上，20G 盘当前有 15G 空闲），推送权限与磁盘容量都够。
    staging_path = f"/home/debian/lobertest-iso-upload-{guest_filename}"
    ssh.push(local_path, staging_path)
    ssh.sudo(f"mv {staging_path} {guest_path}").check(f"移动 ISO 到 {guest_path}")
    ssh.sudo(f"chmod 644 {guest_path}").check("设置 ISO 可读权限")
    return guest_path


@pytest.fixture(scope="session")
def family_iso_paths(deployed: Settings) -> dict[str, str]:
    """把本机真实存在的 ISO 推到 guest 一次，返回 {家族名: guest 路径}。

    只包含本机真实有 ISO 的家族（目前 rhel/debian）——没有 ISO 的家族
    根本不会调用这个 fixture 的返回值（它们在参数化层面已经 skip），
    这里也不应该为它们放一个不存在的路径进字典误导调用方。
    """
    guest_paths: dict[str, str] = {}
    with SSH(deployed) as ssh:
        for family, local_path in _HOST_ISO_PATHS.items():
            if not local_path.is_file():
                continue
            guest_paths[family] = _ensure_iso_pushed(ssh, local_path, local_path.name)
    return guest_paths


@pytest.fixture(scope="session")
def console(deployed: Settings) -> Console:
    """人面 + 机器面两个 base_url 都要给（任务 3 的实现后订正：机器面用例
    缺了 gateway_base_url 会抛 ValueError）。

    机器面 base_url 不进 `Settings`（任务 1 已冻结该 dataclass 的字段），
    在这里按约定拼出来。

    **实测订正（本任务发现的环境缺口）：** guest 内 nginx 的机器面
    listener 绑定的是 `10.10.10.10:80`（guest 内部地址），而
    `start-vm.sh` 最初只给 SSH 与人面（8090）配了 hostfwd，机器面端口
    完全没有从 Windows 宿主转发进去——直接连 `127.0.0.1:80` 会纯挂起
    到超时（实测用 curl 验证过，TCP 层面没有任何东西在监听那个地址，
    不是响应慢）。这不是应该由本文件"猜出正确端口"就能绕过的问题，是
    provision 阶段留下的真实基础设施缺口。已协调加了第三条 hostfwd
    （`start-vm.sh` 2026-09-26 新增：`hostfwd=tcp:127.0.0.1:8080-
    10.10.10.10:80`），本函数因此拼 `127.0.0.1:8080` 而不是
    `127.0.0.1:80`——两者在 guest 内部都还是 80 端口，只是 Windows 宿主
    侧转发用的端口号不同（8090 同理，人面在 guest 内部也不是 8090
    本身，是 nginx 反代到 `127.0.0.1:8190`，这里只关心宿主侧能连到
    哪个端口）。
    """
    from urllib.parse import urlsplit, urlunsplit

    deployed_settings = deployed
    parts = urlsplit(deployed_settings.console_base_url)
    gateway_netloc = parts.netloc.replace(":8090", ":8080")
    gateway_base_url = urlunsplit((parts.scheme, gateway_netloc, "", "", ""))
    return Console(deployed_settings.console_base_url, gateway_base_url=gateway_base_url)


@pytest.fixture(scope="session")
def ready_rhel_artifact_id(console: Console, family_iso_paths: dict[str, str]) -> str:
    """导入一次 RHEL 家族制品集并等到 ready，session 内全程复用。

    准入（test_admission.py）与 RBAC（test_rbac.py）两个文件里的多个
    用例各自都需要「一个 active 模板」作前置，但它们真正要测的是准入
    规则或权限边界，不是「制品集能不能导入成功」——那件事已经由
    test_lifecycle.py 的家族参数化用例验证过。若每个用例各自重新
    import_artifact 一次真实 RHEL ISO，guest 磁盘上会重复出现多份
    ~1.6G 的抽取结果（本任务实测：单个 RHEL 家族抽取产物约 1.6G），
    在这台只有个位数 GB 空闲空间的 VM-S 上不可持续。本 fixture 把这个
    代价收敛到 session 内一次。

    用 rhel 而非 debian：rhel 不需要 netboot_path 这个额外参数，作为
    「随便找一个能 ready 的家族」的选择更简单。
    """
    artifact_id = console.import_artifact(family_iso_paths["rhel"], "lobertest-shared-rhel-artifact")
    console.wait_artifact_ready(artifact_id, timeout=600)
    return artifact_id


# ---------------------------------------------------------------------------
# 层间闸门：L1（needs_vm）失败 → L2/L3（slow）打 skip。
# ---------------------------------------------------------------------------
#
# **实现方式在本任务的验证过程中订正过一次**：最初实现用
# `pytest_collection_modifyitems` 在收集阶段读 `_l1_failed` 决定要不要
# 打 skip 标记——这是错的，已用人为造的假 L1/L2 用例验证过并观察到失败
# （见任务报告「层间闸门的验证过程」一节）。根因：
# `pytest_collection_modifyitems` 只在**收集阶段**调用一次（`pytest` 自己
# 的 hookspec 文档原文「Called after collection has been performed」，
# 没有「之后还会再调用」这层含义），发生在任何用例真正开始跑之前——那时
# `_l1_failed` 必然是空的，因为还没有任何 L1 用例执行过。原实现的注释
# 断言"这个钩子在每个用例真正执行之前都会被 pytest 重新调用一次"是没有
# 核实过就写下的错误假设。
#
# 正确的钩子是 `pytest_runtest_setup`：它在**每个** item 的 setup 阶段
# 之前都会被调用一次（每个用例各自触发一次，不是只在会话开始时触发一次），
# 因此能在运行期看到"目前为止 L1 是否已经失败过"这个随时间累积的事实，
# 并在 L2/L3 用例真正进入 setup（意味着它马上要跑 fixture、马上要跑用例
# 体）之前用 `pytest.skip()` 拦下来。

# 用模块级列表记录本 session 内已失败的 L1（needs_vm）用例 nodeid。
_l1_failed: list[str] = []


def pytest_runtest_logreport(report: pytest.TestReport) -> None:
    """运行期钩子：一旦观察到某个 `needs_vm` 用例失败，记下它的 nodeid。

    只在 `call` 阶段判定失败（不看 `setup`/`teardown` 阶段）：setup 阶段
    失败（比如 fixture 本身抛异常）意味着 `deployed`/`console` 这类
    session fixture 已经出错，那种情形下 session 会直接因为 fixture
    异常向上传播而整体终止（见 `deployed` 的文档），根本不会跑到
    L2/L3 的收集判断这一步；本钩子要处理的是「fixture 都正常、但某条
    L1 用例的断言本身失败」这种更常见的情形。
    """
    if report.when != "call":
        return
    if not report.failed:
        return
    if "needs_vm" not in report.keywords:
        return
    _l1_failed.append(report.nodeid)


def pytest_runtest_setup(item: pytest.Item) -> None:
    """运行期钩子：在每个 item 的 setup 阶段之前调用（每个用例各触发
    一次）。若本 session 目前已经观察到任一 L1（`needs_vm`）用例失败，
    且当前 item 是 L2/L3（`slow`），直接 `pytest.skip()`——接口层断了，
    装机与前端必然跟着断，再跑下去没有信息量。

    用 `pytest.skip()` 而不是在收集阶段给 item 加 skip marker：后者
    （`pytest_collection_modifyitems`）只在收集阶段跑一次，无法感知
    "运行到这里时，前面已经有 L1 用例失败了"这个随时间累积的事实——
    这正是本文件这段实现被订正过一次的原因，订正前的版本假设了一个
    不存在的"重复调用"语义。
    """
    if not _l1_failed:
        return
    if "slow" not in item.keywords:
        return
    reason = f"L1 接口层已有用例失败（{_l1_failed[0]} 等），跳过装机/前端层——" "接口层断了，L2/L3 必然跟着断，跑下去没有信息量"
    pytest.skip(reason)
