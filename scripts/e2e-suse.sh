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
# 结论先行：上一轮（feat/n8-suse-e2e）本脚本在阶段 5 因为一个真实的、
# 阻断性的 Go 侧缺口 FAIL——menu.go/renderer.go 对 OSFamilySUSE 硬编码走
# unimplementedFamilyError，从未调用已经实现好的 AssembleAutoYaST/
# RenderAutoYaSTMenu。**那个阻断本轮（feat/n9-suse-e2e-finish）已经解除**：
# 编排方在 commit 9702a0f 接上了两处分派（menu.go 第 49 行改为
# `return RenderAutoYaSTMenu(ctx)`，renderer.go 第 112 行改为
# `return AssembleAutoYaST(ctx)`），并新增 internal/render/menu_test.go 的
# TestMenu_SUSEUsesAutoYaSTNotKickstart 单元测试锁住"SUSE 菜单用 autoyast=
# 而非 inst.ks="这条断言。本脚本据此把阶段 6-9 补完，阶段 5 的过期"意外"
# 文案也已改正（见下方阶段 5）。
# ============================================================================
#
# 已知限制与真实发现（写在这里而不是假装测过了，本脚本是验证轨，不是
# 修复轨——发现的 Go 侧问题写进任务返回值的 findings/contractGaps，不在
# 本脚本或 Go 代码里修）：
#
#  1. 【不依赖 jq】同 scripts/e2e.sh 第 1 条，本机没有 jq。
#
#  2. 【AutoYaST 渲染器不生成 packages-start/packages-done 两个信号点】——
#     读 internal/render/autoyast.go 逐行确认：preScriptsSection 只发
#     pre-start/pre-done，chrootScriptsSection 的 post 段落只发
#     post-start/post-done/rebooting，全文件 grep "packages-start"/
#     "packages-done" 零命中。与 scripts/e2e-debian.sh 报告记录的
#     preseed.go 同款缺口一致（AutoYaST 没有对应"软件包安装阶段"的钩子可用，
#     与 kickstart %packages 段落不同，YaST 的包安装由 <partitioning>/
#     <software> 等结构化配置驱动，不是一段可以插入钩子的脚本阶段）。本
#     脚本仍在阶段 8 打这两个信号点（验证网关侧协议本身的通用能力），但
#     明确标注这只验证网关能力，不代表"AutoYaST 渲染器已生成该信号点"。
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
# 依赖：curl、sqlite3、go（用 go build 起服务，本脚本阶段 8 另外用
# go run 跑一个临时程序校验 autoinst.xml 是合法 XML，用完即删）。
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
# 阶段 5：preview 端点（不需要真任务、不建任务、不签 token、不落库）
#
# preview 端点内部调用 contract.Renderer.RenderAnswerFile，SUSE 分支现在
# 调用 AssembleAutoYaST（renderer.go 第 112 行，本轮已接线，见文件头结论）。
# 上一轮（n8-suse-e2e）这一步必然 FAIL；本轮预期成功——若仍然失败，说明
# 「阻断已解除」这个前提判断有误，需要人工复核，不应该放宽断言硬凑过去。
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"
[ "${PREVIEW_CODE}" = "0" ] || fail "阶段5: preview 端点未成功——若这里失败，说明「SUSE 分派已接线」这个前提与实际观测不一致，需要人工复核 menu.go/renderer.go 是否被后续改动回退，响应=${PREVIEW_RESP}"
echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 响应缺少 answerFile 字段"
pass "阶段5: preview 端点用假数据渲染成功（menu.go/renderer.go 的 SUSE 分派已接线到 AutoYaST 渲染器，与上一轮的阻断性 FAIL 不同）"

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例（四条，本脚本最有价值的部分，结构与
#         scripts/e2e.sh / scripts/e2e-debian.sh 完全一致）
# ---------------------------------------------------------------------------

# 6a. 完全陌生的机器 → 回 sanboot，响应体逐字符含那三行 sanboot 脚本
#     （§9.1 三道闸门之一：no_task 不建任务、不记状态、不告警）。
STRANGER_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=NO-SUCH-SUSE-SN&mac=00:00:00:00:00:98&uuid=&arch=x86_64&plat=efi")"
echo "${STRANGER_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6a: 陌生机器未收到 sanboot 载荷，响应=${STRANGER_BOOT}"
pass "阶段6a: 完全陌生的机器请求 /v1/boot 回 sanboot"

