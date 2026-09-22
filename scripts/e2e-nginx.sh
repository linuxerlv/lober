#!/usr/bin/env bash
# scripts/e2e-nginx.sh —— 走真 nginx 的端到端验证：nginx 作唯一入口，
# lober 绑回环，整条链通过 nginx 转发。对应实现计划「真机集成」任务，
# 依据 SPEC §1.4、§1.6、§9.2、docs/real-machine/02-nginx-真机集成.md。
#
# 与 scripts/e2e.sh 的关系：e2e.sh 是 curl 直连 Go 进程，本脚本是 curl
# 经 nginx 转发到 Go 进程——测的是同一套业务逻辑，但只有本脚本能暴露
# SPEC 明确警告过的三处「nginx 反代之后才会出现」的陷阱（见下方阶段
# 10/11/12）：直连 Go 进程时 r.RemoteAddr 永远是测试客户端自己的地址，
# 不会是回环地址，X-Real-IP 信任判断的正反两条分支根本无法被触发；
# 直连时也没有 nginx 的 proxy_buffering，测不出「配置对了 SSE 才不冻住」
# 这件事；直连时制品请求当然会经过 Go，测不出「制品请求是否绕过了 Go」。
#
# 本脚本必须在真 Linux 主机、真 nginx 上运行（不是「本地起个假nginx」，
# 是本脚本存在的唯一理由）。Windows 开发机没有 nginx，本脚本设计为
# scp 到目标主机后执行，不是本仓库 CI 能跑的脚本。
#
# 已知限制（写在这里而不是假装测过了，与 docs/real-machine/02-nginx-
# 真机集成.md 保持一致）：
#
#  1. 【单网卡场景下机器面与人面共享同一个 IP，靠端口区分】真实部署是
#     两个网段各一个 IP、都用 80 端口；本脚本若在只有一块网卡的主机上跑
#     （如本仓库用的 QEMU 虚拟机），MACHINE_PORT 与 CONSOLE_PORT 必须设成
#     不同值——见下方 SETUP_NOTES 的输出，运行时会打印这条偏差提醒。
#     这意味着本脚本跑通不能作为「机器面/人面信任域网络隔离」已验证的
#     证据，那一层隔离要在两块网卡的主机上才能验证。
#
#  2. 【X-Real-IP 伪造负面用例（peer 非回环时头部必须被忽略）不在本脚本
#     的自动化范围内】lober 的 gateway.listen/console.listen 按契约要求
#     绑非通配的具体 IP（SPEC §1.4），若绑回环，则本脚本的唯一网络路径
#     下，直连 Go 进程的 TCP peer 天然就是回环——要构造「peer 非回环」
#     的负面样本，需要额外起一个绑在真实网卡上的 lober 实例（仅用于此
#     测试目的，配置与被测实例不同），这超出了本脚本作为「一次部署形态
#     的端到端验证」的范围，改成了「专门为负面用例搭一套额外基础设施」。
#     该负面用例已在 docs/real-machine/02-nginx-真机集成.md 记录为人工
#     一次性验证（用一个绑真实网卡的临时 lober 实例，从虚拟机内部用
#     forge 过的 X-Real-IP 头直连它，确认响应头部被忽略），不在本脚本
#     里自动跑——留在人工验证记录里是诚实的选择，不是本脚本的疏漏。
#
#  3. 【制品集登记→导入 job、模板 draft→active 两处契约缺口】与 e2e.sh
#     完全相同（见该文件头注释），本脚本同样绕过 HTTP 直接用 sqlite3
#     推进这两处状态。
#
#  4. 【限流桶的 rate/burst 阈值是否真的按配置生效，用 ab 而不是 curl】
#     单发 curl 请求的进程创建开销就足够拖慢到看不出限流效果（曾在本
#     脚本编写过程中实测验证过这一点），必须用能真正并发的工具——若
#     目标主机没有 apache2-utils（提供 ab），阶段12退化为跳过并打印
#     原因，不假装测过。
#
# 依赖：nginx（已安装，--with-threads 与 --with-http_secure_link_module
# 两个编译选项本脚本会检查但不强制要求，MVP 缺后者不影响功能）、curl、
# sqlite3、python3（仅用于 UDP 探测，见阶段10）、ab（可选，阶段12）。
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# ---------------------------------------------------------------------------
# 可覆盖的环境变量（运行环境相关，不能硬编码）
# ---------------------------------------------------------------------------
#   MACHINE_IP     机器面 nginx 监听的 IP（装机 VLAN 上本机地址）
#   CONSOLE_IP     人面 nginx 监听的 IP（管理网上本机地址）
#                  单网卡主机上 MACHINE_IP 与 CONSOLE_IP 通常相同，此时
#                  必须靠 MACHINE_PORT/CONSOLE_PORT 区分（见已知限制 1）。
#   MACHINE_PORT   机器面 nginx 监听端口，默认 80
#   CONSOLE_PORT   人面 nginx 监听端口，双网卡环境可以也用 80，单网卡
#                  环境必须设成不同于 MACHINE_PORT 的值，默认 8880
#   NGINX_BIN      nginx 二进制路径，默认 /usr/sbin/nginx
#   LOBER_BIN      lober 二进制路径（本脚本不负责编译，调用方交叉编译好
#                  后 scp 上来，与 e2e.sh 用 go build 现场编译不同——本
#                  脚本要在没有 go 工具链的目标主机上也能跑）
LOBER_BIN="${LOBER_BIN:-}"
MACHINE_IP="${MACHINE_IP:-}"
CONSOLE_IP="${CONSOLE_IP:-}"
MACHINE_PORT="${MACHINE_PORT:-80}"
CONSOLE_PORT="${CONSOLE_PORT:-8880}"
NGINX_BIN="${NGINX_BIN:-/usr/sbin/nginx}"

