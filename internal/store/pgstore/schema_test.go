package pgstore_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

// 表结构断言的测试：人为改坏一个全新的 schema，断言要查出那一项、报出它的名字。AutoMigrate 补得上的偏差（删掉的
// 索引与约束），重新 Open 就修好了；补不上的（同名的索引或约束定义变了、多出来的），重新 Open 要拒绝启动。

// execAll 在 s 的 schema 里依次执行语句。
func execAll(t *testing.T, s *pgstore.Store, sqls ...string) {
	t.Helper()
	for _, sql := range sqls {
		if err := s.DBForTest().WithContext(t.Context()).Exec(sql).Error; err != nil {
			t.Fatalf("执行 %s：%v", sql, err)
		}
	}
}

// mismatchesOf 对 s 的 schema 跑一遍断言（包内的 checkSchema），期望值是 want。
func mismatchesOf(t *testing.T, s *pgstore.Store, want []pgstore.TableForTest) []pgstore.MismatchForTest {
	t.Helper()
	ms, err := pgstore.CheckSchemaForTest(t.Context(), s.DBForTest(), want)
	if err != nil {
		t.Fatalf("断言表结构：%v", err)
	}
	return ms
}

// wantMismatches 要求查出的不一致恰好落在 items 上（「表 项」，按查出的顺序），并且每一条都写明了差在哪里、该怎么改。
func wantMismatches(t *testing.T, got []pgstore.MismatchForTest, items ...string) {
	t.Helper()
	var at []string
	for _, m := range got {
		tbl, item, problem := m.FieldsForTest()
		at = append(at, tbl+" "+item)
		if problem == "" || !strings.Contains(m.String(), item) {
			t.Errorf("不一致 %q 没写清是哪一项、差在哪里", m.String())
		}
	}
	if !slices.Equal(at, items) {
		t.Fatalf("查出的不一致落在 %q，应为 %q；全文：\n%s", at, items, pgstore.DescribeMismatchesForTest(got))
	}
}

// wantRefused 要求用 dsn 再次 Open 被拒绝，错误里写出了每个 item 与 fixes 里的每一种改法。
func wantRefused(t *testing.T, dsn string, items, fixes []string) {
	t.Helper()
	s, err := pgstore.Open(t.Context(), dsn)
	if err == nil {
		s.Close()
		t.Fatal("表结构不一致时 Open 应拒绝启动")
	}
	t.Logf("Open 拒绝启动：%v", err) // -v 时看得到运维会读到的原文
	for _, want := range slices.Concat(items, fixes) {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Open 的错误没写出 %q：%v", want, err)
		}
	}
}

func Test全新的schema断言通过(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest))
}

// 每次启动都跑 AutoMigrate：对已经建好的表，它不能改出偏差，断言也照样通过。
func Test再次Open同一个schema照样通过(t *testing.T) {
	dsn := pgtest.Schema(t)
	pgtest.OpenAt(t, dsn)
	s := pgtest.OpenAt(t, dsn)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest))
}

func Test删掉一个索引被查出来(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX idx_audit_log_actor`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "audit_log idx_audit_log_actor")
	// AutoMigrate 按名字补建缺了的索引，重新 Open 就修好了
	wantMismatches(t, mismatchesOf(t, pgtest.OpenAt(t, dsn), pgstore.TablesForTest))
}

// 旧系统的事件表因为复合索引漏标了一个列，把 (task_id, seq) 建成了 (seq)。同名的索引已经存在，AutoMigrate 不再动它。
func Test复合索引改建成单列被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX idx_audit_log_target`, `CREATE INDEX idx_audit_log_target ON audit_log (target_id)`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "audit_log idx_audit_log_target")
	wantRefused(t, dsn, []string{"idx_audit_log_target", "target_kind, target_id"}, []string{"DROP INDEX idx_audit_log_target"})
}