# 先建一个真正的任务，供 6b/6c/7/8/9 使用。
TASK_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-suse-host-01\"}}")"
TASK_ID="$(json_field "${TASK_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段6: 建任务失败，响应=${TASK_CREATE_RESP}"
echo "建任务成功: task_id=${TASK_ID}" >&2

# 6b. SN 对但 MAC 不对 → 拒签 token，回 sanboot（§4.3 双因子硬要求）。
WRONG_MAC_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_SN}&mac=ff:ff:ff:ff:ff:ff&uuid=&arch=x86_64&plat=efi")"
echo "${WRONG_MAC_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6b: MAC 不匹配未回 sanboot，响应=${WRONG_MAC_BOOT}"
TASK_AFTER_MAC_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_AFTER_MAC_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6b: MAC 不匹配后任务的 failureReason 字段未见 precheck，响应=${TASK_AFTER_MAC_FAIL}"
pass "阶段6b: SN 对但 MAC 不对 → 回 sanboot，且任务被置 failed（failureReason=precheck，双因子拒签 token）"

# 6c. 架构不符 → 硬拒绝且回 sanboot，任务置 failed（§4.6：重试必然同样
#     失败，不消耗 attempt_count 的语义体现在 6d 里"允许 retry"）。用一台
#     新主机+新任务（6b 已经把上一个任务打成 failed，不能复用）。
HOST2_SN="E2E-SUSE-SN-0002"
HOST2_MAC="aa:bb:cc:dd:ee:22"
HOST2_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST2_SN}\",\"mac\":\"${HOST2_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-SUSE-RACK\"}")"
HOST2_ID="$(json_field "${HOST2_RESP}" "id")"
[ -n "${HOST2_ID}" ] || fail "阶段6c: 录入第二台主机失败，响应=${HOST2_RESP}"

TASK2_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST2_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-suse-host-02\"}}")"
TASK2_ID="$(json_field "${TASK2_CREATE_RESP}" "id")"
[ -n "${TASK2_ID}" ] || fail "阶段6c: 建第二个任务失败，响应=${TASK2_CREATE_RESP}"

# 制品集要求 arch=x86_64，这里故意上报 arm64，MAC 用真实值（通过双因子），
# 触发的应该是 arch 不一致而不是 MAC 不匹配。
ARCH_MISMATCH_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=arm64&plat=efi")"
echo "${ARCH_MISMATCH_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6c: 架构不符未回 sanboot，响应=${ARCH_MISMATCH_BOOT}"

TASK2_AFTER_ARCH_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_ARCH_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6c: 架构不符后任务的 failureReason 字段未见 precheck，响应=${TASK2_AFTER_ARCH_FAIL}"
pass "阶段6c: 架构不符 → 回 sanboot 且任务置 failed（failureReason=precheck，硬拒绝，原因可读）"

# 6d. precheck 失败的任务允许 retry 并回到 pending，且不消耗 attempt_count
#     语义的体现：重试后走的是全新一次判定，不是"attempt_count 用尽后拒绝"
#     （§4.6/§8.2 只禁两类超时，不扩大范围）。用 6c 里已经 failed 的
#     TASK2_ID。
RETRY_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK2_ID}/retry" -H "${AUTH_HEADER}")"
RETRY_STATE="$(json_field "${RETRY_RESP}" "state")"
[ "${RETRY_STATE}" = "pending" ] || fail "阶段6d: precheck 失败的任务 retry 后状态 = ${RETRY_STATE}, want pending，响应=${RETRY_RESP}"
pass "阶段6d: precheck 失败（非 reboot_timeout/init_timeout）的任务允许 retry 并回到 pending（不消耗 attempt_count 语义：重试是全新判定，不是「配额用尽」）"

