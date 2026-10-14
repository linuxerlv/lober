package console

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

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

func Test仓储错误映射到状态码(t *testing.T) {
	ps := []model.Problem{model.NewProblem("sn_invalid", model.Subject{Kind: "host"}, "为空")}
	for _, tt := range []struct {
		name string
		err  error
		want int
	}{
		{"missing", store.ErrNotFound, 404}, {"conflict", store.ErrConflict, 409},
		{"duplicate", &store.DuplicateError{Field: "sn", Existing: "SN-1"}, 409},
		{"immutable", store.ErrImmutable, 409}, {"referenced", store.ErrReferenced, 409},
		{"invalid", store.ErrInvalid, 400}, {"audit", store.ErrAuditRequired, 500},
		{"problems", &store.ProblemsError{Kind: store.ErrInvalid, Problems: ps}, 422},
		{"rejected", &store.ProblemsError{Kind: store.ErrRejected, Problems: ps}, 422},
		{"batch", &store.BatchError{Result: admission.BatchResult{ByHost: map[string][]model.Problem{"SN-1": ps}}}, 422},
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
	RespondError(c, &store.ProblemsError{Kind: store.ErrInvalid, Problems: []model.Problem{p}})
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
	RespondError(c, &store.BatchError{Result: admission.BatchResult{ByHost: map[string][]model.Problem{"Z": {z}, "A": {a}}}})
	got := decodeEnvelope(t, w)
	data := got["data"].(map[string]any)
	if got["message"] != "first host" || len(data["common"].([]any)) != 0 || len(data["byHost"].(map[string]any)) != 2 {
		t.Fatal(got)
	}
}

func Test没有详情的错误data是null(t *testing.T) {
	c, w := testContext(t)
	RespondError(c, store.ErrNotFound)
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
	if _, ok := Write(c, func() (string, error) { return "", store.ErrNotFound }); ok || w.Code != 404 || !c.IsAborted() {
		t.Fatal("write error")
	}
	c, w = testContext(t)
	if value, ok := Write(c, func() (int, error) { return 7, nil }); !ok || value != 7 || w.Body.Len() != 0 {
		t.Fatal("write success")
	}
	if _, ok := Get(c, func() (int, error) { return 0, store.ErrConflict }); ok || w.Code != 409 {
		t.Fatal("get error")
	}
}
