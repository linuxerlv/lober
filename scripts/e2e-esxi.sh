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
# 本次修订（t17-fix-e2e）：上一轮编写本脚本时，ESXi 的分派（menu.go/
# renderer.go）尚未接线，所以断言方向是"确认分派未接线、回退到 sanboot"。
# 这一轮 t08-dispatch 轨已经把 ESXi 的菜单渲染（RenderMenu → renderESXiMenu
# → RenderESXiMenuLine）接上（见 internal/render/dispatch_test.go 的
# TestDispatch_RenderMenu_ImplementedFamiliesSucceed/esxi 子测试，已跑通
# 确认），断言方向随之整体反转为验证成功路径——九阶段框架与阶段 1-4/6 的
# 准入负面用例原样保留，只把阶段 5/7/8/9 里建立在"未接线"前提上的断言改为
# 验证真实菜单/应答文件内容。
#
# 但本次运行同时确认：**分派接上的只是菜单渲染，不是应答文件的两段式渲染**
# ——RenderAnswerFile 对 ESXi 请求 "boot.cfg" 时调用的是
# AssembleESXiKickstart（产出 ESXi kickstart 方言正文），不是
# RenderESXiBootCfg（改写引导参数文件的三处 prefix=/kernelopt=/modules=）。
# 这不是本脚本的猜测，是 internal/render/renderer.go 与 internal/render/
# esxi.go 文件末尾"契约缺口 1"自己记录的已知设计决策，本脚本在阶段 8 里
# 精确验证这个决策的真实后果（取到的 boot.cfg 内容实际上是 kickstart 正文，
# 不含任何 prefix=/kernelopt= 改写），而不是断言"三处改写在端到端层生效"
# （那在当前代码下不成立，断言了会是一条长期虚假失败或被迫弱化的检验）。
#
# 已知限制（写在这里而不是假装测过了）：
#
#  1. 【不依赖 jq】同 scripts/e2e.sh 第 1 条：本机没有 jq，用 grep/sed 做
#     最小 JSON 字段提取。
#
#  2. 【ESXi 的导入识别在 MVP 阶段仍被拒绝，本脚本走 sqlite3 绕法登记
#     制品集】——本次运行确认这一点没有变化：
#       - internal/imports/identify.go 的 mvpUnsupportedFamilies 仍把 ESXi
#         列在其中——Identify() 认出 boot.cfg+mboot.efi 特征后返回
#         (model.OSFamilyESXi, ErrUnsupportedOSFamily)。
#       - internal/imports/job.go 只有 RunRHEL 一个执行入口，没有 RunESXi。
#     本脚本对此的绕法与 scripts/e2e.sh 完全一致：先用真实端点落一条
#     importing 记录取得真实分配的 ID，再用 sqlite3 CLI 直接把该记录推进
#     到 ready 并写回 paths_json/os_family/arch/firmware——文件本身仍是
#     mkfixture-esxi.sh 造出的真实文件、真实落在制品根目录下，绕过的只是
#     "识别+导入"这两步，不是伪造磁盘内容。换句话说：本脚本验证的是"制品
#     就绪之后"的链路，不是"导入"本身。
#
#  3. 【应答文件的第二文件名契约缺口——本脚本阶段 8 精确验证其真实后果】
#
#     model.AnswerFileName(esxi) 固定返回 "boot.cfg"，
#     internal/render/renderer.go 的 validateAnswerFileName 对 ESXi 没有
#     像 Ubuntu 的 "meta-data" 那样额外放行第二个文件名。这意味着：
#       a) 请求 /v1/answer/<task>/<token>/boot.cfg 会通过文件名校验（它
#          就是 AnswerFileName() 算出的唯一值），但 RenderAnswerFile 对
#          ESXi 分派到的是 AssembleESXiKickstart——产出的是 kickstart 正文
#          （%firstboot/%pre/%post 等段落），不是一份经过三处改写的
#          boot.cfg 引导参数文件。
#       b) RenderESXiBootCfg（改写 prefix=/kernelopt=/modules= 的函数）
#          在生产代码里没有任何调用点（已用 grep 核对全仓，唯一调用点在
#          esxi_test.go），这意味着 mboot.efi 实际会读到的"boot.cfg"响应
#          其实是一段 kickstart 脚本文本，mboot.efi 无法解析它——真实部署
#          场景下这会导致 ESXi 安装器在引导器解析 boot.cfg 这一步直接失败，
#          比"装到一半停住"更早。
#       c) 请求 /v1/answer/<task>/<token>/ks.cfg（真正的 kickstart 应该在
#          的文件名）会被 validateAnswerFileName 拒绝为 404，因为它只与
#          "boot.cfg" 比较。
#     esxi.go 文件末尾"契约缺口 1"已经完整记录这个缺口与正解方向（建议
#     第二文件名 "ks.cfg"，与 Ubuntu 的 meta-data 先例同构）——本脚本不
#     重复分析过程，只在阶段 8 里把 a)/b) 两点实测坐实（用真实 HTTP 响应
#     内容核对，不满足于读代码推断），并记入 findings。
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

