// Package pgtest 是连 PostgreSQL 测试库的测试夹具（spec §6.4），给第 4、5、7 层的测试起一个真实的仓储：
//
//	s := pgtest.Open(t)
//
// 每次 Open 在测试库里新建一个临时 schema，经 pgstore.Open 建表、做表结构断言，再给审计表装上失败注入；测试结束时
// 关掉仓储，连同 schema 一起删掉，几个测试同时跑也互不干扰。测试库怎么准备、连接串放在哪，见 pgstore 的包说明。
//
// 它不是 _test.go，别的包的测试才 import 得到它；它 import 了 testing，生产代码不该用它。
//
// pgstore 自己的测试也用它，夹具的规则只有这一份。那些测试写在外部测试包 pgstore_test 里：pgtest 依赖 pgstore，
// pgstore 的包内测试再 import 它就成环（import cycle not allowed in test），外部测试包是另一个包，不成环。它们要做白盒
// 断言，所以另有三个入口：OpenStore 给具体类型 *pgstore.Store；Schema 只建 schema、给出 DSN，OpenAt 在给定的 DSN 上
// 打开仓储，供要自己掌握 schema 的测试用。
package pgtest

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/pgstore"
)

// dsnEnv 是给测试库 DSN 的环境变量；没设时，从模块根逐级往上找 dsnFile。
const (
	dsnEnv  = "LOBER_TEST_PG_DSN"
	dsnFile = ".pgtest.dsn"
)

// maxConns 是每个测试仓储的连接上限。
const maxConns = 4

// maxOpen 是一个测试进程里同时在用的临时 schema 数上限。Open 与 OpenStore 在一个 schema 上只开一个仓储，所以它也是这样
// 打开的测试仓储数上限；用 OpenAt 在同一个 schema 上再开的不另占位，那是串行跑的表结构测试。每个仓储最多 maxConns 个
// 连接，而几个测试进程（go test 同时跑的几个包、几个代理各自跑的 go test）共用库的 max_connections：一个进程最多占
// maxOpen*maxConns 个，给别的进程留出余量。它只管得到本进程。
//
// 4 是这样定的：本机 max_connections 是 100，去掉留给超级用户的 3 个，测试角色能用 97 个。一个位最多占 maxConns 个连接，
// 再加建 schema、装失败注入、删 schema 用的 1 个（见 newSchema），一个进程最多 20 个，四个进程同时到顶也只占 80 个。
// 实测（2026-10-08，本机）pgstore 的契约用例串行 117 秒，同时 2、4、8、16 个分别是 73、53、44、49 秒：瓶颈在库里建表建
// 索引，过了 4 个再加，快得不多，最坏情况下占的连接却翻倍。4 个位时实测一个进程同时最多 9 个连接。
const maxOpen = 4

// slots 是限住同时在用的临时 schema 数的信号量：Schema 先占一个位，测试结束、仓储关掉、schema 删掉之后才让出来。位都
// 占满时，Schema 等到有用例结束；一个用例同时建的 schema 超过 maxOpen 个，会一直等下去。
var slots = make(chan struct{}, maxOpen)

// FaultActor 是失败注入的触发条件：审计的 Actor 等于它时，审计写入失败，与审计同一个事务里的动作一起回滚
// （spec §6.2 第 8 条）。只有 Open 与 OpenStore 给的仓储有这条路；pgstore.Open 建的表上（OpenAt 也是经它打开的），
// 它只是一个普通的操作者。
const FaultActor = "lober-test-fault"

// DSN 按 spec §6.4 读测试连接串：先 LOBER_TEST_PG_DSN，再从模块根逐级往上找 .pgtest.dsn（并行克隆没有这个文件，靠
// 往上找读到主目录那一份）。两处都没有就 t.Fatal，不跳过：跳过会让 make check 在存储层零覆盖的情况下变绿。
func DSN(t testing.TB) string {
	t.Helper()
	if dsn := strings.TrimSpace(os.Getenv(dsnEnv)); dsn != "" {
		return dsn
	}
	return dsnAbove(t, moduleRoot(t))
}

// dsnAbove 从 dir 起逐级往上找 dsnFile，返回最近的一份里的 DSN。最近的一份是空的就失败，不再往上找。
func dsnAbove(t testing.TB, dir string) string {
	t.Helper()
	for {
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
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}
	t.Fatalf("找不到测试库的 DSN：设环境变量 %s，或在模块根目录（或它的任一上级目录）放一个 %s，内容是一行 DSN，"+
		"如 postgres://lober_test:<口令>@127.0.0.1:5432/lober_test?sslmode=disable。测试角色要拥有这个库、能在里面建 "+
		"schema，见 pgstore 的包说明", dsnEnv, dsnFile)
	return ""
}

