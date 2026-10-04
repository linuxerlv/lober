package pgstore

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"math/rand/v2"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/storetest"
)

// 测试夹具：每次 Open 在测试库里新建一个 schema，用完删掉，几个代理同时跑测试也互不干扰。测试库怎么准备见 doc.go。

// dsnEnv 是给测试库 DSN 的环境变量；没设时，从模块根逐级往上找 dsnFile。
const (
	dsnEnv  = "LOBER_TEST_PG_DSN"
	dsnFile = ".pgtest.dsn"
)

// maxTestConns 是每个测试仓储的连接上限：几个代理会同时跑测试，本机的 max_connections 只有 100。
const maxTestConns = 4

// testDSN 是测试库的 DSN。两处都没有就失败、不跳过：跳过会让 make check 在存储层零覆盖的情况下变绿。
func testDSN(t *testing.T) string {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(dsnEnv)); dsn != "" {
		return dsn
	}
	for dir := moduleRoot(t); ; dir = filepath.Dir(dir) {
		path := filepath.Join(dir, dsnFile)
		b, err := os.ReadFile(path)
		switch {
		case err == nil:
			if dsn := strings.TrimSpace(string(b)); dsn != "" {
				return dsn
			}
			t.Fatalf("%s 是空的：里面要写一行 DSN（见 pgstore 的包说明）", path)
		case !errors.Is(err, fs.ErrNotExist):
			t.Fatalf("读 %s：%v", path, err)
		}
		if filepath.Dir(dir) == dir {
			break
		}
	}
	t.Fatalf("找不到测试库的 DSN：设环境变量 %s，或在模块根目录（或它的任一上级目录）放一个 %s，内容是一行 DSN，"+
		"如 postgres://lober_test:<口令>@127.0.0.1:5432/lober_test?sslmode=disable。测试角色要拥有这个库、能在里面建 "+
		"schema，见 pgstore 的包说明", dsnEnv, dsnFile)
	return ""
}

// moduleRoot 从测试的工作目录（包目录）往上找到 go.mod 所在的目录。
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("取工作目录：%v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("从工作目录往上找不到 go.mod")
		}
		dir = parent
	}
}

// newSchema 在测试库里新建一个 lober_t_ 加随机串的 schema，返回 search_path 只有它的 DSN；测试结束时连同里面的
// 一切删掉。
func newSchema(t *testing.T) string {
	t.Helper()
	base := testDSN(t)
	name := "lober_t_" + store.NewID()
	adminExec(t, t.Context(), base, "CREATE SCHEMA "+pgx.Identifier{name}.Sanitize())
	t.Cleanup(func() {
		// t.Context() 在 Cleanup 之前就取消了，删 schema 另用一个
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		adminExec(t, ctx, base, "DROP SCHEMA "+pgx.Identifier{name}.Sanitize()+" CASCADE")
	})
	dsn, err := withSearchPath(base, name)
	if err != nil {
		t.Fatal(err)
	}
	return dsn
}

// adminExec 另开一个连接执行建删 schema 这类语句：仓储的连接都只看得见自己的 schema。
func adminExec(t *testing.T, ctx context.Context, dsn, sql string) {
	t.Helper()
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Errorf("连接测试库：%v", err)
		return
	}
	defer conn.Close(ctx)
	if _, err := conn.Exec(ctx, sql); err != nil {
		t.Errorf("执行 %s：%v", sql, err)
	}
}