# 数据库辅助库：连接参数、schema 隔离、制品集/模板的状态推进。
# 见 scripts/e2e/lib-db.sh 的文件头：数据层迁到 PostgreSQL 之后，连接方式、
# schema 隔离与「paths_json 拆成四列」这三处变化收在那一份库里，不逐脚本重复。
. "${SCRIPT_DIR}/e2e/lib-db.sh"
db_require

WORKDIR="$(mktemp -d "${TMPDIR:-/tmp}/lober-e2e-esxi.XXXXXX")"

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

mkdir -p "${ARTIFACT_ROOT}" "${LOG_ROOT}"

# 本次运行专用的 schema，必须在服务启动之前建好——服务起来时 AutoMigrate 会在
# search_path 指向的 schema 里建表，schema 不存在时启动直接失败。
db_schema_create

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
# 四个路径参数对应 PostgreSQL 侧的四列（paths_kernel / paths_initrd /
# paths_repo / paths_extra）。SQLite 时代是一个 paths_json 列装整个
# ArtifactPaths 的 JSON，迁移后拆开了——拆列这件事只在 lib-db.sh 的
# db_artifact_ready 里落一次。
# ESXi 同 Windows：没有内核与仓库，只产出 Extra 里的一组引导文件。
# initrd 传 'null' 而不是 '[]'——SQLite 时代这里写的就是 JSON null，
# 保持同一个语义（jsonb 存 null，解回 Go 是 nil 切片）。
db_artifact_ready "${CREATED_ARTIFACT_ID}" esxi 8.0 x86_64 uefi \
	'' 'null' '' '{"mboot.efi":"mboot.efi","boot.cfg":"boot.cfg","jumpstrt.gz":"jumpstrt.gz","useropts.gz":"useropts.gz","k.b00":"k.b00","chain.b00":"chain.b00"}' \
	|| fail "阶段4: 制品集未能推进到 ready"

READY_CHECK="$(db_value "SELECT state FROM artifact_sets WHERE id='${CREATED_ARTIFACT_ID}';")"
[ "${READY_CHECK}" = "ready" ] || fail "阶段4: 制品集未能推进到 ready（当前=${READY_CHECK}）"

TEMPLATE_CREATE_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d "{\"name\":\"e2e-esxi8\",\"artifactSetId\":\"${CREATED_ARTIFACT_ID}\",\"answerTemplate\":\"\",\"hookRefs\":[],\"kernelArgs\":\"\",\"diskPolicy\":\"largest\",\"onFailure\":\"halt\",\"requiredFormFields\":[]}")"
TEMPLATE_ID="$(json_field "${TEMPLATE_CREATE_RESP}" "id")"
[ -n "${TEMPLATE_ID}" ] || fail "阶段4: 创建模板失败，响应=${TEMPLATE_CREATE_RESP}"

db_exec "UPDATE templates SET state='active' WHERE id='${TEMPLATE_ID}';"
ACTIVE_CHECK="$(db_value "SELECT state FROM templates WHERE id='${TEMPLATE_ID}';")"
[ "${ACTIVE_CHECK}" = "active" ] || fail "阶段4: 模板未能推进到 active（当前=${ACTIVE_CHECK}）"

pass "阶段4: 主机=${HOST_ID}、制品集=${CREATED_ARTIFACT_ID}（ready，esxi/8.0/uefi）、模板=${TEMPLATE_ID}（active）均已就位"

