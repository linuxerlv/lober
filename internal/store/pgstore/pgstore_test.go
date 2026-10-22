package pgstore_test

import (
	"context"
	"errors"
	"fmt"
	"math/rand/v2"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/store/pgstore"
	"github.com/lober/lober/internal/store/pgstore/pgtest"
)

// pgstore 的测试都在外部测试包 pgstore_test 里，夹具只有 pgtest 一份：DSN 从哪读、临时 schema、每个仓储的连接上限、同时
// 在用的 schema 数、失败注入，规则都在那边。外部测试包才 import 得到它：pgtest 依赖 pgstore，包内测试再 import 它就成环。
// 白盒断言要用的包内标识符由 export_test.go 导出，名字都以 ForTest 结尾。测试库怎么准备见 doc.go。
//
// 本文件放只对 PostgreSQL 这份实现才有意义的测试：Open 本身，以及另开一个连接、让对方停在没提交的事务里才造得出的时机。
// 它们要 schema 的 DSN，用 pgtest.Schema 与 pgtest.OpenAt；契约用例用 pgtest.OpenStore。

// 失败注入只在 pgtest.OpenStore 给的仓储上：pgstore.Open 建的表上（pgtest.OpenAt 就是经它打开的），pgtest.FaultActor
// 只是一个普通的操作者。
func TestOpen不注入失败(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	a := repository.Audit{Actor: pgtest.FaultActor, Action: "host.create", At: 1_790_000_000}
	if _, err := s.CreateHost(t.Context(), model.Host{SN: "SN-1"}, a); err != nil {
		t.Fatalf("Open 建的表不该注入失败：%v", err)
	}
	p, err := s.ListAudit(t.Context(), repository.AuditQuery{})
	if err != nil {
		t.Fatalf("列出审计：%v", err)
	}
	if len(p.Items) != 1 || p.Items[0].Actor != pgtest.FaultActor {
		t.Fatalf("审计应写下这一条，得到 %+v", p.Items)
	}
}

// 连接池的上限由装配点从配置读出来、经 WithMaxConns 传给 Open（S7-7）；不传就用 database/sql 的默认值，不设上限。
// 几个选项都传时，后传的为准。
func TestOpen按选项设连接池上限(t *testing.T) {
	dsn := pgtest.Schema(t)
	for _, c := range []struct {
		name string
		opts []pgstore.Option
		want int // sql.DBStats.MaxOpenConnections，0 表示不设上限
	}{
		{"不传", nil, 0},
		{"传 3", []pgstore.Option{pgstore.WithMaxConns(3)}, 3},
		{"后传的为准", []pgstore.Option{pgstore.WithMaxConns(3), pgstore.WithMaxConns(5)}, 5},
	} {
		t.Run(c.name, func(t *testing.T) {
			s, err := pgstore.Open(t.Context(), dsn, c.opts...)
			if err != nil {
				t.Fatalf("Open：%v", err)
			}
			defer s.Close()
			sqlDB, err := s.DBForTest().DB()
			if err != nil {
				t.Fatalf("取连接池：%v", err)
			}
			if got := sqlDB.Stats().MaxOpenConnections; got != c.want {
				t.Fatalf("连接池的上限 = %d，应为 %d", got, c.want)
			}
		})
	}
}

// 游标里藏着存不下的串（PostgreSQL 的 text 不收 NUL）：仓储给不出这样的游标，认不出来（repository.DecodeCursor），报
// ErrInvalid，不把数据库的编码错误交给调用方。每个带游标的列表都试一遍，NUL 放在排序键的不同位置；审计的排序键只有整数，
// 藏不进串。契约测试里的坏游标是解不开的串与排序键为 null 的，排序键里藏着 NUL 的只在这里。按存不下的键取、删与过滤
// 当作没有，见契约测试（hostUnstorableKeys 与 ISO、任务、审计各组列表的过滤用例）。
func Test游标里藏着存不下的串报ErrInvalid(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	ctx := t.Context()
	for _, c := range []struct {
		name string
		list func() error
	}{
		{"主机", func() error {
			_, err := s.ListHosts(ctx, repository.HostQuery{Cursor: repository.EncodeCursor("hosts", "SN\x00")})
			return err
		}},
		{"ISO", func() error {
			_, err := s.ListISOs(ctx, repository.ISOQuery{Cursor: repository.EncodeCursor("isos", "a\x00", "b")})
			return err
		}},
		{"hook", func() error {
			_, err := s.ListHooks(ctx, repository.HookQuery{Cursor: repository.EncodeCursor("hooks", "lvm", "a\x00")})
			return err
		}},
		{"模板", func() error {
			_, err := s.ListTemplates(ctx, repository.TemplateQuery{Cursor: repository.EncodeCursor("templates", "\x00", "a")})
			return err
		}},
		{"任务", func() error {
			_, err := s.ListTasks(ctx, repository.TaskQuery{Cursor: repository.EncodeCursor("tasks", int64(1), "a\x00")})
			return err
		}},
	} {
		if err := c.list(); !errors.Is(err, repository.ErrInvalid) {
			t.Errorf("%s的游标藏着 NUL 应报 ErrInvalid，得到 %v", c.name, err)
		}
	}
}

// 两次系统更新同时给两个 ISO 写同一份内容时，事务里的预查看不到对方还没提交的写入，由部分唯一索引 idx_isos_sha256
// 兜底：后写的等先写的提交，再撞上索引。这时 UpdateISO 要在事务外查出对方，照样报 DuplicateError（C4），第 4 层靠它
// 把导入失败的 ISO 指向已有的那一个（spec §5.1 第 3 步）。契约测试里的撞车都被预查拦下了，走不到这条路；这里另开
// 一个连接，让 A 的写入停在没提交的事务里，造出这个时机。
func Test同时写同一份内容由索引兜底报出对方(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	newISO := func(name string) model.ISO {
		iso, err := s.CreateISO(ctx, model.ISO{Name: name, Family: "linux", Series: "rocky"},
			repository.Audit{Actor: "alice", Action: "iso.create", At: 1})
		if err != nil {
			t.Fatalf("新建 ISO %s：%v", name, err)
		}
		return iso
	}
	a, b := newISO("A"), newISO("B")
	sha := strings.Repeat("5a", 32)

	// A 的导入写下这份内容，事务先不提交
	holder := connect(t, dsn)
	tx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatalf("开事务：%v", err)
	}
	if _, err := tx.Exec(ctx, `UPDATE isos SET sha256 = $1, state = 'importing', rev = rev + 1 WHERE id = $2`, sha, a.ID); err != nil {
		t.Fatalf("A 写下内容：%v", err)
	}

	// B 写同一份内容：预查看不到 A 没提交的写入，UPDATE 卡在索引上，等 A 的事务结束
	in := b
	in.State, in.SHA256 = model.ISOImporting, sha
	done := make(chan error, 1)
	go func() {
		_, err := s.UpdateISO(ctx, in)
		done <- err
	}()
	waitBlockedBy(t, connect(t, dsn), holder.PgConn().PID(), done)

	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交 A 的事务：%v", err)
	}
	select {
	case err = <-done:
	case <-time.After(time.Minute):
		t.Fatal("A 提交之后，B 的系统更新一分钟内没有返回")
	}
	var de *repository.DuplicateError
	if !errors.As(err, &de) || de.Field != "sha256" || de.Existing != a.ID {
		t.Fatalf("B 撞上 A 已提交的内容，应报 DuplicateError{sha256, %s}，得到 %v", a.ID, err)
	}
	if got, err := s.GetISO(ctx, b.ID); err != nil || !reflect.DeepEqual(got, b) {
		t.Fatalf("撞上之后 B 应原样不动：得到 %+v、%v，应为 %+v", got, err, b)
	}
}

