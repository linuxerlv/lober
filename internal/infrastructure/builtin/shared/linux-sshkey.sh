#!/bin/sh
set -eu
: "${LOBER_PARAM_USERNAME:?username required}"
: "${LOBER_PARAM_KEY:?SSH key required}"
entry=$(getent passwd "$LOBER_PARAM_USERNAME") || exit 67
IFS=: read -r name password uid gid gecos home shell <<EOF
$entry
EOF
[ "$name" = "$LOBER_PARAM_USERNAME" ] || exit 67
case "$home" in /*) ;; *) exit 67;; esac
umask 077
install -d -m 700 -o "$uid" -g "$gid" -- "$home/.ssh"
printf '%s\n' "$LOBER_PARAM_KEY" >> "$home/.ssh/authorized_keys"
chown "$uid:$gid" -- "$home/.ssh/authorized_keys"
chmod 600 -- "$home/.ssh/authorized_keys"
if command -v restorecon >/dev/null 2>&1; then restorecon -R -- "$home/.ssh"; fi
