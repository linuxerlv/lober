#!/usr/bin/env bash
# lib-db.sh 是 e2e 脚本共用的数据库辅助库：把「连哪个库、用哪个 schema、
# 怎么写一行制品集」这三件事收敛到一处，供 scripts/e2e*.sh 全部 source。
#
# ---------------------------------------------------------------------------
# 为什么需要这个文件
# ---------------------------------------------------------------------------
#
# 数据层在 2026-09-24 从 SQLite 迁到 GORM + PostgreSQL（commit 04ab93e），
# 但 7 个 e2e 脚本里的 101 处 `sqlite3` 调用一处都没跟上——**六个 OS 家族的
# 端到端验证因此全部跑不起来**，而那是 SPEC Phase 1 唯一的验收形态。
#
# 迁移不是把 sqlite3 换成 psql 那么简单，有三处实质变化：
#
#   1. 连接方式：一个文件路径 → 一组连接参数（host/port/user/dbname/schema）
#   2. schema 隔离：SQLite 靠「每次跑用一个新文件」天然隔离，PostgreSQL 要
#      显式建一个独立 schema，否则两次运行（或两个脚本并行）会互相看见对方
#      的任务与制品集，断言随机失败
#   3. **artifact_sets 的列变了**：paths_json 这一个 JSON 列被拆成四列
#      （paths_kernel / paths_initrd / paths_repo / paths_extra），前两个是
#      TEXT、后两个是 jsonb
#
# 第 3 点是把它做成共享库而不是逐脚本 sed 的直接理由：**同一条 schema 知识
# 散在 7 个脚本里，改一处另六处照旧**——本仓库刚因为同一个形状的问题吃过一次
# 亏（仓库可用性判据的常量被复制三份，Debian 装机 100% 卡死，见 commit
# 65883a5）。这里只留一份。
#
# ---------------------------------------------------------------------------
# 凭据从哪来
# ---------------------------------------------------------------------------
#
# 复用 **LOBER_TEST_PG_DSN**——Go 侧 store 测试用的就是这个环境变量
# （internal/store/postgres/pgtest/pgtest.go）。不另发明一个：两套凭据来源
# 意味着「Go 测试能跑但 e2e 跑不了」这种只差一个环境变量的困惑，而那种问题
# 往往要花掉比配一次环境更多的时间。
#
# 格式：postgres://user:pass@host:port/dbname?sslmode=disable
#
# 用法（在脚本顶部）：
#
#   . "$(dirname "$0")/e2e/lib-db.sh"
#   db_require                      # 校验 psql 与 DSN，解析出连接参数
#   db_schema_create                # 建一个本次运行专用的 schema
#   trap db_schema_drop EXIT        # 跑完（含失败）自动清理
#   ...
#   db_config_fragment >>"${CONFIG_PATH}"   # 往配置里写 store.postgres 段

# ---------------------------------------------------------------------------
# db_require：确认 psql 可用、解析 DSN
# ---------------------------------------------------------------------------
#
# psql 不在 PATH 上是本机的常态（PostgreSQL 17 装在 D:\pgsql，安装器不改
# PATH），因此按常见位置逐个探测而不是直接假设 PATH 里有。探测不到时报出
# 试过哪些位置——比一句「找不到 psql」更能让人一步定位。
db_require() {
	if [ -n "${PSQL:-}" ] && [ -x "${PSQL}" ]; then
		:
	elif command -v psql >/dev/null 2>&1; then
		PSQL="psql"
	else
		for cand in \
			/d/pgsql/bin/psql.exe \
			"/c/Program Files/PostgreSQL/17/bin/psql.exe" \
			"/c/Program Files/PostgreSQL/16/bin/psql.exe"; do
			if [ -x "${cand}" ]; then
				PSQL="${cand}"
				break
			fi
		done
	fi
	if [ -z "${PSQL:-}" ]; then
		echo "错误: 找不到 psql。已探测 PATH、/d/pgsql/bin、C:\\Program Files\\PostgreSQL\\{16,17}\\bin。" >&2
		echo "可用环境变量 PSQL 指定绝对路径后重试。" >&2
		exit 1
	fi

	if [ -z "${LOBER_TEST_PG_DSN:-}" ]; then
		echo "错误: 环境变量 LOBER_TEST_PG_DSN 未设置。" >&2
		echo "它与 Go 侧 store 测试用的是同一个变量（internal/store/postgres/pgtest）," >&2
		echo "格式 postgres://user:pass@host:5432/dbname?sslmode=disable" >&2
		echo "该账号需要 CREATE SCHEMA / DROP SCHEMA 权限（每次运行建一个独立 schema 做隔离）。" >&2
		exit 1
	fi

	db_parse_dsn
}

