"""控制台 API 客户端。

**全部经真实 HTTP（nginx 反代 + TLS 终结），不写数据库。** 这是本测试床与
scripts/e2e*.sh 的关键差别：那七个脚本用 psql 直接 UPDATE artifact_sets 与
templates 到目标状态，于是「导入从不写 arch/firmware」「模板无法激活」
「hook 正文从不落盘」这三个真实缺口被完整掩盖了几个月。

请求经 VMServer 的 curl 发出（在 guest 内部），不用 requests 直连——理由见
server.py 的模块注释。
"""

from __future__ import annotations

import json
import shlex
import time
from dataclasses import dataclass, field
from typing import Any

from vmlab.config import SETTINGS
from vmlab.server import VMServer


class APIError(RuntimeError):
    """业务错误码非 0，或 HTTP 层失败。"""


@dataclass
class ConsoleAPI:
    """控制台 API 的薄封装。"""

    server: VMServer
    settings: Any = SETTINGS
    _tokens: dict[str, str] = field(default_factory=dict)

    # --- 底层 -----------------------------------------------------------

    def token(self, user: str = "admin") -> str:
        """取并缓存某个用户的 access token。"""
        if user in self._tokens:
            return self._tokens[user]

        c = self.settings.console
        if user == "admin":
            username, password = c.admin_user, c.admin_password
        elif user == "approver":
            username, password = c.approver_user, c.approver_password
        else:
            raise ValueError(f"未知用户 {user!r}（实验床只有 admin 与 approver）")

        body = json.dumps({"username": username, "password": password})
        cmd = (
            f"curl -s --cacert {c.ca_cert} -X POST {c.base_url}/auth/login "
            f"-H 'Content-Type: application/json' -d {shlex.quote(body)}"
        )
        raw = self.server.run(cmd).check("登录").stdout
        tok = _unwrap(raw, "登录")["accessToken"]
        self._tokens[user] = tok
        return tok

    def call(
        self,
        method: str,
        path: str,
        payload: dict | None = None,
        user: str = "admin",
    ) -> Any:
        """发一次请求，返回 data 字段。业务码非 0 抛 APIError。"""
        c = self.settings.console
        parts = [
            "curl -s",
            f"--cacert {c.ca_cert}",
            f"-X {method}",
            f"{c.base_url}{path}",
            f"-H 'Authorization: Bearer {self.token(user)}'",
            "-H 'Content-Type: application/json'",
        ]
        if payload is not None:
            parts.append(f"-d {shlex.quote(json.dumps(payload, ensure_ascii=False))}")
        raw = self.server.run(" ".join(parts), timeout=180).check(f"{method} {path}").stdout
        return _unwrap(raw, f"{method} {path}")

    # --- 制品集 ---------------------------------------------------------

    def import_artifact(self, source_path: str, name: str) -> str:
        """登记并触发真实导入（imports.Job.Run），返回制品集 ID。

        source_path 是 VM-S 内的路径。ISO 以只读 virtio 盘挂进 guest，所以传
        /dev/vdb 即可——iso9660.go 的 perCallOpenReaderAt 每次 ReadAt 独立开关
        文件、不校验「必须是普通文件」，块设备可直接读，省掉往 guest 里 scp
        一份 1.6G。
        """
        data = self.call("POST", "/artifact-sets", {"sourcePath": source_path, "name": name})
        return data["id"]

    def artifact(self, artifact_id: str) -> dict:
        for a in self.call("GET", "/artifact-sets"):
            if a["id"] == artifact_id:
                return a
        raise APIError(f"找不到制品集 {artifact_id}")

    def wait_artifact_ready(self, artifact_id: str, timeout: int = 600) -> dict:
        """轮询到 ready。

        failed 时把 failReason 带进异常：导入失败的原因只在那个字段里，不看它
        就只能看到一个无信息的超时（实测踩过——`open /dev/vdb: permission
        denied` 正是从这里读出来的）。
        """
        deadline = time.time() + timeout
        while time.time() < deadline:
            a = self.artifact(artifact_id)
            if a["state"] == "ready":
                return a
            if a["state"] == "failed":
                raise APIError(f"制品集 {artifact_id} 导入失败: {a.get('failReason')}")
            time.sleep(10)
        raise APIError(f"制品集 {artifact_id} 在 {timeout}s 内未就绪")

    # --- 主机 -----------------------------------------------------------

    def create_host(
        self, sn: str, mac: str, arch: str, firmware: str, rack: str = "vmlab"
    ) -> str:
        data = self.call(
            "POST",
            "/hosts",
            {"sn": sn, "mac": mac, "arch": arch, "firmware": firmware, "rack": rack},
        )
        return data["id"]

    def ensure_host(self, sn: str, mac: str, arch: str, firmware: str) -> str:
        """已存在就直接返回它的 id（重复 POST /hosts 会冲突）。"""
        for h in self.call("GET", "/hosts"):
            if h["sn"] == sn:
                return h["id"]
        return self.create_host(sn, mac, arch, firmware)

    # --- hook -----------------------------------------------------------

    def create_hook(
        self,
        name: str,
        stage: str,
        body: str,
        os_families: list[str] | None = None,
        required: bool = True,
        timeout_sec: int = 300,
    ) -> dict:
        return self.call(
            "POST",
            "/hooks",
            {
                "name": name,
                "description": f"vmlab 自动化测试床创建：{name}",
                "stage": stage,
                "osFamilies": os_families or [],
                "interpreter": "bash",
                "body": body,
                "required": required,
                "timeoutSec": timeout_sec,
            },
        )

    def activate_hook(self, hook_id: str) -> dict:
        """两段式审批：draft → review（作者 admin），review → active（approver）。

        SPEC §9.4 要求审批人不能等于作者——pre 阶段 hook 以 root 身份在分区
        之前运行，能力等同「擦掉这台机器所有盘」，双人复核是刻意设计。实测
        用同一个账号批两次会停在 review 不动。
        """
        self.call("POST", f"/hooks/{hook_id}/approve", {}, user="admin")
        return self.call("POST", f"/hooks/{hook_id}/approve", {}, user="approver")

    def hook(self, hook_id: str) -> dict:
        for h in self.call("GET", "/hooks"):
            if h["id"] == hook_id:
                return h
        raise APIError(f"找不到 hook {hook_id}")

    # --- 模板 -----------------------------------------------------------

    def create_template(
        self,
        name: str,
        artifact_set_id: str,
        answer_template: str,
        hook_refs: list[dict] | None = None,
        disk_policy: str = "by-hook",
        kernel_args: str = "",
    ) -> dict:
        return self.call(
            "POST",
            "/templates",
            {
                "name": name,
                "artifactSetId": artifact_set_id,
                "answerTemplate": answer_template,
                "hookRefs": hook_refs or [],
                "diskPolicy": disk_policy,
                "kernelArgs": kernel_args,
                "onFailure": "stop",
                "requiredFormFields": ["hostname"],
            },
        )

    def update_template(self, template_id: str, **fields) -> dict:
        """PUT 产出新版本，状态回到 draft（需重新激活）。"""
        return self.call("PUT", f"/templates/{template_id}", fields)

    def set_template_state(self, template_id: str, state: str, version: int) -> dict:
        """流转模板状态。version 参与乐观锁，不匹配返回 409。"""
        return self.call(
            "POST",
            f"/templates/{template_id}/state",
            {"state": state, "version": version},
        )

    def activate_template(self, template: dict) -> dict:
        """把刚创建/更新的模板推到 active。"""
        return self.set_template_state(template["id"], "active", template["version"])

    # --- 任务 -----------------------------------------------------------

    def tasks(self) -> list[dict]:
        return self.call("GET", "/tasks")

    def task(self, task_id: str) -> dict:
        for t in self.tasks():
            if t["id"] == task_id:
                return t
        raise APIError(f"找不到任务 {task_id}")

    def cancel_active_tasks(self) -> list[str]:
        """取消所有未结束任务。

        必要而非便利：同一 SN 只能有一个活跃任务（idx_tasks_sn_active 这个
        条件唯一索引），不清掉旧的就建不出新的。
        """
        cancelled = []
        for t in self.tasks():
            if t["state"] not in ("done", "failed"):
                self.call("POST", f"/tasks/{t['id']}/cancel", {})
                cancelled.append(t["id"])
        return cancelled

    def create_task(self, host_sn: str, template_id: str, form_data: dict) -> str:
        data = self.call(
            "POST",
            "/tasks",
            {"hostId": host_sn, "templateId": template_id, "formData": form_data},
        )
        return data["id"]

    def events(self, task_id: str) -> list[dict]:
        return self.call("GET", f"/tasks/{task_id}/events") or []

    def answer_file(self, task_id: str, token: str, filename: str) -> str:
        """取渲染出的应答文件（走机器面，无认证——机器在装机时也是这么取的）。"""
        url = f"{self.settings.console.gateway_url}/v1/answer/{task_id}/{token}/{filename}"
        return self.server.run(f"curl -s {shlex.quote(url)}").check("取应答文件").stdout

    def boot_menu(self, sn: str, mac: str, arch: str = "i386", plat: str = "pcbios") -> str:
        """模拟一次 iPXE 的 /v1/boot 请求，返回菜单文本。

        注意这会真的消耗一次 attempt 并推进任务状态——诊断时用，不要在真机
        引导前调用。
        """
        url = (
            f"{self.settings.console.gateway_url}/v1/boot"
            f"?sn={sn}&mac={mac}&uuid=&arch={arch}&plat={plat}"
        )
        return self.server.run(f"curl -s {shlex.quote(url)}").check("取引导菜单").stdout


def _unwrap(raw: str, what: str) -> Any:
    """解包统一响应体 {code, data, message}。"""
    try:
        body = json.loads(raw)
    except json.JSONDecodeError as exc:
        raise APIError(f"{what}: 响应不是合法 JSON（{exc}）: {raw[:300]}") from exc
    if body.get("code") != 0:
        raise APIError(f"{what}: code={body.get('code')} message={body.get('message')}")
    return body.get("data")
