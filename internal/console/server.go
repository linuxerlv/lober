// 本文件定义 Server 类型：控制台的依赖结构与路由注册。对应实现计划任务 1，
// 依据 docs/contracts/http-console.md、SPEC §8.2。
//
// Server 持有全部接缝（M1 契约窄接口 + M3/M4/M7 通过接口注入的实现），
// 按资源分文件写 handler 方法。不 import internal/statemachine、
// internal/render、internal/provider 的实现包——只依赖
// internal/core/contract 的接口类型，测试用自己的桩（stub_test.go）。
package console

import (
	"net/http"
	"time"

	"github.com/lober/lober/internal/apierr"
	"github.com/lober/lober/internal/core/contract"
)

// Deps 是装配点（cmd/lober，M9）注入给 Server 的全部依赖。字段全部是
// contract 的接口类型或本仓库地基层类型，不出现任何实现模块的具体类型
// ——这是「不 import M3/M4/M7 实现包」的直接体现：装配点持有具体实现，
// 但把它们收窄成接口传进来，Server 自身的代码看不到具体类型。
type Deps struct {
	// Store 窄接口族：控制台按资源分别声明需要哪些方法，避免整个 Server
	// 依赖 contract.Store 组合接口——测试桩只需要实现用到的那部分。
	Hosts        contract.HostStore
	ArtifactSets contract.ArtifactSetStore
	Hooks        contract.HookStore
	Templates    contract.TemplateStore
	Tasks        contract.TaskStore
	Events       contract.EventStore

	// StateMachine 是 task.state 的唯一所有者（重试、取消都经过它，不在
	// handler 里自行判断状态转换是否合法）。
	StateMachine contract.StateMachine

	// Renderer 供 preview 端点用假数据渲染应答文件与引导菜单。
	Renderer contract.Renderer

	// ParamsResolver 是建任务路径上的同步决策接口（IPAM/TemplateResolver
	// 等阻塞式 provider），在写库之前调用，失败即整体失败、库里不出现任务
	// （§7.4 的阻塞语义）。
	ParamsResolver contract.TaskParamsResolver

	// DiskPlanner 供磁盘 dry-run 端点使用。
	DiskPlanner contract.DiskPlanner

	// EventBus 用于发布「任务已创建」等生命周期事件（fire-and-forget，
	// §7.5）。可为 nil——个别测试或个别部署形态可能不需要生命周期通知，
	// Server 在调用前判空。
	EventBus contract.EventBus

	// Audit 记录写操作的操作人与动作（§9.4）。
	Audit contract.AuditLogger

	// AuditQuery 供 GET /hooks/{id}/audit 查询审计记录（本次契约变更新增，
	// 见 docs/contracts/http-console.md「变更记录」）。与 Audit 分开声明：
	// 前者是审计中间件与 handler 内部使用的纯写入面，后者是本次新增的
	// 查询面——两者语义不同的窄接口，即便装配点（cmd/lober）目前用同一个
	// sqlite.Store 实例满足两者，Server 内部仍按各自的用途分别持有，不
	// 强行把查询方法塞进 Audit 字段的类型里（Audit 的类型是
	// contract.AuditLogger，只有 Log 一个方法，改它的类型会牵连
	// auditMiddleware 与 hook.go 现有的全部调用点）。可为 nil——测试里
	// 不需要查询能力时不必填。
	AuditQuery contract.AuditStore

	// Clock 供会话过期、任务过期等计算使用，测试里换成 fake.Clock 实现
	// 瞬时推进。
	Clock contract.Clock

	// Users 是认证用户表（来自 config.Auth.Users，转换为本模块内部类型
	// 由 cmd/lober 完成——console 包不 import internal/config，避免为了
	//一个 User 结构体拖入 YAML 解析依赖；见 AuthUser 类型定义）。
	Users []AuthUser

	// SessionTTL 是登录会话的固定过期时长（计划任务 1：不做滑动过期）。
	// 零值时 Server 使用一个保守的默认值（见 NewServer）。
	SessionTTL time.Duration

	// TrustedProxyPeer 判断直连 peer 是否为可信的 nginx 注入点（§1.6，
	// 与 M5 一致）：只在直连 peer 是 loopback 时才信任转发头部里的客户端
	// IP。为 nil 时 Server 使用默认实现（判定 host 部分是否为 loopback）。
	TrustedProxyPeer func(remoteAddr string) bool

	// RootSSHPublicKeys 与 gateway.Deps 的同名字段承载同一个部署级配置值
	// （来自 internal/config.Gateway.RootSSHPublicKeys，装配点原样传入）。
	// preview 端点自己构造 model.RenderContext，必须把它一并传进去——
	// 渲染器对空值是硬拒绝（SPEC §1.5：root 不设可复用密码，公钥是唯一
	// 登录通路），不传的后果是 preview 对任何模板都返回渲染失败，而 §8.2
	// 说明 preview 是开发期性价比最高的两个端点之一。
	RootSSHPublicKeys []string

	// ArtifactImporter 供 POST /artifact-sets 登记成功后触发异步导入 job
	// （本轮新增，见 artifact.go 的 ArtifactImporter 接口文档）。可为
	// nil——为 nil 时登记端点只落库、不触发导入，制品集停留在
	// importing（与本断口接入之前的既有行为一致，不是本次改动引入的
	// 新风险）。
	ArtifactImporter ArtifactImporter

	// ArtifactVerifier 供 POST /artifact-sets/{id}/verify 触发异步重算
	// manifest 校验完整性（本轮新增，见 artifact.go 的 ArtifactVerifier
	// 接口文档）。可为 nil——为 nil 时校验端点仍立即返回接受，只是不会
	// 真的触发任何重算（与 ArtifactImporter 同一种按需注入、判空降级的
	// 接缝设计）。
	ArtifactVerifier ArtifactVerifier
}

