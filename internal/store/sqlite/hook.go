// 本文件实现 contract.HookStore：hook 版本与 digest 反查。对应实现计划
// 任务 8，依据 SPEC §2.2、§2.6、§2.7。
//
// # 与计划原文的偏差（契约缺口）
//
// 计划任务 8 描述「os_families 存成逗号分隔的文本列」，但 M1 的 schema
// （数据契约，本模块不能改）把该列命名为 os_families_json、类型 TEXT、
// 默认值 '[]'，且 schema 文件自己的注释写明「os_families 存 JSON 数组」
// ——即 schema 本身与计划任务 8 的这一句描述互相矛盾。本文件遵循 schema
// 的真实定义（JSON 数组），不是计划的文字描述：schema 是本模块必须遵守
// 的数据契约，计划文档里对 schema 内容的复述如果写错了，也不能反过来
// 要求本模块改 schema 去凑描述。
package sqlite

import (
	"context"
	"errors"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

const hookColumns = `id, version, name, description, stage, os_families_json,
	interpreter, body, body_sha256, params_schema, requires_json, required,
	timeout_sec, state, author, approver, approved_at`

type hookRowScanner interface {
	Scan(dest ...any) error
}

func scanHookRow(row hookRowScanner) (model.Hook, error) {
	var (
		h             model.Hook
		osFamiliesRaw string
		requiresRaw   string
		requiredInt   int
	)
	err := row.Scan(
		&h.ID, &h.Version, &h.Name, &h.Description, &h.Stage, &osFamiliesRaw,
		&h.Interpreter, &h.Body, &h.BodySHA256, &h.ParamsSchema, &requiresRaw, &requiredInt,
		&h.TimeoutSec, &h.State, &h.Author, &h.Approver, &h.ApprovedAt,
	)
	if err != nil {
		return model.Hook{}, err
	}
	h.Required = requiredInt != 0

	if err := decodeJSON(osFamiliesRaw, &h.OSFamilies); err != nil {
		return model.Hook{}, fmt.Errorf("sqlite: 解码 hook %q 的 os_families_json 失败: %w", h.ID, err)
	}
	if err := decodeJSON(requiresRaw, &h.Requires); err != nil {
		return model.Hook{}, fmt.Errorf("sqlite: 解码 hook %q 的 requires_json 失败: %w", h.ID, err)
	}
	return h, nil
}

// errHookBodyDigestMismatch 在写入时传入的 BodySHA256 与从 Body 现算的
// digest 不一致时返回。不导出为 contract sentinel error——这是调用方
// （M4 渲染器之外，此处特指注册 hook 的 handler）的编程错误，不是像
// ErrConflict 那样的正常业务分支。
var errHookBodyDigestMismatch = errors.New("sqlite: hook 正文与传入的 digest 不一致")

// CreateHook 创建一个新 hook 版本。写入前校验传入的 BodySHA256 与 Body
// 现算的 digest 一致——不校验的后果是库里的 digest 与正文对不上，机器侧
// 的 sha256sum -c 会失败，且从机器侧日志很难追溯到库里的数据错了（计划
// 任务 8「设计要点」）。
//
// 同一正文重复注册被拒绝而不是复用：body_sha256 有唯一索引，撞索引时
// 原样把翻译后的错误返回给调用方——两个 hook 记录指向同一正文会让审批
// 状态产生歧义（计划任务 8「实现流程」第 1 步：建议拒绝）。这条冲突不属于
// errors.go 已翻译的两种已知情形（idx_tasks_sn_active /
// idx_task_events_task_seq），会落进「无法识别的错误原样包装返回」分支，
// 冒泡成 500——调用方（控制台的 hook 注册 handler）应在应用层先查一次
// GetHookByDigest 给出更友好的错误，而不是依赖这里的 500。
func (s *Store) CreateHook(ctx context.Context, hook model.Hook) (model.Hook, error) {
	if model.HookDigest(hook.Body) != hook.BodySHA256 {
		return model.Hook{}, errHookBodyDigestMismatch
	}

	osFamiliesJSON, err := encodeJSON(nonNilOSFamilySlice(hook.OSFamilies))
	if err != nil {
		return model.Hook{}, fmt.Errorf("sqlite: 编码 os_families_json 失败: %w", err)
	}
	requiresJSON, err := encodeJSON(hook.Requires)
	if err != nil {
		return model.Hook{}, fmt.Errorf("sqlite: 编码 requires_json 失败: %w", err)
	}

	query := fmt.Sprintf(`INSERT INTO hooks (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, hookColumns)
	_, err = s.db.ExecContext(ctx, query,
		hook.ID, hook.Version, hook.Name, hook.Description, hook.Stage, osFamiliesJSON,
		hook.Interpreter, hook.Body, hook.BodySHA256, hook.ParamsSchema, requiresJSON, boolToInt(hook.Required),
		hook.TimeoutSec, hook.State, hook.Author, hook.Approver, hook.ApprovedAt,
	)
	if err != nil {
		return model.Hook{}, translateConstraintErr(err, "CreateHook")
	}
	return hook, nil
}

// nonNilOSFamilySlice 把 nil 切片归一成空切片，供 JSON 编码产出 "[]"。
func nonNilOSFamilySlice(s []model.OSFamily) []model.OSFamily {
	if s == nil {
		return []model.OSFamily{}
	}
	return s
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// GetHook 按 (id, version) 查 hook。version 非正数表示取该 id 下最新
// 版本。查不到返回 contract.ErrNotFound。
func (s *Store) GetHook(ctx context.Context, id string, version int) (model.Hook, error) {
	var row hookRowScanner
	if version > 0 {
		query := fmt.Sprintf(`SELECT %s FROM hooks WHERE id = ? AND version = ?`, hookColumns)
		row = s.db.QueryRowContext(ctx, query, id, version)
	} else {
		query := fmt.Sprintf(`SELECT %s FROM hooks WHERE id = ? ORDER BY version DESC LIMIT 1`, hookColumns)
		row = s.db.QueryRowContext(ctx, query, id)
	}
	h, err := scanHookRow(row)
	if err != nil {
		return model.Hook{}, translateConstraintErr(err, "GetHook")
	}
	return h, nil
}

// GetHookByDigest 按内容寻址 digest 反查 hook 正文。查不到返回
// contract.ErrNotFound——包括「改一个字节」的篡改 digest（唯一索引精确
// 匹配，不是模糊匹配，计划任务 8「测试要锁住的行为」）。
func (s *Store) GetHookByDigest(ctx context.Context, digest string) (model.Hook, error) {
	query := fmt.Sprintf(`SELECT %s FROM hooks WHERE body_sha256 = ?`, hookColumns)
	row := s.db.QueryRowContext(ctx, query, digest)
	h, err := scanHookRow(row)
	if err != nil {
		return model.Hook{}, translateConstraintErr(err, "GetHookByDigest")
	}
	return h, nil
}

// ListHooks 列出全部 hook（不区分版本，返回各 id 的全部版本），按版本
// 升序排列（计划任务 8「实现流程」第 4 步）。
func (s *Store) ListHooks(ctx context.Context) ([]model.Hook, error) {
	query := fmt.Sprintf(`SELECT %s FROM hooks ORDER BY id ASC, version ASC`, hookColumns)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, translateConstraintErr(err, "ListHooks")
	}
	defer rows.Close()

	out := make([]model.Hook, 0)
	for rows.Next() {
		h, err := scanHookRow(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListHooks")
		}
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListHooks")
	}
	return out, nil
}

// UpdateHookState 更新 hook 的审批状态：改状态、审批人、审批时间三列
// （计划任务 8「实现流程」第 5 步）。正文不可变——本方法不接受修改 Body。
func (s *Store) UpdateHookState(ctx context.Context, id string, version int, state model.HookState, approver string, approvedAt int64) (model.Hook, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE hooks SET state = ?, approver = ?, approved_at = ? WHERE id = ? AND version = ?`,
		state, approver, approvedAt, id, version)
	if err != nil {
		return model.Hook{}, translateConstraintErr(err, "UpdateHookState")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.Hook{}, fmt.Errorf("sqlite: UpdateHookState 读取受影响行数失败: %w", err)
	}
	if affected == 0 {
		return model.Hook{}, contract.ErrNotFound
	}
	return s.GetHook(ctx, id, version)
}