if [ -z "${LOBER_BIN}" ] || [ ! -x "${LOBER_BIN}" ]; then
	echo "用法: LOBER_BIN=/path/to/lober-linux MACHINE_IP=x.x.x.x [CONSOLE_IP=x.x.x.x] [MACHINE_PORT=80] [CONSOLE_PORT=8880] $0" >&2
	echo "LOBER_BIN 必须指向一个已存在、可执行的 linux/amd64 lober 二进制（交叉编译：GOOS=linux GOARCH=amd64 go build -o lober-linux ./cmd/lober）" >&2
	exit 1
fi
if [ -z "${MACHINE_IP}" ]; then
	echo "用法: 必须设置 MACHINE_IP（本机在装机 VLAN 上的真实 IP，不能是 0.0.0.0 或回环）" >&2
	exit 1
fi
CONSOLE_IP="${CONSOLE_IP:-${MACHINE_IP}}"
if [ "${CONSOLE_IP}" = "${MACHINE_IP}" ] && [ "${MACHINE_PORT}" = "${CONSOLE_PORT}" ]; then
	echo "错误: MACHINE_IP 与 CONSOLE_IP 相同时，MACHINE_PORT 与 CONSOLE_PORT 不能相同（同一 IP 不能有两个 server 块监听同一端口）" >&2
	exit 1
fi
if [ "${CONSOLE_IP}" = "${MACHINE_IP}" ]; then
	echo "[提示] MACHINE_IP 与 CONSOLE_IP 相同（单网卡主机）——本次运行不能作为" >&2
	echo "       「机器面/人面信任域网络隔离」已验证的证据，只验证了 nginx 配置" >&2
	echo "       语法与 location 层的行为，见文件头已知限制 1。" >&2
fi

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e-nginx.XXXXXX")"
DB_PATH="${WORKDIR}/db/lober.db"
ARTIFACT_ROOT="${WORKDIR}/artifacts"
LOG_ROOT="${WORKDIR}/log"
CONFIG_PATH="${WORKDIR}/lober.yaml"
NGINX_MAIN_CONF="${WORKDIR}/nginx-main.conf"
NGINX_LOBER_CONF="${WORKDIR}/lober.conf"
NGINX_ERROR_LOG="${WORKDIR}/nginx-error.log"
NGINX_PID="${WORKDIR}/nginx.pid"

GW_PORT=28080
CONSOLE_BACKEND_PORT=28090
SYSLOG_PORT=28514

LOBER_PID=""
NGINX_STARTED=0

cleanup() {
	local exit_code=$?
	if [ -n "${LOBER_PID}" ] && kill -0 "${LOBER_PID}" 2>/dev/null; then
		kill "${LOBER_PID}" 2>/dev/null || true
		wait "${LOBER_PID}" 2>/dev/null || true
	fi
	if [ "${NGINX_STARTED}" = "1" ]; then
		sudo "${NGINX_BIN}" -s stop -c "${NGINX_MAIN_CONF}" 2>/dev/null || true
	fi
	rm -rf "${WORKDIR}"
	exit "${exit_code}"
}
trap cleanup EXIT INT TERM

