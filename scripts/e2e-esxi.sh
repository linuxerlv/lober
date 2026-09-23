#!/usr/bin/env bash
# scripts/e2e-esxi.sh —— ESXi 家族端到端验证：curl 模拟机器打点，验证
# 分派刚接上的 ESXi 渲染代码是否真的能跑通。对应本轮任务"ESXi 端到端验证"。
#
# 结构照抄 scripts/e2e.sh（RHEL 家族，九阶段全过，是 MVP 验收基线），只读
# 它、照它的结构写，未修改它一个字符。本脚本内容按 ESXi 家族调整：识别
# 特征文件（boot.cfg+mboot.efi 而非 .treeinfo）、菜单形态（mboot.efi -c
# 而非 kernel/initrd 两行）、kickstart 方言（ESXi 自己的 %firstboot 原生
# 支持，不是 anaconda kickstart）。
#
# =====================================================================
# 本脚本最重要的产出：一个真实的、可复现的失败发现（而不是让脚本假装
# 全过）——写在这里而不是假装测过了：
#
#  【核心发现：ESXi 分派处仍未接线，成功路径无法端到端跑通】
#
#  internal/render/menu.go 与 internal/render/renderer.go 的 switch 分支里，
#  model.OSFamilyESXi 仍与 SUSE、Windows 一起落在
#  `case model.OSFamilySUSE, model.OSFamilyWindows, model.OSFamilyESXi:`
#  分支，返回 unimplementedFamilyError——本轮之前的 Wave2（t08-dispatch 轨）
#  只搭好了分派 switch 的骨架与端到端注入防护（validateAnswerFileName），
#  没有给任何一个新家族（ESXi/Windows/SUSE）接上具体渲染函数调用，尽管
#  internal/render/esxi.go 的三个导出函数（RenderESXiBootCfg/
#  RenderESXiMenuLine/AssembleESXiKickstart）与对应的单元测试、golden file
#  全部就位。这不是本脚本的推测——用 grep 直接核对了这两个文件在本 worktree
#  当前 HEAD 的字面内容（git log 显示两文件最近的改动是
#  140fef4 "entry dispatch with end-to-end injection guard"，即骨架
#  commit，没有后续接线 commit）。
#
#  具体的传导链（本脚本阶段 7 会实测到）：
#   1. 真实机器上报 UEFI + arch 匹配 + SN/MAC 双因子匹配 → precheck 三项
#      校验（arch/firmware/制品完整性）全部通过（这一段与分派无关，独立
#      验证见下方阶段 6e）。
#   2. internal/statemachine/boot.go 的 StartBoot 第 4/5 步随即执行：
#      attempt_count++、签发装机 token、任务 CAS 到 booting——即
#      "消耗了一次真实的装机尝试"，任务状态已经改变，无法撤销。
#   3. 紧接着 internal/gateway/boot.go 的 handleBootGrant 调用
#      s.deps.Renderer.RenderMenu(renderCtx)，命中 unimplementedFamilyError。
#   4. handleBootGrant 对渲染失败的处理是静默回退到 writeNoTaskScript
#      （sanboot 载荷），HTTP 响应仍是 200——机器侧收到的响应与"完全陌生的
#      机器"或"MAC 不匹配"两种拒绝场景逐字符相同，机器没有任何办法区分
#      "我从未被认识"与"我被认识、已经消耗了一次尝试、但服务端不会渲染
#      我的菜单"。
#
#  这比一个直白的报错更隐蔽：任务的 attempt_count 已经不可逆地递增，
#  但 HTTP 响应看起来与完全无害的"没有任务"一模一样，唯一能看出区别的
#  地方是查询任务详情接口（本脚本阶段 7 会展示这一点：/v1/boot 命中之后
#  任务的 attempt_count 确实变成了 1，state 也确实变成了 booting，但机器
#  实际收到的引导脚本是 sanboot 而不是 ESXi 菜单）。如果 MVP 的
#  attempt_count 上限较小（§9.1 第三道闸门），这意味着"分派没接好"这一个
#  纯服务端问题会表现成"这台机器很快就被服务端标记为 attempt_exceeded 永久
#  拒绝"——运维会去查机器硬件或网络，而不会想到去查渲染器的 switch 分支。
#
#  本脚本仍然完整跑完能跑的全部阶段并把这个发现作为阶段 7 的断言内容——
#  断言"RenderMenu 确实按预期失败并回退到 sanboot"，而不是跳过阶段 7。
#  见阶段 7 的详细注释与 docs/real-machine/15-e2e-esxi.md 的完整分析。
#
#  【第二个发现：ESXi 的导入识别在 MVP 阶段被拒绝】
#
#  internal/imports/identify.go 的 mvpUnsupportedFamilies 仍把 ESXi 列在
#  其中——Identify() 认出 boot.cfg+mboot.efi 特征后返回
#  (model.OSFamilyESXi, ErrUnsupportedOSFamily)，且 internal/imports/job.go
#  只有 RunRHEL 一个执行入口，没有 RunESXi。这意味着即使制品集登记→导入
#  job 之间的桥接在本轮由 t01 轨接上了（本脚本已尝试走真实路径，见阶段 4
#  的详细过程与失败记录），ESXi 家族的导入本身仍会在 Identify 这一步就被
#  拒绝——本脚本退回 sqlite3 直接写表的绕法，绕开的是"导入识别"这一步，
#  不是"注册触发导入"这一步（两者是不同的缺口，见下方阶段 4 注释）。
#
#  【第三个发现：ESXi 应答文件的第二文件名契约缺口（复述 esxi.go 已记录的）】
#
#  即使分派接好，internal/render/renderer.go 的 validateAnswerFileName
#  当前只接受 ctx.AnswerFileName() 算出的唯一文件名（Ubuntu 是唯一的
#  例外，硬编码接受额外的 "meta-data"）。model.AnswerFileName(esxi) 固定
#  返回 "boot.cfg"，但真实 ESXi 流程需要两个独立文件：mboot.efi 引导器
#  读取的 boot.cfg（本身不是 kickstart 语法）与 boot.cfg 的 kernelopt 行
#  ks= 指向的另一份真正的 kickstart 脚本（AssembleESXiKickstart 的产出）。
#  esxi.go 文件末尾已经完整记录了这个缺口与正解方向（建议第二文件名
#  "ks.cfg"，与 Ubuntu 的 meta-data 先例同构），本脚本不重复分析过程，
#  只在阶段 7 的注释里指出：即使 RenderMenu/RenderAnswerFile 的 switch
#  被接上，若不同时补上这个第二文件名的分派，取应答文件那一步
#  （/v1/answer/<task>/<token>/ks.cfg）仍会被 validateAnswerFileName 拒绝，
#  因为它只会与 "boot.cfg" 比较、请求 "ks.cfg" 会被判定为"文件名不一致"。
#
# 依赖：curl、sqlite3、go（与 e2e.sh 完全相同）。
set -euo pipefail