// 同时导入的几批互相重叠时，ImportHosts 按 SN 的顺序写，每批都按同一个顺序给行加锁，等待不会成环，也就不会互相等死
// （SQLSTATE 40P01）。分几轮，每轮几批同时导入同一组主机，每批打乱入参的顺序；按最终结果断言，不依赖调度的顺序。
func Test同时导入同一组主机不会互相等死(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	sqlDB, err := s.DBForTest().DB()
	if err != nil {
		t.Fatalf("取连接池：%v", err)
	}
	sqlDB.SetMaxIdleConns(sqlDB.Stats().MaxOpenConnections) // 连接留着不关，每轮开始时不必现建，几批才真的同时写
	const workers, rounds, n = 8, 5, 60
	const imports = workers * rounds
	hs := make([]model.Host, n)
	sns := make([]string, n)
	for i := range hs {
		sns[i] = fmt.Sprintf("SN-%03d", i)
		hs[i] = model.Host{SN: sns[i]}
	}
	errs := make(chan error, imports)
	for range rounds {
		// 一轮的几批同时开始，让它们加锁的过程尽量交错
		start := make(chan struct{})
		var wg sync.WaitGroup
		for range workers {
			batch := slices.Clone(hs)
			rand.Shuffle(len(batch), func(i, j int) { batch[i], batch[j] = batch[j], batch[i] })
			wg.Go(func() {
				<-start
				_, err := s.ImportHosts(t.Context(), batch, repository.Audit{Actor: "alice", Action: "host.import", At: 1})
				errs <- err
			})
		}
		close(start)
		wg.Wait()
	}
	close(errs)
	var failed []error
	for err := range errs {
		if err != nil {
			failed = append(failed, err)
		}
	}
	if len(failed) > 0 {
		t.Errorf("%d 次导入里有 %d 次失败（加锁的顺序不一致时是 SQLSTATE 40P01），第一次：%v", imports, len(failed), failed[0])
	}

	// 每次导入都给每台的 Rev 加 1（第一次是新建，Rev 为 1），每台每次写一条审计
	got, err := s.GetHosts(t.Context(), sns)
	if err != nil {
		t.Fatalf("读回主机：%v", err)
	}
	audits, total := make(map[string]int, n), 0
	for cursor := ""; ; {
		p, err := s.ListAudit(t.Context(), repository.AuditQuery{Cursor: cursor, Limit: 500})
		if err != nil {
			t.Fatalf("列出审计：%v", err)
		}
		for _, e := range p.Items {
			audits[e.TargetID]++
			total++
		}
		if cursor = p.Next; cursor == "" {
			break
		}
	}
	if total != n*imports {
		t.Errorf("审计有 %d 条，应为台数乘以导入的次数 %d", total, n*imports)
	}
	var wrong []string
	for _, sn := range sns {
		if got[sn].Rev != imports || audits[sn] != imports {
			wrong = append(wrong, fmt.Sprintf("%s 的 Rev 是 %d、审计有 %d 条", sn, got[sn].Rev, audits[sn]))
		}
	}
	if len(wrong) > 0 {
		t.Errorf("%d 台主机的 Rev 或审计条数不是导入的次数 %d，比如 %s", len(wrong), imports, wrong[0])
	}
}

// 主机与 ISO 的乐观锁靠 lockHost、lockISO 的 FOR UPDATE：随后的 UPDATE 与 DELETE 只按主键找行，不再比 rev。没有这把
// 锁，两次带着同一个 Rev 的写入都读到旧的 Rev，比对都能通过；后到的等先到的提交之后照样写入，把它覆盖掉，两边都返回
// 成功，Rev「对不上就写不进去」就落空了（spec §6.2 第 4 条）。契约测试里的 Rev 冲突都是串行的，钉不住这一点。这里另开
// 一个连接当先到的那一次：在没提交的事务里改这一行、让 Rev 加 1。带着旧 Rev 的每一种写入都要等它提交，再报
// ErrConflict，库里留着它写下的内容。
func Test带着旧Rev的写入等对方提交之后报冲突(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	audit := func(action string) repository.Audit { return repository.Audit{Actor: "alice", Action: action, At: 1} }

	for i, c := range []struct {
		name  string
		write func(ctx context.Context, h model.Host) error // 带着 h.Rev 写入
	}{
		{"UpdateHost", func(ctx context.Context, h model.Host) error {
			h.Firmware = "bios"
			_, err := s.UpdateHost(ctx, h, audit("host.update"))
			return err
		}},
		{"DeleteHost", func(ctx context.Context, h model.Host) error {
			return s.DeleteHost(ctx, h.SN, h.Rev, audit("host.delete"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			h, err := s.CreateHost(ctx, model.Host{SN: fmt.Sprintf("SN-%d", i+1), Arch: "x86_64", Firmware: "uefi"},
				audit("host.create"))
			if err != nil {
				t.Fatalf("登记：%v", err)
			}
			err = writeWhileHeld(t, dsn, `UPDATE hosts SET arch = 'aarch64', rev = rev + 1 WHERE sn = $1`, h.SN,
				func() error { return c.write(ctx, h) })
			if !errors.Is(err, repository.ErrConflict) {
				t.Fatalf("对方提交之后 Rev 已是 %d，带着 %d 写入应报 ErrConflict，得到 %v", h.Rev+1, h.Rev, err)
			}
			want := h
			want.Arch, want.Rev = "aarch64", h.Rev+1
			if got, err := s.GetHost(ctx, h.SN); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("库里应留着对方写下的 %+v，得到 %+v、%v", want, got, err)
			}
		})
	}

	for i, c := range []struct {
		name  string
		write func(ctx context.Context, iso model.ISO) error // 带着 iso.Rev 写入
	}{
		{"UpdateISO", func(ctx context.Context, iso model.ISO) error {
			iso.State = model.ISOFailed
			_, err := s.UpdateISO(ctx, iso)
			return err
		}},
		{"DeprecateISO", func(ctx context.Context, iso model.ISO) error {
			return s.DeprecateISO(ctx, iso.ID, iso.Rev, audit("iso.deprecate"))
		}},
		{"DeleteISO", func(ctx context.Context, iso model.ISO) error {
			return s.DeleteISO(ctx, iso.ID, iso.Rev, audit("iso.delete"))
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			iso, err := s.CreateISO(ctx, model.ISO{Name: c.name, Family: "linux", Series: "rocky"}, audit("iso.create"))
			if err != nil {
				t.Fatalf("新建 ISO：%v", err)
			}
			// 先置为 ready（只有 ready 的能废弃）；各个 ISO 的内容不同，不会撞上 idx_isos_sha256
			iso.Version, iso.Arch, iso.State, iso.SHA256 = "9.6", "x86_64", model.ISOReady, fmt.Sprintf("%064x", i+1)
			iso.Artifacts = map[string]string{"kernel": "rocky/9.6/x86_64/vmlinuz", "initrd": "rocky/9.6/x86_64/initrd.img"}
			if iso, err = s.UpdateISO(ctx, iso); err != nil {
				t.Fatalf("置为 ready：%v", err)
			}
			err = writeWhileHeld(t, dsn, `UPDATE isos SET arch = 'aarch64', rev = rev + 1 WHERE id = $1`, iso.ID,
				func() error { return c.write(ctx, iso) })
			if !errors.Is(err, repository.ErrConflict) {
				t.Fatalf("对方提交之后 Rev 已是 %d，带着 %d 写入应报 ErrConflict，得到 %v", iso.Rev+1, iso.Rev, err)
			}
			want := iso
			want.Arch, want.Rev = "aarch64", iso.Rev+1
			if got, err := s.GetISO(ctx, iso.ID); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("库里应留着对方写下的 %+v，得到 %+v、%v", want, got, err)
			}
		})
	}
}

