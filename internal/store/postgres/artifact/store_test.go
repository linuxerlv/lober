// 本文件测试 Store 的六个方法，对应实现计划任务 8（artifact 三分之一），
// 依据 SPEC §2.1、§2.7 与 contract.ArtifactSetStore 的文档。
//
// 夹具用 newTestDB（见下）而不是直接 pgtest.NewDB(t, Models()...)：
// DeleteArtifactSet 的引用检查要查 templates 表，那张表不属于本包。
package artifact

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

// minimalTemplateRow 是 templates 表的最小行结构体，只声明
// DeleteArtifactSet 引用检查用到的四列（id、version、artifact_set_id、
// state），字段与列名严格对应 schema/001_init.sql（已移除） 的 templates 表。
//
// 为什么不 import template 包：那会让两个聚合包互相耦合，而按聚合拆包的
// 全部意义就是它们彼此独立（设计文档 §3）。template 包同样用一个
// minimalTaskRow 桩解决它查 tasks 表的对称问题，两处是同一个模式。
//
// 不追求还原 templates 表的全部列——AutoMigrate 会照单建列，本包的测试
// 只需要这四列真实存在并可写入。
type minimalTemplateRow struct {
	ID            string `gorm:"column:id;primaryKey"`
	Version       int    `gorm:"column:version;primaryKey"`
	ArtifactSetID string `gorm:"column:artifact_set_id;not null"`
	State         string `gorm:"column:state;not null"`
}

func (minimalTemplateRow) TableName() string {
	return "templates"
}

// newTestDB 建一个独立 schema，含本包的表与上面那张最小 templates 表。
func newTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return pgtest.NewDB(t, append(Models(), &minimalTemplateRow{})...)
}

// insertTemplate 直接用 db.Table("templates") 写一行最小模板记录，供
// DeleteArtifactSet 的引用检查测试构造「有 active 模板引用该制品集」的场景。
func insertTemplate(t *testing.T, db *gorm.DB, id string, version int, artifactSetID string, state model.TemplateState) {
	t.Helper()
	err := db.Table("templates").Create(map[string]any{
		"id":              id,
		"version":         version,
		"artifact_set_id": artifactSetID,
		"state":           string(state),
	}).Error
	if err != nil {
		t.Fatalf("insertTemplate 失败: %v", err)
	}
}

// TestCreateGetRoundTrip 覆盖登记 → 按 id 查 → 内容一致，这是六个方法里
// 最基础的往返（计划任务 8「测试必须覆盖」第一条）。
func TestCreateGetRoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:              "artifact-rocky9",
		Name:            "rocky9.4",
		OSFamily:        model.OSFamilyRHEL,
		OSVersion:       "9.4",
		Arch:            model.ArchX86_64,
		Firmware:        model.FirmwareAny,
		SourceISOSHA256: "sha-abc",
		State:           model.ArtifactSetImporting,
		// Channel 显式写成 installer：写入时归一化（零值折叠成 installer），
		// 往返回来必然是这个值。留零值会让下面两处 cmp.Diff 报一处预期内的差异。
		Channel: model.ChannelInstaller,
	}

	created, err := store.CreateArtifactSet(ctx, set)
	if err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	if diff := cmp.Diff(set, created); diff != "" {
		t.Errorf("CreateArtifactSet 返回值与输入不符（-want +got）:\n%s", diff)
	}

	got, err := store.GetArtifactSet(ctx, set.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if diff := cmp.Diff(set, got); diff != "" {
		t.Errorf("GetArtifactSet 返回值与登记时不符（-want +got）:\n%s", diff)
	}
}

// TestGetArtifactSet_NotFound 验证查不到时返回 contract.ErrNotFound。
func TestGetArtifactSet_NotFound(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	_, err := store.GetArtifactSet(ctx, "does-not-exist")
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("GetArtifactSet(不存在的 id) = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
	}
}

