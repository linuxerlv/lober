"""Console API 客户端测试。

全部用 requests-mock 拦截 HTTP 传输层，绝不连真实 VM——这是 L1 在不起
目标机时可跑的编译期保证的运行期对应物（api.py 的 import 列表里不能出现
qemu，见任务 3 的设计约束）。

requests-mock 而不是手写 unittest.mock.patch("requests.Session.request")
的理由：它挂在 requests 的 transport adapter 层，被测代码调用的是真实的
requests.Session/Response 对象与真实的 URL 拼装、query 编码、JSON 序列化
逻辑，只是最终的网络 I/O 被换成本地应答——这样测试验证的是 Console 真的
拼出了正确的方法/路径/参数，而不是「我 mock 了什么就断言什么」（跌进
test-driven-development 技能提醒的「测试 mock 行为而非真实行为」反模式）。
"""

from __future__ import annotations

import pytest
import requests_mock

from lobertest.api import Console
from lobertest.envelope import APIError

CONSOLE_BASE = "http://console.test/console/v1"
GATEWAY_BASE = "http://gateway.test"


def envelope_ok(data):
    return {"code": 0, "data": data, "message": ""}


def envelope_err(code_num, message, http_code=None):
    return {"code": code_num, "data": None, "message": message}


@pytest.fixture
def console():
    return Console(CONSOLE_BASE, gateway_base_url=GATEWAY_BASE)


@pytest.fixture
def m():
    with requests_mock.Mocker() as mocker:
        yield mocker


def stub_login(m, role_to_token: dict[str, str] | None = None):
    """给 /auth/login 注册应答：按请求体里的 username 返回对应角色的 token。

    真实端点只按 (username, password) 认证；Console 用 `lobertest-<role>`
    这个约定（任务 6 的账号命名规则）算出 username，这里原样接受任意
    username 返回一个可辨认来源角色的 token，断言的是 Console 按角色
    缓存/选择 token 这一层逻辑，不是服务端认证逻辑本身。
    """
    role_to_token = role_to_token or {}

    def callback(request, context):
        body = request.json()
        username = body.get("username")
        token = role_to_token.get(username, f"tok-{username}")
        context.status_code = 200
        return envelope_ok({"accessToken": token})

    m.post(f"{CONSOLE_BASE}/auth/login", json=callback)


# ---------------------------------------------------------------------------
# 类别 1：token(role) 同一 role 调两次 -> 只发一次登录请求
# ---------------------------------------------------------------------------


def test_token_same_role_twice_logs_in_once(console, m):
    stub_login(m)

    tok1 = console.token("admin")
    tok2 = console.token("admin")

    assert tok1 == tok2 == "tok-lobertest-admin"
    login_calls = [r for r in m.request_history if r.path.endswith("/auth/login")]
    assert len(login_calls) == 1


def test_token_different_roles_each_login_once(console, m):
    stub_login(m)

    admin_tok = console.token("admin")
    viewer_tok = console.token("viewer")
    admin_tok_again = console.token("admin")

    assert admin_tok == "tok-lobertest-admin"
    assert viewer_tok == "tok-lobertest-viewer"
    assert admin_tok_again == admin_tok
    login_calls = [r for r in m.request_history if r.path.endswith("/auth/login")]
    assert len(login_calls) == 2


# ---------------------------------------------------------------------------
# 类别 2：wait_artifact_ready 遇 failed -> 抛 APIError 且消息含 FailReason
# ---------------------------------------------------------------------------


def test_wait_artifact_ready_failed_raises_with_fail_reason(console, m):
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/artifact-sets",
        json=envelope_ok(
            [
                {
                    "id": "as-1",
                    "name": "rhel9",
                    "osFamily": "rhel",
                    "osVersion": "9",
                    "arch": "x86_64",
                    "firmware": "any",
                    "sourceIsoSha256": "",
                    "state": "failed",
                    "manifestSha256": "",
                    "failReason": "识别不出 OS 家族",
                    "paths": {"kernel": "", "initrd": [], "repo": "", "extra": {}},
                }
            ]
        ),
    )

    with pytest.raises(APIError) as exc_info:
        console.wait_artifact_ready("as-1", timeout=5)

    assert "识别不出 OS 家族" in str(exc_info.value)


