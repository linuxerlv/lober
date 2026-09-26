#!/usr/bin/env bash
# 重建 VM-S（服务端虚拟机）到 lobertest 项目能用的状态：dnsmasq / nginx /
# postgresql / lober 四个服务 systemctl is-active 均为 active，且
# `lober -config /etc/lober/lober.yaml -check` 退出 0。
#
# 幂等：每一步都先检查当前状态，已经是期望状态则跳过。可以在任意时刻
# 重跑本脚本，包括在服务已经健康的机器上重跑（不会重复造成伤害），也
# 包括在服务处于中间破损状态（比如上一轮部署一半失败）时重跑来收敛。
#
# 前提：
#   - Windows 宿主已装 QEMU（D:/msys/ucrt64/bin/qemu-system-x86_64.exe）、
#     uv、Go 1.27.1
#   - D:/qemu-vm/debian-cloud/ 下已有 base.qcow2 + disk.qcow2（overlay）+
#     start-vm.sh + id_ed25519（VM-S 自身登录密钥）
#   - disk.qcow2 已装好 Debian 13，且已 apt install 好
#     dnsmasq/nginx/postgresql（若这一层缺失，本脚本不负责从裸 cloud-init
#     镜像走到"已装包"这一步——那属于 D:/qemu-vm/debian-cloud/ 本身的
#     初始化历史，本脚本假设它已经发生过一次）
#
# 用法：
#   cd tests/lobertest
#   bash provision/provision.sh
#
# 环境变量覆盖（极少需要，默认值对应本机既有布局）：
#   VM_DIR          默认 D:/qemu-vm/debian-cloud
#   SSH_PORT        默认 2222（须与 VM_DIR/start-vm.sh 的 hostfwd 一致）
#   REPO_ROOT       默认从本脚本位置向上找到仓库根

set -euo pipefail

# ---------------------------------------------------------------------------
# 0. 路径与环境
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LOBERTEST_DIR="$(cd "$SCRIPT_DIR/.." && pwd)"
REPO_ROOT="${REPO_ROOT:-$(cd "$LOBERTEST_DIR/../.." && pwd)}"

# 转成 Windows 原生路径（C:\... 形式）再交给内嵌的 Python 代码。Git
# Bash/MSYS 的 `pwd` 产出 POSIX 风格路径（/e/src/...），Windows 上的
# Python 会把开头的 `/` 当成"当前驱动器的根"而不是 MSYS 的驱动器字母
# 约定，`Path("/e/src/...")` 会解析成 `E:\e\src\...` 这种不存在的路径
# ——本脚本第一版就是这样炸的（NotADirectoryError），必须显式转换，
# 不能假设 Python 会理解 MSYS 风格的路径。
REPO_ROOT_WIN="$(cygpath -w "$REPO_ROOT")"

VM_DIR="${VM_DIR:-D:/qemu-vm/debian-cloud}"
SSH_PORT="${SSH_PORT:-2222}"
SSH_KEY="$VM_DIR/id_ed25519"
SSH_OPTS=(-o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -o ConnectTimeout=15 -i "$SSH_KEY" -p "$SSH_PORT")

export PATH="/d/msys/ucrt64/bin:$PATH"

echo "== lobertest VM-S provision =="
echo "REPO_ROOT   = $REPO_ROOT"
echo "LOBERTEST   = $LOBERTEST_DIR"
echo "VM_DIR      = $VM_DIR"
echo

ssh_run() { ssh "${SSH_OPTS[@]}" debian@127.0.0.1 "$@"; }
scp_push() { scp "${SSH_OPTS[@]}" "$1" "debian@127.0.0.1:$2"; }

# ---------------------------------------------------------------------------
# 1. 确保 VM-S 在跑（幂等：start-vm.sh 不做进程存在性检查，本脚本在
#    这一层加一层，避免重跑本脚本时意外起出第二个 QEMU 进程）。
# ---------------------------------------------------------------------------

echo "-- 步骤 1/7：确保 VM-S QEMU 进程在跑 --"

PWSH="/c/Users/v_lvxiaoyang/AppData/Local/Microsoft/WindowsApps/pwsh.exe"
running_pid="$("$PWSH" -NoProfile -Command '
$p = Get-CimInstance -ClassName Win32_Process | Where-Object { $_.Name -like "qemu-system*" -and $_.CommandLine -match "-name debian-cloud\b" }
if ($p) { $p.ProcessId } else { "" }
' | tr -d '\r')"