# db_parse_dsn 把 DSN 拆成 PG_* 变量。
#
# 用 shell 参数展开而不是调 python/sed 链：脚本的依赖清单里越少东西越好
# （本机没有 jq 就是这类问题的先例，见 e2e.sh 头部注释第 1 条）。
db_parse_dsn() {
	local dsn="${LOBER_TEST_PG_DSN}"
	dsn="${dsn#postgres://}"
	dsn="${dsn#postgresql://}"

	# 查询串（?sslmode=...）先切掉，单独取 sslmode。
	local query=""
	case "${dsn}" in
	*\?*)
		query="${dsn#*\?}"
		dsn="${dsn%%\?*}"
		;;
	esac
	PG_SSLMODE="disable"
	case "${query}" in
	*sslmode=*)
		PG_SSLMODE="${query#*sslmode=}"
		PG_SSLMODE="${PG_SSLMODE%%&*}"
		;;
	esac

	# userinfo@hostport/dbname
	local userinfo="" hostport=""
	case "${dsn}" in
	*@*)
		userinfo="${dsn%@*}"
		dsn="${dsn#*@}"
		;;
	esac
	hostport="${dsn%%/*}"
	PG_DBNAME="${dsn#*/}"

	PG_USER="${userinfo%%:*}"
	PG_PASSWORD=""
	case "${userinfo}" in
	*:*) PG_PASSWORD="${userinfo#*:}" ;;
	esac

	PG_HOST="${hostport%%:*}"
	PG_PORT="5432"
	case "${hostport}" in
	*:*) PG_PORT="${hostport#*:}" ;;
	esac

	if [ -z "${PG_HOST}" ] || [ -z "${PG_DBNAME}" ] || [ -z "${PG_USER}" ]; then
		echo "错误: LOBER_TEST_PG_DSN 解析后缺少 host/dbname/user（解析结果 host=${PG_HOST} db=${PG_DBNAME} user=${PG_USER}）" >&2
		exit 1
	fi
}

# ---------------------------------------------------------------------------
# schema 隔离
# ---------------------------------------------------------------------------

# db_schema_create 建一个本次运行专用的 schema，名字写进 PG_SCHEMA。
#
# 名字带脚本名与 PID：脚本名让 psql 里 `\dn` 的输出能看出是哪个家族的验证留下
# 的，PID 保证两次运行（或几个脚本并行）不会撞名。**SQLite 时代靠「每次跑用一个
# 新文件」天然隔离，PostgreSQL 必须显式做这件事**——不做的话两次运行会互相看见
# 对方的任务，而症状是「同一个 SN 已有进行中任务」这类看起来像代码 bug 的断言
# 失败。
#
# 不复用 Go 侧 pgtest 的 schema：那边每个测试用例建删一个，生命周期与 go test
# 绑定；这边的 schema 要活过整个脚本（服务进程启动时 AutoMigrate 建表，之后
# psql 与 HTTP 交替访问同一份数据）。
db_schema_create() {
	# printf 而不是 echo 管到 tr：echo 带的换行会被 tr -c 也换成下划线，
	# 于是名字里多一个（e2e_e2e__1817 那种双下划线）。不影响正确性，但那个
	# 多出来的下划线会让人以为名字拼接逻辑有 bug。
	local tag
	tag="$(printf '%s' "$(basename "$0" .sh)" | tr -c 'a-zA-Z0-9' '_')"
	PG_SCHEMA="${PG_SCHEMA:-e2e_${tag}_$$}"
	# 名字可能因脚本名较长而超过 PostgreSQL 的 63 字符标识符上限——超长会被
	# 静默截断，而截断后两个脚本可能撞名，于是又回到「互相看见对方数据」那个
	# 问题。这里主动截到 60 再拼 PID。
	if [ "${#PG_SCHEMA}" -gt 60 ]; then
		PG_SCHEMA="$(printf '%s' "${PG_SCHEMA}" | cut -c1-52)_$$"
	fi
	db_admin_exec "DROP SCHEMA IF EXISTS \"${PG_SCHEMA}\" CASCADE;"
	db_admin_exec "CREATE SCHEMA \"${PG_SCHEMA}\";"
	echo "[db] 本次运行使用 schema: ${PG_SCHEMA}" >&2
}

