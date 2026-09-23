#!/usr/bin/env bash
# scripts/e2e-suse.sh —— SUSE 端到端验证：curl 模拟机器打点，整条链
# 本地跑通，不碰硬件。对应「SUSE 端到端验证」任务，结构照抄
# scripts/e2e.sh（RHEL 基线）与 scripts/e2e-debian.sh 的形状写（本脚本
# 不修改它们）。
#
# 目的：证明"AutoYaST 渲染器 27 条单元测试 + SUSE 抽取器 7 条测试全绿"与
# "能从 ISO 装出一台 openSUSE"之间的全链路（导入、分派、菜单、AutoYaST
# 应答文件供给、信号）确实衔接得上。
#
# ============================================================================
# 结论先行（写在文件头，而不是让读者跑到最后才知道）：本脚本在阶段 5 就会
# 因为一个真实的、阻断性的 Go 侧缺口而 FAIL——这是如实报告，不是本脚本的
# bug。完整分析见下方「真实发现」第 2 条与本任务返回值的 findings。
# ============================================================================
#
# 已知限制与真实发现（写在这里而不是假装测过了，本脚本是验证轨，不是
# 修复轨——发现的 Go 侧问题写进任务返回值的 findings/contractGaps，不在
# 本脚本或 Go 代码里修）：
#
#  1. 【不依赖 jq】同 scripts/e2e.sh 第 1 条，本机没有 jq。
#
#  2. 【阻断性缺口：menu.go 的 RenderMenu 与 renderer.go 的 RenderAnswerFile
#     两个分派 switch 里，OSFamilySUSE 分支都硬编码走 unimplementedFamilyError，
#     从未调用 autoyast.go 里已经完整实现的 RenderAutoYaSTMenu/
#     AssembleAutoYaST——即使制品集抽取、任务创建、boot 命中全部成功，
#     菜单渲染这一步也会 100% 返回错误，机器拿不到任何引导菜单，AutoYaST
#     应答文件端点更不会被调用。】
#
#     这不是本脚本"没找对调用方式"，是读代码逐字确认的：
#       - internal/render/menu.go 第 46-47 行：
#           case model.OSFamilySUSE:
#               return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
#         第 18-20 行注释写得很直白："AutoYaST 的渲染实现
#         （AssembleAutoYaST/RenderAutoYaSTMenu）在本 worktree 内不存在
#         （已检查 internal/render/ 下所有文件……）"——但
#         internal/render/autoyast.go 里 RenderAutoYaSTMenu（第 496 行起）
#         与 AssembleAutoYaST（第 422 行起）都是完整实现，各有独立的单元
#         测试覆盖（autoyast_test.go）。这条注释是过时的：AutoYaST 渲染器
#         这一轮任务落地之后，menu.go 的分派没有跟着更新。
#       - internal/render/renderer.go 第 85-90 行同样的模式：
#           case model.OSFamilySUSE:
#               // AutoYaST 的渲染实现（AssembleAutoYaST）在本 worktree 内
#               // 不存在（已检查 internal/render/ 下所有文件），没有实现
#               // 可接。
#               return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
#         同样的过时注释，同样的硬编码拒绝。
#       - 全仓 grep "AssembleAutoYaST\b" 与 "RenderAutoYaSTMenu\b"（排除
#         autoyast.go 自己与 autoyast_test.go）：零个调用点。两个函数是
#         被单元测试直接调用验证的"孤岛实现"，从未被 RenderMenu/
#         RenderAnswerFile 这两个真实分派入口引用。
#
#     后果：即使阶段 4 的制品集/模板/主机全部就位，阶段 5 的 preview 端点
#     （内部调用 RenderAnswerFile）与阶段 7 的 /v1/boot 菜单渲染（内部调用
#     RenderMenu）都会返回 500/错误响应，不是"渲染出的 AutoYaST 内容有
#     瑕疵"，是"渲染这一步本身直接被拒绝，从未产出任何内容"。
#
#     本脚本按任务要求"诚实报告失败比让脚本变绿有价值"，在阶段 5 就 FAIL
#     并给出上面的诊断，不会为了让后续阶段能跑而在脚本里加变通把这两个
#     分支绕过去（那需要改 Go 代码，超出本轨"只写 e2e 脚本"的授权范围）。
#
#  3. 【制品集登记→真实导入 job 已经接线，但 MountExtractor.Open 尚未实现
#     真实挂载逻辑，本脚本按要求先尝试真实路径，确认走不通后才退回 sqlite3
#     绕法】——与 scripts/e2e-debian.sh 记录的"完全没接线"不同，读代码
#     确认这一轮已经把桥接补上了：
#       - internal/console/artifact.go 的 handleCreateArtifactSet 在落库
#         之后调用 s.deps.ArtifactImporter.TriggerImport(...)。
#       - cmd/lober/importrunner.go 的 importRunner.TriggerImport 是生产
#         实现，起 goroutine 调用 imports.NewJob(...).Run(...)。
#       - internal/imports/job.go 的 Job.Run 有 familyExtractors 查表分派，
#         SUSE 已在表内（model.OSFamilySUSE: ExtractSUSE）。
#     链路本身是通的，但 cmd/lober/wire.go 第 616 行 newImportRunner(st, nil,
#     ...) 把 extractor 参数传 nil，落到 imports.NewMountExtractor("") 这个
#     生产默认实现——它的 Open 方法（internal/imports/extract.go 第 59-64
#     行）无条件返回"尚未实现真实挂载逻辑"的错误，注释明确写着这是「部署
#     环境适配阶段留下的已知缺口」。这不是 SUSE 专属问题，是全部六个家族
#     共享的同一个缺口（挂载 ISO 需要 Linux 特权与 loop 设备，本机是
#     Windows 开发环境）——本脚本仍按要求先跑一次真实端点验证这一点，取得
#     真实分配的 ID 后退回 sqlite3 绕法，文件本身仍是 mkfixture-suse.sh
#     造出的真实文件、真实落在制品根目录下。
#
#  4. 【syslog 通道本脚本覆盖不到】同 scripts/e2e.sh 第 3 条。
#
# 依赖：curl、sqlite3、go（用 go build 起服务）。
set -euo pipefail