// TestListArtifactSets_EmptyReturnsEmptySlice 验证空库返回空切片而不是
// nil（计划任务 8「测试必须覆盖」第二条：ListArtifactSets 空库返回空切片）。
func TestListArtifactSets_EmptyReturnsEmptySlice(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	got, err := store.ListArtifactSets(ctx)
	if err != nil {
		t.Fatalf("ListArtifactSets 失败: %v", err)
	}
	if got == nil {
		t.Error("ListArtifactSets(空库) = nil, want 空切片（不是 nil）")
	}
	if len(got) != 0 {
		t.Errorf("ListArtifactSets(空库) 长度 = %d, want 0", len(got))
	}
}

// TestListArtifactSets_ReturnsAll 验证列出全部制品集。
func TestListArtifactSets_ReturnsAll(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	sets := []model.ArtifactSet{
		{ID: "a1", Name: "set-a", OSFamily: model.OSFamilyRHEL, OSVersion: "9", Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady},
		{ID: "a2", Name: "set-b", OSFamily: model.OSFamilyDebian, OSVersion: "12", Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady},
	}
	for _, s := range sets {
		if _, err := store.CreateArtifactSet(ctx, s); err != nil {
			t.Fatalf("CreateArtifactSet(%s) 失败: %v", s.ID, err)
		}
	}

	got, err := store.ListArtifactSets(ctx)
	if err != nil {
		t.Fatalf("ListArtifactSets 失败: %v", err)
	}
	if len(got) != len(sets) {
		t.Fatalf("ListArtifactSets 长度 = %d, want %d", len(got), len(sets))
	}
}

// TestUpdateArtifactSetState_DoesNotClobberPaths 验证
// UpdateArtifactSetState 之后 Paths 保持不变——这是「状态更新与内容写回
// 是两个独立方法」这条设计的验证点，计划任务 8 明确要求必须有此测试。
func TestUpdateArtifactSetState_DoesNotClobberPaths(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:        "artifact-clobber-check",
		Name:      "clobber-check",
		OSFamily:  model.OSFamilyRHEL,
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetImporting,
	}
	if _, err := store.CreateArtifactSet(ctx, set); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	paths := model.ArtifactPaths{
		Kernel: "rocky9.4/vmlinuz",
		Initrd: []string{"rocky9.4/initrd.img"},
		Repo:   "rocky9.4/repo/",
		Extra:  map[string]string{"boot_cfg": "rocky9.4/boot.cfg"},
	}
	if _, err := store.SetArtifactSetContent(ctx, set.ID, model.OSFamilyRHEL, paths); err != nil {
		t.Fatalf("SetArtifactSetContent 失败: %v", err)
	}

	updated, err := store.UpdateArtifactSetState(ctx, set.ID, model.ArtifactSetFailed, "manifest-sha", "识别失败")
	if err != nil {
		t.Fatalf("UpdateArtifactSetState 失败: %v", err)
	}
	if updated.State != model.ArtifactSetFailed {
		t.Errorf("UpdateArtifactSetState 之后 State = %q, want %q", updated.State, model.ArtifactSetFailed)
	}
	if updated.FailReason != "识别失败" {
		t.Errorf("UpdateArtifactSetState 之后 FailReason = %q, want %q", updated.FailReason, "识别失败")
	}
	if diff := cmp.Diff(paths, updated.Paths); diff != "" {
		t.Errorf("UpdateArtifactSetState 之后 Paths 被改变（-want +got）:\n%s —— "+
			"这正是 UpdateArtifactSetState 与 SetArtifactSetContent 分成两个方法的全部理由", diff)
	}
	if updated.OSFamily != model.OSFamilyRHEL {
		t.Errorf("UpdateArtifactSetState 之后 OSFamily = %q, want 保持 %q 不变", updated.OSFamily, model.OSFamilyRHEL)
	}

	got, err := store.GetArtifactSet(ctx, set.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if diff := cmp.Diff(paths, got.Paths); diff != "" {
		t.Errorf("重新查询后 Paths 与写入时不符（-want +got）:\n%s", diff)
	}
}