// moduleRoot 从测试的工作目录（包目录）往上找到 go.mod 所在的目录。
func moduleRoot(t testing.TB) string {
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

// Open 新建一个临时 schema、跑 AutoMigrate 与表结构断言、装上失败注入，返回连到这个 schema 的仓储。t.Cleanup 时连
// schema 一起删掉。并发数由包内的信号量限住，见 spec §6.4：同时打开的仓储最多 maxOpen 个，每个最多 maxConns 个连接。
func Open(t testing.TB) store.Store {
	t.Helper()
	return OpenStore(t)
}

// OpenStore 同 Open，返回的是具体类型：给 pgstore 自己的测试直接查接口看不到的列。别的层用 Open，只依赖契约。
func OpenStore(t testing.TB) *pgstore.Store {
	t.Helper()
	acquire(t)
	dsn, _, admin := newSchema(t)
	defer closeConn(admin)
	s := OpenAt(t, dsn)
	installFault(t, admin)
	return s
}

// Schema 先占一个位，再新建一个临时 schema，返回 search_path 只有它的 DSN，不建表；测试结束时删掉 schema、让出位。给要
// 自己掌握 schema 的测试：另开连接让对方停在没提交的事务里、在同一个 schema 上再 Open 一次、改坏表结构再断言。
func Schema(t testing.TB) string {
	t.Helper()
	acquire(t)
	dsn, _, admin := newSchema(t)
	closeConn(admin)
	return dsn
}

// OpenAt 经 pgstore.Open 打开 dsn 指的仓储（多是 Schema 给的），连接上限是 maxConns，测试结束时关闭。它不占位，也不装失败
// 注入：同一个 schema 可以先后 Open 几次。
func OpenAt(t testing.TB, dsn string) *pgstore.Store {
	t.Helper()
	s, err := pgstore.Open(t.Context(), dsn, pgstore.WithMaxConns(maxConns))
	if err != nil {
		t.Fatalf("pgstore.Open：%v", err)
	}
	// Cleanup 后进先出：先关仓储，再删 schema（newSchema 登记的），最后让出位
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("关闭仓储：%v", err)
		}
	})
	return s
}

// acquire 占 slots 的一个位，测试结束时让出来。
func acquire(t testing.TB) {
	t.Helper()
	slots <- struct{}{}
	t.Cleanup(func() { <-slots })
}

// newSchema 在测试库里新建一个 lober_t_ 加随机串的 schema，返回 search_path 只有它的 DSN、schema 的名字，以及建它的那个
// 连接 admin；测试结束时连同 schema 里的一切删掉。admin 不经仓储（仓储的连接都只看得见自己的 schema），它的 search_path
// 已换成这个 schema，由调用方接着用、用完就关（closeConn）：OpenStore 拿它装失败注入，不必另开。新建连接每次都要做一次
// 口令认证，本机约 120 毫秒；装失败注入另开一个的话，pgstore 整包测试要多跑十来秒（2026-10-09 实测）。
func newSchema(t testing.TB) (dsn, name string, admin *pgx.Conn) {
	t.Helper()
	base := DSN(t)
	name = "lober_t_" + store.NewID()
	ident := pgx.Identifier{name}.Sanitize()
	admin, err := pgx.Connect(t.Context(), base)
	if err != nil {
		t.Fatalf("连接测试库：%v", err)
	}
	if _, err := admin.Exec(t.Context(), "CREATE SCHEMA "+ident); err != nil {
		closeConn(admin)
		t.Fatalf("建 schema：%v", err)
	}
	t.Cleanup(func() {
		// t.Context() 在 Cleanup 之前就取消了，删 schema 另用一个；admin 这时早已关掉，另开一个连接
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		if err := adminExec(ctx, base, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("删 schema：%v", err)
		}
	})
	if _, err := admin.Exec(t.Context(), "SET search_path TO "+ident); err != nil {
		closeConn(admin)
		t.Fatalf("换 search_path：%v", err)
	}
	if dsn, err = withSearchPath(base, name); err != nil {
		closeConn(admin)
		t.Fatal(err)
	}
	return dsn, name, admin
}

// adminExec 另开一个连接依次执行 sqls，执行完就关：删 schema 不经仓储。
func adminExec(ctx context.Context, dsn string, sqls ...string) error {
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		return fmt.Errorf("连接测试库：%w", err)
	}
	defer conn.Close(ctx)
	for _, sql := range sqls {
		if _, err := conn.Exec(ctx, sql); err != nil {
			return fmt.Errorf("执行 %s：%w", sql, err)
		}
	}
	return nil
}

// closeConn 关掉 conn。t.Context() 可能已经取消，另用一个。
func closeConn(conn *pgx.Conn) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	conn.Close(ctx)
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

// installFault 经 newSchema 给的 admin 在它的 schema 里给审计表装上失败注入：Actor 为 FaultActor 的插入一律抛错，抛在
// 动作本身写完之后、同一个事务里。函数与触发器都建在这个 schema 里（admin 的 search_path 只有它），随 schema 一起删掉。
func installFault(t testing.TB, admin *pgx.Conn) {
	t.Helper()
	fn := `CREATE FUNCTION lober_test_fault() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
	IF NEW.actor = '` + strings.ReplaceAll(FaultActor, "'", "''") + `' THEN
		RAISE EXCEPTION 'lober_test_fault：注入的审计写入失败';
	END IF;
	RETURN NEW;
END
$$`
	trigger := `CREATE TRIGGER lober_test_fault BEFORE INSERT ON audit_log FOR EACH ROW EXECUTE FUNCTION lober_test_fault()`
	for _, sql := range []string{fn, trigger} {
		if _, err := admin.Exec(t.Context(), sql); err != nil {
			t.Fatalf("装失败注入：%v", err)
		}
	}
}
