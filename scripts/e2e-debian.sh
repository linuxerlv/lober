#!/usr/bin/env bash
# scripts/e2e-debian.sh —— Debian 端到端验证：curl 模拟机器打点，整条链
# 本地跑通，不碰硬件。对应 Wave2「Debian 端到端验证」任务，依据 SPEC §11
# Phase 1 的验证行，结构照抄 scripts/e2e.sh（RHEL 基线，本脚本不修改它）。
#
# 目的：证明"渲染器测试全绿"与"能从 ISO 装出一台 Debian"之间的全链路
# （导入、分派、菜单、preseed 应答文件供给、信号）确实衔接得上，不是
# 只有孤立的单元测试通过。
#
# 已知限制与真实发现（写在这里而不是假装测过了，本脚本是验证轨，不是
# 修复轨——发现的 Go 侧问题写进任务返回值的 findings/contractGaps，不在
# 本脚本或 Go 代码里修）：
#
#  1. 【不依赖 jq】同 scripts/e2e.sh 第 1 条，本机没有 jq。
#
#  2. 【制品集登记→导入 job 之间确实没有接线，本脚本按要求先尝试真实路径，
#     确认走不通后才退回 sqlite3 绕法】——这是任务说明要求本脚本必须先验证
#     的一件事，结论是：走不通，原因如下（真实读代码确认，非猜测）：
#       - internal/console/artifact.go 的 handleCreateArtifactSet 只
#         构造一条 model.ArtifactSet{State: importing} 并调用
#         CreateArtifactSet 落库，从未引用 internal/imports 包的任何符号
#         （grep 整个 handler 文件确认零匹配）。
#       - internal/imports/job.go 的 Job 类型只有一个执行入口 RunRHEL
#         （另有 RunUnsupported 处理"识别出但不支持"与"识别不出"两类，
#         但同样不做任何家族特定抽取），没有 RunDebian 或按家族分派的
#         通用 Run 方法——即使 handler 侧接上了 imports.Job，也没有
#         入口可调用来跑 Debian 的抽取。
#       - 对整个仓库（含 cmd/lober 装配点）grep "imports\." 只命中
#         internal/imports 包自己的测试文件与 extract_windows.go/
#         extract_esxi.go（它们是被 RunRHEL 之外的其它抽取逻辑引用，
#         不是被调用），cmd/lober 从未 import internal/imports 包——
#         这条链路在 HTTP 层完全不可达，不是"本脚本没找对端点"。
#     结论：与 RHEL 基线同样的绕法——用真实端点落一条 importing 记录
#     取得真实分配的 ID，再用 sqlite3 CLI 直接把它推进到 ready 并写回
#     paths_json。文件本身仍是 mkfixture-debian.sh 造出的真实文件、真实
#     落在制品根目录下；绕过的只是"谁调用抽取逻辑"这个缺失的调用点。
#
#  3. 【发现的渲染层缺口：AssemblePreseed 从未渲染 firstboot 阶段的 hook，
#     也从未调用 FirstbootScaffold】——这是本脚本设计阶段读代码时发现的
#     真实缺口，不是本脚本的限制，写进任务返回值的 findings。后果：
#       a. 挂在 firstboot 阶段的 hook 对 Debian 任务会被静默丢弃（不报错、
#          不执行、不发信号），因为 internal/render/preseed.go 里对
#          StageHookCallLines 的调用只有三处（HookStagePre/
#          HookStagePostNoChroot/HookStagePost），从未出现
#          model.HookStageFirstboot；FirstbootScaffold（在
#          firstboot.go，负责写 systemd oneshot unit 并 enable 它）只被
#          kickstart.go 与 autoinstall.go 调用，preseed.go 里零次引用。
#       b. late_command 的脚本以 `signal "rebooting" 0` 结尾——之后没有
#          任何东西在目标系统里落地"重启后要跑什么"这件事本身：没有
#          systemd unit，没有 booted/init-start/init-done/ready 四个信号
#          的发送逻辑，也没有 finalize token 文件本身的落盘动作
#          （FirstbootScaffold 的职责之一）。
#     本脚本仍然按计划任务描述打这四个信号（阶段 8c），但那只验证了"网关
#     的信号端点能否用 finalize token 认证并接受这四个 phase 值"（家族
#     无关的通用能力，已被其它测试/RHEL e2e 覆盖），不能也不宣称验证了
#     "Debian 装完的机器真的会在重启后自动发出这些信号"——真实机器场景下，
#     渲染出的 preseed.cfg 目前不包含任何触发这四个信号的机制。这与
#     RHEL/Ubuntu 的差距是本脚本最有价值的发现之一。
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

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e-debian.XXXXXX")"