# ---------------------------------------------------------------------------
# 阶段 5：preview 端点——分派已接线，断言成功路径。
#
# handleTemplatePreview 内部对未指定 hostId 的请求用 fakePreviewHost（固定
# firmware=uefi），与本任务的 ESXi 制品集固件一致，因此 RenderMenu 与
# RenderAnswerFile 都应成功（不会命中 ErrESXiRequiresUEFI）。
# ---------------------------------------------------------------------------

PREVIEW_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/templates/${TEMPLATE_ID}/preview" \
	-H "${AUTH_HEADER}" -H 'Content-Type: application/json' \
	-d '{}')"
PREVIEW_CODE="$(json_field "${PREVIEW_RESP}" "code")"
[ "${PREVIEW_CODE}" = "0" ] || fail "阶段5: preview 端点未成功，响应=${PREVIEW_RESP}"
echo "${PREVIEW_RESP}" | grep -q "answerFile" || fail "阶段5: preview 响应缺少 answerFile 字段，响应=${PREVIEW_RESP}"
echo "${PREVIEW_RESP}" | grep -qi "mvp 范围内未实现" \
	&& fail "阶段5: preview 响应仍含'未实现'字样，说明分派未真正接线，响应=${PREVIEW_RESP}"
pass "阶段5: preview 端点用假数据渲染成功（RenderMenu/RenderAnswerFile 均已接线，不建任务、不签 token、不落库）"

# ---------------------------------------------------------------------------
# 阶段 6：准入控制负面用例——四条硬性要求 + ESXi 特有的第五条（BIOS 硬拒绝）
#
# 6a-6d 与分派是否接线无关：precheck 三项校验（arch/firmware/制品完整性）
# 都发生在 StartBoot 内部，早于对 Renderer.RenderMenu 的调用
# （internal/statemachine/boot.go vs internal/gateway/boot.go 的
# handleBootGrant）。
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
TASK2_ATTEMPT_COUNT="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK2_ID}';")"
[ "${TASK2_ATTEMPT_COUNT}" = "0" ] || fail "阶段6e: precheck 拒绝（架构+固件共两次）后 attempt_count=${TASK2_ATTEMPT_COUNT}, want 0——precheck 失败不应消耗 attempt"
pass "阶段6e复查: 两次 precheck 拒绝（arch、firmware）均未消耗 attempt_count（仍为 0）"

pass "阶段6: 四条通用准入控制负面用例 + ESXi 特有的 BIOS 硬拒绝，全部通过"

# ---------------------------------------------------------------------------
# 阶段 7：真实成功路径——UEFI + 正确 SN/MAC/arch → 机器应收到真实的 ESXi
# 菜单（mboot.efi -c <boot.cfg URL>，SPEC §4.5），不是 sanboot。
#
# 用 6e 之后需要 retry 回到 pending 的 TASK2_ID（precheck 拒绝允许 retry，
# 与 6d 同样的道理），这次带完全正确的一切（含 plat=efi）请求 /v1/boot。
# ---------------------------------------------------------------------------

RETRY2_RESP="$(curl -fsS -X POST "${CONSOLE_BASE}/tasks/${TASK2_ID}/retry" -H "${AUTH_HEADER}")"
RETRY2_STATE="$(json_field "${RETRY2_RESP}" "state")"
[ "${RETRY2_STATE}" = "pending" ] || fail "阶段7: 固件不符任务 retry 后状态 = ${RETRY2_STATE}, want pending，响应=${RETRY2_RESP}"

ATTEMPT_BEFORE="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK2_ID}';")"

ESXI_MENU="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"

ATTEMPT_AFTER="$(db_value "SELECT attempt_count FROM tasks WHERE id='${TASK2_ID}';")"
STATE_AFTER="$(db_value "SELECT state FROM tasks WHERE id='${TASK2_ID}';")"
SIGNED_TOKEN="$(db_value "SELECT token FROM tasks WHERE id='${TASK2_ID}';")"

# 断言 1：任务状态机侧已推进——precheck 通过、attempt 已消耗、state 已变
# 成 booting、token 已签发（与 scripts/e2e.sh 阶段7 同一断言）。
[ "${STATE_AFTER}" = "booting" ] || fail "阶段7: 预期任务状态推进到 booting（StartBoot 已成功提交），实际=${STATE_AFTER}"
[ "${ATTEMPT_AFTER}" -gt "${ATTEMPT_BEFORE}" ] || fail "阶段7: 预期 attempt_count 已被消耗（${ATTEMPT_BEFORE} -> ${ATTEMPT_AFTER}），实际未变化"
[ -n "${SIGNED_TOKEN}" ] || fail "阶段7: 预期 StartBoot 已签发装机 token，但数据库里的 token 字段为空"