// hook 与模板版本的乐观锁同样靠先对版本行加写锁，道理同上一个测试：另开一个连接当先到的那一次，在没提交的事务里改
// 这个版本、让 Rev 加 1。带着旧 Rev 的保存、状态转换与删除都要等它提交，再报 ErrConflict，库里留着它写下的内容。
func Test带着旧Rev写hook与模板版本的等对方提交之后报冲突(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	audit := func(action string) repository.Audit { return repository.Audit{Actor: "alice", Action: action, At: 1} }
	// setState 不经仓储把版本 1 置为 state：这里只看加锁与 Rev，不必让内容过激活判定
	setState := func(t *testing.T, table, owner, id string, state model.VersionState) {
		t.Helper()
		if err := s.DBForTest().WithContext(t.Context()).Exec("UPDATE "+table+" SET state = ? WHERE "+owner+" = ?", string(state), id).Error; err != nil {
			t.Fatalf("置为 %s：%v", state, err)
		}
	}

	for i, c := range []struct {
		name  string
		state model.VersionState // 写入之前版本所处的状态，这一步写入本来合规矩
		write func(ctx context.Context, h model.Hook) error
	}{
		{"SaveHookDraft", model.VersionDraft, func(ctx context.Context, h model.Hook) error {
			h.Description = "mine"
			_, err := s.SaveHookDraft(ctx, h)
			return err
		}},
		{"SubmitHook", model.VersionDraft, func(ctx context.Context, h model.Hook) error {
			return s.SubmitHook(ctx, h.Key(), h.Rev, audit("hook.submit"))
		}},
		{"ApproveHook", model.VersionReview, func(ctx context.Context, h model.Hook) error {
			return s.ApproveHook(ctx, h.Key(), h.Rev, "bob", audit("hook.approve"))
		}},
		{"RejectHook", model.VersionReview, func(ctx context.Context, h model.Hook) error {
			return s.RejectHook(ctx, h.Key(), h.Rev, audit("hook.reject"))
		}},
		{"DeprecateHook", model.VersionApproved, func(ctx context.Context, h model.Hook) error {
			return s.DeprecateHook(ctx, h.Key(), h.Rev, audit("hook.deprecate"))
		}},
		{"DeleteHookDraft", model.VersionDraft, func(ctx context.Context, h model.Hook) error {
			return s.DeleteHookDraft(ctx, h.Key(), h.Rev)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			h, err := s.CreateHook(ctx, model.Hook{Name: fmt.Sprintf("Hook-%d", i), Author: "alice"})
			if err != nil {
				t.Fatalf("新建 hook：%v", err)
			}
			setState(t, "hook_versions", "hook_id", h.ID, c.state)
			h.State = c.state
			err = writeWhileHeld(t, dsn, `UPDATE hook_versions SET description = 'held', rev = rev + 1 WHERE hook_id = $1`, h.ID,
				func() error { return c.write(ctx, h) })
			if !errors.Is(err, repository.ErrConflict) {
				t.Fatalf("对方提交之后 Rev 已是 %d，带着 %d 写入应报 ErrConflict，得到 %v", h.Rev+1, h.Rev, err)
			}
			want := h
			want.Description, want.Rev = "held", h.Rev+1
			if got, err := s.GetHook(ctx, h.Key()); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("库里应留着对方写下的 %+v，得到 %+v、%v", want, got, err)
			}
		})
	}

	for i, c := range []struct {
		name  string
		state model.VersionState
		write func(ctx context.Context, tpl model.Template) error
	}{
		{"SaveTemplateDraft", model.VersionDraft, func(ctx context.Context, tpl model.Template) error {
			tpl.Body = "mine"
			_, err := s.SaveTemplateDraft(ctx, tpl)
			return err
		}},
		{"SubmitTemplate", model.VersionDraft, func(ctx context.Context, tpl model.Template) error {
			return s.SubmitTemplate(ctx, templateKey(tpl), tpl.Rev, audit("template.submit"))
		}},
		{"ApproveTemplate", model.VersionReview, func(ctx context.Context, tpl model.Template) error {
			return s.ApproveTemplate(ctx, templateKey(tpl), tpl.Rev, "bob", audit("template.approve"))
		}},
		{"RejectTemplate", model.VersionReview, func(ctx context.Context, tpl model.Template) error {
			return s.RejectTemplate(ctx, templateKey(tpl), tpl.Rev, audit("template.reject"))
		}},
		{"DeprecateTemplate", model.VersionApproved, func(ctx context.Context, tpl model.Template) error {
			return s.DeprecateTemplate(ctx, templateKey(tpl), tpl.Rev, audit("template.deprecate"))
		}},
		{"DeleteTemplateDraft", model.VersionDraft, func(ctx context.Context, tpl model.Template) error {
			return s.DeleteTemplateDraft(ctx, templateKey(tpl), tpl.Rev)
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ctx := t.Context()
			tpl, err := s.CreateTemplate(ctx, model.Template{Name: fmt.Sprintf("Template-%d", i), Author: "alice"})
			if err != nil {
				t.Fatalf("新建模板：%v", err)
			}
			setState(t, "template_versions", "template_id", tpl.ID, c.state)
			tpl.State = c.state
			err = writeWhileHeld(t, dsn, `UPDATE template_versions SET body = 'held', rev = rev + 1 WHERE template_id = $1`, tpl.ID,
				func() error { return c.write(ctx, tpl) })
			if !errors.Is(err, repository.ErrConflict) {
				t.Fatalf("对方提交之后 Rev 已是 %d，带着 %d 写入应报 ErrConflict，得到 %v", tpl.Rev+1, tpl.Rev, err)
			}
			want := tpl
			want.Body, want.Rev = "held", tpl.Rev+1
			if got, err := s.GetTemplate(ctx, templateKey(tpl)); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("库里应留着对方写下的 %+v，得到 %+v、%v", want, got, err)
			}
		})
	}
}