if [ -n "$running_pid" ]; then
  echo "已在跑（PID $running_pid），跳过启动。"
else
  echo "未在跑，启动 $VM_DIR/start-vm.sh ..."
  (cd "$VM_DIR" && nohup ./start-vm.sh > /tmp/lobertest-provision-vm-start.log 2>&1 &)
  echo "等待 SSH 端口 $SSH_PORT 就绪 ..."
  for i in $(seq 1 30); do
    if (exec 3<>"/dev/tcp/127.0.0.1/$SSH_PORT") 2>/dev/null; then
      exec 3<&- 3>&-
      echo "端口已开（等待 ${i}x1s）。"
      break
    fi
    sleep 1
  done
fi

# 额外等一小段时间让 sshd 完全起来（端口开了不代表 sshd 已经能应答）。
for i in $(seq 1 20); do
  if ssh_run "true" 2>/dev/null; then
    break
  fi
  sleep 1
done
echo

# ---------------------------------------------------------------------------
# 2. nginx / dnsmasq 配置渡版（幂等：内容一致时跳过推送与重启，避免
#    每次重跑都无意义地 bounce 服务）。
# ---------------------------------------------------------------------------

echo "-- 步骤 2/7：渡 nginx / dnsmasq 配置 --"

deploy_conf_if_changed() {
  local local_file="$1" remote_path="$2" label="$3"
  local remote_hash local_hash
  remote_hash="$(ssh_run "sudo sha256sum '$remote_path' 2>/dev/null | cut -d' ' -f1" || true)"
  local_hash="$(sha256sum "$local_file" | cut -d' ' -f1)"
  if [ "$remote_hash" = "$local_hash" ]; then
    echo "  $label：内容未变，跳过推送。"
    return 1
  fi
  local staging="/tmp/lobertest-provision-$(basename "$remote_path")"
  scp_push "$local_file" "$staging"
  ssh_run "sudo mv '$staging' '$remote_path'"
  echo "  $label：已更新。"
  return 0
}

nginx_changed=0
dnsmasq_changed=0
deploy_conf_if_changed "$SCRIPT_DIR/nginx-lober.conf" "/etc/nginx/sites-available/lober" "nginx" && nginx_changed=1 || true
deploy_conf_if_changed "$SCRIPT_DIR/dnsmasq-lober.conf" "/etc/dnsmasq.d/lober.conf" "dnsmasq" && dnsmasq_changed=1 || true

echo "  校验语法 ..."
ssh_run "sudo nginx -t"
ssh_run "sudo dnsmasq --test --conf-file=/etc/dnsmasq.d/lober.conf"

if [ "$nginx_changed" = "1" ] || [ "$(ssh_run "systemctl is-active nginx" || true)" != "active" ]; then
  ssh_run "sudo systemctl restart nginx"
fi
if [ "$dnsmasq_changed" = "1" ] || [ "$(ssh_run "systemctl is-active dnsmasq" || true)" != "active" ]; then
  ssh_run "sudo systemctl restart dnsmasq"
fi
echo

# ---------------------------------------------------------------------------
# 3. Postgres：lober_test 角色 + lober_test 数据库（幂等：存在则跳过）。
# ---------------------------------------------------------------------------

echo "-- 步骤 3/7：Postgres 角色与数据库 --"

PG_TEST_PASSWORD="lobertest_pw"  # 必须与 lobertest.config.TEST_PG_PASSWORD 一致

role_exists="$(ssh_run "sudo -u postgres psql -tAc \"SELECT 1 FROM pg_roles WHERE rolname='lober_test'\"" || true)"
if [ "$role_exists" = "1" ]; then
  echo "  角色 lober_test 已存在，跳过创建；仍确保密码是本脚本预期的值（幂等 ALTER）。"
  ssh_run "sudo -u postgres psql -c \"ALTER ROLE lober_test WITH PASSWORD '$PG_TEST_PASSWORD';\""
else
  echo "  创建角色 lober_test ..."
  ssh_run "sudo -u postgres psql -c \"CREATE ROLE lober_test LOGIN PASSWORD '$PG_TEST_PASSWORD';\""
