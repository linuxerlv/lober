"""L2 装机层：真的把系统装到一台 QEMU 目标机上。

**本文件当前只做计划任务 9 的类别 1（RHEL BIOS，最小成功线）与类别 4
（失败路径的证据完整性），不做类别 2（RHEL UEFI）/3（Debian/Ubuntu/
openSUSE）——team-lead 的 brief 明确要求"只做 RHEL BIOS 这一个家族，
不要做 UEFI、不要做其余家族"，先证明一台能装成。**

与 L1（`tests/l1_api/test_lifecycle.py`）的主链路步骤一致（建 host →
导制品集 → 建模板 → 建 hook → 建任务 → 取 iPXE 菜单），唯一的本质区别
是：L1 到"取应答文件"就结束（纯 API 断言，从未有真实机器执行那份应答
文件），L2 让一台真实 QEMU 虚拟机去 PXE 引导、真的执行那份 kickstart、
真的重启、真的跑完 firstboot 脚本。装机脚本本身（网络配置、rootpw
--lock、sshkey 注入、signal 回调）全部由服务端渲染产出，本文件不写
任何 kickstart 内容——那是 `internal/render` 的职责，与 L1 是同一套
渲染代码，L2 只是第一次让它跑在真机（而不是断言字符串）上。

两条验收证据（计划任务 9 明确要求缺一不可）：
  1. 任务终态 `done`，且 events 序列反映真实的阶段推进
     （pre → packages → post → rebooting → initializing → done，
     `internal/core/model/signal.go` 的信号表决定了这个映射——契约
     实况订正见 `test_rhel_bios_install_completes` 的文档）。
  2. 用注入的 root 私钥 SSH 登进新装的机器，`/etc/os-release` 与期望
     家族一致，`systemctl is-system-running` 为 running/degraded。

两条证据的分工：第一条证明"服务端认为流程走完了"，第二条证明"装出来的
系统真的是好的、且 rootpw --lock + 公钥注入是唯一登录通路"——单独一条
都不够（计划原文的说明）。
"""

from __future__ import annotations

import time

import pytest

from lobertest.api import Console
from lobertest.envelope import APIError

pytestmark = pytest.mark.slow

# `tests/` 目录没有 `__init__.py`，不是一个可 import 的 Python
# package——本文件因此**不** `from tests.l2_install.conftest import
# ...`。所有跨 fixture 共享的东西（目标机的 MAC/SN/磁盘标识经
# `target_identity` fixture，起停目标机经 `target_launcher` fixture
# 返回的可调用对象，find_ip/ssh 方法挂在 `target_launcher()` 的返回值
# 上）都通过 fixture 访问（conftest.py 的 `TargetIdentity`/`TargetVM`/
# `target_launcher` 三处文档记录了这个决定，以及为什么"起目标机"不能
# 是一个普通 fixture 参数——那会导致目标机在任务建好之前就已经在跑）。

# 目标机从 QEMU 进程启动到发出 DHCP 请求的等待上限。qemu.py 的文档记录过
# BIOS 目标机实测能在这个量级内完成 PXE 链路的前几步（拿地址→TFTP→HTTP
# 菜单），给 3 分钟留出充分余量（远大于"几秒到十几秒"这个实测经验值）。
_DHCP_LEASE_TIMEOUT = 180

# 单家族墙钟预算（计划任务 9 明确的数字）。装机本身（booting 15 分钟 +
# packages 60 分钟 + post 20 分钟 + rebooting 8 分钟 + initializing 15
# 分钟的阶段预算表加起来远超 45 分钟，但那是"每阶段不超过多久"的上限，
# 不是"正常情况下会用多久"——实测 RHEL 系全流程通常在这个量级内。超过
# 45 分钟按失败处理，由 wait_task_state 的 timeout 参数直接落地。
_INSTALL_WALLCLOCK_BUDGET = 45 * 60

# 装完之后到 SSH 可登录之间还有 initializing 阶段的 firstboot 脚本要跑
# （systemctl enable + 首次触发），给一点缓冲时间，不是 done 状态出现的
# 瞬间机器就一定已经完成网络就位。
_POST_DONE_SSH_GRACE = 30


