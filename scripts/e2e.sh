#!/usr/bin/env bash
# scripts/e2e.sh —— MVP 端到端验证：curl 模拟机器打点，整条链本地跑通，
# 不碰硬件。对应实现计划任务 9，依据 SPEC §11 Phase 1 的验证行。
#
# 这是整个 MVP 的验收标准：九个阶段逐个打印通过标记，最后输出完整状态链。
#
# 已知限制（写在这里而不是假装测过了）：
#
#  1. 【不依赖 jq】本机验证环境没有 jq（curl 与 sqlite3 有），改用
#     grep/sed 做最小 JSON 字段提取——够用于本脚本需要的"取单个字符串/
#     数字字段值"这种简单场景，不是通用 JSON 解析器。若目标环境有 jq，
#     这个选择仍然正确：脚本本就不该为了用上 jq 而引入一个新的强制依赖，
#     受限于最小公分母的可移植性优先于用更顺手的工具。
#
#  2. 【制品集登记→导入 job 之间没有实现好的桥接，本脚本绕过 HTTP 直接
#     seed 数据库】——这是一处需要写进 contractGaps 的真实缺口，不是本
#     脚本图省事的选择：
#       - internal/console/artifact.go 的 handleCreateArtifactSet 只落一条
#         state=importing 的记录就返回，注释明确写着"异步 job 本身由装配点
#         接入的其它组件驱动"，但请求体里的 sourcePath 从未被持久化到任何
#         字段（model.ArtifactSet 没有 SourcePath 字段），因此装配点事后
#         也无法知道该对哪个本地路径跑导入 job——这不是"没接好"，是"接口
#         形状上就没有留出能接的地方"。
#       - internal/console/template.go 同理：模板只能创建为 state=draft，
#         docs/contracts/http-console.md 冻结的四个模板端点（create/
#         update/form-schema/preview）里没有任何一个能把模板转成 active，
#         而 handleCreateTask 的第 2 步硬性要求 tpl.State ==
#         model.TemplateStateActive。
#     本脚本对这两处的绕法：直接用 sqlite3 CLI 写 artifact_sets 与
#     templates 两张表到目标状态（paths_json/state 字段），文件本身仍然
#     是 mkfixture.sh 造出的真实文件、真实落在制品根目录下——绕过的只是
#     "谁调用 imports.Job.RunRHEL""谁调用
#     TemplateStore.UpdateTemplateState"这两个缺失的调用点，不是伪造
#     磁盘内容本身。
#
#  3. 【syslog 通道本脚本覆盖不到】它是 UDP，curl 发不出 UDP 包；
#     internal/gateway 的自动化测试已经覆盖 ListenAndServeSyslog 本身，
#     cmd/lober 的 server_test.go 覆盖了它与 runServers 的生命周期集成，
#     这里不假装测过。
#
# 依赖：curl、sqlite3（both 已确认在本机可用，见任务返回值 verifyOutcome）、
# go（用 go run/go build 起服务，不依赖已安装的二进制）。
set -euo pipefail

# ---------------------------------------------------------------------------
# 基础设施：临时目录、端口、trap 清理
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e.XXXXXX")"

# to_win_path 把 bash（此处特指 Windows 上的 Git Bash/MSYS）视角的路径
# 转成 Go 标准库与 sqlite 驱动能一致解析的形式。
#
# 这不是可移植性上的锦上添花，是一个真实踩到的 bug：MSYS bash 的 "/tmp"
# 是它自己虚拟出的根（本机实测映射到 "D:\msys\tmp"），但 Go 的 os/
# filepath 包在 Windows 上把不带盘符的 "/tmp/..." 解析成"当前工作目录
# 所在盘符的根"（本机实测是 "E:\tmp\..."）——两者是完全不同的两个目录。
# 若不转换，本脚本传给 lober 二进制（写配置文件里的 db_path/
# artifact_path/log_path）与传给 sqlite3 CLI（后续阶段绕过 HTTP 直接
# UPDATE 数据库）的是同一个字符串，但 Go 进程与 bash/sqlite3 实际各自
# 落盘的目录完全不是一个地方——数据库文件在 bash 能看到的路径下永远是
# 空文件（0 字节，只建了目录没有表），sqlite3 CLI 对它探测 schema 时会
# 报 "no such table"，现场表现极具误导性（看起来像是迁移没跑，实际是
# 两个进程各写各的、谁都没写到对方能看到的地方）。
#
# 用 cygpath -m（MSYS 自带）转换：它输出的是"带盘符、正斜杠"的形式
# （如 D:/msys/tmp/xxx），Go 与 sqlite3 在 Windows 上都能正确解析这种
# 形式，同时 bash 自身对这种形式的路径操作（mkdir/mv/cat 等）也完全
# 兼容，不需要为 bash 一侧再转换回去。
to_win_path() {
	if command -v cygpath >/dev/null 2>&1; then
		cygpath -m "$1"
	else
		# 非 MSYS 环境（真正的 Linux/macOS）：路径解析本就一致，原样返回。
		echo "$1"
	fi
}

