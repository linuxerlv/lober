package pgtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// fatalTB 记下 Fatal、Fatalf 的信息，再 panic 自己，中止被测函数。别的方法落到内嵌的 nil 接口上，调到就 panic：
// 被测函数要是改成了跳过，测试照样失败。
type fatalTB struct {
	testing.TB
	msg string
}

func (f *fatalTB) Helper() {}

func (f *fatalTB) Fatal(args ...any) {
	f.msg = fmt.Sprint(args...)
	panic(f)
}

func (f *fatalTB) Fatalf(format string, args ...any) {
	f.msg = fmt.Sprintf(format, args...)
	panic(f)
}

// fatalOf 用 fatalTB 跑 fn，返回 fn 给出的结果与 Fatal 的信息；没有 Fatal 时信息为空。
func fatalOf(fn func(testing.TB) string) (got, msg string) {
	f := &fatalTB{}
	defer func() {
		if r := recover(); r != nil && r != any(f) {
			panic(r)
		}
		msg = f.msg
	}()
	return fn(f), ""
}

func TestDSN先读环境变量(t *testing.T) {
	t.Setenv(dsnEnv, "  host=from-env  \n")
	if got := DSN(t); got != "host=from-env" {
		t.Fatalf("DSN = %q，应为环境变量里去掉首尾空白的值", got)
	}
}

// 并行克隆的工作树没有 .pgtest.dsn（它不入库），靠从模块根往上找才读到主目录那一份。
func TestDSN从起点逐级往上找(t *testing.T) {
	root := t.TempDir()
	start := filepath.Join(root, "a", "b")
	if err := os.MkdirAll(start, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(dir, content string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, dsnFile), []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	write(root, "postgres://far\n")
	if got := dsnAbove(t, start); got != "postgres://far" {
		t.Fatalf("起点往上两级才有 %s，读到 %q，应为 postgres://far", dsnFile, got)
	}
	write(filepath.Join(root, "a"), "  postgres://near  \n")
	if got := dsnAbove(t, start); got != "postgres://near" {
		t.Fatalf("近的一份优先，读到 %q，应为 postgres://near", got)
	}
}

// 两处都没有就失败、不跳过；文件是空的也一样，不再往上找。没有 .pgtest.dsn 的那一例从临时目录往上找，临时目录的
// 上级里要是恰好有一份，这一例会报「应失败」。
func TestDSN找不到就失败(t *testing.T) {
	t.Run("哪一级都没有", func(t *testing.T) {
		start := t.TempDir()
		got, msg := fatalOf(func(tb testing.TB) string { return dsnAbove(tb, start) })
		if msg == "" {
			t.Fatalf("从 %s 往上找不到 %s 应失败，得到 %q", start, dsnFile, got)
		}
		for _, want := range []string{dsnEnv, dsnFile} {
			if !strings.Contains(msg, want) {
				t.Errorf("失败信息没写出 %s：%s", want, msg)
			}
		}
	})
	t.Run("文件是空的", func(t *testing.T) {
		root := t.TempDir()
		start := filepath.Join(root, "a")
		if err := os.Mkdir(start, 0o755); err != nil {
			t.Fatal(err)
		}
		for dir, content := range map[string]string{root: "postgres://far\n", start: " \n"} {
			if err := os.WriteFile(filepath.Join(dir, dsnFile), []byte(content), 0o600); err != nil {
				t.Fatal(err)
			}
		}
		got, msg := fatalOf(func(tb testing.TB) string { return dsnAbove(tb, start) })
		if !strings.Contains(msg, "是空的") {
			t.Fatalf("近的一份是空的应失败、写明是空的，得到 %q（失败信息 %q）", got, msg)
		}
	})
}

// Open 给的仓储建好了表，装好了失败注入：Actor 为 FaultActor 的审计写入失败，动作一起回滚；别的操作者照常写入。
func TestOpen给一个装好失败注入的仓储(t *testing.T) {
	s := Open(t)
	ctx := t.Context()
	if _, err := s.CreateHost(ctx, model.Host{SN: "SN-1"}, store.Audit{Actor: FaultActor, Action: "host.create", At: 1}); err == nil {
		t.Fatal("Actor 为 FaultActor 时审计应写入失败")
	}
	if _, err := s.GetHost(ctx, "SN-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("审计写入失败时动作应一起回滚，GetHost 应报 ErrNotFound，得到 %v", err)
	}
	if _, err := s.CreateHost(ctx, model.Host{SN: "SN-1"}, store.Audit{Actor: "alice", Action: "host.create", At: 1}); err != nil {
		t.Fatalf("别的操作者应照常写入：%v", err)
	}
}

// 每次 Open 各用一个 schema：两个仓储互相看不到对方写的。
func TestOpen的仓储互不干扰(t *testing.T) {
	a, b := Open(t), Open(t)
	ctx := t.Context()
	if _, err := a.CreateHost(ctx, model.Host{SN: "SN-1"}, store.Audit{Actor: "alice", Action: "host.create", At: 1}); err != nil {
		t.Fatalf("登记：%v", err)
	}
	if _, err := b.GetHost(ctx, "SN-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("另一个仓储不该看到 SN-1，GetHost 应报 ErrNotFound，得到 %v", err)
	}
}

// Open 占一个位，测试结束时让出来：位不让出来，同一个进程里后面的用例迟早都等在 Open 上。
func TestOpen占的位在测试结束时让出(t *testing.T) {
	before := len(slots)
	t.Run("打开", func(t *testing.T) {
		Open(t)
		if got := len(slots); got != before+1 {
			t.Fatalf("Open 之后占了 %d 个位，应为 %d", got, before+1)
		}
	})
	if got := len(slots); got != before {
		t.Fatalf("测试结束之后还占着 %d 个位，应为 %d", got, before)
	}
}

// 临时 schema 在测试结束时连同里面的一切删掉，DSN 的 search_path 只指向它。
func Test临时schema用完就删(t *testing.T) {
	var name string
	t.Run("建", func(t *testing.T) {
		dsn, created, admin := newSchema(t)
		defer closeConn(admin)
		name = created
		// DSN 与建 schema 的连接（装失败注入用它）都只看得见新建的 schema
		for what, conn := range map[string]*pgx.Conn{"DSN": connect(t, dsn), "建 schema 的连接": admin} {
			var current string
			if err := conn.QueryRow(t.Context(), `SELECT current_schema()`).Scan(&current); err != nil {
				t.Fatalf("查%s的当前 schema：%v", what, err)
			}
			if current != name {
				t.Fatalf("%s的当前 schema 是 %q，应为新建的 %q", what, current, name)
			}
		}
	})
	conn := connect(t, DSN(t))
	var n int
	if err := conn.QueryRow(t.Context(), `SELECT count(*) FROM pg_namespace WHERE nspname = $1`, name).Scan(&n); err != nil {
		t.Fatalf("查 schema：%v", err)
	}
	if n != 0 {
		t.Fatalf("测试结束之后 schema %s 还在", name)
	}
}

// connect 另开一个连到 dsn 的连接，测试结束时关闭。
func connect(t *testing.T, dsn string) *pgx.Conn {
	t.Helper()
	conn, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("连接测试库：%v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		conn.Close(ctx)
	})
	return conn
}