// 索引比的是列的集合与顺序：只换顺序、列还是那两个，也算不一致。(target_kind, target_id) 换成 (target_id, target_kind)
// 之后，只按 TargetKind 过滤审计的查询就用不上它了：复合索引只对开头的列管用。同名的索引已经存在，AutoMigrate 不再动它。
func Test复合索引的列换了顺序被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX idx_audit_log_target`, `CREATE INDEX idx_audit_log_target ON audit_log (target_id, target_kind)`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "audit_log idx_audit_log_target")
	wantRefused(t, dsn, []string{"idx_audit_log_target", "现在是 (target_id, target_kind)", "应为 (target_kind, target_id)"},
		[]string{"DROP INDEX idx_audit_log_target"})
}

func Test改掉WHERE被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX idx_isos_sha256`, `CREATE UNIQUE INDEX idx_isos_sha256 ON isos (sha256) WHERE sha256 <> ''`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "isos idx_isos_sha256")
	wantRefused(t, dsn, []string{"idx_isos_sha256", "state <> 'failed'::text"}, []string{"DROP INDEX idx_isos_sha256"})
}

func Test唯一索引变成不唯一被查出来(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	execAll(t, s, `DROP INDEX idx_isos_sha256`,
		`CREATE INDEX idx_isos_sha256 ON isos (sha256) WHERE sha256 <> '' AND state <> 'failed'`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "isos idx_isos_sha256")
}

func Test主键的列变了被查出来(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	execAll(t, s, `ALTER TABLE audit_log DROP CONSTRAINT audit_log_pkey`, `ALTER TABLE audit_log ADD PRIMARY KEY (id, at)`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "audit_log 主键")
}

// 主键同样比列的顺序：task_events 的 (task_id, seq) 换成 (seq, task_id)，唯一性不变，按任务续读事件（ListEvents 按 task_id
// 取、按 seq 排）却用不上它了。AutoMigrate 不改已有的主键，重新 Open 要拒绝启动。
func Test主键的列换了顺序被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `ALTER TABLE task_events DROP CONSTRAINT task_events_pkey`, `ALTER TABLE task_events ADD PRIMARY KEY (seq, task_id)`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "task_events 主键")
	wantRefused(t, dsn, []string{"task_events 主键", "是 (seq, task_id)", "应为 (task_id, seq)"},
		[]string{"ALTER TABLE task_events DROP CONSTRAINT task_events_pkey", "ALTER TABLE task_events ADD PRIMARY KEY (task_id, seq)"})
}

// 主键还要比名字：translate 按 duplicateFields 里登记的 hosts_pkey 认出 SN 撞车。主键被手工改了名，列照旧，Open 要是照样
// 通过，撞上已登记的 SN 就只报出原样的 23505，不是 DuplicateError。AutoMigrate 不改已有约束的名字，重新 Open 要拒绝启动，
// 并写明怎么改回来。
func Test主键改了名被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `ALTER TABLE hosts RENAME CONSTRAINT hosts_pkey TO hosts_sn_key`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "hosts 主键")
	wantRefused(t, dsn, []string{"hosts 主键", "hosts_sn_key", "应为 hosts_pkey"},
		[]string{"ALTER TABLE hosts RENAME CONSTRAINT hosts_sn_key TO hosts_pkey"})
}

// models 里的行类型与 tables 的期望值要一一对应（open.go 的 models 注释）：AutoMigrate 按 models 建表，断言只查 tables 列出的
// 表。往 models 里加了行类型却没登记期望值，那张表的主键、索引、外键与 CHECK 就都不在断言的范围里，AutoMigrate 的两个盲区
// 又没人守了；反过来，tables 里多出一张 models 没有的表，Open 建不出它，断言只会报它缺了主键。第 4、5 层还要加表。
func Test行类型与表结构的期望值一一对应(t *testing.T) {
	var fromModels, fromTables []string
	for _, m := range pgstore.ModelsForTest {
		named, ok := m.(interface{ TableName() string })
		if !ok {
			t.Fatalf("行类型 %T 没有 TableName：表名要显式写出，不随 GORM 的命名规则变", m)
		}
		fromModels = append(fromModels, named.TableName())
	}
	for _, tb := range pgstore.TablesForTest {
		fromTables = append(fromTables, tb.NameForTest())
	}
	slices.Sort(fromModels)
	slices.Sort(fromTables)
	if !slices.Equal(fromModels, fromTables) {
		t.Fatalf("models 的表是 %q，tables 的期望值是 %q：两边要一一对应", fromModels, fromTables)
	}
}

