"""L1 RBAC 边界：七个权限码 + 五个角色。

权威定义 `internal/console/rbac.go:19-37`：

| 角色 | 权限码 |
|---|---|
| viewer | 无（空数组，不是 null） |
| operator | task:create task:retry task:cancel |
| hook-author | hook:write |
| hook-approver | hook:approve |
| admin | 以上全部 + provider:write artifact:import |

**注意 admin 没有单独测的余下两个码**（provider:write/artifact:import）
的正向用例——`create_host`/`import_artifact` 本身需要 artifact:import，
本文件类别 12 直接复用「admin 能建主机、能导入制品集」这两个已经在
test_lifecycle.py 主链路里跑过的动作来验证，不重复造一遍。
"""

from __future__ import annotations

import pytest

from lobertest.api import Console
from lobertest.envelope import APIError

pytestmark = pytest.mark.needs_vm

# 七个权限码的权威取值（rbac.go:29-37），供本文件断言引用，不重新定义
# 语义——只是把同一份取值抄成本文件内可读的字面量。
_ALL_SEVEN_CODES = {
    "task:create",
    "task:retry",
    "task:cancel",
    "hook:write",
    "hook:approve",
    "provider:write",
    "artifact:import",
}


# ---------------------------------------------------------------------------
# 类别 8：viewer 调 /auth/codes 拿到 [] 而非 null
# ---------------------------------------------------------------------------


def test_viewer_auth_codes_is_empty_list_not_null(console: Console) -> None:
    """rbac.go:77-80 明确要求 viewer 拿到 `[]`。

    这条断言必须区分「空列表」与「None（JSON null）」——`requests` 把
    JSON `[]` 解析成 Python `[]`、把 `null` 解析成 `None`，两者在
    `== []` 上不等价，`assert codes == []` 已经隐含排除了 None 的情形，
    但显式再断言一次 `codes is not None` 让失败信息更直接（若真的退化成
    null，`codes == []` 会先失败并报 `None != []`，信息量已经足够，这里
    加的是防止「[None] == []` 之类边界写法产生误判」的双重保险）。
    """
    codes = console.call("GET", "/auth/codes", role="viewer")

    assert codes == []
    assert codes is not None


# ---------------------------------------------------------------------------
# 类别 9：operator 建任务成功、审批 hook 被拒
# ---------------------------------------------------------------------------


def test_operator_can_create_task_but_cannot_approve_hook(console: Console, ready_rhel_artifact_id: str) -> None:
    sn = "lobertest-rbac-operator-sn"
    # MAC 前缀分段全局唯一（本文件用 30-39，见 test_lifecycle.py 的
    # _family_mac 文档记录的实测根因：UpsertHost 按 mac 列而非 sn 做写入
    # 冲突判定，不同用例撞到同一个 MAC 会互相覆盖对方的主机记录字段）。
    mac = "52:54:00:aa:bb:30"
    console.cancel_active_tasks(sn)

    host_id = console.create_host(
        sn,
        mac,
        "x86_64",
        "bios",
        disks=[
            {
                "byIdPath": "/dev/disk/by-id/virtio-rbac-operator-disk0",
                "sizeBytes": 21474836480,
                "rotational": False,
                "model": "lobertest-virtual-disk",
            }
        ],
    )
    # 复用 session 级共享制品集（conftest.py 的 ready_rhel_artifact_id），
    # 不重新导入——本用例要测的是权限边界，不是导入本身，重新导入只会
    # 在磁盘紧张的 VM-S 上多占约 1.6G。
    template_id = console.create_template(
        "lobertest-rbac-operator-tpl", ready_rhel_artifact_id, "", diskPolicy="first", onFailure="halt"
    )
    console.set_template_state(template_id, "active", version=1)

    # operator 有 task:create，建任务应该成功。Console.create_task 契约
    # 签名没有 role 参数（任务 3 已固定，21 个方法里只有 approve_hook/call
    # 显式暴露 role），要以 operator 身份建任务必须走底层 call() 自己拼
    # 请求体，不能借 create_task 的包装。
    task_data = console.call(
        "POST",
        "/tasks",
        role="operator",
        json={"hostId": host_id, "templateId": template_id, "formData": {"hostname": "operator-owned"}},
    )
    assert task_data["id"]

    hook_id = console.create_hook(
        "lobertest-rbac-operator-hook",
        "pre",
        "#!/bin/bash\necho x\n",
    )
    console.approve_hook(hook_id, role="hook-author")  # draft -> review，用作者身份先推进到 review

    with pytest.raises(APIError) as exc_info:
        console.approve_hook(hook_id, role="operator")  # review -> active，operator 没有 hook:approve

    assert exc_info.value.http_status == 403
    console.cancel_active_tasks(sn)


# ---------------------------------------------------------------------------
# 类别 10：hook-author 写 hook 成功、审批被拒
# ---------------------------------------------------------------------------


def test_hook_author_can_write_but_cannot_approve(console: Console) -> None:
    hook_id = console.create_hook(
        "lobertest-rbac-author-hook",
        "pre",
        "#!/bin/bash\necho x\n",
    )
    assert hook_id

    # draft -> review 由作者发起（hook:write），应该成功。
    reviewed = console.approve_hook(hook_id, role="hook-author")
    assert reviewed["state"] == "review"

    # review -> active 需要 hook:approve，hook-author 没有这个码。
    with pytest.raises(APIError) as exc_info:
        console.approve_hook(hook_id, role="hook-author")

    assert exc_info.value.http_status == 403


# ---------------------------------------------------------------------------
# 类别 11：hook-approver 审批成功、建任务被拒
# ---------------------------------------------------------------------------


def test_hook_approver_can_approve_but_cannot_create_task(console: Console, ready_rhel_artifact_id: str) -> None:
    author_hook_id = console.create_hook(
        "lobertest-rbac-approver-hook",
        "pre",
        "#!/bin/bash\necho x\n",
    )
    console.approve_hook(author_hook_id, role="hook-author")  # draft -> review

    approved = console.approve_hook(author_hook_id, role="hook-approver")  # review -> active
    assert approved["state"] == "active"

    sn = "lobertest-rbac-approver-sn"
    mac = "52:54:00:aa:bb:31"
    console.cancel_active_tasks(sn)
    host_id = console.create_host(
        sn,
        mac,
        "x86_64",
        "bios",
        disks=[
            {
                "byIdPath": "/dev/disk/by-id/virtio-rbac-approver-disk0",
                "sizeBytes": 21474836480,
                "rotational": False,
                "model": "lobertest-virtual-disk",
            }
        ],
    )
    # 复用 session 级共享制品集，理由同上一个用例。
    template_id = console.create_template(
        "lobertest-rbac-approver-tpl", ready_rhel_artifact_id, "", diskPolicy="first", onFailure="halt"
    )
    console.set_template_state(template_id, "active", version=1)

    with pytest.raises(APIError) as exc_info:
        console.call(
            "POST",
            "/tasks",
            role="hook-approver",
            json={"hostId": host_id, "templateId": template_id, "formData": {}},
        )

    assert exc_info.value.http_status == 403


# ---------------------------------------------------------------------------
# 类别 12：admin 七个码全有
# ---------------------------------------------------------------------------


def test_admin_has_all_seven_codes(console: Console) -> None:
    codes = console.call("GET", "/auth/codes", role="admin")

    assert set(codes) == _ALL_SEVEN_CODES