pass "阶段6: 四条准入控制负面用例全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：模拟 iPXE 取引导脚本与菜单，从菜单里抠出装机 token
#
# 用阶段 6d 里 retry 回到 pending 的 TASK2_ID/HOST2_SN/HOST2_MAC——它已经
# 携带正确的 arch 上报值验证过一次拒绝路径，现在带正确的一切走一遍成功
# 路径，天然衔接「同一台机器重试后能正常装上」这条叙事，不需要再造第三
# 台主机（与 scripts/e2e.sh/e2e-debian.sh 同一模式）。
# ---------------------------------------------------------------------------

BOOTSCRIPT="$(curl -fsS "${GW_BASE}/boot.ipxe")"
echo "${BOOTSCRIPT}" | grep -q "chain ${GW_BASE}/v1/boot" \
	|| fail "阶段7: /boot.ipxe 内容不含预期的 chain 指令，响应=${BOOTSCRIPT}"

BOOT_MENU="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"

# SUSE 菜单形状（RenderAutoYaSTMenu，SPEC §0.3/§4.5）：
#   kernel <artifact>/linux \
#     autoyast=<gw>/v1/answer/<task>/<token>/autoinst.xml
#   initrd <artifact>/initrd
# 与 RHEL 的 vmlinuz+initrd.img+inst.ks= 形状不同——SUSE 反复被警示"不能
# 因为都被口头叫 ks 就当 kickstart"，这里显式断言 autoyast= 存在且
# inst.ks= 不存在，不能只断言"菜单非空"。同时断言引导制品文件名是
# linux/initrd（openSUSE 命名，不是 RHEL 的 vmlinuz/initrd.img）。
echo "${BOOT_MENU}" | grep -q "autoyast=" \
	|| fail "阶段7: 引导菜单未包含 autoyast= 行，响应=${BOOT_MENU}"
echo "${BOOT_MENU}" | grep -q "inst.ks=" \
	&& fail "阶段7: 引导菜单含 inst.ks=（那是 RHEL kickstart 的参数，SUSE 绝不能用它，SPEC 反复警示），响应=${BOOT_MENU}"
echo "${BOOT_MENU}" | grep -qE "^kernel .*/linux( |$|\\\\)" \
	|| fail "阶段7: 引导菜单的 kernel 行未指向 linux（SUSE/openSUSE 固定内核输出文件名，不是 vmlinuz），响应=${BOOT_MENU}"
echo "${BOOT_MENU}" | grep -qE "^initrd .*/initrd$" \
	|| fail "阶段7: 引导菜单的 initrd 行未指向 initrd（SUSE 固定 initrd 输出文件名，不是 initrd.img），响应=${BOOT_MENU}"

# autoyast= 的值形如 <gateway_base>/v1/answer/<task_id>/<install_token>/
# autoinst.xml（RenderAutoYaSTMenu）。用 sed 抠出
# <task_id>/<install_token> 这一段，再拆成两个变量。token 走路径段而非
# query（SPEC §4.4），核对值本身不含 "?" 或 "&"。
AUTOYAST_URL_VALUE="$(echo "${BOOT_MENU}" | grep -o "autoyast=[^[:space:]]*")"
case "${AUTOYAST_URL_VALUE}" in
*'?'*|*'&'*)
	fail "阶段7: autoyast= 的值含 ? 或 &，token 应走路径段而非 query（SPEC §4.4），实际值=${AUTOYAST_URL_VALUE}"
	;;
esac

AUTOYAST_URL_SEGMENT="$(echo "${AUTOYAST_URL_VALUE}" | sed -E 's#autoyast=.*/v1/answer/##')"
INSTALL_TASK_ID="$(echo "${AUTOYAST_URL_SEGMENT}" | cut -d/ -f1)"
INSTALL_TOKEN="$(echo "${AUTOYAST_URL_SEGMENT}" | cut -d/ -f2)"
[ "${INSTALL_TASK_ID}" = "${TASK2_ID}" ] || fail "阶段7: 菜单里的 task id (${INSTALL_TASK_ID}) 与预期 (${TASK2_ID}) 不符"
[ -n "${INSTALL_TOKEN}" ] || fail "阶段7: 未能从菜单里抠出装机 token，AUTOYAST_URL_SEGMENT=${AUTOYAST_URL_SEGMENT}"

# 断言 /v1/boot 命中后任务状态变为 booting（计划任务描述明确要求这一断言）。
TASK2_DETAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_DETAIL}" | grep -q '"state":"booting"' \
	|| fail "阶段7: /v1/boot 命中后任务状态未变为 booting，响应=${TASK2_DETAIL}"

