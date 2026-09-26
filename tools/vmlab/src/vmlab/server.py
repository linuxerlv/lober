"""服务端 VM（VM-S）的 SSH 通道。

为什么所有操作都在 guest 内部执行、而不从 Windows 侧发起：

1. 控制台走 HTTPS，证书是实验床自签的、SAN 是 console.vmlab.internal，而
   那个域名只在 guest 的 /etc/hosts 里解析。从 Windows 侧调用要额外配 DNS
   与证书信任，没必要。
2. 机器面 gw.vmlab.internal 绑在装机 LAN（10.20.20.1）上，Windows 宿主根本
   不在那个网段——socket netdev 是 QEMU 进程之间的点对点连接，不经宿主网络栈。
3. nginx 的反代与 TLS 终结因此都留在链路里被真实验证，而不是被绕开。
"""

from __future__ import annotations

import shlex
from dataclasses import dataclass

import paramiko

from vmlab.config import SETTINGS


@dataclass
class Result:
    """一次远程命令的结果。"""

    rc: int
    stdout: str
    stderr: str

    def ok(self) -> bool:
        return self.rc == 0

    def check(self, what: str) -> "Result":
        """rc 非零时抛异常，消息里带上 stderr——远程命令失败最常见的原因是
        权限或路径，那些信息只在 stderr 里。"""
        if self.rc != 0:
            raise RuntimeError(
                f"{what} 失败（rc={self.rc}）\nstdout: {self.stdout.strip()}"
                f"\nstderr: {self.stderr.strip()}"
            )
        return self


class VMServer:
    """VM-S 的 SSH 连接。用作上下文管理器。"""

    def __init__(self, settings=SETTINGS) -> None:
        self.s = settings
        self._client: paramiko.SSHClient | None = None

    def __enter__(self) -> "VMServer":
        client = paramiko.SSHClient()
        # 实验床的 host key 每次重建磁盘都会变，不值得维护 known_hosts。
        client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
        client.connect(
            hostname="127.0.0.1",
            port=self.s.net.ssh_port,
            username=self.s.net.ssh_user,
            key_filename=_win_path(self.s.paths.ssh_key),
            timeout=20,
        )
        self._client = client
        return self

    def __exit__(self, *exc) -> None:
        if self._client is not None:
            self._client.close()
            self._client = None

    def run(self, cmd: str, timeout: int = 120) -> Result:
        """执行一条命令。cmd 原样交给远程 shell。"""
        if self._client is None:
            raise RuntimeError("VMServer 未连接（请用 with 语句）")
        _, stdout, stderr = self._client.exec_command(cmd, timeout=timeout)
        out = stdout.read().decode("utf-8", "replace")
        err = stderr.read().decode("utf-8", "replace")
        return Result(stdout.channel.recv_exit_status(), out, err)

    def sudo(self, cmd: str, timeout: int = 120) -> Result:
        """以 root 执行。guest 的 debian 用户有免密 sudo。"""
        return self.run(f"sudo {cmd}", timeout=timeout)

    def read_file(self, path: str, sudo: bool = True) -> str:
        """读远程文件。默认走 sudo——/srv/lober 与 /etc/lober 都是 750/640。"""
        quoted = shlex.quote(path)
        r = self.sudo(f"cat {quoted}") if sudo else self.run(f"cat {quoted}")
        return r.check(f"读取 {path}").stdout

    def upload(self, local: str, remote: str) -> None:
        """上传文件。用于部署新编译的 lober 二进制。"""
        if self._client is None:
            raise RuntimeError("VMServer 未连接")
        sftp = self._client.open_sftp()
        try:
            sftp.put(_win_path(local), remote)
        finally:
            sftp.close()

    # --- lober 服务 -----------------------------------------------------

    def deploy_lober(self, local_binary: str) -> None:
        """上传并重启 lober。

        先 stop 再 install：正在运行的二进制在 Linux 上可以覆盖写，但
        systemd 的 ExecStartPre 会用新二进制跑 -check，顺序颠倒会让这次
        自检用的是旧版本。
        """
        self.upload(local_binary, "/tmp/lober.new")
        self.sudo("systemctl stop lober").check("停止 lober")
        self.sudo("install -m 755 /tmp/lober.new /opt/lober/bin/lober").check("安装二进制")
        self.run("rm -f /tmp/lober.new")
        self.sudo("systemctl start lober").check("启动 lober")

    def service_active(self, *names: str) -> dict[str, str]:
        """查若干 systemd 服务的状态。"""
        r = self.run(f"systemctl is-active {' '.join(names)}")
        # is-active 对任一非 active 的服务返回非零，所以不 check，直接读输出。
        states = r.stdout.strip().split("\n")
        return dict(zip(names, states))

    # --- 日志 -----------------------------------------------------------

    def signals_for(self, task_id: str) -> list[str]:
        """从 nginx access log 里取某个任务的信号序列（按到达顺序）。

        判据是 URL 形态 `/v1/signal/<task>/<token>/<phase>?rc=N`。用 nginx 日志
        而不是查数据库：日志记录的是**机器真的发出过**的请求，而数据库里只有
        状态机接受后的结果——被 §3.6 只进不退丢弃的信号在库里看不到，而那正是
        需要诊断的情形。
        """
        cmd = (
            f"grep -oE 'signal/{task_id}/[^/]+/[a-z-]+[?]rc=[0-9]+' {self.s.boot_log} "
            "| sed 's|signal/[^/]*/[^/]*/||'"
        )
        r = self.sudo(cmd)
        return [line for line in r.stdout.strip().split("\n") if line]

    def boot_requests(self, pattern: str = "", tail: int = 20) -> list[str]:
        """取机器面 access log 的尾部，可按子串过滤。"""
        grep = f"| grep -F {shlex.quote(pattern)}" if pattern else ""
        r = self.sudo(f"tail -400 {self.s.boot_log} {grep} | tail -{tail}")
        return [line for line in r.stdout.strip().split("\n") if line]

    def task_log_tail(self, task_id: str, lines: int = 40) -> str:
        """取服务端落盘的任务日志（hook 正文输出、syslog）。"""
        d = f"/var/lib/lober/log/tasks/{task_id}"
        r = self.sudo(f"find {d} -type f 2>/dev/null | head -5")
        files = [f for f in r.stdout.strip().split("\n") if f]
        chunks = []
        for f in files:
            body = self.sudo(f"tail -{lines} {shlex.quote(f)}").stdout
            chunks.append(f"--- {f} ---\n{body}")
        return "\n".join(chunks)


def _win_path(msys_path: str) -> str:
    """把 /d/x/y 这类 MSYS 路径转成 D:/x/y。

    paramiko 跑在 Windows 的 Python 上，不认 MSYS 的挂载点写法。
    """
    if len(msys_path) > 2 and msys_path[0] == "/" and msys_path[2] == "/":
        return f"{msys_path[1].upper()}:{msys_path[2:]}"
    return msys_path
