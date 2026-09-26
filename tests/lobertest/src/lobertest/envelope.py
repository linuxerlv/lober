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

    if not resp.ok:
        error = body.get("error") if isinstance(body, dict) else None
        code = error.get("code") if isinstance(error, dict) else None
        message = error.get("message") if isinstance(error, dict) else None
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
