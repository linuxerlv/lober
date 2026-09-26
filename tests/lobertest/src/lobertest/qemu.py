"""VM-S 与目标机的 QEMU 进程生命周期，串口日志落盘。

只被 L2/L3 的 fixture 引用，`api.py` 不 import 本模块——这是 L1 在不起
目标机时可跑的编译期保证。

六条从旧 harness（已删的 tools/vmlab/src/vmlab/qemu.py）逐条取证得来的
硬约束，都带真实报错，不要重新试错：

1. **`-serial file:` 在这个 QEMU 构建上写出 0 字节日志。** 必须用
   `-nographic` 让串口走进程 stdout，再由 subprocess 重定向到文件。
2. **disk_serial 必须拆成 `-drive if=none,id=hd0` + 独立的
   `-device virtio-blk-pci,drive=hd0,serial=...`。** serial 是设备属性
   不是驱动属性，写在 -drive 上 QEMU 直接拒绝启动（`Block format 'qcow2'
   does not support the option 'serial'`）。Debian 家族服务端选盘依赖
   `/dev/disk/by-id/virtio-<serial>`，这是它的前提。
3. **必须给 `-device virtio-rng-pci`。** 否则 guest 内 sshd-keygen 从
   `/dev/random` 取熵时因熵池补充极慢而挂起（实测卡 7 分钟无进展、无明确
   报错）。真机不需要，纯虚拟化环境特有。
4. **残留的目标机进程会占着 VM-S 的 socket listen 端口**，新目标机拿不到
   网络（iPXE 报 `No configuration methods succeeded`），更坏的情形是它
   偷走新任务的 pending 状态。按 `-name` 精确清理，不能按内存大小猜——
   装到一半的目标机占 2.3GB，会被「小于 1500MB」的过滤漏掉。
5. **MSYS 的 argv 翻译只作用于「整个参数就是一个 POSIX 绝对路径」的裸
   参数**，不作用于 `file=X` 这种复合参数内嵌的路径，与路径深浅无关。这是
   `WinError 2` 的真实成因。统一用 `D:/...` 正斜杠形式（winpath）。
6. **串口日志按 mtime 归档而非覆盖。** 装机失败常要跨轮次对比，旧 harness
   有过一次因覆盖丢失 anaconda 错误输出的教训。

WHPX 下 `-cpu max` 与 `-cpu host` 必然 `WHPX: Unexpected VP exit code 4`，
串口零输出；`-cpu qemu64` 是实测唯一可用的值（名义上只到 x86-64-v1，但
实测能跑 RHEL 9 系）。必须同时配 `-accel whpx,kernel-irqchip=off`，否则
刷 `Ignoring request for interrupt vector 0`。

参数拼装（`_server_args`/`_target_args`）与进程启动（`start_server`/
`start_target`）分离成纯函数：四条回归护栏（锁死 -cpu 精确值、-nographic、
disk_serial 拆分、日志归档）因此可以直接断言 argv 列表内容，不需要真的
起进程——本机现在起不来完整装机流程，若护栏必须靠真实 VM 验证，等于没有
护栏。
"""

from __future__ import annotations

import shutil
import subprocess
import time
from pathlib import Path
from typing import TYPE_CHECKING

if TYPE_CHECKING:
    from lobertest.config import Settings

# pwsh 7.x 的 WindowsApps shim，绝对路径调用。必须是 pwsh 而不是内置的
# powershell 5.1——后者控制台输出编码是 GB2312，中文一律乱码。
_PWSH = "C:/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe"

# VM-S 的固定 -name，取自 D:/qemu-vm/debian-cloud/start-vm.sh 的既有约定，
# 不在这里另起一个名字造成两处定义不一致。
SERVER_VM_NAME = "debian-cloud"


def winpath(p: "Path | str") -> str:
    """POSIX 风格路径 → Windows 原生形式，强制绝对化。

    相对路径原样透给 QEMU 会以 QEMU 的 cwd 解析，不是调用方期望的目录，
    所以必须先 resolve() 再统一成正斜杠形式（约束 5：MSYS 的 argv 翻译
    不作用于复合参数内嵌的路径，正斜杠本身 QEMU 原生接受，且不存在反
    斜杠被当转义符的隐患）。
    """
    return Path(p).resolve().as_posix()