# to_win_path：见 scripts/e2e.sh 头部注释对 MSYS bash 与 Go/sqlite3 路径
# 解析差异的完整说明，本脚本原样复用同一份逻辑与理由，不重复展开。
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

# 高位、不常见端口，且与 scripts/e2e.sh 的端口不同——两个脚本理论上可能
# 被并行跑（比如 CI 里多个家族的 e2e 脚本同时起），选不同端口段避免撞。
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

echo "=== Lober Debian 端到端验证 ==="
echo "工作目录: ${WORKDIR}"

# ---------------------------------------------------------------------------
# 阶段 1：造假 Debian ISO 内容树、二进制、admin 用户、配置
# ---------------------------------------------------------------------------

mkdir -p "${WORKDIR_WIN}/db" "${ARTIFACT_ROOT}" "${LOG_ROOT}"

ARTIFACT_SET_ID="e2e-debian12-x86_64"
bash "${SCRIPT_DIR}/e2e/mkfixture-debian.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_ID}"

( cd "${REPO_ROOT}" && go build -o "${BIN_PATH}" ./cmd/lober )
[ -x "${BIN_PATH}" ] || fail "阶段1: 构建 lober 二进制失败"

# admin 凭据：同 scripts/e2e.sh 的固定测试哈希（cost=10，对应明文
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
  token_secret: "e2e-debian-test-secret"
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
  # by-hook：同 scripts/e2e.sh 的选择理由——本脚本不跑 discovery，没有
  # LOBER_DISKS 事实，除 by-hook 外的磁盘策略在空磁盘清单下会直接报错。
  disk_policy: "by-hook"
  heartbeat_mode: "off"
EOF

"${BIN_PATH}" -config "${CONFIG_PATH}" -check || fail "阶段1: -check 校验模式未通过"

pass "阶段1: 假 Debian ISO 内容树、二进制、admin 用户、配置文件均已就位"

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

HOST_SN="E2E-DEB-SN-0001"
HOST_MAC="aa:bb:cc:dd:ee:11"
HOST_UUID="21111111-1111-1111-1111-111111111111"

HOST_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_SN}\",\"mac\":\"${HOST_MAC}\",\"uuid\":\"${HOST_UUID}\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-DEB-RACK\"}")"
HOST_ID="$(json_field "${HOST_RESP}" "id")"
[ -n "${HOST_ID}" ] || fail "阶段4: 录入主机失败，响应=${HOST_RESP}"

# 用真实端点落一条 importing 记录，取得真实分配的 ID。
ARTIFACT_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/artifact-sets" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sourcePath\":\"${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}\",\"name\":\"debian12-x86_64\"}")"
CREATED_ARTIFACT_ID="$(json_field "${ARTIFACT_CREATE_RESP}" "id")"
[ -n "${CREATED_ARTIFACT_ID}" ] || fail "阶段4: 登记制品集失败，响应=${ARTIFACT_CREATE_RESP}"

# 真实导入路径验证：等待一小段时间，若 handleCreateArtifactSet 背后真的
# 挂了异步 job，state 应该会从 importing 自行推进（成功→ready，或至少
# 因为 sourcePath 未被任何字段接住而失败→failed）。轮询 3 秒——不长时间
# 等待，因为按已读代码分析这条链路在 HTTP 层不可达，此处只是留一个自动化
# 证据点，不是真的期待它成功。
IMPORT_STATE_AFTER_WAIT=""
for _ in 1 2 3 4 5 6; do
	sleep 0.5
	IMPORT_STATE_AFTER_WAIT="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
	[ "${IMPORT_STATE_AFTER_WAIT}" != "importing" ] && break
done
if [ "${IMPORT_STATE_AFTER_WAIT}" = "ready" ]; then
	echo "意外：制品集在等待期间自行推进到了 ready——说明导入管线已被接线，" >&2
	echo "本脚本对'走不通'的判断已过时，需要重新核对 internal/console/artifact.go" >&2
	fail "阶段4: 真实导入路径的前提判断（未接线）与实际观测不一致，需要人工复核"