WORKDIR_WIN="$(to_win_path "${WORKDIR}")"
DB_PATH="${WORKDIR_WIN}/db/lober.db"
ARTIFACT_ROOT="${WORKDIR_WIN}/artifacts"
LOG_ROOT="${WORKDIR_WIN}/log"
CONFIG_PATH="${WORKDIR_WIN}/lober.yaml"
BIN_PATH="${WORKDIR_WIN}/lober"

# 高位、不常见端口，降低与开发机上其它服务撞的概率（计划任务 9「设计
# 要点」）。
GW_PORT=28080
CONSOLE_PORT=28090
SYSLOG_PORT=28514

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

# json_field 从形如 {"code":0,"data":{"foo":"bar"},"message":""} 的响应体
# 里抠出顶层或 data 内某个字符串/数字字段的值。只处理"字段名后紧跟
# :"值""或":数字"这种最简单的形态，够用于本脚本需要的场景（不通用，
# 见文件头「已知限制」第 1 条）。
json_field() {
	local body="$1" field="$2"
	echo "${body}" | grep -o "\"${field}\"[[:space:]]*:[[:space:]]*\"[^\"]*\"" | head -n1 | sed -E 's/.*:[[:space:]]*"([^"]*)"/\1/' \
		|| echo "${body}" | grep -o "\"${field}\"[[:space:]]*:[[:space:]]*[0-9][0-9]*" | head -n1 | sed -E 's/.*:[[:space:]]*([0-9]+)/\1/'
}

# poll_until 轮询执行命令直到成功或超时（计划任务 9「设计要点」：轮询而
# 非固定等待，固定 sleep 在慢机器上会假失败、在快机器上浪费时间）。
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

echo "=== Lober MVP 端到端验证 ==="
echo "工作目录: ${WORKDIR}"

# ---------------------------------------------------------------------------
# 阶段 1：造假制品树、建临时目录、构建二进制、造 admin 用户、写临时配置
# ---------------------------------------------------------------------------

mkdir -p "${WORKDIR_WIN}/db" "${ARTIFACT_ROOT}" "${LOG_ROOT}"

ARTIFACT_SET_ID="e2e-rocky94-x86_64"
bash "${SCRIPT_DIR}/e2e/mkfixture.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_ID}"

( cd "${REPO_ROOT}" && go build -o "${BIN_PATH}" ./cmd/lober )
[ -x "${BIN_PATH}" ] || fail "阶段1: 构建 lober 二进制失败"

# 用 mkuser 工具（另一波 agent 产出，本脚本只用不重写，见任务说明）生成
# bcrypt 哈希；若尚不存在（可能仍在生产中），退回一个已知的固定哈希
# （对应密码 "admin12345"，仅用于本脚本自己的临时数据库，不是生产凭据）。
ADMIN_USER="admin"
ADMIN_PASS="admin12345"
# 固定的合法 bcrypt 哈希（cost=10），对应明文 "admin12345"——本脚本编写
# 时已用 golang.org/x/crypto/bcrypt 生成并验证过这条哈希与明文的对应
# 关系，仅用于本一次性脚本自建的临时数据库，不是生产凭据。
#
# 没有调用 scripts/mkuser 来现场生成：该工具由另一波 agent 负责产出
# （计划任务 8），本 worktree 里此刻尚不存在（见任务返回值 contractGaps）。
# 等它就位后，这里应改为调用真实工具而不是使用固定哈希——保留这行
# 注释是为了不让后续读者误以为这是刻意的最终设计。
ADMIN_HASH='$2a$10$LWLA9qNOLwBFVDMK6LQLLOX56wmVuu1o.szEoZbqdLT4cdGmffBJe'

