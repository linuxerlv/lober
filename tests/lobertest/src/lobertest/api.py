"""控制台 API 客户端。

一个 Console 类覆盖 L1 需要的全部端点，所有错误经 envelope.APIError 统一
抛出。**不 import qemu**——这是 L1 在不起目标机时可跑的编译期保证，任何
后续修改本文件的人都不应该破坏这一条。

人面（console）与机器面（gateway）是两个截然不同的协议：人面在
`/console/v1` 下，响应统一包裹 `{code, data, message}`，需要 Bearer token；
机器面（`boot_menu`/`answer_file`）是纯文本裸响应，无认证，走独立的
base_url（`docs/contracts/http-machine.md`）。Console 内部同时持有两个
base_url，按方法各自选择。

测试账号的用户名前缀与密码从 `lobertest.config` import（`TEST_USER_PREFIX`、
`TEST_PASSWORD`），不在本文件内联字面量——`deploy.py` 生成账号哈希时用的是
同一份常量。两边一旦各自记一份、又漂移出不同的值，症状是全部 L1 用例 401，
且两处代码各自看起来都对（任务 6）。
"""

from __future__ import annotations

import time
from typing import Any

import requests

from lobertest.config import TEST_PASSWORD, TEST_USER_PREFIX
from lobertest.envelope import APIError, unwrap

_DEFAULT_ARTIFACT_TIMEOUT = 600


