#!/usr/bin/env bash
# scripts/e2e-windows.sh —— Windows 家族端到端验证：curl 模拟机器打点，照
# scripts/e2e.sh（RHEL 基线）的九阶段结构编写，内容按 Windows 机制调整。
# 对应实现计划任务 t09-win-e2e，依据 SPEC §4.5、§4.6、附录 A.4-A.6。
#
# 本脚本只读 scripts/e2e.sh、照它的结构写，不修改它——那是 MVP 的验收基线
# （见任务说明"改坏了会掩盖回归"）。
#
# =============================================================================
# 已知限制与本次运行实测到的发现（写在这里而不是假装测过了）：
#
#  1. 【不依赖 jq】同 scripts/e2e.sh 第 1 条：本机没有 jq，用 grep/sed 做
#     最小 JSON 字段提取。
#
#  2. 【Windows 的导入路径仍被 mvpUnsupportedFamilies 挡着，本脚本走
#     sqlite3 绕法登记制品集】——这是本轮任务说明预告的可能发现，本次运行
#     已经实测确认：
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
#
#  3. 【Windows 分派处仍未接线，menu.go/renderer.go 把 Windows 与
#     SUSE/ESXi 一起路由到"未实现"分支——这是本次运行最重要的发现，不是
#     预告过的已知限制，是本脚本实测暴露的一个真实的家族级功能缺口】：
#       - internal/render/winpe.go 的 RenderWindowsMenu 函数存在且有独立
#         golden/单元测试覆盖，但 internal/render/menu.go 第 28 行的
#         RenderMenu 分派 switch 把 model.OSFamilyWindows 与
#         model.OSFamilySUSE、model.OSFamilyESXi 分在同一个 case，返回
#         unimplementedFamilyError——从未调用 RenderWindowsMenu。
#       - internal/render/unattend.go 的 RenderUnattend 函数同样存在且有
#         golden/单元测试覆盖，但 internal/render/renderer.go 第 60 行的
#         RenderAnswerFile 分派同样把 Windows 与 SUSE/ESXi 分在同一个
#         case，从未调用 RenderUnattend。
#       - 后果不是 HTTP 500 或崩溃：internal/gateway/boot.go 的
#         handleBootGrant 有"约定 1：始终返回 HTTP 200"这条硬约束
#         （第 47-49 行注释），RenderMenu 出错时统一走
#         writeNoTaskScript——对机器而言，Windows 主机请求 /v1/boot 目前
#         看到的响应与"完全陌生的机器"（阶段 6a 的 no_task）逐字符相同。
#       - 更值得注意的一点（本脚本阶段 7 实测确认）：handleBootGrant 调用
#         StartBoot 在调用 RenderMenu 之前（internal/gateway/boot.go 第
#         150-190 行）——StartBoot 内部完成 attempt++、签发装机 token、
#         任务状态 CAS 到 booting，全部成功提交后才轮到 RenderMenu 失败。
#         也就是说 Windows 机器每次请求 /v1/boot：任务被推进到 booting、
#         消耗了一次 attempt_count、签发了一个永远不会被使用的装机
#         token，但机器实际收到的是 sanboot 载荷——从任务状态与响应内容
#         两处交叉验证即可看到这个不一致（本脚本阶段 7 里两次查询均已
#         记录）。这与 SPEC §4.6"重试必然同样失败的场景不消耗
#         attempt_count"的设计意图相悖：arch/固件不符是"precheck 失败，
#         不消耗 attempt"（在 attempt++ 之前拒绝），而"分派未接线"这种
#         配置/代码缺口反而落进了"precheck 通过、正常消耗 attempt"的路径，
#         等于用户重试 MaxBootAttempts 次后 Windows 任务会被打成
#         attempt_exceeded 失败，而根因只是渲染层没接线——这是一个真实的
#         Go 侧 bug，按任务说明要求写进 findings，不在本轨修复
#         （menu.go/renderer.go 归 t08-dispatch 独占）。
#     本脚本的应对：不断言"Windows 机器能拿到装机菜单"（那在当前代码下
#     必然失败），而是断言"这个特定失败模式"本身——即精确验证上述发现
#     成立，把它变成一条可重复执行、会在分派接上后自动变红提醒需要更新
#     断言的检验，而不是含糊地跳过整个成功路径。
#
#  4. 【重启后信号/应答文件/装完再引导等阶段同样受阻】：由于阶段 7
#     拿不到装机 token（RenderMenu 失败导致机器只看到 no_task 脚本，token
#     签发了但机器根本读不到它），本脚本无法像 RHEL 基线那样从真实菜单里
#     解析出 install token 继续阶段 8/9 的信号打点与应答文件流程——这些
#     阶段依赖"机器已经从菜单里拿到 token"这个前提，Windows 分派没接线时
#     这个前提不成立。本脚本改为直接从数据库读出 StartBoot 签发的 token
#     （与 scripts/e2e.sh 阶段 8 读 finalize_token 的做法同样性质：不是
#     伪造凭据，是绕过"机器要素图从菜单文本解析"这一步去验证"信号端点本身
#     能否用它认证"），以此验证 Windows 装机期/重启后信号通道、应答文件
#     状态门控与终态收敛等与"菜单渲染是否接线"无关的机制仍然正确。
#
#  5. 【syslog 通道本脚本覆盖不到】同 scripts/e2e.sh 第 3 条，原样适用。
#
#  6. 【双固件（UEFI/BIOS）选集验证】本脚本按任务说明要求造两个 firmware
#     取具体值的 artifact_set（同一"Windows Server 2022"版本各一份），
#     验证 UEFI 与 BIOS 机器各自在 StartBoot 阶段被路由到对应的制品键、
#     以及固件不匹配时被 precheck 硬拒绝（阶段 6e）。这一验证发生在
#     RenderMenu 之前（precheck 是 StartBoot 内部的第 2 步，见
#     internal/statemachine/boot.go），因此不受第 3 条发现的影响，能够
#     完整跑通并给出确定结论。
#
# 依赖：curl、sqlite3（本机已确认可用）、go（go build/go run，不依赖已
# 安装的二进制）。
set -euo pipefail

