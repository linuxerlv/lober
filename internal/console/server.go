// 本文件定义 Server 类型：控制台的依赖结构与路由注册。对应实现计划任务 1，
// 依据 docs/contracts/http-console.md、SPEC §8.2。
//
// Server 持有全部接缝（M1 契约窄接口 + M3/M4/M7 通过接口注入的实现），
// 按资源分文件写 handler 方法。不 import internal/statemachine、
// internal/render、internal/provider 的实现包——只依赖
// internal/core/contract 的接口类型，测试用自己的桩（stub_test.go）。
package console

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

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
	// postgres.Store 实例满足两者，Server 内部仍按各自的用途分别持有，不
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
//
// mux 换成 *gin.Engine 是本次 gin 迁移骨架的核心改动（迁移契约 §5），但
// Server 对外仍然实现 http.Handler——ServeHTTP 直接委托给 engine，装配点
// cmd/lober 与全部既有测试按 http.Handler 用它这一点完全不变（gin.Engine
// 本身就实现 ServeHTTP，这不是新增能力，只是内部换了实现）。
type Server struct {
	deps     Deps
	sessions *sessionStore
	engine   *gin.Engine

	clock            contract.Clock
	sessionTTL       time.Duration
	trustedProxyPeer func(remoteAddr string) bool
}

// NewServer 用给定依赖构造一个 Server 并完成路由注册。
func NewServer(deps Deps) *Server {
	// gin.SetMode 是包级全局状态：ReleaseMode 关掉 debug 模式启动时打印
	// 全部路由表的行为（迁移契约 §5）——debug 模式的路由表会淹掉
	// go test -v 的输出，生产环境也不该跑 debug 模式。测试环境如需
	// TestMode（避免警告噪音）由测试自己的 init() 设置，这里统一设成
	// ReleaseMode 是生产默认值，不因为在测试里跑就退让。
	gin.SetMode(gin.ReleaseMode)

	engine := gin.New()
	// 用 gin.New() 而不是 gin.Default()：后者自带 Logger 与 Recovery 中间件
	// （迁移契约 §5）。Logger 会往 stdout 打 gin 自己格式的访问日志，与
	// 项目统一用 log/slog 的方向冲突，形成两套并行日志输出。Recovery
	// 也不能用默认版本——默认版本恢复 panic 后写一个纯文本 500，不是本
	// 项目的 Envelope 信封，前端拦截器解析不出 code 字段。ginRecovery
	// 是本文件下面的替代实现，恢复 panic 后走 apierr.Abort，保证响应
	// 形态与其它错误路径一致。
	engine.Use(ginRecovery())

	s := &Server{
		deps:     deps,
		sessions: newSessionStore(),
		engine:   engine,
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

// ServeHTTP 实现 http.Handler，直接委托给内部 gin 引擎。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.engine.ServeHTTP(w, r)
}

// ginRecovery 是接了 apierr.Abort 的 panic 恢复中间件（迁移契约 §5）：
// gin 默认的 Recovery 恢复后写一段纯文本 500，不是本项目的 JSON 信封，
// 前端拦截器读不到 code 字段就会把「服务端崩溃」误判成「网络层错误」，
// 走不到统一的错误提示逻辑。本实现恢复后改走 apierr.Abort，保证 panic
// 与其它错误路径一样返回 {code:1000,data:null,message:"..."} 信封，且
// 原始 panic 值只进 slog（与 apierr 包「未识别错误不进响应体」的安全
// 边界保持一致方向，不把 panic 携带的任意内容吐给客户端）。
func ginRecovery() gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				slog.Error("控制台 handler panic，已恢复", slog.Any("panic", rec), slog.String("path", c.Request.URL.Path))
				apierr.Abort(c, apierr.New(http.StatusInternalServerError, apierr.CodeInternal,
					"服务器内部错误，请联系管理员并提供操作时间以便查询日志"))
			}
		}()
		c.Next()
	}
}