def test_wait_artifact_ready_failed_raises_immediately_not_after_timeout(console, m, monkeypatch):
    """failed 命中时必须立即抛，不等超时——用极大的 timeout 反证。"""
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/artifact-sets",
        json=envelope_ok(
            [
                {
                    "id": "as-1",
                    "name": "rhel9",
                    "osFamily": "rhel",
                    "osVersion": "9",
                    "arch": "x86_64",
                    "firmware": "any",
                    "sourceIsoSha256": "",
                    "state": "failed",
                    "manifestSha256": "",
                    "failReason": "boom",
                    "paths": {"kernel": "", "initrd": [], "repo": "", "extra": {}},
                }
            ]
        ),
    )

    sleep_calls = []
    monkeypatch.setattr("lobertest.api.time.sleep", lambda s: sleep_calls.append(s))

    with pytest.raises(APIError):
        console.wait_artifact_ready("as-1", timeout=99999)

    assert sleep_calls == []


# ---------------------------------------------------------------------------
# 类别 3：wait_artifact_ready 一直 importing 到超时 -> 抛超时错误且消息含
# 已等待秒数
# ---------------------------------------------------------------------------


def test_wait_artifact_ready_times_out_with_elapsed_seconds(console, m, monkeypatch):
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/artifact-sets",
        json=envelope_ok(
            [
                {
                    "id": "as-1",
                    "name": "rhel9",
                    "osFamily": "rhel",
                    "osVersion": "9",
                    "arch": "x86_64",
                    "firmware": "any",
                    "sourceIsoSha256": "",
                    "state": "importing",
                    "manifestSha256": "",
                    "failReason": "",
                    "paths": {"kernel": "", "initrd": [], "repo": "", "extra": {}},
                }
            ]
        ),
    )
    monkeypatch.setattr("lobertest.api.time.sleep", lambda s: None)

    fake_clock = iter([0.0, 0.5, 1.0, 10.0])
    monkeypatch.setattr("lobertest.api.time.monotonic", lambda: next(fake_clock))

    with pytest.raises(TimeoutError) as exc_info:
        console.wait_artifact_ready("as-1", timeout=1)

    assert "10" in str(exc_info.value) or "1" in str(exc_info.value)


def test_wait_artifact_ready_default_timeout_is_600(console, m, monkeypatch):
    """契约要求默认超时 600 秒（真 ISO 抽取实测 16.8 秒，Ubuntu 镜像更久）。"""
    import inspect

    sig = inspect.signature(console.wait_artifact_ready)
    assert sig.parameters["timeout"].default == 600


# ---------------------------------------------------------------------------
# 类别 4：任一方法收到 4xx Envelope -> 抛 APIError 且 code 正确
# ---------------------------------------------------------------------------


def test_create_host_4xx_raises_api_error_with_code(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/hosts",
        status_code=400,
        json=envelope_err(1001, "MAC 格式不合法"),
    )

    with pytest.raises(APIError) as exc_info:
        console.create_host("SN1", "aa:bb:cc:dd:ee:ff", "x86_64", "bios")

    assert exc_info.value.http_status == 400


def test_create_task_4xx_active_task_exists(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/tasks",
        status_code=409,
        json=envelope_err(1006, "该主机已存在活跃任务"),
    )

    with pytest.raises(APIError) as exc_info:
        console.create_task("SN1", "tpl-1", {})

    assert exc_info.value.http_status == 409


