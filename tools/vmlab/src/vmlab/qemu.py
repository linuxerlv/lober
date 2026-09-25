"""目标机（待装机的裸机）的 QEMU 进程控制。

三条本机硬约束，都是实测踩出来的，不要凭猜改：

1. **WHPX 下不能用 -cpu max / -cpu host**，会直接 `WHPX: Unexpected VP exit
   code 4`，串口一个字节都不输出。必须 `-cpu qemu64` 配
   `-accel whpx,kernel-irqchip=off`。qemu64 名义上只到 x86-64-v1，但实测能跑
   RHEL 9 系（glibc 的 v2 检查没有触发）。
2. **残留的目标机进程会占着 VM-S 的 socket listen 端口**，新目标机拿不到网络，
   症状是 iPXE 报 `No configuration methods succeeded`。所以每次启动前按
   `-name` 精确清理，不能按内存大小猜（踩过一次：一台装到一半的目标机占了
   2.3GB，被「小于 1500MB」的过滤漏掉）。
3. **所有传给 QEMU 的路径都要是 Windows 原生形式**。QEMU 是原生 Windows 二进制，
   MSYS 的 argv 翻译只作用于「整个参数就是一个 POSIX 绝对路径」的裸参数，
   不作用于 `file=X` 这种复合参数内嵌的路径——与路径深浅无关。这里统一用
   D:/... 形式，正斜杠（QEMU 接受，且不存在反斜杠被当转义符的隐患）。
"""

from __future__ import annotations

import subprocess
import time
from dataclasses import dataclass
from pathlib import Path

from vmlab.config import SETTINGS

# pwsh 7.x 的 WindowsApps shim。**用 Windows 原生路径**：本模块跑在 Windows 的
# Python 上，subprocess 不认 MSYS 的 /c/... 挂载点写法（实测 WinError 2）。
# 必须是 pwsh 而不是内置的 powershell 5.1——后者控制台输出编码是 GB2312，
# 中文一律乱码，连自己的错误信息也是。
PWSH = "C:/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe"

# 只保留这个名字的 QEMU 进程（服务端 VM），其余全部清掉。
SERVER_VM_NAME = "vmlab-server"


