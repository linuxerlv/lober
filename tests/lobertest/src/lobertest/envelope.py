"""Envelope 解包与错误码表。

叶子模块，不 import 工程内任何其他模块。L1 的负面用例要断言错误码，这批
断言不该依赖 API 客户端的具体方法，所以解包逻辑单独成模块。
"""

from __future__ import annotations

from typing import Any

import requests

_BODY_TRUNCATE_LEN = 500


class APIError(Exception):
    """控制台响应不满足 Envelope 约定，或服务端返回了错误 Envelope。

    消息里必须带原始响应体（截断到前 500 字符），否则错误无法定位。
    """

    def __init__(self, what: str, http_status: int, message: str, *, code: str | None = None):
        self.what = what
        self.http_status = http_status
        self.code = code
        self.message = message
        super().__init__(
            f"{what} 失败：HTTP {http_status}"
            + (f"，code={code}" if code else "")
            + f"：{message}"
        )


def _truncate(body: str) -> str:
    if len(body) <= _BODY_TRUNCATE_LEN:
        return body
    return body[:_BODY_TRUNCATE_LEN]


def unwrap(resp: requests.Response, what: str) -> Any:
    """解出 Envelope 的 data 字段。

    成功（2xx 且响应体是带 data 字段的合法 JSON）时返回 data。
    其余情况——非 2xx、data 缺失、响应体不是 JSON——统一抛 APIError，
    消息里带原始响应体（截断到前 500 字符）帮助定位。

    `what` 是调用方给的语义标签（如 "create host"），进异常消息帮助定位
    这次解包失败发生在哪个操作上。
    """
    raw_text = resp.text
    truncated = _truncate(raw_text)

    try:
        body = resp.json()
    except ValueError:
        raise APIError(
            what,
            resp.status_code,
            f"响应体不是合法 JSON：{truncated!r}",
        ) from None

    # 判定成败的唯一依据是 Envelope.code（顶层整数字段，0 即成功），不是
    # HTTP 状态码。真实后端的 Envelope 是扁平形态
    # {"code":整数,"data":...,"message":"..."}（internal/apierr.Envelope
    # 的 JSON 定义，没有嵌套的 error 对象），且该后端故意让部分业务冲突
    # 携带 HTTP 200（internal/console/task.go 的固件/家族不兼容判定用
    # apierr.New(http.StatusOK, apierr.CodeConflict, ...)，apierr.Fail
    # 原样把这个状态码写到响应行）。只按 resp.ok 判断会把「200 但
    # code=1005」这种响应误判为成功（task 8 第一次真连 VM-S 才发现：
    # 旧实现从未见过真实响应体，contract 是凭空编的 {"error":{"code":..}}
    # 嵌套形状）。
    code = body.get("code") if isinstance(body, dict) else None
    if code != 0:
        message = body.get("message") if isinstance(body, dict) else None
        raise APIError(
            what,
            resp.status_code,
            message or f"响应体：{truncated}",
            code=code,
        )

    if not isinstance(body, dict) or "data" not in body:
        raise APIError(
            what,
            resp.status_code,
            f"响应缺少 data 字段，响应体：{truncated}",
        )

    return body["data"]