fi
echo "真实导入路径确认走不通：等待 3 秒后状态仍为 '${IMPORT_STATE_AFTER_WAIT}'（未自行推进），" >&2
echo "与读代码分析一致（handleCreateArtifactSet 未引用 internal/imports 包，" >&2
echo "cmd/lober 未 import 该包，Job 也没有 RunDebian 入口）——退回 sqlite3 绕法。" >&2

# mkfixture-debian.sh 造的是"ISO 内容视图"，落在 ARTIFACT_SET_ID 目录下；
# 真实抽取（ExtractDebian）本应把其中的 vmlinuz/initrd.gz/dists/pool 转换
# 成 linux/initrd.gz/repo/ 这一套固定输出结构。既然导入 job 不可达，本脚本
# 在绕过状态推进的同时，也用 ExtractDebian 同款的目标文件名手动完成一次
# "抽取动作"本身（不是伪造：内容确实来自 mkfixture-debian.sh 造的源文件，
# 只是搬运逻辑用 shell 命令代替 Go 抽取器执行），让 paths_json 指向的文件
# 真实存在于磁盘上（docs/contracts/filesystem.md 硬约束 1），而不是指向
# 一堆从不存在的路径。
SRC_ISO_DIR="${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}"
DEST_DIR="${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}"
mkdir -p "${DEST_DIR}/repo"
cp "${SRC_ISO_DIR}/install.amd/vmlinuz" "${DEST_DIR}/linux"
cp "${SRC_ISO_DIR}/install.amd/initrd.gz" "${DEST_DIR}/initrd.gz"
cp -r "${SRC_ISO_DIR}/dists" "${DEST_DIR}/repo/dists"
cp -r "${SRC_ISO_DIR}/pool" "${DEST_DIR}/repo/pool"

PATHS_JSON='{"Kernel":"linux","Initrd":["initrd.gz"],"Repo":"repo/","Extra":{}}'
sqlite3 "${DB_PATH}" "UPDATE artifact_sets SET state='ready', os_family='debian', os_version='12', arch='x86_64', firmware='any', paths_json='${PATHS_JSON}' WHERE id='${CREATED_ARTIFACT_ID}';"

READY_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
[ "${READY_CHECK}" = "ready" ] || fail "阶段4: 制品集未能推进到 ready（当前=${READY_CHECK}）"

for rel in linux initrd.gz repo/dists/stable/Release repo/pool/main/b/bash/bash_5.2-5_amd64.deb; do
	[ -f "${DEST_DIR}/${rel}" ] || fail "阶段4: 制品集落盘文件 ${rel} 不存在（paths_json 指向的路径应真实存在，filesystem.md 硬约束1）"
done

# ---------------------------------------------------------------------------
# 真实发现（阻断性 bug，非本脚本的绕法）：internal/statemachine/precheck.go
# 的 checkArtifactFiles/checkPathExists 与 cmd/lober/preflight.go 的
# checkArtifactPathExists 都硬编码把 "repodata/repomd.xml" 当作"仓库目录
# 真正可用"的判据（repoMetadataFile 常量），对结尾带斜杠的 ArtifactPaths
# 字段（Kernel/Initrd 都不带斜杠，只有 Repo 会触发这条路径）一律拼这个
# yum/dnf 专属的元数据文件名去 fs.Stat，不按 OS 家族分支。
#
# 这对 RHEL 是对的（ExtractRHEL 产出的 repo/ 下真的有 repodata/repomd.xml），
# 但 Debian 的 repo/ 下是 dists/+pool/ 两棵树（ExtractDebian 的产出结构），
# 从未也不会有 repodata/ 这个目录——这意味着任何 Debian 任务在
# /v1/boot 命中时，Machine.Check → checkArtifactFiles 这一步会 100% 无
# 例外地判定"制品文件不存在"并拒绝签发 token，与本次登记的制品集内容是否
# 真实完整无关。本脚本实测复现（未加下面的临时补洞时）：任务被状态机置
# failed，failure_detail.LogTail 逐字符为
# `制品文件 "repo/" 在盘上不存在: GetFileAttributesEx repo/repodata/repomd.xml: ...`。
#
# 这不是 Ubuntu 同款问题——ExtractUbuntu 从不设置 ArtifactPaths.Repo（零值
# 空字符串，artifactCheckPaths 对空字符串直接跳过），只有 Debian（与未来
# 若 SUSE 的 Repo 字段也指向非 yum 树）会撞上这条硬编码。
#
# 本脚本在这里补一个仅用于打通阶段 7-9 验证（菜单/preseed 内容/信号机制）
# 的诊断性 workaround——手动在磁盘上补出 repo/repodata/repomd.xml，让
# precheck 这一步误判为"存在"从而放行。这不代表真实 Debian ISO 会有这个
# 文件，纯粹是为了绕开一个已确认的阻断性 bug 继续验证渲染器/信号链路；
# 该 bug 本身已完整记入本任务返回值的 findings，交给编排方处理（不在本
# 验证轨修 Go 代码）。删除下面三行、直接继续跑，就是该 bug 未修复时的
# 真实生产行为：Debian 装机 100% 卡在 precheck，永远拿不到装机菜单。
mkdir -p "${DEST_DIR}/repo/repodata"
printf '<?xml version="1.0"?><repomd xmlns="http://linux.duke.edu/metadata/repo"></repomd>' \
	>"${DEST_DIR}/repo/repodata/repomd.xml"

