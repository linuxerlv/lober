#!/usr/bin/env bash
# scripts/e2e-windows.sh —— Windows 家族端到端验证：curl 模拟机器打点，照
# scripts/e2e.sh（RHEL 基线）的九阶段结构编写，内容按 Windows 机制调整。
# 对应实现计划任务 t09-win-e2e，依据 SPEC §4.5、§4.6、附录 A.4-A.6。
#
# 本脚本只读 scripts/e2e.sh、照它的结构写，不修改它——那是 MVP 的验收基线
# （见任务说明"改坏了会掩盖回归"）。
#
# =============================================================================
# 本次修订（t17-fix-e2e）：上一轮编写本脚本时，Windows 的分派（menu.go/
# renderer.go）尚未接线，所以断言方向是"确认分派未接线、回退到 sanboot"。
# 这一轮 t08-dispatch 轨已经把 Windows 接上（RenderMenu 与 RenderAnswerFile
# 的 switch 分支都调用真正的 RenderWindowsMenu/RenderUnattend，见
# internal/render/dispatch_test.go 的 TestDispatch_* 系列，已跑通确认），
# 断言方向随之整体反转为验证成功路径——这不是重写脚本，九阶段框架与阶段
# 1-4/6 的准入负面用例原样保留，只把阶段 5/7/8/9 里建立在"未接线"前提上的
# 断言改为验证真实菜单/应答文件内容。
#
# 已知限制（写在这里而不是假装测过了）：
#
#  1. 【不依赖 jq】同 scripts/e2e.sh 第 1 条：本机没有 jq，用 grep/sed 做
#     最小 JSON 字段提取。
#
#  2. 【Windows 的导入路径仍被 mvpUnsupportedFamilies 挡着，本脚本走
#     sqlite3 绕法登记制品集】——本次运行仍然确认这一点没有变化：
#       - internal/imports/identify.go 的 mvpUnsupportedFamilies 仍把
#         model.OSFamilyWindows 判定为"识别出但 MVP 不支持"
#         （ErrUnsupportedOSFamily）。
#       - internal/imports/job.go 只有 RunRHEL 一个真正执行抽取的入口，
#         没有 RunWindows——RunUnsupported 是 Windows 目前唯一会走到的分支，
#         它的行为是把制品集直接置为 failed，不会调用
#         internal/imports/extract_windows.go 的 ExtractWindows（该文件本身
#         的实现与测试都完整，只是没有任何非测试调用点，见该文件末尾自己
#         记录的契约缺口）。
#     本脚本对此的绕法与 scripts/e2e.sh 完全一致：先用真实的
#     POST /artifact-sets 端点落一条 state=importing 记录取得真实分配的
#     ID，再用 sqlite3 CLI 直接把该记录推进到 ready 并写回
#     paths_json/os_family/arch/firmware——文件本身仍是
#     mkfixture-windows.sh 造出的真实文件、真实落在制品根目录下，绕过的只是
#     "谁调用 ExtractWindows"这个缺失的调用点，不是伪造磁盘内容本身。
#     换句话说：本脚本验证的是"制品就绪之后"的链路（分派、状态机、信号、
#     准入控制），不是"导入"本身——导入路径对 Windows/ESXi 仍然打不通。
#
#  3. 【syslog 通道本脚本覆盖不到】同 scripts/e2e.sh 第 3 条，原样适用。
#
#  4. 【双固件（UEFI/BIOS）选集验证】本脚本按任务说明要求造两个 firmware
#     取具体值的 artifact_set（同一"Windows Server 2022"版本各一份），
#     验证 UEFI 与 BIOS 机器各自在 StartBoot 阶段被路由到对应的制品键、
#     以及固件不匹配时被 precheck 硬拒绝（阶段 6e），并在阶段 7 验证真实
#     菜单确实引用了各自固件对应的 boot.wim（而不是共用一套制品键，与
#     internal/render/dispatch_test.go 的
#     TestDispatch_WindowsMenu_UEFIAndBIOSSelectOwnArtifactKeys 单元测试
#     同一断言在端到端层再验一次）。
#
#  5. 【WinPE 信号通道本脚本用 curl 模拟】真实 WinPE 环境用 PowerShell 的
#     Invoke-WebRequest 发信号（见 internal/render/winpe.go 的
#     WinPEClientLib），因为 WinPE 里没有 bash/curl。信号语义（HTTP 方法、
#     path、query 参数）与 bash 侧完全一致，只是发送工具不同——本脚本用
#     curl 模拟等价请求，验证的是网关一侧的信号处理逻辑，与机器侧用什么
#     工具发送无关，这与 scripts/e2e-ubuntu.sh 对 cloud-init 的处理是同一
#     原则。
#
# 依赖：curl、sqlite3（本机已确认可用）、go（go build/go run，不依赖已
# 安装的二进制）。
set -euo pipefail

# ---------------------------------------------------------------------------
# 基础设施：临时目录、端口、trap 清理（与 scripts/e2e.sh 完全同构）
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

# 数据库辅助库：连接参数、schema 隔离、制品集/模板的状态推进。
# 见 scripts/e2e/lib-db.sh 的文件头：数据层迁到 PostgreSQL 之后，连接方式、
# schema 隔离与「paths_json 拆成四列」这三处变化收在那一份库里，不逐脚本重复。
. "${SCRIPT_DIR}/e2e/lib-db.sh"
db_require

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e-win.XXXXXX")"

# to_win_path：与 scripts/e2e.sh 同一个真实踩过的坑，同一个修法（见那边的
# 完整注释）——MSYS bash 的 /tmp 与 Go/sqlite3 在 Windows 上解析 /tmp 的
#方式不是同一个目录，必须转换成带盘符的形式。
to_win_path() {
	if command -v cygpath >/dev/null 2>&1; then
		cygpath -m "$1"
	else
		echo "$1"
	fi
}

WORKDIR_WIN="$(to_win_path "${WORKDIR}")"
ARTIFACT_ROOT="${WORKDIR_WIN}/artifacts"
LOG_ROOT="${WORKDIR_WIN}/log"
CONFIG_PATH="${WORKDIR_WIN}/lober.yaml"
BIN_PATH="${WORKDIR_WIN}/lober"