// withSearchPath 给 DSN 加上 search_path：URL 形式加在查询参数里，key=value 形式接在末尾。出错时不带 DSN 原文，
// 免得把口令打进测试输出。
func withSearchPath(dsn, schema string) (string, error) {
	if !strings.HasPrefix(dsn, "postgres://") && !strings.HasPrefix(dsn, "postgresql://") {
		return dsn + " search_path=" + schema, nil
	}
	u, err := url.Parse(dsn)
	if err != nil {
		return "", errors.New("测试库的 DSN 不是合法的 URL（为免泄露口令，不打出原文）")
	}
	q := u.Query()
	q.Set("search_path", schema)
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// openStore 用 dsn 打开仓储，限住连接数，测试结束时关闭。
func openStore(t *testing.T, dsn string) *Store {
	t.Helper()
	s, err := Open(t.Context(), dsn)
	if err != nil {
		t.Fatalf("Open：%v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("关闭仓储：%v", err)
		}
	})
	sqlDB, err := s.db.DB()
	if err != nil {
		t.Fatalf("取连接池：%v", err)
	}
	sqlDB.SetMaxOpenConns(maxTestConns)
	return s
}

// installFault 在 s 的 schema 里给审计表装上失败注入（storetest.Harness 的要求）：Actor 为 storetest.FaultActor 的
// 插入一律抛错，抛在动作本身写完之后、同一个事务里。只有测试的 schema 有这个触发器，Open 建的表上没有这条路。
func installFault(t *testing.T, s *Store) {
	t.Helper()
	fn := `CREATE FUNCTION lober_test_fault() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.actor = '` + strings.ReplaceAll(storetest.FaultActor, "'", "''") + `' THEN
		RAISE EXCEPTION 'lober_test_fault：注入的审计写入失败';
	END IF;
	RETURN NEW;
END
$$`
	trigger := `CREATE TRIGGER lober_test_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION lober_test_fault()`
	for _, sql := range []string{fn, trigger} {
		if err := s.db.WithContext(t.Context()).Exec(sql).Error; err != nil {
			t.Fatalf("装失败注入：%v", err)
		}
	}
}

// harness 给契约测试一个全新的空仓储，装好了失败注入。
type harness struct{}

func (harness) Open(t *testing.T) store.Store {
	s := openStore(t, newSchema(t))
	installFault(t, s)
	return s
}

func Test主机契约(t *testing.T) { storetest.RunHosts(t, harness{}) }

func TestISO契约(t *testing.T) { storetest.RunISOs(t, harness{}) }

func Test审计契约(t *testing.T) { storetest.RunAudit(t, harness{}) }

// 失败注入只在测试的 schema 里：Open 建的表上，FaultActor 只是一个普通的操作者。
func TestOpen不注入失败(t *testing.T) {
	s := openStore(t, newSchema(t))
	a := store.Audit{Actor: storetest.FaultActor, Action: "host.create", At: 1_790_000_000}
	if _, err := s.CreateHost(t.Context(), model.Host{SN: "SN-1"}, a); err != nil {
		t.Fatalf("Open 建的表不该注入失败：%v", err)
	}
	p, err := s.ListAudit(t.Context(), store.AuditQuery{})
	if err != nil {
		t.Fatalf("列出审计：%v", err)
	}
	if len(p.Items) != 1 || p.Items[0].Actor != storetest.FaultActor {
		t.Fatalf("审计应写下这一条，得到 %+v", p.Items)
	}
}

