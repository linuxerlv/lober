#!/usr/bin/env bash
# 跑一次完整装机：清残留 VM → 取消旧任务 → 建新任务 → 起目标机 → 轮询信号。
#
# 用法：
#   bash scripts/vmlab/run-install.sh [模板ID] [超时秒数]
#
# 只保留名为 vmlab-server 的 QEMU 进程（VM-S），其余一律清掉——残留的目标机
# 会占着 VM-S 的 socket listen 端口，新目标机拿不到网络，症状是
# 「No configuration methods succeeded」（实测踩过两次）。
set -uo pipefail

TEMPLATE_ID="${1:-2a18e4d1-1e2f-4461-83d4-9855825bc31e}"
BUDGET="${2:-1800}"
HOST_SN="${HOST_SN:-VMLAB-T01}"
MAC="${MAC:-52:54:00:30:30:01}"
DISK="${DISK:-D:/qemu-vm/vmlab-targets/t01.qcow2}"
LOG="${LOG:-D:/qemu-vm/vmlab-targets/t01-run.log}"
LAN_PORT="${LAN_PORT:-12520}"
MEM="${MEM:-3072}"
QEMU=/d/msys/ucrt64/bin/qemu-system-x86_64.exe
QEMU_IMG=/d/msys/ucrt64/bin/qemu-img.exe
PWSH=/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe
SSH_VMS=(ssh -i /d/qemu-vm/debian-cloud/id_ed25519 -p 2222
         -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null
         -o LogLevel=ERROR debian@127.0.0.1)

say() { printf '\n=== %s ===\n' "$1"; }

say "清理残留目标机（只留 vmlab-server）"
"$PWSH" -NoProfile -Command '
  $all = Get-CimInstance -ClassName Win32_Process | Where-Object { $_.Name -like "qemu*" }
  foreach ($p in $all) {
    if ($p.CommandLine -notmatch "vmlab-server") {
      "  停 PID " + $p.ProcessId
      Stop-Process -Id $p.ProcessId -Force -ErrorAction SilentlyContinue
    }
  }' 2>/dev/null
sleep 3

say "建新任务"
TASK=$("${SSH_VMS[@]}" "TEMPLATE_ID='$TEMPLATE_ID' HOST_SN='$HOST_SN' bash -s" <<'GUEST'
set -uo pipefail
CA=/etc/lober/tls/console.cert.pem
BASE=https://console.vmlab.internal/console/v1
AT=$(curl -s --cacert $CA -X POST $BASE/auth/login -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"vmlab-admin-pw"}' \
     | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')
api() { curl -s --cacert $CA -X "$1" "$BASE$2" -H "Authorization: Bearer $AT" \
             -H 'Content-Type: application/json' ${3:+-d "$3"}; }
# 取消所有未结束任务：同一 SN 只能有一个活跃任务（idx_tasks_sn_active）。
for id in $(api GET /tasks | python3 -c "
import json,sys
for t in json.load(sys.stdin)['data']:
    if t['state'] not in ('done','failed'): print(t['id'])"); do
  api POST "/tasks/$id/cancel" '{}' >/dev/null
done
api POST /tasks "{\"hostId\":\"$HOST_SN\",\"templateId\":\"$TEMPLATE_ID\",\"formData\":{\"hostname\":\"vmlab-t01\"}}" \
  | python3 -c "
import json,sys
d=json.load(sys.stdin)
print(d['data']['id'] if d['data'] else 'ERROR: '+str(d.get('message')), end='')"
GUEST
)
echo "task=$TASK"
case "$TASK" in ERROR*) echo "建任务失败，中止" >&2; exit 1 ;; esac

say "重建目标机磁盘并启动"
rm -f "$LOG"
"$QEMU_IMG" create -f qcow2 "$DISK" 12G >/dev/null
nohup "$QEMU" -name t01 -machine q35 \
  -accel whpx,kernel-irqchip=off -cpu qemu64 \
  -m "$MEM" -smp 2 \
  -drive file="$DISK",if=virtio,format=qcow2 \
  -netdev "socket,id=lan,connect=127.0.0.1:${LAN_PORT}" \
  -device "virtio-net-pci,netdev=lan,mac=${MAC}" \
  -boot n -display none -nographic > "$LOG" 2>&1 &
echo "目标机已启动，串口日志 $LOG"

say "轮询状态（预算 ${BUDGET}s）"
START=$(date +%s)
LAST=""
while :; do
  sleep 30
  NOW=$(date +%s); ELAPSED=$((NOW-START))
  LINE=$("${SSH_VMS[@]}" "TASK='$TASK' bash -s" <<'GUEST'
set -uo pipefail
CA=/etc/lober/tls/console.cert.pem
BASE=https://console.vmlab.internal/console/v1
AT=$(curl -s --cacert $CA -X POST $BASE/auth/login -H 'Content-Type: application/json' \
     -d '{"username":"admin","password":"vmlab-admin-pw"}' \
     | sed -n 's/.*"accessToken":"\([^"]*\)".*/\1/p')
SIGS=$(sudo grep -oE "signal/$TASK/[^/]+/[a-z-]+\?rc=[0-9]+" /var/log/nginx/lober-boot.log 2>/dev/null \
       | sed "s|signal/[^/]*/[^/]*/||" | tr '\n' ' ')
curl -s --cacert $CA "$BASE/tasks" -H "Authorization: Bearer $AT" | TASK="$TASK" SIGS="$SIGS" python3 -c "
import json,os,sys
task=os.environ['TASK']
for t in json.load(sys.stdin)['data']:
    if t['id']==task:
        d=t.get('failureDetail') or {}
        print(t['state'], t.get('currentPhase'), '|', os.environ['SIGS'].strip(), '|',
              (d.get('logTail') or '')[:80])
        break
GUEST
)
  [ "$LINE" != "$LAST" ] && { printf '[%4ds] %s\n' "$ELAPSED" "$LINE"; LAST="$LINE"; }
  case "$LINE" in
    done*)   say "装机成功（${ELAPSED}s）"; exit 0 ;;
    failed*) say "装机失败（${ELAPSED}s）"; exit 1 ;;
  esac
  if [ "$ELAPSED" -ge "$BUDGET" ]; then say "超出预算 ${BUDGET}s，停止等待"; exit 2; fi
done