# 断言 2：机器收到的不再是 sanboot，而是真实的 ESXi 菜单——分派已接线的
# 直接证据。
echo "${ESXI_MENU}" | grep -q "sanboot --no-describe --drive 0x80" \
	&& fail "阶段7: 要素齐全的机器仍收到 sanboot 载荷，说明 ESXi 分派回归到未接线状态，响应=${ESXI_MENU}"

# 断言 3：菜单形态是唯一一行 `kernel <mboot.efi URL> -c <boot.cfg URL>`
# （RenderESXiMenuLine 的产出，SPEC §4.5），没有 initrd 行——ESXi 的模块由
# boot.cfg 的 modules= 列出，不走 iPXE 的 initrd 机制（esxi.go 文件头注释
# 明确说明"没有 initrd 行"是刻意设计，不是遗漏）。
echo "${ESXI_MENU}" | grep -q "^kernel .*mboot\.efi -c " \
	|| fail "阶段7: 菜单不是预期的 'kernel <mboot.efi URL> -c <boot.cfg URL>' 形态，响应=${ESXI_MENU}"
MENU_LINE_COUNT="$(echo "${ESXI_MENU}" | grep -cv '^[[:space:]]*$')"
[ "${MENU_LINE_COUNT}" = "1" ] || fail "阶段7: ESXi 菜单应恰好一行（无 initrd 行），实际非空行数=${MENU_LINE_COUNT}，响应=${ESXI_MENU}"
echo "${ESXI_MENU}" | grep -q "^initrd " \
	&& fail "阶段7: ESXi 菜单不应含 initrd 行（模块清单在 boot.cfg 的 modules= 里），响应=${ESXI_MENU}"

# 断言 4：-c 参数指向的 boot.cfg URL 含正确的 task/token 路径段
# （GatewayURL("v1","answer",taskID,token,"boot.cfg")，与 renderRockyMenu
# 拼 ksURL 同一模式）。
echo "${ESXI_MENU}" | grep -q "/v1/answer/${TASK2_ID}/${SIGNED_TOKEN}/boot.cfg" \
	|| fail "阶段7: 菜单里 -c 参数未指向预期的 /v1/answer/${TASK2_ID}/${SIGNED_TOKEN}/boot.cfg，响应=${ESXI_MENU}"

pass "阶段7: 要素齐全的 UEFI 机器请求 /v1/boot——任务推进到 booting（attempt ${ATTEMPT_BEFORE}→${ATTEMPT_AFTER}，token 已签发），机器收到真实 ESXi 菜单（唯一一行 kernel=mboot.efi -c <boot.cfg URL>，无 initrd 行）"

# ---------------------------------------------------------------------------
# 阶段 8：取应答文件——精确验证"boot.cfg 请求收到的是 kickstart 正文，不是
# 改写后的引导参数文件"这个已知契约缺口的真实后果（本文件头部「已知限制」
# 第 3 条），而不是断言三处改写在端到端层生效（那不成立）。
# ---------------------------------------------------------------------------

ANSWER_URL="${GW_BASE}/v1/answer/${TASK2_ID}/${SIGNED_TOKEN}/boot.cfg"
ANSWER_HTTP_CODE="$(curl -s -o "${WORKDIR_WIN}/answer_body.txt" -w '%{http_code}' "${ANSWER_URL}")"
ANSWER_BODY="$(cat "${WORKDIR_WIN}/answer_body.txt")"
STATE_AFTER_ANSWER="$(db_value "SELECT state FROM tasks WHERE id='${TASK2_ID}';")"

[ "${ANSWER_HTTP_CODE}" = "200" ] || fail "阶段8: 取应答文件失败，HTTP ${ANSWER_HTTP_CODE}，body=${ANSWER_BODY}"
[ -n "${ANSWER_BODY}" ] || fail "阶段8: 应答文件内容为空，url=${ANSWER_URL}"
[ "${STATE_AFTER_ANSWER}" = "installing" ] || fail "阶段8: 取应答文件后任务状态未变为 installing，实际=${STATE_AFTER_ANSWER}"