@dataclass
class TargetVM:
    """一台目标机。"""

    name: str = "t01"
    mac: str = "52:54:00:30:30:01"
    memory_mb: int = 4096
    disk_gb: int = 12
    firmware: str = "bios"  # bios | uefi
    settings: object = SETTINGS

    @property
    def disk_path(self) -> str:
        return f"{self.settings.paths.targets_dir}/{self.name}.qcow2"

    @property
    def console_log(self) -> str:
        """当前一轮的串口日志路径。

        每轮一个带时间戳的文件、外加一个指向最新的固定名字。此前固定名字被
        每轮 start() 删掉重建，于是上一轮的证据在下一轮开始时就没了——而装机
        失败往往要跨轮次对比（这一轮卡在哪、上一轮同一位置报了什么），实测
        因此丢过一次 anaconda 的错误输出。
        """
        return f"{self.settings.paths.targets_dir}/{self.name}-console.log"

    def archived_log(self, stamp: str) -> str:
        return f"{self.settings.paths.targets_dir}/{self.name}-console-{stamp}.log"

    # --- 生命周期 -------------------------------------------------------

    def reset_disk(self) -> None:
        """重建空白磁盘。

        每个条目一块全新盘：条目之间零污染，且失败后那块盘还在，能挂起来看
        anaconda 日志。
        """
        Path(self.settings.paths.targets_dir).mkdir(parents=True, exist_ok=True)
        _run(
            [
                self.settings.paths.qemu_img,
                "create",
                "-f",
                "qcow2",
                self.disk_path,
                f"{self.disk_gb}G",
            ],
            what="重建目标机磁盘",
        )

    def start(self) -> None:
        """后台启动目标机，纯网络引导。

        串口走 -nographic（写到本进程 stdout，再由 subprocess 重定向到日志
        文件）而不是 `-serial file:`：后者在这个 QEMU 构建上不写入任何内容，
        实测得到 0 字节的日志文件。
        """
        log = Path(self.console_log)
        log.parent.mkdir(parents=True, exist_ok=True)
        if log.exists():
            # 归档上一轮而不是删掉：装机失败往往要跨轮次对比（这一轮卡在哪、
            # 上一轮同一位置报了什么），实测因此丢过一次 anaconda 的错误输出。
            stamp = time.strftime("%m%d-%H%M%S", time.localtime(log.stat().st_mtime))
            log.replace(Path(self.archived_log(stamp)))

        args = [
            self.settings.paths.qemu,
            "-name",
            self.name,
            "-machine",
            "q35",
            "-accel",
            "whpx,kernel-irqchip=off",
            "-cpu",
            "qemu64",
            "-m",
            str(self.memory_mb),
            "-smp",
            "2",
            "-drive",
            f"file={self.disk_path},if=virtio,format=qcow2",
            "-netdev",
            f"socket,id=lan,connect=127.0.0.1:{self.settings.net.lan_port}",
            "-device",
            f"virtio-net-pci,netdev=lan,mac={self.mac}",
            # 熵源。**没有它 installer 会挂在 sshd-keygen 上**：ECDSA/RSA 密钥
            # 生成要从 /dev/random 取熵，而无 rng 设备的虚拟机里熵池补充极慢
            # （实测 2026-09-26：串口停在 `Starting sshd-keygen@ecdsa.service`
            # 七分钟无进展，anaconda 始终起不来）。VM-S 的启动脚本一开始就有
            # 这个设备，目标机漏了。
            #
            # 真机不需要这条：现代 CPU 都有 RDRAND，内核直接用。这是虚拟化
            # 环境的特有问题，因此属于测试床配置而非 lober 的缺陷。
            "-device",
            "virtio-rng-pci",
            "-boot",
            "n",
            "-display",
            "none",
            "-nographic",
        ]
        if self.firmware == "uefi":
            # msys 的 qemu 包把固件放在 share/qemu/edk2-<arch>-code.fd，没有
            # Linux 发行版惯用的 OVMF_CODE.fd 这个名字。
            args += [
                "-drive",
                f"if=pflash,format=raw,readonly=on,file={self.settings.paths.ovmf_code}",
            ]

        handle = open(log, "wb")
        subprocess.Popen(args, stdout=handle, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL)

    def console_tail(self, chars: int = 2000) -> str:
        """取串口日志尾部。日志里有 NUL 与 ANSI 转义，读时清掉 NUL。"""
        p = Path(self.console_log)
        if not p.exists():
            return ""
        raw = p.read_bytes()[-chars:]
        return raw.replace(b"\x00", b"").decode("utf-8", "replace")

    def disk_bytes(self) -> int:
        """磁盘实际占用。qcow2 是稀疏的，装机真的写入后会明显变大——这是
        「系统装进去了」最直接的物理证据。"""
        p = Path(self.disk_path)
        return p.stat().st_size if p.exists() else 0


def kill_target_vms() -> list[int]:
    """清掉除服务端 VM 之外的所有 QEMU 进程，返回被停掉的 PID。

    按 `-name` 匹配命令行，不按内存大小猜。
    """
    script = f"""
$all = Get-CimInstance -ClassName Win32_Process | Where-Object {{ $_.Name -like "qemu*" }}
foreach ($p in $all) {{
  if ($p.CommandLine -notmatch "{SERVER_VM_NAME}") {{
    $p.ProcessId
    Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
  }}
}}
"""
    r = subprocess.run(
        [PWSH, "-NoProfile", "-Command", script],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    pids = [int(x) for x in r.stdout.split() if x.strip().isdigit()]
    if pids:
        time.sleep(3)  # 给 socket 端口释放留点时间
    return pids


def running_vms() -> list[str]:
    """列出在跑的 QEMU 进程的 -name 值。"""
    script = """
$all = Get-CimInstance -ClassName Win32_Process | Where-Object { $_.Name -like "qemu*" }
foreach ($p in $all) {
  if ($p.CommandLine -match "-name (\\S+)") { $Matches[1] }
}
"""
    r = subprocess.run(
        [PWSH, "-NoProfile", "-Command", script],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return [line.strip() for line in r.stdout.split("\n") if line.strip()]


def _run(args: list[str], what: str) -> None:
    r = subprocess.run(args, capture_output=True, text=True, encoding="utf-8", errors="replace")
    if r.returncode != 0:
        raise RuntimeError(f"{what} 失败（rc={r.returncode}）: {r.stderr.strip()}")