# ---------------------------------------------------------------------------
# 基础设施：临时目录、端口、trap 清理（结构与 scripts/e2e.sh 一致）
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e-suse.XXXXXX")"

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

# 高位、不常见端口，且与其它 e2e 脚本的端口不同段，降低并行跑撞车概率。
GW_PORT=28280
CONSOLE_PORT=28290
SYSLOG_PORT=28714

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
		tail -n 80 "${WORKDIR_WIN}/server.log" >&2 || true
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

echo "=== Lober SUSE 端到端验证 ==="
echo "工作目录: ${WORKDIR}"

# ---------------------------------------------------------------------------
# 阶段 1：造假 SUSE ISO 内容树、二进制、admin 用户、配置
# ---------------------------------------------------------------------------

mkdir -p "${WORKDIR_WIN}/db" "${ARTIFACT_ROOT}" "${LOG_ROOT}"

ARTIFACT_SET_ID="e2e-suse-leap156-x86_64"
bash "${SCRIPT_DIR}/e2e/mkfixture-suse.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_ID}"

( cd "${REPO_ROOT}" && go build -o "${BIN_PATH}" ./cmd/lober )
[ -x "${BIN_PATH}" ] || fail "阶段1: 构建 lober 二进制失败"

# admin 凭据：同其它 e2e 脚本的固定测试哈希（cost=10，对应明文
# "admin12345"），仅用于本脚本自建的临时数据库，不是生产凭据。
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
  token_secret: "e2e-suse-test-secret"
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
  # by-hook：同其它 e2e 脚本的选择理由——本脚本不跑 discovery，没有
  # LOBER_DISKS 事实，除 by-hook 外的磁盘策略在空磁盘清单下会直接报错。
  disk_policy: "by-hook"
  heartbeat_mode: "off"
EOF

"${BIN_PATH}" -config "${CONFIG_PATH}" -check || fail "阶段1: -check 校验模式未通过"

pass "阶段1: 假 SUSE ISO 内容树、二进制、admin 用户、配置文件均已就位"

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
# 阶段 4：录入主机、登记制品集（先试真实导入路径，确认走不通后退回
#          sqlite3 绕法）、建模板（active）
# ---------------------------------------------------------------------------

