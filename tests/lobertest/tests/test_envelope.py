"""envelope.unwrap() 的四类输入：合法响应、缺 data、4xx 错误、非 JSON 响应体。"""

from __future__ import annotations

import json

import pytest
import requests

from lobertest.envelope import APIError, unwrap


def _response(status_code: int, body: bytes, *, url: str = "https://console.example/console/v1/hosts") -> requests.Response:
    """构造一个真实的 requests.Response，不 mock——unwrap 读的是响应对象本身。"""
    resp = requests.Response()
    resp.status_code = status_code
    resp.url = url
    resp._content = body
    return resp


def test_unwrap_2xx_valid_envelope_returns_data():
    """2xx + 合法 Envelope → 返回 data 字段。"""
    body = json.dumps({"data": {"id": "host-1"}}).encode("utf-8")
    resp = _response(200, body)

    result = unwrap(resp, "create host")

    assert result == {"id": "host-1"}


def test_unwrap_2xx_missing_data_field_raises_api_error():
    """2xx 但响应体没有 data 字段 → APIError（服务端违反了 Envelope 约定）。"""
    body = json.dumps({"something_else": True}).encode("utf-8")
    resp = _response(200, body)

    with pytest.raises(APIError) as exc_info:
        unwrap(resp, "create host")

    assert exc_info.value.http_status == 200
    assert "create host" in str(exc_info.value)


def test_unwrap_4xx_error_envelope_raises_api_error_with_code_from_body():
    """4xx + 错误 Envelope → APIError，code 取自响应体。"""
    body = json.dumps(
        {"error": {"code": "sn_already_active", "message": "该 SN 已有活跃任务"}}
    ).encode("utf-8")
    resp = _response(409, body)

    with pytest.raises(APIError) as exc_info:
        unwrap(resp, "create task")

    err = exc_info.value
    assert err.code == "sn_already_active"
    assert err.http_status == 409
    assert "create task" in str(err)


def test_unwrap_non_json_body_raises_api_error_with_raw_text_fragment():
    """响应体不是 JSON（如 nginx 502 页面）→ APIError，消息含原始文本片段。"""
    body = b"<html><body><h1>502 Bad Gateway</h1></body></html>"
    resp = _response(502, body)

    with pytest.raises(APIError) as exc_info:
        unwrap(resp, "wait artifact ready")

    message = str(exc_info.value)
    assert "wait artifact ready" in message
    assert "502 Bad Gateway" in message


def test_unwrap_message_truncates_long_body_to_500_chars():
    """消息里带的原始响应体要截断到前 500 字符，不能把整个大响应体塞进异常消息。"""
    long_body = ("x" * 10_000).encode("utf-8")
    resp = _response(500, long_body)

    with pytest.raises(APIError) as exc_info:
        unwrap(resp, "import artifact")

    message = str(exc_info.value)
    # 原始响应体不是合法 JSON,所以走非 JSON 分支;截断到 500 字符意味着
    # 消息里不会出现完整的一万个 x。
    assert "x" * 10_000 not in message
    assert "x" * 500 in message