func Test改掉CHECK被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `ALTER TABLE hosts DROP CONSTRAINT chk_hosts_rev`, `ALTER TABLE hosts ADD CONSTRAINT chk_hosts_rev CHECK (rev >= 0)`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "hosts chk_hosts_rev")
	wantRefused(t, dsn, []string{"chk_hosts_rev", "CHECK (rev >= 1)"}, []string{"ALTER TABLE hosts DROP CONSTRAINT chk_hosts_rev"})
}

func Test少了CHECK被查出来(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `ALTER TABLE isos DROP CONSTRAINT chk_isos_state`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "isos chk_isos_state")
	wantMismatches(t, mismatchesOf(t, pgtest.OpenAt(t, dsn), pgstore.TablesForTest)) // AutoMigrate 按名字补建
}

// 本层的表上只能有期望值列出的索引与约束：多出来的多半是手工建的，或者行类型加了 tag 却没登记期望值。
func Test多出来的索引与约束也算不一致(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `CREATE INDEX idx_hosts_arch ON hosts (arch)`, `ALTER TABLE hosts ADD CONSTRAINT chk_hosts_sn CHECK (sn <> '')`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "hosts idx_hosts_arch", "hosts chk_hosts_sn")
	wantRefused(t, dsn, []string{"idx_hosts_arch", "chk_hosts_sn"},
		[]string{"DROP INDEX idx_hosts_arch", "ALTER TABLE hosts DROP CONSTRAINT chk_hosts_sn"})
}

// 外键比对的几种偏差（列、被引用的表与列、删除时的动作）用几张临时的表造：被引用的表要能换成一张同构的，才查得出比的
// 是被引用的表本身。
func Test外键缺了或者变了被查出来(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	execAll(t, s,
		`CREATE TABLE probe_parent (id text, version bigint, PRIMARY KEY (id, version))`,
		`CREATE TABLE probe_other (id text, version bigint, PRIMARY KEY (id, version))`,
		`CREATE TABLE probe_child (id text PRIMARY KEY, parent_id text NOT NULL, parent_version bigint NOT NULL,
			CONSTRAINT fk_probe_child_parent FOREIGN KEY (parent_id, parent_version)
			REFERENCES probe_parent (id, version) ON DELETE RESTRICT)`)
	want := append(slices.Clone(pgstore.TablesForTest),
		pgstore.NewTableForTest("probe_parent", []string{"id", "version"}),
		pgstore.NewTableForTest("probe_other", []string{"id", "version"}),
		pgstore.NewTableForTest("probe_child", []string{"id"}, pgstore.NewForeignForTest(
			"fk_probe_child_parent", []string{"parent_id", "parent_version"}, "probe_parent", []string{"id", "version"}, "RESTRICT")))
	wantMismatches(t, mismatchesOf(t, s, want))

	for _, c := range []struct {
		name string
		fk   string // 换上的外键，空表示去掉
	}{
		{"去掉", ""},
		{"删除时的动作变了", `FOREIGN KEY (parent_id, parent_version) REFERENCES probe_parent (id, version) ON DELETE CASCADE`},
		{"被引用的表变了", `FOREIGN KEY (parent_id, parent_version) REFERENCES probe_other (id, version) ON DELETE RESTRICT`},
		{"列的顺序变了", `FOREIGN KEY (parent_version, parent_id) REFERENCES probe_parent (version, id) ON DELETE RESTRICT`},
	} {
		t.Run(c.name, func(t *testing.T) {
			execAll(t, s, `ALTER TABLE probe_child DROP CONSTRAINT IF EXISTS fk_probe_child_parent`)
			if c.fk != "" {
				execAll(t, s, `ALTER TABLE probe_child ADD CONSTRAINT fk_probe_child_parent `+c.fk)
			}
			wantMismatches(t, mismatchesOf(t, s, want), "probe_child fk_probe_child_parent")
		})
	}
}