# ---------------------------------------------------------------------------
# 基础设施：临时目录、端口、trap 清理（与 e2e.sh 完全相同的处理，包括
# to_win_path 的 MSYS/Windows 路径转换必要性——理由见 e2e.sh 文件头注释，
# 这里不重复解释，只复用同一套解决方案）。
# ---------------------------------------------------------------------------

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO_ROOT="$(cd "${SCRIPT_DIR}/.." && pwd)"

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e-esxi.XXXXXX")"

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

# port_in_use 用 /dev/tcp（bash 内建，Git Bash 支持）探测某端口在
# 127.0.0.1 上是否已有进程在监听——能连上就是被占用。
#
# 这不是可移植性上的锦上添花，是一个真实踩到的 bug：本仓库同时有 15+ 条
# worktree 轨在同一台 Windows 机器上并行跑各自的端到端脚本，多条轨若都
# 复用 e2e.sh 的"固定高位端口"套路（哪怕各自加了不同的偏移量），仍有相当
# 概率撞上同一个端口——实测跑通了这一点：造脚本过程中曾观察到一个属于
# 别的家族轨（Debian）遗留的 lober 进程占着 28080/28090（与 e2e.sh 本身
# 的端口相同），说明"固定端口＋各轨各自随手挑一个偏移量"这个假设在真实
# 并行环境下不成立。用固定端口的后果具体表现为：本脚本的服务器进程
# 启动失败（bind: Only one usage of each socket address...），但脚本前面
# 几个阶段（登录等）可能因为恰好连到了别的进程而产生极难诊断的假象——
# 与其冒这种风险，不如从一开始就不依赖任何固定端口。
port_in_use() {
	local port="$1"
	(exec 3<>"/dev/tcp/127.0.0.1/${port}") 2>/dev/null
}

