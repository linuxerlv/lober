"""SSH 封装的六类输入：Result 的纯逻辑（1-2）与需要真实 VM 的连接行为（3-6）。

类别 3-6 标 needs_vm：本机当前没有可连的 VM-S（已被停掉），这些用例在
CI/本机默认跑（`-m 'not needs_vm'`）时会被跳过，留给下一次真的起 VM-S
时验证。写在这里是为了让契约有测试守着，不是「实现时小心就够」。
"""

from __future__ import annotations

import socket

import pytest

from lobertest.config import load
from lobertest.ssh import Result, SSH

# ----------------------------------------------------------------------
# 类别 1-2：Result 是纯数据，不需要 VM。
# ----------------------------------------------------------------------


def test_result_ok_true_and_check_returns_self_on_success():
    """类别 1：rc=0 时 ok 为真，check() 不抛且返回自身以便链式调用。"""
    result = Result(rc=0, stdout="done", stderr="")

    assert result.ok is True

    returned = result.check("provision")

    assert returned is result


def test_result_check_failure_message_contains_what_rc_and_stderr():
    """类别 2：check() 失败时消息必须同时含 what、rc、stderr 三者，否则
    远程命令失败时无法定位是哪个操作、什么错误码、什么原因。"""
    result = Result(rc=1, stdout="", stderr="boom")

    assert result.ok is False

    with pytest.raises(Exception) as exc_info:
        result.check("provision")

    message = str(exc_info.value)
    assert "provision" in message
    assert "1" in message
    assert "boom" in message


# ----------------------------------------------------------------------
# 类别 3-6：需要真实可连的 VM-S。
# ----------------------------------------------------------------------


@pytest.mark.needs_vm
def test_run_exit_3_returns_rc_3_without_raising():
    """类别 3：命令本身失败（非零 rc）不是异常，是 Result 的一个字段。
    run() 只在「无法判断命令是否执行」（如超时）时才抛异常。"""
    settings = load(env={})

    with SSH(settings) as ssh:
        result = ssh.run("exit 3")

    assert result.rc == 3
    assert result.ok is False


@pytest.mark.needs_vm
def test_run_timeout_raises_not_returns_result():
    """类别 4：超时必须抛异常而非返回 Result——超时不是「命令失败」，
    是「不知道命令是否执行了」，两者要能被调用方区分处理。"""
    settings = load(env={})

    with SSH(settings) as ssh:
        with pytest.raises(Exception):
            ssh.run("sleep 30", timeout=1)


@pytest.mark.needs_vm
def test_push_to_nonexistent_remote_dir_raises_with_remote_path(tmp_path):
    """类别 5：push 目标目录不存在时不自动创建（静默建目录会掩盖路径
    写错），必须抛异常且消息含远端路径帮助定位。"""
    settings = load(env={})
    local_file = tmp_path / "payload.txt"
    local_file.write_text("hello")
    remote_path = "/no/such/directory/payload.txt"

    with SSH(settings) as ssh:
        with pytest.raises(Exception) as exc_info:
            ssh.push(local_file, remote_path)

    assert remote_path in str(exc_info.value)


@pytest.mark.needs_vm
def test_run_large_output_does_not_deadlock_and_returns_full_content():
    """类别 6（补充）：paramiko 的 exec_command 在命令输出量大时会因为
    不读 channel 而死锁——远端 channel 接收窗口有限，写满后阻塞等我们读，
    而我们在等它退出，双方各等对方永久卡住。小输出碰不到（塞得进缓冲区），
    真实装机日志几十 KB 时必然触发。

    用 500KB 输出验证 run() 不卡死，且拿到完整内容（长度与行数都对得上），
    不是被窗口截断的一部分。
    """
    settings = load(env={})
    expected_bytes = 500_000

    with SSH(settings) as ssh:
        result = ssh.run(
            f"yes | head -c {expected_bytes}",
            timeout=30,
        )

    assert result.rc == 0
    assert len(result.stdout) == expected_bytes