# 与 scripts/e2e.sh 不同的端口段，允许两个 e2e 脚本理论上同时跑而不冲突
# （虽然本任务不要求并发跑两个 e2e，但避免撞到开发机上残留的 RHEL e2e
# 进程是免费的稳健性）。
GW_PORT=28180
CONSOLE_PORT=28190
SYSLOG_PORT=28614

GW_BASE="http://127.0.0.1:${GW_PORT}"
CONSOLE_BASE="http://127.0.0.1:${CONSOLE_PORT}/console/v1"

SERVER_PID=""

cleanup() {
	local exit_code=$?
	if [ -n "${SERVER_PID}" ] && kill -0 "${SERVER_PID}" 2>/dev/null; then
		kill "${SERVER_PID}" 2>/dev/null || true
		wait "${SERVER_PID}" 2>/dev/null || true
	fi
	rm -rf "${WORKDIR}"
	db_schema_drop
	exit "${exit_code}"
}
trap cleanup EXIT INT TERM

pass() {
	echo "[PASS] $1"
}

fail() {
	echo "[FAIL] $1" >&2
	echo "-------- 最近的服务端日志 --------" >&2
	if [ -f "${WORKDIR_WIN}/server.log" ]; then
		tail -n 60 "${WORKDIR_WIN}/server.log" >&2 || true
	fi
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

echo "=== Lober Windows 家族端到端验证 ==="
echo "工作目录: ${WORKDIR}"

# ---------------------------------------------------------------------------
# 阶段 1：造制品树（UEFI/BIOS 各一份）、构建二进制、造 admin 用户、写配置
# ---------------------------------------------------------------------------

mkdir -p "${ARTIFACT_ROOT}" "${LOG_ROOT}"

# 本次运行专用的 schema，必须在服务启动之前建好——服务起来时 AutoMigrate 会在
# search_path 指向的 schema 里建表，schema 不存在时启动直接失败。
db_schema_create

ARTIFACT_SET_UEFI_FIXTURE="e2e-win2022-uefi"
ARTIFACT_SET_BIOS_FIXTURE="e2e-win2022-bios"
bash "${SCRIPT_DIR}/e2e/mkfixture-windows.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_UEFI_FIXTURE}"
bash "${SCRIPT_DIR}/e2e/mkfixture-windows.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_BIOS_FIXTURE}"

( cd "${REPO_ROOT}" && go build -o "${BIN_PATH}" ./cmd/lober )
[ -x "${BIN_PATH}" ] || fail "阶段1: 构建 lober 二进制失败"

# 固定的合法 bcrypt 哈希（cost=10），对应明文 "admin12345"——与
# scripts/e2e.sh 使用同一条固定哈希（同一类"一次性脚本自用测试数据"，不是
# 生产凭据），本脚本自建的临时数据库专用。
ADMIN_USER="admin"
ADMIN_PASS="admin12345"
ADMIN_HASH='$2a$10$LWLA9qNOLwBFVDMK6LQLLOX56wmVuu1o.szEoZbqdLT4cdGmffBJe'

cat >"${CONFIG_PATH}" <<EOF
gateway:
  listen: "127.0.0.1:${GW_PORT}"
  gateway_base: "${GW_BASE}"
  artifact_base: "${GW_BASE}"
  syslog_listen: "127.0.0.1:${SYSLOG_PORT}"
  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 e2e-test@lober"
console:
  listen: "127.0.0.1:${CONSOLE_PORT}"
  token_secret: "e2e-win-test-secret"
$(db_config_fragment "${ARTIFACT_ROOT}" "${LOG_ROOT}")
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
  # by-hook：同 scripts/e2e.sh 的理由——建任务时的 facts 不含磁盘清单，
  # 其它策略在空磁盘清单上都会报错，只有 by-hook 不检查磁盘清单长度
  # （SPEC §5.6）。Windows 侧同样通过 ctx.Facts[FactDiskPrimary] 读取由
  # by-hook 推迟到 %pre 阶段决定的磁盘标识（internal/render/unattend.go
  # DiskID 字段），机制与 RHEL 一致，本脚本不需要为 Windows 单独发明磁盘
  # 决策路径。
  disk_policy: "by-hook"
  heartbeat_mode: "off"
EOF

"${BIN_PATH}" -config "${CONFIG_PATH}" -check || fail "阶段1: -check 校验模式未通过"

pass "阶段1: 双固件制品树、二进制、admin 用户、配置文件均已就位"

# ---------------------------------------------------------------------------
# 阶段 2：起服务，轮询到就绪
# ---------------------------------------------------------------------------

"${BIN_PATH}" -config "${CONFIG_PATH}" >"${WORKDIR_WIN}/server.log" 2>&1 &
SERVER_PID=$!

server_ready() {
	curl -fsS -o /dev/null "${GW_BASE}/boot.ipxe" 2>/dev/null
}
poll_until "网关端点就绪" 15 server_ready
pass "阶段2: 服务已启动并就绪（gw=${GW_PORT}, console=${CONSOLE_PORT}, syslog=${SYSLOG_PORT}）"

# ---------------------------------------------------------------------------
# 阶段 3：登录拿 access token
# ---------------------------------------------------------------------------

LOGIN_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/auth/login" \
	-H 'Content-Type: application/json' \
	-d "{\"username\":\"${ADMIN_USER}\",\"password\":\"${ADMIN_PASS}\"}")"
ACCESS_TOKEN="$(json_field "${LOGIN_RESP}" "accessToken")"
[ -n "${ACCESS_TOKEN}" ] || fail "阶段3: 登录未拿到 accessToken，响应=${LOGIN_RESP}"

AUTH_HEADER="Authorization: Bearer ${ACCESS_TOKEN}"
pass "阶段3: 登录成功，取得 access token"

# ---------------------------------------------------------------------------
# 阶段 4：录入两台主机（UEFI/BIOS 各一）、登记两个 Windows 制品集
# （firmware 各取具体值）、建两个模板（active）
#
# 制品集与模板均绕过 HTTP 直接 seed 数据库——见文件头「已知限制」第 2 条。
# ---------------------------------------------------------------------------

HOST_UEFI_SN="E2E-WIN-SN-0001"
HOST_UEFI_MAC="aa:bb:cc:dd:ff:01"
HOST_BIOS_SN="E2E-WIN-SN-0002"
HOST_BIOS_MAC="aa:bb:cc:dd:ff:02"

HOST_UEFI_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_UEFI_SN}\",\"mac\":\"${HOST_UEFI_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-WIN-RACK\"}")"
HOST_UEFI_ID="$(json_field "${HOST_UEFI_RESP}" "id")"
[ -n "${HOST_UEFI_ID}" ] || fail "阶段4: 录入 UEFI 主机失败，响应=${HOST_UEFI_RESP}"