cat >"${CONFIG_PATH}" <<EOF
gateway:
  listen: "127.0.0.1:${GW_PORT}"
  gateway_base: "${GW_BASE}"
  artifact_base: "${GW_BASE}"
  syslog_listen: "127.0.0.1:${SYSLOG_PORT}"
  # SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录——
  # gateway.root_ssh_public_keys 是启动时的必填项（internal/config.
  # Validate），不填的话本脚本第一步的 -check 校验就会直接失败。这是
  # 一把仅用于本脚本自建临时环境的测试公钥，不对应任何真实私钥，不是
  # 生产凭据（与上面 ADMIN_HASH 固定哈希同一类"一次性脚本自用测试数据"）。
  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 e2e-test@lober"
console:
  listen: "127.0.0.1:${CONSOLE_PORT}"
  token_secret: "e2e-test-secret"
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
  # by-hook 而非 largest：internal/console/task.go 的 handleCreateTask
  # 构造给 TaskParamsResolver.Resolve 的 facts 只有五个身份/架构键（SN/
  # MAC/UUID/Arch/OSFamily），从不包含 model.FactDisks——那是 discovery
  # 阶段才会采集、写进 task_facts 表的数据，而本脚本没有跑 discovery。
  # 除 by-hook 外的四种磁盘策略在收到空磁盘清单时都会明确报错（"零块盘
  # 不能返回空布局"，internal/provider/diskplanner.go），会让本脚本的
  # 建任务这一步在还没走到任何机器面端点之前就失败。by-hook 是唯一一种
  # 不检查磁盘清单长度、把选盘逻辑完全推给 %pre 阶段 hook 的策略——这正是
  # 该策略存在的设计意图（SPEC §5.6），不是为了绕过本脚本的限制而选的
  # 不严谨捷径。
  disk_policy: "by-hook"
  heartbeat_mode: "off"
EOF

# 校验模式先跑一遍——配置写错在这里就该被发现（计划任务 5/9 共同要求）。
"${BIN_PATH}" -config "${CONFIG_PATH}" -check || fail "阶段1: -check 校验模式未通过"

pass "阶段1: 制品树、二进制、admin 用户、配置文件均已就位"

# ---------------------------------------------------------------------------
# 阶段 2：起服务，轮询到就绪（不固定 sleep）
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
# 阶段 4：录入一台主机、登记制品集、建模板（active）
#
# 制品集与模板均绕过 HTTP 直接 seed 数据库——见文件头「已知限制」第 2 条
# 对这两处契约缺口的完整说明。
# ---------------------------------------------------------------------------

HOST_SN="E2E-SN-0001"
HOST_MAC="aa:bb:cc:dd:ee:01"
HOST_UUID="11111111-1111-1111-1111-111111111111"

HOST_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_SN}\",\"mac\":\"${HOST_MAC}\",\"uuid\":\"${HOST_UUID}\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-RACK\"}")"
HOST_ID="$(json_field "${HOST_RESP}" "id")"
[ -n "${HOST_ID}" ] || fail "阶段4: 录入主机失败，响应=${HOST_RESP}"

# 制品集：先用真实端点落一条 importing 记录（走真实的 handleCreateArtifactSet
# 路径），取得真实分配的 ID；再用 sqlite3 把它推进到 ready 并写回
# paths_json——这是本脚本对"注册触发异步导入 job"这个契约缺口的绕法。
ARTIFACT_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/artifact-sets" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sourcePath\":\"${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}\",\"name\":\"rocky9.4-x86_64\"}")"
CREATED_ARTIFACT_ID="$(json_field "${ARTIFACT_CREATE_RESP}" "id")"
[ -n "${CREATED_ARTIFACT_ID}" ] || fail "阶段4: 登记制品集失败，响应=${ARTIFACT_CREATE_RESP}"