HOST_SN="E2E-SUSE-SN-0001"
HOST_MAC="aa:bb:cc:dd:ee:21"
HOST_UUID="31111111-1111-1111-1111-111111111111"

HOST_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_SN}\",\"mac\":\"${HOST_MAC}\",\"uuid\":\"${HOST_UUID}\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-SUSE-RACK\"}")"
HOST_ID="$(json_field "${HOST_RESP}" "id")"
[ -n "${HOST_ID}" ] || fail "阶段4: 录入主机失败，响应=${HOST_RESP}"

# 真实导入路径验证：用真实端点落一条 importing 记录，取得真实分配的 ID，
# 再观察它是否会自行推进。
ARTIFACT_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/artifact-sets" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sourcePath\":\"${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}\",\"name\":\"opensuse-leap15.6-x86_64\"}")"
CREATED_ARTIFACT_ID="$(json_field "${ARTIFACT_CREATE_RESP}" "id")"
[ -n "${CREATED_ARTIFACT_ID}" ] || fail "阶段4: 登记制品集失败，响应=${ARTIFACT_CREATE_RESP}"

# 轮询等待，观察 TriggerImport 触发的异步 job 是否推进（这一轮已经把
# handleCreateArtifactSet → ArtifactImporter.TriggerImport →
# imports.Job.Run 的调用链接上，理应能观察到状态变化，即使最终结果是
# failed 而非 ready）。
IMPORT_STATE_AFTER_WAIT=""
for _ in 1 2 3 4 5 6 7 8; do
	sleep 0.5
	IMPORT_STATE_AFTER_WAIT="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
	[ "${IMPORT_STATE_AFTER_WAIT}" != "importing" ] && break
done
echo "真实导入路径观测：等待 4 秒后状态=${IMPORT_STATE_AFTER_WAIT}" >&2

if [ "${IMPORT_STATE_AFTER_WAIT}" = "ready" ]; then
	echo "真实导入路径已推进到 ready——MountExtractor.Open 显然已经实现真实挂载" >&2
	echo "逻辑，与读代码时的判断（extract.go 的 Open 无条件返回未实现错误）不一致，" >&2
	echo "沿用这条真实产出的制品集继续后续阶段。" >&2
	DEST_DIR="${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}"
elif [ "${IMPORT_STATE_AFTER_WAIT}" = "failed" ]; then
	FAIL_DETAIL="$(sqlite3 "${DB_PATH}" "SELECT failure_detail FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';" 2>/dev/null || echo "(无 failure_detail 字段或查询失败)")"
	echo "真实导入路径确认走不通：job 已异步执行并自行判定 failed，详情=${FAIL_DETAIL}" >&2
	echo "与读代码分析一致（cmd/lober/wire.go 把 extractor 参数传 nil，落到" >&2
	echo "imports.NewMountExtractor(\"\")，其 Open 方法无条件返回" >&2
	echo "\"尚未实现真实挂载逻辑\" 错误）——退回 sqlite3 绕法。" >&2

	# mkfixture-suse.sh 造的是"ISO 内容视图"，落在 ARTIFACT_SET_ID 目录下；
	# 真实抽取（ExtractSUSE）本应把其中的 boot/x86_64/loader/{linux,initrd}
	# 与 repodata/、suse/ 两棵仓库树转换成 linux/initrd/repo/ 这一套固定
	# 输出结构。既然导入 job 的挂载步骤走不通，本脚本在绕过状态推进的同时，
	# 也用 ExtractSUSE 同款的目标文件名手动完成一次"抽取动作"本身（不是
	# 伪造：内容确实来自 mkfixture-suse.sh 造的源文件，只是搬运逻辑用 shell
	# 命令代替 Go 抽取器执行），让 paths_json 指向的文件真实存在于磁盘上
	# （docs/contracts/filesystem.md 硬约束 1）。
	SRC_ISO_DIR="${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}"
	DEST_DIR="${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}"
	mkdir -p "${DEST_DIR}/repo"
	cp "${SRC_ISO_DIR}/boot/x86_64/loader/linux" "${DEST_DIR}/linux"
	cp "${SRC_ISO_DIR}/boot/x86_64/loader/initrd" "${DEST_DIR}/initrd"
	cp -r "${SRC_ISO_DIR}/repodata" "${DEST_DIR}/repo/repodata"
	cp -r "${SRC_ISO_DIR}/suse" "${DEST_DIR}/repo/suse"

	PATHS_JSON='{"Kernel":"linux","Initrd":["initrd"],"Repo":"repo/","Extra":{}}'
	sqlite3 "${DB_PATH}" "UPDATE artifact_sets SET state='ready', os_family='suse', os_version='leap15.6', arch='x86_64', firmware='any', paths_json='${PATHS_JSON}' WHERE id='${CREATED_ARTIFACT_ID}';"
