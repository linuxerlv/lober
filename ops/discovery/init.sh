#!/bin/sh

export PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
export LC_ALL=C
umask 077
mount -t proc proc /proc
mount -t sysfs sysfs /sys
mount -t devtmpfs devtmpfs /dev
mount -t tmpfs -o mode=0755 tmpfs /run
exec </dev/console >/dev/console 2>&1

finish() {
    echo "Lober: $1"
    echo "Lober: discovery finished; waiting at this screen"
    # 作为 PID 1 保持运行。避免 PXE 排首位的机器反复重启。
    while :; do sleep 3600; done
}

boot_mac=
for argument in $(cat /proc/cmdline); do
    case "$argument" in
        lober.discovery_mac=*) boot_mac=${argument#*=} ;;
    esac
done
boot_mac=$(printf '%s' "$boot_mac" | tr 'A-F-' 'a-f:')
ip link set lo up
echo "Lober: loading local hardware drivers"
for bus in pci usb platform acpi; do
    for alias in /sys/bus/"$bus"/devices/*/modalias; do
        [ -r "$alias" ] || continue
        read -r device < "$alias"
        [ -n "$device" ] && timeout 6 modprobe -b "$device" 2>/dev/null
    done
done
for driver in af_packet virtio_net virtio_blk virtio_scsi sd_mod ses ipmi_si ipmi_devintf coretemp k10temp drivetemp; do
    timeout 6 modprobe "$driver" 2>/dev/null
done
mdev -s

preferred=
candidates=
for port in /sys/class/net/*; do
    [ -d "$port/device" ] && [ -r "$port/address" ] || continue
    name=${port##*/}
    read -r mac < "$port/address"
    if [ -n "$boot_mac" ] && [ "$mac" = "$boot_mac" ]; then
        preferred=$name
    else
        candidates="$candidates $name"
    fi
done
if [ -z "$preferred$candidates" ]; then
    echo "Lober: no usable physical network interface"
    for device in /sys/bus/pci/devices/*; do
        [ -r "$device/class" ] || continue
        read -r class < "$device/class"
        case "$class" in
            0x02*) printf '%s ' "${device##*/}"; cat "$device/vendor" "$device/device" ;;
        esac
    done
    dmesg | grep -Ei 'firmware.*(failed|not found)|module.*(error|fail)' | tail -n 20
    finish "network unavailable; hardware report not uploaded"
fi
active=
attempts=0
for name in $preferred $candidates; do
    attempts=$((attempts + 1))
    [ "$attempts" -le 4 ] || break
    echo "Lober: requesting network address on $name"
    ip link set dev "$name" up || continue
    rm -f /run/lober-address-ready
    if udhcpc -f -n -q -t 3 -T 3 -i "$name" -s /opt/lober/dhcp &&
       [ -f /run/lober-address-ready ]; then
        active=$name
        break
    fi
    ip link set dev "$name" down
done
if [ -z "$active" ]; then
    finish "network address unavailable; hardware report not uploaded"
fi
echo "Lober: network address acquired on $active"
if python3 /opt/lober/collect.py; then
    finish "hardware report uploaded"
fi
finish "network connected; hardware report not uploaded"