// 断言只看当前 schema：几个代理同时跑测试，库里有许多同名的表，别的 schema 里的偏差不算这一个的。索引与约束分两条
// 查询取，两条都要限定 schema：b 里两样各多一个、少一个，约束还改坏一个，a 照样一致；b 报的都是自己的，少了的约束
// 也不会被 a 里同名的那个掩盖。
func Test只看当前schema(t *testing.T) {
	a := pgtest.OpenAt(t, pgtest.Schema(t))
	b := pgtest.OpenAt(t, pgtest.Schema(t))
	execAll(t, b, `CREATE INDEX idx_hosts_arch ON hosts (arch)`, `DROP INDEX idx_audit_log_at`,
		`ALTER TABLE hosts ADD CONSTRAINT chk_hosts_sn CHECK (sn <> '')`,
		`ALTER TABLE hosts DROP CONSTRAINT chk_hosts_rev`, `ALTER TABLE hosts ADD CONSTRAINT chk_hosts_rev CHECK (rev >= 0)`,
		`ALTER TABLE isos DROP CONSTRAINT chk_isos_state`)
	wantMismatches(t, mismatchesOf(t, a, pgstore.TablesForTest))
	wantMismatches(t, mismatchesOf(t, b, pgstore.TablesForTest), "hosts idx_hosts_arch", "hosts chk_hosts_rev", "hosts chk_hosts_sn",
		"isos chk_isos_state", "audit_log idx_audit_log_at")
}

// 「每个身份同时最多一个未审批的版本」靠的是部分唯一索引。它被改成全表唯一时，一个身份就只能有一个版本：版本 1 审批
// 之后，再也开不了新版本。同名的索引已经存在，AutoMigrate 不会改它。
func Test部分唯一索引改成全表唯一被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX idx_hook_versions_unapproved`,
		`CREATE UNIQUE INDEX idx_hook_versions_unapproved ON hook_versions (hook_id)`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "hook_versions idx_hook_versions_unapproved")
	wantRefused(t, dsn, []string{"idx_hook_versions_unapproved", "WHERE " + pgstore.UnapprovedForTest},
		[]string{"DROP INDEX idx_hook_versions_unapproved"})
}

// 删模板草稿时，它记着的 hook 引用要随它一起删掉（CASCADE）。外键的删除动作被改成 RESTRICT，草稿就删不掉了。
func Test模板引用的外键改了删除动作被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `ALTER TABLE template_hook_refs DROP CONSTRAINT fk_template_hook_refs_template`,
		`ALTER TABLE template_hook_refs ADD CONSTRAINT fk_template_hook_refs_template FOREIGN KEY (template_id, template_version)
			REFERENCES template_versions (template_id, version) ON DELETE RESTRICT`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "template_hook_refs fk_template_hook_refs_template")
	wantRefused(t, dsn, []string{"fk_template_hook_refs_template", "ON DELETE CASCADE"},
		[]string{"ALTER TABLE template_hook_refs DROP CONSTRAINT fk_template_hook_refs_template"})
}

// C31 靠 tasks 上的部分唯一索引：它不见了，同一台主机就能同时有两个未结束的任务，建任务与推进的兜底都跟着落空。
// AutoMigrate 按名字补建缺了的索引，重新 Open 就修好了。
func TestC31的部分唯一索引不见了被查出来(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX `+pgstore.OpenTaskIndexForTest)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "tasks "+pgstore.OpenTaskIndexForTest)
	wantMismatches(t, mismatchesOf(t, pgtest.OpenAt(t, dsn), pgstore.TablesForTest))
}

// 事件按 (task_id, key) 去重。它被建成只有 key 的单列索引时，不同任务里的同一个 Key 也会撞上：第 7 层给每个任务的信号
// 用同样的 Key，就只有第一个任务记得下来。同名的索引已经存在，AutoMigrate 不再动它。
func Test事件Key的唯一索引变成单列被查出来并拒绝启动(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	execAll(t, s, `DROP INDEX idx_task_events_key`, `CREATE UNIQUE INDEX idx_task_events_key ON task_events (key) WHERE key <> ''`)
	wantMismatches(t, mismatchesOf(t, s, pgstore.TablesForTest), "task_events idx_task_events_key")
	wantRefused(t, dsn, []string{"idx_task_events_key", "task_id, key"}, []string{"DROP INDEX idx_task_events_key"})
}

