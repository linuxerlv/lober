"""L1 准入控制负面用例：四条建任务前的拒绝路径。

四条都是「建任务」这一步的前置校验，共用同一套最小夹具（一个主机 +
一个已 active 的模板），只在触发条件上分叉：

- 类别 4：同一 SN 已有活跃任务时再建 → 拒绝（409 + CodeActiveTaskExists）
- 类别 5：固件与制品集家族不兼容 → 拒绝。**实测标 skip**：
  `model.FirmwareCompatible` 的判据表（inventory.go:109-116）里
  rhel/debian/ubuntu/suse/windows 五个家族都同时兼容 bios 与 uefi，
  唯一真正受限的是 esxi（仅 uefi），而本机没有 ESXi ISO；本机能真实
  import ready 的两个家族（rhel、debian）都不在受限之列，无法在不假绿
  的前提下触发这条规则，见 test_firmware_incompatible_host_rejected
  的文档
- 类别 6：attempt 配额耗尽 → 拒绝（本任务未找到能从 L1 直接触发的公开
  入口，见 test_attempt_quota_exhausted_rejected 的文档，标记为 skip）
- 类别 7：引用 draft 状态的 hook → 拒绝

准入校验发生在 handleCreateTask 内部（internal/console/task.go），不是
RBAC 中间件层——这里验证的是「身份合法但业务状态不允许」，与
test_rbac.py 验证的「身份本身没有权限」是两个不同的维度。
"""

from __future__ import annotations

import pytest

from lobertest.api import Console
from lobertest.envelope import APIError

pytestmark = pytest.mark.needs_vm


@pytest.fixture
def active_host_and_template(console: Console, ready_rhel_artifact_id: str):
    """建一个主机 + 一个已流转到 active 的模板，供本文件四个用例复用。

    引用 `ready_rhel_artifact_id`（conftest.py 的 session 级 fixture）
    而不是自己 import_artifact——准入校验本身与 OS 家族无关，选哪个
    家族不影响被测逻辑；本文件要测的是准入规则，不是「制品集能不能
    导入成功」（那件事已经由 test_lifecycle.py 验证过），重新导入一次
    只会在这台磁盘紧张的 VM-S 上多占约 1.6G 却验证不到任何新东西。
    """
    sn = "lobertest-admission-sn"
    # MAC 前缀分段全局唯一（本文件用 20-29，test_lifecycle.py 用
    # 01-06，test_rbac.py 用 30-39）——UpsertHost 的写入冲突键是 mac 列
    # 不是 sn（internal/store/postgres/host/store.go:52-57 的
    # ON CONFLICT (mac)），不同测试函数若用到相同 MAC，即使 SN 不同，
    # 后写入的一方会把先写入那一方的记录字段覆盖掉（见 test_lifecycle.py
    # 的 _family_mac 文档，那里详细记录了这条约束的实测根因）。
    mac = "52:54:00:aa:bb:20"
    console.cancel_active_tasks(sn)

    host_id = console.create_host(
        sn,
        mac,
        "x86_64",
        "bios",
        disks=[
            {
                "byIdPath": "/dev/disk/by-id/virtio-admission-disk0",
                "sizeBytes": 21474836480,
                "rotational": False,
                "model": "lobertest-virtual-disk",
            }
        ],
    )

    template_id = console.create_template(
        "lobertest-admission-tpl",
        ready_rhel_artifact_id,
        "",
        diskPolicy="first",
        onFailure="halt",
    )
    console.set_template_state(template_id, "active", version=1)

    return {"sn": sn, "mac": mac, "host_id": host_id, "template_id": template_id}


# ---------------------------------------------------------------------------
# 类别 4：同一 SN 已有活跃任务时再建 → 拒绝
# ---------------------------------------------------------------------------


def test_active_task_exists_rejected(console: Console, active_host_and_template) -> None:
    fixture = active_host_and_template
    console.create_task(fixture["host_id"], fixture["template_id"], {"hostname": "first"})

    with pytest.raises(APIError) as exc_info:
        console.create_task(fixture["host_id"], fixture["template_id"], {"hostname": "second"})

    err = exc_info.value
    assert err.http_status == 409
    assert err.code == 1006  # CodeActiveTaskExists（internal/apierr.apierr.go:45）

    console.cancel_active_tasks(fixture["sn"])