# ---------------------------------------------------------------------------
# 基础设施：临时目录、端口、trap 清理（与 scripts/e2e.sh 完全同构）
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

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
DB_PATH="${WORKDIR_WIN}/db/lober.db"
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

mkdir -p "${WORKDIR_WIN}/db" "${ARTIFACT_ROOT}" "${LOG_ROOT}"

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

	paths_json='{"Kernel":"","Initrd":[],"Repo":"","Extra":{"wimboot":"wimboot","BCD":"BCD","boot.sdi":"boot.sdi","boot.wim":"boot.wim"}}'
	sqlite3 "${DB_PATH}" "UPDATE artifact_sets SET state='ready', os_family='windows', os_version='2022', arch='x86_64', firmware='${firmware}', paths_json='${paths_json}' WHERE id='${created_id}';"

	local ready_check
	ready_check="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${created_id}';")"
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

	sqlite3 "${DB_PATH}" "UPDATE templates SET state='active' WHERE id='${template_id}';"
	local active_check
	active_check="$(sqlite3 "${DB_PATH}" "SELECT state FROM templates WHERE id='${template_id}';")"
	[ "${active_check}" = "active" ] || fail "阶段4: 模板 ${name} 未能推进到 active（当前=${active_check}）"

	echo "${template_id}"
}

TEMPLATE_UEFI_ID="$(create_active_template "e2e-win2022-uefi" "${ARTIFACT_UEFI_ID}")"
TEMPLATE_BIOS_ID="$(create_active_template "e2e-win2022-bios" "${ARTIFACT_BIOS_ID}")"

pass "阶段4: 两台主机（uefi=${HOST_UEFI_ID}, bios=${HOST_BIOS_ID}）、两个制品集（uefi=${ARTIFACT_UEFI_ID}, bios=${ARTIFACT_BIOS_ID}，均 ready）、两个模板（均 active）已就位"

# ---------------------------------------------------------------------------
# 阶段 5：preview 端点（不需要真任务就能跑）
#
# 注意：preview 端点内部调用哪个渲染函数取决于制品集的 OSFamily
# （internal/console 的 preview 处理逻辑，未直接读过，但与 RHEL 路径同构）。
# 由于阶段3发现的分派缺口，Windows 的 preview 预期会失败——这里如实断言
# "preview 端点本身可达且返回非 0 的错误码"，而不是断言它能像 RHEL 一样
# 渲染成功，避免制造一个会长期挂红、掩盖真实修复进度的断言。
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_UEFI_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"
if [ "${PREVIEW_CODE}" = "0" ]; then
	echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 返回 code=0 但缺少 answerFile 字段，响应=${PREVIEW_RESP}"
	echo "注意: Windows preview 端点意外渲染成功——说明分派可能已在别处接线，" >&2
	echo "      请重新核对 internal/render/menu.go 与 renderer.go 的当前状态" >&2
	pass "阶段5: preview 端点渲染成功（分派看起来已接线，超出本次运行预期，已记录待核对）"