// 版本表的 CHECK 收下的状态要与 model 认的完全相同（VersionState.Valid 的注释），理由同 ISO 的状态。
func Test版本状态的CHECK与model一致(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	execAll(t, s, `INSERT INTO hooks (id, name, name_key, builtin, last_version) VALUES ('h', 'H', 'h', false, 1)`,
		`INSERT INTO templates (id, name, name_key, builtin, last_version) VALUES ('t', 'T', 't', false, 1)`)
	candidates := []model.VersionState{model.VersionDraft, model.VersionReview, model.VersionApproved,
		model.VersionDeprecated, "", "Approved", "rejected", "draft "}
	for _, c := range []struct{ chk, insert, remove string }{
		{"chk_hook_versions_state", `INSERT INTO hook_versions (hook_id, version, description, state, author, approver,
			builtin, params, timeout_sec, required, impls, rev) VALUES ('h', 1, '', ?, '', '', false, 'null', 0, false, 'null', 1)`,
			`DELETE FROM hook_versions`},
		{"chk_template_versions_state", `INSERT INTO template_versions (template_id, version, state, author, approver,
			builtin, derived_from, family, series, body, params, bindings, assembly, rev)
			VALUES ('t', 1, ?, '', '', false, 'null', '', 'null', '', 'null', 'null', 'null', 1)`,
			`DELETE FROM template_versions`},
	} {
		for _, state := range candidates {
			err := s.DBForTest().WithContext(t.Context()).Exec(c.insert, string(state)).Error
			var pe *pgconn.PgError
			rejected := errors.As(err, &pe) && pe.Code == "23514" && pe.ConstraintName == c.chk
			switch {
			case err != nil && !rejected:
				t.Fatalf("%s 写入状态 %q：%v", c.chk, state, err)
			case rejected == state.Valid():
				t.Errorf("%s 状态 %q：model 认不认是 %v，CHECK 拒不拒是 %v", c.chk, state, state.Valid(), rejected)
			}
			execAll(t, s, c.remove) // 写进去的删掉：同一个身份同时只能有一个未审批的版本
		}
	}
}

// isos 的 CHECK 收下的状态要与 model 认的完全相同（ISOState.Valid 的注释）：CHECK 少了一个，model 认的状态写不进库；
// 多了一个，坏数据就有了落脚处。
func TestISO状态的CHECK与model一致(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	candidates := []model.ISOState{model.ISOAcquiring, model.ISOImporting, model.ISOReady, model.ISOFailed,
		model.ISODeprecated, "", "Ready", "done", "importing "}
	for i, state := range candidates {
		err := s.DBForTest().WithContext(t.Context()).Exec(`INSERT INTO isos (id, name, family, series, version, arch, state, sha256, artifacts, offers, rev)
			VALUES (?, '', '', '', '', '', ?, '', 'null', 'null', 1)`, fmt.Sprintf("iso-%d", i), string(state)).Error
		var pe *pgconn.PgError
		rejected := errors.As(err, &pe) && pe.Code == "23514" && pe.ConstraintName == "chk_isos_state"
		switch {
		case err != nil && !rejected:
			t.Fatalf("写入状态 %q：%v", state, err)
		case rejected == state.Valid():
			t.Errorf("状态 %q：model 认不认是 %v，CHECK 拒不拒是 %v", state, state.Valid(), rejected)
		}
	}
}

