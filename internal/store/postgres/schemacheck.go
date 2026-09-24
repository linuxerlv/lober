// 本文件实现 verifyIndexes：AutoMigrate 之后校验四个唯一索引没有静默
// 退化，对应实现计划任务 6，依据设计文档 §6。
//
// 为什么必须校验，不能只信任 tag。GORM 的 index tag 解析对「WHERE 子句
// 含逗号需要反斜杠转义」这类写法没有校验：转义写错时 GORM 不报错，只是
// 把 WHERE 截断成半句或整条丢掉，索引退化成无条件唯一索引。这不会让任何
// 业务测试失败——症状是「装完的机器再也不能重装」（终态任务开始占用
// host_sn 的唯一约束）或「第二个未签发 token 的任务建不出来」（空串撞
// 唯一约束）。这两个症状都只在特定业务场景下才会暴露，且暴露时看起来
// 像是业务逻辑 bug，不会让人第一时间怀疑到索引定义——必须在启动时用一次
// 结构断言直接拦住，不能依赖「迟早会有测试覆盖到这个场景」的运气。
//
// 比较方式基于实测。PostgreSQL 会把 WHERE 条件规范化输出（indexdef 是
// 数据库重新生成的文本，不是把 CREATE INDEX 语句原样存起来）：
//   - `!=` 规范化为 `<>`
//   - 每个比较操作数加类型标注，如空字符串字面量变成 ""::text
//   - `NOT IN (a, b)` 规范化为 `<> ALL (ARRAY[a, b])`
//   - 每个比较表达式外层加一对括号
//
// 本文件的四个 wantWhere 字面量是照着 TestProbeIndexDef 探针在真实
// PostgreSQL 15 上跑出的实际 indexdef 输出抄录的（探针跑法：对 tasks/
// task_events 建完表后查 pg_indexes.indexdef，用完即删，不保留在交付
// 物里），不是凭空猜测的正则。比较本身做「去空白后完全相等」，不做
// 语义等价的模糊匹配——四条 WHERE 都很短，字面相等足够，模糊匹配反而会
// 放过「看起来差不多但语义变了」的退化。
package postgres

import (
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/store/postgres/pgerr"
)

// indexExpectation 描述一个索引应当满足的形态：属于哪张表、覆盖哪些列、
// 是否唯一、WHERE 子句的规范化字面值（空字符串表示无 WHERE，对应普通
// 唯一索引）。
type indexExpectation struct {
	name      string
	table     string
	wantCols  string // 规范化后的列清单，如 "(task_id, seq)"，含括号与列序
	unique    bool
	wantWhere string // 空串表示无 WHERE；非空必须与规范化后的 indexdef 逐字符相等
}

// wantIndexes 是四个必须校验的唯一索引，字面值抄自 TestProbeIndexDef
// 在真实 PostgreSQL 上跑出的 indexdef（探针输出见本文件头注释）。
//
// 索引名用 pgerr 导出的常量而不是重复写字符串字面量——pgerr.Translate
// 按 ConstraintName 分派错误正依赖这两个名字，两处字面值不一致会让
// 那个分支静默失效（计划任务 2 的风险提示）。idx_tasks_token 与
// idx_tasks_finalize_token 这两个索引不参与 pgerr 的 sentinel 翻译
// （撞上时冒泡成通用错误，设计文档 §7 的错误映射表只列了另外两个），
// pgerr 因此没有为它们导出常量，此处用字面量。
// wantCols 一并校验：GORM 的复合索引靠「同名 index tag 出现在多个字段上」
// 加 priority 拼出来，只标一个字段就会静默建成单列索引，AutoMigrate 不报错。
// 这正是首版 event 包踩过的坑——idx_task_events_task_seq 漏标 TaskID，建成
// 了 UNIQUE (seq) 单列，导致任意两个任务并发装机时第二个任务的第一条事件
// 被误判为重复投递。WHERE 校验拦不住这一类退化（那个索引本来就无 WHERE），
// 必须比列集合。
var wantIndexes = []indexExpectation{
	{
		name:      pgerr.IdxTasksSNActive,
		table:     "tasks",
		wantCols:  "(host_sn)",
		unique:    true,
		wantWhere: `((state <> ALL (ARRAY['done'::text, 'failed'::text])) AND (host_sn <> ''::text))`,
	},
	{
		name:      "idx_tasks_token",
		table:     "tasks",
		wantCols:  "(token)",
		unique:    true,
		wantWhere: `(token <> ''::text)`,
	},
	{
		name:      "idx_tasks_finalize_token",
		table:     "tasks",
		wantCols:  "(finalize_token)",
		unique:    true,
		wantWhere: `(finalize_token <> ''::text)`,
	},
	{
		name:      pgerr.IdxTaskEventsTaskSeq,
		table:     "task_events",
		wantCols:  "(task_id, seq)",
		unique:    true,
		wantWhere: "", // 普通唯一索引，无 WHERE
	},
}

// pgIndexRow 对应 pg_indexes 的一行，只取本校验需要的三列。
type pgIndexRow struct {
	Indexname string
	Tablename string
	Indexdef  string
}

