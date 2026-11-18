#!/bin/sh

case "$1" in
    bound|renew)
        ip addr flush dev "$interface"
        # ifconfig 直接使用 DHCP 给出的点分十进制掩码。
        ifconfig "$interface" "$ip" netmask "${subnet:-255.255.255.255}" up || exit 1
        for gateway in $router; do
            ip route replace default via "$gateway" dev "$interface"
            break
        done
        : > /etc/resolv.conf
        for server in $dns; do
            printf 'nameserver %s\n' "$server" >> /etc/resolv.conf
        done
        : > /run/lober-address-ready
        ;;
esac