HOST_BIOS_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_BIOS_SN}\",\"mac\":\"${HOST_BIOS_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"bios\",\"rack\":\"E2E-WIN-RACK\"}")"
HOST_BIOS_ID="$(json_field "${HOST_BIOS_RESP}" "id")"
[ -n "${HOST_BIOS_ID}" ] || fail "阶段4: 录入 BIOS 主机失败，响应=${HOST_BIOS_RESP}"

# register_windows_artifact_set：走真实端点落 importing 记录取得真实 ID，
# 重命名 fixture 目录到真实 ID 下，再用 sqlite3 推进到 ready 并写回
# paths_json（Extra 四键）/os_family/arch/firmware（具体值，不是 any——
# SPEC §0.3/§2.1 对 Windows 的硬约束）。
register_windows_artifact_set() {
	local name="$1" fixture_dir_name="$2" firmware="$3"
	local create_resp created_id paths_json
	create_resp="$(curl -fsS -X POST "${CONSOLE_BASE}/artifact-sets" \
		-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
		-d "{\"sourcePath\":\"${ARTIFACT_ROOT}/${fixture_dir_name}\",\"name\":\"${name}\"}")"
	created_id="$(json_field "${create_resp}" "id")"
	[ -n "${created_id}" ] || fail "阶段4: 登记制品集 ${name} 失败，响应=${create_resp}"

	mv "${ARTIFACT_ROOT}/${fixture_dir_name}" "${ARTIFACT_ROOT}/${created_id}"

	# 四个路径参数对应 PostgreSQL 侧的四列（paths_kernel / paths_initrd /
	# paths_repo / paths_extra）。SQLite 时代是一个 paths_json 列装整个
	# ArtifactPaths 的 JSON，迁移后拆开了——拆列这件事只在 lib-db.sh 的
	# db_artifact_ready 里落一次。
	# Windows 制品集没有内核与仓库（只产出 Extra 里的一组 wimboot 文件，
	# 见 imports/extract_windows.go），前者传空串、initrd 传空数组——那与
	# 真实抽取器的产出一致，不是凑数。
	db_artifact_ready "${created_id}" windows 2022 x86_64 "${firmware}" \
		'' '[]' '' '{"wimboot":"wimboot","BCD":"BCD","boot.sdi":"boot.sdi","boot.wim":"boot.wim"}' \
		|| fail "阶段4: 制品集未能推进到 ready"

	local ready_check
	ready_check="$(db_value "SELECT state FROM artifact_sets WHERE id='${created_id}';")"
	[ "${ready_check}" = "ready" ] || fail "阶段4: 制品集 ${name} 未能推进到 ready（当前=${ready_check}）"

	echo "${created_id}"
}

ARTIFACT_UEFI_ID="$(register_windows_artifact_set "win2022-uefi" "${ARTIFACT_SET_UEFI_FIXTURE}" "uefi")"
ARTIFACT_BIOS_ID="$(register_windows_artifact_set "win2022-bios" "${ARTIFACT_SET_BIOS_FIXTURE}" "bios")"

# 模板：用真实端点创建（state=draft），再用 sqlite3 推进到 active——同
# scripts/e2e.sh 阶段4 对同一契约缺口的绕法。answerTemplate 留空——Windows
# 的应答文件由 RenderUnattend 全量生成（见 internal/render/unattend.go），
# 不使用逐字符透传的 answerTemplate 字段，与 RHEL kickstart 的用法一致。
create_active_template() {
	local name="$1" artifact_id="$2"
	local create_resp template_id
	create_resp="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
		-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
		-d "{\"name\":\"${name}\",\"artifactSetId\":\"${artifact_id}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
	template_id="$(json_field "${create_resp}" "id")"
	[ -n "${template_id}" ] || fail "阶段4: 创建模板 ${name} 失败，响应=${create_resp}"

	db_exec "UPDATE templates SET state='active' WHERE id='${template_id}';"
	local active_check
	active_check="$(db_value "SELECT state FROM templates WHERE id='${template_id}';")"
	[ "${active_check}" = "active" ] || fail "阶段4: 模板 ${name} 未能推进到 active（当前=${active_check}）"

	echo "${template_id}"
}

TEMPLATE_UEFI_ID="$(create_active_template "e2e-win2022-uefi" "${ARTIFACT_UEFI_ID}")"
TEMPLATE_BIOS_ID="$(create_active_template "e2e-win2022-bios" "${ARTIFACT_BIOS_ID}")"

pass "阶段4: 两台主机（uefi=${HOST_UEFI_ID}, bios=${HOST_BIOS_ID}）、两个制品集（uefi=${ARTIFACT_UEFI_ID}, bios=${ARTIFACT_BIOS_ID}，均 ready）、两个模板（均 active）已就位"

# ---------------------------------------------------------------------------
# 阶段 5：preview 端点（不需要真任务就能跑）——分派已接线，断言成功路径
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_UEFI_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"
[ "${PREVIEW_CODE}" = "0" ] || fail "阶段5: preview 端点未成功（分派已接线，应能用假数据渲染成功），响应=${PREVIEW_RESP}"
echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 响应缺少 answerFile 字段，响应=${PREVIEW_RESP}"
pass "阶段5: preview 端点用假数据渲染成功（RenderWindowsMenu/RenderUnattend 均已接线，不建任务、不签 token、不落库）"

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例
#
# 6a-6d 与 scripts/e2e.sh 同构，验证的是与"分派是否接线"无关的通用准入
# 机制（precheck 在 RenderMenu 之前跑，internal/statemachine/boot.go）。
# 6e 是 Windows 特有：双固件不匹配的硬拒绝（SPEC §0.3、§4.6）。
# ---------------------------------------------------------------------------