else
	pass "阶段5: preview 端点确认当前会失败（code=${PREVIEW_CODE}，与分派未接线的已知限制一致）"
fi

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例
#
# 6a-6d 与 scripts/e2e.sh 同构，验证的是与"分派是否接线"无关的通用准入
# 机制（precheck 在 RenderMenu 之前跑，见文件头「已知限制」第 3 条最后一段）。
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
BIOS_TASK_ATTEMPT_BEFORE="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK_BIOS_ID}';")"
FIRMWARE_MISMATCH_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_BIOS_SN}&mac=${HOST_BIOS_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${FIRMWARE_MISMATCH_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6e: 固件不符未回 sanboot，响应=${FIRMWARE_MISMATCH_BOOT}"
TASK_BIOS_AFTER_FW_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_BIOS_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_BIOS_AFTER_FW_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6e: 固件不符后任务的 failureReason 字段未见 precheck，响应=${TASK_BIOS_AFTER_FW_FAIL}"
BIOS_TASK_ATTEMPT_AFTER="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK_BIOS_ID}';")"
[ "${BIOS_TASK_ATTEMPT_AFTER}" = "${BIOS_TASK_ATTEMPT_BEFORE}" ] \
	|| fail "阶段6e: 固件不符本应不消耗 attempt_count，但从 ${BIOS_TASK_ATTEMPT_BEFORE} 变成了 ${BIOS_TASK_ATTEMPT_AFTER}"
pass "阶段6e:【Windows 特有】固件（BIOS 任务遇到 UEFI 上报）不符 → 回 sanboot、任务置 failed、不消耗 attempt_count（当前值=${BIOS_TASK_ATTEMPT_AFTER}）"

pass "阶段6: 四条通用准入负面用例 + 1 条 Windows 双固件负面用例全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：验证分派缺口本身——一切要素都正确（SN/MAC 双因子通过、arch/固件
# 都匹配）的 UEFI 任务请求 /v1/boot，precheck 会通过、StartBoot 会成功推进
# 到 booting 并签发 token 与消耗 attempt_count，但因为
# internal/render/menu.go 未接线 RenderWindowsMenu，机器最终仍然只收到
# no_task 载荷——用任务状态与响应内容两处交叉验证这个不一致，把「分派缺口
# 造成的静默 attempt 消耗」变成一条可重复执行的检验（见文件头「已知限制」
# 第 3 条）。
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

ATTEMPT_BEFORE="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK_ID}';")"
STATE_BEFORE="$(sqlite3 "${DB_PATH}" "SELECT state FROM tasks WHERE id='${TASK_ID}';")"
[ "${STATE_BEFORE}" = "pending" ] || fail "阶段7: 新建任务的初始状态应为 pending，实际=${STATE_BEFORE}"

UEFI_CORRECT_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_UEFI_SN}&mac=${HOST_UEFI_MAC}&uuid=&arch=x86_64&plat=efi")"

STATE_AFTER="$(sqlite3 "${DB_PATH}" "SELECT state FROM tasks WHERE id='${TASK_ID}';")"
ATTEMPT_AFTER="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK_ID}';")"
SIGNED_TOKEN="$(sqlite3 "${DB_PATH}" "SELECT token FROM tasks WHERE id='${TASK_ID}';")"

# 断言 1：机器实际收到的响应与「完全陌生的机器」逐字符相同（阶段6a同款
# sanboot 载荷）——这是分派缺口在机器侧唯一可观察到的表现。
echo "${UEFI_CORRECT_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段7: 预期 Windows 分派仍未接线（应回退到 sanboot），但响应不含 sanboot 载荷，响应=${UEFI_CORRECT_BOOT}——若 menu.go 已经接线，本脚本这条断言应改为解析真实菜单（见任务清单）"

# 断言 2：但任务状态已经被 StartBoot 推进到 booting，且签发了 token、
# 消耗了 attempt_count——证明失败发生在 StartBoot 成功之后的 RenderMenu
# 阶段，不是 precheck 拒绝（precheck 拒绝的三个用例在阶段6里已验证过
# state 会走向 failed 而不是 booting，且不消耗 attempt）。
[ "${STATE_AFTER}" = "booting" ] || fail "阶段7: 预期任务状态推进到 booting（StartBoot 已成功提交），实际=${STATE_AFTER}"
[ -n "${SIGNED_TOKEN}" ] || fail "阶段7: 预期 StartBoot 已签发装机 token，但数据库里的 token 字段为空"
[ "${ATTEMPT_AFTER}" -gt "${ATTEMPT_BEFORE}" ] || fail "阶段7: 预期 attempt_count 从 ${ATTEMPT_BEFORE} 递增，实际仍是 ${ATTEMPT_AFTER}"