// 开新版本先给身份行加写锁（lockForNewVersion 经 lockIdent 的 FOR UPDATE）：同一个身份同时开新版本的两次因此排队，后一次
// 看得到前一次开的草稿，报 ErrConflict（已有未审批的版本）。没有这把锁，两次都看不到对方还没提交的草稿，分到同一个版本号，
// 后一次等前一次提交之后撞上 hook_versions 的主键，报出没翻译的 23505：部分唯一索引兜不住，主键先撞。契约测试里的开新版本
// 都是串行的，钉不住这一点。另开一个连接当先到的那一次：在没提交的事务里照 NewHookVersion 的写法记下分配的版本号 2、写下
// 版本 2 的草稿。带着同一个来源版本再开，要等它提交，再报 ErrConflict，库里只有它开的那个版本。
func Test同时开新版本等对方提交之后报冲突(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	h, err := s.CreateHook(ctx, model.Hook{Name: "LVM", Author: "alice"})
	if err != nil {
		t.Fatalf("新建 hook：%v", err)
	}
	// 不经仓储把版本 1 置为 approved，好在它之上开新版本：这里只看加锁，不必让内容过激活判定
	if err := s.DBForTest().WithContext(ctx).Exec(`UPDATE hook_versions SET state = 'approved' WHERE hook_id = ?`, h.ID).Error; err != nil {
		t.Fatalf("置为 approved：%v", err)
	}
	hold := `WITH u AS (UPDATE hooks SET last_version = 2 WHERE id = $1 RETURNING id) ` +
		`INSERT INTO hook_versions (hook_id, version, description, state, author, approver, builtin, params, timeout_sec, ` +
		`required, impls, rev) SELECT id, 2, '', 'draft', 'carol', '', false, 'null', 0, false, 'null', 1 FROM u`
	err = writeWhileHeld(t, dsn, hold, h.ID, func() error {
		_, err := s.NewHookVersion(ctx, h.ID, 1, "dave")
		return err
	})
	if !errors.Is(err, repository.ErrConflict) {
		t.Fatalf("对方开的版本 2 提交之后还没审批，再开新版本应报 ErrConflict，得到 %v", err)
	}
	vs, err := s.ListHookVersions(ctx, h.ID)
	if err != nil {
		t.Fatalf("列出版本：%v", err)
	}
	if len(vs) != 2 || vs[1].Version != 2 || vs[1].Author != "carol" {
		t.Fatalf("库里应只有版本 1 与对方开的版本 2，得到 %+v", vs)
	}
}

// 审批模板时，对它引用的 hook 版本加读锁，在锁住之后读到的最新数据上重跑判定（C26）。另开一个连接，在没提交的事务里
// 废弃模板引用的 hook 版本：审批要等它提交，再报 hook_ref_deprecated，模板仍待审批。不加读锁的话，审批读到的是废弃
// 之前的版本，不等就批了下来，批准的模板引用的却是一个已废弃的 hook。契约测试里的废弃都在审批之前提交，钉不住这一点。
func Test审批模板等引用的hook版本提交之后再判定(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	audit := func(action string) repository.Audit { return repository.Audit{Actor: "alice", Action: action, At: 1} }
	h, err := s.CreateHook(ctx, model.Hook{Name: "LVM", Author: "alice", Impls: []model.Impl{{
		Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "autopart --type=lvm\n"}}})
	if err != nil {
		t.Fatalf("新建 hook：%v", err)
	}
	if err := s.SubmitHook(ctx, h.Key(), h.Rev, audit("hook.submit")); err != nil {
		t.Fatalf("提交 hook：%v", err)
	}
	if err := s.ApproveHook(ctx, h.Key(), h.Rev+1, "bob", audit("hook.approve")); err != nil {
		t.Fatalf("审批 hook：%v", err)
	}
	tpl, err := s.CreateTemplate(ctx, model.Template{
		Name: "KS", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"},
		Body:     "text\n{{lober:syslog}}\n{{slot:分区}}\n%post\n{{lober:firstboot}}\n{{lober:cleanup}}\n%end\n",
		Assembly: map[string][]model.Instance{"分区": {{HookID: h.ID, HookVersion: 1}}},
	})
	if err != nil {
		t.Fatalf("新建模板：%v", err)
	}
	if err := s.SubmitTemplate(ctx, templateKey(tpl), tpl.Rev, audit("template.submit")); err != nil {
		t.Fatalf("提交模板：%v", err)
	}
	if tpl, err = s.GetTemplate(ctx, templateKey(tpl)); err != nil {
		t.Fatalf("读模板：%v", err)
	}

	approve := func() error {
		return s.ApproveTemplate(ctx, templateKey(tpl), tpl.Rev, "bob", audit("template.approve"))
	}
	err = writeWhileHeld(t, dsn, `UPDATE hook_versions SET state = 'deprecated', rev = rev + 1 WHERE hook_id = $1`, h.ID, approve)
	var pe *repository.ProblemsError
	if !errors.As(err, &pe) || pe.Kind != repository.ErrRejected || len(pe.Problems) != 1 || pe.Problems[0].Code != "hook_ref_deprecated" {
		t.Fatalf("hook 的废弃提交之后，审批应报 ProblemsError(ErrRejected)，问题是 hook_ref_deprecated，得到 %v", err)
	}
	if got, err := s.GetTemplate(ctx, templateKey(tpl)); err != nil || !reflect.DeepEqual(got, tpl) {
		t.Fatalf("审批被拒之后模板应原样不动：得到 %+v、%v，应为 %+v", got, err, tpl)
	}
}