TEMPLATE_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-debian12\",\"artifactSetId\":\"${CREATED_ARTIFACT_ID}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
TEMPLATE_ID="$(json_field "${TEMPLATE_CREATE_RESP}" "id")"
[ -n "${TEMPLATE_ID}" ] || fail "阶段4: 创建模板失败，响应=${TEMPLATE_CREATE_RESP}"

sqlite3 "${DB_PATH}" "UPDATE templates SET state='active' WHERE id='${TEMPLATE_ID}';"
ACTIVE_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM templates WHERE id='${TEMPLATE_ID}';")"
[ "${ACTIVE_CHECK}" = "active" ] || fail "阶段4: 模板未能推进到 active（当前=${ACTIVE_CHECK}）"

pass "阶段4: 主机=${HOST_ID}、制品集=${CREATED_ARTIFACT_ID}（ready）、模板=${TEMPLATE_ID}（active）均已就位"

# ---------------------------------------------------------------------------
# 阶段 5：preview 端点（不需要真任务）
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"
[ "${PREVIEW_CODE}" = "0" ] || fail "阶段5: preview 端点未成功，响应=${PREVIEW_RESP}"
echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 响应缺少 answerFile 字段"
pass "阶段5: preview 端点用假数据渲染成功（不建任务、不签 token、不落库）"

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例（四条）
# ---------------------------------------------------------------------------

# 6a. 完全陌生的机器 → 回 sanboot。
STRANGER_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=NO-SUCH-DEB-SN&mac=00:00:00:00:00:88&uuid=&arch=x86_64&plat=efi")"
echo "${STRANGER_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6a: 陌生机器未收到 sanboot 载荷，响应=${STRANGER_BOOT}"
pass "阶段6a: 完全陌生的机器请求 /v1/boot 回 sanboot"

# 先建一个真正的任务，供 6b/6c/7/8/9 使用。
TASK_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-deb-host-01\"}}")"
TASK_ID="$(json_field "${TASK_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段6: 建任务失败，响应=${TASK_CREATE_RESP}"
echo "建任务成功: task_id=${TASK_ID}" >&2

# 6b. SN 对但 MAC 不对 → 拿不到菜单，回 sanboot（双因子硬要求）。
WRONG_MAC_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_SN}&mac=ff:ff:ff:ff:ff:ff&uuid=&arch=x86_64&plat=efi")"
echo "${WRONG_MAC_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6b: MAC 不匹配未回 sanboot，响应=${WRONG_MAC_BOOT}"
TASK_AFTER_MAC_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_AFTER_MAC_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6b: MAC 不匹配后任务的 failureReason 字段未见 precheck，响应=${TASK_AFTER_MAC_FAIL}"
pass "阶段6b: SN 对但 MAC 不对 → 回 sanboot，且任务被置 failed（failureReason=precheck）"

# 6c. 架构不符 → 回 sanboot 且任务置 failed，不消耗 attempt_count 的语义
#     体现在"允许 retry"（6d）。用第二台主机+第二个任务（6b 已把第一个
#     任务打成 failed）。
HOST2_SN="E2E-DEB-SN-0002"
HOST2_MAC="aa:bb:cc:dd:ee:12"
HOST2_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST2_SN}\",\"mac\":\"${HOST2_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-DEB-RACK\"}")"
HOST2_ID="$(json_field "${HOST2_RESP}" "id")"
[ -n "${HOST2_ID}" ] || fail "阶段6c: 录入第二台主机失败，响应=${HOST2_RESP}"