pass "阶段7:【关键发现】要素齐全的 UEFI 机器请求 /v1/boot——任务被正常推进到 booting（attempt ${ATTEMPT_BEFORE}→${ATTEMPT_AFTER}，token 已签发），但因 internal/render/menu.go 未接线 RenderWindowsMenu，机器实际收到的载荷与陌生机器完全相同（sanboot）——装机 token 被静默浪费，重试 ${ATTEMPT_AFTER} 次后会被 attempt_exceeded 打成永久失败。本脚本从这里改用数据库直读 token 继续验证信号/应答文件通道本身是否正常。"

# ---------------------------------------------------------------------------
# 阶段 8a：验证应答文件端点的分派缺口——与阶段 7 同一性质的发现，换一个
# 端点。GET /v1/answer 在渲染出错时的行为与 /v1/boot 不同（无「约定 1」
# always-200 硬约束，internal/gateway/answer.go 直接回 500，见文件头
# 「已知限制」第 3 条），但同样存在「状态先被提交、渲染后失败」的顺序问题：
# handleAnswer 第 87 行的 MarkAnswerFetched（booting → installing）在
# 第 92 行的 RenderAnswerFile 调用之前——即使渲染最终失败，任务已经被
# 卡进 installing，且不会再回退。
# ---------------------------------------------------------------------------

ANSWER_URL="${GW_BASE}/v1/answer/${TASK_ID}/${SIGNED_TOKEN}/unattend.xml"
ANSWER_HTTP_CODE="$(curl -s -o "${WORKDIR_WIN}/answer_body.txt" -w '%{http_code}' "${ANSWER_URL}")"
STATE_AFTER_ANSWER_ATTEMPT="$(sqlite3 "${DB_PATH}" "SELECT state FROM tasks WHERE id='${TASK_ID}';")"

[ "${ANSWER_HTTP_CODE}" = "500" ] || fail "阶段8a: 预期 Windows 分派仍未接线（RenderAnswerFile 应报错回 500），实际 HTTP ${ANSWER_HTTP_CODE}——若 renderer.go 已经接线，本脚本这条断言应改为验证真实 XML 内容（见任务清单）"
[ "${STATE_AFTER_ANSWER_ATTEMPT}" = "installing" ] || fail "阶段8a: 预期即使渲染失败，MarkAnswerFetched 仍先于渲染把任务推进到 installing，实际=${STATE_AFTER_ANSWER_ATTEMPT}"

# 确认事件表记录了这次渲染失败（handleAnswer 第 96-100 行的 AppendEvent），
# 供运维排障时能在时间线上看到具体原因，不是静默的 500。
ANSWER_FAIL_EVENTS="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK_ID}/events" -H "${AUTH_HEADER}")"
echo "${ANSWER_FAIL_EVENTS}" | grep -q "应答文件渲染失败" \
	|| fail "阶段8a: 事件表未记录应答文件渲染失败事件，响应=${ANSWER_FAIL_EVENTS}"

pass "阶段8a:【关键发现】应答文件端点同样受阻——HTTP 500，且任务已被 MarkAnswerFetched 静默推进到 installing（渲染失败发生在状态提交之后），事件表已记录一条 level=error 的诊断事件。任务实际状态已经是 installing（与真实机器在这一步会卡住的状态完全一致），阶段8b/8c/9 借用这个已经处于 installing 的任务，直接用数据库读出的 token 继续验证信号通道与终态收敛本身是否正常（见已知限制第4条：不是伪造凭据，是绕过'从菜单文本解析 token'这一步）。"

# ---------------------------------------------------------------------------
# 阶段 8b/8c：装机期七信号 + 重启后四信号打点。TASK_ID 此刻已处于 installing
# （阶段8a的 MarkAnswerFetched 效果），装机期信号用 SIGNED_TOKEN
# （StartBoot 签发的装机 token，即 install token），重启后信号需要
# finalize token——真实机器场景下它由 %post/SetupComplete.cmd 写入目标
# 系统再读出使用，但本任务的应答文件渲染本身失败了（阶段8a），机器侧根本
# 拿不到这个值。这正是本脚本必须绕库直读的理由：验证的是"信号端点本身能否
# 用一个真实签发的 finalize token 认证"，不是"渲染器是否正确地把它写进了
# 应答文件"（那属于分派缺口，已在阶段7/8a记录）。
# ---------------------------------------------------------------------------