// 名字的唯一索引是兜底：同时用同一个名字新建的两次写入，事务里谁都看不到对方还没提交的身份。另开一个连接，在没提交的
// 事务里占住一个名字：用它新建的要等对方提交，再撞上 idx_hooks_name_key，在事务外查出对方，照样报 DuplicateError，
// 自己什么都没写下。契约测试里的撞名都是串行的，走不到「等对方提交」这一步。
func Test同时占用同一个名字由唯一索引兜底报出对方(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	err := writeWhileHeld(t, dsn, `INSERT INTO hooks (id, name, name_key, builtin, last_version) VALUES ($1, 'Same', 'same', false, 1)`,
		"held", func() error {
			_, err := s.CreateHook(ctx, model.Hook{Name: "SAME", Author: "alice"})
			return err
		})
	var de *repository.DuplicateError
	if !errors.As(err, &de) || de.Field != "name" || de.Existing != "held" {
		t.Fatalf("撞上对方已提交的名字，应报 DuplicateError{name, held}，得到 %v", err)
	}
	var idents, versions int64
	if err := s.DBForTest().WithContext(ctx).Raw(`SELECT (SELECT count(*) FROM hooks), (SELECT count(*) FROM hook_versions)`).
		Row().Scan(&idents, &versions); err != nil {
		t.Fatalf("数行数：%v", err)
	}
	if idents != 1 || versions != 0 {
		t.Fatalf("库里应只有对方占名字的那一个身份，得到 %d 个身份、%d 个版本", idents, versions)
	}
}

// 草稿可以引用不存在的 hook 版本（激活时才报 hook_ref_missing），引用的 ID 只要是可存文本，再长也存得下（spec §3 总则：
// 能存成 UTF-8 文本的草稿都能存）。template_hook_refs 的主键与 (hook_id, hook_version) 的索引都带着
// hook_id，btree 索引的一行最多两千七百来个字节，压不下去的长 ID 写进去就超了（SQLSTATE 54000）。契约测试里的 ID
// 都短，钉不住这一点。随机的小写字母压不下去；种子固定，每次跑的都一样。
func Test模板草稿引用很长的hookID照样存得下(t *testing.T) {
	s := pgtest.OpenAt(t, pgtest.Schema(t))
	r := rand.New(rand.NewPCG(1, 2))
	longID := func() string {
		b := make([]byte, 3000)
		for i := range b {
			b[i] = byte('a' + r.IntN(26))
		}
		return string(b)
	}
	// readBack 查返回的版本带着写进去的装配表，读回来的与返回的相同。ID 太长，不把装配表打出来
	readBack := func(t *testing.T, got model.Template, asm map[string][]model.Instance) {
		t.Helper()
		if !reflect.DeepEqual(got.Assembly, asm) {
			t.Fatal("返回的装配表与写进去的不同")
		}
		if again, err := s.GetTemplate(t.Context(), templateKey(got)); err != nil || !reflect.DeepEqual(again, got) {
			t.Fatalf("读回来的与写进去的不同（读的错误：%v）", err)
		}
	}

	t.Run("新建", func(t *testing.T) {
		asm := map[string][]model.Instance{"分区": {{HookID: longID(), HookVersion: 1}}}
		tpl, err := s.CreateTemplate(t.Context(), model.Template{Name: "KS-1", Author: "alice", Assembly: asm})
		if err != nil {
			t.Fatalf("新建引用长 ID 的模板草稿应能存下，得到 %v", err)
		}
		readBack(t, tpl, asm)
	})
	t.Run("保存", func(t *testing.T) {
		tpl, err := s.CreateTemplate(t.Context(), model.Template{Name: "KS-2", Author: "alice"})
		if err != nil {
			t.Fatalf("新建模板：%v", err)
		}
		tpl.Assembly = map[string][]model.Instance{"分区": {{HookID: longID(), HookVersion: 1}}}
		saved, err := s.SaveTemplateDraft(t.Context(), tpl)
		if err != nil {
			t.Fatalf("保存引用长 ID 的模板草稿应能存下，得到 %v", err)
		}
		readBack(t, saved, tpl.Assembly)
	})
}

// 建任务在事务里对锁住的主机重跑准入，可同一台主机上失败的任务正被重试、还没提交时，准入看不到它：推进不锁主机，C31 由
// 部分唯一索引 idx_tasks_open 兜底（批量建任务第 4 步）。新任务的插入要等那次推进提交，再撞上索引：整批回滚，报
// BatchError，结果与准入看得到那次重试时给的相同，只有撞上的那台报 host_has_open_task；任务与审计都没写。契约测试里的撞车
// 都被准入拦下了，走不到这条路；这里另开一个连接当那次重试，让它停在没提交的事务里。
func Test建任务撞上没提交的重试由部分唯一索引兜底(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	f := newTaskFixture(t, s, "SN-1", "SN-2")
	audit := repository.Audit{Actor: "alice", Action: "task.create", At: 1}
	failed := f.mustFailed(t, s, "SN-1")
	audits := auditCount(t, s)
	err := writeWhileHeld(t, dsn, `UPDATE tasks SET state = 'pending', rev = rev + 1 WHERE id = $1`, failed.ID, func() error {
		_, err := s.CreateTasks(ctx, f.tasks("SN-2", "SN-1"), audit)
		return err
	})
	want := admission.BatchResult{Common: []model.Problem{}, ByHost: map[string][]model.Problem{
		"SN-1": admission.HostSelectable(model.Host{SN: "SN-1"}, true),
		"SN-2": {},
	}}
	var be *repository.BatchError
	if !errors.As(err, &be) || !errors.Is(err, repository.ErrRejected) || !reflect.DeepEqual(be.Result, want) {
		t.Fatalf("撞上重试之后应报 BatchError，只有 SN-1 报 host_has_open_task：得到 %v（%+v）", err, be)
	}
	p, err := s.ListTasks(ctx, repository.TaskQuery{})
	if err != nil || len(p.Items) != 1 || p.Items[0].ID != failed.ID || p.Items[0].State != model.TaskPending {
		t.Fatalf("库里应只有重试回 pending 的那一个任务，得到 %+v、%v", p.Items, err)
	}
	if n := auditCount(t, s); n != audits {
		t.Fatalf("整批回滚时不写审计：审计从 %d 条变成了 %d 条", audits, n)
	}
}