# Content-Type：ESXi 走 answerFileContentType 的 default 分支，应为
# text/plain（与 ks.cfg/preseed.cfg/user-data 同一档，不同于 Windows/SUSE
# 的 application/xml）。
ANSWER_CONTENT_TYPE="$(curl -s -o /dev/null -w '%{content_type}' "${ANSWER_URL}")"
case "${ANSWER_CONTENT_TYPE}" in
text/plain*) ;;
*) fail "阶段8: 应答文件 Content-Type=${ANSWER_CONTENT_TYPE}, want text/plain*" ;;
esac

# boot.cfg 现在返回的是**真正的引导参数文件**（2026-09-24 修复：gateway 层
# 读出导入时保存的原始模板，调注入的 RenderESXiBootCfg 做三处改写）。
# 用内容特征区分两种语法，不满足于"能取到内容"——这个区分正是当初发现
# 缺口的方式：kickstart 语法（install/rootpw/network）与引导参数语法
# （prefix=/kernelopt=/modules=）完全不同，mboot.efi 只认后者。
echo "${ANSWER_BODY}" | grep -q "^prefix=" 	|| fail "阶段8: boot.cfg 不含 prefix= 行——mboot.efi 靠它定位模块，缺了直接引导失败，内容=${ANSWER_BODY}"
echo "${ANSWER_BODY}" | grep -q "^kernelopt=" 	|| fail "阶段8: boot.cfg 不含 kernelopt= 行——ks= 就写在这一行里，缺了脚本化安装无法启动（附录 A.8），内容=${ANSWER_BODY}"
echo "${ANSWER_BODY}" | grep -q "^kernelopt=.*ks=" 	|| fail "阶段8: boot.cfg 的 kernelopt= 行不含 ks= 参数，内容=${ANSWER_BODY}"

# 模块路径前的前导斜杠必须被去掉（SPEC §2.1 的三处改写之一，最容易漏的一条：
# 漏了安装器找不到模块）。
if echo "${ANSWER_BODY}" | grep -q "^modules=/"; then
	fail "阶段8: boot.cfg 的 modules= 行仍以 / 开头——前导斜杠必须去掉，否则安装器找不到模块，内容=${ANSWER_BODY}"
fi

# 反向：boot.cfg **不应**含 kickstart 指令。这条是对修复的回归锁——
# 修复前这里拿到的正是 kickstart 正文（install/rootpw/network/reboot）。
echo "${ANSWER_BODY}" | grep -q "^install " 	&& fail "阶段8: boot.cfg 含 kickstart 的 install 指令，说明又退回到了返回 kickstart 正文的错误行为，内容=${ANSWER_BODY}"

pass "阶段8: 取 boot.cfg 成功，内容是引导参数文件（prefix= 与 kernelopt=ks= 齐全、modules= 无前导斜杠），不是 kickstart 正文"

# ── 阶段 8a：ks.cfg 才是 kickstart 正文 ──
# ESXi 的应答文件有两个：boot.cfg（引导参数，上面已验）与 ks.cfg（kickstart
# 正文）。后者的放行与 Ubuntu 的 meta-data 同构（gateway/answer.go 的
# answerFileNameAllowed 对 ESXi 额外放行 ks.cfg）。
KS_URL="${GW_BASE}/v1/answer/${TASK2_ID}/${SIGNED_TOKEN}/ks.cfg"
KS_BODY="$(curl -fsS "${KS_URL}")" || fail "阶段8a: 取 ks.cfg 失败，url=${KS_URL}"

echo "${KS_BODY}" | grep -q "^install " 	|| fail "阶段8a: ks.cfg 不含 install 指令（ESXi kickstart 特征），内容=${KS_BODY}"

# 断言不是 RHEL kickstart——不含 RHEL 特有的段落头与指令。
echo "${KS_BODY}" | grep -q "%packages" 	&& fail "阶段8a: ks.cfg 含 RHEL kickstart 特征 %packages，说明被错误路由到了 AssembleKickstart，内容=${KS_BODY}"
echo "${KS_BODY}" | grep -q "^sshkey " 	&& fail "阶段8a: ks.cfg 含 RHEL 专属的 sshkey 指令（ESXi kickstart 没有这个指令），内容=${KS_BODY}"