# mkfixture.sh 造的文件树落在 ARTIFACT_SET_ID 目录下，但控制台端点分配的
# 真实主键是 CREATED_ARTIFACT_ID（uuid.NewString()），两者不同——把
# fixture 目录重命名/链接到真实 ID 下，让 Paths 里存的相对路径与磁盘上
# 真实存在的目录对应（docs/contracts/filesystem.md 硬约束 1）。
mv "${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}" "${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}"

PATHS_JSON='{"Kernel":"vmlinuz","Initrd":["initrd.img"],"Repo":"repo/","Extra":{}}'
sqlite3 "${DB_PATH}" "UPDATE artifact_sets SET state='ready', os_family='rhel', os_version='9.4', arch='x86_64', firmware='any', paths_json='${PATHS_JSON}' WHERE id='${CREATED_ARTIFACT_ID}';"

READY_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
[ "${READY_CHECK}" = "ready" ] || fail "阶段4: 制品集未能推进到 ready（当前=${READY_CHECK}）"

# 模板：用真实端点创建（state=draft），再用 sqlite3 推进到 active——见
# 文件头「已知限制」第 2 条：docs/contracts/http-console.md 冻结的四个
# 模板端点里没有任何一个能做这件事。
TEMPLATE_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-rocky94\",\"artifactSetId\":\"${CREATED_ARTIFACT_ID}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
TEMPLATE_ID="$(json_field "${TEMPLATE_CREATE_RESP}" "id")"
[ -n "${TEMPLATE_ID}" ] || fail "阶段4: 创建模板失败，响应=${TEMPLATE_CREATE_RESP}"

sqlite3 "${DB_PATH}" "UPDATE templates SET state='active' WHERE id='${TEMPLATE_ID}';"
ACTIVE_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM templates WHERE id='${TEMPLATE_ID}';")"
[ "${ACTIVE_CHECK}" = "active" ] || fail "阶段4: 模板未能推进到 active（当前=${ACTIVE_CHECK}）"

pass "阶段4: 主机=${HOST_ID}、制品集=${CREATED_ARTIFACT_ID}（ready）、模板=${TEMPLATE_ID}（active）均已就位"

# ---------------------------------------------------------------------------
# 阶段 5：建模板已在阶段 4 完成；这里顺手验证 preview 端点（不需要真任务
# 就能跑，SPEC 计划任务 5 明确要求）。
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"
[ "${PREVIEW_CODE}" = "0" ] || fail "阶段5: preview 端点未成功，响应=${PREVIEW_RESP}"
echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 响应缺少 answerFile 字段"
pass "阶段5: preview 端点用假数据渲染成功（不建任务、不签 token、不落库）"

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例（四条，本脚本最有价值的部分）
# ---------------------------------------------------------------------------

# 6a. 完全陌生的机器 → 回 sanboot，且任务状态不变、事件表无新增
#     （§4.2、§0.1：no_task 不建任务不记状态不告警）。
STRANGER_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=NO-SUCH-SN&mac=00:00:00:00:00:99&uuid=&arch=x86_64&plat=efi")"
echo "${STRANGER_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6a: 陌生机器未收到 sanboot 载荷，响应=${STRANGER_BOOT}"
pass "阶段6a: 完全陌生的机器请求 /v1/boot 回 sanboot"