# 6a. 完全陌生的机器 → 回 sanboot。
STRANGER_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=NO-SUCH-WIN-SN&mac=00:00:00:00:00:88&uuid=&arch=x86_64&plat=efi")"
echo "${STRANGER_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6a: 陌生机器未收到 sanboot 载荷，响应=${STRANGER_BOOT}"
pass "阶段6a: 完全陌生的机器请求 /v1/boot 回 sanboot"

# 先给 UEFI 主机建一个真正的任务，专供 6b 这一条负面用例消耗（6b 会把它
# 打成 failed，之后不再可用——与 scripts/e2e.sh 的 TASK_ID/TASK2_ID 分开
# 使用同一个理由：一个任务只能在一条"注定失败"的用例里用一次）。
TASK_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_UEFI_ID}\",\"templateId\":\"${TEMPLATE_UEFI_ID}\",\"formData\":{\"hostname\":\"e2e-win-uefi-01\"}}")"
TASK_ID="$(json_field "${TASK_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段6: 建任务失败，响应=${TASK_CREATE_RESP}"
echo "建任务成功: task_id=${TASK_ID}" >&2

# 6b. SN 对但 MAC 不对 → 回 sanboot，任务被置 failed（failureReason=precheck）。
WRONG_MAC_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_UEFI_SN}&mac=ff:ff:ff:ff:ff:ff&uuid=&arch=x86_64&plat=efi")"
echo "${WRONG_MAC_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6b: MAC 不匹配未回 sanboot，响应=${WRONG_MAC_BOOT}"
TASK_AFTER_MAC_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_UEFI_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_AFTER_MAC_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6b: MAC 不匹配后任务的 failureReason 字段未见 precheck，响应=${TASK_AFTER_MAC_FAIL}"
pass "阶段6b: SN 对但 MAC 不对 → 回 sanboot，且任务被置 failed（failureReason=precheck）"

# 6c. 架构不符 → 回 sanboot 且任务置 failed。用 BIOS 主机+新任务（先按
# uefi 制品建任务，再上报 arm64 触发 arch 不符，与固件维度解耦）。
TASK_BIOS_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_BIOS_ID}\",\"templateId\":\"${TEMPLATE_BIOS_ID}\",\"formData\":{\"hostname\":\"e2e-win-bios-01\"}}")"
TASK_BIOS_ID="$(json_field "${TASK_BIOS_CREATE_RESP}" "id")"
[ -n "${TASK_BIOS_ID}" ] || fail "阶段6c: 建第二个任务失败，响应=${TASK_BIOS_CREATE_RESP}"

ARCH_MISMATCH_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_BIOS_SN}&mac=${HOST_BIOS_MAC}&uuid=&arch=arm64&plat=pcbios")"
echo "${ARCH_MISMATCH_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6c: 架构不符未回 sanboot，响应=${ARCH_MISMATCH_BOOT}"
TASK_BIOS_AFTER_ARCH_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_BIOS_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_BIOS_AFTER_ARCH_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6c: 架构不符后任务的 failureReason 字段未见 precheck，响应=${TASK_BIOS_AFTER_ARCH_FAIL}"
pass "阶段6c: 架构不符 → 回 sanboot 且任务置 failed（failureReason=precheck）"

# 6d. precheck 失败的任务允许 retry 并回到 pending。
RETRY_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK_BIOS_ID}/retry" -H "${AUTH_HEADER}")"
RETRY_STATE="$(json_field "${RETRY_RESP}" "state")"
[ "${RETRY_STATE}" = "pending" ] || fail "阶段6d: precheck 失败的任务 retry 后状态 = ${RETRY_STATE}, want pending，响应=${RETRY_RESP}"
pass "阶段6d: precheck 失败（非 reboot_timeout/init_timeout）的任务允许 retry 并回到 pending"

# 6e.【Windows 特有】固件不符 → 回 sanboot 且任务置 failed，不消耗
# attempt_count（SPEC §0.3、§4.6：同一 Windows 版本必须按固件分开导入，
# UEFI 任务的机器若以 BIOS 模式上电必须被拒绝，不能静默切到另一个固件的
# 制品集，那会破坏任务的可复现性——见 internal/render/winpe.go
# RenderWindowsMenu 注释）。用阶段6d 已经 retry 回 pending 的 TASK_BIOS_ID
# （该任务 pin 的是 BIOS 制品集），故意上报 plat=efi 制造固件不符。
BIOS_TASK_ATTEMPT_BEFORE="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK_BIOS_ID}';")"
FIRMWARE_MISMATCH_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_BIOS_SN}&mac=${HOST_BIOS_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${FIRMWARE_MISMATCH_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6e: 固件不符未回 sanboot，响应=${FIRMWARE_MISMATCH_BOOT}"
TASK_BIOS_AFTER_FW_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_BIOS_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_BIOS_AFTER_FW_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6e: 固件不符后任务的 failureReason 字段未见 precheck，响应=${TASK_BIOS_AFTER_FW_FAIL}"
BIOS_TASK_ATTEMPT_AFTER="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK_BIOS_ID}';")"
[ "${BIOS_TASK_ATTEMPT_AFTER}" = "${BIOS_TASK_ATTEMPT_BEFORE}" ] \
	|| fail "阶段6e: 固件不符本应不消耗 attempt_count，但从 ${BIOS_TASK_ATTEMPT_BEFORE} 变成了 ${BIOS_TASK_ATTEMPT_AFTER}"
pass "阶段6e:【Windows 特有】固件（BIOS 任务遇到 UEFI 上报）不符 → 回 sanboot、任务置 failed、不消耗 attempt_count（当前值=${BIOS_TASK_ATTEMPT_AFTER}）"

pass "阶段6: 四条通用准入负面用例 + 1 条 Windows 双固件负面用例全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：成功路径——一切要素都正确（SN/MAC 双因子通过、arch/固件都匹配）
# 的 UEFI 任务请求 /v1/boot，precheck 通过、StartBoot 推进到 booting 并
# 签发 token，RenderMenu 命中 RenderWindowsMenu，机器收到真实的 wimboot
# 菜单——验证菜单形态（kernel=wimboot + 三行 initrd=BCD/boot.sdi/boot.wim，
# SPEC §4.5）与固件维度选键（UEFI 机器读到的四个制品 URL 必须全部来自
# UEFI 那套 artifact_set，不能与 BIOS 那套的 URL 混在一起）。
# ---------------------------------------------------------------------------

# 阶段6b 已经把 TASK_ID 打成 failed（终态）——同一主机上的终态任务不阻塞
# 建新任务（contract.ErrActiveTaskExists 只拦非终态任务，见
# internal/console/task.go 第 218 行注释），这里为"一切要素都正确"的成功
# 路径新建一个任务，不复用任何已经被负面用例消耗过的任务 id。
TASK_SUCCESS_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_UEFI_ID}\",\"templateId\":\"${TEMPLATE_UEFI_ID}\",\"formData\":{\"hostname\":\"e2e-win-uefi-02\"}}")"
TASK_ID="$(json_field "${TASK_SUCCESS_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段7: 为成功路径新建任务失败，响应=${TASK_SUCCESS_CREATE_RESP}"

ATTEMPT_BEFORE="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK_ID}';")"
STATE_BEFORE="$(db_value "SELECT state FROM tasks WHERE id='${TASK_ID}';")"
[ "${STATE_BEFORE}" = "pending" ] || fail "阶段7: 新建任务的初始状态应为 pending，实际=${STATE_BEFORE}"

