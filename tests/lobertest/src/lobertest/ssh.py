"""VM-S 的 SSH 通道：paramiko 封装。

三层都要远程执行命令、读文件、推文件。`Result.check()` 把 rc 与 stderr
一起抛，是旧 harness（已删的 tools/vmlab/src/vmlab/server.py）唯一值得
原样保留的设计。

死锁规避（本模块最重要的约束）：SSH channel 的接收窗口有限，远端写满
后阻塞等我们读；如果调用方先把 stdout 读完再读 stderr（或反过来），
一旦远端把另一路写满，就会出现远端等我们读、我们在等远端退出的双向
阻塞，永久卡住。小输出碰不到（塞得进缓冲区），装机日志几十 KB 时必然
触发。`run()` 用 `select` 在同一个循环里交替抽取 stdout 与 stderr，
两路都排空、且 channel 报告退出状态就绪后才调用 `recv_exit_status()`。
"""

from __future__ import annotations

import select
import time
from dataclasses import dataclass
from pathlib import Path

import paramiko

from lobertest.config import Settings


@dataclass
class Result:
    """一次远程命令的结果。"""

    rc: int
    stdout: str
    stderr: str

    @property
    def ok(self) -> bool:
        return self.rc == 0

    def check(self, what: str) -> "Result":
        """rc 非零时抛异常，消息里带上 what/rc/stderr——远程命令失败最
        常见的原因是权限或路径，那些信息只在 stderr 里，且必须知道是
        哪个操作失败的才能定位。返回 self 以便链式调用。"""
        if self.rc != 0:
            raise RuntimeError(
                f"{what} 失败（rc={self.rc}）\n"
                f"stdout: {self.stdout.strip()}\n"
                f"stderr: {self.stderr.strip()}"
            )
        return self


class SSHTimeoutError(Exception):
    """命令在给定时限内既未完成也无法判断是否已执行。

    与「命令执行完成但 rc 非零」是两种不同的失败：后者用 Result 表达，
    前者必须是异常，调用方不能把「不知道」误当成「失败」处理。
    """


class SSH:
    """VM-S 的 SSH 连接。用作上下文管理器。"""

    def __init__(self, settings: Settings) -> None:
        self._settings = settings
        self._client: paramiko.SSHClient | None = None

    def __enter__(self) -> "SSH":
        client = paramiko.SSHClient()
        # 一次性测试 VM，host key 每次重建磁盘都会变，不值得维护
        # known_hosts——这不是生产连接。
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        client.connect(
            hostname=self._settings.ssh_host,
            port=self._settings.ssh_port,
            username=self._settings.ssh_user,
            key_filename=str(self._settings.ssh_key_path),
            timeout=20,
        )
        self._client = client
        return self

    def __exit__(self, *exc) -> None:
        if self._client is not None:
            self._client.close()
            self._client = None

    def run(self, cmd: str, timeout: int = 120) -> Result:
        """执行一条命令，cmd 原样交给远程 shell。

        超时抛 SSHTimeoutError（不返回 Result）；命令本身以非零 rc
        结束不算异常，那是 Result.rc 的正常取值。

        实现刻意不用 `exec_command()` 返回的三个 file-like 对象分别
        `.read()`——那等价于「先把 stdout 读完再读 stderr」，正是本模块
        文档开头描述的死锁模式。改用 channel 级别的 `select`，在同一
        个循环里交替抽取两路，任一路有数据就读，避免因为另一路的
        缓冲区满而卡住。
        """
        if self._client is None:
            raise RuntimeError("SSH 未连接（请用 with 语句）")

        transport = self._client.get_transport()
        assert transport is not None
        channel = transport.open_session(timeout=timeout)
        channel.exec_command(cmd)

        stdout_chunks: list[bytes] = []
        stderr_chunks: list[bytes] = []
        # 用截止时刻而不是每轮重置的固定 timeout：若按固定值传给每次
        # select，远端只要间歇性地吐一点数据（哪怕每次只吐 1 字节）
        # 就会不断重新扣满整段 timeout，永远不会真正超时。
        deadline = time.monotonic() + timeout

        try:
            while True:
                remaining = deadline - time.monotonic()
                if remaining <= 0:
                    raise SSHTimeoutError(f"命令超时（{timeout}s）：{cmd!r}")

                select.select([channel], [], [], remaining)

                drained_stdout = _drain(channel, stdout_chunks, is_stderr=False)
                drained_stderr = _drain(channel, stderr_chunks, is_stderr=True)

                # exit_status_ready() 为真之后，channel 里可能还剩没读完
                # 的数据（远端已退出但缓冲区还有内容），所以退出条件是
                # 「已就绪 且 两路都已排空」，不是「已就绪」本身。这里不
                # 依赖 select 的返回值来判断是否超时——那个判断交给上面
                # 的 remaining 检查，select 只负责让循环在有数据时立刻
                # 醒来，而不是每次都空转到 remaining 用完。
                if (
                    channel.exit_status_ready()
                    and not drained_stdout
                    and not drained_stderr
                    and not channel.recv_ready()
                    and not channel.recv_stderr_ready()
                ):
                    break
        finally:
            rc = channel.recv_exit_status() if channel.exit_status_ready() else -1
            channel.close()

        stdout = b"".join(stdout_chunks).decode("utf-8", "replace")
        stderr = b"".join(stderr_chunks).decode("utf-8", "replace")
        return Result(rc=rc, stdout=stdout, stderr=stderr)

    def sudo(self, cmd: str, timeout: int = 120) -> Result:
        """以 root 执行。guest 的测试用户有免密 sudo。"""
        return self.run(f"sudo {cmd}", timeout=timeout)

    def read_file(self, path: str, *, sudo: bool = True) -> str:
        """读远程文件。默认走 sudo——/srv/lober 与 /etc/lober 这类路径
        权限通常是 750/640，非 sudo 用户读不了。"""
        quoted = _shell_quote(path)
        runner = self.sudo if sudo else self.run
        result = runner(f"cat {quoted}")
        return result.check(f"读取 {path}").stdout

    def push(self, local: Path, remote: str) -> None:
        """经 SFTP 推送本地文件到远端路径。

        目标目录不存在时不自动创建——静默建目录会掩盖调用方把远端路径
        写错这类问题，宁可显式失败。
        """
        if self._client is None:
            raise RuntimeError("SSH 未连接（请用 with 语句）")

        sftp = self._client.open_sftp()
        try:
            try:
                sftp.put(str(local), remote)
            except (OSError, IOError) as exc:
                raise RuntimeError(
                    f"推送到 {remote} 失败（本地文件 {local}）：{exc}"
                ) from exc
        finally:
            sftp.close()


def _drain(channel, chunks: list[bytes], *, is_stderr: bool) -> bool:
    """把 channel 的 stdout 或 stderr 路里当前已就绪的数据非阻塞地全部
    读到 chunks，返回是否读到了任何字节。

    只搬已经在缓冲区里的数据，不做阻塞式 `.read()`——那正是死锁模式：
    在一路上阻塞等待，会让另一路的缓冲区被远端写满后卡住。
    """
    ready_fn = channel.recv_stderr_ready if is_stderr else channel.recv_ready
    recv_fn = channel.recv_stderr if is_stderr else channel.recv
    drained_any = False
    while ready_fn():
        data = recv_fn(4096)
        if not data:
            break
        chunks.append(data)
        drained_any = True
    return drained_any


def _shell_quote(path: str) -> str:
    """给远程 shell 命令里的路径加引号，避免路径含空格或特殊字符时
    被 shell 拆词。"""
    return "'" + path.replace("'", "'\\''") + "'"