class Console:
    """控制台 API 客户端，覆盖人面与机器面全部端点。"""

    def __init__(self, base_url: str, *, gateway_base_url: str | None = None, timeout: int = 30) -> None:
        self._base_url = base_url.rstrip("/")
        self._gateway_base_url = gateway_base_url.rstrip("/") if gateway_base_url else None
        self._timeout = timeout
        self._session = requests.Session()
        self._tokens: dict[str, str] = {}

    # ------------------------------------------------------------------
    # 认证与底层调用
    # ------------------------------------------------------------------

    def token(self, role: str) -> str:
        """按角色取登录 token，同一 role 缓存后不重复登录。"""
        if role in self._tokens:
            return self._tokens[role]

        username = f"{TEST_USER_PREFIX}{role}"
        resp = self._session.post(
            f"{self._base_url}/auth/login",
            json={"username": username, "password": TEST_PASSWORD},
            timeout=self._timeout,
        )
        data = unwrap(resp, f"login as {role}")
        tok = data["accessToken"]
        self._tokens[role] = tok
        return tok

    def call(self, method: str, path: str, *, role: str = "admin", **kw: Any) -> Any:
        """对人面端点发一次请求并解包 Envelope。

        `path` 是 `/console/v1` 之后的部分（如 `/hosts`）。`role` 决定用
        哪个身份的 token，默认 admin。`kw` 原样转发给 requests（json=/
        params= 等）。
        """
        headers = kw.pop("headers", {})
        headers = {**headers, "Authorization": f"Bearer {self.token(role)}"}
        resp = self._session.request(
            method,
            f"{self._base_url}{path}",
            headers=headers,
            timeout=self._timeout,
            **kw,
        )
        return unwrap(resp, f"{method} {path}")

    def _gateway_get(self, path: str, *, params: dict[str, Any] | None = None) -> str:
        """对机器面端点发一次请求，返回纯文本正文（不套 Envelope，无认证）。"""
        if self._gateway_base_url is None:
            raise ValueError(
                "Console 未配置 gateway_base_url，无法调用机器面端点；"
                "构造时请传入 gateway_base_url 参数"
            )
        resp = self._session.get(
            f"{self._gateway_base_url}{path}",
            params=params,
            timeout=self._timeout,
        )
        resp.raise_for_status()
        return resp.text

    # ------------------------------------------------------------------
    # host
    # ------------------------------------------------------------------

    def create_host(
        self,
        sn: str,
        mac: str,
        arch: str,
        firmware: str,
        disks: list[dict] | None = None,
    ) -> str:
        body: dict[str, Any] = {"sn": sn, "mac": mac, "arch": arch, "firmware": firmware}
        if disks is not None:
            body["disks"] = disks
        data = self.call("POST", "/hosts", json=body)
        return data["id"]

    def ensure_host(self, sn: str, mac: str, arch: str, firmware: str) -> str:
        """按 sn 查主机清单，存在则返回其 id，不存在则创建。"""
        hosts = self.call("GET", "/hosts", params={"q": sn})
        for host in hosts:
            if host.get("sn") == sn:
                return host["id"]
        return self.create_host(sn, mac, arch, firmware)

    # ------------------------------------------------------------------
    # artifact-set
    # ------------------------------------------------------------------

    def import_artifact(self, source_path: str, name: str) -> str:
        data = self.call(
            "POST",
            "/artifact-sets",
            json={"sourcePath": source_path, "name": name},
        )
        return data["id"]

    def wait_artifact_ready(self, artifact_id: str, timeout: int = _DEFAULT_ARTIFACT_TIMEOUT) -> dict:
        """轮询到 ready；命中 failed 立即抛（不等超时），把 FailReason 放进消息。"""
        start = time.monotonic()
        while True:
            items = self.call("GET", "/artifact-sets")
            item = next((i for i in items if i["id"] == artifact_id), None)
            if item is None:
                raise APIError(
                    "wait artifact ready",
                    200,
                    f"制品集 {artifact_id} 不在列表中",
                )

            state = item.get("state")
            if state == "ready":
                return item
            if state == "failed":
                raise APIError(
                    "wait artifact ready",
                    200,
                    f"制品集 {artifact_id} 导入失败：{item.get('failReason', '')}",
                )

            elapsed = time.monotonic() - start
            if elapsed >= timeout:
                raise TimeoutError(
                    f"等待制品集 {artifact_id} 变为 ready 超时"
                    f"（已等待 {elapsed:.1f} 秒，超时阈值 {timeout} 秒，当前状态 {state}）"
                )
            time.sleep(1)

    # ------------------------------------------------------------------
    # template
    # ------------------------------------------------------------------

    def create_template(self, name: str, family: str, body: str, **fields: Any) -> str:
        """建模板。`family` 对应 `POST /templates` 入参的 `artifactSetId`
        （契约里模板没有独立的 family 字段，家族是通过它引用的制品集间接
        决定的；调用点用 family 命名是为了让「这是哪个家族的模板」在调用
        处自解释，本方法负责把它转成契约要求的字段名）。`**fields` 转发
        `diskPolicy`/`onFailure`/`hookRefs`/`kernelArgs`/`requiredFormFields`
        等其余入参。
        """
        payload: dict[str, Any] = {
            "name": name,
            "artifactSetId": family,
            "answerTemplate": body,
            **fields,
        }
        data = self.call("POST", "/templates", json=payload)
        return data["id"]

    def set_template_state(self, template_id: str, state: str, version: int) -> dict:
        """流转模板状态，必带 version（乐观锁）。缺失时在发请求前明确报错。"""
        if version is None:
            raise ValueError(
                "set_template_state 缺少 version（乐观锁字段）；"
                "该字段必填，缺失会让服务端把请求当成语义不完整而不应静默发出"
            )
        return self.call(
            "POST",
            f"/templates/{template_id}/state",
            json={"state": state, "version": version},
        )

    def activate_template(self, template: dict) -> dict:
        """便利包装：从 template dict 里取 version，流转到 active。"""
        return self.set_template_state(template["id"], "active", version=template["version"])

    # ------------------------------------------------------------------
    # hook
    # ------------------------------------------------------------------

    def create_hook(self, name: str, phase: str, script: str) -> str:
        data = self.call(
            "POST",
            "/hooks",
            json={"name": name, "stage": phase, "body": script},
        )
        return data["id"]

    def approve_hook(self, hook_id: str, *, role: str = "hook-approver") -> dict:
        """审批 hook。hook:approve 权限码只有 hook-approver 与 admin 有，
        默认用 hook-approver 身份。"""
        return self.call("POST", f"/hooks/{hook_id}/approve", role=role, json={})

    # ------------------------------------------------------------------
    # task
    # ------------------------------------------------------------------

    def create_task(self, host_sn: str, template_id: str, form_data: dict) -> str:
        """建任务。前置不自动清活跃任务——清理是 cancel_active_tasks 的事。

        契约要求入参是主机清单 id（约定 3），本方法签名的 host_sn 参数
        直接转发为 hostId：L1 的调用点在建任务前已经拿到主机清单 id
        （ensure_host 的返回值），调用方按「host 标识」的语义传入即可，
        字段名沿用计划契约里的 host_sn 不改名。
        """
        data = self.call(
            "POST",
            "/tasks",
            json={"hostId": host_sn, "templateId": template_id, "formData": form_data},
        )
        return data["id"]

    def wait_task_state(self, task_id: str, want: str, timeout: int) -> dict:
        """轮询任务状态直到变为 want。

        契约里没有单条 `GET /tasks/{id}` 端点（只有列表 `GET /tasks` 与
        `GET /tasks/{id}/events`），因此按 id 过滤列表结果——与
        `cancel_active_tasks` 走同一个列表端点是刻意的一致。
        """
        start = time.monotonic()
        last_state = None
        while True:
            tasks = self.call("GET", "/tasks")
            task = next((t for t in tasks if t.get("id") == task_id), None)
            if task is None:
                raise APIError(
                    "wait task state",
                    200,
                    f"任务 {task_id} 不在列表中",
                )
            last_state = task.get("state")
            if last_state == want:
                return task

            elapsed = time.monotonic() - start
            if elapsed >= timeout:
                raise TimeoutError(
                    f"等待任务 {task_id} 状态变为 {want} 超时"
                    f"（已等待 {elapsed:.1f} 秒，超时阈值 {timeout} 秒，"
                    f"当前状态 {last_state}）"
                )
            time.sleep(1)

    def cancel_active_tasks(self, sn: str) -> list[str]:
        """取消该 SN 名下全部非终态任务，返回被取消的任务 id 列表。"""
        terminal_states = {"done", "failed"}
        tasks = self.call("GET", "/tasks", params={"sn": sn})
        cancelled = []
        for task in tasks:
            if task.get("state") in terminal_states:
                continue
            self.call("POST", f"/tasks/{task['id']}/cancel", json={})
            cancelled.append(task["id"])
        return cancelled

    def events(self, task_id: str) -> list[dict]:
        return self.call("GET", f"/tasks/{task_id}/events")

    # ------------------------------------------------------------------
    # 机器面（经 gateway，非 console）
    # ------------------------------------------------------------------

    def boot_menu(self, sn: str, mac: str, arch: str = "i386", plat: str = "pcbios") -> str:
        """取 iPXE 菜单。arch 默认 i386——真实 32 位 BIOS iPXE 的上报值。"""
        return self._gateway_get(
            "/v1/boot",
            params={"sn": sn, "mac": mac, "arch": arch, "plat": plat},
        )

    def answer_file(self, task_id: str, token: str, filename: str) -> str:
        return self._gateway_get(f"/v1/answer/{task_id}/{token}/{filename}")