# ESXi 的阶段映射：%firstboot 原生支持（不需要 systemd oneshot 脚手架），
# post-nochroot 在 ESXi 上不存在（SPEC §5.2 表格该列是 "—"）。
echo "${KS_BODY}" | grep -q "%firstboot" 	|| fail "阶段8a: ks.cfg 缺少 %firstboot 段落（ESXi 原生支持首次启动段），内容=${KS_BODY}"
echo "${KS_BODY}" | grep -qi "post-nochroot\|%post --nochroot" 	&& fail "阶段8a: ks.cfg 含 post-nochroot 相关内容，但该阶段在 ESXi 上不存在（SPEC §5.2），内容=${KS_BODY}"

# 重复取应答文件仍应成功（幂等，SPEC §6.3）。
curl -fsS -o /dev/null "${ANSWER_URL}" || fail "阶段8a: 重复取 boot.cfg 失败（应幂等成功）"
curl -fsS -o /dev/null "${KS_URL}" || fail "阶段8a: 重复取 ks.cfg 失败（应幂等成功）"

pass "阶段8a: 取 ks.cfg 成功，内容是 ESXi kickstart 正文（install/%firstboot 齐全，不含 RHEL 特征 %packages/sshkey，不含 ESXi 不存在的 post-nochroot），任务进入 installing，两个应答文件重复取均幂等成功"

# ---------------------------------------------------------------------------
# 阶段 8b/8c：装机期信号打点。ESXi 的 %firstboot 段落（esxiFirstbootSection）
# 发 booted/init-start/init-done/ready 四个信号，与 Linux/Windows 共用同一
# 组 model.SignalKind；没有 WinPE 那种独立的"装机期七信号"分组——ESXi 没有
# 类似 kickstart 的 pre/post 显式信号点插入约定（RenderESXiStageSection 里
# 没有插 signal 调用，只有 %firstboot 里插了，见 esxiFirstbootSection）。
# 装机期信号改用 SIGNED_TOKEN（install token）验证权限模型本身：install
# token 应能发任意 signal，finalize token 越权发 install 阶段信号应被拒。
# ---------------------------------------------------------------------------

FINALIZE_TOKEN="$(db_value "SELECT finalize_token FROM tasks WHERE id='${TASK2_ID}';")"
[ -n "${FINALIZE_TOKEN}" ] || fail "阶段8: 无法从数据库取得 finalize token（应已在阶段7的 /v1/boot 命中路径签发）"

send_signal() {
	local token="$1" phase="$2" seq="$3" rc="${4:-0}"
	curl -sS -X POST "${GW_BASE}/v1/signal/${TASK2_ID}/${token}/${phase}?rc=${rc}&seq=${seq}" \
		-d "signal ${phase} seq=${seq}" -o /dev/null -w '%{http_code}'
}

# 装机期信号：ESXi 的 %pre/%post 段落本身不插信号点（esxiHookStageDirectives
# 只有 pre/post 两个 header，RenderESXiStageSection 内部只在有 hook 引用时
# 才渲染内容，没有 hook 时返回空字符串），因此本脚本不模拟一组"固定的装机
# 期信号名"（那样会断言一个 ESXi 渲染器实际不会生成的信号序列）。这里只用
# install token 打一个通用信号验证权限模型：install token 应能发信号。
SEQ=1
INSTALL_SIGNAL_CODE="$(send_signal "${SIGNED_TOKEN}" "pre-start" "${SEQ}")"
[ "${INSTALL_SIGNAL_CODE}" = "200" ] || fail "阶段8b: install token 发 pre-start 信号返回 HTTP ${INSTALL_SIGNAL_CODE}, want 200"
SEQ=$((SEQ + 1))

FINALIZE_MISUSE_CODE="$(send_signal "${FINALIZE_TOKEN}" "pre-start" 100)"
[ "${FINALIZE_MISUSE_CODE}" = "403" ] || fail "阶段8b: finalize token 发装机期信号（pre-start）返回 HTTP ${FINALIZE_MISUSE_CODE}, want 403"

pass "阶段8b: install token 能发信号（权限模型验证）；finalize token 越权发装机期信号被拒（403）"

# 重启后四个信号（%firstboot 段落原生发出，见 esxiFirstbootSection）：
# booted → init-start → init-done → ready，用 finalize token。
for phase in booted init-start init-done ready; do
	code="$(send_signal "${FINALIZE_TOKEN}" "${phase}" "${SEQ}")"
	[ "${code}" = "200" ] || fail "阶段8c: 信号 ${phase}（重启后）返回 HTTP ${code}, want 200"
	SEQ=$((SEQ + 1))
