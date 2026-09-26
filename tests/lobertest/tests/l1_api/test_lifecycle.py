"""L1 主链路：六家族参数化，跑在 VM-S 的真实 Linux 部署上。

建 host（带磁盘清单）→ 导制品集并轮询 ready → 建模板并流转到 active →
建 hook 并审批到 active（两步：draft→review 由作者、review→active 由
审批人，两者不能是同一账号——hook.go 的 handleApproveHook 明确校验
「审批人不能等于作者」）→ 建任务 → 取 iPXE 菜单（这一步同时把任务从
pending 推进到 booting，并签发 InstallToken）→ 从菜单里抠出应答文件
URL 取应答文件。

家族差异（ISO 路径、家族名、应答文件名、菜单形态）全进 `FAMILIES`
参数表——现在七个 bash 脚本各写一遍同样的流程，改一个端点要改七处，
这个参数表就是用来消掉它的。

**ISO 制品现状（本任务实测确认，不是计划原文的假设）：** 计划原文说
`D:/qemu-vm/isos` 下有 OpenCloudOS 与 Debian 的 ISO，实测该目录根本不
存在（`config.iso_dir` 的默认值从未被真正验证过）。真实存在的 ISO 在
`D:/` 与 `D:/lober-isos/` 下：`D:/OpenCloudOS-9.6-x86_64-minimal.iso`
（`.treeinfo` 判据下识别为 rhel 家族，OpenCloudOS 是 RHEL 系
anaconda-based 发行版）、`D:/lober-isos/debian-netinst.iso`。没有
ubuntu/suse/windows/esxi 的 ISO——这四个家族在参数表里标
`pytest.mark.skip` 并写明缺什么、从哪能拿到，不降低断言让它假绿。

**`import_artifact` 的 `sourcePath` 必须是 guest 路径，不是 Windows
宿主路径**（另一处本任务实测才暴露的事实）：导入 job 跑在 guest 内的
lober 进程里，Windows 宿主上的 `D:/...` 路径对它不可见。`conftest.py`
的 `family_iso_paths` fixture 负责把上述两个 ISO 推到 guest 一次
（session 级），本文件按家族名从它取 guest 路径。

**Debian 家族额外需要 `netboot_path`：** netinst ISO 自带的
`install.amd/initrd.gz` 是光驱版 installer initrd，不具备网络安装能力
（`ErrInitrdCDOnly`）。VM-S 上已有真实的
`/srv/lober/sources/debian-13-netboot.tar.gz`（实测用 `tar tzf` 核实过
是真实 `debian-installer/amd64/{linux,initrd.gz}` 布局），不需要额外
下载。
"""

from __future__ import annotations

import re
from dataclasses import dataclass

import pytest

from lobertest.api import Console

pytestmark = pytest.mark.needs_vm


@dataclass(frozen=True)
class FamilyCase:
    """一个家族的主链路参数。

    `iso_path` 不在这里写死——ISO 必须先推到 guest 才能被 lober 自己的
    导入 job 打开（本任务实测发现：`import_artifact` 的 `sourcePath` 由
    guest 内的 lober 进程 open()，Windows 宿主路径对它毫无意义），guest
    路径由 `family_iso_paths` fixture（conftest.py）在 session 开始时
    推送并返回，用例体按 `case.family` 去这个字典里取实际路径。
    """

    family: str
    answer_filename: str
    interpreter: str
    netboot_guest_path: str | None = None