// verifyIndexes 查 pg_indexes.indexdef，对 wantIndexes 里的四个索引逐一
// 断言存在、属于正确的表、是 unique、且 WHERE 条件与期望的规范化字面值
// 一致。任何一条不满足都视为失败，错误信息汇总全部问题（而不是遇到
// 第一个问题就返回）——启动时一次性看到全部退化点，比排查时发现一个
// 修一个、每次重新连库验证要快。
func verifyIndexes(db *gorm.DB) error {
	var rows []pgIndexRow
	tables := make([]string, 0, len(wantIndexes))
	seen := map[string]bool{}
	for _, want := range wantIndexes {
		if !seen[want.table] {
			seen[want.table] = true
			tables = append(tables, want.table)
		}
	}

	// 必须显式按 schemaname = current_schema() 过滤。pg_indexes 是横跨
	// 全库全部 schema 的系统视图，不加这个条件时，若同一个数据库里另有
	// 一个 schema 恰好也建过同名的 tasks/task_events 表（每个测试用例都
	// 会用 pgtest 建一个新 schema，生产库也可能有历史遗留 schema），
	// 查询会把两个 schema 的同名索引一起返回，用索引名去 map 里找的
	// byName 只会保留其中一行——保留哪一行是 map 遍历顺序决定的，未
	// 定义、不可预测，实测会导致「明明在当前 schema 把索引删了，
	// verifyIndexes 却因为看见了别的 schema 里那份同名索引而误判通过」。
	// current_schema() 是当前连接 search_path 解析出的第一个 schema，
	// 与本连接实际操作的 schema 一致（GORM 的每次查询都在同一个连接
	// 会话上，search_path 在 pgtest.NewDB / Open 建库时已经设置好）。
	if err := db.Raw(
		`SELECT indexname, tablename, indexdef FROM pg_indexes
		 WHERE schemaname = current_schema() AND tablename IN (?)`,
		tables,
	).Scan(&rows).Error; err != nil {
		return fmt.Errorf("verifyIndexes: 查询 pg_indexes 失败: %w", err)
	}

	byName := make(map[string]pgIndexRow, len(rows))
	for _, r := range rows {
		byName[r.Indexname] = r
	}

	var problems []string
	for _, want := range wantIndexes {
		got, ok := byName[want.name]
		if !ok {
			problems = append(problems, fmt.Sprintf(
				"索引 %s 不存在（期望建在表 %s 上，unique=%v，WHERE=%q）",
				want.name, want.table, want.unique, want.wantWhere,
			))
			continue
		}
		if err := checkOneIndex(want, got); err != "" {
			problems = append(problems, err)
		}
	}

	if len(problems) > 0 {
		return fmt.Errorf("verifyIndexes: 发现 %d 处索引退化，可能导致「装完的机器再也不能重装」或「token 唯一约束失效」——\n  %s",
			len(problems), strings.Join(problems, "\n  "))
	}
	return nil
}

// checkOneIndex 校验单个索引的 indexdef 是否满足期望，返回非空字符串
// 表示发现的具体问题；返回空字符串表示通过。
func checkOneIndex(want indexExpectation, got pgIndexRow) string {
	def := got.Indexdef

	if !strings.HasPrefix(def, "CREATE UNIQUE INDEX") {
		if want.unique {
			return fmt.Sprintf(
				"索引 %s 期望是 unique，实际 indexdef 不是：%q",
				want.name, def,
			)
		}
	}

	// 列集合校验：indexdef 里列清单紧跟 "USING btree " 之后、WHERE 之前。
	// 比对含括号与列序的整段字面量——列序影响复合索引的可用性，(seq, task_id)
	// 与 (task_id, seq) 不是一回事。
	if gotCols := extractCols(def); normalizeWhitespace(gotCols) != normalizeWhitespace(want.wantCols) {
		return fmt.Sprintf(
			"索引 %s 的列集合不符——期望 %s，实际 %s（完整 indexdef: %q）。"+
				"复合索引必须每个参与列都标同名 index tag 并用 priority 定列序，只标一个字段会静默建成单列索引",
			want.name, want.wantCols, gotCols, def,
		)
	}

	gotWhere := extractWhere(def)
	gotWhereNorm := normalizeWhitespace(gotWhere)
	wantWhereNorm := normalizeWhitespace(want.wantWhere)

	if gotWhereNorm != wantWhereNorm {
		return fmt.Sprintf(
			"索引 %s 的 WHERE 条件不符——期望 %q，实际 %q（完整 indexdef: %q）",
			want.name, want.wantWhere, gotWhere, def,
		)
	}
	return ""
}

// extractWhere 从 indexdef 里取出 WHERE 关键字之后的部分。indexdef 形如
// `CREATE UNIQUE INDEX name ON schema.table USING btree (col) WHERE (...)`，
// 没有 WHERE 时整句里不含该关键字，返回空字符串。
func extractWhere(indexdef string) string {
	const marker = " WHERE "
	idx := strings.Index(indexdef, marker)
	if idx < 0 {
		return ""
	}
	return strings.TrimSpace(indexdef[idx+len(marker):])
}

// extractCols 取出 indexdef 里的列清单，含外层括号，形如 "(task_id, seq)"。
//
// PG 规范化后的 indexdef 形如：
//
//	CREATE UNIQUE INDEX 名 ON schema.表 USING btree (列, 列) WHERE (条件)
//
// 列清单紧跟 "USING btree " 之后，到 " WHERE " 或字符串结尾为止。找不到
// 索引方法标记时返回空串，调用方会因与期望不等而报出「列集合不符」并把
// 完整 indexdef 打进错误信息——比在这里猜一个默认值更容易定位。
func extractCols(indexdef string) string {
	const marker = " USING btree "
	idx := strings.Index(indexdef, marker)
	if idx < 0 {
		return ""
	}
	rest := indexdef[idx+len(marker):]
	if w := strings.Index(rest, " WHERE "); w >= 0 {
		rest = rest[:w]
	}
	return strings.TrimSpace(rest)
}

// normalizeWhitespace 把连续空白折叠成单个空格，两端去空白。indexdef
// 是数据库生成的规范化文本，理论上不会有多余空白，但比较前统一处理一次
// 能让本函数在空白差异上更宽容，不必依赖「PostgreSQL 输出永远不变」这个
// 假设。
func normalizeWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
