// 本文件测试 pgerr.Translate，对应实现计划任务 2。
//
// 阻塞说明（同一段落也会写进 StructuredOutput 的 blockers）：本包按设计
// 要求还需处理 *pgconn.PgError（Code == "23505" 时按 ConstraintName 映射
// 到 ErrActiveTaskExists / ErrDuplicateSeq），但 github.com/jackc/pgx/v5
// 未出现在 go.mod / go.sum 里（任务交付说明称该依赖已就位，实测不成立，
// 见下方长注释与 go.mod 现状）。硬性约束第 4 条禁止本任务自行
// go get，因此本文件只覆盖当前可编译验证的三类：nil、
// gorm.ErrRecordNotFound、未识别错误的 %w 包装。23505 分支的测试与实现
// 留空，等依赖补齐后补上——不伪造一个本地假的 pgconn.PgError 类型来让
// 测试自欺式通过，那样产出的代码在真实 pgx 错误面前根本不会走到该分支，
// 比「明确留空」更危险。
package pgerr

import (
	"errors"
	"fmt"
	"testing"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/core/contract"
)

// TestTranslate_Nil 验证 err == nil 时返回 nil，不包装成任何东西。
func TestTranslate_Nil(t *testing.T) {
	got := Translate(nil, "ctx")
	if got != nil {
		t.Errorf("Translate(nil, ...) = %v, want nil", got)
	}
}

// TestTranslate_RecordNotFound 验证 gorm.ErrRecordNotFound 翻译成
// contract.ErrNotFound。
func TestTranslate_RecordNotFound(t *testing.T) {
	got := Translate(gorm.ErrRecordNotFound, "GetTask")
	if !errors.Is(got, contract.ErrNotFound) {
		t.Errorf("Translate(gorm.ErrRecordNotFound, ...) = %v, want errors.Is(_, contract.ErrNotFound) = true", got)
	}
}

// TestTranslate_RecordNotFound_Wrapped 验证 gorm.ErrRecordNotFound 被
// %w 包装后依然能被识别——GORM 的实际调用点可能经过中间层包装，Translate
// 用 errors.Is 判定而不是直接比较，才能兼容这种情况。
func TestTranslate_RecordNotFound_Wrapped(t *testing.T) {
	wrapped := fmt.Errorf("查询失败: %w", gorm.ErrRecordNotFound)
	got := Translate(wrapped, "GetTask")
	if !errors.Is(got, contract.ErrNotFound) {
		t.Errorf("Translate(包装过的 ErrRecordNotFound, ...) = %v, want errors.Is(_, contract.ErrNotFound) = true", got)
	}
}

// TestTranslate_Unknown_WrapsWithCtxLabel 验证未识别的错误用 %w 包装
// ctxLabel 后返回——不吞掉，且原错误必须能被 errors.Is/errors.As 取出，
// 未知的数据库错误应该冒泡成 500 并进服务端日志（计划任务 2 设计要点）。
func TestTranslate_Unknown_WrapsWithCtxLabel(t *testing.T) {
	original := errors.New("connection reset by peer")

	got := Translate(original, "CreateTask")

	if got == nil {
		t.Fatal("Translate(未识别错误, ...) = nil, want 非 nil")
	}
	if !errors.Is(got, original) {
		t.Errorf("Translate(...) = %v, want errors.Is(_, original) = true —— "+
			"未知的数据库错误必须能被 errors.Is/errors.As 取出原错误，不能吞掉", got)
	}
	if !containsCtxLabel(got.Error(), "CreateTask") {
		t.Errorf("Translate(...).Error() = %q, want 包含 ctxLabel %q", got.Error(), "CreateTask")
	}
}

// containsCtxLabel 是测试内部的小工具，避免直接在断言里写 strings.Contains
// 影响可读性。
func containsCtxLabel(errMsg, label string) bool {
	for i := 0; i+len(label) <= len(errMsg); i++ {
		if errMsg[i:i+len(label)] == label {
			return true
		}
	}
	return false
}