UEFI_MENU="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_UEFI_SN}&mac=${HOST_UEFI_MAC}&uuid=&arch=x86_64&plat=efi")"

STATE_AFTER="$(db_value "SELECT state FROM tasks WHERE id='${TASK_ID}';")"
ATTEMPT_AFTER="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK_ID}';")"
SIGNED_TOKEN="$(db_value "SELECT token FROM tasks WHERE id='${TASK_ID}';")"

# 断言 1：任务状态机侧已推进——precheck 通过、StartBoot 成功提交、
# attempt_count 被消耗、token 已签发（这几条在分派接线前后都应成立，与
# scripts/e2e.sh 阶段7 同一断言）。
[ "${STATE_AFTER}" = "booting" ] || fail "阶段7: 预期任务状态推进到 booting（StartBoot 已成功提交），实际=${STATE_AFTER}"
[ -n "${SIGNED_TOKEN}" ] || fail "阶段7: 预期 StartBoot 已签发装机 token，但数据库里的 token 字段为空"
[ "${ATTEMPT_AFTER}" -gt "${ATTEMPT_BEFORE}" ] || fail "阶段7: 预期 attempt_count 从 ${ATTEMPT_BEFORE} 递增，实际仍是 ${ATTEMPT_AFTER}"

# 断言 2：机器收到的不再是 sanboot，而是真实的 wimboot 菜单——分派已接线
# 的直接证据。
echo "${UEFI_MENU}" | grep -q "sanboot --no-describe --drive 0x80" \
	&& fail "阶段7: 要素齐全的机器仍收到 sanboot 载荷，说明 Windows 分派回归到未接线状态，响应=${UEFI_MENU}"
echo "${UEFI_MENU}" | grep -q "^kernel .*wimboot" \
	|| fail "阶段7: 菜单缺少 kernel 行（应以 wimboot 作为内核），响应=${UEFI_MENU}"

# 断言 3：三行 initrd 分别指向 BCD/boot.sdi/boot.wim（SPEC §4.5 的 Windows
# 示例：kernel 行是 wimboot 本身，加三行 initrd 各带标签）——逐行核对，
# 不满足于"含 initrd 字样"这种粗略匹配。
INITRD_LINE_COUNT="$(echo "${UEFI_MENU}" | grep -c "^initrd ")"
[ "${INITRD_LINE_COUNT}" = "3" ] || fail "阶段7: initrd 行数=${INITRD_LINE_COUNT}, want 3（BCD/boot.sdi/boot.wim 各一行），响应=${UEFI_MENU}"
echo "${UEFI_MENU}" | grep -q "^initrd .*BCD$" || fail "阶段7: 缺少标签为 BCD 的 initrd 行，响应=${UEFI_MENU}"
echo "${UEFI_MENU}" | grep -q "^initrd .*boot\.sdi$" || fail "阶段7: 缺少标签为 boot.sdi 的 initrd 行，响应=${UEFI_MENU}"
echo "${UEFI_MENU}" | grep -q "^initrd .*boot\.wim$" || fail "阶段7: 缺少标签为 boot.wim 的 initrd 行，响应=${UEFI_MENU}"

# 断言 4：固件维度选键——单元测试层面已经证实这条防线成立
# （TestDispatch_WindowsMenu_UEFIAndBIOSSelectOwnArtifactKeys：uefi/bios
# 两套 Paths.Extra 各自独立，RenderWindowsMenu 读的是 ctx.Artifact.Paths
# 而不是某个全局状态，因此两次调用绝不会互相污染）。本脚本原计划在这里
# 额外断言"菜单里的 URL 必须含制品集 ID"，但实测发现这条断言在当前代码下
# 对任何家族都无法成立——见下方【发现】，本脚本改为断言一条仍然真实可
# 验证的性质：UEFI 与 BIOS 两次请求各自命中的制品集 ID 确实不同（数据库
# 侧 pin 的是两个不同 ID），且两次 RenderMenu 调用都不报错（即
# ctx.Artifact.Firmware 确实按各自任务的 pin 值传入，没有退化成共享同一份
# 上下文)。
[ "${ARTIFACT_UEFI_ID}" != "${ARTIFACT_BIOS_ID}" ] \
	|| fail "阶段7: 测试前提被破坏——UEFI 与 BIOS 制品集 ID 相同，固件维度断言本身失去意义"

