package console

import (
	"encoding/json"
	"errors"
	"github.com/gin-gonic/gin"
	"github.com/gin-gonic/gin/binding"
	"github.com/lober/lober/internal/application/audit"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/model"
	"io"
	"log/slog"
	"maps"
	"net/http"
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
	var rangeError *media.RangeError
	if errors.As(err, &rangeError) {
		Fail(c, 400, nil, model.ConsoleContentRange)
		return
	}

	var csvFile *hosts.CSVFileError
	if errors.As(err, &csvFile) {
		Fail(c, 400, nil, csvFile.Message)
		return
	}
	var csvRejected *hosts.ImportRejectedError
	if errors.As(err, &csvRejected) {
		Fail(c, 422, csvPlanOf(csvRejected.Plan), csvRejected.Message)
		return
	}

	switch {
	case errors.Is(err, tasks.ErrControlUnavailable):
		Fail(c, 422, nil, model.ConsoleTaskControlUnavailable)
		return
	case errors.Is(err, identity.ErrLoginFailed):
		Fail(c, 401, nil, model.ConsoleLoginFailed)
		return
	case errors.Is(err, identity.ErrBadRequest), errors.Is(err, audit.ErrBadRequest), errors.Is(err, tasks.ErrBadRequest):
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return
	case errors.Is(err, identity.ErrSelfDisable):
		Fail(c, 409, nil, model.ConsoleSelfDisable)
		return
	case errors.Is(err, identity.ErrInitialPasswordUnchanged):
		Fail(c, 409, nil, model.ConsolePasswordChangeRequired)
		return
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
	var previewProblems *authoring.ProblemsError
	if errors.As(err, &previewProblems) {
		Judge(c, previewProblems.Problems)
		return
	}
	var applicationProblems *tasks.ProblemsError
	if errors.As(err, &applicationProblems) {
		Judge(c, applicationProblems.Problems)
		return
	}
	status, message := 500, model.ConsoleInternal
	if failure, ok := ports.DescribeFailure(err); ok {
		switch failure.Kind {
		case ports.TemplateNotCurrent:
			status, message = 409, model.ConsoleTemplateCurrent
		case ports.LastAdmin:
			status, message = 409, model.ConsoleLastAdmin
		case ports.ReferencedDetail:
			refs := make([]subjectJSON, len(failure.References))
			for i, s := range failure.References {
				refs[i] = subjectJSON{s.Kind, s.ID, s.Path, s.Line, s.Col}
			}
			Fail(c, 409, referenceDetail{refs}, model.ConsoleReferenced)
			return
		case ports.BatchRejected:
			byHost := make(map[string][]problemJSON, len(failure.ByHost))
			for sn, ps := range failure.ByHost {
				byHost[sn] = jsonProblems(ps)
			}
			Fail(c, 422, batchDetail{jsonProblems(failure.Common), byHost}, firstMessage(failure.Common, failure.ByHost))
			return
		case ports.StoredProblems:
			Fail(c, 422, problemList{jsonProblems(failure.Problems)}, firstMessage(failure.Problems, nil))
			return
		case ports.DomainProblems:
			Judge(c, failure.Problems)
			return
		case ports.NotFound:
			status, message = 404, model.ConsoleNotFound
		case ports.Conflict:
			status, message = 409, model.ConsoleConflict
		case ports.Duplicate:
			status, message = 409, model.ConsoleDuplicate
		case ports.Immutable:
			status, message = 409, model.ConsoleImmutable
		case ports.Referenced:
			status, message = 409, model.ConsoleReferenced
		case ports.Invalid:
			status, message = 400, model.ConsoleBadRequest
		}
	}
	if status == 500 {
		slog.ErrorContext(c.Request.Context(), "console request failed", "error", err)
	}
	Fail(c, status, nil, message)
}
func StatusOfProblems(ps []model.Problem) int {
	if ports.ProblemClass(ps) == ports.ProblemConflict {
		return 409
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

const (
	loginBodyLimit = 16 << 10
	jsonBodyLimit  = 4 << 20
	hostCSVLimit   = 16 << 20
)

func readRequestBody(c *gin.Context, limit int64) ([]byte, bool) {
	if c.Request.Body == nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return nil, false
	}
	if c.Request.ContentLength > limit {
		Fail(c, http.StatusRequestEntityTooLarge, nil, "请求内容超过大小限制")
		return nil, false
	}
	body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, limit))
	var oversized *http.MaxBytesError
	if errors.As(err, &oversized) {
		Fail(c, http.StatusRequestEntityTooLarge, nil, "请求内容超过大小限制")
		return nil, false
	}
	if err != nil {
		Fail(c, 400, nil, model.ConsoleBadRequest)
		return nil, false
	}
	return body, true
}

// Bind 读取一个完整的 JSON 值，并校验必填字段。
func Bind(c *gin.Context, v any) bool { return bindJSON(c, v, jsonBodyLimit) }

func bindJSON(c *gin.Context, v any, limit int64) bool {
	body, ok := readRequestBody(c, limit)
	if !ok {
		return false
	}
	if !utf8.Valid(body) || json.Unmarshal(body, v) != nil {
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
