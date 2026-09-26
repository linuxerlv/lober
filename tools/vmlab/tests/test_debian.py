"""Debian 13 端到端真实装机。

与 RHEL 的三处关键差别（都是机制层面的，不是参数差异）：

1. **应答文件是 preseed**（debconf 键值对），不是 kickstart。
2. **不支持 by-hook 磁盘策略**：preseed 的 `partman-auto/disk` 直接吃路径，
   没有 kickstart `%include` 那种延迟机制。所以主机必须先录入磁盘清单，
   由服务端的磁盘规划器选盘（策略用 largest）。
3. **装包源是 netinst 的 dists/ + pool/ 树**，不是 RPM 仓库——`mirror/http/*`
   三个 debconf 键指向它。

跑法与 RHEL 相同：

    uv run --project tools/vmlab pytest tools/vmlab/tests/test_debian.py -m "not slow" -s
    uv run --project tools/vmlab pytest tools/vmlab/tests/test_debian.py -m slow -s
"""

from __future__ import annotations

import pytest

from vmlab import INSTALL_SIGNALS, InstallRunner, TargetVM

# Debian netinst 挂在 VM-S 的第二块 virtio 盘上（vdb 是 OpenCloudOS）。
ISO_DEVICE = "/dev/vdc"

# 与 RHEL 用同一台目标机（串行排队，一次只跑一个家族），但 SN 不同——同一 SN
# 只能有一个活跃任务（idx_tasks_sn_active），两个家族各用自己的主机记录更清晰。
HOST_SN = "VMLAB-T02"
HOST_MAC = "52:54:00:30:30:02"
TARGET_IP = "10.20.20.113"

# 目标机磁盘的 by-id 路径。QEMU 的 virtio 盘默认没有 by-id 链接，用 serial
# 参数给它一个——不然只能退回内核名（/dev/vda），而那个名字在不同启动之间会
# 漂移（SPEC §10 失败模式 5）。
DISK_SERIAL = "vmlab-t02-disk"
DISK_BY_ID = f"/dev/disk/by-id/virtio-{DISK_SERIAL}"
DISK_SIZE = 12 * 1024 * 1024 * 1024

# preseed 的 tasksel 选择。standard 是最小可用集合，加 openssh-server 供验收
# 第二条（SSH 登入）。
TASKSEL = "standard, ssh-server"


@pytest.fixture(scope="session")
def debian_artifact(api) -> dict:
    """导入 Debian netinst。"""
    name = "debian-13.6.0-vmlab"
    for a in api.call("GET", "/artifact-sets"):
        if a["name"] == name and a["state"] == "ready":
            return a
    artifact_id = api.import_artifact(ISO_DEVICE, name)
    return api.wait_artifact_ready(artifact_id, timeout=900)


@pytest.fixture(scope="session")
def debian_host(api) -> str:
    """建主机并录入磁盘清单。

    **Debian 必须有这一步**：preseed 不支持 by-hook，磁盘规划要在建任务时完成，
    而服务端只能从 Host.Disks 拿到盘（FactStore 按 taskID 存，建任务时任务还不
    存在，是循环依赖）。
    """
    for h in api.call("GET", "/hosts"):
        if h["sn"] == HOST_SN:
            return h["id"]
    return api.call(
        "POST",
        "/hosts",
        {
            "sn": HOST_SN,
            "mac": HOST_MAC,
            "arch": "x86_64",
            "firmware": "bios",
            "rack": "vmlab",
            "disks": [
                {
                    "byIdPath": DISK_BY_ID,
                    "sizeBytes": DISK_SIZE,
                    "rotational": True,
                    "model": "QEMU HARDDISK",
                }
            ],
        },
    )["id"]


@pytest.fixture(scope="session")
def debian_template(api, debian_artifact) -> dict:
    """Debian 模板，磁盘策略 largest（服务端选盘）。

    不挂 hook：Debian 这一轮先验证 preseed 主链路，hook 机制已由 RHEL 那轮
    验证过（同一份 clientlib 被六个家族共用）。
    """
    tpl = api.create_template(
        name="vmlab-debian-minimal",
        artifact_set_id=debian_artifact["id"],
        answer_template=TASKSEL,
        hook_refs=[],
        disk_policy="largest",
    )
    return api.activate_template(tpl)


