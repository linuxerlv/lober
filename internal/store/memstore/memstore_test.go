package memstore_test

import (
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