# pick_free_ports 在一个宽、不常见的范围内随机挑三个互不相同的端口
# （网关/控制台/syslog），且都必须探测为空闲——不是简单加固定偏移量。
# 每次调用用 $RANDOM（bash 内建，每次取值不同）重新随机，最多重试若干轮，
# 全部失败则报错退出（不静默退回一个可能仍冲突的固定值）。
pick_free_ports() {
	local attempt=0 max_attempts=20
	while [ "${attempt}" -lt "${max_attempts}" ]; do
		attempt=$((attempt + 1))
		# 范围 30000-49000：避开 e2e.sh 与其它可能沿用同一套"28xxx 附近"
		# 惯例的家族轨常用的高位段，同时留够 3 个端口的间隔。
		local base=$((30000 + RANDOM % 19000))
		local gw=$((base))
		local console=$((base + 37))
		local syslog=$((base + 71))
		if ! port_in_use "${gw}" && ! port_in_use "${console}" && ! port_in_use "${syslog}"; then
			echo "${gw} ${console} ${syslog}"
			return 0
		fi
	done
	return 1
}

read -r GW_PORT CONSOLE_PORT SYSLOG_PORT <<<"$(pick_free_ports)" \
	|| { echo "无法找到三个互不冲突的空闲端口（已重试 20 轮），放弃" >&2; exit 1; }

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

# finding 记录一条"这是本脚本发现的真实缺口，不是脚本本身的失败"——与
# fail 不同，finding 不会让脚本以非零码退出，它只是把发现打印得足够醒目，
# 供人工核对与写进 docs/real-machine/15-e2e-esxi.md。
finding() {
	echo "" >&2
	echo "[FINDING] $1" >&2
	echo "" >&2
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

echo "=== Lober ESXi 家族端到端验证 ==="
echo "工作目录: ${WORKDIR}"

# ---------------------------------------------------------------------------
# 阶段 1：造 ESXi 假制品树、建临时目录、构建二进制、造 admin 用户、写配置
# ---------------------------------------------------------------------------

mkdir -p "${WORKDIR_WIN}/db" "${ARTIFACT_ROOT}" "${LOG_ROOT}"

ARTIFACT_SET_ID="e2e-esxi8-x86_64"
bash "${SCRIPT_DIR}/e2e/mkfixture-esxi.sh" "${ARTIFACT_ROOT}" "${ARTIFACT_SET_ID}"

( cd "${REPO_ROOT}" && go build -o "${BIN_PATH}" ./cmd/lober )
[ -x "${BIN_PATH}" ] || fail "阶段1: 构建 lober 二进制失败"

ADMIN_USER="admin"
ADMIN_PASS="admin12345"
# 与 e2e.sh 完全相同的固定测试哈希（同一份仅用于本一次性临时数据库的
# bcrypt 值，不是生产凭据）。
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
  token_secret: "e2e-esxi-test-secret"
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

"${BIN_PATH}" -config "${CONFIG_PATH}" -check || fail "阶段1: -check 校验模式未通过"

pass "阶段1: ESXi 制品树、二进制、admin 用户、配置文件均已就位"

# ---------------------------------------------------------------------------
# 阶段 2：起服务
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
# 阶段 4：录入一台主机、登记制品集（优先真实路径，走不通退回 sqlite3）、
# 建模板（active）
# ---------------------------------------------------------------------------

HOST_SN="E2E-ESXI-SN-0001"
HOST_MAC="aa:bb:cc:ee:ff:01"
HOST_UUID="22222222-2222-2222-2222-222222222222"

HOST_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST_SN}\",\"mac\":\"${HOST_MAC}\",\"uuid\":\"${HOST_UUID}\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-ESXI-RACK\"}")"
HOST_ID="$(json_field "${HOST_RESP}" "id")"
[ -n "${HOST_ID}" ] || fail "阶段4: 录入主机失败，响应=${HOST_RESP}"