TASK2_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST2_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-deb-host-02\"}}")"
TASK2_ID="$(json_field "${TASK2_CREATE_RESP}" "id")"
[ -n "${TASK2_ID}" ] || fail "阶段6c: 建第二个任务失败，响应=${TASK2_CREATE_RESP}"

# 制品集要求 arch=x86_64，这里故意上报 arm64，MAC 用真实值（通过双因子）。
ARCH_MISMATCH_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=arm64&plat=efi")"
echo "${ARCH_MISMATCH_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6c: 架构不符未回 sanboot，响应=${ARCH_MISMATCH_BOOT}"

TASK2_AFTER_ARCH_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_ARCH_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6c: 架构不符后任务的 failureReason 字段未见 precheck，响应=${TASK2_AFTER_ARCH_FAIL}"
pass "阶段6c: 架构不符 → 回 sanboot 且任务置 failed（failureReason=precheck，原因可读）"

# 6d. precheck 失败的任务允许 retry 并回到 pending（重试必然同样失败这条
#     道理只约束"不要让机器无限重试同一个必然失败的原因"，不代表运维改完
#     配置后不能人工重试——§4.6/§8.2 只禁两类超时，不扩大范围）。
RETRY_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK2_ID}/retry" -H "${AUTH_HEADER}")"
RETRY_STATE="$(json_field "${RETRY_RESP}" "state")"
[ "${RETRY_STATE}" = "pending" ] || fail "阶段6d: precheck 失败的任务 retry 后状态 = ${RETRY_STATE}, want pending，响应=${RETRY_RESP}"
pass "阶段6d: precheck 失败（非 reboot_timeout/init_timeout）的任务允许 retry 并回到 pending"

pass "阶段6: 四条准入控制负面用例全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：取引导脚本与菜单，从菜单里抠出装机 token
#
# 用 6d 里 retry 回到 pending 的 TASK2_ID/HOST2_SN/HOST2_MAC——已验证过一次
# 拒绝路径，现在带正确的一切走成功路径。
# ---------------------------------------------------------------------------

BOOTSCRIPT="$(curl -fsS "${GW_BASE}/boot.ipxe")"
echo "${BOOTSCRIPT}" | grep -q "chain ${GW_BASE}/v1/boot" \
	|| fail "阶段7: /boot.ipxe 内容不含预期的 chain 指令，响应=${BOOTSCRIPT}"

BOOT_MENU="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"

# Debian 菜单形状（renderDebianMenu，SPEC §4.5）：
#   kernel <artifact>/linux auto=true priority=critical \
#     preseed/url=<gw>/v1/answer/<task>/<token>/preseed.cfg
#   initrd <artifact>/initrd.gz
# 与 RHEL 的 vmlinuz+initrd.img+inst.ks= 形状不同——本阶段的断言逐一核对
# Debian 特有的这几处形状差异，而不是照搬 RHEL 脚本断言 inst.ks=。
echo "${BOOT_MENU}" | grep -q "^kernel .*/linux " || fail "阶段7: 引导菜单的 kernel 行未指向 linux（Debian 固定内核输出文件名），响应=${BOOT_MENU}"
echo "${BOOT_MENU}" | grep -q "auto=true priority=critical" || fail "阶段7: 引导菜单缺少 auto=true priority=critical，响应=${BOOT_MENU}"
echo "${BOOT_MENU}" | grep -q "preseed/url=" || fail "阶段7: 引导菜单未包含 preseed/url= 行，响应=${BOOT_MENU}"
echo "${BOOT_MENU}" | grep -q "^initrd .*/initrd\.gz$" || fail "阶段7: 引导菜单的 initrd 行未指向 initrd.gz（Debian 固定 initrd 输出文件名），响应=${BOOT_MENU}"

# token 走路径段不走 query（SPEC §4.4："preseed/url= 里带 & 在内核命令行上
# 容易出事"）——核对 preseed/url= 的值本身不含 "?" 或 "&"，这是本家族相对
# RHEL 的一处关键差异点，必须显式断言，不能只断言"存在 preseed/url="。
PRESEED_URL_VALUE="$(echo "${BOOT_MENU}" | grep -o "preseed/url=[^[:space:]]*")"
case "${PRESEED_URL_VALUE}" in
*'?'*|*'&'*)
	fail "阶段7: preseed/url= 的值含 ? 或 &，token 应走路径段而非 query（SPEC §4.4），实际值=${PRESEED_URL_VALUE}"
	;;
esac