# 先建一个真正的任务，供 6b/6c/7/8/9 使用。
TASK_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-host-01\"}}")"
TASK_ID="$(json_field "${TASK_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段6: 建任务失败，响应=${TASK_CREATE_RESP}"
echo "建任务成功: task_id=${TASK_ID}" >&2

# 6b. SN 对但 MAC 不对 → 拿不到菜单，回 sanboot（§4.3 的双因子硬要求）。
WRONG_MAC_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_SN}&mac=ff:ff:ff:ff:ff:ff&uuid=&arch=x86_64&plat=efi")"
echo "${WRONG_MAC_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6b: MAC 不匹配未回 sanboot，响应=${WRONG_MAC_BOOT}"
# 这一支是"范围内失败"（§4.6）：状态机会把任务置 failed。用任务列表里的
# failureReason 字段核对（internal/gateway/boot.go 的 MAC 双因子拒绝分支
# 复用 model.FailureReasonPrecheck，因为 FailureReason 枚举里没有专门的
# "MAC 不匹配"原因——这本身是一处记入 contractGaps 的偏差，本脚本按
# 代码实际行为断言，不按"理应有专门原因"断言）。事件表里的 Message 是
# 格式化好的诊断文案，不含 failure reason 的字面值，因此不能靠 grep
# 事件消息判断，要查 taskItem.failureReason 这个结构化字段。
TASK_AFTER_MAC_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_AFTER_MAC_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6b: MAC 不匹配后任务的 failureReason 字段未见 precheck，响应=${TASK_AFTER_MAC_FAIL}"
pass "阶段6b: SN 对但 MAC 不对 → 回 sanboot，且任务被置 failed（failureReason=precheck）"

# 6c. 架构不符 → 回 sanboot 且任务置 failed，失败原因可读为 precheck
#     （§4.6 硬拒绝）。用一台新主机+新任务（6b 已经把上一个任务打成
#     failed，不能复用）。
HOST2_SN="E2E-SN-0002"
HOST2_MAC="aa:bb:cc:dd:ee:02"
HOST2_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST2_SN}\",\"mac\":\"${HOST2_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-RACK\"}")"
HOST2_ID="$(json_field "${HOST2_RESP}" "id")"
[ -n "${HOST2_ID}" ] || fail "阶段6c: 录入第二台主机失败，响应=${HOST2_RESP}"

TASK2_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST2_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-host-02\"}}")"
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
pass "阶段6c: 架构不符 → 回 sanboot 且任务置 failed（failureReason=precheck，原因可读）"

# 6d. precheck 失败的任务允许 retry 并回到 pending（§8.2 只禁两类超时，
#     不扩大范围）。用 6c 里已经 failed 的 TASK2_ID。
RETRY_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK2_ID}/retry" -H "${AUTH_HEADER}")"
RETRY_STATE="$(json_field "${RETRY_RESP}" "state")"
[ "${RETRY_STATE}" = "pending" ] || fail "阶段6d: precheck 失败的任务 retry 后状态 = ${RETRY_STATE}, want pending，响应=${RETRY_RESP}"
pass "阶段6d: precheck 失败（非 reboot_timeout/init_timeout）的任务允许 retry 并回到 pending"

pass "阶段6: 四条准入控制负面用例全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：模拟 iPXE 取引导脚本与菜单，从菜单里抠出装机 token
#
# 用阶段 6d 里 retry 回到 pending 的 TASK2_ID/HOST2_SN/HOST2_MAC——它已经
# 携带正确的 arch 上报值验证过一次拒绝路径，现在带正确的一切走一遍成功
# 路径，天然衔接「同一台机器重试后能正常装上」这条叙事，不需要再造第三
# 台主机。
# ---------------------------------------------------------------------------

# 先取首跳脚本，确认其中包含 chain 到 /v1/boot 的指令（不模拟 iPXE 变量
# 展开，那是固件的事；这里只确认端点本身可用，SPEC §4.1/§4.7）。
BOOTSCRIPT="$(curl -fsS "${GW_BASE}/boot.ipxe")"
echo "${BOOTSCRIPT}" | grep -q "chain ${GW_BASE}/v1/boot" \
	|| fail "阶段7: /boot.ipxe 内容不含预期的 chain 指令，响应=${BOOTSCRIPT}"

BOOT_MENU="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${BOOT_MENU}" | grep -q "inst.ks=" || fail "阶段7: 引导菜单未包含 inst.ks= 行，响应=${BOOT_MENU}"

