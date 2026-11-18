#!/usr/bin/env bash
set -euo pipefail
export LC_ALL=C

if [[ $# != 2 ]]; then
    echo "用法：sudo bash $0 x86_64|aarch64 输出目录" >&2
    exit 2
fi
arch=$1
version=3.24.4
case $arch in
    x86_64) digest=cdaa38cb8de3537967ee5b6c9b6cd00cc962da2b018e6eea48696bb274923012 ;;
    aarch64) digest=4b14c18f7e90a063b61462ea1462a5732c154bf6adad594974fe6487cd98a8de ;;
    *) echo "不支持的架构：$arch" >&2; exit 2 ;;
esac
if [[ $(id -u) != 0 || $(uname -m) != "$arch" ]]; then
    echo "请以 root 在目标架构的 Linux 环境内运行。" >&2
    exit 2
fi
here=$(cd -- "$(dirname -- "$0")" && pwd)
mkdir -p -- "$2"
out=$(cd -- "$2" && pwd)
work=$(mktemp -d)
trap 'rm -rf -- "$work"' EXIT
root=$work/root
mkdir -- "$root"
archive="alpine-minirootfs-$version-$arch.tar.gz"
mirror=${ALPINE_MIRROR:-https://dl-cdn.alpinelinux.org/alpine}
curl --fail --silent --show-error --location --retry 2 --output "$work/$archive" \
    "https://dl-cdn.alpinelinux.org/alpine/v3.24/releases/$arch/$archive"
printf '%s  %s\n' "$digest" "$work/$archive" | sha256sum --check
tar -xzpf "$work/$archive" -C "$root"
printf '%s\n' "$mirror/v3.24/main" "$mirror/v3.24/community" > "$root/etc/apk/repositories"
cp -L /etc/resolv.conf "$root/etc/resolv.conf"

nic=(virtio_net e1000 e1000e igb ixgbe i40e ice mlx4_en mlx5_core tg3 bnx2 bnx2x bnxt_en r8169 ena)
firmware=(linux-firmware-bnx2 linux-firmware-bnx2x linux-firmware-brcm linux-firmware-intel
          linux-firmware-isci linux-firmware-mellanox linux-firmware-rtl_nic linux-firmware-tigon)
read -r -a extra_nic <<< "${EXTRA_NIC_MODULES:-}"
read -r -a extra_firmware <<< "${EXTRA_FIRMWARE_PACKAGES:-}"
nic+=("${extra_nic[@]}")
firmware+=("${extra_firmware[@]}")
for name in "${nic[@]}" "${firmware[@]}"; do
    [[ $name =~ ^[a-zA-Z0-9_.][a-zA-Z0-9_.-]*$ ]] || { echo "无效的驱动或固件包名：$name" >&2; exit 2; }
done
chroot "$root" apk add --no-cache --no-scripts \
    linux-lts kmod python3 curl ca-certificates dmidecode pciutils lscpu lsblk \
    ethtool smartmontools ipmitool "${firmware[@]}"
chroot "$root" /bin/busybox --install -s
chroot "$root" update-ca-certificates
chroot "$root" apk info -v > "$work/packages.txt"

release_dirs=("$root"/lib/modules/*)
[[ ${#release_dirs[@]} == 1 && -d ${release_dirs[0]} ]]
modules=${release_dirs[0]}
release=${modules##*/}
chroot "$root" depmod "$release"
declare -A keep=()
select_module() {
    local name=$1 line action path
    line=$(chroot "$root" modprobe --show-depends --set-version "$release" "$name") || return 1
    while read -r action path _; do
        if [[ $action == insmod && $path == /lib/modules/* ]]; then
            keep["$root$path"]=1
        fi
    done <<< "$line"
}
for name in "${nic[@]}"; do
    select_module "$name"
done
# DHCP 客户端用 Linux 的包套接字。Alpine 将它编为单独模块。
select_module af_packet
optional=(sd_mod sr_mod nvme nvme_core ahci ata_piix virtio_pci virtio_blk virtio_scsi
          megaraid_sas mpt3sas mpi3mr smartpqi hpsa arcmsr aacraid isci
          ses enclosure ipmi_si ipmi_ssif ipmi_devintf ipmi_msghandler acpi_ipmi
          coretemp k10temp i2c_i801 jc42 drivetemp xhci_pci)
for name in "${optional[@]}"; do
    select_module "$name" 2>/dev/null || continue
done
while IFS= read -r -d '' path; do
    if [[ ! -v keep["$path"] ]]; then
        rm -- "$path"
    fi
done < <(find "$modules" -type f -name '*.ko*' -print0)
chroot "$root" depmod "$release"
for name in "${nic[@]}"; do
    chroot "$root" modprobe --show-depends --set-version "$release" "$name" >/dev/null
done
printf '%s\n' "${nic[@]}" > "$work/nic-drivers.txt"
firmware_args=()
for package in "${extra_firmware[@]}"; do
    firmware_args+=(--keep-package "$package")
done
install -m 0644 "$here/firmware.py" "$root/firmware.py"
chroot "$root" python3 /firmware.py "${firmware_args[@]}" "$release" "${nic[@]}" > "$work/firmware-files.txt"
rm -- "$root/firmware.py"

install -m 0755 "$here/init.sh" "$root/init"
mkdir -p "$root/opt/lober" "$root/run" "$root/proc" "$root/sys" "$root/dev"
install -m 0644 "$here/collect.py" "$root/opt/lober/collect.py"
install -m 0755 "$here/dhcp.sh" "$root/opt/lober/dhcp"
if [[ -n ${STORCLI_BINARY:-} ]]; then
    # 可选的厂商本机工具需事先校验并确认允许在本单位镜像中使用。
    [[ -n ${STORCLI_SHA256:-} ]] || { echo "需要提供 STORCLI_SHA256" >&2; exit 2; }
    printf '%s  %s\n' "$STORCLI_SHA256" "$STORCLI_BINARY" | sha256sum --check
    install -m 0755 "$STORCLI_BINARY" "$root/usr/local/sbin/storcli"
    # Alpine 使用 musl，为版本匹配的厂商二进制提供兼容库。
    chroot "$root" apk add --no-cache --no-scripts gcompat libstdc++
    chroot "$root" apk info -v > "$work/packages.txt"
    printf 'storcli sha256=%s\n' "$STORCLI_SHA256" >> "$work/packages.txt"
fi
install -m 0644 "$root/boot/vmlinuz-lts" "$work/vmlinuz"
find "$root/boot" -mindepth 1 -delete
: > "$root/etc/resolv.conf"
(
    cd "$root"
    find . -print0 | sort -z | cpio --null --quiet --create --format=newc --reproducible |
        gzip -9 -n > "$work/initramfs.gz"
)
(
    cd "$work"
    sha256sum vmlinuz initramfs.gz packages.txt nic-drivers.txt firmware-files.txt > SHA256SUMS
)
for file in vmlinuz initramfs.gz packages.txt nic-drivers.txt firmware-files.txt SHA256SUMS; do
    install -m 0644 "$work/$file" "$out/$file"
done
