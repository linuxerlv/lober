"""RHEL 系（OpenCloudOS 9.6）端到端真实装机。

这不是单元测试——它真的起 QEMU、真的让 anaconda 把系统写进磁盘。跑法：

    export PATH="/c/Users/v_lvxiaoyang/.local/bin:$PATH"
    uv run --project tools/vmlab pytest tools/vmlab/tests -m "not slow" -s   # 前置
    uv run --project tools/vmlab pytest tools/vmlab/tests -m slow -s         # 真装机

验收标准两条独立证据（缺一条都不算通过，见
docs/superpowers/specs/2026-09-25-vmlab-real-install-design.md §7）：

1. 状态机走到 done，且七个装机期信号按 Seq 升序全部到达；
2. SSH 能登进新装的机器（用注入的公钥）。

第二条不能省：那串信号是我们自己渲染的脚本发出来的，全到齐只证明脚本跑完了，
不证明系统是好的。
"""

from __future__ import annotations

import pytest

from conftest import HOST_MAC, HOST_SN
from vmlab import INSTALL_SIGNALS, InstallRunner, TargetVM

# 装完的目标机在装机 LAN 上的地址。dnsmasq 的 dhcp-range 从 .100 起，目标机
# 是网段里唯一的 DHCP 客户端，所以实测稳定拿到 .112（同一 MAC 续租同一地址）。
TARGET_IP = "10.20.20.112"


def test_services_up(server):
    """前置：四个服务都在跑。不通过的话后面的失败都没有诊断价值。"""
    states = server.service_active("dnsmasq", "nginx", "postgresql", "lober")
    bad = {k: v for k, v in states.items() if v != "active"}
    assert not bad, f"这些服务不是 active: {bad}"


def test_artifact_import_sets_identity(rhel_artifact):
    """导入必须写全身份四项。

    锁住一个真实缺口：导入路径此前只写 OSFamily 与 Paths，arch/firmware 永远
    是空。precheck 的两道硬闸门直接比对它们（checkArch 与 MatchesFirmware），
    空值必然拒绝，于是制品集能进 ready 却在每次 /v1/boot 被拒。
    """
    a = rhel_artifact
    assert a["osFamily"] == "rhel", f"家族识别错误: {a['osFamily']}"
    assert a["arch"] == "x86_64", (
        f"arch={a['arch']!r}——空值会让 precheck 的 checkArch 拿机器上报的 "
        "x86_64 与空串比对，必然拒绝每一次引导"
    )
    assert a["firmware"] == "any", (
        f"firmware={a['firmware']!r}——Linux 四家一套制品通吃两种固件，应为 any；"
        "空串不等于 any，MatchesFirmware 会拒绝所有机器"
    )
    assert a["paths"]["kernel"], "kernel 路径为空"
    assert a["paths"]["initrd"], "initrd 路径为空"


def test_hook_published_and_servable(api, server, disk_hook):
    """hook 正文必须落盘且经 nginx 可取。

    锁住另一个真实缺口：机器侧从 /artifacts/hooks/<digest>/script.sh 取脚本
    （SPEC §6.3），而此前没有任何代码写那个文件——required 的 hook 取不到
    脚本即失败，%pre 卡到阶段超时，失败信息里只说「阶段 pre 超时」。
    """
    digest = disk_hook["bodySha256"]
    path = f"{server.s.artifact_root}/hooks/{digest}/script.sh"
    assert server.sudo(f"test -f {path}").ok(), f"hook 正文未落盘到 {path}"

    # 落盘了但权限不对同样是 404，所以要经 nginx 实测一次。
    url = f"{api.settings.console.gateway_url}/artifacts/hooks/{digest}/script.sh"
    code = (
        server.run(f'curl -s -o /dev/null -w "%{{http_code}}" {url}')
        .check("取 hook 脚本")
        .stdout.strip()
    )
    assert code == "200", f"nginx 发 hook 脚本返回 {code}，应为 200"
    assert disk_hook["state"] == "active", f"hook 未推进到 active: {disk_hook['state']}"