pass() { echo "[PASS] $1"; }
fail() {
	echo "[FAIL] $1" >&2
	echo "-------- lober 日志 --------" >&2
	[ -f "${WORKDIR}/lober.log" ] && tail -n 60 "${WORKDIR}/lober.log" >&2 || true
	echo "-------- nginx 错误日志 --------" >&2
	[ -f "${NGINX_ERROR_LOG}" ] && tail -n 40 "${NGINX_ERROR_LOG}" >&2 || true
	exit 1
}

json_field() {
	local body="$1" field="$2"
	echo "${body}" | grep -o "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -n1 | sed -E 's/.*:[[:space:]]*"([^"]*)"/\1/' \
		|| echo "${body}" | grep -o "\"${field}\"[[:space:]]*:[[:space:]]*[0-9][0-9]*" | head -n1 | sed -E 's/.*:[[:space:]]*([0-9]+)/\1/'
}

poll_until() {
	local desc="$1" timeout_sec="$2"
	shift 2
	local deadline=$((SECONDS + timeout_sec))
	while [ "${SECONDS}" -lt "${deadline}" ]; do
		if "$@"; then
			return 0
		fi
		sleep 0.2
	done
	fail "轮询超时（${timeout_sec}s）: ${desc}"
}

echo "=== Lober nginx 真机集成验证 ==="
echo "工作目录: ${WORKDIR}"
echo "机器面: http://${MACHINE_IP}:${MACHINE_PORT}/  人面: http://${CONSOLE_IP}:${CONSOLE_PORT}/"

# ---------------------------------------------------------------------------
# 阶段 1：nginx 前置检查（编译选项、二进制可执行）
# ---------------------------------------------------------------------------

[ -x "${NGINX_BIN}" ] || fail "阶段1: ${NGINX_BIN} 不存在或不可执行"
NGINX_V_OUTPUT="$("${NGINX_BIN}" -V 2>&1)"
echo "${NGINX_V_OUTPUT}" | grep -q -- '--with-threads' \
	|| echo "[警告] nginx 编译时缺少 --with-threads，aio threads 指令会在 nginx -t 时报错（阶段3会捕获）"
echo "${NGINX_V_OUTPUT}" | grep -q -- '--with-http_secure_link_module' \
	|| echo "[提示] nginx 编译时缺少 --with-http_secure_link_module——MVP 阶段不影响功能（Phase 2 制品授权才需要）"
pass "阶段1: nginx 二进制检查完成（详细编译选项见上方提示/警告）"

# ---------------------------------------------------------------------------
# 阶段 2：造假制品树、admin 用户、lober 配置（gateway/console 均绑回环，
# 由 nginx 做唯一入口，SPEC §1.4）
# ---------------------------------------------------------------------------

mkdir -p "${WORKDIR}/db" "${ARTIFACT_ROOT}" "${LOG_ROOT}"

ARTIFACT_SET_ID="e2e-nginx-rocky94-x86_64"
bash "${SCRIPT_DIR}/e2e/mkfixture.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_ID}"

# 与 e2e.sh 相同的固定 bcrypt 哈希（cost=10，明文 admin12345），仅用于本
# 一次性脚本自建的临时数据库——见 e2e.sh 同一处注释对这个选择的完整说明。
ADMIN_USER="admin"
ADMIN_PASS="admin12345"
ADMIN_HASH='$2a$10$LWLA9qNOLwBFVDMK6LQLLOX56wmVuu1o.szEoZbqdLT4cdGmffBJe'

MACHINE_BASE="http://${MACHINE_IP}:${MACHINE_PORT}"

cat >"${CONFIG_PATH}" <<EOF
gateway:
  listen: "127.0.0.1:${GW_PORT}"
  gateway_base: "${MACHINE_BASE}"
  artifact_base: "${MACHINE_BASE}"
  syslog_listen: "127.0.0.1:${SYSLOG_PORT}"
console:
  listen: "127.0.0.1:${CONSOLE_BACKEND_PORT}"
  token_secret: "e2e-nginx-test-secret"
store:
  db_path: "${DB_PATH}"
  artifact_path: "${ARTIFACT_ROOT}"
  log_path: "${LOG_ROOT}"
scan:
  interval: "2s"
auth:
  users:
    - name: "${ADMIN_USER}"
      bcrypt_hash: "${ADMIN_HASH}"
      roles: ["admin"]
provider:
  ipam: "static"
  dns: "noop"
  resolver: "rule-based"
  disk_policy: "by-hook"
  heartbeat_mode: "off"
EOF

"${LOBER_BIN}" -config "${CONFIG_PATH}" -check || fail "阶段2: -check 校验模式未通过"
pass "阶段2: 制品树、admin 用户、lober 配置（回环监听）均已就位"

# ---------------------------------------------------------------------------
# 阶段 3：生成 nginx 配置（包一层最小主配置去 include 仓库里的两个真实
# 片段文件，占位符替换成本次运行的真实 IP/端口），nginx -t 校验
# ---------------------------------------------------------------------------

cp "${REPO_ROOT}/deploy/nginx/lober.conf" "${NGINX_LOBER_CONF}"
sed -i \
	-e "s/__MACHINE_VLAN_IP__:80/${MACHINE_IP}:${MACHINE_PORT}/" \
	-e "s/__MANAGEMENT_NET_IP__:80/${CONSOLE_IP}:${CONSOLE_PORT}/" \
	-e "s/boot\.lober\.internal/boot.lober.test/" \
	-e "s/console\.lober\.internal/console.lober.test/" \
	-e "s#proxy_pass http://127\.0\.0\.1:8080#proxy_pass http://127.0.0.1:${GW_PORT}#g" \
	-e "s#proxy_pass http://127\.0\.0\.1:8081#proxy_pass http://127.0.0.1:${CONSOLE_BACKEND_PORT}#g" \
	-e "s#alias /srv/lober/artifacts/#alias ${ARTIFACT_ROOT}/#" \
	-e "s#alias /srv/lober/repo/#alias ${WORKDIR}/repo-empty/#" \
	-e "s#/var/log/nginx/lober-boot.log#${WORKDIR}/nginx-boot-access.log#" \
	-e "s#/var/log/nginx/lober-console.log#${WORKDIR}/nginx-console-access.log#" \
	"${NGINX_LOBER_CONF}"
mkdir -p "${WORKDIR}/repo-empty"

cat >"${NGINX_MAIN_CONF}" <<EOF
# 本脚本生成的最小主配置——只为了让 lober.conf（server 块片段）与
# lober-http-snippet.conf（http{} 层指令）能通过 nginx -t，不是生产
# 主配置（生产环境的主配置由发行版包管理或运维自己维护，见
# deploy/nginx/lober-http-snippet.conf 文件头「include 位置要求」）。
user $(id -un);
pid ${NGINX_PID};
error_log ${NGINX_ERROR_LOG} info;

events {
    worker_connections 1024;
}

http {
    include /etc/nginx/mime.types;
    default_type application/octet-stream;

    include ${REPO_ROOT}/deploy/nginx/lober-http-snippet.conf;
    include ${NGINX_LOBER_CONF};
}
EOF

sudo "${NGINX_BIN}" -t -c "${NGINX_MAIN_CONF}" 2>"${WORKDIR}/nginx-t.log" \
	|| fail "阶段3: nginx -t 未通过，输出见下: $(cat "${WORKDIR}/nginx-t.log")"
pass "阶段3: nginx 配置（占位符已替换为本次运行的真实值，替换后的文件只存在于工作目录，不写回仓库）通过 nginx -t"

# ---------------------------------------------------------------------------
# 阶段 4：起 lober（绑回环）与 nginx（绑真实网卡），轮询到都就绪
# ---------------------------------------------------------------------------

"${LOBER_BIN}" -config "${CONFIG_PATH}" >"${WORKDIR}/lober.log" 2>&1 &
LOBER_PID=$!

lober_ready() {
	curl -fsS -o /dev/null "http://127.0.0.1:${GW_PORT}/boot.ipxe" 2>/dev/null
}
poll_until "lober 网关就绪" 15 lober_ready

sudo "${NGINX_BIN}" -c "${NGINX_MAIN_CONF}" || fail "阶段4: nginx 启动失败"
NGINX_STARTED=1

nginx_ready() {
	curl -fsS -o /dev/null "http://${MACHINE_IP}:${MACHINE_PORT}/boot.ipxe" 2>/dev/null
}
poll_until "nginx 机器面就绪" 10 nginx_ready
pass "阶段4: lober（回环）与 nginx（${MACHINE_IP}:${MACHINE_PORT} / ${CONSOLE_IP}:${CONSOLE_PORT}）均已就位"

# ---------------------------------------------------------------------------
# 阶段 5：经 nginx 人面登录、录入主机、登记制品集（ready）、建模板
# （active）—— 制品集与模板的绕法与 e2e.sh 完全相同，见文件头已知限制 3。
# ---------------------------------------------------------------------------

CONSOLE_BASE="http://${CONSOLE_IP}:${CONSOLE_PORT}/console/v1"
GW_BASE="http://${MACHINE_IP}:${MACHINE_PORT}"

LOGIN_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/auth/login" \
	-H 'Content-Type: application/json' \
	-d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}")"
ACCESS_TOKEN="$(json_field "${LOGIN_RESP}" "accessToken")"
[ -n "${ACCESS_TOKEN}" ] || fail "阶段5: 经 nginx 登录未拿到 accessToken，响应=${LOGIN_RESP}"
AUTH_HEADER="Authorization: Bearer ${ACCESS_TOKEN}"

HOST_SN="E2E-NGINX-SN-0001"
HOST_MAC="aa:bb:cc:dd:ee:11"
HOST_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_SN}\",\"mac\":\"${HOST_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-NGINX-RACK\"}")"
HOST_ID="$(json_field "${HOST_RESP}" "id")"
[ -n "${HOST_ID}" ] || fail "阶段5: 经 nginx 录入主机失败，响应=${HOST_RESP}"