def _collect_failure_evidence(
    console: Console,
    task_id: str | None,
    *,
    target=None,
    vms_ssh=None,
) -> str:
    """尽力收集失败诊断所需的六类证据（第七类"目标机 overlay 保留不删"
    不在这里体现——那是"不要删磁盘文件"这个消极约束，不是一段要收集的
    文本），拼成一段文本用于失败时打印到测试输出。

    **可以工作在"从未起过目标机"的场景下**（本文件的类别 4 用例就是
    这种场景：ISO 路径错误导致导入 job 直接失败，从未建过任务、从未
    起过 QEMU）——`target`/`vms_ssh` 为 `None` 时对应的证据栏位显式写
    "不适用"而不是抛异常，这是刻意设计：先用一个不需要真实目标机的
    廉价场景验证"证据收集本身是完整的、不会在证据缺失时崩溃"，再拿去
    应对类别 1 那种耗时 20 分钟到 1 小时的真实装机失败——若证据收集
    代码本身有 bug，应该在秒级的类别 4 用例里发现，不是在真实装机跑了
    大半小时失败之后才发现"抓证据的代码也崩了"。
    """
    parts = [f"=== 失败诊断证据（task_id={task_id}） ==="]

    if task_id is not None:
        try:
            events = console.events(task_id)
            parts.append(f"--- events()（{len(events)} 条） ---")
            for e in events:
                parts.append(
                    f"  seq={e.get('seq')} phase={e.get('phase')} "
                    f"level={e.get('level')} rc={e.get('rc')} msg={e.get('message')!r}"
                )
        except Exception as exc:  # noqa: BLE001 —— 证据收集本身失败不能掩盖原始失败
            parts.append(f"--- events() 获取失败：{exc} ---")
    else:
        parts.append("--- events()：不适用（从未建立任务） ---")

    if target is not None:
        try:
            log_bytes = target.serial_log_path.read_bytes()
            parts.append(
                f"--- 目标机串口日志（{target.serial_log_path}，{len(log_bytes)} 字节） ---"
            )
            parts.append(log_bytes.decode("utf-8", "replace")[-4000:])
        except Exception as exc:  # noqa: BLE001
            parts.append(f"--- 读取串口日志失败：{exc} ---")
    else:
        parts.append("--- 目标机串口日志：不适用（从未起目标机） ---")

    if vms_ssh is not None:
        for label, cmd in [
            ("journalctl -u lober（最后 100 行）", "journalctl -u lober --no-pager -n 100"),
            ("journalctl -u dnsmasq（最后 50 行）", "journalctl -u dnsmasq --no-pager -n 50"),
            ("nginx access log（最后 50 行）", "tail -n 50 /var/log/nginx/lober-boot.log"),
        ]:
            try:
                result = vms_ssh.sudo(cmd)
                parts.append(f"--- {label} ---")
                parts.append(result.stdout)
            except Exception as exc:  # noqa: BLE001
                parts.append(f"--- {label} 获取失败：{exc} ---")
    else:
        parts.append("--- VM-S 侧日志（lober/dnsmasq/nginx）：不适用（无 SSH 连接） ---")

    return "\n".join(parts)


# ---------------------------------------------------------------------------
# 类别 4：指向不存在的 ISO → 状态判定为失败，且证据收集本身不崩溃。
# ---------------------------------------------------------------------------