// TestSetArtifactSetContent_RoundTrip 验证 SetArtifactSetContent 之后
// 能读回完整 Paths（计划任务 8「测试必须覆盖」第四条），含非空切片
// （Initrd）与非空 map（Extra）——这两类字段最容易在序列化时掉字段。
func TestSetArtifactSetContent_RoundTrip(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:        "artifact-content-roundtrip",
		Name:      "content-roundtrip",
		OSFamily:  model.OSFamilyESXi,
		OSVersion: "8.0",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareUEFI,
		State:     model.ArtifactSetImporting,
	}
	if _, err := store.CreateArtifactSet(ctx, set); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	paths := model.ArtifactPaths{
		Kernel: "esxi8/mboot.efi",
		Initrd: []string{"esxi8/initrd1.img", "esxi8/initrd2.img"},
		Repo:   "esxi8/repo/",
		Extra: map[string]string{
			"boot_cfg": "esxi8/boot.cfg",
		},
	}

	updated, err := store.SetArtifactSetContent(ctx, set.ID, model.OSFamilyESXi, paths)
	if err != nil {
		t.Fatalf("SetArtifactSetContent 失败: %v", err)
	}
	if updated.OSFamily != model.OSFamilyESXi {
		t.Errorf("SetArtifactSetContent 返回值 OSFamily = %q, want %q", updated.OSFamily, model.OSFamilyESXi)
	}
	if diff := cmp.Diff(paths, updated.Paths); diff != "" {
		t.Errorf("SetArtifactSetContent 返回值 Paths 不符（-want +got）:\n%s", diff)
	}

	got, err := store.GetArtifactSet(ctx, set.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if diff := cmp.Diff(paths, got.Paths); diff != "" {
		t.Errorf("重新查询后 Paths 与写入时不符（-want +got）:\n%s", diff)
	}
}

// TestSetArtifactSetContent_DoesNotClobberFailReason 验证
// SetArtifactSetContent 不清空已有的 FailReason——与
// UpdateArtifactSetState 不碰 Paths 对称的另一半保证（旧实现
// sqlite/artifact.go 对此有专门注释：「不能依赖调用方一定会紧接着清空，
// 独立保证不主动清空已有 FailReason 才是本方法自己的职责」）。
func TestSetArtifactSetContent_DoesNotClobberFailReason(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:        "artifact-keep-fail-reason",
		Name:      "keep-fail-reason",
		OSFamily:  model.OSFamilyRHEL,
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetFailed,
	}
	if _, err := store.CreateArtifactSet(ctx, set); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	if _, err := store.UpdateArtifactSetState(ctx, set.ID, model.ArtifactSetFailed, "", "抽取失败"); err != nil {
		t.Fatalf("UpdateArtifactSetState 失败: %v", err)
	}

	if _, err := store.SetArtifactSetContent(ctx, set.ID, model.OSFamilyRHEL, model.ArtifactPaths{Kernel: "vmlinuz"}); err != nil {
		t.Fatalf("SetArtifactSetContent 失败: %v", err)
	}

	got, err := store.GetArtifactSet(ctx, set.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if got.FailReason != "抽取失败" {
		t.Errorf("SetArtifactSetContent 之后 FailReason = %q, want 保持 %q 不变", got.FailReason, "抽取失败")
	}
}