fi

db_exists="$(ssh_run "sudo -u postgres psql -tAc \"SELECT 1 FROM pg_database WHERE datname='lober_test'\"" || true)"
if [ "$db_exists" = "1" ]; then
  echo "  数据库 lober_test 已存在，跳过创建。"
else
  echo "  创建数据库 lober_test ..."
  ssh_run "sudo -u postgres psql -c 'CREATE DATABASE lober_test OWNER lober_test;'"
fi
ssh_run "sudo -u postgres psql -c 'GRANT ALL PRIVILEGES ON DATABASE lober_test TO lober_test;'"
echo

# ---------------------------------------------------------------------------
# 4. 交叉编译（第一道闸门：失败立即停止，不做任何后续步骤）。
# ---------------------------------------------------------------------------

echo "-- 步骤 4/7：交叉编译 linux/amd64 --"

cd "$LOBERTEST_DIR"
mkdir -p .tmp
uv run python -c "
from pathlib import Path
from lobertest import deploy

repo_root = Path(r'$REPO_ROOT_WIN')
out = Path('.tmp/lober-linux-amd64')
result = deploy.cross_compile(repo_root, out)
print('交叉编译产出：', result, result.stat().st_size, '字节')
"
echo

# ---------------------------------------------------------------------------
# 5. 渡 lober.yaml（用 deploy.py 的真实 render_config/build_test_users，
#    不手写 YAML——这样任何字段形状的改动都会自动反映到 provision 里，
#    不需要在两处分别维护同一份配置）。
# ---------------------------------------------------------------------------

echo "-- 步骤 5/7：渡 lober.yaml --"

uv run python -c "
from pathlib import Path
from lobertest import deploy
from lobertest.config import load

repo_root = Path(r'$REPO_ROOT_WIN')
settings = load(env={})
users = deploy.build_test_users(repo_root)
config_text = deploy.render_config(schema=settings.schema, users=users)
Path('.tmp/lober.yaml').write_text(config_text, encoding='utf-8')
print('已渡 config，schema =', settings.schema)
for u in users:
    print(' ', u.name, u.roles)
"
echo

# ---------------------------------------------------------------------------
# 6. schema + 部署 + 重启（deploy.py 的 create_schema 已是幂等的
#    CREATE SCHEMA IF NOT EXISTS ... AUTHORIZATION lober_test）。
# ---------------------------------------------------------------------------

echo "-- 步骤 6/7：建 schema、推送、重启 --"

uv run python -c "
from pathlib import Path
from lobertest import deploy
from lobertest.config import load
from lobertest.ssh import SSH

settings = load(env={})
binary = Path('.tmp/lober-linux-amd64')
config_text = Path('.tmp/lober.yaml').read_text(encoding='utf-8')

with SSH(settings) as ssh:
    deploy.create_schema(ssh, settings.schema)
    deploy.deploy(ssh, binary, config_text)
print('部署完成')
"
echo

# ---------------------------------------------------------------------------
# 7. 等就绪并汇报四个服务状态。
# ---------------------------------------------------------------------------

echo "-- 步骤 7/7：等待就绪 --"

uv run python -c "
from lobertest import deploy
from lobertest.config import load
from lobertest.ssh import SSH
from lobertest.api import Console

settings = load(env={})
console = Console(settings.console_base_url)

with SSH(settings) as ssh:
    deploy.wait_ready(ssh, console, timeout=60)
print('lober 三条就绪判据全过')
"

echo
echo "== 四个服务最终状态 =="
for s in dnsmasq nginx postgresql lober; do
  status="$(ssh_run "systemctl is-active $s" || true)"
  echo "  $s: $status"
done

echo
echo "== lober -check 退出码 =="
ssh_run "sudo /opt/lober/bin/lober -config /etc/lober/lober.yaml -check" && echo "  exit 0" || echo "  exit $?"

echo
echo "provision 完成。清理临时编译产物 ..."
rm -f "$LOBERTEST_DIR/.tmp/lober-linux-amd64" "$LOBERTEST_DIR/.tmp/lober.yaml"
echo "完成。可运行 'uv run pytest -m needs_vm -v' 验证。"
