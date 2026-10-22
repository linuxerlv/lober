package console

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/activation"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type rejectRequest struct {
	Reason string `json:"reason" binding:"required"`
}
type validateResponse struct {
	OK       bool          `json:"ok"`
	Problems []problemJSON `json:"problems"`
}

func (s *server) templateProblems(ctx context.Context, t model.Template) ([]model.Problem, error) {
	hooks, err := s.templateHooks(ctx, []model.Template{t})
	if err != nil {
		return nil, err
	}
	return activation.Template(t, hooks), nil
}

func (s *server) hookLifecycleRoutes() []route {
	const base = "/console/v1/hooks/:id/versions/:version"
	return []route{
		{method: "POST", path: base + "/submit", perm: PermHookWrite, audit: "hook.submit", tag: "hooks", summary: "Submit hook for review", query: revisionQuery{}, resp: hookResponse{}, handle: s.submitHook},
		{method: "POST", path: base + "/approve", perm: PermHookApprove, audit: "hook.approve", tag: "hooks", summary: "Approve hook after transactional validation", query: revisionQuery{}, resp: hookResponse{}, handle: s.approveHook},
		{method: "POST", path: base + "/reject", perm: PermHookApprove, audit: "hook.reject", tag: "hooks", summary: "Reject hook with a reason", query: revisionQuery{}, req: rejectRequest{}, resp: hookResponse{}, handle: s.rejectHook},
		{method: "POST", path: base + "/deprecate", perm: PermHookApprove, audit: "hook.deprecate", tag: "hooks", summary: "Deprecate approved hook", query: revisionQuery{}, resp: hookResponse{}, handle: s.deprecateHook},
		{method: "POST", path: base + "/validate", perm: PermHookWrite, tag: "hooks", summary: "Read hook activation diagnostics", resp: validateResponse{}, handle: s.validateHook},
	}
}
func (s *server) templateLifecycleRoutes() []route {
	const base = "/console/v1/templates/:id/versions/:version"
	return []route{
		{method: "POST", path: base + "/submit", perm: PermTemplateWrite, audit: "template.submit", tag: "templates", summary: "Submit template for review", query: revisionQuery{}, resp: templateResponse{}, handle: s.submitTemplate},
		{method: "POST", path: base + "/approve", perm: PermTemplateApprove, audit: "template.approve", tag: "templates", summary: "Approve template and pinned references transactionally", query: revisionQuery{}, resp: templateResponse{}, handle: s.approveTemplate},
		{method: "POST", path: base + "/reject", perm: PermTemplateApprove, audit: "template.reject", tag: "templates", summary: "Reject template with a reason", query: revisionQuery{}, req: rejectRequest{}, resp: templateResponse{}, handle: s.rejectTemplate},
		{method: "POST", path: base + "/deprecate", perm: PermTemplateApprove, audit: "template.deprecate", tag: "templates", summary: "Deprecate approved template", query: revisionQuery{}, resp: templateResponse{}, handle: s.deprecateTemplate},
		{method: "POST", path: base + "/validate", perm: PermTemplateWrite, tag: "templates", summary: "Read template activation diagnostics with pinned hooks", resp: validateResponse{}, handle: s.validateTemplate},
	}
}

type hookStateStep func(context.Context, ports.Actor, authoring.HookRevision) (authoring.HookView, error)
type templateStateStep func(context.Context, model.TemplateKey, int64, repository.Audit) error

func (s *server) stepHook(c *gin.Context, step hookStateStep) {
	h, ok := Write(c, func() (authoring.HookView, error) { return step(c.Request.Context(), actorOf(c), hookRevisionOf(c)) })
	if ok {
		Ok(c, toHookResponse(h))
	}
}
func (s *server) stepTemplate(c *gin.Context, step templateStateStep, detail string) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	rev, ok := queryRev(c)
	if !ok {
		return
	}
	if err := step(c.Request.Context(), k, rev, s.auditFrom(c, detail)); err != nil {
		RespondError(c, err)
		return
	}
	s.reloadTemplate(c, k)
}
func rejectReason(c *gin.Context) (string, bool) {
	var req rejectRequest
	if !Bind(c, &req) {
		return "", false
	}
	if strings.TrimSpace(req.Reason) == "" || !model.StorableText(req.Reason) {
		Fail(c, 400, nil, model.ConsoleRejectReason)
		return "", false
	}
	return req.Reason, true
}
func (s *server) submitHook(c *gin.Context)  { s.stepHook(c, s.deps.HookAuthoring.Submit) }
func (s *server) approveHook(c *gin.Context) { s.stepHook(c, s.deps.HookAuthoring.Approve) }
func (s *server) rejectHook(c *gin.Context) {
	var req rejectRequest
	if !Bind(c, &req) {
		return
	}
	h, ok := Write(c, func() (authoring.HookView, error) {
		return s.deps.HookAuthoring.Reject(c.Request.Context(), actorOf(c), authoring.HookRejection{HookRevision: hookRevisionOf(c), Reason: req.Reason})
	})
	if ok {
		Ok(c, toHookResponse(h))
	}
}
func (s *server) deprecateHook(c *gin.Context) { s.stepHook(c, s.deps.HookAuthoring.Deprecate) }
func (s *server) validateHook(c *gin.Context) {
	ps, ok := Get(c, func() ([]model.Problem, error) {
		return s.deps.HookAuthoring.Validate(c.Request.Context(), actorOf(c), hookKeyOf(c))
	})
	if ok {
		Ok(c, validateResponse{OK: len(ps) == 0, Problems: jsonProblems(ps)})
	}
}
func (s *server) submitTemplate(c *gin.Context) {
	s.stepTemplate(c, s.deps.Templates.SubmitTemplate, "")
}
func (s *server) approveTemplate(c *gin.Context) {
	release := s.deps.SelectionGuard.WriteLock()
	defer release()
	s.stepTemplate(c, func(ctx context.Context, k model.TemplateKey, rev int64, a repository.Audit) error {
		return s.deps.Templates.ApproveTemplate(ctx, k, rev, userOf(c).Username, a)
	}, "")
}
func (s *server) rejectTemplate(c *gin.Context) {
	reason, ok := rejectReason(c)
	if ok {
		s.stepTemplate(c, s.deps.Templates.RejectTemplate, reason)
	}
}
func (s *server) deprecateTemplate(c *gin.Context) {
	release := s.deps.SelectionGuard.WriteLock()
	defer release()
	s.stepTemplate(c, s.deps.Templates.DeprecateTemplate, "")
}
func (s *server) validateTemplate(c *gin.Context) {
	k, ok := templateKeyOf(c)
	if !ok {
		return
	}
	t, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), k) })
	if !ok {
		return
	}
	ps, ok := Get(c, func() ([]model.Problem, error) { return s.templateProblems(c.Request.Context(), t) })
	if ok {
		Ok(c, validateResponse{OK: len(ps) == 0, Problems: jsonProblems(ps)})
	}
}
func (s *server) reloadTemplate(c *gin.Context, k model.TemplateKey) {
	t, ok := Get(c, func() (model.Template, error) { return s.deps.Templates.GetTemplate(c.Request.Context(), k) })
	if ok {
		s.respondTemplate(c, t)
	}
}
