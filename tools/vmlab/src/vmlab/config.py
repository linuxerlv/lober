"""实验床的固定坐标。

全部是本机实测确认过的值，不要凭记忆改。来源见各字段注释。
"""

from __future__ import annotations

from dataclasses import dataclass, field
from pathlib import Path


@dataclass(frozen=True)
class Paths:
    """本机工具与镜像的绝对路径。

    **一律用 Windows 原生形式（D:/...）**：这些路径最终交给 subprocess，而
    测试床跑在 Windows 的 Python 上，它不认 MSYS 的 /d/... 挂载点写法（实测
    WinError 2）。正斜杠而非反斜杠：Python 字符串里不用转义，QEMU 也接受。

    交给 VM-S 的远程路径（guest 内的 /srv/lober 等）不在这里，它们是真正的
    POSIX 路径，见 Settings 的其余字段。
    """

    # QEMU 11.0.1 装在 msys2 的 ucrt64 前缀下。
    qemu: str = "D:/msys/ucrt64/bin/qemu-system-x86_64.exe"
    qemu_img: str = "D:/msys/ucrt64/bin/qemu-img.exe"
    # msys 的 qemu 包把固件放在 share/qemu/edk2-<arch>-code.fd，
    # **没有** Linux 发行版惯用的 OVMF_CODE.fd 这个名字。
    ovmf_code: str = "D:/msys/ucrt64/share/qemu/edk2-x86_64-code.fd"
    # 服务端 VM 的 SSH 私钥（与目标机装完后注入的公钥是同一对）。
    ssh_key: str = "D:/qemu-vm/debian-cloud/id_ed25519"
    # 目标机磁盘与串口日志都落在 D:（C: 与 E: 空间都不够）。
    targets_dir: str = "D:/qemu-vm/vmlab-targets"


@dataclass(frozen=True)
class Network:
    """装机 LAN 与 SSH 转发。"""

    # VM-S 的 socket netdev listen 端口，目标机 connect 同一个。
    # 实测 QEMU 11.0.1 的 listen 在 peer 断开后继续接受新连接，因此六个家族
    # 串行装机可以复用它，不需要桥、也不需要第二块 socket 网卡。
    lan_port: int = 12520
    # VM-S 的 SSH hostfwd（现有那台 debian-cloud 用 2222，共用同一实例）。
    ssh_port: int = 2222
    ssh_user: str = "debian"
    # 装机 LAN 上 VM-S 的地址，dnsmasq 与 nginx 都绑它。
    server_ip: str = "10.20.20.1"


@dataclass(frozen=True)
class Console:
    """控制台 API 的接入参数。

    人面走 HTTPS——nginx 的人面 server 块强制 `listen ... ssl`，80 端口是 301
    跳转。证书是实验床自签的（deploy/ca/gen-ca.sh 的 CA 私钥必须交互输入密码
    短语，没有非交互入口，用不了），SAN 是 console.vmlab.internal，guest 的
    /etc/hosts 里指向 127.0.0.1。
    """

    base_url: str = "https://console.vmlab.internal/console/v1"
    ca_cert: str = "/etc/lober/tls/console.cert.pem"
    # 两个账号：admin 建东西，approver 批 hook。SPEC §9.4 要求 hook 的
    # review → active 必须由另一个人批（审批人不能等于作者），所以实验床
    # 也得有两个。
    admin_user: str = "admin"
    admin_password: str = "vmlab-admin-pw"
    approver_user: str = "approver"
    approver_password: str = "vmlab-approver-pw"
    # 机器面（装机 LAN 上的 nginx），明文 HTTP 是永久决定，见
    # docs/tls-与安全启动-范围决定.md。
    gateway_url: str = "http://gw.vmlab.internal"


@dataclass(frozen=True)
class Settings:
    paths: Paths = field(default_factory=Paths)
    net: Network = field(default_factory=Network)
    console: Console = field(default_factory=Console)
    # nginx 的机器面 access log，信号与制品请求都记在这里。
    boot_log: str = "/var/log/nginx/lober-boot.log"
    artifact_root: str = "/srv/lober/artifacts"


SETTINGS = Settings()
