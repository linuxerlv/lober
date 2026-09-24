// 本文件测试 New：八个嵌入指针必须全部非 nil。对应实现计划任务 10。
//
// 为什么这条测试值得单独写。八个嵌入指针若有任一为 nil，调用落在该
// 嵌入类型方法集里的方法会在运行期 panic（nil pointer dereference），
// 而不是编译期报错——Go 只检查 *Store 的方法集是否满足 contract.Store，
// 不检查嵌入指针本身是否为 nil。这条测试用反射遍历八个嵌入字段，比
// 「调用某个方法确认不 panic」更直接地覆盖全部八个字段，也不依赖
// 「调用了但恰好该聚合的这个方法不触发 nil 解引用」这种偶然性。
package postgres

import (
	"context"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// TestNew_AllEightEmbeddedStoresAreNonNil 用反射遍历 *Store 的匿名嵌入
// 字段，断言每一个都是非 nil 指针。
func TestNew_AllEightEmbeddedStoresAreNonNil(t *testing.T) {
	db := pgtest.NewDB(t, AllModels()...)
	s := New(db)

	v := reflect.ValueOf(*s)
	typ := v.Type()

	embeddedCount := 0
	for i := 0; i < typ.NumField(); i++ {
		field := typ.Field(i)
		if !field.Anonymous {
			continue // db、inTx 不是匿名嵌入字段，跳过
		}
		embeddedCount++

		fv := v.Field(i)
		if fv.Kind() != reflect.Ptr {
			t.Errorf("嵌入字段 %s 期望是指针类型，实际 kind=%v", field.Name, fv.Kind())
			continue
		}
		if fv.IsNil() {
			t.Errorf("嵌入字段 %s 是 nil——New 必须把八个聚合全部构造出来，否则该字段"+
				"方法集里的方法会在运行期 panic 而不是编译期报错", field.Name)
		}
	}

	const wantEmbedded = 8
	if embeddedCount != wantEmbedded {
		t.Errorf("期望 *Store 有 %d 个匿名嵌入字段，实际 %d 个——本测试的覆盖面依赖"+
			"这个数字，八个聚合包数量变化时本测试也要跟着改", wantEmbedded, embeddedCount)
	}
}

// TestNew_TopLevelStoreCanCallEachAggregateWithoutPanic 用只读方法各调
// 一次八个聚合，作为反射断言之外的第二重证据——即便反射遍历漏掉了
// 什么，真实调用一次也能兜住。
func TestNew_TopLevelStoreCanCallEachAggregateWithoutPanic(t *testing.T) {
	db := pgtest.NewDB(t, AllModels()...)
	s := New(db)
	ctx := context.Background()

	if _, err := s.ListTasks(ctx, contract.TaskFilter{}); err != nil {
		t.Errorf("task.Store.ListTasks() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.ListHosts(ctx); err != nil {
		t.Errorf("host.Store.ListHosts() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.ListHooks(ctx); err != nil {
		t.Errorf("hook.Store.ListHooks() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.ListTemplates(ctx); err != nil {
		t.Errorf("template.Store.ListTemplates() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.ListArtifactSets(ctx); err != nil {
		t.Errorf("artifact.Store.ListArtifactSets() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.GetFacts(ctx, "no-such-task"); err != nil {
		t.Errorf("fact.Store.GetFacts() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.ListAudit(ctx, contract.AuditFilter{}); err != nil {
		t.Errorf("audit.Store.ListAudit() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
	if _, err := s.ListEvents(ctx, "no-such-task", contract.EventFilter{}); err != nil {
		t.Errorf("event.Store.ListEvents() 应该在空库上正常返回空结果，实际报错: %v", err)
	}
}
