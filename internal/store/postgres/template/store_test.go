// 本文件测试 contract.TemplateStore 的五个方法，对应实现计划任务 9 的一半
// （template 聚合），依据 SPEC §2.3、§2.6、§2.7。
package template

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgtest"
)

// newTestStore 建一个独立 schema 并跑完 AutoMigrate（含 tasks 表——
// DeleteTemplate 的引用检查要查 tasks，测试里也需要这张表存在），返回
// 一个可直接用的 *Store。
//
// tasks 表在这里用最小的匿名行结构体建出来，不 import task 包——task 包
// 由另一个 agent 并行开发，import 会撞上未完成的代码，也会让两个聚合包
// 互相耦合（任务简报「注意」一节的要求）。这个匿名结构体只声明
// DeleteTemplate 引用检查用到的三列（template_id、template_version、
// state），字段与列名严格对应 schema/001_init.sql 的 tasks 表，但不追求
// 还原 tasks 表的全部列——AutoMigrate 会照单建列，测试只需要这三列真实
// 存在并可写入即可。
type minimalTaskRow struct {
	ID              string `gorm:"column:id;primaryKey"`
	TemplateID      string `gorm:"column:template_id;not null"`
	TemplateVersion int    `gorm:"column:template_version;not null"`
	State           string `gorm:"column:state;not null"`
}

func (minimalTaskRow) TableName() string {
	return "tasks"
}

func newTestStore(t *testing.T) *Store {
	models := append(Models(), &minimalTaskRow{})
	db := pgtest.NewDB(t, models...)
	return New(db)
}

// insertTask 直接用 db.Table("tasks") 写一行最小任务记录，供 DeleteTemplate
// 的引用检查测试构造「有任务引用该模板」的场景。
func insertTask(t *testing.T, db *gorm.DB, id, templateID string, templateVersion int, state model.TaskState) {
	t.Helper()
	err := db.Table("tasks").Create(map[string]any{
		"id":               id,
		"template_id":      templateID,
		"template_version": templateVersion,
		"state":            string(state),
	}).Error
	if err != nil {
		t.Fatalf("insertTask 失败: %v", err)
	}
}

func sampleTemplate(id string, version int, refs []model.HookRef) model.Template {
	return model.Template{
		ID:                 id,
		Name:               "示例模板",
		ArtifactSetID:      "artifact-x",
		AnswerTemplate:     "# body",
		KernelArgs:         "",
		DiskPolicy:         model.DiskPolicyFirst,
		OnFailure:          model.OnFailureReboot,
		RequiredFormFields: []string{"hostname"},
		HookRefs:           refs,
		Version:            version,
		State:              model.TemplateStateActive,
	}
}

// TestCreateAndGetTemplate_HookRefsOrderAndContent 验证 CreateTemplate 带
// 若干 hook_refs 后，GetTemplate 读回的 refs 顺序与内容都要与写入时一致
// ——ref_order 保证顺序，读回时要按它排序（任务简报「测试必须覆盖」的
// 第一条）。故意打乱写入顺序（先写 Order=2 的引用，再写 Order=1 的），
// 断言读回结果按 Order 升序排列。
func TestCreateAndGetTemplate_HookRefsOrderAndContent(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	refs := []model.HookRef{
		{HookID: "hook-b", HookDigest: "digest-b", Stage: model.HookStagePost, Order: 2, Required: false, ParamsDefaults: map[string]string{"k": "v"}},
		{HookID: "hook-a", HookDigest: "digest-a", Stage: model.HookStagePre, Order: 1, Required: true},
	}
	tpl := sampleTemplate("tpl-1", 1, refs)

	if _, err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	got, err := store.GetTemplate(ctx, "tpl-1", 1)
	if err != nil {
		t.Fatalf("GetTemplate 失败: %v", err)
	}

	want := []model.HookRef{
		{HookID: "hook-a", HookDigest: "digest-a", Stage: model.HookStagePre, Order: 1, Required: true},
		{HookID: "hook-b", HookDigest: "digest-b", Stage: model.HookStagePost, Order: 2, Required: false, ParamsDefaults: map[string]string{"k": "v"}},
	}
	if diff := cmp.Diff(want, got.HookRefs); diff != "" {
		t.Errorf("GetTemplate().HookRefs 与写入时不符（-want +got）:\n%s", diff)
	}
	if got.AnswerTemplate != tpl.AnswerTemplate {
		t.Errorf("GetTemplate().AnswerTemplate = %q, want %q", got.AnswerTemplate, tpl.AnswerTemplate)
	}
}