def _qemu_img_bin(settings: "Settings") -> str:
    """qemu-img 与 qemu-system-x86_64 同目录，MSYS/ucrt64 的既有布局。"""
    return winpath(settings.qemu_bin.parent / "qemu-img.exe")


def _disk_args(disk_path: str, disk_serial: str) -> list[str]:
    """拼磁盘相关参数（约束 2）。

    disk_serial 为空时用 `-drive if=virtio` 简写；非空时必须拆成
    `-drive if=none,id=hd0` + 独立的 `-device virtio-blk-pci,...,serial=`
    ——serial 是设备属性，写在 -drive 上直接被 QEMU 拒绝启动。
    """
    if not disk_serial:
        return ["-drive", f"file={disk_path},if=virtio,format=qcow2"]
    return [
        "-drive", f"file={disk_path},if=none,id=hd0,format=qcow2",
        "-device", f"virtio-blk-pci,drive=hd0,serial={disk_serial}",
    ]


def _vars_path(entry_id: str, settings: "Settings") -> Path:
    """entry 独立的可写 UEFI vars 副本路径（共用会让引导项互相污染）。

    只算路径，不碰磁盘——磁盘上的实际文件由 `_ensure_vars_copy`
    在 `start_target` 里按需创建，`_target_args` 保持纯函数。
    """
    return settings.targets_dir / f"{entry_id}-vars.fd"


def _ensure_vars_copy(entry_id: str, settings: "Settings") -> Path:
    """确保 entry 的可写 vars 副本文件存在，返回其路径。

    从 `settings.ovmf_vars_template` 拷贝，**不能自造全零文件**：真实的
    vars 模板在偏移 0x28 处有 `_FVH` 固件卷签名与卷头，全零文件缺这个
    结构会被 OVMF 判成损坏的 NVRAM（且 flash 的擦除态是 0xFF 不是 0x00，
    全零在语义上也不对——表示"所有位都被编程过"）。vars 与 code 是两个
    独立的 pflash 设备，各自按自己的文件大小映射，**不需要同大小**
    （模板 540672 字节，code.fd 3653632 字节）。
    """
    vars_path = _vars_path(entry_id, settings)
    if vars_path.exists():
        return vars_path

    vars_path.parent.mkdir(parents=True, exist_ok=True)
    shutil.copy(settings.ovmf_vars_template, vars_path)
    return vars_path


def _target_args(
    *,
    entry_id: str,
    firmware: str,
    mem: str,
    disk_serial: str,
    mac: str,
    settings: "Settings",
) -> list[str]:
    """拼装目标机的 QEMU argv，不启动进程。纯函数，供测试直接断言。"""
    disk_path = winpath(settings.targets_dir / f"{entry_id}.qcow2")

    args = [
        winpath(settings.qemu_bin),
        "-name", entry_id,
        "-machine", "q35",
        "-accel", "whpx,kernel-irqchip=off",
        "-cpu", "qemu64",
        "-m", mem,
        "-smp", "2",
        *_disk_args(disk_path, disk_serial),
        # 装机 VLAN 网卡：socket 类型，connect 到 VM-S 的 listen 侧
        # （start-vm.sh 里 `-netdev socket,id=lan,listen=127.0.0.1:12520`）。
        # 两台 QEMU 的虚拟网卡由此形成一个二层 hub，DHCP 广播才能到达 dnsmasq
        # ——SLIRP 做不到这件事，它是用户态 NAT，两台 guest 之间不互通。
        #
        # 顺序有硬依赖：VM-S（listen 侧）必须先在跑，否则这里 connect 立刻被拒
        # 而 QEMU 直接退出。`-boot n` 没有网卡时同样是必败的（没有可引导设备），
        # 这正是本函数此前的缺口：有 `-boot n` 却一张网卡都没有。
        #
        # mac 由调用方传入而不是从 entry_id 派生：lober 按 MAC 认机器
        # （UpsertHost 用 mac 做写入冲突判定），必须与控制台里建的 host 记录
        # 一致。派生值一旦跨进程不稳定，就会在库里累积同一 SN 的多行脏数据。
        "-netdev", f"socket,id=lan,connect=127.0.0.1:{settings.pxe_socket_port}",
        "-device", f"virtio-net-pci,netdev=lan,mac={mac}",
        "-device", "virtio-rng-pci",
        "-boot", "n",
        "-display", "none",
        "-nographic",
    ]

    if firmware == "uefi":
        # 只算路径，不在这里创建文件——_target_args 是纯函数，磁盘上的
        # vars 副本由调用方（start_target）通过 _ensure_vars_copy 保证
        # 存在。测试断言参数列表时不应有副作用地在 tmp_path 里写文件。
        vars_path = _vars_path(entry_id, settings)
        args += [
            "-drive",
            f"if=pflash,format=raw,readonly=on,file={winpath(settings.ovmf_code)}",
            "-drive",
            f"if=pflash,format=raw,file={winpath(vars_path)}",
        ]

    return args