// tasks 的 CHECK 收下的状态要与 model 认的完全相同（TaskState.Valid 的注释），理由同 ISO 的状态。C31 的部分唯一索引挡住的
// 也要恰好是 model 认为未结束的状态（TaskState.Finished）：同一台主机已有一个任务时再写一个 pending 的，前一个未结束才撞上。
// rebooting、initializing 这类契约测试推进不到的状态也在其中。
func Test任务状态的CHECK与C31的索引都与model一致(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	execAll(t, s, `INSERT INTO hosts (sn, arch, firmware, attrs, rev) VALUES ('SN-1', '', '', 'null', 1)`,
		`INSERT INTO isos (id, name, family, series, version, arch, state, sha256, artifacts, offers, rev)
			VALUES ('iso', '', '', '', '', '', 'ready', '', 'null', 'null', 1)`,
		`INSERT INTO templates (id, name, name_key, builtin, last_version) VALUES ('t', 'T', 't', false, 1)`,
		`INSERT INTO template_versions (template_id, version, state, author, approver, builtin, derived_from, family, series,
			body, params, bindings, assembly, rev)
			VALUES ('t', 1, 'approved', '', '', false, 'null', '', 'null', '', 'null', 'null', 'null', 1)`)
	insert := func(id string, state model.TaskState) error {
		return s.DBForTest().WithContext(t.Context()).Exec(`INSERT INTO tasks (id, host_sn, iso_id, template_id, template_version, state,
			snapshot, created_by, created_at, rev, last_seq) VALUES (?, 'SN-1', 'iso', 't', 1, ?, 'null', 'alice', 1, 1, 0)`,
			id, string(state)).Error
	}
	rejectedBy := func(err error, code, name string) bool {
		var pe *pgconn.PgError
		return errors.As(err, &pe) && pe.Code == code && pe.ConstraintName == name
	}
	// model 认的状态取自 model.TaskStates，不在这里另抄：model 加了状态，CHECK 与索引谓词没跟上，这里就报红（S9-1）
	candidates := slices.Concat(model.TaskStates(),
		[]model.TaskState{"", "Pending", "started", "done ", " booting", "canceled", "FAILED", "init"})
	for _, state := range candidates {
		err := insert("a", state)
		rejected := rejectedBy(err, "23514", "chk_tasks_state")
		switch {
		case err != nil && !rejected:
			t.Fatalf("写入状态 %q：%v", state, err)
		case rejected == state.Valid():
			t.Errorf("chk_tasks_state 状态 %q：model 认不认是 %v，CHECK 拒不拒是 %v", state, state.Valid(), rejected)
		}
		if err == nil {
			err = insert("b", model.TaskPending)
			taken := rejectedBy(err, "23505", pgstore.OpenTaskIndexForTest)
			switch {
			case err != nil && !taken:
				t.Fatalf("已有 %q 的任务时再写一个 pending 的：%v", state, err)
			case taken == state.Finished():
				t.Errorf("%s 在已有 %q 的任务时：model 说它结束了没有是 %v，索引挡没挡是 %v", pgstore.OpenTaskIndexForTest, state, state.Finished(), taken)
			}
		}
		execAll(t, s, `DELETE FROM tasks`)
	}
}

// 查「这台主机有没有未结束的任务」用的 finishedStates，要恰好是 model.TaskStates 里 Finished 为真的那些，顺序也相同（S9-1）：
// 它要是又写回手抄的清单，model 加了结束状态，FindOpenTask 与建任务时的那条查询就和 model 对不上了。
func Test查未结束任务用的结束状态由model的清单算出(t *testing.T) {
	var want []string
	for _, st := range model.TaskStates() {
		if st.Finished() {
			want = append(want, string(st))
		}
	}
	if got := pgstore.FinishedStatesForTest; len(want) == 0 || !slices.Equal(got, want) {
		t.Errorf("finishedStates 是 %q，应为 model.TaskStates 里 Finished 为真的 %q", got, want)
	}
}

// 撞上唯一约束时报 DuplicateError 的那张表，约束名要与期望值对得上：期望值改了名字，翻译跟着失效，撞了也只报原样
// 的数据库错误。
func Test唯一约束的翻译表与期望值一致(t *testing.T) {
	for name := range pgstore.DuplicateFieldsForTest {
		found := false
		for _, tb := range pgstore.TablesForTest {
			if name == tb.NameForTest()+"_pkey" {
				found = true
			}
			for ix, unique := range tb.IndexesForTest() {
				found = found || (ix == name && unique)
			}
		}
		if !found {
			t.Errorf("翻译表里的 %s 不是期望值里的主键或唯一索引", name)
		}
	}
}