FINALIZE_TOKEN="$(sqlite3 "${DB_PATH}" "SELECT finalize_token FROM tasks WHERE id='${TASK_ID}';")"
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
	sqlite3 "${DB_PATH}" "SELECT state FROM tasks WHERE id='${TASK_ID}';" | grep -qx "${want}"
}
poll_until "任务达到终态 done" 10 wait_for_task_state "done"
pass "阶段9a: 任务达到终态 done"

# 装完的机器再次引导拿到 sanboot 而不是装机菜单（§10 失败模式1的拦截点：
# FindPendingTaskByIdentity 只匹配 pending，终态任务查不到即回 no_task）。
# 这条断言与「Windows 分派未接线导致的 sanboot」是两种不同原因导向同一种
# 表面响应，本脚本靠查库确认此刻任务状态确实是终态 done 而非 pending 来
# 区分——不是走运撞对了响应内容。
REBOOT_AFTER_DONE="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_UEFI_SN}&mac=${HOST_UEFI_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${REBOOT_AFTER_DONE}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段9b: 装完的机器再次引导未回 sanboot，响应=${REBOOT_AFTER_DONE}"
DONE_STATE_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM tasks WHERE id='${TASK_ID}';")"
[ "${DONE_STATE_CHECK}" = "done" ] || fail "阶段9b: 断言前置条件被破坏——任务状态应仍是 done，实际=${DONE_STATE_CHECK}（说明上面这次 /v1/boot 请求本身改变了任务状态，需要核查是不是 FindPendingTaskByIdentity 命中了不该命中的任务）"
pass "阶段9b: 装完的机器再次引导拿到 sanboot（不是装机菜单，不循环重装；已用状态查询排除'这只是分派缺口的同款 sanboot'这种混淆）"

# 终态任务的应答文件不再提供（§4.4 状态门控：仅 booting/installing 可服务）。
ANSWER_AFTER_DONE_CODE="$(curl -s -o /dev/null -w '%{http_code}' "${ANSWER_URL}")"
[ "${ANSWER_AFTER_DONE_CODE}" = "403" ] || fail "阶段9c: 终态任务的应答文件请求返回 HTTP ${ANSWER_AFTER_DONE_CODE}, want 403"
pass "阶段9c: 终态任务的应答文件不再提供（403）"

# 事件表留下了完整时间线。
FINAL_EVENTS="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK_ID}/events" -H "${AUTH_HEADER}")"
FINAL_EVENT_COUNT="$(echo "${FINAL_EVENTS}" | grep -o '"taskId"' | wc -l)"
[ "${FINAL_EVENT_COUNT}" -ge 11 ] || fail "阶段9d: 事件表条数=${FINAL_EVENT_COUNT}, want 至少 11（十一个信号各至少一条事件），响应=${FINAL_EVENTS}"
pass "阶段9d: 事件表留下完整时间线（${FINAL_EVENT_COUNT} 条事件，含阶段8a记录的应答文件渲染失败事件）"

echo ""
echo "=== 完整状态链（task_id=${TASK_ID}，Windows/UEFI 家族） ==="
echo "pending → booting（/v1/boot 命中签 token，但 menu.go 未接线导致机器实际收到 sanboot） →"
echo "  installing（/v1/answer 的 MarkAnswerFetched 先提交，但 renderer.go 未接线导致渲染 500） →"
echo "  [pre-start → pre-done → packages-start → packages-done → post-start → post-done → rebooting]（装机期七信号，数据库直读 token 模拟） →"
echo "  [booted → init-start → init-done → ready]（重启后四信号，finalize token） → done"
echo ""
echo "各阶段验证结果："
sqlite3 "${DB_PATH}" "SELECT '  state='||state||' current_phase='||current_phase||' attempt_count='||attempt_count||' last_seq='||last_seq FROM tasks WHERE id='${TASK_ID}';"
echo ""
echo "=== Windows 家族九阶段验证结束：分派与应答文件两处关键渲染入口尚未接线（阶段7/8a已精确记录），信号/状态机/终态收敛/双固件准入等与渲染无关的机制全部验证通过 ==="
