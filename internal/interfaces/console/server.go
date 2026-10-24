package console

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/audit"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/hosts"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"log/slog"
	"runtime/debug"
	"strings"
)

type Clock = ports.Clock
type Deps struct {
	HostRegistry      hosts.Registry
	HookAuthoring     authoring.Hooks
	TemplateAuthoring authoring.Templates
	AuthoringEditor   authoring.Editor
	MediaRegistry     media.Registry
	Tasks             repository.Tasks
	Wizard            tasks.Wizard
	TaskReader        tasks.Reader
	TaskController    tasks.Controller
	Events            repository.Events
	AuditReader       audit.Reader
	Identity          identity.Registry
	FrozenPreview     ports.FrozenPreview
	Notifier          Notifier
	Clock             Clock
	Logger            *slog.Logger
}
type Server struct {
	Engine *gin.Engine
	server *server
	routes []RouteInfo
}
type server struct {
	deps   Deps
	hub    Notifier
	logger *slog.Logger
	index  map[string]route
	routes []route
	owner  *Server
}

func New(d Deps) *Server {
	if d.Clock == nil {
		panic("console requires an injected clock")
	}
	if d.Notifier == nil {
		d.Notifier = NewHub()
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	s := &server{deps: d, hub: d.Notifier, logger: d.Logger, index: make(map[string]route)}
	out := &Server{Engine: gin.New(), server: s}
	s.owner = out
	out.Engine.Use(s.recoverPanic(), s.requestLog())
	out.Engine.NoRoute(func(c *gin.Context) { Fail(c, 404, nil, model.ConsoleNotFound) })
	s.installRoutes(out, s.allRoutes())
	return out
}
func (s *server) clock() int64 { return s.deps.Clock() }

const ctxPrincipal = "console.principal"
const ctxActor = "console.actor"
const ctxStream = "console.stream"

func principalOf(c *gin.Context) identity.Principal {
	v, _ := c.Get(ctxPrincipal)
	p, _ := v.(identity.Principal)
	return p
}
func actorOf(c *gin.Context) ports.Actor {
	if v, ok := c.Get(ctxPrincipal); ok {
		p, _ := v.(identity.Principal)
		return p.Actor()
	}
	v, _ := c.Get(ctxActor)
	a, _ := v.(ports.Actor)
	return a
}

func (s *server) recoverPanic() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if p := recover(); p != nil {
				s.logger.ErrorContext(c.Request.Context(), "handler panic", "path", c.FullPath(), "type", fmt.Sprintf("%T", p), "stack", string(debug.Stack()))
				if !c.Writer.Written() {
					Fail(c, 500, nil, model.ConsoleInternal)
				} else {
					c.Abort()
				}
			}
		}()
		c.Next()
	}
}
func (s *server) requestLog() gin.HandlerFunc {
	return func(c *gin.Context) {
		start := s.clock()
		c.Next()
		s.logger.InfoContext(c.Request.Context(), "request", "method", c.Request.Method, "path", c.FullPath(), "status", c.Writer.Status(), "seconds", max(int64(0), s.clock()-start))
	}
}
func bearerToken(c *gin.Context) string {
	if header := c.GetHeader("Authorization"); header != "" {
		parts := strings.Fields(header)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
			return ""
		}
		return parts[1]
	}
	allowed, _ := c.Get(ctxStream)
	if c.Request.Method == "GET" && allowed == true {
		return c.Query("access_token")
	}
	return ""
}
func (s *server) authenticate() gin.HandlerFunc {
	return func(c *gin.Context) {
		if rt, ok := s.index[c.Request.Method+" "+c.FullPath()]; ok {
			c.Set(ctxStream, rt.stream)
		}
		if s.deps.Identity == nil {
			Fail(c, 500, nil, model.ConsoleInternal)
			return
		}
		p, err := s.deps.Identity.Authenticate(c.Request.Context(), bearerToken(c))
		if err != nil {
			RespondError(c, err)
			return
		}
		c.Set(ctxPrincipal, p)
		c.Next()
	}
}
func (s *server) authorize(rt route) gin.HandlerFunc {
	return func(c *gin.Context) {
		err := s.deps.Identity.Authorize(principalOf(c), identity.AccessRequest{Permission: rt.perm, Operation: rt.identityOperation, TargetUsername: c.Param("username")})
		if err != nil {
			RespondError(c, err)
			return
		}
		c.Next()
	}
}