def _server_args(*, settings: "Settings") -> list[str]:
    """拼装 VM-S 的 QEMU argv，复用同一条 WHPX/cpu 约束。"""
    return [
        winpath(settings.qemu_bin),
        "-name", SERVER_VM_NAME,
        "-machine", "q35",
        "-accel", "whpx,kernel-irqchip=off",
        "-cpu", "qemu64",
        "-device", "virtio-rng-pci",
        "-display", "none",
    ]


def start_server(settings: "Settings") -> subprocess.Popen | None:
    """启动 VM-S，幂等——已在跑则返回 None（不产生新进程句柄）。

    调用方若需要既有进程的句柄，`_find_processes(SERVER_VM_NAME)` 能给
    PID 但拿不回 Popen 对象（这是 subprocess 模块的固有限制：Popen 只能
    包裹自己创建的进程）。本模块的其余函数都按 PID 而非 Popen 句柄操作
    远程进程状态，调用方不应假设 start_server 总能返回可等待的句柄。
    """
    if _find_processes(SERVER_VM_NAME):
        return None

    args = _server_args(settings=settings)
    return subprocess.Popen(
        args, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, stdin=subprocess.DEVNULL
    )


def reset_overlay(entry_id: str, size: str, settings: "Settings") -> Path:
    """重建 entry 的空白磁盘（旧盘存在则先删）。

    每个条目一块全新盘：条目之间零污染，失败后那块盘还在能挂起来查看。
    这里没有 golden base image 可继承——目标机走 PXE 网络引导，"重置" 就是
    "给一块空盘"，不是 qcow2 backing-file 意义上的差分覆盖。
    """
    settings.targets_dir.mkdir(parents=True, exist_ok=True)
    disk_path = settings.targets_dir / f"{entry_id}.qcow2"
    if disk_path.exists():
        disk_path.unlink()

    args = [
        _qemu_img_bin(settings),
        "create",
        "-f", "qcow2",
        winpath(disk_path),
        size,
    ]
    result = subprocess.run(args, capture_output=True, text=True, encoding="utf-8", errors="replace")
    if result.returncode != 0:
        raise RuntimeError(
            f"重建 {entry_id} 的磁盘失败（rc={result.returncode}）：{result.stderr.strip()}"
        )
    return disk_path


def start_target(
    entry_id: str,
    firmware: str,
    mem: str,
    disk_serial: str = "",
    *,
    mac: str,
    settings: "Settings",
) -> subprocess.Popen:
    """启动目标机。先清理残留进程（约束 4），串口走 -nographic 重定向到
    日志文件（约束 1），日志已存在时先归档（约束 6），uefi 固件时确保
    entry 独立的 vars 副本存在。

    `mac` 是必需的关键字参数，没有默认值：它必须与控制台里建的 host 记录
    用同一个值（lober 按 MAC 认机器），给个默认值只会让调用方忘记传而拿到
    一台永远匹配不上任务的机器。
    """
    kill_stale_targets(settings=settings)
    if firmware == "uefi":
        _ensure_vars_copy(entry_id, settings)

    log_path = serial_log_path(entry_id, settings=settings)
    args = _target_args(
        entry_id=entry_id,
        firmware=firmware,
        mem=mem,
        disk_serial=disk_serial,
        mac=mac,
        settings=settings,
    )

    handle = open(log_path, "wb")
    return subprocess.Popen(
        args, stdout=handle, stderr=subprocess.STDOUT, stdin=subprocess.DEVNULL
    )