pass "阶段7: /v1/boot 命中签发 token，任务进入 booting，从 SUSE 菜单（linux/initrd/autoyast= 形状，无 inst.ks=）取得 install token"

# ---------------------------------------------------------------------------
# 阶段 8：取 autoinst.xml 应答文件（断言合法 XML、非 kickstart、四个 hook
#          段落都在）、装机期七信号、重启后四信号
# ---------------------------------------------------------------------------

ANSWER_URL="${GW_BASE}/v1/answer/${TASK2_ID}/${INSTALL_TOKEN}/autoinst.xml"
ANSWER_BODY="$(curl -fsS "${ANSWER_URL}")"
[ -n "${ANSWER_BODY}" ] || fail "阶段8: 取应答文件失败或内容为空，url=${ANSWER_URL}"

# 临时目录：只用于本阶段的 XML 校验小程序，用完即删（铁律 1：不留任何
# 临时文件在仓库/worktree 里）。
TMP_XML_CHECK_DIR="${REPO_ROOT}/.tmp/e2e-suse-xmlcheck"
mkdir -p "${TMP_XML_CHECK_DIR}"
ANSWER_XML_FILE="${TMP_XML_CHECK_DIR}/autoinst.xml"
printf '%s' "${ANSWER_BODY}" >"${ANSWER_XML_FILE}"

# 用 encoding/xml 解析确认这是合法 XML——不满足于"看起来像 XML"这种表面
# 断言（任务要求："用 go run 临时跑一个调 encoding/xml 的小程序解析它"）。
cat >"${TMP_XML_CHECK_DIR}/main.go" <<'EOF'
// 临时程序：只用于 scripts/e2e-suse.sh 阶段 8 校验 autoinst.xml 是合法
// XML，不是本仓库的一部分，验证完毕后由脚本删除整个 .tmp/e2e-suse-xmlcheck
// 目录。
package main