// 推进回未结束的状态时，C31 只靠部分唯一索引 idx_tasks_open 挡：推进不锁主机。要是先查一遍「这台主机有没有别的未结束
// 任务」再写，别人刚建、还没提交的任务查不到，两边就都落了库。另开一个连接，在没提交的事务里给同一台主机写一个 pending 的
// 任务：失败任务的重试要等它提交，再撞上索引，报 ProblemsError(ErrRejected)，问题是 host_has_open_task，推进连同带的事件
// 与审计都不生效。契约测试里挡住重试的新任务都已提交，钉不住这一点。
func Test推进撞上没提交的新任务由部分唯一索引兜底(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	f := newTaskFixture(t, s, "SN-1")
	failed := f.mustFailed(t, s, "SN-1")
	audits := auditCount(t, s)
	hold := `INSERT INTO tasks (id, host_sn, iso_id, template_id, template_version, state, snapshot, created_by, created_at, rev, ` +
		`last_seq) SELECT 'held', host_sn, iso_id, template_id, template_version, 'pending', snapshot, created_by, created_at, 1, 0 ` +
		`FROM tasks WHERE id = $1`
	err := writeWhileHeld(t, dsn, hold, failed.ID, func() error {
		_, _, err := s.TransitionTaskByUser(ctx, failed.ID, repository.TaskExpect{State: failed.State, Rev: failed.Rev}, model.TaskPending,
			[]repository.NewEvent{{At: 1, Kind: "task.retry"}}, repository.Audit{Actor: "alice", Action: "task.retry", At: 1})
		return err
	})
	var pe *repository.ProblemsError
	want := admission.HostSelectable(model.Host{SN: "SN-1"}, true)
	if !errors.As(err, &pe) || pe.Kind != repository.ErrRejected || !reflect.DeepEqual(pe.Problems, want) {
		t.Fatalf("撞上别人刚建的任务之后，重试应报 ProblemsError(ErrRejected)，问题是 host_has_open_task，得到 %v", err)
	}
	if got, err := s.GetTask(ctx, failed.ID); err != nil || !reflect.DeepEqual(got, failed) {
		t.Fatalf("推进被拒之后任务应原样不动：得到 %+v、%v，应为 %+v", got, err, failed)
	}
	if es, err := s.ListEvents(ctx, failed.ID, 0, 0); err != nil || len(es) != 0 {
		t.Fatalf("推进被拒之后不写事件，得到 %+v、%v", es, err)
	}
	wantSeqCounter(t, s, failed.ID, 0, "推进撞上索引、回滚之后")
	if n := auditCount(t, s); n != audits {
		t.Fatalf("推进被拒之后不写审计：审计从 %d 条变成了 %d 条", audits, n)
	}
}

// 建任务给涉及的主机加写锁，给 ISO、模板版本与它引用的 hook 版本加读锁，再对锁住之后读到的最新数据重跑准入（批量建任务第
// 2、3 步）。另开一个连接，在没提交的事务里改掉其中一样：建任务要等它提交，再按改了之后的数据报问题，一个任务都不建。不加锁
// 的话，建任务读到的是改之前的数据，不等就建了下来，任务引用的却是已废弃的 ISO、模板或 hook，或者与主机新登记的架构不符。
// 契约测试里的改动都在建任务之前提交，钉不住这一点。
func Test建任务等锁住的记录提交之后再重跑准入(t *testing.T) {
	for _, c := range []struct {
		name string
		hold string                     // 在没提交的事务里执行，$1 是 key 给的值
		key  func(f taskFixture) string // 要改的那一行的键
		code model.Code                 // 改了之后，重跑准入报的问题
	}{
		{"主机改了架构", `UPDATE hosts SET arch = 'aarch64', rev = rev + 1 WHERE sn = $1`,
			func(taskFixture) string { return "SN-1" }, "arch_mismatch"},
		{"ISO 废弃了", `UPDATE isos SET state = 'deprecated', rev = rev + 1 WHERE id = $1`,
			func(f taskFixture) string { return f.iso.ID }, "iso_not_ready"},
		{"模板版本废弃了", `UPDATE template_versions SET state = 'deprecated', rev = rev + 1 WHERE template_id = $1`,
			func(f taskFixture) string { return f.tpl.ID }, "template_not_approved"},
		{"引用的 hook 版本废弃了", `UPDATE hook_versions SET state = 'deprecated', rev = rev + 1 WHERE hook_id = $1`,
			func(f taskFixture) string { return f.hook.ID }, "template_blocked_by_deprecated"},
	} {
		t.Run(c.name, func(t *testing.T) {
			dsn := pgtest.Schema(t)
			s := pgtest.OpenAt(t, dsn)
			ctx := t.Context()
			f := newTaskFixture(t, s, "SN-1", "SN-2")
			err := writeWhileHeld(t, dsn, c.hold, c.key(f), func() error {
				_, err := s.CreateTasks(ctx, f.tasks("SN-2", "SN-1"), repository.Audit{Actor: "alice", Action: "task.create", At: 1})
				return err
			})
			var be *repository.BatchError
			if !errors.As(err, &be) {
				t.Fatalf("对方提交之后，建任务应报 BatchError，得到 %v", err)
			}
			var codes []model.Code
			for _, p := range slices.Concat(be.Result.Common, be.Result.ByHost["SN-1"], be.Result.ByHost["SN-2"]) {
				codes = append(codes, p.Code)
			}
			if !slices.Equal(codes, []model.Code{c.code}) {
				t.Fatalf("重跑准入应只报 %s，得到 %v", c.code, be)
			}
			if p, err := s.ListTasks(ctx, repository.TaskQuery{}); err != nil || len(p.Items) != 0 {
				t.Fatalf("准入没通过时一个任务都不建，得到 %+v、%v", p.Items, err)
			}
		})
	}
}

