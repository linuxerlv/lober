#!/bin/sh
set -eu
: "${LOBER_PARAM_KEY:?SSH key required}"
[ "$LOBER_PARAM_USERNAME" = root ] || exit 64
umask 077
mkdir -p /etc/ssh/keys-root
printf '%s\n' "$LOBER_PARAM_KEY" >> /etc/ssh/keys-root/authorized_keys
chmod 600 /etc/ssh/keys-root/authorized_keys
vim-cmd hostsvc/enable_ssh
vim-cmd hostsvc/start_ssh
/sbin/auto-backup.sh