def test_approve_hook_4xx_forbidden(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/hooks/hook-1/approve",
        status_code=403,
        json=envelope_err(1003, "审批人不能等于作者"),
    )

    with pytest.raises(APIError) as exc_info:
        console.approve_hook("hook-1")

    assert exc_info.value.http_status == 403


# ---------------------------------------------------------------------------
# 类别 5：set_template_state 缺 version -> 明确报错而非发出非法请求
# ---------------------------------------------------------------------------


def test_set_template_state_requires_version(console, m):
    with pytest.raises(TypeError):
        console.set_template_state("tpl-1", "active")  # type: ignore[call-arg]


def test_set_template_state_none_version_raises_before_request(console, m):
    with pytest.raises(ValueError):
        console.set_template_state("tpl-1", "active", version=None)  # type: ignore[arg-type]

    assert len(m.request_history) == 0


def test_set_template_state_sends_version_in_body(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/templates/tpl-1/state",
        json=envelope_ok({"id": "tpl-1", "state": "active", "version": 3}),
    )

    result = console.set_template_state("tpl-1", "active", version=3)

    assert result["state"] == "active"
    sent = m.request_history[-1].json()
    assert sent == {"state": "active", "version": 3}


# ---------------------------------------------------------------------------
# 补充覆盖：host / artifact-set / template / hook / task 主链路方法各至少
# 一条正向用例，确认端点路径、方法、角色与响应解包都对得上（不是仅靠五类
# 硬性输入就能兜住 21 个方法的路径拼装正确性）。
# ---------------------------------------------------------------------------


def test_create_host_success_returns_id(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/hosts",
        json=envelope_ok({"id": "host-1", "sn": "SN1", "mac": "aa:bb:cc:dd:ee:ff"}),
    )

    host_id = console.create_host("SN1", "aa:bb:cc:dd:ee:ff", "x86_64", "bios")

    assert host_id == "host-1"
    sent = m.request_history[-1].json()
    assert sent["sn"] == "SN1"
    assert sent["arch"] == "x86_64"
    assert sent["firmware"] == "bios"
    assert "disks" not in sent or sent["disks"] is None


def test_create_host_with_disks(console, m):
    stub_login(m)
    m.post(f"{CONSOLE_BASE}/hosts", json=envelope_ok({"id": "host-1"}))

    disks = [{"byIdPath": "/dev/disk/by-id/wwn-0x1", "sizeBytes": 1, "rotational": False, "model": "x"}]
    console.create_host("SN1", "aa:bb:cc:dd:ee:ff", "x86_64", "bios", disks=disks)

    sent = m.request_history[-1].json()
    assert sent["disks"] == disks


def test_ensure_host_creates_when_absent(console, m):
    stub_login(m)
    m.get(f"{CONSOLE_BASE}/hosts", json=envelope_ok([]))
    m.post(f"{CONSOLE_BASE}/hosts", json=envelope_ok({"id": "host-1", "sn": "SN1"}))

    host_id = console.ensure_host("SN1", "aa:bb:cc:dd:ee:ff", "x86_64", "bios")

    assert host_id == "host-1"


def test_ensure_host_returns_existing_without_creating(console, m):
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/hosts",
        json=envelope_ok([{"id": "host-existing", "sn": "SN1", "mac": "aa:bb:cc:dd:ee:ff"}]),
    )
    post_stub = m.post(f"{CONSOLE_BASE}/hosts", json=envelope_ok({"id": "should-not-be-used"}))

    host_id = console.ensure_host("SN1", "aa:bb:cc:dd:ee:ff", "x86_64", "bios")

    assert host_id == "host-existing"
    assert not post_stub.called


def test_import_artifact_success(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/artifact-sets",
        json=envelope_ok({"id": "as-1", "name": "rhel9", "state": "importing"}),
    )

    artifact_id = console.import_artifact("/data/iso/rhel9.iso", "rhel9")

    assert artifact_id == "as-1"
    sent = m.request_history[-1].json()
    assert sent == {"sourcePath": "/data/iso/rhel9.iso", "name": "rhel9"}


