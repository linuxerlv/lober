package pgstore

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// 表结构断言的测试：人为改坏一个全新的 schema，断言要查出那一项、报出它的名字。AutoMigrate 补得上的偏差（删掉的
// 索引与约束），重新 Open 就修好了；补不上的（同名的索引或约束定义变了、多出来的），重新 Open 要拒绝启动。

// execAll 在 s 的 schema 里依次执行语句。
func execAll(t *testing.T, s *Store, sqls ...string) {
	t.Helper()
	for _, sql := range sqls {
		if err := s.db.WithContext(t.Context()).Exec(sql).Error; err != nil {
			t.Fatalf("执行 %s：%v", sql, err)
		}
	}
}

// mismatchesOf 对 s 的 schema 跑一遍断言，期望值是 want。
func mismatchesOf(t *testing.T, s *Store, want []table) []mismatch {
	t.Helper()
	ms, err := checkSchema(t.Context(), s.db, want)
	if err != nil {
		t.Fatalf("断言表结构：%v", err)
	}
	return ms
}

// wantMismatches 要求查出的不一致恰好落在 items 上（「表 项」，按查出的顺序），并且每一条都写明了差在哪里、该怎么改。
func wantMismatches(t *testing.T, got []mismatch, items ...string) {
	t.Helper()
	var at []string
	for _, m := range got {
		at = append(at, m.table+" "+m.item)
		if m.problem == "" || !strings.Contains(m.String(), m.item) {
			t.Errorf("不一致 %q 没写清是哪一项、差在哪里", m.String())
		}
	}
	if !slices.Equal(at, items) {
		t.Fatalf("查出的不一致落在 %q，应为 %q；全文：\n%s", at, items, describeMismatches(got))
	}
}

// wantRefused 要求用 dsn 再次 Open 被拒绝，错误里写出了每个 item 与 fixes 里的每一种改法。
func wantRefused(t *testing.T, dsn string, items, fixes []string) {
	t.Helper()
	s, err := Open(t.Context(), dsn)
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
	s := openStore(t, newSchema(t))
	wantMismatches(t, mismatchesOf(t, s, tables))
}

// 每次启动都跑 AutoMigrate：对已经建好的表，它不能改出偏差，断言也照样通过。
func Test再次Open同一个schema照样通过(t *testing.T) {
	dsn := newSchema(t)
	openStore(t, dsn)
	s := openStore(t, dsn)
	wantMismatches(t, mismatchesOf(t, s, tables))
}

func Test删掉一个索引被查出来(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	execAll(t, s, `DROP INDEX idx_audit_log_actor`)
	wantMismatches(t, mismatchesOf(t, s, tables), "audit_log idx_audit_log_actor")
	// AutoMigrate 按名字补建缺了的索引，重新 Open 就修好了
	wantMismatches(t, mismatchesOf(t, openStore(t, dsn), tables))
}

// 旧系统的事件表因为复合索引漏标了一个列，把 (task_id, seq) 建成了 (seq)。同名的索引已经存在，AutoMigrate 不再动它。
func Test复合索引改建成单列被查出来并拒绝启动(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	execAll(t, s, `DROP INDEX idx_audit_log_target`, `CREATE INDEX idx_audit_log_target ON audit_log (target_id)`)
	wantMismatches(t, mismatchesOf(t, s, tables), "audit_log idx_audit_log_target")
	wantRefused(t, dsn, []string{"idx_audit_log_target", "target_kind, target_id"}, []string{"DROP INDEX idx_audit_log_target"})
}

func Test改掉WHERE被查出来并拒绝启动(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	execAll(t, s, `DROP INDEX idx_isos_sha256`, `CREATE UNIQUE INDEX idx_isos_sha256 ON isos (sha256) WHERE sha256 <> ''`)
	wantMismatches(t, mismatchesOf(t, s, tables), "isos idx_isos_sha256")
	wantRefused(t, dsn, []string{"idx_isos_sha256", "state <> 'failed'::text"}, []string{"DROP INDEX idx_isos_sha256"})
}