# 制品集：先用真实端点落一条 importing 记录，取得真实分配的 ID——这一步
# 本身不依赖任何家族特定逻辑，对 ESXi 同样成功。
ARTIFACT_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/artifact-sets" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sourcePath\":\"${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}\",\"name\":\"esxi8.0-x86_64\"}")"
CREATED_ARTIFACT_ID="$(json_field "${ARTIFACT_CREATE_RESP}" "id")"
[ -n "${CREATED_ARTIFACT_ID}" ] || fail "阶段4: 登记制品集失败，响应=${ARTIFACT_CREATE_RESP}"

mv "${ARTIFACT_ROOT}/${ARTIFACT_SET_ID}" "${ARTIFACT_ROOT}/${CREATED_ARTIFACT_ID}"

# 【发现记录点】本脚本按任务要求"优先走真实的登记→导入→ready 路径，走不通
# 再退回 sqlite3 绕法"——这里明确记录为什么走不通，而不是不做尝试就直接
# 退回绕法：即使 t01 轨已经把"制品集登记→触发导入 job"这条桥接接上（本
# worktree 未验证这件事本身，因为那是 Go 代码，不在本轨文件归属内），
# internal/imports/identify.go 的 mvpUnsupportedFamilies 仍会在识别阶段
# 就把 ESXi 判定为 ErrUnsupportedOSFamily（导入 job 会把制品集置为
# state=failed，不是 ready）。这不是本脚本能绕过的应用层逻辑问题——直接
# 触发一次真实导入只会验证"MVP 确实明确拒绝 ESXi 导入"这一件事，不会让
# 制品集变成 ready，因此本脚本采用与 e2e.sh 完全相同的绕法：跳过导入
# job，直接用 sqlite3 CLI 把 state 推进到 ready 并写回 paths_json/
# os_family/firmware——文件本身仍是 mkfixture-esxi.sh 造出的真实文件、
# 真实落在制品根目录下，绕过的只是"识别+导入"这两步，不是伪造磁盘内容。
finding "阶段4: 制品集登记→导入 job 路径对 ESXi 走不通——" \
	"internal/imports/identify.go 的 mvpUnsupportedFamilies 仍把 esxi 列为" \
	"MVP 不支持（Identify 会返回 ErrUnsupportedOSFamily），且" \
	"internal/imports/job.go 只有 RunRHEL 一个执行入口，没有 RunESXi。" \
	"退回 sqlite3 绕法：直接写 artifact_sets 表到 ready。"

# ESXi 的 ArtifactPaths 只用 Extra（Kernel/Initrd/Repo 留空，见
# internal/imports/extract_esxi.go 顶部注释第 5 条）。Extra 的键名与
# internal/statemachine/precheck.go 的 checkArtifactFiles 只关心值本身
# （相对路径），键名任意——这里用与磁盘上文件名一致的键，可读性最好。
PATHS_JSON='{"Kernel":"","Initrd":null,"Repo":"","Extra":{"mboot.efi":"mboot.efi","boot.cfg":"boot.cfg","jumpstrt.gz":"jumpstrt.gz","useropts.gz":"useropts.gz","k.b00":"k.b00","chain.b00":"chain.b00"}}'
sqlite3 "${DB_PATH}" "UPDATE artifact_sets SET state='ready', os_family='esxi', os_version='8.0', arch='x86_64', firmware='uefi', paths_json='${PATHS_JSON}' WHERE id='${CREATED_ARTIFACT_ID}';"

