package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test统一错误说明保留响应顺序与空详情(t *testing.T) {
	p := model.Problem{Code: "sn_invalid", Message: "原问题"}
	state := model.Problem{Code: "template_not_approved", Message: "状态问题"}
	for _, tc := range []struct {
		name    string
		err     error
		status  int
		message string
		detail  string
	}{
		{"授权仍在通用错误之前", errors.Join(repository.ErrTemplateNotCurrent, ports.ErrForbidden), 403, model.ConsoleForbidden, "null"},
		{"预览问题仍在通用错误之前", errors.Join(repository.ErrTemplateNotCurrent, &authoring.ProblemsError{Problems: []model.Problem{p}}), 422, p.Message, "problems"},
		{"任务问题仍在通用错误之前", errors.Join(repository.ErrTemplateNotCurrent, &tasks.ProblemsError{Problems: []model.Problem{p}}), 422, p.Message, "problems"},
		{"模板当前优先", errors.Join(&repository.ReferencedError{}, repository.ErrTemplateNotCurrent), 409, model.ConsoleTemplateCurrent, "null"},
		{"最后管理员优先", errors.Join(&repository.BatchError{}, repository.ErrLastAdmin), 409, model.ConsoleLastAdmin, "null"},
		{"领域问题优先于普通冲突", errors.Join(repository.ErrConflict, p), 422, p.Message, "problems"},
		{"仓储状态问题仍为422", &repository.ProblemsError{Kind: repository.ErrConflict, Problems: []model.Problem{state}}, 422, state.Message, "problems"},
		{"空引用", &repository.ReferencedError{}, 409, model.ConsoleReferenced, "references"},
		{"空问题", &repository.ProblemsError{Kind: repository.ErrInvalid}, 422, model.ConsoleRejected, "problems"},
		{"空批量", &repository.BatchError{}, 422, model.ConsoleRejected, "batch"},
		{"批量共用问题优先", &repository.BatchError{Result: admission.BatchResult{Common: []model.Problem{p}, ByHost: map[string][]model.Problem{"A": {state}}}}, 422, p.Message, "batch"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, w := testContext(t)
			RespondError(c, fmt.Errorf("包装: %w", tc.err))
			got := decodeEnvelope(t, w)
			if w.Code != tc.status || got["message"] != tc.message {
				t.Fatalf("status=%d response=%v", w.Code, got)
			}
			if tc.detail == "null" {
				if got["data"] != nil {
					t.Fatal(got)
				}
				return
			}
			data := got["data"].(map[string]any)
			if tc.detail == "batch" {
				if _, ok := data["common"].([]any); !ok {
					t.Fatal("common必须是数组", got)
				}
				if _, ok := data["byHost"].(map[string]any); !ok {
					t.Fatal("byHost必须是对象", got)
				}
				return
			}
			if _, ok := data[tc.detail].([]any); !ok {
				t.Fatal("详情必须是数组", got)
			}
		})
	}
}

type failureLogHandler struct{ original error }

func (h *failureLogHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *failureLogHandler) Handle(_ context.Context, record slog.Record) error {
	record.Attrs(func(attr slog.Attr) bool {
		if attr.Key == "error" {
			h.original, _ = attr.Value.Any().(error)
		}
		return true
	})
	return nil
}
func (h *failureLogHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *failureLogHandler) WithGroup(string) slog.Handler      { return h }

func Test未知错误响应不泄露日志保留原错误(t *testing.T) {
	previous := slog.Default()
	handler := &failureLogHandler{}
	slog.SetDefault(slog.New(handler))
	t.Cleanup(func() { slog.SetDefault(previous) })
	original := fmt.Errorf("包装: %w", errors.New("private backend credential"))
	c, w := testContext(t)
	RespondError(c, original)
	got := decodeEnvelope(t, w)
	if w.Code != 500 || got["message"] != model.ConsoleInternal || got["data"] != nil || strings.Contains(w.Body.String(), "private backend credential") {
		t.Fatal(w.Code, w.Body.String())
	}
	if handler.original != original {
		t.Fatal("日志没有收到原错误", handler.original)
	}
}

func Test状态码按原因类别映射(t *testing.T) {
	for _, tt := range []struct {
		code model.Code
		args []any
		want int
	}{
		{"sn_invalid", []any{"为空"}, 422},
		{"hook_no_impl", nil, 422},
		{"series_not_ticked", []any{"rocky"}, 409},
		{"template_not_approved", []any{"草稿"}, 409},
	} {
		t.Run(string(tt.code), func(t *testing.T) {
			if got := StatusOfProblems([]model.Problem{model.NewProblem(tt.code, model.Subject{}, tt.args...)}); got != tt.want {
				t.Fatalf("got %d want %d", got, tt.want)
			}
		})
	}
}

func Test类别未登记按422(t *testing.T) {
	if StatusOfProblems([]model.Problem{{Code: "not_registered"}}) != 422 {
		t.Fatal("unknown category")
	}
}