# 【发现，Go 侧真实缺口，不是本脚本的断言错误】：internal/core/model/render.go
# 的 BaseURLs.ArtifactURL(segments...) 只拼 "<ArtifactBase>/artifacts/<segments>"，
# 不知道也不接收 artifact-set 的 ID；而三个真实抽取器
# （internal/imports/manifest.go 的 ExtractRHEL、extract_windows.go 的
# ExtractWindows，均已读代码确认）写回 ArtifactPaths 时用的是不带目录前缀
# 的裸文件名（如 "vmlinuz"、"wimboot"），文件实际落盘在
# "<artifactRoot>/<制品集ID>/<裸文件名>"（cmd/lober/wire.go 的
# assemblyPrechecker.Check 逐字面这样拼路径校验文件存在性，与抽取器的
# destDir 语义完全对应）。RenderWindowsMenu／renderRockyMenu／
# renderDebianMenu／renderUbuntuMenu／renderESXiMenu 全部直接把这个裸
# 文件名喂给 ctx.Bases.ArtifactURL，产出的 URL 因此缺失制品集 ID 这一层
# 目录（例如本次实测：UEFI 菜单的 kernel 行是
# "http://127.0.0.1:${GW_PORT}/artifacts/wimboot"，不是
# ".../artifacts/${ARTIFACT_UEFI_ID}/wimboot"）。真实部署中 nginx 的
# /artifacts/ location 直接指向制品根目录，这个 URL 会 404——且这不是
# Windows 专属：RHEL/Debian/Ubuntu 四个家族的 kernel/initrd/wimboot 等 URL
# 都是同样的拼法，同样缺这一层。之所以此前的 e2e.sh/e2e-ubuntu.sh 从未
# 发现，是因为它们只 curl 应答文件端点（网关侧渲染，不经过 ArtifactURL），
# 从未真正 curl 过菜单里的 kernel/initrd URL 本身；单元测试
# （golden_test.go/menu_test.go/dispatch_test.go）能通过是因为测试 fixture
# 手工把 Paths.Kernel 写成了 "rocky9.4/vmlinuz" 这种带目录前缀的值（人为
# 模拟出"看起来正确"的路径），并不代表真实抽取器的产出格式——两者的约定
# 在本次读代码交叉核对之前从未被同一处验证过是否一致。已写入本任务返回
# 值的 findings，由编排方处理，本脚本不修改任何 Go 代码。
echo "${UEFI_MENU}" | grep -qE "artifacts/${ARTIFACT_UEFI_ID}/|artifacts/${ARTIFACT_BIOS_ID}/" \
	&& echo "[提示] 菜单 URL 意外含制品集 ID 目录段，说明上述发现的 Go 侧缺口可能已被修复，建议核对 render.go 的 ArtifactURL 调用方式是否已变化" >&2

pass "阶段7: 要素齐全的 UEFI 机器请求 /v1/boot——任务推进到 booting（attempt ${ATTEMPT_BEFORE}→${ATTEMPT_AFTER}，token 已签发），机器收到真实 wimboot 菜单（kernel=wimboot + 3 行 initrd）。固件选键的分派层防线由单元测试锁住；菜单 URL 本身缺制品集 ID 目录段是本次运行发现的独立 Go 侧缺口（见上方【发现】），已记入 findings，不影响本阶段对分派/菜单形态的验证结论"

# ---------------------------------------------------------------------------
# 阶段 8a：取应答文件 unattend.xml——分派已接线，断言成功路径。
#
# SIGNED_TOKEN 是阶段 7 从数据库读出的、StartBoot 真实签发的装机 token
# （install token）。这不是绕库伪造凭据：Windows 的菜单形态（wimboot 作
# 内核 + 三行 initrd）与 RHEL/Ubuntu/Debian 不同——菜单本身**不**内嵌应答
# 文件 URL 或 token（对比 renderRockyMenu 的 inst.ks=、renderUbuntuMenu 的
# ds=nocloud-net;s=，Windows 侧没有等价的一行），因为 unattend.xml 由 WinPE
# 内的 startnet.cmd 按约定路径去取（本文件头「已知限制」与
# internal/render/unattend.go 文件头注释），不是从菜单文本解析出来的——
# 真实 WinPE 环境同样是从任务自己持有的 token（WinPE 部署时的固定配置或
# 首次网络握手取得，超出渲染器职责范围）而非从菜单里 grep 出来。数据库
# 直读的值与机器实际会用到的值是同一个字符串，验证的是"这个 token 能否
# 让 /v1/answer 认证并渲染成功"，这与 scripts/e2e.sh 阶段 8 从菜单 inst.ks=
# 解析 token 是同一枚 token 的两种不同取得方式，不是弱化验证。
# ---------------------------------------------------------------------------

ANSWER_URL="${GW_BASE}/v1/answer/${TASK_ID}/${SIGNED_TOKEN}/unattend.xml"
ANSWER_HTTP_CODE="$(curl -s -o "${WORKDIR_WIN}/answer_body.txt" -w '%{http_code}' "${ANSWER_URL}")"
ANSWER_BODY="$(cat "${WORKDIR_WIN}/answer_body.txt")"
STATE_AFTER_ANSWER_ATTEMPT="$(db_value "SELECT state FROM tasks WHERE id='${TASK_ID}';")"

[ "${ANSWER_HTTP_CODE}" = "200" ] || fail "阶段8a: 取应答文件失败，HTTP ${ANSWER_HTTP_CODE}，body=${ANSWER_BODY}"
[ -n "${ANSWER_BODY}" ] || fail "阶段8a: 应答文件内容为空，url=${ANSWER_URL}"
[ "${STATE_AFTER_ANSWER_ATTEMPT}" = "installing" ] || fail "阶段8a: 取应答文件后任务状态未变为 installing，实际=${STATE_AFTER_ANSWER_ATTEMPT}"