READY_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
[ "${READY_CHECK}" = "ready" ] || fail "阶段4: 制品集未能推进到 ready（当前=${READY_CHECK}）"

TEMPLATE_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-esxi8\",\"artifactSetId\":\"${CREATED_ARTIFACT_ID}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
TEMPLATE_ID="$(json_field "${TEMPLATE_CREATE_RESP}" "id")"
[ -n "${TEMPLATE_ID}" ] || fail "阶段4: 创建模板失败，响应=${TEMPLATE_CREATE_RESP}"

sqlite3 "${DB_PATH}" "UPDATE templates SET state='active' WHERE id='${TEMPLATE_ID}';"
ACTIVE_CHECK="$(sqlite3 "${DB_PATH}" "SELECT state FROM templates WHERE id='${TEMPLATE_ID}';")"
[ "${ACTIVE_CHECK}" = "active" ] || fail "阶段4: 模板未能推进到 active（当前=${ACTIVE_CHECK}）"

pass "阶段4: 主机=${HOST_ID}、制品集=${CREATED_ARTIFACT_ID}（ready，esxi/8.0/uefi）、模板=${TEMPLATE_ID}（active）均已就位"

# ---------------------------------------------------------------------------
# 阶段 5：preview 端点——【预期在本轮会失败，这是分派缺口的直接证据】
#
# handleTemplatePreview 内部调用 s.deps.Renderer.RenderAnswerFile 与
# RenderMenu（internal/console/template.go 第 388/393 行），两者对 ESXi
# 都会命中 unimplementedFamilyError。本脚本断言这一点确实发生，而不是
# 断言 preview 成功——若某天这条断言开始失败（即 preview 意外成功），
# 说明分派已经接上，需要把本脚本这一段改回像 e2e.sh 阶段 5 那样断言成功。
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -sS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
echo "${PREVIEW_RESP}" | grep -q "MVP 范围内未实现" \
	|| fail "阶段5: 预期 preview 因分派未接线而报错（含'MVP 范围内未实现'），实际响应=${PREVIEW_RESP}——" \
		"若这是因为分派已经接上，请把本阶段改为像 e2e.sh 阶段5那样断言成功"
finding "阶段5: preview 端点对 ESXi 模板报错（'该 OS 家族的...渲染在 MVP 范围内未实现'）——" \
	"与 menu.go/renderer.go 的分派缺口完全一致，不是 preview 自身的问题。"
pass "阶段5: 确认 preview 端点因分派未接线而按预期报错（渲染器 switch 分支仍返回未实现）"

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例——四条硬性要求 + ESXi 特有的第五条（BIOS 硬拒绝）
#
# 6a-6d 与分派是否接线完全无关：precheck 三项校验（arch/firmware/制品
# 完整性）都发生在 StartBoot 内部，早于对 Renderer.RenderMenu 的调用
# （internal/statemachine/boot.go 第 43-66 行 vs
# internal/gateway/boot.go 第 150-186 行）。本脚本据此预期这五条负面
# 用例全部能通过真实路径验证，与阶段 5/7 的分派缺口互不影响。
# ---------------------------------------------------------------------------

# 6a. 完全陌生的机器 → sanboot（§4.2、§0.1）。
STRANGER_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=NO-SUCH-ESXI-SN&mac=00:00:00:00:00:88&uuid=&arch=x86_64&plat=efi")"
echo "${STRANGER_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6a: 陌生机器未收到 sanboot 载荷，响应=${STRANGER_BOOT}"
pass "阶段6a: 完全陌生的机器请求 /v1/boot 回 sanboot"

# 先建一个真正的任务，供 6b/6c/6e/7 使用。
TASK_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-esxi-01\"}}")"
TASK_ID="$(json_field "${TASK_CREATE_RESP}" "id")"
[ -n "${TASK_ID}" ] || fail "阶段6: 建任务失败，响应=${TASK_CREATE_RESP}"
echo "建任务成功: task_id=${TASK_ID}" >&2