def test_template_active(rhel_template):
    """模板必须能激活。

    锁住第三个真实缺口：模板只能被创建成 draft，而建任务硬性要求 active，
    而契约冻结的四个模板端点里此前没有任何一个能改状态——任何模板都永远
    无法用于建任务。
    """
    assert rhel_template["state"] == "active", f"模板未激活: {rhel_template['state']}"
    assert rhel_template["artifactSetId"], "模板没有关联制品集"
    assert rhel_template["hookRefs"], "模板没有引用任何 hook"


def test_boot_menu_shape(api, rhel_template, rhel_host):
    """引导菜单的形态检查（不起 VM，只看渲染结果）。

    三条都是实测踩过的缺口：缺 #!ipxe 时 iPXE 不把内容当脚本执行、缺 boot
    时只加载不跳转、inst.repo 指向包索引目录时 anaconda 取 stage2 404。
    三者的症状完全相同——机器在 PXE 里转圈，而服务端一切正常。
    """
    api.cancel_active_tasks()
    task_id = api.create_task(HOST_SN, rhel_template["id"], {"hostname": "vmlab-t01"})
    menu = api.boot_menu(HOST_SN, HOST_MAC)

    assert menu.startswith("#!ipxe\n"), (
        f"菜单首行不是 #!ipxe——iPXE 只有首行是它才把内容当脚本执行，"
        f"缺了就静默不执行。实际:\n{menu[:200]}"
    )
    assert menu.rstrip().endswith("boot"), (
        f"菜单末行不是 boot——kernel/initrd 只加载不执行，机器会退回固件"
        f"引导顺序继续 PXE 轮询。实际:\n{menu}"
    )
    # inst.stage2 指介质根（有 images/install.img），inst.repo 指包仓库
    # （有 repodata/repomd.xml）。两者在真实 RHEL 9 系介质上不是同一个目录，
    # 用一个参数指必然有一边取不到——两种错法都实测过，见
    # internal/render/menu_stage2_test.go 的记录。
    kernel_line = menu.split("initrd")[0]
    assert "inst.stage2=" in kernel_line, (
        f"菜单缺 inst.stage2——anaconda 会退回从 inst.repo 根找 "
        f"images/install.img，而包仓库在 BaseOS/ 下、那里没有。实际:\n{menu}"
    )
    assert "inst.repo=" in kernel_line, f"菜单缺 inst.repo:\n{menu}"
    stage2 = kernel_line.split("inst.stage2=")[1].split()[0]
    repo = kernel_line.split("inst.repo=")[1].split()[0]
    assert stage2.endswith("/repo/"), f"inst.stage2 应指介质根，实际 {stage2}"
    assert repo.endswith("/repo/BaseOS/"), (
        f"inst.repo 应指包仓库（本 ISO 的 .treeinfo 声明 repository=BaseOS），"
        f"实际 {repo}"
    )
    api.cancel_active_tasks()


@pytest.mark.slow
def test_real_install_reaches_done(server, api, rhel_template, rhel_host, request):
    """**最小成功线**：目标机真的装完，状态机走到 done。"""
    runner = InstallRunner(
        server=server,
        api=api,
        vm=TargetVM(name="t01", mac=HOST_MAC, firmware="bios"),
        budget_sec=2700,
    )
    result = runner.run(HOST_SN, rhel_template["id"])
    request.session.install_result = result

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


@pytest.mark.slow
def test_ssh_into_installed_machine(server, request):
    """验收第二条：SSH 登进新装的机器。

    那串信号是我们自己渲染的脚本发出来的，全到齐只证明脚本跑完了，不证明
    系统是好的。这条同时验证 kickstart 的 `rootpw --lock` + 公钥注入是唯一
    登录通路（SPEC §1.5）。

    从 VM-S 内部发起：目标机在装机 LAN 上，Windows 宿主不在那个网段。
    """
    result = getattr(request.session, "install_result", None)
    if result is None or not result.ok():
        pytest.skip("装机未成功，跳过 SSH 验收")

    probe = server.run(
        "ssh -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null "
        f"-o ConnectTimeout=10 -i /home/debian/.ssh/id_ed25519 root@{TARGET_IP} "
        "'cat /etc/os-release; systemctl is-system-running'",
        timeout=60,
    )
    assert probe.ok(), f"SSH 登入失败: {probe.stderr}"
    assert "OpenCloudOS" in probe.stdout, f"装的不是 OpenCloudOS: {probe.stdout[:300]}"