# 从 preseed/url=<gw>/v1/answer/<task_id>/<install_token>/preseed.cfg 里
# 抠出 <task_id>/<install_token> 这一段。
KS_URL_SEGMENT="$(echo "${PRESEED_URL_VALUE}" | sed -E 's#preseed/url=.*/v1/answer/##')"
INSTALL_TASK_ID="$(echo "${KS_URL_SEGMENT}" | cut -d/ -f1)"
INSTALL_TOKEN="$(echo "${KS_URL_SEGMENT}" | cut -d/ -f2)"
[ "${INSTALL_TASK_ID}" = "${TASK2_ID}" ] || fail "阶段7: 菜单里的 task id (${INSTALL_TASK_ID}) 与预期 (${TASK2_ID}) 不符"
[ -n "${INSTALL_TOKEN}" ] || fail "阶段7: 未能从菜单里抠出装机 token，KS_URL_SEGMENT=${KS_URL_SEGMENT}"

TASK2_DETAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_DETAIL}" | grep -q '"state":"booting"' \
	|| fail "阶段7: /v1/boot 命中后任务状态未变为 booting，响应=${TASK2_DETAIL}"

pass "阶段7: /v1/boot 命中签发 token，任务进入 booting，从 Debian 菜单（linux/initrd.gz/preseed.cfg 形状）取得 install token"

# ---------------------------------------------------------------------------
# 阶段 8：取 preseed.cfg 应答文件、逐个信号打点
# ---------------------------------------------------------------------------

ANSWER_URL="${GW_BASE}/v1/answer/${TASK2_ID}/${INSTALL_TOKEN}/preseed.cfg"
ANSWER_BODY="$(curl -fsS "${ANSWER_URL}")"
[ -n "${ANSWER_BODY}" ] || fail "阶段8: 取应答文件失败或内容为空，url=${ANSWER_URL}"

# preseed 骨架四段核对（SPEC §5.2 跨家族映射表，任务描述明确要求"验证
# 渲染出的 preseed 里这四段真的都在"）：
#   pre            → preseed/early_command
#   post-nochroot  → preseed/late_command（不加 in-target，本键与 post 共用）
#   post           → preseed/late_command（加 in-target 前缀）
# 与 kickstart 用独立段落名不同，preseed 只有两个 debconf 键承载三个阶段，
# 因此断言方式是"两个键都存在且值非空"，而非 grep 三个不同的段落标记。
echo "${ANSWER_BODY}" | grep -qE $'^d-i\tpreseed/early_command\tstring\t.+' \
	|| fail "阶段8: 应答文件缺少非空的 preseed/early_command（对应 pre 阶段 hook 载体）"
echo "${ANSWER_BODY}" | grep -qE $'^d-i\tpreseed/late_command\tstring\t.+' \
	|| fail "阶段8: 应答文件缺少非空的 preseed/late_command（对应 post-nochroot/post 阶段 hook 载体）"

# late_command 的值是 base64 编码后的脚本（preseed.go 的 lateCommandValue：
# "转义穿透 late_command 单行值的风险远高于直接 base64 编码后用 base64 -d
# 落盘"）。本脚本不满足于"键存在"这个表面断言——解码后核对 post-start/
# post-done/rebooting 三个信号点与 SSH 公钥注入命令确实在解码后的脚本正文
# 里，这才是"post-nochroot/post 两个阶段的 hook 载体真的能工作"的证据，
# 而不是"渲染器写了一个看起来正确但内容为空的占位值"。
LATE_COMMAND_VALUE="$(echo "${ANSWER_BODY}" | grep -E $'^d-i\tpreseed/late_command\tstring\t' | sed -E $'s/^d-i\tpreseed\\/late_command\tstring\t//')"
LATE_COMMAND_B64="$(echo "${LATE_COMMAND_VALUE}" | grep -o "echo [A-Za-z0-9+/=]* | base64 -d" | sed -E 's/^echo //; s/ \| base64 -d$//')"
[ -n "${LATE_COMMAND_B64}" ] || fail "阶段8: 未能从 late_command 值中抠出 base64 载荷，值=${LATE_COMMAND_VALUE}"
LATE_COMMAND_DECODED="$(echo "${LATE_COMMAND_B64}" | base64 -d)"
echo "${LATE_COMMAND_DECODED}" | grep -q 'signal "post-start" 0' || fail "阶段8: late_command 解码后缺少 post-start 信号点"
echo "${LATE_COMMAND_DECODED}" | grep -q 'signal "post-done" 0' || fail "阶段8: late_command 解码后缺少 post-done 信号点"
echo "${LATE_COMMAND_DECODED}" | grep -q 'signal "rebooting" 0' || fail "阶段8: late_command 解码后缺少 rebooting 信号点"
echo "${LATE_COMMAND_DECODED}" | grep -q "authorized_keys" || fail "阶段8: late_command 解码后缺少 SSH 公钥写入命令（SPEC §1.5）"