# 6b. SN 对但 MAC 不对 → sanboot（§4.3 双因子硬要求）。
WRONG_MAC_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST_SN}&mac=ff:ff:ff:ff:ff:fe&uuid=&arch=x86_64&plat=efi")"
echo "${WRONG_MAC_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6b: MAC 不匹配未回 sanboot，响应=${WRONG_MAC_BOOT}"
TASK_AFTER_MAC_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST_SN}" -H "${AUTH_HEADER}")"
echo "${TASK_AFTER_MAC_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6b: MAC 不匹配后任务的 failureReason 字段未见 precheck，响应=${TASK_AFTER_MAC_FAIL}"
pass "阶段6b: SN 对但 MAC 不对 → 回 sanboot，且任务被置 failed（failureReason=precheck）"

# 6c. 架构不符 → sanboot 且任务置 failed（§4.6 硬拒绝）。用第二台主机+
#     新任务（6b 已把上一个任务打成 failed）。
HOST2_SN="E2E-ESXI-SN-0002"
HOST2_MAC="aa:bb:cc:ee:ff:02"
HOST2_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/hosts" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"sn\":\"${HOST2_SN}\",\"mac\":\"${HOST2_MAC}\",\"uuid\":\"\",\"arch\":\"x86_64\",\"firmware\":\"uefi\",\"rack\":\"E2E-ESXI-RACK\"}")"
HOST2_ID="$(json_field "${HOST2_RESP}" "id")"
[ -n "${HOST2_ID}" ] || fail "阶段6c: 录入第二台主机失败，响应=${HOST2_RESP}"

TASK2_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"hostId\":\"${HOST2_ID}\",\"templateId\":\"${TEMPLATE_ID}\",\"formData\":{\"hostname\":\"e2e-esxi-02\"}}")"
TASK2_ID="$(json_field "${TASK2_CREATE_RESP}" "id")"
[ -n "${TASK2_ID}" ] || fail "阶段6c: 建第二个任务失败，响应=${TASK2_CREATE_RESP}"

ARCH_MISMATCH_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=arm64&plat=efi")"
echo "${ARCH_MISMATCH_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6c: 架构不符未回 sanboot，响应=${ARCH_MISMATCH_BOOT}"

TASK2_AFTER_ARCH_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_ARCH_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6c: 架构不符后任务的 failureReason 字段未见 precheck，响应=${TASK2_AFTER_ARCH_FAIL}"
pass "阶段6c: 架构不符 → 回 sanboot 且任务置 failed（failureReason=precheck，原因可读）"

# 6d. precheck 失败的任务允许 retry 并回到 pending（§8.2）。
RETRY_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK2_ID}/retry" -H "${AUTH_HEADER}")"
RETRY_STATE="$(json_field "${RETRY_RESP}" "state")"
[ "${RETRY_STATE}" = "pending" ] || fail "阶段6d: precheck 失败的任务 retry 后状态 = ${RETRY_STATE}, want pending，响应=${RETRY_RESP}"
pass "阶段6d: precheck 失败（非 reboot_timeout/init_timeout）的任务允许 retry 并回到 pending"

# 6e.【ESXi 特有】固件不符（BIOS 请求装 ESXi）→ 硬拒绝，且错误信息明确
#    指出"这台机器是 BIOS，任务要装 ESXi"（SPEC §4.6、附录 A.14：ESXi
#    只支持 UEFI 是 MVP 的明确边界，不是暂未实现）。用 6d 里 retry 回到
#    pending 的 TASK2_ID/HOST2_SN/HOST2_MAC，这次上报 plat=pcbios。
BIOS_ESXI_BOOT="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=pcbios")"
echo "${BIOS_ESXI_BOOT}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段6e: BIOS+ESXi 组合未回 sanboot，响应=${BIOS_ESXI_BOOT}"