else
	fail "阶段4: 真实导入路径的状态既非 ready 也非 failed（当前=${IMPORT_STATE_AFTER_WAIT}），前提判断与实际观测不一致，需要人工复核"
fi

READY_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
[ "${READY_CHECK}" = "ready" ] || fail "阶段4: 制品集未能推进到 ready（当前=${READY_CHECK}）"

for rel in linux initrd repo/repodata/repomd.xml; do
	[ -f "${DEST_DIR}/${rel}" ] || fail "阶段4: 制品集落盘文件 ${rel} 不存在（paths_json 指向的路径应真实存在，filesystem.md 硬约束1）"
done

TEMPLATE_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-suse-leap156\",\"artifactSetId\":\"${CREATED_ARTIFACT_ID}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
TEMPLATE_ID="$(json_field "${TEMPLATE_CREATE_RESP}" "id")"
[ -n "${TEMPLATE_ID}" ] || fail "阶段4: 创建模板失败，响应=${TEMPLATE_CREATE_RESP}"

sqlite3 "${DB_PATH}" "UPDATE templates SET state='active' WHERE id='${TEMPLATE_ID}';"
ACTIVE_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM templates WHERE id='${TEMPLATE_ID}';")"
[ "${ACTIVE_CHECK}" = "active" ] || fail "阶段4: 模板未能推进到 active（当前=${ACTIVE_CHECK}）"

pass "阶段4: 主机=${HOST_ID}、制品集=${CREATED_ARTIFACT_ID}（ready）、模板=${TEMPLATE_ID}（active）均已就位"

# ---------------------------------------------------------------------------
# 阶段 5：preview 端点（不需要真任务）——预期在这里就 FAIL
#
# preview 端点内部调用 contract.Renderer.RenderAnswerFile，而
# renderer.go 对 OSFamilySUSE 硬编码走 unimplementedFamilyError（见文件头
# 「真实发现」第 2 条）。本脚本按原计划调用该端点、验证响应，如实报告
# 结果，不因为预期会失败就跳过这一步或改断言方向。
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -sS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"

if [ "${PREVIEW_CODE}" = "0" ]; then
	echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 响应缺少 answerFile 字段"
	pass "阶段5: preview 端点用假数据渲染成功（意外——与读代码判断的\"SUSE 分派硬编码拒绝\"不一致，需要人工复核 menu.go/renderer.go 是否已经改动）"
else
	echo "阶段5 观测：preview 端点未成功，响应=${PREVIEW_RESP}" >&2
	echo "${PREVIEW_RESP}" | grep -qi "MVP" || echo "警告：响应体里未见预期的 \"MVP 未实现\" 类文案，实际错误可能与本脚本文件头记录的诊断不是同一个原因，需要人工核对" >&2
	FAIL_MSG="阶段5: preview 端点未成功——这是预期内的真实阻断（见本脚本文件头「真实发现」第 2 条：renderer.go 对 OSFamilySUSE 硬编码走 unimplementedFamilyError，从未调用已经实现好的 AssembleAutoYaST），响应=${PREVIEW_RESP}。本脚本到此停止——阶段 6-9（准入负面用例、菜单渲染、AutoYaST 应答文件、装机期与重启后信号、终态断言）全部依赖菜单/应答文件渲染成功，在这个阻断解除之前无法继续验证，继续硬跑只会把断言改宽来掩盖这个真实缺口，价值为负。"
	fail "${FAIL_MSG}"
fi