// handle 注册一个业务端点：要求已登录；code 非空时检查权限码；action 非空
// 时写审计（迁移契约 §5）。等价于迁移前的 handleFunc，参数含义逐一对应。
//
// 权限码是按端点分配的，不是按前缀（迁移契约 §4）——例如 /hooks 下
// create/update/validate 要 codeHookWrite，approve/reject 不要，因此本
// 函数必须支持逐端点指定，不能套 gin RouterGroup 的层级中间件。中间件
// 注册顺序沿用迁移前 handleFunc 的语义：requireAuth 最先跑，其次
// requireCode，最后 auditMiddleware，业务 handler 在最内层——这与「先
// 认证再授权」「审计记录里要有已确定的操作人」的顺序要求一致：审计中间件
// 必须在认证之后才能读到 context 里的用户身份。gin 的 Use 是按注册顺序
// 依次执行（不是标准库 http.Handler 那种反向嵌套），因此这里的调用顺序
// 直接就是执行顺序，不需要像迁移前那样反着包一层。
func (s *Server) handle(method, path, code, action string, h gin.HandlerFunc) {
	handlers := []gin.HandlerFunc{s.requireAuth()}
	if code != "" {
		handlers = append(handlers, s.requireCode(code))
	}
	if action != "" {
		handlers = append(handlers, s.auditMiddleware(action))
	}
	handlers = append(handlers, h)

	full := "/console/v1" + path
	s.engine.Handle(method, full, handlers...)
}

// handlePublic 注册一个不需要认证的端点（目前只有登录本身，迁移契约 §5）。
func (s *Server) handlePublic(method, path string, h gin.HandlerFunc) {
	full := "/console/v1" + path
	s.engine.Handle(method, full, h)
}

// handleFuncRole 与 handle 类似，但按角色名而非权限码卡门（见 requireRole
// 的说明：少数端点的权限表述是「某个角色才能做」）。
//
// 契约缺口说明：迁移契约 §4/§5 冻结的 29 端点表与骨架 API 都没有覆盖
// POST /templates、PUT /templates/{id} 这两个端点——它们在迁移前的代码里
// 就通过 handleFuncRole/requireRole 按 roleAdmin 卡门，不是本次改动引入
// 的新机制。template.go 属于六个资源域文件之一，本 agent 不能修改它的
// 调用点；若骨架只提供契约 §5 列出的 handle/handlePublic，这两个端点会
// 因为没有对应的注册方式而编译失败或需要 template.go 自行发明等价机制，
// 等于把「按角色卡门」这个既有业务语义在迁移中悄悄丢掉。保留本函数是
// 「不改任何业务语义」这条硬约束下唯一不动 template.go 的选择；已在本
// 任务的交付报告里作为契约缺口单独列出，供主会话核对是否需要补一份契约
// 修订。
func (s *Server) handleFuncRole(method, path string, role string, action string, h gin.HandlerFunc) {
	handlers := []gin.HandlerFunc{s.requireAuth()}
	if role != "" {
		handlers = append(handlers, s.requireRole(role))
	}
	if action != "" {
		handlers = append(handlers, s.auditMiddleware(action))
	}
	handlers = append(handlers, h)

	full := "/console/v1" + path
	s.engine.Handle(method, full, handlers...)
}

// writeInternal 是 handler 内部遇到不属于任何已知 sentinel 的错误时的
// 统一出口，直接委托 apierr.WriteError——保留这个小包装是为了让 handler
// 代码里出现的都是 apierr.WriteError/writeInternal 两种调用之一，风格
// 统一，不是每个 handler 各写一遍 err != nil 的分支结构。
func writeInternal(w http.ResponseWriter, err error) {
	apierr.WriteError(w, err)
}

// routes 只管分组与公共前缀（迁移契约 §5）：具体端点由六个资源域各自的
// routesXxx() 注册，本函数只负责调用它们，调用关系照旧不变——六个资源域
// 文件本身不属于本 agent 的改动范围。
func (s *Server) routes() {
	// 认证：登录本身不需要认证；其余三个只需要已登录，无特定权限码。
	s.handlePublic("POST", "/auth/login", s.handleLogin)
	s.handle("GET", "/user/info", "", "", s.handleUserInfo)
	s.handle("GET", "/auth/codes", "", "", s.handleAuthCodes)
	s.handle("POST", "/auth/logout", "", "", s.handleLogout)

	s.routesHost()
	s.routesArtifactSet()
	s.routesHook()
	s.routesTemplate()
	s.routesTask()
	s.routesDiskDryRun()
	s.routesReport()
	s.routesProvenance()
	s.routesExport()
}