// 存不下的键不可能登记过（PostgreSQL 的 text 不收 NUL 与非法的 UTF-8）：按它查、删、过滤，结果与内存实现相同，
// 不把数据库的编码错误交给调用方。契约测试没有这几例，这里补上。
func Test存不下的键与过滤条件当作没有(t *testing.T) {
	s := openStore(t, newSchema(t))
	ctx := t.Context()
	if _, err := s.CreateHost(ctx, model.Host{SN: "SN-1"}, store.Audit{Actor: "alice", Action: "host.create", At: 1}); err != nil {
		t.Fatalf("登记：%v", err)
	}
	for _, bad := range []string{"SN\x00-1", "SN-1\xff"} {
		if _, err := s.GetHost(ctx, bad); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("GetHost(%q) 应报 ErrNotFound，得到 %v", bad, err)
		}
		if err := s.DeleteHost(ctx, bad, 1, store.Audit{Actor: "alice", Action: "host.delete", At: 1}); !errors.Is(err, store.ErrNotFound) {
			t.Errorf("DeleteHost(%q) 应报 ErrNotFound，得到 %v", bad, err)
		}
		m, err := s.GetHosts(ctx, []string{bad, "SN-1"})
		if err != nil || len(m) != 1 {
			t.Errorf("GetHosts 带着 %q 应只取到 SN-1，得到 %v、%v", bad, m, err)
		}
		for _, q := range []store.HostQuery{{SNPrefix: bad}, {AttrKey: "rack", AttrValue: bad}, {AttrKey: bad, AttrValue: "A1"}} {
			if p, err := s.ListHosts(ctx, q); err != nil || p.Items == nil || len(p.Items) != 0 {
				t.Errorf("ListHosts(%+v) 应得到空的一页，得到 %+v、%v", q, p, err)
			}
		}
		if p, err := s.ListISOs(ctx, store.ISOQuery{Family: "linux", States: []model.ISOState{model.ISOState(bad)}}); err != nil || p.Items == nil || len(p.Items) != 0 {
			t.Errorf("ListISOs 按状态 %q 应得到空的一页，得到 %+v、%v", bad, p, err)
		}
		if p, err := s.ListAudit(ctx, store.AuditQuery{Actor: bad}); err != nil || p.Items == nil || len(p.Items) != 0 {
			t.Errorf("ListAudit 按操作者 %q 应得到空的一页，得到 %+v、%v", bad, p, err)
		}
	}
	// 游标里藏着存不下的串：仓储给不出这样的游标，同样认不出来
	if _, err := s.ListHosts(ctx, store.HostQuery{Cursor: encodeCursor("hosts", "SN\x00")}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("主机的游标藏着 NUL 应报 ErrInvalid，得到 %v", err)
	}
	if _, err := s.ListISOs(ctx, store.ISOQuery{Cursor: encodeCursor("isos", "a\x00", "b")}); !errors.Is(err, store.ErrInvalid) {
		t.Errorf("ISO 的游标藏着 NUL 应报 ErrInvalid，得到 %v", err)
	}
}

// 两次系统更新同时给两个 ISO 写同一份内容时，事务里的预查看不到对方还没提交的写入，由部分唯一索引 idx_isos_sha256
// 兜底：后写的等先写的提交，再撞上索引。这时 UpdateISO 要在事务外查出对方，照样报 DuplicateError（C4），第 4 层靠它
// 把导入失败的 ISO 指向已有的那一个（spec §5.1 第 3 步）。契约测试里的撞车都被预查拦下了，走不到这条路；这里另开
// 一个连接，让 A 的写入停在没提交的事务里，造出这个时机。
func Test同时写同一份内容由索引兜底报出对方(t *testing.T) {
	dsn := newSchema(t)
	s := openStore(t, dsn)
	ctx := t.Context()
	newISO := func(name string) model.ISO {
		iso, err := s.CreateISO(ctx, model.ISO{Name: name, Family: "linux", Series: "rocky"},
			store.Audit{Actor: "alice", Action: "iso.create", At: 1})
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
	var de *store.DuplicateError
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
	s := openStore(t, newSchema(t))
	sqlDB, err := s.db.DB()
	if err != nil {
		t.Fatalf("取连接池：%v", err)
	}
	sqlDB.SetMaxIdleConns(maxTestConns) // 连接留着不关，每轮开始时不必现建，几批才真的同时写
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
				_, err := s.ImportHosts(t.Context(), batch, store.Audit{Actor: "alice", Action: "host.import", At: 1})
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
		p, err := s.ListAudit(t.Context(), store.AuditQuery{Cursor: cursor, Limit: 500})
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
	dsn := newSchema(t)
	s := openStore(t, dsn)
	audit := func(action string) store.Audit { return store.Audit{Actor: "alice", Action: action, At: 1} }

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
			if !errors.Is(err, store.ErrConflict) {
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
			if !errors.Is(err, store.ErrConflict) {
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