# inst.ks= 形如 <gateway_base>/v1/answer/<task_id>/<install_token>/ks.cfg
# （internal/render/menu.go 的 renderRockyMenu）。用 sed 抠出
# <task_id>/<install_token> 这一段，再拆成两个变量。
KS_URL_SEGMENT="$(echo "${BOOT_MENU}" | grep -o "inst.ks=[^ ]*" | sed -E 's#inst.ks=.*/v1/answer/##')"
INSTALL_TASK_ID="$(echo "${KS_URL_SEGMENT}" | cut -d/ -f1)"
INSTALL_TOKEN="$(echo "${KS_URL_SEGMENT}" | cut -d/ -f2)"
[ "${INSTALL_TASK_ID}" = "${TASK2_ID}" ] || fail "阶段7: 菜单里的 task id (${INSTALL_TASK_ID}) 与预期 (${TASK2_ID}) 不符"
[ -n "${INSTALL_TOKEN}" ] || fail "阶段7: 未能从菜单里抠出装机 token，KS_URL_SEGMENT=${KS_URL_SEGMENT}"

# 断言 state=booting（计划任务描述明确要求这一断言）。
BOOTING_STATE="$(json_field "$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK2_ID}/events" -H "${AUTH_HEADER}")" "state" 2>/dev/null || true)"
TASK2_DETAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_DETAIL}" | grep -q '"state":"booting"' \
	|| fail "阶段7: /v1/boot 命中后任务状态未变为 booting，响应=${TASK2_DETAIL}"

pass "阶段7: /v1/boot 命中签发 token，任务进入 booting，从菜单里取得 install token"

# ---------------------------------------------------------------------------
# 阶段 8：取应答文件（断言 installing）、逐个信号打点
# ---------------------------------------------------------------------------

ANSWER_URL="${GW_BASE}/v1/answer/${TASK2_ID}/${INSTALL_TOKEN}/ks.cfg"
ANSWER_BODY="$(curl -fsS "${ANSWER_URL}")"
[ -n "${ANSWER_BODY}" ] || fail "阶段8: 取应答文件失败或内容为空，url=${ANSWER_URL}"

# 应答文件含渲染器骨架三件套：内联参数（kickstart 的 %pre/网络参数一类
# 内联指令）、syslog 指向（logging --host=...）、firstboot 脚手架
# （%post 里落地 init.pending 等文件的指令）。
echo "${ANSWER_BODY}" | grep -q "logging --host=" || fail "阶段8: 应答文件缺少 syslog 指向（logging --host=）"
echo "${ANSWER_BODY}" | grep -q "init.pending" || fail "阶段8: 应答文件缺少 firstboot 脚手架（init.pending）"
echo "${ANSWER_BODY}" | grep -qE "%pre|%post" || fail "阶段8: 应答文件缺少内联参数区段（%pre/%post）"

# 重复发同一次取应答文件请求仍应成功——installer 可能重复取（Ubuntu 的
# cloud-init 会取两次，本例是 RHEL 但幂等性是 MarkAnswerFetched 的通用
# 保证，SPEC §6.3）。
curl -fsS -o /dev/null "${ANSWER_URL}" || fail "阶段8: 重复取应答文件失败（应幂等成功）"

TASK2_AFTER_ANSWER="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_ANSWER}" | grep -q '"state":"installing"' \
	|| fail "阶段8: 取应答文件后任务状态未变为 installing，响应=${TASK2_AFTER_ANSWER}"
pass "阶段8a: 取应答文件成功，含渲染器骨架三件套，任务进入 installing，重复取仍幂等成功"

# finalize token：命中引导时网关已经顺带签发（handleBootGrant 里调用
# IssueFinalizeToken），但网关不会把它暴露在任何人面可读的响应里
# （§6：人面 API 不回传任务的装机凭据）。真实机器场景下 finalize token
# 是渲染器写进应答文件正文的（%post 阶段落 /var/lib/lober/finalize.token，
# docs/contracts/filesystem.md 的 install/echo 两行，见
# internal/render/firstboot.go），但那两行的具体文本格式是
# "install -m 600 /dev/null /var/lib/lober/finalize.token" 与
# "echo '<token>' > /var/lib/lober/finalize.token"——用 grep/sed 从这种
# shell 脚本正文里可靠地抠出被引号包住的值，比直接查库更脆弱且没有实际
# 收益（这不是在验证渲染器的输出格式本身，那是 M4 的 golden file 测试的
# 职责）。本脚本改为直接查库取得同一个真实值，只用于验证"信号端点能否用
# 它认证"这后半段。
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