func Test数据库错误的翻译(t *testing.T) {
	pg := func(code, constraint string) error {
		return fmt.Errorf("包了一层：%w", &pgconn.PgError{Code: code, ConstraintName: constraint})
	}
	holder := func(field string) string { return "已有的" + field }
	for _, c := range []struct {
		name   string
		err    error
		del    bool
		holder func(string) string
		want   error  // 契约的哨兵；nil 表示原样返回
		field  string // 翻成 DuplicateError 时的 Field
	}{
		{"主键撞了", pg("23505", "hosts_pkey"), false, holder, repository.ErrDuplicate, "sn"},
		{"SHA256 撞了", pg("23505", "idx_isos_sha256"), false, holder, repository.ErrDuplicate, "sha256"},
		{"hook 的名字撞了", pg("23505", "idx_hooks_name_key"), false, holder, repository.ErrDuplicate, "name"},
		{"模板的名字撞了", pg("23505", "idx_templates_name_key"), false, holder, repository.ErrDuplicate, "name"},
		// 约束认得，只是占着它的那一方在事务外查的时候刚好变了（导入失败、删掉、改了名）：重读之后必然是成功或
		// DuplicateError，所以报 ErrConflict，不报原样的数据库错误
		{"查不到占着 SHA256 的那一个报 ErrConflict", pg("23505", "idx_isos_sha256"), false, func(string) string { return "" }, repository.ErrConflict, ""},
		{"查不到占着名字的那一个报 ErrConflict", pg("23505", "idx_templates_name_key"), false, func(string) string { return "" }, repository.ErrConflict, ""},
		{"没给 holder 的写入撞上认得的唯一约束，原样返回", pg("23505", "hosts_pkey"), false, nil, nil, ""},
		{"认不出的唯一约束原样返回", pg("23505", "isos_pkey"), false, holder, nil, ""},
		{"删除时外键报 ErrReferenced", pg("23503", "fk_hook_versions_hook"), true, nil, repository.ErrReferenced, ""},
		{"写入时外键报 ErrNotFound", pg("23503", "fk_template_hook_refs_template"), false, nil, repository.ErrNotFound, ""},
		{"认不出的外键原样返回", pg("23503", "fk_nobody"), true, nil, nil, ""},
		{"删除主机时 RESTRICT 报 ErrReferenced", pg("23001", "fk_tasks_host"), true, nil, repository.ErrReferenced, ""},
		{"删除 ISO 时 RESTRICT 报 ErrReferenced", pg("23001", "fk_tasks_iso"), true, nil, repository.ErrReferenced, ""},
		{"非删除的 RESTRICT 原样返回", pg("23001", "fk_tasks_host"), false, nil, nil, ""},
		{"认不出的 RESTRICT 原样返回", pg("23001", "fk_nobody"), true, nil, nil, ""},
		{"CHECK 名的 RESTRICT 原样返回", pg("23001", "chk_isos_state"), true, nil, nil, ""},
		{"CHECK 报 ErrInvalid", pg("23514", "chk_isos_state"), false, nil, repository.ErrInvalid, ""},
		{"认不出的 CHECK 原样返回", pg("23514", "chk_nobody"), false, nil, nil, ""},
		{"别的数据库错误原样返回", pg("P0001", ""), false, holder, nil, ""},
		{"不是数据库错误原样返回", errors.New("连接断了"), false, holder, nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := pgstore.TranslateForTest(c.err, c.del, c.holder)
			if c.want == nil {
				if got != c.err {
					t.Fatalf("应原样返回 %v，得到 %v", c.err, got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Fatalf("应翻成 %v，得到 %v", c.want, got)
			}
			var de *repository.DuplicateError
			if errors.As(got, &de) != (c.field != "") {
				t.Fatalf("是不是 DuplicateError 不对：%v", got)
			}
			if de != nil && (de.Field != c.field || de.Existing != "已有的"+c.field) {
				t.Fatalf("DuplicateError 是 %+v，应为 Field %s、Existing 已有的%s", de, c.field, c.field)
			}
		})
	}
}
