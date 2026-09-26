#!/usr/bin/env bash
# 起一台目标机（待装机的裸机），纯网络引导。
#
# 网卡用 socket connect 接到 VM-S 的 listen 端口——实测 QEMU 11.0.1 的
# listen 在 peer 断开后继续接受新连接，所以六个家族串行装机可以复用同一个
# 端口，不需要桥、也不需要第二块 socket 网卡。
#
# MAC 必须与控制台里登记的主机一致：/v1/boot 除了按 SN 查任务，还要做 MAC
# 双因子校验（§4.3、§9.1 第二道闸门），不一致会回 no_task 且不给任何提示
# ——这是刻意的，不给攻击者「SN 猜对了」的反馈。
set -euo pipefail

DISK="${DISK:-/d/qemu-vm/vmlab-targets/t01.qcow2}"
MAC="${MAC:-52:54:00:30:30:01}"
MEM="${MEM:-3072}"
LAN_PORT="${LAN_PORT:-12520}"
LOG="${LOG:-/d/qemu-vm/vmlab-targets/t01-console.log}"
FIRMWARE="${FIRMWARE:-bios}"

# QEMU 是原生 Windows 二进制：复合参数（-drive file=X）里内嵌的 POSIX 路径
# 不会被 MSYS 翻译，必须自己转。cygpath -m 输出正斜杠，内嵌进参数时没有
# 反斜杠被当转义符的隐患。
DISK_WIN="$(cygpath -m -a "$DISK")"
LOG_WIN="$(cygpath -m -a "$LOG")"

args=(
  -name vmlab-target
  -machine q35
  # WHPX 下不能用 -cpu max/host（必然 Unexpected VP exit code 4，串口零输出）。
  -accel whpx,kernel-irqchip=off
  -cpu qemu64
  -m "$MEM" -smp 2
  -drive file="$DISK_WIN",if=virtio,format=qcow2,cache=writeback
  -netdev "socket,id=lan,connect=127.0.0.1:${LAN_PORT}"
  -device "virtio-net-pci,netdev=lan,mac=${MAC}"
  -boot n
  -display none
  -serial "file:${LOG_WIN}"
)

if [ "$FIRMWARE" = "uefi" ]; then
  # OVMF：msys 的 qemu 包把固件放在 share/qemu/edk2-<arch>-code.fd，
  # 没有 Linux 发行版惯用的 OVMF_CODE.fd 这个名字。
  args+=(-drive "if=pflash,format=raw,readonly=on,file=$(cygpath -m -a /d/msys/ucrt64/share/qemu/edk2-x86_64-code.fd)")
fi

exec /d/msys/ucrt64/bin/qemu-system-x86_64.exe "${args[@]}"