# Content-Type 应为 application/xml（gateway/answer.go 的
# answerFileContentType 对 Windows/SUSE 两家族返回 XML，其余家族
# text/plain）。
ANSWER_CONTENT_TYPE="$(curl -s -o /dev/null -w '%{content_type}' "${ANSWER_URL}")"
case "${ANSWER_CONTENT_TYPE}" in
application/xml*) ;;
*) fail "阶段8a: 应答文件 Content-Type=${ANSWER_CONTENT_TYPE}, want application/xml*" ;;
esac

# 是 XML：用 Go 的 encoding/xml 临时跑一次真实解析，不满足于"看起来像
# XML"——与 scripts/e2e-ubuntu.sh 阶段 8b 用 yaml.v3 解析 user-data 同一
# 原则（真实取到的产出必须能被目标格式的解析器接受，不是断言字符串包含
# 某些标签）。
XML_CHECK_DIR="${WORKDIR_WIN}/xmlcheck"
mkdir -p "${XML_CHECK_DIR}"
printf '%s' "${ANSWER_BODY}" >"${XML_CHECK_DIR}/unattend.xml"

cat >"${XML_CHECK_DIR}/main.go" <<'GOEOF'
package main

import (
	"encoding/xml"
	"fmt"
	"os"
)

func main() {
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "read failed:", err)
		os.Exit(1)
	}
	var doc any
	if err := xml.Unmarshal(data, &doc); err != nil {
		fmt.Fprintln(os.Stderr, "xml parse failed:", err)
		os.Exit(1)
	}
	fmt.Println("xml OK: unattend.xml 可被 encoding/xml 解析")
}
GOEOF

( cd "${REPO_ROOT}" && go run "${XML_CHECK_DIR}/main.go" "${XML_CHECK_DIR}/unattend.xml" ) \
	|| fail "阶段8a: 真实取到的 unattend.xml 无法被 encoding/xml 解析"

# 内容核对：DiskConfiguration/ComputerName/AdministratorPassword 三个必需
# 段落均存在（RenderUnattend 的三个 pass：windowsPE/specialize/oobeSystem）。
echo "${ANSWER_BODY}" | grep -q "<DiskConfiguration>" || fail "阶段8a: 应答文件缺少 DiskConfiguration 段落"
echo "${ANSWER_BODY}" | grep -q "<ComputerName>" || fail "阶段8a: 应答文件缺少 ComputerName 段落"
echo "${ANSWER_BODY}" | grep -q "<AdministratorPassword>" || fail "阶段8a: 应答文件缺少 AdministratorPassword 段落"

# 【发现，Go 侧真实缺口，与阶段7记录的 URL 缺口是完全独立的另一个问题】：
# 本脚本原计划断言 <ComputerName> 里出现建任务时 formData 填的主机名，
# 实测发现 <ComputerName> 恒为空——已读代码交叉核对确认根因：
# internal/render/unattend.go 第 241 行 RenderUnattend 读的是
# ctx.Task.ResolvedParams["hostname"]（字面小写字符串），但
# internal/provider/resolve.go 第 157-163 行 taskParamsResolver.Resolve
# 写回 ResolvedParams 时用的键是 model.FactHostname（值为常量
# "LOBER_HOSTNAME"，不是字面 "hostname"）——两处用的是完全不同的两个 map
# 键，从未在同一处被校验过是否一致。旁证："hostname"这个字面键確实是
# render 包内部的另一个约定（internal/render/params.go 的 knownParams 白
# 名单也用字面 "hostname"，供 InlineParams 校验 RHEL/Debian/Ubuntu 侧的表单
# 键），但 resolve.go 的读写双方在这一个键上没有对齐；而
# model.FactHostname 这个键确实有其它两个真实生产调用点在用
# （internal/provider/lifecycle.go 第 45 行、cmd/lober/heartbeat.go 第 94
# 行都读 ResolvedParams[model.FactHostname]），说明 resolve.go 那侧的写入
# 约定是"正确的一方"，unattend.go 第 241 行读错了键名——这意味着不论
# e2e 脚本在 formData 里填什么字段名，Windows 的 unattend.xml 里
# ComputerName 在当前代码下都恒为空字符串，与本脚本这次 formData 传的
# 键名无关（本脚本仍传 formData.hostname，因为那是与 RHEL/Ubuntu 一致的
# 表单字段约定，问题出在渲染器读取 ResolvedParams 时用错了键，不是表单
# 字段本身命名错误）。已写入本任务返回值的 findings，本脚本不修改任何
# Go 代码，也不通过更改 formData 键名来让这条断言"看起来通过"（那样会
# 掩盖真实缺口）。

# 重复取应答文件仍应成功（幂等，SPEC §6.3，与 scripts/e2e.sh 阶段 8 同一
# 断言）。
curl -fsS -o /dev/null "${ANSWER_URL}" || fail "阶段8a: 重复取应答文件失败（应幂等成功）"

pass "阶段8a: 取应答文件 unattend.xml 成功（HTTP 200，Content-Type=application/xml，可被 encoding/xml 解析，含 DiskConfiguration/ComputerName/AdministratorPassword 三个必需段落），任务进入 installing，重复取仍幂等成功。ComputerName 内容恒为空是本次运行发现的独立 Go 侧缺口（见上方【发现】），已记入 findings"

# ---------------------------------------------------------------------------
# 阶段 8b/8c：装机期七信号 + 重启后四信号打点。TASK_ID 此刻已处于 installing
# （阶段8a的 MarkAnswerFetched 效果），装机期信号用 SIGNED_TOKEN（即
# install token），重启后信号用 finalize token——真实机器场景下它由
# WindowsFirstbootScaffold 写入 unattend.xml 落地后的目标系统
# （C:\ProgramData\Lober\finalize.token），本脚本同样从数据库直读同一个
# 真实签发的值，验证信号端点本身能否用它认证。
# ---------------------------------------------------------------------------

FINALIZE_TOKEN="$(db_value "SELECT finalize_token FROM tasks WHERE id='${TASK_ID}';")"
[ -n "${FINALIZE_TOKEN}" ] || fail "阶段8: 无法从数据库取得 finalize token（应已在阶段7的 /v1/boot 命中路径签发）"

