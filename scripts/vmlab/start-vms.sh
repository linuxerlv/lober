#!/usr/bin/env bash
# 起服务端 VM（VM-S）：在现有 debian-cloud 实例上加两样东西——
#   1. 一块 socket 网卡（装机 LAN，listen 模式），目标机用 connect 接进来。
#      实测 QEMU 11.0.1 的 listen 在 peer 断开后继续接受新连接，因此六个
#      家族串行装机可以复用同一个端口，不需要桥、不需要第二块 socket 网卡。
#   2. 把 OpenCloudOS ISO 以只读 virtio 盘挂进去（/dev/vdb），导入制品集时
#      直接用这个块设备当 sourcePath——iso9660.go 的 perCallOpenReaderAt
#      每次 ReadAt 独立开关文件、不校验必须是普通文件，块设备可直接读，
#      省掉往 guest 里 scp 1.6G 的时间与磁盘。
# SLIRP 那块网卡保持原样（apt/ssh 走它），绝不动它。
set -euo pipefail
cd /d/qemu-vm/debian-cloud

MEM="${MEM:-2560}"
CPUS="${CPUS:-2}"
SSH_PORT="${SSH_PORT:-2222}"
LAN_PORT="${LAN_PORT:-12520}"
ISO="${ISO:-/d/OpenCloudOS-9.6-x86_64-minimal.iso}"

# QEMU 是原生 Windows 二进制：复合参数（-drive file=X）里内嵌的 POSIX 路径
# 不会被 MSYS 翻译，必须自己转。相对路径（disk.qcow2）不受影响。
ISO_WIN="$(cygpath -m -a "$ISO")"

exec /d/msys/ucrt64/bin/qemu-system-x86_64.exe \
  -name vmlab-server \
  -machine q35 \
  -accel whpx,kernel-irqchip=off \
  -cpu qemu64 \
  -m "$MEM" -smp "$CPUS" \
  -drive file=disk.qcow2,if=virtio,format=qcow2,cache=writeback,discard=unmap \
  -drive file=seed.iso,media=cdrom,readonly=on \
  -drive file="$ISO_WIN",if=virtio,format=raw,readonly=on \
  -netdev "user,id=net0,net=10.10.10.0/24,host=10.10.10.2,dns=10.10.10.3,dhcpstart=10.10.10.10,hostfwd=tcp:127.0.0.1:${SSH_PORT}-10.10.10.10:22" \
  -device virtio-net-pci,netdev=net0 \
  -netdev "socket,id=lan,listen=127.0.0.1:${LAN_PORT}" \
  -device virtio-net-pci,netdev=lan,mac=52:54:00:20:20:01 \
  -device virtio-rng-pci \
  -display none \
  -serial file:console-vmlab.log \
  -monitor telnet:127.0.0.1:4455,server=on,wait=off
