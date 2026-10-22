package console

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
	"log/slog"
	"maps"
	"slices"
	"unicode/utf8"
)

type subjectJSON struct {
	Kind string `json:"kind"`
	ID   string `json:"id"`
	Path string `json:"path"`
	Line int    `json:"line"`
	Col  int    `json:"col"`
}
type problemJSON struct {
	Code    model.Code  `json:"code"`
	Subject subjectJSON `json:"subject"`
	Message string      `json:"message"`
	Next    string      `json:"next"`
}
type problemList struct {
	Problems []problemJSON `json:"problems"`
}
type batchDetail struct {
	Common []problemJSON            `json:"common"`
	ByHost map[string][]problemJSON `json:"byHost"`
}
type referenceDetail struct {
	References []subjectJSON `json:"references"`
}

func jsonProblems(ps []model.Problem) []problemJSON {
	out := make([]problemJSON, len(ps))
	for i, p := range ps {
		out[i] = problemJSON{Code: p.Code, Subject: subjectJSON{p.Subject.Kind, p.Subject.ID, p.Subject.Path, p.Subject.Line, p.Subject.Col}, Message: p.Message, Next: p.Next}
	}
	return out
}
func RespondError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, authoring.ErrRejectReason):
		Fail(c, 400, nil, model.ConsoleRejectReason)
		return
	case errors.Is(err, ports.ErrUnauthenticated):
		Fail(c, 401, nil, model.ConsoleUnauthorized)
		return
	case errors.Is(err, ports.ErrPasswordChangeRequired):
		Fail(c, 403, nil, model.ConsolePasswordChangeRequired)
		return
	case errors.Is(err, ports.ErrForbidden):
		Fail(c, 403, nil, model.ConsoleForbidden)
		return
	}
	var applicationProblems *tasks.ProblemsError
	if errors.As(err, &applicationProblems) {
		Judge(c, applicationProblems.Problems)
		return
	}
	if errors.Is(err, repository.ErrTemplateNotCurrent) {
		Fail(c, 409, nil, model.ConsoleTemplateCurrent)
		return
	}
	if errors.Is(err, repository.ErrLastAdmin) {
		Fail(c, 409, nil, model.ConsoleLastAdmin)
		return
	}
	var referenced *repository.ReferencedError
	if errors.As(err, &referenced) {
		refs := make([]subjectJSON, len(referenced.References))
		for i, s := range referenced.References {
			refs[i] = subjectJSON{s.Kind, s.ID, s.Path, s.Line, s.Col}
		}
		Fail(c, 409, referenceDetail{refs}, model.ConsoleReferenced)
		return
	}
	var batch *repository.BatchError
	if errors.As(err, &batch) {
		byHost := make(map[string][]problemJSON, len(batch.Result.ByHost))
		for sn, ps := range batch.Result.ByHost {
			byHost[sn] = jsonProblems(ps)
		}
		Fail(c, 422, batchDetail{jsonProblems(batch.Result.Common), byHost}, firstMessage(batch.Result.Common, batch.Result.ByHost))
		return
	}
	var pe *repository.ProblemsError
	if errors.As(err, &pe) {
		Fail(c, 422, problemList{jsonProblems(pe.Problems)}, firstMessage(pe.Problems, nil))
		return
	}
	status, message := 500, model.ConsoleInternal
	var p model.Problem
	if errors.As(err, &p) {
		Judge(c, []model.Problem{p})
		return
	}
	var pp *model.Problem
	if errors.As(err, &pp) && pp != nil {
		Judge(c, []model.Problem{*pp})
		return
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		status, message = 404, model.ConsoleNotFound
	case errors.Is(err, repository.ErrConflict):
		status, message = 409, model.ConsoleConflict
	case errors.Is(err, repository.ErrDuplicate):
		status, message = 409, model.ConsoleDuplicate
	case errors.Is(err, repository.ErrImmutable):
		status, message = 409, model.ConsoleImmutable
	case errors.Is(err, repository.ErrReferenced):
		status, message = 409, model.ConsoleReferenced
	case errors.Is(err, repository.ErrInvalid):
		status, message = 400, model.ConsoleBadRequest
	}
	if status == 500 {
		slog.ErrorContext(c.Request.Context(), "console request failed", "error", err)
	}
	Fail(c, status, nil, message)
}
func StatusOfProblems(ps []model.Problem) int {
	if len(ps) > 0 {
		switch ps[0].Code.Category() {
		case model.CategoryIncompatible, model.CategoryState:
			return 409
		}
	}
	return 422
}
func Judge(c *gin.Context, ps []model.Problem) bool {
	if len(ps) == 0 {
		return true
	}
	Fail(c, StatusOfProblems(ps), problemList{jsonProblems(ps)}, ps[0].Message)
	return false
}

// Bind accepts exactly one JSON value and checks declared required fields.
func Bind(c *gin.Context, v any) bool {
	if c.Request.Body == nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false
	}
	d := json.NewDecoder(c.Request.Body)
	var raw json.RawMessage
	if err := d.Decode(&raw); err != nil || !utf8.Valid(raw) {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false
	}
	if err := json.Unmarshal(raw, v); err != nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false
	}
	if err := binding.Validator.ValidateStruct(v); err != nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false
	}
	return true
}
func Get[T any](c *gin.Context, load func() (T, error)) (T, bool) {
	v, err := load()
	if err != nil {
		RespondError(c, err)
		return v, false
	}
	return v, true
}
func Write[T any](c *gin.Context, do func() (T, error)) (T, bool) { return Get(c, do) }
func firstMessage(common []model.Problem, byHost map[string][]model.Problem) string {
	if len(common) > 0 {
		return common[0].Message
	}
	for _, sn := range slices.Sorted(maps.Keys(byHost)) {
		if ps := byHost[sn]; len(ps) > 0 {
			return ps[0].Message
		}
	}
	return model.ConsoleRejected
}