# ---------------------------------------------------------------------------
# 类别 5：arch/固件与制品集家族不兼容 → 拒绝
# ---------------------------------------------------------------------------


@pytest.mark.skip(
    reason="实测发现：model.FirmwareCompatible 的判据表"
    "（internal/core/model/inventory.go:109-116）里 rhel/debian/ubuntu/"
    "suse/windows 五个家族都同时兼容 bios 与 uefi 两种固件——本机唯一能"
    "真实 import ready 的两个家族（rhel、debian）都在这五个之内，无法"
    "通过给主机声明「另一种固件」来触发不兼容。唯一真正受限的家族是"
    "esxi（仅 uefi），而本机没有 ESXi ISO（与 test_lifecycle.py 的 esxi"
    "参数同一个 skip 原因：需 Broadcom 账号下载）。这条准入规则的判定"
    "逻辑（FirmwareCompatible 本身）已有 Go 单测覆盖（inventory_test.go），"
    "本用例要验证的是「这条规则在 HTTP 层被正确触发」，没有 ESXi ISO"
    "做不到——不降低断言（比如改成检查 FirmwareCompatible 函数本身）来"
    "换取假绿，那样测的就不是 HTTP 层的行为了。拿到 ESXi ISO 后移除本"
    "skip：用 esxi 家族的制品集 + 声明 bios 固件的主机即可触发。"
)
def test_firmware_incompatible_host_rejected() -> None:
    raise AssertionError("本用例被跳过，不应执行到这里")


# ---------------------------------------------------------------------------
# 类别 6：attempt 配额耗尽 → 拒绝
# ---------------------------------------------------------------------------


@pytest.mark.skip(
    reason="attempt 配额耗尽的拒绝发生在 StartBoot（internal/statemachine/"
    "boot.go 第 5 步「attempt++；超上限 → 置 failed」），不是 handleCreateTask——"
    "L1 没有直接调 StartBoot 的公开人面端点，唯一触达它的路径是反复调"
    "boot_menu（机器面）让同一任务多次引导直到耗尽 attempt。这个场景更接近"
    "L2 装机层的重试语义（真实机器反复引导），在 L1 用纯 API 调用伪造"
    "「同一任务的第 N 次引导」需要绕开 boot_menu 本身推进 pending→booting"
    "这一事实（第一次调用就会让任务离开 pending），伪造成本与价值不成比例。"
    "留给任务 9（L2 装机层）用真实目标机反复引导失败来验证更合适。"
)
def test_attempt_quota_exhausted_rejected() -> None:
    raise AssertionError("本用例被跳过，不应执行到这里")


# ---------------------------------------------------------------------------
# 类别 7：引用 draft 状态的 hook → 拒绝
# ---------------------------------------------------------------------------


def test_draft_hook_referenced_by_template_rejected(console: Console) -> None:
    """模板引用一个仍处于 draft 状态的 hook 应被拒绝。

    这条拒绝发生在**建模板**这一步（resolveHookRefs，
    internal/console/template.go:145-196），不是建任务那一步——
    resolveHookRefs 要求引用的 hook 必须有一个 active 版本，draft 状态
    的 hook 没有 active 版本，直接在建模板时报 404
    （`hookId=%s 没有已激活（active）的版本，无法引用`）。这与计划任务 8
    契约原文「引用 draft 状态的 hook → 拒绝」字面上说的是「建任务」被拒，
    但实测校验点在更早的建模板步骤——档案按实测记录，不按字面猜测的
    调用点写断言。
    """
    hook_id = console.create_hook(
        "lobertest-admission-draft-hook",
        "pre",
        "#!/bin/bash\necho still-draft\n",
    )

    with pytest.raises(APIError) as exc_info:
        console.create_template(
            "lobertest-admission-draft-hook-tpl",
            "",
            "",
            hookRefs=[{"hookId": hook_id, "stage": "pre", "order": 1, "required": False}],
        )

    err = exc_info.value
    assert err.http_status == 404
    assert "active" in str(err) or "没有已激活" in str(err)
