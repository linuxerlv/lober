package console

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"io"
	"log/slog"
	"maps"
	"slices"
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

func jsonProblems(ps []model.Problem) []problemJSON {
	out := make([]problemJSON, len(ps))
	for i, p := range ps {
		out[i] = problemJSON{Code: p.Code, Subject: subjectJSON{p.Subject.Kind, p.Subject.ID, p.Subject.Path, p.Subject.Line, p.Subject.Col}, Message: p.Message, Next: p.Next}
	}
	return out
}
func RespondError(c *gin.Context, err error) {
	var batch *store.BatchError
	if errors.As(err, &batch) {
		byHost := make(map[string][]problemJSON, len(batch.Result.ByHost))
		for sn, ps := range batch.Result.ByHost {
			byHost[sn] = jsonProblems(ps)
		}
		Fail(c, 422, batchDetail{jsonProblems(batch.Result.Common), byHost}, firstMessage(batch.Result.Common, batch.Result.ByHost))
		return
	}
	var pe *store.ProblemsError
	if errors.As(err, &pe) {
		Fail(c, 422, problemList{jsonProblems(pe.Problems)}, firstMessage(pe.Problems, nil))
		return
	}
	status, message := 500, model.ConsoleInternal
	switch {
	case errors.Is(err, store.ErrNotFound):
		status, message = 404, model.ConsoleNotFound
	case errors.Is(err, store.ErrConflict):
		status, message = 409, model.ConsoleConflict
	case errors.Is(err, store.ErrDuplicate):
		status, message = 409, model.ConsoleDuplicate
	case errors.Is(err, store.ErrImmutable):
		status, message = 409, model.ConsoleImmutable
	case errors.Is(err, store.ErrReferenced):
		status, message = 409, model.ConsoleReferenced
	case errors.Is(err, store.ErrInvalid):
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
	d := json.NewDecoder(c.Request.Body)
	if err := d.Decode(v); err != nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return false
	}
	var extra any
	if err := d.Decode(&extra); !errors.Is(err, io.EOF) {
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
