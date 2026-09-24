// 本文件实现 contract.HookStore 的五个方法。对应实现计划任务 9（hook
// 聚合的前一半），依据 SPEC §2.2、§2.6、§2.7。
package hook

import (
	"context"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store/postgres/pgerr"
	"github.com/lober/lober/internal/store/postgres/pgtype"
)

// Store 实现 contract.HookStore。
type Store struct {
	db *gorm.DB
}

// New 用给定的 *gorm.DB 构造 Store。db 可能是顶层连接，也可能是
// RunInTx 传入的事务句柄——两者是同一个 Go 类型，Store 不需要区分
// （设计文档 §4）。
func New(db *gorm.DB) *Store {
	return &Store{db: db}
}

// CreateHook 创建一个新 hook 版本，原样写入不做 Body/BodySHA256 一致性
// 校验——契约（contract.HookStore.CreateHook）本身没有这条要求，本方法
// 照抄契约签名与语义，不额外加校验（那属于调用方注册 handler 的职责，
// 旧 sqlite 实现在这一点上加了校验，但契约文档没有把它列为 Store 层的
// 职责，本实现不重复引入）。
func (s *Store) CreateHook(ctx context.Context, hook model.Hook) (model.Hook, error) {
	row := fromModel(hook)
	if err := s.db.WithContext(ctx).Create(&row).Error; err != nil {
		return model.Hook{}, pgerr.Translate(err, "CreateHook")
	}
	return row.toModel(), nil
}

// GetHook 按 (id, version) 查 hook。version 非正数（0 或负数）表示取该
// id 下最新版本，正数表示取确切那一版。查不到返回 contract.ErrNotFound。
func (s *Store) GetHook(ctx context.Context, id string, version int) (model.Hook, error) {
	var row hookRow
	q := s.db.WithContext(ctx).Where("id = ?", id)
	if version > 0 {
		q = q.Where("version = ?", version)
	} else {
		q = q.Order("version DESC")
	}
	if err := q.First(&row).Error; err != nil {
		return model.Hook{}, pgerr.Translate(err, "GetHook")
	}
	return row.toModel(), nil
}

// GetHookByDigest 按内容寻址 digest 反查 hook 正文。查不到返回
// contract.ErrNotFound。
func (s *Store) GetHookByDigest(ctx context.Context, digest string) (model.Hook, error) {
	var row hookRow
	if err := s.db.WithContext(ctx).Where("body_sha256 = ?", digest).First(&row).Error; err != nil {
		return model.Hook{}, pgerr.Translate(err, "GetHookByDigest")
	}
	return row.toModel(), nil
}

// ListHooks 列出全部 hook（不区分版本，返回各 id 的全部版本），按 id、
// version 升序排列，与旧 sqlite 实现同一顺序（计划任务 8「实现流程」
// 对 hook 列表排序的约定，hook 与 template 共用同一份计划描述）。
func (s *Store) ListHooks(ctx context.Context) ([]model.Hook, error) {
	var rows []hookRow
	if err := s.db.WithContext(ctx).Order("id ASC, version ASC").Find(&rows).Error; err != nil {
		return nil, pgerr.Translate(err, "ListHooks")
	}
	out := make([]model.Hook, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.toModel())
	}
	return out, nil
}

// UpdateHookState 更新 hook 的审批状态（state、approver、approved_at
// 三列），正文不可变——本方法不接受修改 Body。
//
// 用 map 形式的 Updates 而不是传结构体：GORM 的 Updates 默认忽略零值
// 字段，approved_at 从非 0 改回 0 时目标恰好是「写成 SQL NULL 这个零值」，
// 结构体形式的 Updates 会把这次改动静默跳过（计划任务 5 的风险提示对
// 本方法同样适用，这是本方法最容易静默出错的一点）。
func (s *Store) UpdateHookState(ctx context.Context, id string, version int, state model.HookState, approver string, approvedAt int64) (model.Hook, error) {
	var existing hookRow
	if err := s.db.WithContext(ctx).Where("id = ? AND version = ?", id, version).First(&existing).Error; err != nil {
		return model.Hook{}, pgerr.Translate(err, "UpdateHookState")
	}

	updates := map[string]any{
		"state":       string(state),
		"approver":    approver,
		"approved_at": pgtype.NullTimeFromUnix(approvedAt),
	}
	if err := s.db.WithContext(ctx).Model(&hookRow{}).
		Where("id = ? AND version = ?", id, version).
		Updates(updates).Error; err != nil {
		return model.Hook{}, pgerr.Translate(err, "UpdateHookState")
	}

	return s.GetHook(ctx, id, version)
}