def stop_target(
    entry_id: str,
    *,
    proc: "subprocess.Popen | None" = None,
    settings: "Settings",
    timeout: float = 15.0,
) -> None:
    """停止 entry 对应的目标机，确认进程真的退出（内存约束下一条目才能
    起，不能只发信号就当作已停）。

    进程已经退出时不抛（幂等）——调用方在「不确定是否已停」的情况下会
    重复调用。`proc` 由调用方持有的 Popen 句柄传入（`start_target` 的
    返回值）；未传入时按 `-name` 精确匹配去杀（约束 4）。
    """
    if proc is not None:
        if proc.poll() is not None:
            return  # 已经死了，幂等返回
        proc.terminate()
        try:
            proc.wait(timeout=timeout)
        except subprocess.TimeoutExpired:
            proc.kill()
            proc.wait(timeout=timeout)
        return

    pids = _find_processes(entry_id)
    if not pids:
        return  # 已经死了，幂等返回

    for pid in pids:
        _run_pwsh(f"Stop-Process -Id {pid} -Force -ErrorAction SilentlyContinue")

    deadline = time.monotonic() + timeout
    while time.monotonic() < deadline:
        if not _find_processes(entry_id):
            return
        time.sleep(0.5)
    raise RuntimeError(f"停止 {entry_id} 超时（{timeout}s）：进程仍在 {_find_processes(entry_id)}")


def kill_stale_targets(settings: "Settings | None" = None) -> list[int]:
    """按 -name 清理全部残留的目标机进程（VM-S 自身排除在外），返回被杀
    的 PID 列表。

    与 stop_target 分开：后者管单个已知 entry，前者管「上一轮崩溃留下
    的、entry_id 已不可知的」进程（约束 4）。按命令行里的 -name 匹配，
    不按内存大小猜——装到一半的目标机占 2.3GB，会被「小于 1500MB」的
    过滤漏掉。
    """
    script = f"""
$all = Get-CimInstance -ClassName Win32_Process | Where-Object {{ $_.Name -like "qemu-system*" }}
foreach ($p in $all) {{
  if ($p.CommandLine -notmatch "-name {SERVER_VM_NAME}\\b") {{
    $p.ProcessId
  }}
}}
"""
    pids = _run_pwsh_query(script)
    for pid in pids:
        _run_pwsh(f"Stop-Process -Id {pid} -Force -ErrorAction SilentlyContinue")
    if pids:
        time.sleep(1)  # 给 socket 端口释放留点时间
    return pids


def serial_log_path(entry_id: str, *, settings: "Settings") -> Path:
    """entry 当前轮的串口日志固定路径。旧文件存在时先归档（按其 mtime
    生成 stamp），不覆盖——装机失败常要跨轮次对比。
    """
    settings.targets_dir.mkdir(parents=True, exist_ok=True)
    path = settings.targets_dir / f"{entry_id}-console.log"
    if path.exists():
        stamp = time.strftime("%m%d-%H%M%S", time.localtime(path.stat().st_mtime))
        archived = archived_log_path(entry_id, stamp, settings=settings)
        path.replace(archived)
    return path


def archived_log_path(entry_id: str, stamp: str, *, settings: "Settings") -> Path:
    return settings.targets_dir / f"{entry_id}-console-{stamp}.log"


def _find_processes(name_pattern: str) -> list[int]:
    """按命令行里的 `-name <pattern>` 精确匹配，返回 PID 列表。"""
    script = f"""
$all = Get-CimInstance -ClassName Win32_Process | Where-Object {{ $_.Name -like "qemu-system*" }}
foreach ($p in $all) {{
  if ($p.CommandLine -match "-name {name_pattern}\\b") {{
    $p.ProcessId
  }}
}}
"""
    return _run_pwsh_query(script)


def _run_pwsh_query(script: str) -> list[int]:
    result = subprocess.run(
        [_PWSH, "-NoProfile", "-Command", script],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
    return [int(x) for x in result.stdout.split() if x.strip().isdigit()]


def _run_pwsh(command: str) -> None:
    subprocess.run(
        [_PWSH, "-NoProfile", "-Command", command],
        capture_output=True,
        text=True,
        encoding="utf-8",
        errors="replace",
    )