# db_schema_drop 删掉本次运行的 schema。适合挂在 trap EXIT 上。
#
# 失败不中断（|| true）：它通常跑在 trap 里，而 trap 中的失败会覆盖脚本真正的
# 退出码——清理失败不该让一次成功的验证报告成失败，也不该掩盖一次真实的失败。
db_schema_drop() {
	[ -n "${PG_SCHEMA:-}" ] || return 0
	db_admin_exec "DROP SCHEMA IF EXISTS \"${PG_SCHEMA}\" CASCADE;" >/dev/null 2>&1 || true
}

# db_admin_exec 在库级别（不设 search_path）执行一条语句，供建删 schema 用。
#
# client_min_messages=warning 压掉 NOTICE：`DROP SCHEMA IF EXISTS` 对不存在的
# schema 会打一行「注意: 模式 "xxx" 不存在」，那是正常路径（每次运行的 schema
# 名带 PID，本来就不该存在），但它出现在脚本输出最前面，看起来像是出了问题。
db_admin_exec() {
	PGPASSWORD="${PG_PASSWORD}" PGOPTIONS="-c client_min_messages=warning" "${PSQL}" \
		-h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d "${PG_DBNAME}" \
		-v ON_ERROR_STOP=1 -q -c "$1"
}

# ---------------------------------------------------------------------------
# 查询与执行（sqlite3 CLI 的替代）
# ---------------------------------------------------------------------------

# search_path 通过 PGOPTIONS 在**连接建立时**设定，不作为一条 SQL 语句发。
#
# 这一点踩过一次：写成 `-c "SET search_path TO x; SELECT ..."` 时 psql 会把
# 两条语句各自的输出都打出来，于是 db_value 返回的是两行——第一行 "SET"、第二
# 行才是真值。调用方拿到的 `$(db_value ...)` 变成 "SET\n42"，与 grep -qx 这类
# 精确比较全部对不上，而错误信息里看到的是一个莫名多出 SET 的值。
db_pg_env() {
	PGPASSWORD="${PG_PASSWORD}"
	PGOPTIONS="-c search_path=${PG_SCHEMA}"
	export PGPASSWORD PGOPTIONS
}

# db_value 执行一条 SELECT 并返回单个值，等价于旧脚本里的
# `$(sqlite3 "${DB_PATH}" "SELECT ...")`。
#
# -At 是关键的两个 flag：-A 关掉对齐（否则值两侧带补齐空格，与 grep -qx 这类
# 精确比较对不上），-t 只出数据行不出表头。查不到行时输出空串——与 sqlite3 对
# 空结果集的行为一致，调用方现有的空值判断不需要改。
#
# tr -d '\r' 收拾 Windows 换行：psql.exe 在 MSYS bash 下输出 CRLF，而 \r 会
# 留在变量末尾，让 [ "$x" = "ready" ] 这种比较无声失败。
db_value() {
	(
		db_pg_env
		"${PSQL}" -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d "${PG_DBNAME}" \
			-v ON_ERROR_STOP=1 -At -c "$1" 2>/dev/null
	) | tr -d '\r'
}

# db_exec 执行一条写语句（UPDATE/INSERT/DELETE）。
#
# ON_ERROR_STOP=1 让 SQL 报错时 psql 以非零退出——不加的话 psql 打一行错误
# 然后正常退出，`set -e` 抓不到，脚本会带着「那条 UPDATE 其实没生效」继续往下
# 跑，最终在几十行之后以一个与真正原因无关的断言失败。
db_exec() {
	(
		db_pg_env
		"${PSQL}" -h "${PG_HOST}" -p "${PG_PORT}" -U "${PG_USER}" -d "${PG_DBNAME}" \
			-v ON_ERROR_STOP=1 -q -c "$1"
	)
}

# ---------------------------------------------------------------------------
# 配置片段
# ---------------------------------------------------------------------------

# db_config_fragment 输出 lober.yaml 的 store 段（含 postgres 子段）。
#
# 参数：$1 = artifact_path，$2 = log_path。
#
# max_open_conns 压到 4：e2e 脚本只有一个服务进程、请求是串行发的，生产默认的
# 25 条池纯属浪费——而本机 PostgreSQL 的 max_connections 是 100，几个脚本并行
# 跑时按生产默认值开池会打满（commit a8162e4 修的正是这个形状的问题，那次是
# go test 并行导致的）。
db_config_fragment() {
	cat <<EOF
store:
  postgres:
    host: "${PG_HOST}"
    port: ${PG_PORT}
    user: "${PG_USER}"
    password: "${PG_PASSWORD}"
    dbname: "${PG_DBNAME}"
    sslmode: "${PG_SSLMODE}"
    search_path: "${PG_SCHEMA}"
    max_open_conns: 4
  artifact_path: "$1"
  log_path: "$2"
EOF
}