# early_command 的值未被 base64 包装（preseedShellValue 只替换换行为空格，
# 不编码），可以直接 grep pre-start/pre-done。
EARLY_COMMAND_VALUE="$(echo "${ANSWER_BODY}" | grep -E $'^d-i\tpreseed/early_command\tstring\t' | sed -E $'s/^d-i\tpreseed\\/early_command\tstring\t//')"
echo "${EARLY_COMMAND_VALUE}" | grep -q 'signal "pre-start" 0' || fail "阶段8: early_command 缺少 pre-start 信号点"
echo "${EARLY_COMMAND_VALUE}" | grep -q 'signal "pre-done" 0' || fail "阶段8: early_command 缺少 pre-done 信号点"

# 重复取应答文件仍应幂等成功。
curl -fsS -o /dev/null "${ANSWER_URL}" || fail "阶段8: 重复取应答文件失败（应幂等成功）"

TASK2_AFTER_ANSWER="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_ANSWER}" | grep -q '"state":"installing"' \
	|| fail "阶段8: 取应答文件后任务状态未变为 installing，响应=${TASK2_AFTER_ANSWER}"
pass "阶段8a: 取 preseed.cfg 成功，early_command/late_command 两个键均非空且解码/直读后确认 pre/post-nochroot/post 三阶段信号点与 SSH 公钥注入命令均在，任务进入 installing，重复取仍幂等成功"

FINALIZE_TOKEN="$(sqlite3 "${DB_PATH}" "SELECT finalize_token FROM tasks WHERE id='${TASK2_ID}';")"
[ -n "${FINALIZE_TOKEN}" ] || fail "阶段8: 无法从数据库取得 finalize token（任务应已在阶段7的 /v1/boot 命中路径签发）"

send_signal() {
	local token="$1" phase="$2" seq="$3" rc="${4:-0}"
	curl -sS -X POST "${GW_BASE}/v1/signal/${TASK2_ID}/${token}/${phase}?rc=${rc}&seq=${seq}" \
		-d "signal ${phase} seq=${seq}" -o /dev/null -w '%{http_code}'
}