func Test唯一索引变成不唯一被查出来(t *testing.T) {
	s := openStore(t, newSchema(t))
	execAll(t, s, `DROP INDEX idx_isos_sha256`,
		`CREATE INDEX idx_isos_sha256 ON isos (sha256) WHERE sha256 <> '' AND state <> 'failed'`)
	wantMismatches(t, mismatchesOf(t, s, tables), "isos idx_isos_sha256")
}

func Test主键的列变了被查出来(t *testing.T) {
	s := openStore(t, newSchema(t))
	execAll(t, s, `ALTER TABLE audit_log DROP CONSTRAINT audit_log_pkey`, `ALTER TABLE audit_log ADD PRIMARY KEY (id, at)`)
	wantMismatches(t, mismatchesOf(t, s, tables), "audit_log 主键")
}

func Test改掉CHECK被查出来并拒绝启动(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	execAll(t, s, `ALTER TABLE hosts DROP CONSTRAINT chk_hosts_rev`, `ALTER TABLE hosts ADD CONSTRAINT chk_hosts_rev CHECK (rev >= 0)`)
	wantMismatches(t, mismatchesOf(t, s, tables), "hosts chk_hosts_rev")
	wantRefused(t, dsn, []string{"chk_hosts_rev", "CHECK (rev >= 1)"}, []string{"ALTER TABLE hosts DROP CONSTRAINT chk_hosts_rev"})
}

func Test少了CHECK被查出来(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	execAll(t, s, `ALTER TABLE isos DROP CONSTRAINT chk_isos_state`)
	wantMismatches(t, mismatchesOf(t, s, tables), "isos chk_isos_state")
	wantMismatches(t, mismatchesOf(t, openStore(t, dsn), tables)) // AutoMigrate 按名字补建
}

// 本层的表上只能有期望值列出的索引与约束：多出来的多半是手工建的，或者行类型加了 tag 却没登记期望值。
func Test多出来的索引与约束也算不一致(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	execAll(t, s, `CREATE INDEX idx_hosts_arch ON hosts (arch)`, `ALTER TABLE hosts ADD CONSTRAINT chk_hosts_sn CHECK (sn <> '')`)
	wantMismatches(t, mismatchesOf(t, s, tables), "hosts idx_hosts_arch", "hosts chk_hosts_sn")
	wantRefused(t, dsn, []string{"idx_hosts_arch", "chk_hosts_sn"},
		[]string{"DROP INDEX idx_hosts_arch", "ALTER TABLE hosts DROP CONSTRAINT chk_hosts_sn"})
}

// 本任务的三张表没有外键（任务 8、9 的表才有），这里用两张临时的表查外键的比对：列、被引用的表与列、删除时的动作。
func Test外键缺了或者变了被查出来(t *testing.T) {
	s := openStore(t, newSchema(t))
	execAll(t, s,
		`CREATE TABLE probe_parent (id text, version bigint, PRIMARY KEY (id, version))`,
		`CREATE TABLE probe_other (id text, version bigint, PRIMARY KEY (id, version))`,
		`CREATE TABLE probe_child (id text PRIMARY KEY, parent_id text NOT NULL, parent_version bigint NOT NULL,
			CONSTRAINT fk_probe_child_parent FOREIGN KEY (parent_id, parent_version)
			REFERENCES probe_parent (id, version) ON DELETE RESTRICT)`)
	want := append(slices.Clone(tables),
		table{name: "probe_parent", primary: []string{"id", "version"}},
		table{name: "probe_other", primary: []string{"id", "version"}},
		table{name: "probe_child", primary: []string{"id"}, foreign: []foreign{{
			name: "fk_probe_child_parent", columns: []string{"parent_id", "parent_version"},
			refTable: "probe_parent", refColumns: []string{"id", "version"}, onDelete: "RESTRICT",
		}}})
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
	a := openStore(t, newSchema(t))
	b := openStore(t, newSchema(t))
	execAll(t, b, `CREATE INDEX idx_hosts_arch ON hosts (arch)`, `DROP INDEX idx_audit_log_at`,
		`ALTER TABLE hosts ADD CONSTRAINT chk_hosts_sn CHECK (sn <> '')`,
		`ALTER TABLE hosts DROP CONSTRAINT chk_hosts_rev`, `ALTER TABLE hosts ADD CONSTRAINT chk_hosts_rev CHECK (rev >= 0)`,
		`ALTER TABLE isos DROP CONSTRAINT chk_isos_state`)
	wantMismatches(t, mismatchesOf(t, a, tables))
	wantMismatches(t, mismatchesOf(t, b, tables), "hosts idx_hosts_arch", "hosts chk_hosts_rev", "hosts chk_hosts_sn",
		"isos chk_isos_state", "audit_log idx_audit_log_at")
}