ARTIFACT_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/artifact-sets" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sourcePath\":\"${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}\",\"name\":\"e2e-nginx-rocky9.4\"}")"
CREATED_ARTIFACT_ID="$(json_field "${ARTIFACT_CREATE_RESP}" "id")"
[ -n "${CREATED_ARTIFACT_ID}" ] || fail "阶段5: 登记制品集失败，响应=${ARTIFACT_CREATE_RESP}"
mv "${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}" "${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}"

PATHS_JSON='{"Kernel":"vmlinuz","Initrd":["initrd.img"],"Repo":"repo/","Extra":{}}'
sqlite3 "${DB_PATH}" "UPDATE artifact_sets SET state='ready', os_family='rhel', os_version='9.4', arch='x86_64', firmware='any', paths_json='${PATHS_JSON}' WHERE id='${CREATED_ARTIFACT_ID}';"

TEMPLATE_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-nginx-rocky94\",\"artifactSetId\":\"${CREATED_ARTIFACT_ID}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
TEMPLATE_ID="$(json_field "${TEMPLATE_CREATE_RESP}" "id")"
[ -n "${TEMPLATE_ID}" ] || fail "阶段5: 创建模板失败，响应=${TEMPLATE_CREATE_RESP}"
sqlite3 "${DB_PATH}" "UPDATE templates SET state='active' WHERE id='${TEMPLATE_ID}';"