// 建任务给涉及的主机加写锁（lockHosts 的 FOR UPDATE），锁住之后再查这几台已有的未结束任务（openTasksOf），交给准入逐台报出
// （批量建任务第 2、3 步）。另开一个连接，在没提交的事务里给批里的两台主机各写一个 pending 的任务：插入时的外键检查给这两行
// 主机加上 FOR KEY SHARE，与它冲突的只有 FOR UPDATE。建任务要在给主机加锁时等它提交，再对两台都报 host_has_open_task，任务与
// 审计都不写。主机的锁弱于 FOR UPDATE，或者不查已有的未结束任务、全靠 idx_tasks_open 兜底，建任务都要到插入时才撞上索引，只报
// 得出先撞上的那一台，契约却要求 ByHost 逐台列出原因；锁换成 FOR SHARE 这种彼此不冲突的，两批同时给重叠的主机建任务、插入的
// 顺序不同时，还会互相等死（SQLSTATE 40P01）。契约测试里只有一台主机有未结束的任务，兜底给的结果恰好与准入的相同；
// Test建任务等锁住的记录提交之后再重跑准入 里改主机的对方持的是 FOR NO KEY UPDATE，FOR SHARE 也会等它。两处都钉不住这一点。
func Test建任务等主机上没提交的新任务提交之后逐台报出(t *testing.T) {
	dsn := pgtest.Schema(t)
	s := pgtest.OpenAt(t, dsn)
	ctx := t.Context()
	f := newTaskFixture(t, s, "SN-1", "SN-2")
	audits := auditCount(t, s)
	hold := `INSERT INTO tasks (id, host_sn, iso_id, template_id, template_version, state, snapshot, created_by, created_at, rev, ` +
		`last_seq) SELECT 'held' || n, 'SN-' || n, i.id, v.template_id, v.version, 'pending', 'null', 'bob', 1, 1, 0 ` +
		`FROM isos i, template_versions v, generate_series(1, 2) AS n WHERE i.id = $1`
	err := writeWhileHeld(t, dsn, hold, f.iso.ID, func() error {
		_, err := s.CreateTasks(ctx, f.tasks("SN-2", "SN-1"), repository.Audit{Actor: "alice", Action: "task.create", At: 1})
		return err
	})
	want := admission.BatchResult{Common: []model.Problem{}, ByHost: map[string][]model.Problem{
		"SN-1": admission.HostSelectable(model.Host{SN: "SN-1"}, true),
		"SN-2": admission.HostSelectable(model.Host{SN: "SN-2"}, true),
	}}
	var be *repository.BatchError
	if !errors.As(err, &be) || !errors.Is(err, repository.ErrRejected) || !reflect.DeepEqual(be.Result, want) {
		t.Fatalf("对方提交之后，建任务应报 BatchError，两台都报 host_has_open_task：得到 %v（%+v）", err, be)
	}
	p, err := s.ListTasks(ctx, repository.TaskQuery{})
	if err != nil {
		t.Fatalf("列出任务：%v", err)
	}
	var ids []string
	for _, task := range p.Items {
		ids = append(ids, task.ID)
	}
	if !slices.Equal(ids, []string{"held1", "held2"}) {
		t.Fatalf("库里应只有对方写下的两个任务，得到 %v", ids)
	}
	if n := auditCount(t, s); n != audits {
		t.Fatalf("准入没通过时不写审计：审计从 %d 条变成了 %d 条", audits, n)
	}
}

// 推进与追加事件都先给任务行加写锁（lockTask 的 FOR UPDATE），比对期望值、分配 Seq、查已有的 Key 都看锁住之后读到的数据。
// 另开一个连接当先到的那一次：在没提交的事务里照仓储的写法推进一步，并追加一条带 Key 的事件。带着旧期望值的推进要等它提交，
// 再报 ErrConflict（C33）；追加要等它提交，再接着它的 Seq 分配，同一个 Key 报告为 Duplicate。没有这把锁，推进读到旧的 Rev，
// 等对方提交之后照样写入、把它覆盖掉；追加读到旧的计数，分到同一个 Seq，撞上主键。Test契约并发 的用例要靠调度撞在一起，
// 撞不上时钉不住这一点。
func Test推进与追加事件等对方提交之后再比对与分配(t *testing.T) {
	hold := `WITH u AS (UPDATE tasks SET state = 'booting', rev = rev + 1, last_seq = last_seq + 1 WHERE id = $1 RETURNING id) ` +
		`INSERT INTO task_events (task_id, seq, at, kind, key, fields) SELECT id, 1, 1, 'task.boot', 'sig-1', 'null' FROM u`
	for _, c := range []struct {
		name  string
		write func(ctx context.Context, s *pgstore.Store, task model.Task) error // 在对方提交之前开始，带着对方提交之前读到的 task
	}{
		{"带着旧期望值推进", func(ctx context.Context, s *pgstore.Store, task model.Task) error {
			_, _, err := s.TransitionTask(ctx, task.ID, repository.TaskExpect{State: task.State, Rev: task.Rev}, model.TaskInstalling, nil)
			if !errors.Is(err, repository.ErrConflict) {
				return fmt.Errorf("对方提交之后 Rev 已是 %d，带着 %d 推进应报 ErrConflict，得到 %v", task.Rev+1, task.Rev, err)
			}
			return nil
		}},
		{"追加事件", func(ctx context.Context, s *pgstore.Store, task model.Task) error {
			got, err := s.AppendEvents(ctx, task.ID,
				[]repository.NewEvent{{At: 1, Kind: "signal.recv", Key: "sig-1"}, {At: 1, Kind: "signal.recv", Key: "sig-2"}})
			if want := []repository.Appended{{Seq: 1, Duplicate: true}, {Seq: 2}}; err != nil || !reflect.DeepEqual(got, want) {
				return fmt.Errorf("对方提交之后，追加应得到 %+v，得到 %+v、%v", want, got, err)
			}
			return nil
		}},
	} {
		t.Run(c.name, func(t *testing.T) {
			dsn := pgtest.Schema(t)
			s := pgtest.OpenAt(t, dsn)
			ctx := t.Context()
			in := newTaskFixture(t, s, "SN-1").tasks("SN-1")
			created, err := s.CreateTasks(ctx, in, repository.Audit{Actor: "alice", Action: "task.create", At: 1})
			if err != nil {
				t.Fatalf("建任务：%v", err)
			}
			task := created[0]
			if err := writeWhileHeld(t, dsn, hold, task.ID, func() error { return c.write(ctx, s, task) }); err != nil {
				t.Fatal(err)
			}
			want := task
			want.State, want.Rev = model.TaskBooting, task.Rev+1
			if got, err := s.GetTask(ctx, task.ID); err != nil || !reflect.DeepEqual(got, want) {
				t.Fatalf("库里应留着对方推进之后的 %+v，得到 %+v、%v", want, got, err)
			}
		})
	}
}

// taskFixture 是上面几个测试建任务要的记录：一个 ready 的 ISO、一个已审批的模板与它引用的 hook 版本。模板与 hook 都不经仓储
// 置为 approved：这里只看加锁与约束，不必让内容过激活判定，建任务时重跑的准入也不看它。
type taskFixture struct {
	iso  model.ISO
	tpl  model.Template
	hook model.Hook
}

