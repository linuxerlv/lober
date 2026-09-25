// 本文件实现 contract.ArtifactSetStore 的六个方法，对应实现计划任务 8
// （artifact 三分之一），依据 SPEC §2.1、§2.7。
//
// 状态更新（UpdateArtifactSetState）与内容写回（SetArtifactSetContent）
// 是两个独立方法，这是刻意的设计：导入失败时只改状态、不该碰已写入的
// Paths——Paths 是识别与抽取完成后才知道的产物，登记时运维只给名字与
// 本地路径。两者用 GORM 的 Select 显式指定要更新的列，各自只碰自己
// 负责的那部分列，互不覆盖（row.go 的文件头说明了这与旧 SQLite 实现
// read-modify-write 手法的区别：本次 FailReason 有专属列，不需要先读出
// 整列再拼回去）。
package artifact

import (
	"context"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
)

// Store 实现 contract.ArtifactSetStore。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store（计划任务 8 的接口契约：与任务 3
// 同一形态，统一收 *gorm.DB，组合器与 RunInTx 都靠这个统一签名）。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// CreateArtifactSet 登记一个新制品集，初始状态通常是 importing。
func (s *Store) CreateArtifactSet(ctx context.Context, set model.ArtifactSet) (model.ArtifactSet, error) {
	row := fromModel(set)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return model.ArtifactSet{}, pgerr.Translate(err, "CreateArtifactSet")
	}
	return row.toModel(), nil
}

// GetArtifactSet 按 id 查制品集。查不到返回 contract.ErrNotFound。
func (s *Store) GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error) {
	var row artifactSetRow
	if err := s.db.WithContext(ctx).Where("id = ?", id).First(&row).Error; err != nil {
		return model.ArtifactSet{}, pgerr.Translate(err, "GetArtifactSet")
	}
	return row.toModel(), nil
}

// ListArtifactSets 列出全部制品集，供选集时按四元组过滤（过滤逻辑在
// 调用方）。空库返回空切片而不是 nil——调用方（控制台 handler）不需要
// 为「没有任何制品集」这个正常状态专门处理 nil 分支。
func (s *Store) ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error) {
	var rows []artifactSetRow
	if err := s.db.WithContext(ctx).Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListArtifactSets")
	}

	out := make([]model.ArtifactSet, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toModel())
	}
	return out, nil
}

// UpdateArtifactSetState 只改 State、ManifestSHA256 与 FailReason，不触及
// Paths 与 OSFamily——导入失败路径调用本方法，失败时 Paths 可能尚未写入
// 或只写了一半，绝不能被本方法覆盖成半成品。
//
// 用 Select 显式指定要更新的列（不是让 GORM 按零值推断）：reason 为空
// 字符串是「没有失败原因」的正常表达，若不显式 Select，GORM 的 Updates
// 默认会跳过零值字段，导致清空 FailReason 这个操作本身失效——成功路径
// 调用本方法时 reason 传空字符串正是要把上一次的失败原因清掉。
func (s *Store) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error) {
	result := s.db.WithContext(ctx).Model(&artifactSetRow{}).
		Where("id = ?", id).
		Select("state", "manifest_sha256", "fail_reason").
		Updates(artifactSetRow{
			State:          string(state),
			ManifestSHA256: manifestSHA256,
			FailReason:     reason,
		})
	if result.Error != nil {
		return model.ArtifactSet{}, pgerr.Translate(result.Error, "UpdateArtifactSetState")
	}
	if result.RowsAffected == 0 {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return s.GetArtifactSet(ctx, id)
}

// SetArtifactSetContent 写回导入完成后才知道的内容：OSFamily 识别结果与
// Paths。只改这两项，不触及 State/ManifestSHA256/FailReason——导入 job
// 成功路径调用本方法。
//
// 同样用 Select 显式指定列：Paths 的四个子字段（Kernel/Initrd/Repo/Extra）
// 都可能是零值（空字符串、nil 切片、nil map），必须显式声明才能让「写入
// 空值」这个操作本身生效，而不是被 GORM 的零值跳过规则悄悄吃掉。
func (s *Store) SetArtifactSetContent(ctx context.Context, id string, identity model.ArtifactIdentity, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	// arch/firmware/os_version 三列必须与 os_family、paths 同批写入：precheck
	// 的两道硬闸门直接比对 arch 与 firmware（statemachine/precheck.go 的
	// checkArch / checkFirmware），少写任何一列都会让制品集进到 ready 却在
	// 每次 /v1/boot 被拒。用 Select 显式列出全部列名，因为 GORM 的
	// Updates(struct) 默认跳过零值字段——firmware 取 "any" 时非零没问题，
	// 但 os_version 对 Debian 介质常常是空串，不显式 Select 就会被跳过，
	// 留下上一次导入的旧值。
	result := s.db.WithContext(ctx).Model(&artifactSetRow{}).
		Where("id = ?", id).
		Select("os_family", "os_version", "arch", "firmware",
			"paths_kernel", "paths_initrd", "paths_repo", "paths_extra").
		Updates(artifactSetRow{
			OSFamily:    string(identity.OSFamily),
			OSVersion:   identity.OSVersion,
			Arch:        string(identity.Arch),
			Firmware:    string(identity.Firmware),
			PathsKernel: paths.Kernel,
			PathsInitrd: paths.Initrd,
			PathsRepo:   paths.Repo,
			PathsExtra:  paths.Extra,
		})
	if result.Error != nil {
		return model.ArtifactSet{}, pgerr.Translate(result.Error, "SetArtifactSetContent")
	}
	if result.RowsAffected == 0 {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return s.GetArtifactSet(ctx, id)
}

// DeleteArtifactSet 删除制品集。若仍被某个 active 状态的 template 引用，
// 返回 contract.ErrStillReferenced（§2.7：只能 deprecate，不能删）。
// deprecated 的模板引用不阻止删除。
//
// 引用检查直接查 templates 表的原始表名/列名，不 import template 包——
// 两个包是并行任务的产物，本包只允许碰 artifact_sets 表（计划任务 8
// 「只写 artifact 下的文件，别碰别的包」），跨包耦合只能通过数据库层面
// 的表名约定，不能通过 Go 类型依赖。
func (s *Store) DeleteArtifactSet(ctx context.Context, id string) error {
	var refCount int64
	err := s.db.WithContext(ctx).
		Table("templates").
		Where("artifact_set_id = ? AND state = ?", id, string(model.TemplateStateActive)).
		Count(&refCount).Error
	if err != nil {
		return pgerr.Translate(err, "DeleteArtifactSet(check_refs)")
	}
	if refCount > 0 {
		return contract.ErrStillReferenced
	}

	result := s.db.WithContext(ctx).Where("id = ?", id).Delete(&artifactSetRow{})
	if result.Error != nil {
		return pgerr.Translate(result.Error, "DeleteArtifactSet")
	}
	return nil
}
