package console

import (
	"errors"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
	"log/slog"
	"runtime/debug"
	"slices"
	"strings"
	"sync"
)

type Clock func() int64
type Deps struct {
	Hosts            store.Hosts
	ISOs             store.ISOs
	Hooks            store.Hooks
	Templates        store.Templates
	Tasks            store.Tasks
	Events           store.Events
	Audit            store.AuditLog
	Users            store.Users
	ISOSource        ISOSource
	Sources          ISOSourceState
	ISODeletion      ISODeletion
	BootConfig       BootConfig
	PreviewEndpoints PreviewEndpoints
	TaskCtl          TaskControl
	Notifier         Notifier
	Clock            Clock
	SessionTTL       int64
	Logger           *slog.Logger
}
type Server struct {
	Engine *gin.Engine
	server *server
	routes []RouteInfo
}
type server struct {
	deps              Deps
	sessions          *sessions
	hub               Notifier
	logger            *slog.Logger
	index             map[string]route
	routes            []route
	owner             *Server
	templateSelection sync.RWMutex
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
	if d.SessionTTL == 0 {
		d.SessionTTL = 12 * 3600
	}
	s := &server{deps: d, hub: d.Notifier, logger: d.Logger, index: make(map[string]route)}
	s.sessions = newSessions(d.SessionTTL, d.Clock)
	out := &Server{Engine: gin.New(), server: s}
	s.owner = out
	out.Engine.Use(s.recoverPanic(), s.requestLog())
	out.Engine.NoRoute(func(c *gin.Context) { Fail(c, 404, nil, model.ConsoleNotFound) })
	s.installRoutes(out, s.allRoutes())
	return out
}
func (s *server) clock() int64 { return s.deps.Clock() }

const ctxUser = "console.user"
const ctxStream = "console.stream"

func userOf(c *gin.Context) *store.User { v, _ := c.Get(ctxUser); u, _ := v.(*store.User); return u }
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
		token := bearerToken(c)
		username, ok := s.sessions.lookup(token)
		if !ok {
			Fail(c, 401, nil, model.ConsoleUnauthorized)
			return
		}
		if s.deps.Users == nil {
			Fail(c, 500, nil, model.ConsoleInternal)
			return
		}
		u, err := s.deps.Users.GetUser(c.Request.Context(), username)
		if errors.Is(err, store.ErrNotFound) || err == nil && u.Disabled {
			s.sessions.drop(token)
			Fail(c, 401, nil, model.ConsoleUnauthorized)
			return
		}
		if err != nil {
			RespondError(c, err)
			return
		}
		if !u.Role.Valid() {
			Fail(c, 401, nil, model.ConsoleUnauthorized)
			return
		}
		c.Set(ctxUser, &u)
		c.Next()
	}
}
func (s *server) authorize(perm string) gin.HandlerFunc {
	return func(c *gin.Context) {
		u := userOf(c)
		if u != nil && u.MustChangePassword {
			path := c.FullPath()
			allowed := c.Request.Method == "GET" && path == "/console/v1/user/info" || c.Request.Method == "POST" && path == "/console/v1/auth/logout" || c.Request.Method == "PUT" && path == "/console/v1/users/:username" && c.Param("username") == u.Username
			if !allowed {
				Fail(c, 403, nil, model.ConsolePasswordChangeRequired)
				return
			}
		}
		if u == nil || perm != PermAnyUser && !slices.Contains(PermsOf(u.Role), perm) {
			Fail(c, 403, nil, model.ConsoleForbidden)
			return
		}
		c.Next()
	}
}
func (s *server) auditFrom(c *gin.Context, detail string) store.Audit {
	r, ok := s.index[c.Request.Method+" "+c.FullPath()]
	u := userOf(c)
	if !ok || r.audit == "" || u == nil {
		return store.Audit{}
	}
	return store.Audit{Actor: u.Username, Action: r.audit, Detail: detail, At: s.clock()}
}