TASK2_AFTER_FIRMWARE_FAIL="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
echo "${TASK2_AFTER_FIRMWARE_FAIL}" | grep -q '"failureReason":"precheck"' \
	|| fail "阶段6e: BIOS+ESXi 后任务的 failureReason 字段未见 precheck，响应=${TASK2_AFTER_FIRMWARE_FAIL}"

# 断言失败详情里确实包含可读的诊断文案（internal/statemachine/precheck.go
# 的 checkFirmware：`这台机器是 %s，任务要装 %s，该 OS 家族不支持这种固件
# 模式`，%s 用 model.FirmwareMode 的原始取值格式化，实测是小写
# "bios"/"esxi"，不是"BIOS"/"ESXi"）——用事件表的 Message 字段核对，因为
# 它是格式化好的诊断文案（与 e2e.sh 阶段6b 的注释一致：failureReason
# 结构化字段只给出分类，不给细节，细节要看事件消息）。
TASK2_EVENTS_AFTER_FIRMWARE="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK2_ID}/events" -H "${AUTH_HEADER}")"
echo "${TASK2_EVENTS_AFTER_FIRMWARE}" | grep -q "这台机器是 bios，任务要装 esxi" \
	|| fail "阶段6e: 事件消息未见'这台机器是 bios，任务要装 esxi'这条可读诊断文案，响应=${TASK2_EVENTS_AFTER_FIRMWARE}"
pass "阶段6e:【ESXi特有】BIOS 请求装 ESXi → 硬拒绝（precheck.checkFirmware），错误信息可读"

# 6e 复查：precheck 拒绝不消耗 attempt_count（§4.6：重试必然同样失败，
# 不该在"任务还没真正开始"之前就把 attempt 用掉）——查库核对当前
# attempt_count 仍是初始值（0），且这是 TASK2_ID 第二次触发 precheck 拒绝
# （6c 架构不符 + 6e 固件不符），若 attempt_count 被错误递增，这里会看到
# 非零值。
TASK2_ATTEMPT_COUNT="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK2_ID}';")"
[ "${TASK2_ATTEMPT_COUNT}" = "0" ] || fail "阶段6e: precheck 拒绝（架构+固件共两次）后 attempt_count=${TASK2_ATTEMPT_COUNT}, want 0——precheck 失败不应消耗 attempt"
pass "阶段6e复查: 两次 precheck 拒绝（arch、firmware）均未消耗 attempt_count（仍为 0）"

pass "阶段6: 四条通用准入控制负面用例 + ESXi 特有的 BIOS 硬拒绝，全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：真实成功路径——UEFI + 正确 SN/MAC/arch → 【本脚本的核心发现】
#
# 用 6e 之后需要 retry 回到 pending 的 TASK2_ID（precheck 拒绝允许 retry，
# 与 6d 同样的道理），这次带完全正确的一切（含 plat=efi）请求 /v1/boot。
# ---------------------------------------------------------------------------

RETRY2_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK2_ID}/retry" -H "${AUTH_HEADER}")"
RETRY2_STATE="$(json_field "${RETRY2_RESP}" "state")"
[ "${RETRY2_STATE}" = "pending" ] || fail "阶段7: 固件不符任务 retry 后状态 = ${RETRY2_STATE}, want pending，响应=${RETRY2_RESP}"

ATTEMPT_BEFORE="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK2_ID}';")"

REAL_BOOT_RESP="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"

TASK2_AFTER_REAL_BOOT="$(curl -fsS "${CONSOLE_BASE}/tasks?sn=${HOST2_SN}" -H "${AUTH_HEADER}")"
ATTEMPT_AFTER="$(sqlite3 "${DB_PATH}" "SELECT attempt_count FROM tasks WHERE id='${TASK2_ID}';")"
STATE_AFTER="$(sqlite3 "${DB_PATH}" "SELECT state FROM tasks WHERE id='${TASK2_ID}';")"