// TestGetTemplate_ExactVersionZeroNegativeAndNotFound 覆盖取确切版本、
// version 传 0、传 -1、不存在四种情况（任务简报「测试必须覆盖」第二条）。
// 0 和负数都要走取最新，不能只实现 0。
func TestGetTemplate_ExactVersionZeroNegativeAndNotFound(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-2", 1, nil)); err != nil {
		t.Fatalf("创建 v1 失败: %v", err)
	}
	if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-2", 2, nil)); err != nil {
		t.Fatalf("创建 v2 失败: %v", err)
	}

	t.Run("取确切版本", func(t *testing.T) {
		got, err := store.GetTemplate(ctx, "tpl-2", 1)
		if err != nil {
			t.Fatalf("GetTemplate(version=1) 失败: %v", err)
		}
		if got.Version != 1 {
			t.Errorf("Version = %d, want 1", got.Version)
		}
	})

	t.Run("version 传 0 取最新", func(t *testing.T) {
		got, err := store.GetTemplate(ctx, "tpl-2", 0)
		if err != nil {
			t.Fatalf("GetTemplate(version=0) 失败: %v", err)
		}
		if got.Version != 2 {
			t.Errorf("Version = %d, want 2（version=0 应取最新版）", got.Version)
		}
	})

	t.Run("version 传 -1 取最新", func(t *testing.T) {
		got, err := store.GetTemplate(ctx, "tpl-2", -1)
		if err != nil {
			t.Fatalf("GetTemplate(version=-1) 失败: %v", err)
		}
		if got.Version != 2 {
			t.Errorf("Version = %d, want 2（version=-1 应取最新版，不能只实现 0）", got.Version)
		}
	})

	t.Run("id 不存在", func(t *testing.T) {
		_, err := store.GetTemplate(ctx, "tpl-does-not-exist", 0)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("err = %v, want errors.Is(err, ErrNotFound)", err)
		}
	})
}

// TestListTemplates_ReturnsAllVersions 验证 ListTemplates 返回全部版本
// （任务简报「测试必须覆盖」第三条）。
func TestListTemplates_ReturnsAllVersions(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-3", 1, nil)); err != nil {
		t.Fatalf("创建 v1 失败: %v", err)
	}
	if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-3", 2, nil)); err != nil {
		t.Fatalf("创建 v2 失败: %v", err)
	}
	if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-4", 1, nil)); err != nil {
		t.Fatalf("创建 tpl-4 v1 失败: %v", err)
	}

	got, err := store.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates 失败: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("ListTemplates 返回 %d 条，want 3（tpl-3 两个版本 + tpl-4 一个版本）", len(got))
	}
}

// TestUpdateTemplateState 验证改状态（任务简报「测试必须覆盖」第四条）。
func TestUpdateTemplateState(t *testing.T) {
	store := newTestStore(t)
	ctx := context.Background()

	if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-5", 1, nil)); err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	got, err := store.UpdateTemplateState(ctx, "tpl-5", 1, model.TemplateStateDeprecated)
	if err != nil {
		t.Fatalf("UpdateTemplateState 失败: %v", err)
	}
	if got.State != model.TemplateStateDeprecated {
		t.Errorf("State = %q, want %q", got.State, model.TemplateStateDeprecated)
	}

	reread, err := store.GetTemplate(ctx, "tpl-5", 1)
	if err != nil {
		t.Fatalf("重读失败: %v", err)
	}
	if reread.State != model.TemplateStateDeprecated {
		t.Errorf("重读 State = %q, want %q（状态更新未持久化）", reread.State, model.TemplateStateDeprecated)
	}
}

// TestDeleteTemplate 覆盖三种情况：无引用可删、有非终态 task 引用（期望
// ErrStillReferenced）、引用的 task 已终态则可删（任务简报「测试必须
// 覆盖」第五条）。
func TestDeleteTemplate(t *testing.T) {
	t.Run("无引用可删", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()

		if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-del-1", 1, []model.HookRef{
			{HookID: "hook-a", HookDigest: "digest-a", Stage: model.HookStagePre, Order: 1},
		})); err != nil {
			t.Fatalf("创建失败: %v", err)
		}

		if err := store.DeleteTemplate(ctx, "tpl-del-1", 1); err != nil {
			t.Fatalf("DeleteTemplate 失败: %v", err)
		}

		_, err := store.GetTemplate(ctx, "tpl-del-1", 1)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("删除后 GetTemplate err = %v, want ErrNotFound", err)
		}
	})

	t.Run("有非终态 task 引用期望 ErrStillReferenced", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()

		if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-del-2", 1, nil)); err != nil {
			t.Fatalf("创建失败: %v", err)
		}
		insertTask(t, store.db, "task-1", "tpl-del-2", 1, model.TaskStatePending)

		err := store.DeleteTemplate(ctx, "tpl-del-2", 1)
		if !errors.Is(err, contract.ErrStillReferenced) {
			t.Errorf("err = %v, want errors.Is(err, ErrStillReferenced)", err)
		}

		// 模板必须仍然存在——拦截失败的删除不能是「先删了一部分」。
		if _, err := store.GetTemplate(ctx, "tpl-del-2", 1); err != nil {
			t.Errorf("拦截删除后模板应仍存在，GetTemplate 却返回: %v", err)
		}
	})

	t.Run("引用的 task 已终态则可删", func(t *testing.T) {
		store := newTestStore(t)
		ctx := context.Background()

		if _, err := store.CreateTemplate(ctx, sampleTemplate("tpl-del-3", 1, nil)); err != nil {
			t.Fatalf("创建失败: %v", err)
		}
		insertTask(t, store.db, "task-2", "tpl-del-3", 1, model.TaskStateDone)

		if err := store.DeleteTemplate(ctx, "tpl-del-3", 1); err != nil {
			t.Fatalf("DeleteTemplate 失败: %v，终态 task 引用的模板应可删", err)
		}

		_, err := store.GetTemplate(ctx, "tpl-del-3", 1)
		if !errors.Is(err, contract.ErrNotFound) {
			t.Errorf("删除后 GetTemplate err = %v, want ErrNotFound", err)
		}
	})
}