func Test单条领域错误保留原因类别(t *testing.T) {
	p := model.NewProblem("template_not_approved", model.Subject{Kind: model.KindTemplate}, "draft")
	for _, err := range []error{p, &p} {
		c, w := testContext(t)
		RespondError(c, err)
		if w.Code != 409 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
}

func Test仓储错误映射到状态码(t *testing.T) {
	ps := []model.Problem{model.NewProblem("sn_invalid", model.Subject{Kind: "host"}, "为空")}
	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{"missing", repository.ErrNotFound, 404}, {"conflict", repository.ErrConflict, 409},
		{"duplicate", &repository.DuplicateError{Field: "sn", Existing: "SN-1"}, 409},
		{"immutable", repository.ErrImmutable, 409}, {"referenced", repository.ErrReferenced, 409},
		{"invalid", repository.ErrInvalid, 400}, {"audit", repository.ErrAuditRequired, 500},
		{"problems", &repository.ProblemsError{Kind: repository.ErrInvalid, Problems: ps}, 422},
		{"rejected", &repository.ProblemsError{Kind: repository.ErrRejected, Problems: ps}, 422},
		{"batch", &repository.BatchError{Result: admission.BatchResult{ByHost: map[string][]model.Problem{"SN-1": ps}}}, 422},
		{"unknown", errors.New("backend-secret"), 500},
	} {
		t.Run(tt.name, func(t *testing.T) {
			c, w := testContext(t)
			RespondError(c, tt.err)
			if w.Code != tt.want || decodeEnvelope(t, w)["code"] != float64(tt.want) || strings.Contains(w.Body.String(), "backend-secret") {
				t.Fatalf("got %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func Test失败响应的data装结构化详情(t *testing.T) {
	p := model.NewProblem("sn_invalid", model.Subject{Kind: "host", ID: "SN-1", Path: "arch", Line: 2, Col: 3}, "为空")
	c, w := testContext(t)
	RespondError(c, &repository.ProblemsError{Kind: repository.ErrInvalid, Problems: []model.Problem{p}})
	got := decodeEnvelope(t, w)
	problem := got["data"].(map[string]any)["problems"].([]any)[0].(map[string]any)
	subject := problem["subject"].(map[string]any)
	if problem["code"] != "sn_invalid" || problem["message"] != p.Message || problem["next"] != p.Next || got["message"] != p.Message || subject["kind"] != "host" || subject["id"] != "SN-1" || subject["path"] != "arch" || subject["line"] != float64(2) || subject["col"] != float64(3) {
		t.Fatal(got)
	}
}

func Test批量被拒时按SN给出每台的问题(t *testing.T) {
	a := model.Problem{Code: "unknown", Message: "first host"}
	z := model.Problem{Code: "unknown", Message: "last host"}
	c, w := testContext(t)
	RespondError(c, &repository.BatchError{Result: admission.BatchResult{ByHost: map[string][]model.Problem{"Z": {z}, "A": {a}}}})
	got := decodeEnvelope(t, w)
	data := got["data"].(map[string]any)
	if got["message"] != "first host" || len(data["common"].([]any)) != 0 || len(data["byHost"].(map[string]any)) != 2 {
		t.Fatal(got)
	}
}

func Test没有详情的错误data是null(t *testing.T) {
	c, w := testContext(t)
	RespondError(c, repository.ErrNotFound)
	if decodeEnvelope(t, w)["data"] != nil {
		t.Fatal(w.Body.String())
	}
}

func TestJudge没有问题时继续有问题时停止(t *testing.T) {
	c, w := testContext(t)
	if !Judge(c, nil) || w.Body.Len() != 0 {
		t.Fatal("empty decision wrote response")
	}
	p := model.NewProblem("series_not_ticked", model.Subject{}, "rocky")
	if Judge(c, []model.Problem{p}) || !c.IsAborted() || w.Code != 409 {
		t.Fatal(w.Body.String())
	}
}

func TestBind拒绝坏JSON与多个JSON值(t *testing.T) {
	for _, body := range []string{"", "{", `{"name":"x"}{"name":"y"}`} {
		c, w := testContext(t)
		c.Request.Body = io.NopCloser(strings.NewReader(body))
		var value struct {
			Name string `json:"name"`
		}
		if Bind(c, &value) || w.Code != 400 || !c.IsAborted() {
			t.Fatalf("accepted %q", body)
		}
	}
}

func TestBind解析合法JSON并检查必填字段(t *testing.T) {
	for _, body := range []string{`{"name":"node"}`, `{}`} {
		c, w := testContext(t)
		c.Request.Body = io.NopCloser(strings.NewReader(body))
		var value struct {
			Name string `json:"name" binding:"required"`
		}
		ok := Bind(c, &value)
		if body == `{}` {
			if ok || w.Code != 400 {
				t.Fatal("missing required field accepted")
			}
		} else if !ok || value.Name != "node" || w.Body.Len() != 0 {
			t.Fatal("valid request rejected")
		}
	}
}

func TestGet与Write失败时停止成功时返回值(t *testing.T) {
	c, w := testContext(t)
	if value, ok := Get(c, func() (string, error) { return "value", nil }); !ok || value != "value" || w.Body.Len() != 0 {
		t.Fatal("get success")
	}
	if _, ok := Write(c, func() (string, error) { return "", repository.ErrNotFound }); ok || w.Code != 404 || !c.IsAborted() {
		t.Fatal("write error")
	}
	c, w = testContext(t)
	if value, ok := Write(c, func() (int, error) { return 7, nil }); !ok || value != 7 || w.Body.Len() != 0 {
		t.Fatal("write success")
	}
	if _, ok := Get(c, func() (int, error) { return 0, repository.ErrConflict }); ok || w.Code != 409 {
		t.Fatal("get error")
	}
}
