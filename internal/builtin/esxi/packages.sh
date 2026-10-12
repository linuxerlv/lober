#!/bin/sh
set -eu
: "${LOBER_PARAM_PACKAGES:?individual qualified VIB URLs required}"
esxcli system maintenanceMode set --enable true
printf '%s\n' "$LOBER_PARAM_PACKAGES" | while IFS= read -r package; do
    esxcli software vib install --viburl="$package"
done
esxcli system maintenanceMode set --enable false