done

pass "阶段8c: 重启后四个信号（booted/init-start/init-done/ready，ESXi 家族 %firstboot 原生对应阶段）打点成功"

# ---------------------------------------------------------------------------
# 阶段 9：断言终态 done；装完再引导一次验证不循环重装；终态任务的应答文件
# 不再提供；事件时间线完整。
# ---------------------------------------------------------------------------

wait_for_task_state() {
	local want="$1"
	db_value "SELECT state FROM tasks WHERE id='${TASK2_ID}';" | grep -qx "${want}"
}
poll_until "任务达到终态 done" 10 wait_for_task_state "done"
pass "阶段9a: 任务达到终态 done"

# 装完的机器再次引导拿到 sanboot 而不是装机菜单（§10 失败模式1：
# FindPendingTaskByIdentity 只匹配 pending，终态任务查不到即回 no_task）。
REBOOT_AFTER_DONE="$(curl -fsS "${GW_BASE}/v1/boot?sn=${HOST2_SN}&mac=${HOST2_MAC}&uuid=&arch=x86_64&plat=efi")"
echo "${REBOOT_AFTER_DONE}" | grep -q "sanboot --no-describe --drive 0x80" \
	|| fail "阶段9b: 装完的机器再次引导未回 sanboot，响应=${REBOOT_AFTER_DONE}"
DONE_STATE_CHECK="$(db_value "SELECT state FROM tasks WHERE id='${TASK2_ID}';")"
[ "${DONE_STATE_CHECK}" = "done" ] || fail "阶段9b: 断言前置条件被破坏——任务状态应仍是 done，实际=${DONE_STATE_CHECK}"
pass "阶段9b: 装完的机器再次引导拿到 sanboot（不是装机菜单，不循环重装；已用状态查询确认任务确实处于终态 done）"

# 终态任务的应答文件不再提供（§4.4 状态门控：仅 booting/installing 可服务）。
ANSWER_AFTER_DONE_CODE="$(curl -s -o /dev/null -w '%{http_code}' "${ANSWER_URL}")"
[ "${ANSWER_AFTER_DONE_CODE}" = "403" ] || fail "阶段9c: 终态任务的应答文件请求返回 HTTP ${ANSWER_AFTER_DONE_CODE}, want 403"
pass "阶段9c: 终态任务的应答文件不再提供（403）"

# 事件表留下了完整时间线。
FINAL_EVENTS="$(curl -fsS "${CONSOLE_BASE}/tasks/${TASK2_ID}/events" -H "${AUTH_HEADER}")"
FINAL_EVENT_COUNT="$(echo "${FINAL_EVENTS}" | grep -o '"taskId"' | wc -l)"
[ "${FINAL_EVENT_COUNT}" -ge 5 ] || fail "阶段9d: 事件表条数=${FINAL_EVENT_COUNT}, want 至少 5（五个信号各至少一条事件），响应=${FINAL_EVENTS}"
pass "阶段9d: 事件表留下完整时间线（${FINAL_EVENT_COUNT} 条事件）"

echo ""
echo "=== 完整状态链（task_id=${TASK2_ID}，ESXi/UEFI 家族） ==="
echo "pending → booting（/v1/boot 命中签 token，机器收到真实 ESXi 菜单 mboot.efi -c boot.cfg） →"
echo "  installing（/v1/answer 渲染出 ESXi kickstart 正文——不是改写后的 boot.cfg，见 findings） →"
echo "  pre-start（install token 验证）→ [booted → init-start → init-done → ready]（重启后四信号，%firstboot 原生发出） → done"
echo ""
echo "各阶段验证结果："
db_task_summary "${TASK2_ID}"
echo ""
echo "=== ESXi 家族九阶段验证结束：分派已接线，菜单形态/固件硬拒绝/信号通道/状态机/终态收敛全部验证通过。" \
	"两处此前记录的 Go 侧缺口都已修复：1) boot.cfg 现在返回经三处改写的引导参数文件
#     （gateway 层读原始模板 + 注入的 RenderESXiBootCfg），ks.cfg 返回 kickstart 正文，
#     两条路径各自有断言；2) ESXi 的导入封锁已解除（identify.go 返回 nil，job.go 有
#     runESXi 分派）。制品集仍经 sqlite3 造，原因是 MountExtractor 需要真实 ISO 文件 ==="