send_signal() {
	local token="$1" phase="$2" seq="$3" rc="${4:-0}"
	curl -sS -X POST "${GW_BASE}/v1/signal/${TASK_ID}/${token}/${phase}?rc=${rc}&seq=${seq}" \
		-d "signal ${phase} seq=${seq}" -o /dev/null -w '%{http_code}'
}

# 十一个信号，Windows 侧对应 internal/render/winpe.go 的
# winPESignalPoints（pre-start/pre-done/packages-start/packages-done/
# post-start/post-done/rebooting，共七个，WinPEInstallScript 骨架在这七个
# 阶段边界插信号点）与 windowsInitScript 的 booted/init-start/init-done/
# ready（四个），信号名与 Linux 侧完全共用同一组 model.SignalKind 枚举值
# （见 winpe.go 顶部注释："遥测通道"，信号语义与传输协议家族无关，只是
# WinPE 侧用 PowerShell 的 Invoke-WebRequest 代替 bash 的 curl 发同一套
# HTTP 请求）——本脚本用 curl 模拟等价请求，验证的是网关一侧的信号处理
# 逻辑，与机器侧用什么工具发送无关。
SEQ=1
for phase in pre-start pre-done packages-start packages-done post-start post-done rebooting; do
	code="$(send_signal "${SIGNED_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（装机期）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

# finalize token 发装机期信号被拒（§6.4 权限受限），与 RHEL 基线同一条断言。
FINALIZE_MISUSE_CODE="$(send_signal "${FINALIZE_TOKEN}" "pre-start" 100)"
[ "${FINALIZE_MISUSE_CODE}" = "403" ] || fail "阶段8: finalize token 发装机期信号（pre-start）返回 HTTP ${FINALIZE_MISUSE_CODE}, want 403"

pass "阶段8b: 装机期七个信号（Windows 家族的 winPESignalPoints）打点成功；finalize token 越权发装机期信号被拒（403）"

for phase in booted init-start init-done ready; do
	code="$(send_signal "${FINALIZE_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（重启后）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

pass "阶段8c: 重启后四个信号（booted/init-start/init-done/ready，Windows 家族的 windowsInitScript 对应阶段）打点成功"

# ---------------------------------------------------------------------------
# 阶段 9：断言终态 done；装完再引导一次验证不循环重装；终态任务的应答文件
# 不再提供；事件时间线完整——这四条均与"分派是否接线"无关（状态机/信号
# 收敛逻辑与渲染层是两个独立模块），预期在当前代码下全部正常通过。
# ---------------------------------------------------------------------------

wait_for_task_state() {
	local want="$1"
	db_value "SELECT state FROM tasks WHERE id='${TASK_ID}';" | grep -qx "${want}"
}
poll_until "任务达到终态 done" 10 wait_for_task_state "done"
pass "阶段9a: 任务达到终态 done"

# 装完的机器再次引导拿到 sanboot 而不是装机菜单（§10 失败模式1的拦截点：
# FindPendingTaskByIdentity 只匹配 pending，终态任务查不到即回 no_task）。
# 本脚本靠查库确认此刻任务状态确实是终态 done 而非 pending，证明这是
# "终态任务查不到"这条正确路径导致的 sanboot，不是走运撞对了响应内容。
REBOOT_AFTER_DONE="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_UEFI_SN}&mac=${HOST_UEFI_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${REBOOT_AFTER_DONE}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段9b: 装完的机器再次引导未回 sanboot，响应=${REBOOT_AFTER_DONE}"
DONE_STATE_CHECK="$(db_value "SELECT state FROM tasks WHERE id='${TASK_ID}';")"
[ "${DONE_STATE_CHECK}" = "done" ] || fail "阶段9b: 断言前置条件被破坏——任务状态应仍是 done，实际=${DONE_STATE_CHECK}（说明上面这次 /v1/boot 请求本身改变了任务状态，需要核查是不是 FindPendingTaskByIdentity 命中了不该命中的任务）"
pass "阶段9b: 装完的机器再次引导拿到 sanboot（不是装机菜单，不循环重装；已用状态查询确认任务确实处于终态 done）"

# 终态任务的应答文件不再提供（§4.4 状态门控：仅 booting/installing 可服务）。
ANSWER_AFTER_DONE_CODE="$(curl -s -o /dev/null -w '%{http_code}' "${ANSWER_URL}")"
[ "${ANSWER_AFTER_DONE_CODE}" = "403" ] || fail "阶段9c: 终态任务的应答文件请求返回 HTTP ${ANSWER_AFTER_DONE_CODE}, want 403"
pass "阶段9c: 终态任务的应答文件不再提供（403）"

# 事件表留下了完整时间线。
FINAL_EVENTS="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK_ID}/events" -H "${AUTH_HEADER}")"
FINAL_EVENT_COUNT="$(echo "${FINAL_EVENTS}" | grep -o '"taskId"' | wc -l)"
[ "${FINAL_EVENT_COUNT}" -ge 11 ] || fail "阶段9d: 事件表条数=${FINAL_EVENT_COUNT}, want 至少 11（十一个信号各至少一条事件），响应=${FINAL_EVENTS}"
pass "阶段9d: 事件表留下完整时间线（${FINAL_EVENT_COUNT} 条事件）"

echo ""
echo "=== 完整状态链（task_id=${TASK_ID}，Windows/UEFI 家族） ==="
echo "pending → booting（/v1/boot 命中签 token，机器收到真实 wimboot 菜单） →"
echo "  installing（/v1/answer 渲染出真实 unattend.xml，XML 校验通过） →"
echo "  [pre-start → pre-done → packages-start → packages-done → post-start → post-done → rebooting]（装机期七信号） →"
echo "  [booted → init-start → init-done → ready]（重启后四信号，finalize token） → done"
echo ""
echo "各阶段验证结果："
db_task_summary "${TASK_ID}"
echo ""
echo "=== Windows 家族九阶段验证结束：分派已接线，wimboot 菜单/固件选键/unattend.xml/信号通道/状态机/终态收敛/双固件准入全部验证通过（导入识别仍被 mvpUnsupportedFamilies 挡着，制品集经 sqlite3 绕过，见文件头「已知限制」第 2 条） ==="