// TestDeleteArtifactSet_ThenGetReturnsNotFound 验证 DeleteArtifactSet
// 删掉后 Get 返回 ErrNotFound（计划任务 8「测试必须覆盖」第五条）。
func TestDeleteArtifactSet_ThenGetReturnsNotFound(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:        "artifact-to-delete",
		Name:      "to-delete",
		OSFamily:  model.OSFamilyUbuntu,
		OSVersion: "24.04",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetReady,
	}
	if _, err := store.CreateArtifactSet(ctx, set); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	if err := store.DeleteArtifactSet(ctx, set.ID); err != nil {
		t.Fatalf("DeleteArtifactSet 失败: %v", err)
	}

	_, err := store.GetArtifactSet(ctx, set.ID)
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("删除后 GetArtifactSet = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
	}
}

// TestDeleteArtifactSet_NoSuchID 验证删除一个不存在的 id 时的行为——
// 不能 panic，返回值本身不是契约强制规定的（契约文档只规定「被引用时
// 返回 ErrStillReferenced」），这里断言不 panic 且不误报已引用。
func TestDeleteArtifactSet_NoSuchID(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	err := store.DeleteArtifactSet(ctx, "does-not-exist")
	if errors.Is(err, contract.ErrStillReferenced) {
		t.Errorf("DeleteArtifactSet(不存在的 id) = %v, 不应误报 ErrStillReferenced", err)
	}
}

// TestDeleteArtifactSet_ReferencedByActiveTemplate 验证仍被 active 状态
// template 引用的制品集不能删除，返回 ErrStillReferenced（§2.7）。
//
// 本测试直接用 db.Table 写入一行 templates 记录，不 import template 包
// ——template 包是另一个并行任务的产物，本包只允许碰 artifact_sets 表，
// 与它的耦合只能通过原始表名/列名，不能通过 Go 类型（计划任务 8「只写
// artifact 下的文件，别碰别的包」）。
func TestDeleteArtifactSet_ReferencedByActiveTemplate(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:        "artifact-referenced",
		Name:      "referenced",
		OSFamily:  model.OSFamilyRHEL,
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetReady,
	}
	if _, err := store.CreateArtifactSet(ctx, set); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	insertTemplate(t, db, "tpl-1", 1, set.ID, model.TemplateStateActive)

	err := store.DeleteArtifactSet(ctx, set.ID)
	if !errors.Is(err, contract.ErrStillReferenced) {
		t.Errorf("DeleteArtifactSet(被 active 模板引用) = %v, want errors.Is(_, contract.ErrStillReferenced) = true", err)
	}

	if _, getErr := store.GetArtifactSet(ctx, set.ID); getErr != nil {
		t.Errorf("拦截删除后 GetArtifactSet 应仍能查到该制品集, got err: %v", getErr)
	}
}

// TestDeleteArtifactSet_ReferencedByDeprecatedTemplate_CanDelete 验证
// deprecated 状态的模板引用不阻止删除——只有 active 状态的引用才拦截
// （§2.7：deprecated 的引用不阻止删除，这是与「仍被引用」条件的边界）。
func TestDeleteArtifactSet_ReferencedByDeprecatedTemplate_CanDelete(t *testing.T) {
	db := newTestDB(t)
	store := New(db)
	ctx := context.Background()

	set := model.ArtifactSet{
		ID:        "artifact-deprecated-ref",
		Name:      "deprecated-ref",
		OSFamily:  model.OSFamilyRHEL,
		OSVersion: "9.4",
		Arch:      model.ArchX86_64,
		Firmware:  model.FirmwareAny,
		State:     model.ArtifactSetReady,
	}
	if _, err := store.CreateArtifactSet(ctx, set); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	insertTemplate(t, db, "tpl-2", 1, set.ID, model.TemplateStateDeprecated)

	if err := store.DeleteArtifactSet(ctx, set.ID); err != nil {
		t.Fatalf("DeleteArtifactSet(仅被 deprecated 模板引用) 失败: %v, want 成功", err)
	}

	_, err := store.GetArtifactSet(ctx, set.ID)
	if !errors.Is(err, contract.ErrNotFound) {
		t.Errorf("删除后 GetArtifactSet = %v, want errors.Is(_, contract.ErrNotFound) = true", err)
	}
}