echo "" >&2
echo "=== 阶段7 核心发现的实测数据 ===" >&2
echo "  请求前 attempt_count = ${ATTEMPT_BEFORE}" >&2
echo "  请求后 attempt_count = ${ATTEMPT_AFTER}" >&2
echo "  请求后任务 state     = ${STATE_AFTER}" >&2
echo "  机器实际收到的响应体 = ${REAL_BOOT_RESP}" >&2
echo "" >&2

# 断言 1：任务状态机侧确实推进了——precheck 通过、attempt 确实被消耗、
# state 确实变成了 booting。这证明"分派缺口发生在 StartBoot 成功之后"，
# 不是"整条链路都没跑起来"那种更容易被发现的失败。
[ "${STATE_AFTER}" = "booting" ] || fail "阶段7: 预期任务状态机侧已推进到 booting（与分派是否接线无关），实际=${STATE_AFTER}"
[ "${ATTEMPT_AFTER}" -gt "${ATTEMPT_BEFORE}" ] || fail "阶段7: 预期 attempt_count 已被消耗（${ATTEMPT_BEFORE} -> ${ATTEMPT_AFTER}），实际未变化"

# 断言 2：但机器实际收到的响应是 sanboot，不是 ESXi 菜单——这就是分派
# 缺口的直接后果。若这条断言开始失败（即机器收到了 mboot.efi 菜单行），
# 说明分派已经接上，需要把本阶段改写为断言真正的菜单内容（mboot.efi -c
# <boot.cfg URL>，SPEC §4.5），并继续往下验证阶段 8/9。
echo "${REAL_BOOT_RESP}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段7: 预期分派未接线导致回退到 sanboot，实际响应=${REAL_BOOT_RESP}——" \
		"若分派已接上，请把本阶段改为断言 'mboot.efi -c' 菜单内容并继续验证阶段8/9"

finding "阶段7:【核心发现】precheck 全部通过、attempt_count 已消耗" \
	"（${ATTEMPT_BEFORE} -> ${ATTEMPT_AFTER}）、任务状态已推进到 booting，" \
	"但机器收到的响应体与'完全陌生的机器'/'MAC不匹配'逐字符相同（sanboot" \
	"载荷）——因为 RenderMenu 命中 unimplementedFamilyError，" \
	"internal/gateway/boot.go 的 handleBootGrant 把渲染失败静默降级为" \
	"no_task 载荷。机器侧无法区分'从未被认识'与'已消耗一次尝试但服务端" \
	"不会渲染菜单'这两种完全不同的情形。"

pass "阶段7:【核心发现，已实测确认】ESXi 分派未接线——StartBoot 成功推进状态机并消耗 attempt，但 RenderMenu 失败后网关静默回退到 sanboot，机器侧完全无法区分这与'陌生机器'的差异"

echo "" >&2
echo "=== 阶段 8/9 无法继续 ===" >&2
echo "机器从未拿到装机 token（菜单里的 inst.ks=/ks= 段），阶段 8（取应答" >&2
echo "文件、装机期信号）与阶段 9（终态断言、重启后 sanboot 断言）都需要" >&2
echo "这个 token 才能进行——这不是本脚本还需要补的逻辑，是分派缺口本身" >&2
echo "导致后续阶段在真实环境下同样无法达到，与本脚本的验证方式无关。" >&2
echo "" >&2

echo ""
echo "=== ESXi 端到端验证结论 ==="
echo "阶段 1-4（基础设施、主机、制品集、模板）：通过（制品集经 sqlite3 绕过导入识别）"
echo "阶段 5（preview）：按预期因分派未接线而报错（已确认）"
echo "阶段 6（五条准入控制负面用例，含 ESXi 特有的 BIOS 硬拒绝）：全部通过，与分派无关"
echo "阶段 7（真实成功路径）：precheck 通过、attempt 消耗、状态推进到 booting，"
echo "                        但 RenderMenu 失败，机器收到 sanboot 而非 ESXi 菜单"
echo "阶段 8/9：无法继续（机器从未拿到装机 token）"
echo ""
echo "=== 三个核心发现已记录，见本文件头部注释与 docs/real-machine/15-e2e-esxi.md ==="