FAMILIES = [
    pytest.param(
        FamilyCase(family="rhel", answer_filename="ks.cfg", interpreter="bash"),
        id="rhel",
    ),
    pytest.param(
        FamilyCase(
            family="debian",
            answer_filename="preseed.cfg",
            interpreter="bash",
            # 真实网络安装 initrd（本任务实测发现：netinst ISO 自带的
            # install.amd/initrd.gz 是光驱版，不具备网络安装能力，导入
            # 会以 ErrInitrdCDOnly 失败——job.go:234-237 的逻辑）。VM-S
            # 的 /srv/lober/sources/debian-13-netboot.tar.gz 已经是真实
            # debian-installer/amd64/{linux,initrd.gz} 布局的网络安装
            # 制品（本任务实测 `tar tzf` 核实过内容），不需要另外下载。
            netboot_guest_path="/srv/lober/sources/debian-13-netboot.tar.gz",
        ),
        id="debian",
    ),
    pytest.param(
        None,
        id="ubuntu",
        marks=pytest.mark.skip(
            reason="缺 Ubuntu ISO（live-server 或等价的可网络安装介质）。"
            "本机 D:/ 与 D:/lober-isos/ 下未找到任何 ubuntu 相关 ISO。"
            "可从 https://releases.ubuntu.com/ 下载 live-server ISO 放到"
            "D:/lober-isos/ 下后移除本 skip。"
        ),
    ),
    pytest.param(
        None,
        id="suse",
        marks=pytest.mark.skip(
            reason="缺 openSUSE ISO。本机未找到任何 suse 相关 ISO，可从"
            "https://get.opensuse.org/ 下载 Leap 或 Tumbleweed 的网络安装"
            "介质（NET install）放到 D:/lober-isos/ 下后移除本 skip。"
        ),
    ),
    pytest.param(
        None,
        id="windows",
        marks=pytest.mark.skip(
            reason="缺 Windows 安装介质与配套制品（wimboot / BCD /"
            "boot.sdi / boot.wim）。这四项均需具体 Windows 版本授权介质，"
            "本机未准备，计划任务 8 风险清单已预见此项。"
        ),
    ),
    pytest.param(
        None,
        id="esxi",
        marks=pytest.mark.skip(
            reason="缺 ESXi ISO，需 Broadcom 账号下载（VMware 已被收购，"
            "旧的免费下载入口已下线）。计划任务 8 风险清单已预见此项，"
            "且 ESXi 仅支持 UEFI 固件、要求 8G 内存（本机 15.5G 天花板下"
            "与 VM-S 的 2.5G 叠加会贴近换页边界），拿到 ISO 后优先用"
            "ESXi 7.0 而非 8.0。"
        ),
    ),
]


def _extract_answer_url_parts(menu: str, filename_hint: str) -> tuple[str, str, str]:
    """从 iPXE 菜单文本里抠出 `/v1/answer/<task>/<token>/<filename>` 三段。

    菜单渡的是绝对 URL（`ctx.Bases.GatewayURL(...)` 拼出的完整地址，见
    `internal/render/menu.go`），本函数只关心路径部分里 `v1/answer/`
    之后的三段，不关心 URL 的 scheme/host（那部分是渲染器按部署配置的
    `gateway_base` 拼的，与本次测试直连的 `console.gateway_base_url`
    可能是不同的字面值但指向同一台机器——这是任务 8 provision 阶段的
    偏离，不是本函数要处理的问题）。
    """
    pattern = re.compile(r"v1/answer/([^/]+)/([^/]+)/([^\s\"']+)")
    match = pattern.search(menu)
    assert match is not None, f"菜单里找不到 /v1/answer/ 形式的应答文件 URL：{menu!r}"
    task_id, token, filename = match.group(1), match.group(2), match.group(3)
    assert filename.endswith(filename_hint) or filename == filename_hint, (
        f"应答文件名 {filename!r} 与家族期望的 {filename_hint!r} 不一致"
    )
    return task_id, token, filename