def test_debian_artifact_identity(debian_artifact):
    """导入必须认出 debian 并写全身份。

    这条同时验证 commit 131d659 的修复：真实 netinst 不再被误判成 Ubuntu
    （判据从 .disk/info 换成 casper/，而 Debian 没有 casper/）。
    """
    a = debian_artifact
    assert a["osFamily"] == "debian", (
        f"家族识别错误: {a['osFamily']}——真实 netinst 曾被误判成 ubuntu"
        "（.disk/info 是两家共有的，casper/ 才是 Ubuntu 独有）"
    )
    assert a["arch"] == "x86_64", (
        f"arch={a['arch']!r}——Debian 的架构判据是 dists/<suite>/main/binary-<arch>/ "
        "的目录名"
    )
    assert a["firmware"] == "any", f"firmware={a['firmware']!r}，Linux 四家应为 any"
    assert a["paths"]["kernel"] == "linux", (
        f"Debian 的内核文件名是 linux（不是 RHEL 的 vmlinuz）: {a['paths']['kernel']}"
    )
    assert a["paths"]["initrd"] == ["initrd.gz"], (
        f"Debian 的 initrd 是 initrd.gz（不是 initrd.img）: {a['paths']['initrd']}"
    )


def test_debian_host_has_disks(api, debian_host):
    """主机磁盘清单必须录进去了。"""
    for h in api.call("GET", "/hosts"):
        if h["id"] == debian_host:
            disks = h.get("disks") or []
            assert disks, (
                "主机没有磁盘清单——preseed 不支持 by-hook，磁盘规划要在建任务时"
                "完成，没有清单的话 largest 策略会以「磁盘清单为空」失败"
            )
            assert disks[0]["byIdPath"] == DISK_BY_ID
            return
    pytest.fail(f"找不到主机 {debian_host}")


def test_debian_template_active(debian_template):
    assert debian_template["state"] == "active"
    assert debian_template["diskPolicy"] == "largest", (
        f"Debian 必须用服务端选盘策略，by-hook 会被渲染器拒绝: "
        f"{debian_template['diskPolicy']}"
    )


def test_debian_preseed_shape(server, api, debian_template, debian_host):
    """preseed 的形态检查（不起 VM）。

    三条都是 preseed 特有的、与 kickstart 完全不同的机制。
    """
    api.cancel_active_tasks()
    task_id = api.create_task(HOST_SN, debian_template["id"], {"hostname": "vmlab-t02"})
    menu = api.boot_menu(HOST_SN, HOST_MAC)

    assert menu.startswith("#!ipxe\n"), f"菜单首行不是 #!ipxe:\n{menu[:200]}"
    assert menu.rstrip().endswith("boot"), f"菜单末行不是 boot:\n{menu}"
    assert "preseed/url=" in menu, (
        f"Debian 菜单应用 preseed/url= 指向应答文件（不是 RHEL 的 inst.ks=）:\n{menu}"
    )
    assert "console=ttyS0" in menu, f"菜单缺串口控制台:\n{menu}"

    # token 不在 GET /tasks 的响应里——那是签给机器的凭据，人面 API 刻意不回显
    # （docs/contracts/http-console.md 的 TaskItem 字段表）。诊断时从库里读。
    token = server.sudo(
        f"-u postgres psql -d lober -qtAc "
        f"\"SELECT token FROM tasks WHERE id = '{task_id}'\""
    ).check("取 token").stdout.strip()
    preseed = api.answer_file(task_id, token, "preseed.cfg")

    assert "partman-auto/disk" in preseed, f"preseed 缺分区指令:\n{preseed[:600]}"
    assert DISK_BY_ID in preseed, (
        f"服务端选出的盘没进 preseed——磁盘清单到决策器这一环断了。"
        f"preseed 前 600 字:\n{preseed[:600]}"
    )
    assert "mirror/http/hostname" in preseed, (
        f"preseed 缺 mirror/http/* 三键——Debian 的装包源靠它们指向 dists/ 树:"
        f"\n{preseed[:600]}"
    )
    api.cancel_active_tasks()


@pytest.mark.slow
def test_debian_real_install(server, api, debian_template, debian_host):
    """Debian 真实装机走到 done。"""
    runner = InstallRunner(
        server=server,
        api=api,
        vm=TargetVM(
            name="t02",
            mac=HOST_MAC,
            firmware="bios",
            disk_serial=DISK_SERIAL,
        ),
        budget_sec=2700,
    )
    result = runner.run(HOST_SN, debian_template["id"], {"hostname": "vmlab-t02"})

    print("\n" + result.summary())
    if not result.ok():
        print("\n--- 串口尾部 ---\n" + result.console_tail[-1500:])
        print("\n--- 任务日志 ---\n" + server.task_log_tail(result.task_id))

    missing = result.missing_signals(INSTALL_SIGNALS)
    assert not missing, (
        f"装机期信号缺失 {missing}；已到达 {result.signals}。"
        f"状态={result.state} 阶段={result.phase} 详情={result.fail_detail}"
    )
    assert result.disk_bytes > 500 * 1024 * 1024, (
        f"目标盘只写入 {result.disk_bytes // 1024 // 1024}MB——系统没真的装进去"
    )
    assert result.ok(), f"状态机未到 done: {result.state}/{result.phase} {result.fail_detail}"