// isos 的 CHECK 收下的状态要与 model 认的完全相同（ISOState.Valid 的注释）：CHECK 少了一个，model 认的状态写不进库；
// 多了一个，坏数据就有了落脚处。
func TestISO状态的CHECK与model一致(t *testing.T) {
	s := openStore(t, newSchema(t))
	candidates := []model.ISOState{model.ISOAcquiring, model.ISOImporting, model.ISOReady, model.ISOFailed,
		model.ISODeprecated, "", "Ready", "done", "importing "}
	for i, state := range candidates {
		err := s.db.WithContext(t.Context()).Exec(`INSERT INTO isos (id, name, family, series, version, arch, state, sha256, artifacts, offers, rev)
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

// 撞上唯一约束时报 DuplicateError 的那张表，约束名要与期望值对得上：期望值改了名字，翻译跟着失效，撞了也只报原样
// 的数据库错误。
func Test唯一约束的翻译表与期望值一致(t *testing.T) {
	for name := range duplicateFields {
		found := false
		for _, tb := range tables {
			if name == tb.name+"_pkey" {
				found = true
			}
			for _, ix := range tb.indexes {
				found = found || (ix.name == name && ix.unique)
			}
		}
		if !found {
			t.Errorf("翻译表里的 %s 不是期望值里的主键或唯一索引", name)
		}
	}
}

func Test数据库错误的翻译(t *testing.T) {
	// 本任务的表没有外键，借一份带外键的期望值查 23503 的翻译。它换掉了包级的 tables，不能与别的测试并行
	old := tables
	tables = append(slices.Clone(tables), table{name: "probe_child", foreign: []foreign{{name: "fk_probe_child_parent"}}})
	t.Cleanup(func() { tables = old })

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
		{"主键撞了", pg("23505", "hosts_pkey"), false, holder, store.ErrDuplicate, "sn"},
		{"SHA256 撞了", pg("23505", "idx_isos_sha256"), false, holder, store.ErrDuplicate, "sha256"},
		{"查不到已有的那一个，原样返回", pg("23505", "idx_isos_sha256"), false, func(string) string { return "" }, nil, ""},
		{"认不出的唯一约束原样返回", pg("23505", "isos_pkey"), false, holder, nil, ""},
		{"删除时外键报 ErrReferenced", pg("23503", "fk_probe_child_parent"), true, nil, store.ErrReferenced, ""},
		{"写入时外键报 ErrNotFound", pg("23503", "fk_probe_child_parent"), false, nil, store.ErrNotFound, ""},
		{"认不出的外键原样返回", pg("23503", "fk_nobody"), true, nil, nil, ""},
		{"CHECK 报 ErrInvalid", pg("23514", "chk_isos_state"), false, nil, store.ErrInvalid, ""},
		{"认不出的 CHECK 原样返回", pg("23514", "chk_nobody"), false, nil, nil, ""},
		{"别的数据库错误原样返回", pg("P0001", ""), false, holder, nil, ""},
		{"不是数据库错误原样返回", errors.New("连接断了"), false, holder, nil, ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			got := translate(c.err, c.del, c.holder)
			if c.want == nil {
				if got != c.err {
					t.Fatalf("应原样返回 %v，得到 %v", c.err, got)
				}
				return
			}
			if !errors.Is(got, c.want) {
				t.Fatalf("应翻成 %v，得到 %v", c.want, got)
			}
			var de *store.DuplicateError
			if errors.As(got, &de) != (c.field != "") {
				t.Fatalf("是不是 DuplicateError 不对：%v", got)
			}
			if de != nil && (de.Field != c.field || de.Existing != "已有的"+c.field) {
				t.Fatalf("DuplicateError 是 %+v，应为 Field %s、Existing 已有的%s", de, c.field, c.field)
			}
		})
	}
}