def test_import_artifact_without_netboot_path_omits_field(console, m):
    """不传 netboot_path 时请求体不带 netbootPath 键（不是发一个空字符串）
    ——这与既有的 test_import_artifact_success 是同一断言，本用例独立
    写出是为了在下面 with_netboot_path 的对照用例旁边显式说明这条约定，
    不依赖读者去翻另一个测试函数才能看到对比。"""
    stub_login(m)
    m.post(f"{CONSOLE_BASE}/artifact-sets", json=envelope_ok({"id": "as-1"}))

    console.import_artifact("/data/iso/rhel9.iso", "rhel9")

    sent = m.request_history[-1].json()
    assert "netbootPath" not in sent


def test_import_artifact_with_netboot_path_sends_field(console, m):
    """任务 8 实测发现：Debian 家族的 netinst ISO 自带 initrd 是光驱版，
    不具备网络安装能力，导入会以 ErrInitrdCDOnly 失败
    （internal/imports/job.go:234-237）；必须额外传 netboot_path，服务端
    才会用它覆盖 ISO 内的那份 initrd（console.ImportSource.NetbootPath，
    internal/console/artifact.go 的 createArtifactSetRequest.NetbootPath
    字段）。任务 3 完成 Console.import_artifact 时没有暴露这个参数——
    当时这是纯理论知识（旧 vmlab 的 api.py 从未真连过 VM-S），任务 8
    第一次真跑 Debian 主链路才需要用到它。"""
    stub_login(m)
    m.post(f"{CONSOLE_BASE}/artifact-sets", json=envelope_ok({"id": "as-1"}))

    console.import_artifact(
        "/srv/lober/sources/debian-netinst.iso",
        "lobertest-debian",
        netboot_path="/srv/lober/sources/debian-13-netboot.tar.gz",
    )

    sent = m.request_history[-1].json()
    assert sent == {
        "sourcePath": "/srv/lober/sources/debian-netinst.iso",
        "name": "lobertest-debian",
        "netbootPath": "/srv/lober/sources/debian-13-netboot.tar.gz",
    }


def test_wait_artifact_ready_polls_then_returns_ready_item(console, m, monkeypatch):
    stub_login(m)
    states = iter(["importing", "ready"])

    def callback(request, context):
        state = next(states)
        return envelope_ok(
            [
                {
                    "id": "as-1",
                    "name": "rhel9",
                    "osFamily": "rhel",
                    "osVersion": "9",
                    "arch": "x86_64",
                    "firmware": "any",
                    "sourceIsoSha256": "abc",
                    "state": state,
                    "manifestSha256": "def",
                    "failReason": "",
                    "paths": {"kernel": "k", "initrd": ["i"], "repo": "r", "extra": {}},
                }
            ]
        )

    m.get(f"{CONSOLE_BASE}/artifact-sets", json=callback)
    monkeypatch.setattr("lobertest.api.time.sleep", lambda s: None)

    result = console.wait_artifact_ready("as-1", timeout=5)

    assert result["state"] == "ready"


def test_create_template_success(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/templates",
        json=envelope_ok({"id": "tpl-1", "name": "rhel-default", "state": "draft", "version": 1}),
    )

    tpl_id = console.create_template(
        "rhel-default", "as-rhel9", "answer body", diskPolicy="largest"
    )

    assert tpl_id == "tpl-1"
    sent = m.request_history[-1].json()
    assert sent["name"] == "rhel-default"
    assert sent["artifactSetId"] == "as-rhel9"
    assert sent["diskPolicy"] == "largest"


def test_activate_template_uses_version_from_dict(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/templates/tpl-1/state",
        json=envelope_ok({"id": "tpl-1", "state": "active", "version": 2}),
    )

    template = {"id": "tpl-1", "version": 2, "state": "draft"}
    result = console.activate_template(template)

    assert result["state"] == "active"
    sent = m.request_history[-1].json()
    assert sent == {"state": "active", "version": 2}


