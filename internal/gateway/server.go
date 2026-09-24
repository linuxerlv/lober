// 本文件定义 Server 类型、依赖结构与路由注册。对应实现计划任务 1，依据
// docs/contracts/http-machine.md 端点总表与 SPEC §4 全节、§6 全节。
//
// 网关只做 HTTP 层的事：状态转换交给状态机、文本生成交给渲染器，它自己
// 负责解析请求、构造渲染上下文、把结果写成 iPXE 能消费的形态（计划
// 「架构」小节）。本包不 import internal/statemachine 与 internal/render
// 的实现包，只依赖 internal/core/contract 的接口——这是七个模块能完全
// 并行开发的全部保障，internal/archtest 在合并后的 CI 里拦住任何违反。
package gateway

import (
	"net/http"
	"sync"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// Deps 是 Server 的依赖结构体，把全部接缝（Store 的窄接口、状态机、渲染器、
// 日志存储、时钟、基址配置）都列出来。用结构体传入而不是长参数列表——
// 将来加接缝时不改调用点（计划任务 1「Server 的依赖结构要把六个接缝都
// 列出来」）。
//
// Store 拆成六个窄接口字段而不是嵌入组合的 contract.Store：网关用到任务
// 查找与 token 校验（TaskStore）、facts 读写（FactStore）、信号事件追加
// （EventStore）、渲染上下文取模板/制品集/主机（TemplateStore/
// ArtifactSetStore/HostStore）——窄依赖让测试桩（内存 Store 本身已经满足
// 这些接口）与未来的实现更容易看清网关真正用到了什么（依据
// contract/store.go「各模块应依赖上面的窄接口」的设计说明）。
type Deps struct {
	TaskStore        contract.TaskStore
	FactStore        contract.FactStore
	EventStore       contract.EventStore
	TemplateStore    contract.TemplateStore
	ArtifactSetStore contract.ArtifactSetStore
	HostStore        contract.HostStore

	StateMachine contract.StateMachine
	Renderer     contract.Renderer
	LogStore     contract.LogStore
	Clock        contract.Clock

	// Bases 是网关与制品服务的基址配置（SPEC §1.5：模板中不出现字面
	// http://，scheme 完全来自配置）。
	Bases model.BaseURLs

	// SyslogHost/SyslogPort 是渲染上下文里 syslog 目标字段的来源（供渲染器
	// 把 syslog 目标写进应答文件，机器侧据此上报 syslog）。
	SyslogHost string
	SyslogPort int

	// RootSSHPublicKeys 是要注入每台新装机器 root 账户的 SSH 公钥列表
	// （SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录），
	// 来自 internal/config.Gateway.RootSSHPublicKeys，装配点在构造 Deps
	// 时原样传入。buildRenderContext（rendercontext.go）把它原样复制进
	// 每次渲染的 model.RenderContext.RootSSHPublicKeys 字段——本字段与
	// Bases/SyslogHost/SyslogPort 是同一种"部署级配置值，随 Deps 注入、
	// 每次构造渲染上下文时原样传递"的接缝，不是随请求变化的运行期数据。
	RootSSHPublicKeys []string

	// SecureLinkEnabled/SecureLinkSecret/SecureLinkTTL 是制品授权
	// （SPEC §9.3 Phase 2 项）的三个接缝，来自
	// internal/config.Gateway 的同名字段（装配点原样传入，见
	// cmd/lober/wire.go）。SecureLinkEnabled 为 false 时（默认值）
	// handleBootGrant 不对渲染出的菜单文本做任何签名后处理，行为与本次
	// 改动之前完全一致——这是一个可选的加固开关，不是新的硬性要求。
	//
	// 密钥本身不出现在 model.RenderContext 里（见 siglink.go 文件头
	// 注释：渲染器是纯函数，不该持有密钥），只在网关自己的
	// handleBootGrant 里使用，构造 SignedArtifactURL 的调用参数。
	SecureLinkEnabled bool
	SecureLinkSecret  SecureLinkSecret
	SecureLinkTTL     time.Duration

	// ArtifactRoot 是制品根目录（磁盘路径，与 internal/config.Store.
	// ArtifactPath 同一个值），供 handleAnswer 在 ESXi 家族的 boot.cfg
	// 请求上读出「导入时保存的原始引导参数文件模板」文本——那份文本只在
	// 磁盘上存在，ArtifactSet.Paths.Extra["boot.cfg"]（model.RenderContext
	// 能拿到的输入）只是相对路径字符串，不是文件内容（见
	// esxi_bootcfg.go 文件头注释）。
	//
	// 与 Bases.ArtifactBase 是两个不同维度的东西：Bases.ArtifactBase 是
	// 给机器用的 HTTP 基址（拼 URL），本字段是本进程自己读盘用的本地
	// 文件系统路径——网关此前只做 HTTP 层的事、不碰本地磁盘，这是它第一次
	// 需要读盘，因此原有的六个 Store 接口字段与 Bases 都不能替代它。
	//
	// 空字符串表示未注入（例如某些测试或部署形态不需要它）：此时 ESXi 的
	// boot.cfg 请求走 handleAnswer 的降级路径（500 + TaskEvent，见
	// esxi_bootcfg.go），不会静默返回 kickstart 正文——那正是本字段加入
	// 之前的错误行为。
	ArtifactRoot string

	// RenderESXiBootCfg 是 internal/render.RenderESXiBootCfg 的注入点：
	// 网关不能直接 import internal/render（那是一次跨模块 import，
	// internal/archtest 的 TestModulesDoNotImportEachOther 会拦住——
	// render 与 gateway 是 M2-M8 七个互斥模块中的两个，即使这次的需求是
	// 「网关读盘、渲染器改写」这种看起来自然的分工，网关也只能通过接口/
	// 函数值间接引用渲染逻辑，不能直接 import 它的包）。
	//
	// 签名与 internal/render.RenderESXiBootCfg(template, bootCfgURLBase,
	// answerFileURL string) (string, error) 逐字符一致，因此装配点
	// （cmd/lober/wire.go，全仓唯一豁免跨模块 import 的包）只需要写
	// `RenderESXiBootCfg: render.RenderESXiBootCfg` 一行即可完成注入，
	// 不需要包一层适配函数。
	//
	// 为 nil 时（测试未注入、或某些部署形态不需要）handleAnswer 对 ESXi
	// 的 boot.cfg 请求走降级路径（500 + TaskEvent），不会 panic、也不会
	// 静默返回 kickstart 正文。
	RenderESXiBootCfg func(template, bootCfgURLBase, answerFileURL string) (string, error)
}

// Server 持有全部依赖，实现 http.Handler（内部用标准库 ServeMux 路由）。
type Server struct {
	deps Deps
	mux  *http.ServeMux

	// clientTaskMu/clientTaskMap 是进程内的「客户端 IP → 任务」归档映射，
	// 只服务于 syslog 接收器（见 syslog.go 文件头注释里的契约缺口说明：
	// model.Task 没有「当前客户端 IP」字段，Store 契约也没有按 IP 反查
	// 任务的方法，本映射是网关自己维护的最保守绕法）。零值可用（nil map
	// 只读安全，写入前 rememberClientTask 会惰性初始化）。
	clientTaskMu  sync.RWMutex
	clientTaskMap map[string]clientTaskEntry
}

// NewServer 构造一个 Server 并注册全部路由。deps 的字段均不做 nil 校验——
// 那是装配点（cmd/lober，M9）的职责，网关本身假定收到的依赖已经装配完整
// （与 M1 其它契约实现的分工一致）。
func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, mux: http.NewServeMux()}
	s.routes()
	return s
}

// ServeHTTP 实现 http.Handler，直接委托给内部的 ServeMux。
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mux.ServeHTTP(w, r)
}

// routes 注册六条机器面 HTTP 路由（syslog 是独立的 UDP 监听器，不在这个
// ServeMux 里，见 syslog.go）。路径与方法逐字对应
// docs/contracts/http-machine.md 的端点总表，不自行发明路径形态。
func (s *Server) routes() {
	s.mux.HandleFunc("GET /boot.ipxe", s.handleBootScript)
	s.mux.HandleFunc("GET /v1/boot", s.handleBoot)
	s.mux.HandleFunc("GET /v1/answer/{task}/{token}/{file}", s.handleAnswer)
	s.mux.HandleFunc("POST /v1/signal/{task}/{token}/{phase}", s.handleSignal)
	s.mux.HandleFunc("POST /v1/facts/{task}/{token}", s.handleFactsWrite)
	s.mux.HandleFunc("GET /v1/facts/{task}/{token}", s.handleFactsRead)
}