// AuthUser 是 console 包认识的认证记录形态：用户名、bcrypt 哈希、角色
// 列表。字段与 config.User 逐一对应，但类型定义在本包内——console 不
// import internal/config（那会把「谁能认证」这件小事和 YAML 解析、四类
// 启动校验之类的大依赖绑在一起，且 config 包本身明确说明它不校验角色名，
// 角色码的权威定义在本模块，两者本就该是两个类型）。
type AuthUser struct {
	Name       string
	BcryptHash string
	Roles      []string
}

// defaultSessionTTL 是 SessionTTL 未配置时的兜底值。12 小时与
// config.defaultConsoleTokenTTL 保持一致的量级（一次装机运维班次内足够），
// 但 console 包不引用 config 包的私有常量（未导出且分属不同包），因此
// 这里独立声明一份同值常量。
const defaultSessionTTL = 12 * time.Hour

// Server 持有全部依赖与内部状态（会话表），实现 http.Handler 供
// cmd/lober 挂载到 /console/v1 前缀下。
type Server struct {
	deps     Deps
	sessions *sessionStore
	mux      *http.ServeMux

	clock            contract.Clock
	sessionTTL       time.Duration
	trustedProxyPeer func(remoteAddr string) bool
}

// NewServer 用给定依赖构造一个 Server 并完成路由注册。
func NewServer(deps Deps) *Server {
	s := &Server{
		deps:     deps,
		sessions: newSessionStore(),
		mux:      http.NewServeMux(),
		clock:    deps.Clock,
	}
	if s.sessionTTL = deps.SessionTTL; s.sessionTTL <= 0 {
		s.sessionTTL = defaultSessionTTL
	}
	if s.trustedProxyPeer = deps.TrustedProxyPeer; s.trustedProxyPeer == nil {
		s.trustedProxyPeer = defaultTrustedProxyPeer
	}
	s.routes()
	return s
}

// ServeHTTP 实现 http.Handler，直接委托给内部 mux。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// handleFunc 是路由注册的公共封装：统一加上 /console/v1 前缀，并按
// needAuth/needCode 组合中间件链。中间件顺序固定为
// 审计 → 授权 → 认证 → 业务 handler（Go 的 http.Handler 包装顺序是
// 「外层先执行」，因此写法上按「先认证、再授权、最后审计」的语义反过来
// 嵌套：requireAuth 包 requireCode 包 audit 包 handlerFunc？——不，见下方
// 实际实现：中间件从外到内依次是 requireAuth（最外层先跑）、requireCode、
// audit、handlerFunc（最内层）。这与计划任务 1「先认证再授权」、计划任务 2
// 「审计记录里要有已确定的操作人」的顺序要求一致：审计中间件必须在认证
// 之后才能读到 context 里的用户身份。
func (s *Server) handleFunc(method, pattern string, code string, action string, h http.HandlerFunc) {
	var handler http.Handler = h
	if action != "" {
		handler = s.auditMiddleware(action, handler)
	}
	if code != "" {
		handler = s.requireCode(code)(handler)
	}
	// 全部业务端点都要求已登录（viewer 只读也需要登录才能看到数据，
	// §12.5：viewer 角色可见全部路由，但路由本身仍在登录墙之后）。
	handler = s.requireAuth(handler)

	full := "/console/v1" + pattern
	s.mux.Handle(method+" "+full, handler)
}

// handleFuncPublic 注册一个不需要认证的端点（目前只有登录本身）。
func (s *Server) handleFuncPublic(method, pattern string, h http.HandlerFunc) {
	full := "/console/v1" + pattern
	s.mux.Handle(method+" "+full, h)
}

// handleFuncRole 与 handleFunc 类似，但按角色名而非权限码卡门（见
// requireRole 的说明：少数端点的权限表述是「某个角色才能做」）。
func (s *Server) handleFuncRole(method, pattern string, role string, action string, h http.HandlerFunc) {
	var handler http.Handler = h
	if action != "" {
		handler = s.auditMiddleware(action, handler)
	}
	if role != "" {
		handler = s.requireRole(role)(handler)
	}
	handler = s.requireAuth(handler)

	full := "/console/v1" + pattern
	s.mux.Handle(method+" "+full, handler)
}

// writeInternal 是 handler 内部遇到不属于任何已知 sentinel 的错误时的
// 统一出口，直接委托 apierr.WriteError——保留这个小包装是为了让 handler
// 代码里出现的都是 apierr.WriteError/writeInternal 两种调用之一，风格
// 统一，不是每个 handler 各写一遍 err != nil 的分支结构。
func writeInternal(w http.ResponseWriter, err error) {
	apierr.WriteError(w, err)
}

// routes 注册 /console/v1 下的全部端点。按资源分组，组内按
// docs/contracts/http-console.md 的端点总表顺序排列，方便逐条核对。
//
// 按计划任务顺序逐步补全：本行以下的分组随任务 3-8 依次实现，实现前
// 该分组的注册语句被注释掉（handler 方法尚不存在，注册会导致编译失败），
// 每完成一个任务就取消对应分组的注释。
func (s *Server) routes() {
	// 认证：登录本身不需要认证；其余三个只需要已登录，无特定权限码。
	s.handleFuncPublic("POST", "/auth/login", s.handleLogin)
	s.handleFunc("GET", "/user/info", "", "", s.handleUserInfo)
	s.handleFunc("GET", "/auth/codes", "", "", s.handleAuthCodes)
	s.handleFunc("POST", "/auth/logout", "", "", s.handleLogout)

	s.routesHost()
	s.routesArtifactSet()
	s.routesHook()
	s.routesTemplate()
	s.routesTask()
	s.routesDiskDryRun()
}