// newTaskFixture 登记 sns 这几台主机，备好 ISO、hook 与模板。
func newTaskFixture(t *testing.T, s *pgstore.Store, sns ...string) taskFixture {
	t.Helper()
	ctx := t.Context()
	audit := repository.Audit{Actor: "alice", Action: "fixture.setup", At: 1}
	for _, sn := range sns {
		if _, err := s.CreateHost(ctx, model.Host{SN: sn}, audit); err != nil {
			t.Fatalf("登记 %s：%v", sn, err)
		}
	}
	iso, err := s.CreateISO(ctx, model.ISO{Name: "Rocky", Family: "linux", Series: "rocky"}, audit)
	if err != nil {
		t.Fatalf("新建 ISO：%v", err)
	}
	iso.Version, iso.Arch, iso.State, iso.SHA256 = "9.6", "x86_64", model.ISOReady, strings.Repeat("5a", 32)
	iso.Artifacts = map[string]string{"kernel": "rocky/9.6/x86_64/vmlinuz", "initrd": "rocky/9.6/x86_64/initrd.img"}
	if iso, err = s.UpdateISO(ctx, iso); err != nil {
		t.Fatalf("置为 ready：%v", err)
	}
	hook, err := s.CreateHook(ctx, model.Hook{Name: "LVM", Author: "alice"})
	if err != nil {
		t.Fatalf("新建 hook：%v", err)
	}
	tpl, err := s.CreateTemplate(ctx, model.Template{Name: "KS", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"},
		Assembly: map[string][]model.Instance{"分区": {{HookID: hook.ID, HookVersion: hook.Version}}}})
	if err != nil {
		t.Fatalf("新建模板：%v", err)
	}
	for _, v := range []struct{ table, owner, id string }{
		{"hook_versions", "hook_id", hook.ID}, {"template_versions", "template_id", tpl.ID},
	} {
		if err := s.DBForTest().WithContext(ctx).Exec("UPDATE "+v.table+" SET state = 'approved' WHERE "+v.owner+" = ?", v.id).Error; err != nil {
			t.Fatalf("把 %s 置为 approved：%v", v.table, err)
		}
	}
	hook.State, tpl.State = model.VersionApproved, model.VersionApproved
	return taskFixture{iso: iso, tpl: tpl, hook: hook}
}

// tasks 是给 sns 这几台主机各建一个任务的入参。快照只填仓储要对上的几项（repository.CheckNewTasks）：仓储不解释快照。
func (f taskFixture) tasks(sns ...string) []model.Task {
	ts := make([]model.Task, len(sns))
	for i, sn := range sns {
		ts[i] = model.Task{HostSN: sn, ISOID: f.iso.ID, TemplateID: f.tpl.ID, TemplateVersion: f.tpl.Version, CreatedBy: "alice",
			CreatedAt: 1, Snapshot: model.Snapshot{Schema: model.SnapshotSchema, Host: model.SnapshotHost{SN: sn},
				ISO: model.SnapshotISO{ID: f.iso.ID}, Template: model.SnapshotTemplate{ID: f.tpl.ID, Version: f.tpl.Version}}}
	}
	return ts
}

// mustFailed 给主机 sn 建一个任务，再推进到 failed：结束了的任务不占 C31 的位，重试时才回到未结束的状态。
func (f taskFixture) mustFailed(t *testing.T, s *pgstore.Store, sn string) model.Task {
	t.Helper()
	ctx := t.Context()
	created, err := s.CreateTasks(ctx, f.tasks(sn), repository.Audit{Actor: "alice", Action: "task.create", At: 1})
	if err != nil {
		t.Fatalf("给 %s 建任务：%v", sn, err)
	}
	failed, _, err := s.TransitionTask(ctx, created[0].ID, repository.TaskExpect{State: model.TaskPending, Rev: 1}, model.TaskFailed, nil)
	if err != nil {
		t.Fatalf("把任务推进到 failed：%v", err)
	}
	return failed
}

// auditCount 是 s 的 schema 里审计的条数。
func auditCount(t *testing.T, s *pgstore.Store) int64 {
	t.Helper()
	var n int64
	if err := s.DBForTest().WithContext(t.Context()).Raw(`SELECT count(*) FROM audit_log`).Row().Scan(&n); err != nil {
		t.Fatalf("数审计：%v", err)
	}
	return n
}

func templateKey(tpl model.Template) model.TemplateKey {
	return model.TemplateKey{ID: tpl.ID, Version: tpl.Version}
}

// connect 不经仓储，另开一个连到 dsn 的连接，测试结束时关闭；连接上没提交的事务随之回滚。
func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("连接测试库：%v", err)
	}
	t.Cleanup(func() {
		// t.Context() 在 Cleanup 之前就取消了，关连接另用一个
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		conn.Close(ctx)
	})
	return conn
}

// waitBlockedBy 经 watch 轮询，直到有会话在等后端进程 pid 的锁。只数被 pid 挡住的会话，别的代理同时跑的测试不算；
// done 先收到结果，说明要等的那一步没有等就返回了。
func waitBlockedBy(t *testing.T, watch *pgx.Conn, pid uint32, done <-chan error) {
	t.Helper()
	for deadline := time.Now().Add(time.Minute); time.Now().Before(deadline); {
		var blocked int
		err := watch.QueryRow(t.Context(), `SELECT count(*) FROM pg_stat_activity WHERE $1 = ANY(pg_blocking_pids(pid))`, pid).Scan(&blocked)
		switch {
		case err != nil:
			t.Fatalf("查锁等待：%v", err)
		case blocked > 0:
			return
		}
		select {
		case err := <-done:
			t.Fatalf("要等锁的那一步没有等就返回了：%v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	t.Fatalf("一分钟内没等到有会话卡在后端进程 %d 的锁上", pid)
}

// writeWhileHeld 另开一个连到 dsn 的连接，在没提交的事务里执行 hold（$1 是 key），再在 goroutine 里执行 write；等 write
// 被这个连接挡住之后提交，返回 write 的结果。
func writeWhileHeld(t *testing.T, dsn, hold, key string, write func() error) error {
	t.Helper()
	ctx := t.Context()
	holder := connect(t, dsn)
	tx, err := holder.Begin(ctx)
	if err != nil {
		t.Fatalf("开事务：%v", err)
	}
	if _, err := tx.Exec(ctx, hold, key); err != nil {
		t.Fatalf("在没提交的事务里改 %s：%v", key, err)
	}
	done := make(chan error, 1)
	go func() { done <- write() }()
	waitBlockedBy(t, connect(t, dsn), holder.PgConn().PID(), done)
	if err := tx.Commit(ctx); err != nil {
		t.Fatalf("提交对方的事务：%v", err)
	}
	select {
	case err = <-done:
	case <-time.After(time.Minute):
		t.Fatal("对方提交之后，等着的那一次写入一分钟内没有返回")
	}
	return err
}