def test_create_hook_success(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/hooks",
        json=envelope_ok({"id": "hook-1", "name": "partition", "state": "draft"}),
    )

    hook_id = console.create_hook("partition", "pre", "#!/bin/bash\necho hi")

    assert hook_id == "hook-1"


def test_approve_hook_defaults_to_hook_approver_role(console, m):
    """约束：approve_hook 默认用 hook-approver 身份。"""
    login_calls = []

    def login_callback(request, context):
        body = request.json()
        login_calls.append(body["username"])
        return envelope_ok({"accessToken": f"tok-{body['username']}"})

    m.post(f"{CONSOLE_BASE}/auth/login", json=login_callback)
    m.post(f"{CONSOLE_BASE}/hooks/hook-1/approve", json=envelope_ok({"id": "hook-1", "state": "active"}))

    console.approve_hook("hook-1")

    assert login_calls == ["lobertest-hook-approver"]
    approve_call = [r for r in m.request_history if r.path.endswith("/hooks/hook-1/approve")][0]
    assert approve_call.headers["Authorization"] == "Bearer tok-lobertest-hook-approver"


def test_approve_hook_accepts_explicit_role_override(console, m):
    login_calls = []

    def login_callback(request, context):
        body = request.json()
        login_calls.append(body["username"])
        return envelope_ok({"accessToken": f"tok-{body['username']}"})

    m.post(f"{CONSOLE_BASE}/auth/login", json=login_callback)
    m.post(f"{CONSOLE_BASE}/hooks/hook-1/approve", json=envelope_ok({"id": "hook-1", "state": "active"}))

    console.approve_hook("hook-1", role="admin")

    assert login_calls == ["lobertest-admin"]


def test_create_task_success(console, m):
    stub_login(m)
    m.post(
        f"{CONSOLE_BASE}/tasks",
        json=envelope_ok({"id": "task-1", "state": "pending"}),
    )

    task_id = console.create_task("SN1", "tpl-1", {"hostname": "h1"})

    assert task_id == "task-1"
    sent = m.request_history[-1].json()
    assert sent == {"hostId": "SN1", "templateId": "tpl-1", "formData": {"hostname": "h1"}}


def test_wait_task_state_reaches_want(console, m, monkeypatch):
    stub_login(m)
    states = iter(["pending", "booting", "done"])

    def callback(request, context):
        return envelope_ok([{"id": "task-1", "state": next(states)}])

    m.get(f"{CONSOLE_BASE}/tasks", json=callback)
    monkeypatch.setattr("lobertest.api.time.sleep", lambda s: None)

    result = console.wait_task_state("task-1", "done", timeout=5)

    assert result["state"] == "done"


def test_wait_task_state_missing_task_raises(console, m):
    stub_login(m)
    m.get(f"{CONSOLE_BASE}/tasks", json=envelope_ok([]))

    with pytest.raises(APIError):
        console.wait_task_state("task-missing", "done", timeout=1)


def test_cancel_active_tasks_returns_cancelled_ids(console, m):
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/tasks",
        json=envelope_ok(
            [
                {"id": "task-1", "hostSn": "SN1", "state": "pending"},
                {"id": "task-2", "hostSn": "SN1", "state": "booting"},
                {"id": "task-3", "hostSn": "SN1", "state": "done"},
            ]
        ),
    )
    m.post(f"{CONSOLE_BASE}/tasks/task-1/cancel", json=envelope_ok({"id": "task-1", "state": "failed"}))
    m.post(f"{CONSOLE_BASE}/tasks/task-2/cancel", json=envelope_ok({"id": "task-2", "state": "failed"}))

    cancelled = console.cancel_active_tasks("SN1")

    assert sorted(cancelled) == ["task-1", "task-2"]