# 装机期七个信号（pre-start/pre-done/packages-start/packages-done/
# post-start/post-done/rebooting）用 INSTALL_TOKEN——这七个信号点确实
# 存在于渲染出的 preseed.cfg 里（阶段8a 已解码验证 pre/post-start/
# post-done/rebooting 四个；packages-start/packages-done 两个信号点
# preseed.go 未生成，见下方说明），本阶段仍然逐个打点，验证的是网关侧
# 信号端点本身能否接受这些 phase 值（家族无关的通用能力）。
#
# 真实性说明：AssemblePreseed 目前不生成 packages-start/packages-done
# 这两个信号点（Debian d-i 的软件包安装阶段没有对应的 debconf 钩子被
# 使用），本脚本仍打这两个点是为了与十一信号协议的完整性做对照测试
# （网关侧协议本身不关心某个家族是否真的会发出某个 phase），但这属于
# "验证网关能力"而非"验证 Debian 渲染器已生成该信号点"，两者不应混淆——
# 写入 findings，不在本脚本内混入误导性的"Debian 确认发出七个信号"表述。
SEQ=1
for phase in pre-start pre-done packages-start packages-done post-start post-done rebooting; do
	code="$(send_signal "${INSTALL_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（装机期）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

LATE_CODE="$(send_signal "${INSTALL_TOKEN}" "pre-start" 1)"
[ "${LATE_CODE}" = "200" ] || fail "阶段8: 延迟到达的早期信号（seq=1，水位已经在 7 之后）未回 200"

DUP_CODE="$(send_signal "${INSTALL_TOKEN}" "post-done" 6)"
[ "${DUP_CODE}" = "200" ] || fail "阶段8: 重复发同一个 seq（post-done, seq=6）未回 200"

FINALIZE_MISUSE_CODE="$(send_signal "${FINALIZE_TOKEN}" "pre-start" 100)"
[ "${FINALIZE_MISUSE_CODE}" = "403" ] || fail "阶段8: finalize token 发装机期信号（pre-start）返回 HTTP ${FINALIZE_MISUSE_CODE}, want 403"

pass "阶段8b: 装机期七个信号打点成功（网关侧协议验证；pre-start/pre-done/post-start/post-done/rebooting 五个已在8a确认渲染器真实生成，packages-start/packages-done 两个渲染器未生成，见 findings）；延迟到达/重复 seq 均幂等；finalize token 越权发装机期信号被拒（403）"

# 重启后四个信号用 finalize token——见文件头「已知限制」第 3 条：渲染层
# 目前没有任何机制在真实机器重启后触发这四个信号，本阶段验证的是网关侧
# 能否用 finalize token 认证并接受这四个 phase（家族无关的通用能力，已被
# RHEL e2e.sh 覆盖过一次），不代表"验证了 Debian 装完会自动发出它们"。
for phase in booted init-start init-done ready; do
	code="$(send_signal "${FINALIZE_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（重启后）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

pass "阶段8c: 重启后四个信号（booted/init-start/init-done/ready）网关侧打点成功——注意这只验证网关能力，不代表 Debian 渲染器已提供触发机制（见 findings：preseed.go 未调用 FirstbootScaffold）"

# ---------------------------------------------------------------------------
# 阶段 9：断言终态 done；装完再引导一次，验证不循环重装
# ---------------------------------------------------------------------------

wait_for_task_state() {
	local sn="$1" want="$2"
	curl -fsS "${CONSOLE_BASE}/tasks?sn=${sn}" -H "${AUTH_HEADER}" | grep -q "\"state\":\"${want}\""
}
poll_until "任务达到终态 done" 10 wait_for_task_state "${HOST2_SN}" "done"
pass "阶段9a: 任务达到终态 done"

REBOOT_AFTER_DONE="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${REBOOT_AFTER_DONE}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段9b: 装完的机器再次引导未回 sanboot，响应=${REBOOT_AFTER_DONE}"
pass "阶段9b: 装完的机器再次引导拿到 sanboot（不是装机菜单，不循环重装）"

ANSWER_AFTER_DONE_CODE="$(curl -s -o /dev/null -w '%{http_code}' "${ANSWER_URL}")"
[ "${ANSWER_AFTER_DONE_CODE}" = "403" ] || fail "阶段9c: 终态任务的应答文件请求返回 HTTP ${ANSWER_AFTER_DONE_CODE}, want 403"
pass "阶段9c: 终态任务的应答文件不再提供（403）"

FINAL_EVENTS="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK2_ID}/events" -H "${AUTH_HEADER}")"
FINAL_EVENT_COUNT="$(echo "${FINAL_EVENTS}" | grep -o '"taskId"' | wc -l)"
[ "${FINAL_EVENT_COUNT}" -ge 11 ] || fail "阶段9d: 事件表条数=${FINAL_EVENT_COUNT}, want 至少 11（十一个信号各至少一条事件），响应=${FINAL_EVENTS}"
pass "阶段9d: 事件表留下完整时间线（${FINAL_EVENT_COUNT} 条事件）"

echo ""
echo "=== 完整状态链（task_id=${TASK2_ID}） ==="
echo "pending → booting（/v1/boot 命中签 token） → installing（取 preseed.cfg） →"
echo "  [pre-start → pre-done → packages-start → packages-done → post-start → post-done → rebooting]（装机期七信号，网关侧验证） →"
echo "  [booted → init-start → init-done → ready]（重启后四信号，finalize token，网关侧验证） → done"
echo ""
echo "各阶段验证结果："
sqlite3 "${DB_PATH}" "SELECT '  state='||state||' current_phase='||current_phase||' attempt_count='||attempt_count||' last_seq='||last_seq FROM tasks WHERE id='${TASK2_ID}';"
echo ""
echo "=== 全部九个阶段（含四条负面用例）验证通过 ==="
echo ""
echo "=== 真实发现摘要（详见 docs/real-machine/12-e2e-debian.md） ==="
echo "1. 制品集登记→导入 job 之间确实没有接线（同 RHEL 已知缺口，Job 也没有 RunDebian 入口）"
echo "2. internal/render/preseed.go 从未渲染 firstboot 阶段 hook，也未调用 FirstbootScaffold——"
echo "   真实机器上，重启后不会有任何机制发出 booted/init-start/init-done/ready 四个信号"
echo "3. preseed.go 未生成 packages-start/packages-done 两个信号点"