# ---------------------------------------------------------------------------
# 业务写入：制品集与模板的状态推进
# ---------------------------------------------------------------------------
#
# 下面两个函数封装的是 e2e 脚本绕过 HTTP 直接写库的那两处「已知契约缺口」
# （登记制品集后没有真实导入管线把它推进到 ready；建模板后没有审批端点把它
# 推进到 active）。封装的理由不是省字数，是把 schema 知识收在一处——见文件头。

# db_artifact_ready 把一个制品集推进到 ready 并写回家族/版本/架构/固件与四个
# 路径列。
#
# 参数：
#   $1 id            $2 os_family   $3 os_version  $4 arch   $5 firmware
#   $6 paths_kernel  $7 paths_initrd（JSON 数组文本）
#   $8 paths_repo    $9 paths_extra（JSON 对象文本，省略即 {}）
#
# **这里是 paths_json 拆成四列这件事的唯一落点。** 旧脚本写的是
#   UPDATE artifact_sets SET ... paths_json='{"Kernel":...,"Initrd":[...],...}'
# 而 PostgreSQL 侧的列是 paths_kernel(TEXT) / paths_initrd(jsonb) /
# paths_repo(TEXT) / paths_extra(jsonb)——GORM tag 是 schema 真源，见
# internal/store/postgres/artifact/row.go。
#
# Windows 与 ESXi 的制品集没有内核与仓库（只产出 Extra 里的一组文件），对它们
# 传空串即可：那与真实抽取器的产出一致（见 imports/extract_esxi.go），不是凑数。
db_artifact_ready() {
	local id="$1" family="$2" version="$3" arch="$4" firmware="$5"
	local kernel="$6" initrd="$7" repo="$8" extra="${9:-{\}}"

	db_exec "UPDATE artifact_sets SET
		state='ready',
		os_family='${family}',
		os_version='${version}',
		arch='${arch}',
		firmware='${firmware}',
		paths_kernel='${kernel}',
		paths_initrd='${initrd}'::jsonb,
		paths_repo='${repo}',
		paths_extra='${extra}'::jsonb
	WHERE id='${id}';"

	local got
	got="$(db_value "SELECT state FROM artifact_sets WHERE id='${id}';")"
	if [ "${got}" != "ready" ]; then
		echo "错误: 制品集 ${id} 未能推进到 ready（当前=${got}）" >&2
		return 1
	fi
}

# db_template_active 把一个模板推进到 active。
#
# 模板主键是 (id, version) 复合键（见 postgres/template/row.go），而脚本建的
# 模板只有 version=1 一版——不带 version 条件即可，但顺手做一次行数校验：
# 若将来脚本改成建多版本，这里会立刻红而不是静默把两版都激活。
db_template_active() {
	local id="$1"
	db_exec "UPDATE templates SET state='active' WHERE id='${id}';"

	local got
	got="$(db_value "SELECT state FROM templates WHERE id='${id}' AND version=1;")"
	if [ "${got}" != "active" ]; then
		echo "错误: 模板 ${id} 未能推进到 active（当前=${got}）" >&2
		return 1
	fi
}

# db_task_field 取任务的某一列，供脚本断言状态机推进。
# 用法：db_task_field "${TASK_ID}" state / finalize_token / attempt_count ...
db_task_field() {
	db_value "SELECT ${2} FROM tasks WHERE id='${1}';"
}

# db_task_summary 打印任务的四个关键字段，供脚本在失败时输出诊断信息。
# 替代旧脚本里那条用 SQLite 的 || 拼接字符串的 SELECT（PostgreSQL 的 || 对
# 整数列不会隐式转字符串，原样搬过来会报类型错误——这是迁移时最容易踩的一处）。
db_task_summary() {
	db_value "SELECT '  state=' || state
		|| ' current_phase=' || current_phase
		|| ' attempt_count=' || attempt_count::text
		|| ' last_seq=' || last_seq::text
	FROM tasks WHERE id='${1}';"
}