# 十一个信号按顺序打点（计划任务描述："逐个信号打点
# pre-start/pre-done/packages-*/post-*/rebooting"到"booted/init-*/ready"，
# 共十一个：pre-start/pre-done/packages-start/packages-done/post-start/
# post-done/rebooting/booted/init-start/init-done/ready）。装机期信号
# （前七个）用 INSTALL_TOKEN，重启后信号（后四个）用 FINALIZE_TOKEN
# （§6.4）。
SEQ=1
for phase in pre-start pre-done packages-start packages-done post-start post-done rebooting; do
	code="$(send_signal "${INSTALL_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（装机期）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

# 延迟到达的早期信号不把阶段打回去（§3.6 只进不退）：用一个比当前水位
# 更小的 seq 重发 pre-start，断言仍回 200（去重/水位判断在服务端内部
# 处理，机器侧只需要看到成功，不需要也不能感知到"这次没有真的推进"）。
LATE_CODE="$(send_signal "${INSTALL_TOKEN}" "pre-start" 1)"
[ "${LATE_CODE}" = "200" ] || fail "阶段8: 延迟到达的早期信号（seq=1，水位已经在 7 之后）未回 200"

# 重复发同一个 seq 仍回成功（§6.3：机器侧会重试三次）。
DUP_CODE="$(send_signal "${INSTALL_TOKEN}" "post-done" 6)"
[ "${DUP_CODE}" = "200" ] || fail "阶段8: 重复发同一个 seq（post-done, seq=6）未回 200"

# finalize token 发装机期信号被拒（§6.4 权限受限）：pre-start 不在
# FinalizeCanSend 白名单内。
FINALIZE_MISUSE_CODE="$(send_signal "${FINALIZE_TOKEN}" "pre-start" 100)"
[ "${FINALIZE_MISUSE_CODE}" = "403" ] || fail "阶段8: finalize token 发装机期信号（pre-start）返回 HTTP ${FINALIZE_MISUSE_CODE}, want 403"

pass "阶段8b: 装机期七个信号打点成功；延迟到达/重复 seq 均幂等；finalize token 越权发装机期信号被拒（403）"

# 重启后四个信号用 finalize token。
for phase in booted init-start init-done ready; do
	code="$(send_signal "${FINALIZE_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8: 信号 ${phase}（重启后）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

pass "阶段8c: 重启后四个信号（booted/init-start/init-done/ready）打点成功"

# ---------------------------------------------------------------------------
# 阶段 9：断言终态 done；装完再引导一次，验证不循环重装
# ---------------------------------------------------------------------------

wait_for_task_state() {
	local sn="$1" want="$2"
	curl -fsS "${CONSOLE_BASE}/tasks?sn=${sn}" -H "${AUTH_HEADER}" | grep -q "\"state\":\"${want}\""
}
poll_until "任务达到终态 done" 10 wait_for_task_state "${HOST2_SN}" "done"
pass "阶段9a: 任务达到终态 done"

# 装完的机器再次引导拿到 sanboot 而不是装机菜单——这是 §10 失败模式 1
# 的拦截点：任务已是终态，FindPendingTaskByIdentity 只匹配 pending，
# 查不到即回 no_task。
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
echo "pending → booting（/v1/boot 命中签 token） → installing（取应答文件） →"
echo "  [pre-start → pre-done → packages-start → packages-done → post-start → post-done → rebooting]（装机期七信号） →"
echo "  [booted → init-start → init-done → ready]（重启后四信号，finalize token） → done"
echo ""
echo "各阶段验证结果："
sqlite3 "${DB_PATH}" "SELECT '  state='||state||' current_phase='||current_phase||' attempt_count='||attempt_count||' last_seq='||last_seq FROM tasks WHERE id='${TASK2_ID}';"
echo ""
echo "=== 全部九个阶段（含四条负面用例）验证通过 ==="