def test_import_nonexistent_iso_fails_with_full_evidence(l2_console: Console) -> None:
    """`import_artifact` 指向一个 guest 上不存在的路径，导入 job 应以
    `failed` 收尾，且 `_collect_failure_evidence` 在"从未建立任务、从未
    起目标机"的场景下不抛异常、能正常产出诊断文本。

    这是计划任务 9 要求的"先跑类别 4"——不是因为这条路径本身特别重要，
    是因为失败路径的证据收集代码比成功路径更容易写漏，值得先用一个
    秒级场景验证一遍，再去跑一次成本高得多的真实装机（类别 1）。

    不需要 `target` fixture（不起 QEMU 目标机）：这条失败发生在导入
    job 尝试打开源文件那一步，早于建任务、早于任何机器引导，起目标机
    对这条路径没有意义，只会浪费内存名额。
    """
    with pytest.raises(APIError) as exc_info:
        artifact_id = l2_console.import_artifact(
            "/srv/lober/sources/does-not-exist.iso",
            "lobertest-l2-nonexistent-iso",
        )
        # 有些校验可能延迟到轮询阶段才暴露为 failed（异步 job），因此
        # 无论 import_artifact 本身是否已经同步报错，都尝试等一次 ready
        # ——两条路径（同步 4xx、异步 failed）都应该落进本函数的
        # pytest.raises 块里，因为 wait_artifact_ready 命中 failed 时
        # 会主动抛 APIError（api.py 的既有实现，不是本文件新增行为）。
        l2_console.wait_artifact_ready(artifact_id, timeout=60)

    evidence = _collect_failure_evidence(l2_console, None, target=None, vms_ssh=None)
    assert "不适用（从未建立任务）" in evidence
    assert "不适用（从未起目标机）" in evidence
    assert "不适用（无 SSH 连接）" in evidence

    print(f"\n{evidence}")
    print(f"\n预期失败已确认：{exc_info.value}")


# ---------------------------------------------------------------------------
# 类别 1：RHEL BIOS 真实装机（最小成功线）。
# ---------------------------------------------------------------------------


