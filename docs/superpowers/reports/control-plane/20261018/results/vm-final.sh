#!/usr/bin/env bash
set -euo pipefail
root=${LOBER_CONTROL_RUN_ROOT:-/home/dev/lober-control-20261018-final}
old=/home/dev/lober-enterprise-20261017-f68432a
export PATH="$old/bin:/usr/local/go/bin:/usr/local/bin:/usr/bin:/bin"
export GOMAXPROCS=2 GOMEMLIMIT=3GiB GOFLAGS='-p=2 -buildvcs=false'
export GIN_MODE=release LOBER_TEST_BASH=/usr/bin/bash LOBER_TEST_BUSYBOX=/usr/bin/busybox
export LOBER_TEST_PG_DSN="$(tr -d '\r\n' < "$old/test-pg.dsn")"
export LOBER_ENTERPRISE_BINARY="$root/lober-test"
export LOBER_ENTERPRISE_FIXTURE="$old/synthetic-rocky.iso"
export LOBER_ENTERPRISE_VM_IP=192.168.137.21
export LOBER_ENTERPRISE_NODE=/usr/local/bin/node
export LOBER_ENTERPRISE_UI_SCRIPT="$root/source/tests/lobertest/enterprise/ui.mjs"
export LOBER_ENTERPRISE_PLAYWRIGHT="$old/frontend-source/web/node_modules/.pnpm/playwright@1.63.0/node_modules/playwright/index.mjs"
cd "$root/source"
product=$(python3 -c 'import json; print(json.load(open("../identities.json"))["product_sha"])')
testsha=$(python3 -c 'import json; print(json.load(open("../identities.json"))["test_sha"])')
name=$1
set +e
case "$name" in
 backend) (make check && go test -count=1 ./...) > "$root/results/backend.log" 2>&1; rc=$?;;
 frontend) (cd /home/dev/lober-security-20261017/frontend/web && pnpm check:console) > "$root/results/frontend.log" 2>&1; rc=$?;;
 gate|control|flows|events|recovery)
  python3 tests/lobertest/enterprise/run.py "$name" --output "$root/results" --product-sha "$product" --test-sha "$testsha" --manifest "$root/candidate.json" > "$root/results/$name.log" 2>&1
  rc=$?;;
 *) exit 2;;
esac
printf '%s\n' "$rc" > "$root/results/$name.exit"
tail -n 7 "$root/results/$name.log"
exit "$rc"