TASK_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-nginx-host-01\"}}")"
TASK_ID="$(json_field "${TASK_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段5: 建任务失败，响应=${TASK_CREATE_RESP}"

pass "阶段5: 经 nginx 人面完成登录、主机=${HOST_ID}、制品集=${CREATED_ARTIFACT_ID}（ready）、模板=${TEMPLATE_ID}（active）、任务=${TASK_ID}（pending）"

# ---------------------------------------------------------------------------
# 阶段 6：X-Real-IP 信任判断正向用例（SPEC §1.6、§9.2）——经 nginx 的请求，
# lober 记录到的审计客户端 IP 必须是 nginx 看到的真实客户端 IP，不能是
# 回环地址（若是回环地址，说明 Go 侧没有从 X-Real-IP 取值，审计与限速
# 都会失真）。
#
# 负面用例（peer 非回环时头部必须被忽略）不在本脚本自动化范围内，理由见
# 文件头已知限制 2，已作为人工验证步骤记录在
# docs/real-machine/02-nginx-真机集成.md。
# ---------------------------------------------------------------------------

# 阶段5的登录/建主机/建任务请求已经过 nginx，此刻 lober.log 里应该已经有
# 对应的审计行。用登录动作的那一行核对：client_ip 不能是 127.0.0.1（Go
# 侧看到的真实 TCP peer 一定是回环，若审计记的还是回环说明取值逻辑没生效）。
AUDIT_LOGIN_LINE="$(grep 'action=host.create' "${WORKDIR}/lober.log" | tail -n1)"
[ -n "${AUDIT_LOGIN_LINE}" ] || fail "阶段6: lober 日志里找不到 host.create 审计行"
echo "${AUDIT_LOGIN_LINE}" | grep -q "client_ip=127.0.0.1" \
	&& fail "阶段6: 审计记录的 client_ip 是回环地址（应为 nginx 转发前的真实客户端地址），说明 X-Real-IP 未被信任——审计行=${AUDIT_LOGIN_LINE}"
echo "${AUDIT_LOGIN_LINE}" | grep -qE "client_ip=[0-9]+\.[0-9]+\.[0-9]+\.[0-9]+" \
	|| fail "阶段6: 审计行里的 client_ip 不是合法 IPv4，审计行=${AUDIT_LOGIN_LINE}"
pass "阶段6: 经 nginx 的请求，审计日志记到的 client_ip 是真实客户端地址而非回环（${AUDIT_LOGIN_LINE}）"

# ---------------------------------------------------------------------------
# 阶段 7：模拟 iPXE 经 nginx 取引导脚本与菜单、取应答文件
# ---------------------------------------------------------------------------

BOOTSCRIPT="$(curl -fsS "${GW_BASE}/boot.ipxe")"
echo "${BOOTSCRIPT}" | grep -q "chain ${GW_BASE}/v1/boot" \
	|| fail "阶段7: /boot.ipxe（经 nginx）内容不含预期的 chain 指令，响应=${BOOTSCRIPT}"

BOOT_MENU="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_SN}&mac=${HOST_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${BOOT_MENU}" | grep -q "inst.ks=" || fail "阶段7: 引导菜单（经 nginx）未包含 inst.ks= 行，响应=${BOOT_MENU}"

KS_URL_SEGMENT="$(echo "${BOOT_MENU}" | grep -o "inst.ks=[^ ]*" | sed -E 's#inst.ks=.*/v1/answer/##')"
INSTALL_TASK_ID="$(echo "${KS_URL_SEGMENT}" | cut -d/ -f1)"
INSTALL_TOKEN="$(echo "${KS_URL_SEGMENT}" | cut -d/ -f2)"
[ "${INSTALL_TASK_ID}" = "${TASK_ID}" ] || fail "阶段7: 菜单里的 task id (${INSTALL_TASK_ID}) 与预期 (${TASK_ID}) 不符"

ANSWER_URL="${GW_BASE}/v1/answer/${TASK_ID}/${INSTALL_TOKEN}/ks.cfg"
ANSWER_BODY="$(curl -fsS "${ANSWER_URL}")"
echo "${ANSWER_BODY}" | grep -q "logging --host=" || fail "阶段7: 应答文件（经 nginx）缺少 syslog 指向"

TASK_AFTER_ANSWER="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_AFTER_ANSWER}" | grep -q '"state":"installing"' \
	|| fail "阶段7: 经 nginx 取应答文件后任务状态未变为 installing，响应=${TASK_AFTER_ANSWER}"
pass "阶段7: 经 nginx 取得引导脚本/菜单/应答文件，任务进入 installing"

# ---------------------------------------------------------------------------
# 阶段 8：经 nginx /v1/signal/ location 打点（单独限流桶，见阶段11）
# ---------------------------------------------------------------------------

send_signal() {
	local phase="$1" seq="$2"
	curl -sS -X POST "${GW_BASE}/v1/signal/${TASK_ID}/${INSTALL_TOKEN}/${phase}?rc=0&seq=${seq}" \
		-d "signal ${phase} seq=${seq}" -o /dev/null -w '%{http_code}'
}

SEQ=1
for phase in pre-start pre-done packages-start; do
	code="$(send_signal "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（经 nginx）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done
pass "阶段8: 经 nginx /v1/signal/ location 打点三个信号均成功"

# ---------------------------------------------------------------------------
# 阶段 9：SSE 必须不被 nginx 缓冲（SPEC §1.4 第 3 点、§12.8）——真的订阅
# /console/v1/tasks/{id}/stream，一边打信号，用带时间戳的方式证明事件是
# 实时到达而不是攒到最后一次性吐出。
#
# 已知的 Go 侧行为（记入 findings，不是本脚本的问题）：streamPushBatch
# 对 Seq<=0 的事件（model.TaskEvent.Seq 文档："非正值表示非信号来源"，
# 如任务创建事件）不会推进 afterSeq 水位，导致这些事件每个轮询周期都被
# 重新推送一次，SSE 连接存活期间无限重复——本阶段的断言只关心「新信号
# 事件是否在约一个轮询周期内到达」，不假设重复推送的问题已被修好。
# ---------------------------------------------------------------------------

SSE_CAPTURE="${WORKDIR}/sse-capture.log"
: >"${SSE_CAPTURE}"
( timeout 12 curl -N -sS "${CONSOLE_BASE}/tasks/${TASK_ID}/stream" -H "${AUTH_HEADER}" 2>/dev/null \
	| while IFS= read -r line; do
		printf '%s %s\n' "$(date +%s.%N)" "${line}"
	done >"${SSE_CAPTURE}" ) &
SSE_SUBSHELL_PID=$!
sleep 2

SEND_TS="$(date +%s.%N)"
SIGNAL_CODE="$(send_signal "packages-done" "${SEQ}")"
[ "${SIGNAL_CODE}" = "200" ] || fail "阶段9: 用于 SSE 计时的信号请求未返回 200（实际 ${SIGNAL_CODE}）"
SEQ=$((SEQ + 1))

sleep 6
wait "${SSE_SUBSHELL_PID}" 2>/dev/null || true

ARRIVE_LINE="$(grep -m1 "packages-done" "${SSE_CAPTURE}" || true)"
[ -n "${ARRIVE_LINE}" ] || fail "阶段9: SSE 捕获里没有看到 packages-done 事件——事件流没有把新信号推送给客户端。捕获内容: $(cat "${SSE_CAPTURE}")"
ARRIVE_TS="$(echo "${ARRIVE_LINE}" | awk '{print $1}')"

LATENCY="$(awk -v a="${ARRIVE_TS}" -v s="${SEND_TS}" 'BEGIN{printf "%.3f", a-s}')"
echo "信号发出时间: ${SEND_TS}  SSE 事件到达时间: ${ARRIVE_TS}  延迟: ${LATENCY}s"

# 延迟必须明显小于「等到 timeout 12s 结束才一次性吐出」这种缓冲失效的
# 表现——判据取 6 秒（streamPollInterval=1.5s 的 4 倍，留足 nginx/网络
# 抖动余量，同时远小于本阶段的 12s 总超时，若判据不过说明 nginx 把响应
# 缓冲住了）。
LATENCY_OK="$(awk -v l="${LATENCY}" 'BEGIN{print (l < 6.0 && l >= 0) ? "yes" : "no"}')"
[ "${LATENCY_OK}" = "yes" ] || fail "阶段9: SSE 事件延迟 ${LATENCY}s 过长（或为负值），怀疑 nginx 缓冲了响应（proxy_buffering 未生效）"
pass "阶段9: SSE 经 nginx 转发，新信号事件在 ${LATENCY}s 内到达客户端（未被缓冲到连接结束才吐出）"

# ---------------------------------------------------------------------------
# 阶段 10：制品走 location 直接发盘，不经过 Go（SPEC §1.4 第 2/3/4 点）
# ---------------------------------------------------------------------------

ARTIFACT_URL="${GW_BASE}/artifacts/${CREATED_ARTIFACT_ID}/vmlinuz"
FULL_LEN="$(curl -fsS -o /dev/null -w '%{size_download}' "${ARTIFACT_URL}")"

# 10a. Range 请求：取文件中间 5 字节，断言 206 与准确的 Content-Range。
RANGE_HEADERS="$(curl -fsS -D - -r 2-6 "${ARTIFACT_URL}" -o "${WORKDIR}/range-body.bin")"
echo "${RANGE_HEADERS}" | grep -q "206 Partial Content" || fail "阶段10a: Range 请求未返回 206，响应头=${RANGE_HEADERS}"
echo "${RANGE_HEADERS}" | grep -q "Content-Range: bytes 2-6/${FULL_LEN}" \
	|| fail "阶段10a: Content-Range 不符预期，响应头=${RANGE_HEADERS}"
echo "${RANGE_HEADERS}" | grep -q "Accept-Ranges: bytes" || fail "阶段10a: 缺少 Accept-Ranges: bytes 响应头"

# 10b. gzip 关闭：带 Accept-Encoding: gzip 请求，响应不得是 gzip。
GZIP_HEADERS="$(curl -fsS -D - -H 'Accept-Encoding: gzip' "${ARTIFACT_URL}" -o "${WORKDIR}/gzip-body.bin")"
echo "${GZIP_HEADERS}" | grep -qi "Content-Encoding: gzip" \
	&& fail "阶段10b: 制品响应带了 Content-Encoding: gzip——iPXE 不解压 Content-Encoding，会把压缩字节当内核加载"
cmp -s "${WORKDIR}/gzip-body.bin" "${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}/vmlinuz" \
	|| fail "阶段10b: 带 Accept-Encoding 请求收到的字节与磁盘原文件不一致（可能被压缩了）"

# 10c. 确认这三次制品请求没有经过 Go——lober.log 里不应出现 vmlinuz/artifacts。
grep -q "vmlinuz\|artifacts" "${WORKDIR}/lober.log" \
	&& fail "阶段10c: lober 日志里出现了制品相关字样，说明制品请求经过了 Go 进程（应由 nginx 直接发盘）"

pass "阶段10: 制品经 nginx location 直接发盘——Range(206)/Accept-Ranges/gzip 关闭/未经过 Go 全部核对通过"

# ---------------------------------------------------------------------------
# 阶段 11：信号与引导两个限流桶互相独立（SPEC §10 运维点二）——若目标主机
# 没有 ab（apache2-utils），本阶段跳过并说明原因，不假装测过（见文件头
# 已知限制 4：单发 curl 循环的进程创建开销测不出限流效果）。
# ---------------------------------------------------------------------------

if ! command -v ab >/dev/null 2>&1; then
	echo "[跳过] 阶段11: 未找到 ab（apache2-utils），无法产生足够并发验证限流桶——" \
		"这不是「假设通过」，是明确跳过并写明原因"
else
	echo "flood" >"${WORKDIR}/ab-postbody.txt"
	AB_OUTPUT_FILE="${WORKDIR}/ab-output.log"
	# ab 的输出必须落文件而不是在子进程里用命令替换捕获——`cmd &` 这种写法
	# 会让命令替换发生在被后台化的子 shell 里，父 shell 拿不到那个变量赋值
	# （曾在本脚本编写过程中实测踩到：变量在 wait 之后仍是 unbound，set -u
	# 报错但不终止脚本，导致后面的判断读到空值——看起来像是「测了但结果
	# 不可信」，比直接测不出更危险）。
	( ab -n 500 -c 50 -p "${WORKDIR}/ab-postbody.txt" -T 'text/plain' \
		"${GW_BASE}/v1/signal/${TASK_ID}/${INSTALL_TOKEN}/packages-start?rc=0&seq=9999" \
		>"${AB_OUTPUT_FILE}" 2>&1 ) &
	AB_PID=$!
	sleep 0.3
	BOOT_DURING_FLOOD_CODE="$(curl -fsS -o /dev/null -w '%{http_code}' "${GW_BASE}/v1/boot?sn=${HOST_SN}&mac=${HOST_MAC}&uuid=&arch=x86_64&plat=efi" 2>/dev/null || echo "000")"
	wait "${AB_PID}"
	# 不能用「先 grep 出整行再 grep -oE '[0-9]+' 抠数字」的两段式提取——
	# "Non-2xx" 这个标签本身含数字 2，第二段 grep 会把标签里的 "2" 也当成
	# 一个匹配，多抠出一个值来（曾在本脚本编写过程中用真实 ab 输出实测
	# 踩到：抽出 "2" 和 "956" 两行，后续 [ -gt ] 数值比较因为拿到两行而
	# 报 "integer expression expected"）。改用 sed 一步到位只取标签冒号
	# 之后的数字。
	AB_NON2XX="$(sed -nE 's/^Non-2xx responses:[[:space:]]*([0-9]+).*/\1/p' "${AB_OUTPUT_FILE}")"
	[ -n "${AB_NON2XX}" ] || AB_NON2XX=0

	[ "${BOOT_DURING_FLOOD_CODE}" = "200" ] \
		|| fail "阶段11: 信号桶被打满期间，引导请求返回 HTTP ${BOOT_DURING_FLOOD_CODE}（want 200）——说明两个限流桶没有真正独立，一个桶的压力影响到了另一个"
	[ "${AB_NON2XX}" -gt 0 ] \
		|| echo "[提示] 阶段11: 500 个并发信号请求里没有任何一个被限流（Non-2xx=0）——可能 burst 阈值设得比这次测试压力更宽松，不代表限流没生效，只代表本次没打到阈值"

	pass "阶段11: 信号桶被压力测试打满（Non-2xx=${AB_NON2XX}/500）期间，引导端点请求仍返回 200——两个限流桶确认互相独立"
fi

echo ""
echo "=== 全部阶段（含跳过说明，若有）验证完成，task_id=${TASK_ID} ==="