def test_rhel_bios_install_completes(
    l2_console: Console,
    l2_rhel_iso_guest_path: str,
    l2_artifact_reaper,
    target_identity,
    target_launcher,
    vms_ssh,
) -> None:
    """RHEL（OpenCloudOS）BIOS 家族真实装机，两条独立证据齐全。

    步骤与 L1 的 `test_family_full_lifecycle` 一致到"建任务"这一步
    （建 host 带磁盘清单 → 导入并等 ready → 建模板到 active → 建 hook
    并审批到 active → 建任务）。

    **必须先建完任务、再起目标机**（`target_launcher` 而不是
    `target`——见 conftest.py 里 `target_launcher` 的详细文档：本任务
    第一次真实装机踩到的时序坑，pytest 的普通 fixture 参数会在测试
    函数体开始之前就 setup，导致目标机在任务还不存在时就已经在问
    `/v1/boot`，服务端正确地不建任务、机器 fallback sanboot 失败卡死，
    机器不会自己重试）。`target_launcher()` 被调用之后，目标机的 iPXE
    ROM 自己发起 `/v1/boot`，链式引导最终打到已经存在的这个任务——
    测试代码此后要做的只是等待、观察、验证，装机本身的每一步 HTTP
    请求都是目标机自己发出的，本函数不再调用 `boot_menu`。

    建 host 用的 sn/mac/byIdPath 来自 `target_identity` fixture（不是
    本文件内联的字面量）——`target_launcher()` 起出来的目标机用的正是
    同一份 `target_identity`，两处从同一个来源取值，不可能漂移出
    不一致的值（conftest.py 的 `TargetIdentity` 类文档记录了这个设计
    决定的完整背景：早先的版本在两个文件各写一份字面量，本身就是一种
    重复维护的隐患）。
    """
    l2_console.cancel_active_tasks(target_identity.sn)

    host_id = l2_console.create_host(
        target_identity.sn,
        target_identity.mac,
        "x86_64",
        "bios",
        disks=[
            {
                "byIdPath": target_identity.disk_by_id,
                "sizeBytes": 21474836480,  # 20GiB，与 target_launcher 起
                # 出来的目标机磁盘（qemu.reset_overlay("20G")）是同一块
                # 盘的两种描述——前者是服务端拿去写进 kickstart 的
                # "声明值"，后者是 QEMU 实际创建的磁盘文件大小，两者必须
                # 一致：声明的容量比实际磁盘大会让 anaconda 的分区计划
                # （part / --grow）在真实磁盘上放不下而失败。
                "rotational": False,
                "model": "lobertest-virtual-disk",
            }
        ],
    )

    artifact_id = l2_console.import_artifact(l2_rhel_iso_guest_path, "lobertest-l2-rhel")
    l2_artifact_reaper(artifact_id)
    artifact = l2_console.wait_artifact_ready(artifact_id, timeout=600)
    assert artifact["osFamily"] == "rhel"

    template_id = l2_console.create_template(
        "lobertest-l2-rhel-bios-tpl",
        artifact_id,
        "",
        diskPolicy="first",
        onFailure="halt",
    )
    l2_console.set_template_state(template_id, "active", version=1)

    author_hook_id = l2_console.create_hook(
        "lobertest-l2-rhel-bios-hook",
        "pre",
        "#!/bin/bash\necho lobertest-l2-hook-ran\n",
    )
    l2_console.approve_hook(author_hook_id, role="hook-author")
    l2_console.approve_hook(author_hook_id, role="hook-approver")

    task_id = l2_console.create_task(host_id, template_id, {"hostname": "lobertest-l2-rhel"})

    # 任务此刻已经存在，服务端已经能对这个 MAC 的引导请求给出正确响应
    # ——现在才是启动目标机的正确时刻。
    target = target_launcher()

    try:
        task = l2_console.wait_task_state(task_id, "done", timeout=_INSTALL_WALLCLOCK_BUDGET)
    except Exception:
        evidence = _collect_failure_evidence(l2_console, task_id, target=target, vms_ssh=vms_ssh)
        print(f"\n{evidence}")
        raise

    # ---- 证据一：任务终态 + 阶段序列 ----
    assert task["state"] == "done"

    events = l2_console.events(task_id)
    phases_seen = [e["phase"] for e in events if e.get("phase")]
    # **实测契约（本任务第一次真实装机才能确认，写在这里而不是计划
    # 原文，因为计划原文的"booting → installing → rebooting →
    # initializing → done"是状态机的 State 值，不是事件的 Phase
    # 字段）**：`internal/core/model/task.go` 定义的 State 只有五个
    # （booting/installing/rebooting/initializing/done），但
    # `TaskEvent.Phase` 用的是更细的六个阶段名（pending/booting/pre/
    # packages/post/rebooting/initializing/done，`signal.go` 的信号
    # 映射表），其中 pre/packages/post 三者都映射到 State=installing、
    # 没有单独的 "installing" 阶段名——`StartBoot` 本身在正常成功路径
    # 上也不主动追加一条 phase=booting 的事件（`booting` 只出现在
    # `internal/statemachine/scan.go` 的超时扫描逻辑里）。所以这里按
    # **事件实际会出现的阶段名**断言顺序，而不是死抠计划原文的字面
    # 措辞：pre → packages → post → rebooting → initializing → done。
    expected_order = ["pre", "packages", "post", "rebooting", "initializing", "done"]
    seen_expected = [p for p in phases_seen if p in expected_order]
    # 断言"出现过的这些阶段名，相对顺序符合预期"，不要求恰好等于
    # expected_order（比如 pre 阶段内 pre-start/pre-done 两个事件都是
    # phase=pre，会重复出现，用 dict.fromkeys 去重后比较更稳）。
    deduped = list(dict.fromkeys(seen_expected))
    assert deduped == expected_order, (
        f"事件阶段顺序与预期不符：实际去重后是 {deduped}，"
        f"预期 {expected_order}；完整原始序列：{phases_seen}"
    )

    # ---- 证据二：SSH 登进新装的机器 ----
    target_ip = target.find_ip(vms_ssh, timeout=_DHCP_LEASE_TIMEOUT)
    time.sleep(_POST_DONE_SSH_GRACE)

    with target.ssh(vms_ssh, target_ip, timeout=30) as target_ssh:
        os_release = target_ssh.run("cat /etc/os-release")
        os_release.check("读取 /etc/os-release")
        assert "opencloudos" in os_release.stdout.lower() or "rhel" in os_release.stdout.lower(), (
            f"/etc/os-release 内容与期望的 RHEL 系家族不符：{os_release.stdout!r}"
        )

        system_state = target_ssh.run("systemctl is-system-running")
        state_text = system_state.stdout.strip()
        assert state_text in ("running", "degraded"), (
            f"systemctl is-system-running 返回 {state_text!r}，"
            f"不在可接受的 running/degraded 范围内"
        )
        if state_text == "degraded":
            failed_units = target_ssh.run("systemctl --failed --no-legend")
            print(f"\n[注意] 系统状态是 degraded，失败的 unit：\n{failed_units.stdout}")
