package memstore_test

import (
	"errors"
	"testing"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"github.com/lober/lober/internal/store/memstore"
	"github.com/lober/lober/internal/store/storetest"
)

// harness 给契约测试一个全新的内存仓储，打开了失败注入。
type harness struct{}

func (harness) Open(t *testing.T) store.Store {
	s := memstore.NewFaulty(storetest.FaultActor)
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("关闭仓储：%v", err)
		}
	})
	return s
}

// 内存实现实现了完整的契约，跑全部分组。
func Test契约(t *testing.T) { storetest.RunAll(t, harness{}) }

// 游标由 store.DecodeCursor 解开，与 pgstore 同一份：排序键里藏着存不下的串的，仓储给不出，只能是伪造的，每个列表都报
// ErrInvalid。pgstore 拿这样的串去查询会报编码错误，内存实现照样拒收，两边认不认一个游标才一致。
func Test游标藏着存不下的串报ErrInvalid(t *testing.T) {
	s := memstore.New()
	ctx := t.Context()
	for _, c := range []struct {
		name string
		list func() error
	}{
		{"主机", func() error {
			_, err := s.ListHosts(ctx, store.HostQuery{Cursor: store.EncodeCursor("hosts", "SN\x00")})
			return err
		}},
		{"ISO", func() error {
			_, err := s.ListISOs(ctx, store.ISOQuery{Cursor: store.EncodeCursor("isos", "a\x00", "b")})
			return err
		}},
		{"hook", func() error {
			_, err := s.ListHooks(ctx, store.HookQuery{Cursor: store.EncodeCursor("hooks", "lvm", "a\x00")})
			return err
		}},
		{"模板", func() error {
			_, err := s.ListTemplates(ctx, store.TemplateQuery{Cursor: store.EncodeCursor("templates", "\x00", "a")})
			return err
		}},
		{"任务", func() error {
			_, err := s.ListTasks(ctx, store.TaskQuery{Cursor: store.EncodeCursor("tasks", int64(1), "a\x00")})
			return err
		}},
	} {
		if err := c.list(); !errors.Is(err, store.ErrInvalid) {
			t.Errorf("%s的游标藏着 NUL 应报 ErrInvalid，得到 %v", c.name, err)
		}
	}
}

// 失败注入只在测试里打开：New 构造的仓储，FaultActor 只是一个普通的操作者。
func TestNew不注入失败(t *testing.T) {
	s := memstore.New()
	a := store.Audit{Actor: storetest.FaultActor, Action: "host.create", At: 1_790_000_000}
	if _, err := s.CreateHost(t.Context(), model.Host{SN: "SN-1"}, a); err != nil {
		t.Fatalf("New 构造的仓储不该注入失败：%v", err)
	}
	p, err := s.ListAudit(t.Context(), store.AuditQuery{})
	if err != nil {
		t.Fatalf("列出审计：%v", err)
	}
	if len(p.Items) != 1 || p.Items[0].Actor != storetest.FaultActor {
		t.Fatalf("审计应写下这一条，得到 %+v", p.Items)
	}
}