import (
	"encoding/xml"
	"fmt"
	"os"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "用法: main <xml文件路径>")
		os.Exit(1)
	}
	data, err := os.ReadFile(os.Args[1])
	if err != nil {
		fmt.Fprintf(os.Stderr, "读文件失败: %v\n", err)
		os.Exit(1)
	}
	var v any
	if err := xml.Unmarshal(data, &v); err != nil {
		fmt.Fprintf(os.Stderr, "XML 解析失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("XML 解析成功")
}
EOF

( cd "${REPO_ROOT}" && go run "${TMP_XML_CHECK_DIR}/main.go" "${ANSWER_XML_FILE}" ) \
	|| fail "阶段8: autoinst.xml 未通过 encoding/xml 解析校验（不是合法 XML），内容见 ${ANSWER_XML_FILE}"

# 清理临时校验目录（铁律 1：用完只删自己建的这一个）。
rm -rf "${TMP_XML_CHECK_DIR}"

pass "阶段8a: autoinst.xml 通过 encoding/xml 解析校验（合法 XML）"

# 断言不含 kickstart 特征——SPEC 反复警示"绝不能因为都被口头叫 ks 就把
# SUSE 当 kickstart"，AutoYaST 是独立 XML 格式，不应该出现任何 kickstart
# 段落标记。
echo "${ANSWER_BODY}" | grep -q "%packages" \
	&& fail "阶段8: autoinst.xml 含 %packages（kickstart 特征段落，SUSE 绝不能出现，说明渲染器误路由到了 kickstart 模板）"
echo "${ANSWER_BODY}" | grep -qE "^%pre|^%post" \
	&& fail "阶段8: autoinst.xml 含 %pre/%post（kickstart 段落标记，AutoYaST 应该是 XML 的 <pre-scripts>/<chroot-scripts>，不是段落式纯文本）"
pass "阶段8b: autoinst.xml 不含 kickstart 特征（%packages/%pre/%post 段落标记均不存在）"

# 断言 AutoYaST 的四个 hook 阶段对应段落都在——SPEC 要求 pre-scripts/
# chroot-scripts/post-scripts/init-scripts 四段，Lober 的映射是 pre →
# <pre-scripts>，post-nochroot → <chroot-scripts chrooted=false>，post →
# <chroot-scripts chrooted=true>（见 internal/render/autoyast.go 文件头
# 「假同源陷阱」说明：post-scripts/init-scripts 两个官方段落名不用，
# Lober 的四阶段全部落在 pre-scripts/chroot-scripts 两种段落类型里，用
# <chrooted> 布尔子元素区分 post-nochroot/post）。
echo "${ANSWER_BODY}" | grep -q "<pre-scripts" \
	|| fail "阶段8: autoinst.xml 缺少 <pre-scripts> 段落（Lober pre 阶段的载体）"
echo "${ANSWER_BODY}" | grep -q "<chroot-scripts" \
	|| fail "阶段8: autoinst.xml 缺少 <chroot-scripts> 段落（Lober post-nochroot/post 两阶段的载体）"
echo "${ANSWER_BODY}" | grep -q "<chrooted config:type=\"boolean\">false</chrooted>" \
	|| fail "阶段8: autoinst.xml 缺少 chrooted=false 的 chroot-script（对应 post-nochroot 阶段）"
echo "${ANSWER_BODY}" | grep -q "<chrooted config:type=\"boolean\">true</chrooted>" \
	|| fail "阶段8: autoinst.xml 缺少 chrooted=true 的 chroot-script（对应 post 阶段，应携带 SSH 公钥注入与 firstboot 脚手架）"
pass "阶段8c: autoinst.xml 含 <pre-scripts>/<chroot-scripts>（chrooted=false 与 chrooted=true 均存在），四个 hook 阶段的载体齐全"

# 断言渲染器骨架三件套：SSH 公钥注入（authorized_keys）、firstboot 脚手架
# （init.pending）、CDATA 包裹的脚本正文（<source><![CDATA[）。
echo "${ANSWER_BODY}" | grep -q "authorized_keys" || fail "阶段8: autoinst.xml 缺少 SSH 公钥注入（authorized_keys，SPEC §1.5：root 不设可复用密码）"
echo "${ANSWER_BODY}" | grep -q "init.pending" || fail "阶段8: autoinst.xml 缺少 firstboot 脚手架（init.pending）"
echo "${ANSWER_BODY}" | grep -q "<source><!\[CDATA\[" || fail "阶段8: autoinst.xml 缺少 CDATA 包裹的脚本正文（<source><![CDATA[）"

# 五个真实由渲染器生成的信号点（pre-start/pre-done/post-start/post-done/
# rebooting）应该能在脚本正文里直接找到——与 scripts/e2e-debian.sh 阶段
# 8a 解码验证的严谨程度一致，不满足于"段落存在"这种表面断言。
# packages-start/packages-done 两个**不**在这里断言存在——见文件头「已知
# 限制」第 2 条：AutoYaST 没有对应的软件包安装钩子，渲染器不生成它们，
# 断言"不存在"才是诚实的（若断言存在，等于把一个不存在的能力说成存在）。
for point in 'signal "pre-start" 0' 'signal "pre-done" 0' 'signal "post-start" 0' 'signal "post-done" 0' 'signal "rebooting" 0'; do
	echo "${ANSWER_BODY}" | grep -qF "${point}" \
		|| fail "阶段8: autoinst.xml 的脚本正文缺少信号点 ${point}"
done
echo "${ANSWER_BODY}" | grep -qF 'signal "packages-start"' \
	&& echo "警告：autoinst.xml 意外包含 packages-start 信号点，与读代码判断（autoyast.go 不生成该信号点）不一致，需要人工复核" >&2
pass "阶段8d: autoinst.xml 脚本正文确认含 pre-start/pre-done/post-start/post-done/rebooting 五个真实信号点（packages-start/packages-done 两个由 AutoYaST 渲染器本身不生成，属已知限制，未断言存在）"

# 重复发同一次取应答文件请求仍应成功——installer 可能重复取（幂等性是
# MarkAnswerFetched 的通用保证，SPEC §6.3）。
curl -fsS -o /dev/null "${ANSWER_URL}" || fail "阶段8: 重复取应答文件失败（应幂等成功）"

TASK2_AFTER_ANSWER="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_ANSWER}" | grep -q '"state":"installing"' \
	|| fail "阶段8: 取应答文件后任务状态未变为 installing，响应=${TASK2_AFTER_ANSWER}"
pass "阶段8e: 取应答文件成功，任务进入 installing，重复取仍幂等成功"

# finalize token：命中引导时网关已经顺带签发（handleBootGrant 里调用
# IssueFinalizeToken），但网关不会把它暴露在任何人面可读的响应里
# （§6：人面 API 不回传任务的装机凭据）。直接查库取得同一个真实值，只用
# 于验证"信号端点能否用它认证"这后半段（与 RHEL/Debian e2e 脚本同一处理）。
FINALIZE_TOKEN="$(sqlite3 "${DB_PATH}" "SELECT finalize_token FROM tasks WHERE id='${TASK2_ID}';")"
[ -n "${FINALIZE_TOKEN}" ] || fail "阶段8: 无法从数据库取得 finalize token（任务应已在阶段7的 /v1/boot 命中路径签发）"

send_signal() {
	local token="$1" phase="$2" seq="$3" rc="${4:-0}"
	# 故意不用 -f：本函数的调用方里有"预期非 2xx"的负面用例（finalize
	# token 发装机期信号应得 403），-f 会让 curl 本身在非 2xx 时返回非零
	# 退出码，配合 set -e 会在还没来得及检查 http_code 之前就整个杀死
	# 脚本——用 -s -S（仅在网络层错误时报错，HTTP 状态码留给调用方自己
	# 判断）搭配 -w '%{http_code}' 才能既拿到状态码又不被非预期的严格模式
	# 提前终止。
	curl -sS -X POST "${GW_BASE}/v1/signal/${TASK2_ID}/${token}/${phase}?rc=${rc}&seq=${seq}" \
		-d "signal ${phase} seq=${seq}" -o /dev/null -w '%{http_code}'
}

# 十一个信号按顺序打点：装机期七个（前七个）用 INSTALL_TOKEN，重启后四个
# （后四个）用 FINALIZE_TOKEN（§6.4）。本阶段验证的是网关侧信号端点本身
# 能否接受这十一个 phase 值（家族无关的通用能力，已被其它 e2e 脚本与
# 自动化测试覆盖）；packages-start/packages-done 两点在阶段 8d 已经明确
# 记录"渲染器不生成"，这里仍打点是为了与十一信号协议的完整性做对照测试，
# 不代表"验证了 SUSE 装机会真的发出这两个信号"（与 e2e-debian.sh 阶段 8b
# 同一处理方式，不应混淆两种不同性质的验证）。
SEQ=1
for phase in pre-start pre-done packages-start packages-done post-start post-done rebooting; do
	code="$(send_signal "${INSTALL_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（装机期）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

# 延迟到达的早期信号不把阶段打回去（§3.6 只进不退）：用一个比当前水位
# 更小的 seq 重发 pre-start，断言仍回 200。
LATE_CODE="$(send_signal "${INSTALL_TOKEN}" "pre-start" 1)"
[ "${LATE_CODE}" = "200" ] || fail "阶段8: 延迟到达的早期信号（seq=1，水位已经在 7 之后）未回 200"

# 重复发同一个 seq 仍回成功（§6.3：机器侧会重试三次）。
DUP_CODE="$(send_signal "${INSTALL_TOKEN}" "post-done" 6)"
[ "${DUP_CODE}" = "200" ] || fail "阶段8: 重复发同一个 seq（post-done, seq=6）未回 200"

# finalize token 发装机期信号被拒（§6.4 权限受限）：pre-start 不在
# FinalizeCanSend 白名单内。
FINALIZE_MISUSE_CODE="$(send_signal "${FINALIZE_TOKEN}" "pre-start" 100)"
[ "${FINALIZE_MISUSE_CODE}" = "403" ] || fail "阶段8: finalize token 发装机期信号（pre-start）返回 HTTP ${FINALIZE_MISUSE_CODE}, want 403"

pass "阶段8f: 装机期七个信号打点成功（网关侧协议验证；pre-start/pre-done/post-start/post-done/rebooting 五个已在8d确认渲染器真实生成，packages-start/packages-done 两个渲染器未生成，见 findings）；延迟到达/重复 seq 均幂等；finalize token 越权发装机期信号被拒（403）"

# 重启后四个信号用 finalize token。
for phase in booted init-start init-done ready; do
	code="$(send_signal "${FINALIZE_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（重启后）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

pass "阶段8g: 重启后四个信号（booted/init-start/init-done/ready）打点成功"

# ---------------------------------------------------------------------------
# 阶段 9：断言终态 done；装完再引导一次验证不循环重装；应答文件不再提供；
#          事件时间线完整
# ---------------------------------------------------------------------------

wait_for_task_state() {
	local sn="$1" want="$2"
	curl -fsS "${CONSOLE_BASE}/tasks?sn=${sn}" -H "${AUTH_HEADER}" | grep -q "\"state\":\"${want}\""
}
poll_until "任务达到终态 done" 10 wait_for_task_state "${HOST2_SN}" "done"
pass "阶段9a: 任务达到终态 done"

# 装完的机器再次引导拿到 sanboot 而不是装机菜单——这是 §10 失败模式 1
# 的拦截点：任务已是终态，FindPendingTaskByIdentity 只匹配 pending，
# 查不到即回 no_task。BIOS/MBR 下没有 efibootmgr，装完的机器完全靠这一行
# 回本地盘——这条断言的现实意义正是这一点。
REBOOT_AFTER_DONE="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${REBOOT_AFTER_DONE}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段9b: 装完的机器再次引导未回 sanboot，响应=${REBOOT_AFTER_DONE}"
pass "阶段9b: 装完的机器再次引导拿到 sanboot（不是装机菜单，不循环重装）"

# 终态任务的应答文件不再提供（§4.4 状态门控：仅 booting/installing 可服务）。
ANSWER_AFTER_DONE_CODE="$(curl -s -o /dev/null -w '%{http_code}' "${ANSWER_URL}")"
[ "${ANSWER_AFTER_DONE_CODE}" = "403" ] || fail "阶段9c: 终态任务的应答文件请求返回 HTTP ${ANSWER_AFTER_DONE_CODE}, want 403"
pass "阶段9c: 终态任务的应答文件不再提供（403）"

# 事件表留下了完整时间线（任务详情页靠它）。
FINAL_EVENTS="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK2_ID}/events" -H "${AUTH_HEADER}")"
FINAL_EVENT_COUNT="$(echo "${FINAL_EVENTS}" | grep -o '"taskId"' | wc -l)"
[ "${FINAL_EVENT_COUNT}" -ge 11 ] || fail "阶段9d: 事件表条数=${FINAL_EVENT_COUNT}, want 至少 11（十一个信号各至少一条事件），响应=${FINAL_EVENTS}"
pass "阶段9d: 事件表留下完整时间线（${FINAL_EVENT_COUNT} 条事件）"

echo ""
echo "=== 完整状态链（task_id=${TASK2_ID}） ==="
echo "pending → booting（/v1/boot 命中签 token，autoyast= 而非 inst.ks=） → installing（取 autoinst.xml） →"
echo "  [pre-start → pre-done → packages-start → packages-done → post-start → post-done → rebooting]（装机期七信号，网关侧验证；packages-* 两点渲染器未生成） →"
echo "  [booted → init-start → init-done → ready]（重启后四信号，finalize token） → done"
echo ""
echo "各阶段验证结果："
sqlite3 "${DB_PATH}" "SELECT '  state='||state||' current_phase='||current_phase||' attempt_count='||attempt_count||' last_seq='||last_seq FROM tasks WHERE id='${TASK2_ID}';"
echo ""
echo "=== 全部九个阶段（含四条负面用例）验证通过 ==="
echo ""
echo "=== 真实发现摘要（详见 docs/real-machine/16-e2e-suse.md） ==="
echo "1. 上一轮阻断（menu.go/renderer.go 对 SUSE 硬编码拒绝）已解除，本轮验证到完整链路可用"
echo "2. AutoYaST 渲染器不生成 packages-start/packages-done 两个信号点（与 Debian 的 preseed.go 同款缺口）"
echo "3. MountExtractor.Open 仍未实现真实挂载逻辑（全家族共享缺口，非 SUSE 专属），本脚本退回 sqlite3 绕法"