def test_cancel_active_tasks_none_active_returns_empty(console, m):
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/tasks",
        json=envelope_ok([{"id": "task-1", "hostSn": "SN1", "state": "done"}]),
    )

    cancelled = console.cancel_active_tasks("SN1")

    assert cancelled == []


def test_events_returns_list(console, m):
    stub_login(m)
    m.get(
        f"{CONSOLE_BASE}/tasks/task-1/events",
        json=envelope_ok([{"taskId": "task-1", "seq": 1, "phase": "pre", "level": "info", "message": "x"}]),
    )

    events = console.events("task-1")

    assert len(events) == 1
    assert events[0]["seq"] == 1


# ---------------------------------------------------------------------------
# 机器面：boot_menu / answer_file 走独立 base_url，纯文本响应，不套 Envelope
# ---------------------------------------------------------------------------


def test_boot_menu_defaults_to_i386_arch(console, m):
    m.get(
        f"{GATEWAY_BASE}/v1/boot",
        text="#!ipxe\nsanboot --no-describe --drive 0x80\n",
    )

    menu = console.boot_menu("SN1", "aa:bb:cc:dd:ee:ff")

    assert menu.startswith("#!ipxe")
    sent_qs = m.request_history[-1].qs
    assert sent_qs["arch"] == ["i386"]
    assert sent_qs["plat"] == ["pcbios"]


def test_boot_menu_no_task_payload_verbatim(console, m):
    """约定 2 的三行固定载荷，逐字符匹配（HTTP 契约要求）。"""
    payload = "#!ipxe\necho lober: no task for SN1\nsanboot --no-describe --drive 0x80\n"
    m.get(f"{GATEWAY_BASE}/v1/boot", text=payload)

    menu = console.boot_menu("SN1", "aa:bb:cc:dd:ee:ff")

    assert menu == payload


def test_boot_menu_does_not_use_console_auth(console, m):
    """机器面无认证，boot_menu 不应该触发登录请求。"""
    m.get(f"{GATEWAY_BASE}/v1/boot", text="#!ipxe\n")

    console.boot_menu("SN1", "aa:bb:cc:dd:ee:ff")

    login_calls = [r for r in m.request_history if r.path.endswith("/auth/login")]
    assert login_calls == []


def test_answer_file_plain_text_no_envelope(console, m):
    m.get(
        f"{GATEWAY_BASE}/v1/answer/task-1/tok-abc/ks.cfg",
        text="#version=RHEL9\nauth --enableshadow\n",
    )

    body = console.answer_file("task-1", "tok-abc", "ks.cfg")

    assert body.startswith("#version=RHEL9")


def test_console_without_gateway_base_url_raises_on_boot_menu():
    console_no_gateway = Console(CONSOLE_BASE)

    with pytest.raises(ValueError):
        console_no_gateway.boot_menu("SN1", "aa:bb:cc:dd:ee:ff")


# ---------------------------------------------------------------------------
# call() 的 role 参数与 APIError 消息里的原始响应体
# ---------------------------------------------------------------------------


def test_call_defaults_to_admin_role(console, m):
    login_calls = []

    def login_callback(request, context):
        body = request.json()
        login_calls.append(body["username"])
        return envelope_ok({"accessToken": f"tok-{body['username']}"})

    m.post(f"{CONSOLE_BASE}/auth/login", json=login_callback)
    m.get(f"{CONSOLE_BASE}/hosts", json=envelope_ok([]))

    console.call("GET", "/hosts")

    assert login_calls == ["lobertest-admin"]


def test_call_non_json_response_raises_api_error_with_body_fragment(console, m):
    stub_login(m)
    m.get(f"{CONSOLE_BASE}/hosts", text="<html>502 Bad Gateway</html>", status_code=502)

    with pytest.raises(APIError) as exc_info:
        console.call("GET", "/hosts")

    assert "502 Bad Gateway" in str(exc_info.value)