@pytest.mark.parametrize("case", FAMILIES)
def test_family_full_lifecycle(
    case: FamilyCase,
    console: Console,
    deployed,
    family_iso_paths: dict[str, str],
    artifact_reaper,
) -> None:
    """类别 1-3：六个家族各走完整主链路；应答文件形态按家族断言；iPXE
    菜单含 `#!ipxe` 首行与 boot 命令（`99b6ca7`、`4e32ed9` 的回归护栏）。

    ISO 路径不存在的家族在参数化层面已经 skip（见 FAMILIES 表的
    `pytest.mark.skip`），pytest 对被 skip 的参数不会执行本函数体，
    因此这里不需要额外判断 case 是否有效。"""
    sn = f"lobertest-{case.family}-sn"
    mac = _family_mac(case.family)

    console.cancel_active_tasks(sn)

    host_id = console.create_host(
        sn,
        mac,
        "x86_64",
        "bios",
        disks=[
            {
                "byIdPath": f"/dev/disk/by-id/virtio-{case.family}-disk0",
                "sizeBytes": 21474836480,
                "rotational": False,
                "model": "lobertest-virtual-disk",
            }
        ],
    )

    guest_iso_path = family_iso_paths[case.family]
    artifact_id = console.import_artifact(
        guest_iso_path,
        f"lobertest-{case.family}",
        netboot_path=case.netboot_guest_path,
    )
    # 登记给 artifact_reaper，session 结束时回收这份制品占的磁盘（rhel 约
    # 1.6G、debian 约 958M）。**登记要紧跟 import 之后、在 wait_ready 之前**：
    # 导入失败或超时的制品同样已经在盘上占了空间，等到断言之后再登记就漏掉
    # 了失败路径——而失败恰恰是最容易留垃圾的路径。
    #
    # 为什么不是调 API 删（那条路 409）、为什么不做孤儿探测，见
    # conftest.py 里 artifact_reaper 的文档。
    artifact_reaper(artifact_id)

    artifact = console.wait_artifact_ready(artifact_id, timeout=600)
    assert artifact["osFamily"] == case.family

    template_id = console.create_template(
        f"lobertest-{case.family}-tpl",
        artifact_id,
        "",
        diskPolicy="first",
        onFailure="halt",
    )
    # set_template_state 需要 version；Console.create_template 只返回 id
    # （契约签名如此，任务 3 已固定），不需要额外一次查询去发现 version——
    # 新建模板的 version 恒为 1（handleCreateTemplate 硬编码
    # `Version: 1`，见 template.go:269）。
    console.set_template_state(template_id, "active", version=1)

    author_hook_id = console.create_hook(
        f"lobertest-{case.family}-hook",
        "pre",
        "#!/bin/bash\necho lobertest-hook\n",
    )
    # draft → review：作者本人发起，用 hook-author 身份（hook:write 权限）。
    console.approve_hook(author_hook_id, role="hook-author")
    # review → active：审批人不能等于作者，用 hook-approver 身份
    # （hook:approve 权限）。两步共用同一个 approve 端点，服务端按当前
    # state 分流（hook.go 的 handleApproveHook）。
    console.approve_hook(author_hook_id, role="hook-approver")

    task_id = console.create_task(host_id, template_id, {"hostname": f"lobertest-{case.family}"})

    menu = console.boot_menu(sn, mac)
    assert menu.startswith("#!ipxe"), f"菜单缺 #!ipxe 首行（{case.family}）：{menu!r}"
    assert "boot" in menu or "sanboot" in menu or "chain" in menu or "kernel" in menu, (
        f"菜单缺 boot 类命令（{case.family}）：{menu!r}"
    )

    menu_task_id, token, filename = _extract_answer_url_parts(menu, case.answer_filename)
    assert menu_task_id == task_id

    answer_body = console.answer_file(task_id, token, filename)
    assert answer_body, f"应答文件正文为空（{case.family}）"


_FAMILY_MAC_SUFFIX = {
    "rhel": "01",
    "debian": "02",
    "ubuntu": "03",
    "suse": "04",
    "windows": "05",
    "esxi": "06",
}


def _family_mac(family: str) -> str:
    """按家族生成一个固定 MAC。

    **必须是跨进程稳定的固定映射，不能用 Python 内置 `hash()`**——本任务
    重构时发现：`hash(str)` 在不同解释器进程间不稳定（`PYTHONHASHSEED`
    默认随机化），若用它派生 MAC，同一家族在下一次 `pytest` 调用里会
    生成不同的 MAC 值。这不只是「测试数据看起来不一致」，是真实的数据
    完整性问题——`postgres.Store.UpsertHost` 的写入冲突键是 **mac 列**
    （不是 sn，`ON CONFLICT (mac) DO UPDATE`，见
    `internal/store/postgres/host/store.go:52-57`），MAC 变了会让每次
    重跑都在数据库里插入一行**新的**主机记录（同一个 SN，不同的 MAC），
    而不是更新已有记录——旧行不会被清理，累积成同一个 SN 对应多行的
    脏数据，`FindHostByIdentity` 按 SN 反查时会因为不知道该选哪一行而
    行为不确定。改成固定的字面量映射后，同一家族在任何一次运行里都是
    同一个 MAC，UpsertHost 走的是「更新」分支，不产生新行。"""
    return f"52:54:00:aa:bb:{_FAMILY_MAC_SUFFIX[family]}"
