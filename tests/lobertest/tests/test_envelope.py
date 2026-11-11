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
    """2xx + 合法 Envelope → 返回 data 字段。

    真实后端总是显式写出 code 字段（Go 的 int 零值在 JSON 里就是 0，
    不是缺失——internal/apierr.Envelope 的 Code 字段没有
    omitempty，序列化时永远出现），本用例的 fixture 因此显式带
    "code": 0，不是省略它——省略这个字段在真实响应体里不会发生，
    这不是本用例要验证的边界。"""
    body = json.dumps({"code": 0, "data": {"id": "host-1"}, "message": ""}).encode("utf-8")
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
    """4xx + 错误 Envelope → APIError，code 取自响应体顶层 code 字段。

    真实 Envelope 是扁平的 {"code":整数,"data":null,"message":"..."}
    （internal/apierr.Envelope 的 JSON 定义，Code 字段直接挂在顶层，
    没有嵌套的 error 对象——本用例原来断言的 {"error":{"code":...}}
    形状是凭空编的契约，从未对照过真实响应体（task 8 第一次真连 VM-S
    时，用 curl -X POST /auth/login -d '{}' 拿到的真实响应体是
    {"code":1009,"data":null,"message":"username：不能为空；password：不能为空"}，
    确认了这一点）。"""
    body = json.dumps({"code": 1006, "data": None, "message": "该主机已存在一个未结束的任务"}).encode(
        "utf-8"
    )
    resp = _response(409, body)

    with pytest.raises(APIError) as exc_info:
        unwrap(resp, "create task")

    err = exc_info.value
    assert err.code == 1006
    assert err.http_status == 409
    assert "create task" in str(err)


def test_unwrap_200_with_nonzero_code_raises_api_error():
    """HTTP 200 但 Envelope.code 非零 → APIError。

    这是本次修复要补的类别：本仓库的 handleCreateTask
    （internal/console/task.go:244-248）对固件/家族不兼容这类业务冲突
    故意用 `apierr.New(http.StatusOK, apierr.CodeConflict, ...)`
    ——HTTP 200 承载业务失败码是这个后端的既有约定，不是本用例的臆造：
    `apierr.Fail` 原样把 *Error 携带的 HTTPStatus 写到响应行
    （internal/apierr/gin.go:70-73，`c.JSON(httpStatus, env)`）。旧实现
    只按 `resp.ok`（2xx 与否）判定成功/失败，会把这种响应误判为成功
    并把 data=null 当作正常结果返回——对应 L1 准入用例「arch 不兼容 →
    拒绝」如果不修这里会假绿：create_task 不会抛，返回值是 None。"""
    body = json.dumps(
        {"code": 1005, "data": None, "message": "该主机上报的固件模式与制品集要求的 OS 家族不兼容，无法建任务"}
    ).encode("utf-8")
    resp = _response(200, body)

    with pytest.raises(APIError) as exc_info:
        unwrap(resp, "create task")

    err = exc_info.value
    assert err.code == 1005
    assert err.http_status == 200
    assert "不兼容" in str(err)


def test_unwrap_200_zero_code_still_succeeds():
    """回归护栏：修复「200 但 code 非零要抛」之后，正常的 200+code=0 响应
    不能被误伤——真实成功响应的 code 字段是整数 0（CodeOK），不是缺失
    这个字段。"""
    body = json.dumps({"code": 0, "data": {"id": "host-1"}, "message": ""}).encode("utf-8")
    resp = _response(200, body)

    result = unwrap(resp, "create host")

    assert result == {"id": "host-1"}


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
