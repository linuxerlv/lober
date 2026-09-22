// 本文件是全仓唯一的装配点：把 M2-M8 七个模块接到一起，构造出一个持有
// 全部组件的 App 结构体。对应实现计划任务 1，依据 SPEC §1.1、§1.6、§7.4。
//
// 构造顺序有依赖（计划任务 1「实现流程」）：
//  1. 建三个存储目录（先建目录再开库——目录不存在时失败点会推迟到第一条
//     日志写入，那时机器已经在装了）
//  2. 开 SQLite 并跑迁移
//  3. 构造基础件：系统时钟、Store、日志存储、事件总线
//  4. 构造状态机的四个依赖：token 签发器、precheck 器、日志存储、事件总线
//  5. 构造渲染器（无依赖，纯函数）
//  6. 按配置构造七个 provider
//  7. 构造网关与控制台的 handler，各自注入所需接缝
//  8. 组装成一个持有全部组件的结构体返回
//
// cmd/lober 是 internal/archtest 中唯一豁免跨模块 import 的包——装配点的
// 职责就是看见所有实现，TestAssemblyPointWiresModules 反向确认这一点。
package main

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/console"
	consoleui "github.com/lober/lober/internal/console/ui"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/gateway"
	"github.com/lober/lober/internal/provider"
	"github.com/lober/lober/internal/render"
	"github.com/lober/lober/internal/statemachine"
	"github.com/lober/lober/internal/store/sqlite"
)

// App 持有装配完成后的全部组件，字段全部是 contract 的接口类型（除了
// db/store 本身与两个 HTTP handler），供 main.go 与测试逐个断言「这个
// 接缝接上了」（计划任务 1「装配结构体的字段类型全部是 contract 的接口」）。
//
// 字段导出：测试需要逐个断言七个接缝非空。
type App struct {
	DB *sql.DB

	Store  contract.Store
	Clock  contract.Clock
	Events contract.EventBus

	TokenIssuer  contract.TokenIssuer
	Prechecker   contract.Prechecker
	StateMachine contract.StateMachine
	Renderer     contract.Renderer

	Providers      provider.Providers
	ParamsResolver contract.TaskParamsResolver

	GatewayServer *gateway.Server
	ConsoleServer *console.Server
	UIHandler     http.Handler

	// artifactRoot 是制品根目录（cfg.Store.ArtifactPath），供 preflight.go
	// 的启动自检构造 os.DirFS 使用——与 assemblyPrechecker 注入的
	// artifactFS 指向同一个目录，两处校验（引导时的 precheck、启动时的
	// preflight）看到的是同一份磁盘状态。
	artifactRoot string

	cfg *config.Config
}

// systemClock 是 contract.Clock 的生产实现：Now() 直接读系统时间。
//
// 本仓库此前只有 internal/core/fake.Clock（测试用的可控时钟），生产环境
// 需要一个真实读系统时间的实现——这是装配点必须补的最后一块（没有第二个
// 模块负责它）。
type systemClock struct{}

func (systemClock) Now() int64 { return time.Now().Unix() }

var _ contract.Clock = systemClock{}

// tokenRandomBytes 与 internal/statemachine/token.go 的同名常量取相同值
// （32 字节 = 256 位）——两处独立实现同一条强度要求（必须用 crypto/rand，
// 不能用 math/rand、不能从时间戳或任务 id 派生，SPEC §0.1/§9.1），逻辑本身
// 只有几行且已被 M3 的单元测试覆盖过一次，装配点这里重复它是为了避免走
// assemblyPrechecker 那种「两阶段构造」的额外复杂度——IssueToken 与
// Check 不同，装配点完全没有必要复用 *Machine 的实现，直接写一份同样
// 简单直接的版本即可。
const tokenRandomBytes = 32

// randomTokenIssuer 是 contract.TokenIssuer 的生产实现：用
// crypto/rand 生成不可预测的随机串，base64url 编码、不带填充（与
// internal/statemachine/token.go 的生成方式一致，token 走 URL 路径段，
// SPEC §4.4）。
type randomTokenIssuer struct{}

func (randomTokenIssuer) IssueToken(ctx context.Context) (string, error) {
	buf := make([]byte, tokenRandomBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("cmd/lober: 生成 token 随机字节失败: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

var _ contract.TokenIssuer = randomTokenIssuer{}

// assemblyPrechecker 是本装配点为解决「statemachine 的 StartBoot 硬编码
// 空文件系统」这一契约缺口而构造的 contract.Prechecker 实现。
//
// 契约缺口（详见任务返回值 contractGaps，此处只记落地方式）：
// internal/statemachine/boot.go 的 StartBoot 在调用
// m.prechecker.Check(ctx, arch, firmware, artifact, files) 时，files 参数
// 固定是包内私有函数 artifactFilesFS() 返回的空文件系统，不受装配点控制；
// Machine 的构造函数 New(store, clock, tokens, prechecker, logs, events)
// 也没有 fs.FS 类型的参数可供装配点注入真实制品目录。这两点使得装配点
// 无法通过任何公开的构造参数把 os.DirFS(制品根目录) 传进 StartBoot 的
// 校验路径。
//
// 最保守的绕法（不改 internal/statemachine 任何文件）：
//  1. *statemachine.Machine 本身实现了 contract.Prechecker（Check 方法
//     的三项校验逻辑——arch 一致性、固件兼容、制品文件存在性——全部是
//     导出方法，且该方法不读取 m.prechecker 字段，只用它自己收到的四个
//     参数），因此可以把「已经装配好的 *Machine」当作真正的校验实现来
//     复用。
//  2. 本类型包一层：Check 方法忽略调用方传入的 files（那正是问题所在的
//     空文件系统），转而使用装配时注入的 artifactFS（os.DirFS(制品根
//     目录)），再委托给 inner.Check 完成实际的三项校验。
//  3. 两阶段构造打破循环依赖：先构造 assemblyPrechecker{artifactFS: ...}
//     （inner 字段留空），把它作为 contract.Prechecker 传给
//     statemachine.New 构造出 *Machine；再把这个 *Machine 反填进
//     assemblyPrechecker.inner。StartBoot 只在请求到达时才调用
//     m.prechecker.Check，构造阶段两者互不调用，因此这个「先占位、
//     后填充」的顺序是安全的。
//
// 正解方向（不是本类型能做到的）：给 contract.Prechecker 或
// StateMachine.New 的签名补一个 fs.FS 依赖，由装配点直接注入，
// StartBoot 内部不再自己决定用哪个文件系统。
type assemblyPrechecker struct {
	mu    sync.RWMutex
	inner contract.Prechecker

	// artifactRoot 是制品根目录（cfg.Store.ArtifactPath），不是某一个
	// 制品集自己的目录。docs/contracts/filesystem.md 硬约束 1：
	// ArtifactSet.Paths 里存的相对路径是相对于该制品集自己的根目录
	// （/srv/lober/artifacts/<artifact_set_id>/），而
	// internal/statemachine/precheck.go 的 checkArtifactFiles 直接把
	// artifact.Paths 里的相对路径原样喂给 files fs.FS 校验——这意味着
	// 传给 Check 的 files 必须已经定位到具体某个制品集的子目录，而不是
	// 制品根目录本身。由于 Check 的参数里已经带着 artifact model.ArtifactSet
	// （含 artifact.ID），本类型选择在每次 Check 调用时才拼出
	// os.DirFS(filepath.Join(artifactRoot, artifact.ID))，而不是在构造期
	// 固定一个 fs.FS——固定一个的话就只能正确校验某一个制品集，其余全部
	// 制品集的校验会因为路径多套了一层制品集 ID 目录而误判为缺失。
	artifactRoot string
}

func (p *assemblyPrechecker) setInner(inner contract.Prechecker) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.inner = inner
}

// Check 忽略调用方传入的 files（statemachine.StartBoot 硬编码的空文件
// 系统），改用装配时注入的真实制品根目录、按本次校验的 artifact.ID
// 定位到具体制品集子目录后的文件系统。
func (p *assemblyPrechecker) Check(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, _ fs.FS) error {
	p.mu.RLock()
	inner := p.inner
	root := p.artifactRoot
	p.mu.RUnlock()
	if inner == nil {
		return fmt.Errorf("cmd/lober: assemblyPrechecker 尚未完成两阶段构造（inner 为空），这是装配代码本身的 bug")
	}
	fsys := os.DirFS(filepath.Join(root, artifact.ID))
	return inner.Check(ctx, actualArch, actualFirmware, artifact, fsys)
}

var _ contract.Prechecker = (*assemblyPrechecker)(nil)

// errStartBootPrecheckFailed 是 startBootErrorFixup 包装出的错误，供
// internal/gateway/boot.go 现有的 `if err != nil { writeNoTaskScript(...) }`
// 分支识别到「这次 StartBoot 调用不该被当成成功」。不导出为 contract 层
// sentinel——它只是本装配点为弥合一个跨模块契约裂缝而引入的内部标记，不
// 应该被其它地方 errors.Is 依赖。
var errStartBootPrecheckFailed = errors.New("cmd/lober: StartBoot 已将任务置为终态失败，" +
	"网关不应继续渲染成功菜单")

// stateMachineErrorFixup 是本装配点为修复一处真实的跨模块契约裂缝而构造
// 的 contract.StateMachine 包装：
//
// 契约缺口（详见任务返回值 contractGaps，此处只记落地方式）：
// internal/gateway/boot.go 的 handleBootGrant 依赖
// `updated, err := s.deps.StateMachine.StartBoot(...); if err != nil { 回
// sanboot }` 这一行为判断本次引导是否应该失败——它的注释明确写着
// "StartBoot 内部已经把 precheck 错误、attempt 超限都处理成状态机侧的
// Fail 调用"，暗示这类情形下 err 会非 nil。但 internal/statemachine/
// boot.go 的 StartBoot 在 precheck 不通过或 attempt 超限时调用的
// failTask（machine.go）返回的是 (result, nil)——把任务置为 failed 是
// 该函数认定的"成功完成了这次调用"，不是一个需要向上传播的 error。
//
// 两侧对同一个契约方法的行为假设不一致，是本次实测复现（用 e2e 脚本的
// 架构不符负面用例，故意让机器上报 arch=arm64 而制品集要求 x86_64）
// 出来的真实 bug，不是理论推测：机器会收到一份完整的、可以正常拉取内核/
// initrd/kickstart 的成功菜单，即使服务端库里那条任务此刻已经是
// state=failed、failure_reason=precheck——这与 §4.6 硬拒绝的设计意图
// 完全相反（本该拒绝引导的机器仍然能装机）。
//
// 最保守的绕法（不改 internal/gateway 或 internal/statemachine 任何
// 文件）：本类型包一层 contract.StateMachine，只在 StartBoot 上加一条
// 事后检查——调用真实实现后，若返回的任务快照已经是终态失败
// （model.TaskStateFailed），把 (result, nil) 改写成 (result,
// errStartBootPrecheckFailed)，恢复 gateway 的 `if err != nil` 分支原本
// 期望看到的行为。其余七个方法原样透传，不做任何改写——这条裂缝只在
// StartBoot 这一个方法上被 gateway 以「err 非 nil」的方式依赖，其它方法
// 的调用方（console 的 Retry/Cancel、gateway 的 ApplySignal/
// MarkAnswerFetched/IssueFinalizeToken）都是直接检查返回的 error 或
// Task 快照本身，不存在同样的假设错位。
//
// 正解方向（不是本类型能做到的）：让 contract.StateMachine.StartBoot 的
// 文档明确规定「置为 failed 时是否返回 error」，并让
// internal/statemachine 的实现与 internal/gateway 的调用方基于同一份
// 明确的约定，而不是两边各自猜测。
type stateMachineErrorFixup struct {
	inner contract.StateMachine
}

func (f stateMachineErrorFixup) StartBoot(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
	result, err := f.inner.StartBoot(ctx, taskID, actualArch, actualFirmware)
	if err != nil {
		return result, err
	}
	if result.State == model.TaskStateFailed {
		return result, errStartBootPrecheckFailed
	}
	return result, nil
}

func (f stateMachineErrorFixup) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	return f.inner.MarkAnswerFetched(ctx, taskID)
}

func (f stateMachineErrorFixup) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	return f.inner.ApplySignal(ctx, taskID, signal, seq)
}

func (f stateMachineErrorFixup) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	return f.inner.IssueFinalizeToken(ctx, taskID)
}

func (f stateMachineErrorFixup) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	return f.inner.Fail(ctx, taskID, reason, detail)
}

func (f stateMachineErrorFixup) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	return f.inner.Cancel(ctx, taskID)
}

func (f stateMachineErrorFixup) Retry(ctx context.Context, taskID string) (model.Task, error) {
	return f.inner.Retry(ctx, taskID)
}

func (f stateMachineErrorFixup) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	return f.inner.ScanTimeouts(ctx, asOf)
}

var _ contract.StateMachine = stateMachineErrorFixup{}

// resolvedParamsKeyFixup 是本装配点为修复另一处真实的跨模块契约裂缝而
// 构造的 contract.TaskParamsResolver 包装。
//
// 契约缺口（详见任务返回值 contractGaps，此处只记落地方式）：
// internal/provider/resolve.go 的 taskParamsResolver.Resolve 产出的
// resolved map，键名用的是 SPEC §5.4/§5.5 fact 命名空间的形式
// （model.FactHostname="LOBER_HOSTNAME"、model.FactBizIP="LOBER_BIZ_IP"
// 等），外加两个纯 bookkeeping 键 "template_id"/"template_version"；这个
// map 未经任何转换就被 internal/console/task.go 的 handleCreateTask
// 直接写进 task.ResolvedParams。但 internal/render/params.go 的
// InlineParams（AssembleKickstart 在 %pre 段调用它渲染
// ctx.Task.ResolvedParams）只认一张写死的白名单
// {"hostname","biz_ip","role"}——裸键名、不带 LOBER_ 前缀、也不认
// template_id/template_version。render/params.go 自己的注释已经承认了
// 部分缺口（"一旦 M6/M7 确认这两个键会出现在 ResolvedParams 里"，指
// biz_gw/biz_mask），但它假设的是"缺两个键"，而实测（e2e 脚本走一次真实
// 建任务→引导→取应答文件）复现的是更根本的问题："resolved 里的键从来
// 就不是白名单期待的形式"——不是漏了几个键，是键名体系本身就不对齐，
// 任何非空 ResolvedParams 的任务在渲染应答文件这一步都会 100% 报错
// （"表单键 ... 不在已知校验器白名单中"），HTTP 状态码 500。这不是本
// 装配点凭空猜测的边界情形，是本次 e2e 验证第一次跑到"取应答文件"这一
// 步就必然触发的真实故障。
//
// 最保守的绕法（不改 internal/provider 或 internal/render 任何文件）：
// 本类型包一层 contract.TaskParamsResolver，在 Resolve 返回之后、写入
// task.ResolvedParams 之前，把已知的 fact 命名空间键映射回 InlineParams
// 白名单认得的裸键名，并整体剔除两个 bookkeeping 键（template_id/
// template_version 已经分别以 model.Task.TemplateID/TemplateVersion
// 字段存在，不需要也不应该在 ResolvedParams 里重复出现——重复出现只会
// 让第一次渲染就报错）。不认识的 LOBER_BIZ_GW/LOBER_BIZ_MASK
// /LOBER_DISK_PRIMARY 等键按 render/params.go 自己记录的立场原样剔除
// （它们的正确去向是 ctx.Facts，由 InjectFacts 单独处理，不是
// ResolvedParams/InlineParams 这条管线的职责——但目前没有任何模块在建
// 任务路径上调用 FactStore.MergeFacts 把它们写进 facts 表，这是另一段
// 记入 contractGaps 的缺口：即使本类型不剔除这些键，它们也不会通过
// ResolvedParams 这条管线到达 Facts）。
//
// 正解方向（不是本类型能做到的）：让 render/params.go 的 knownParams
// 白名单与 provider/resolve.go 产出的键名体系基于同一份显式约定（要么
// provider 侧改产出裸键名，要么 render 侧改认 LOBER_ 前缀），并让
// biz_gw/biz_mask/disk_primary 类决策结果走 Facts 管线而不是
// ResolvedParams，由 M6 在建任务成功后调用 FactStore.MergeFacts 落库。
type resolvedParamsKeyFixup struct {
	inner contract.TaskParamsResolver
}

// factKeyToInlineParamKey 是 fact 命名空间键到 InlineParams 白名单裸键名
// 的映射表，只覆盖 render/params.go 白名单已经收录、且 provider/
// resolve.go 确实会产出对应决策的两项——hostname 与 biz_ip。不在此表中
// 的键（template_id/template_version/LOBER_BIZ_GW/LOBER_BIZ_MASK/
// LOBER_DISK_PRIMARY）在 Resolve 返回后被整体剔除，见类型文档。
var factKeyToInlineParamKey = map[string]string{
	model.FactHostname: "hostname",
	model.FactBizIP:    "biz_ip",
}

func (f resolvedParamsKeyFixup) Resolve(ctx context.Context, formData map[string]string, facts map[string]string) (map[string]string, error) {
	resolved, err := f.inner.Resolve(ctx, formData, facts)
	if err != nil {
		return nil, err
	}

	fixed := make(map[string]string, len(factKeyToInlineParamKey))
	for factKey, inlineKey := range factKeyToInlineParamKey {
		if v, ok := resolved[factKey]; ok && v != "" {
			fixed[inlineKey] = v
		}
	}
	return fixed, nil
}

var _ contract.TaskParamsResolver = resolvedParamsKeyFixup{}

// buildApp 按顺序装配全部组件，返回一个持有全部接缝的 App。
//
// cfg 必须已经通过 config.Config.Validate()——buildApp 本身不重复做那四类
// 启动校验，只负责按已确认合法的配置构造组件；构造失败要给出能定位到
// 具体依赖的错误（计划任务 1「构造失败要给出能定位到具体依赖的错误」）。
func buildApp(cfg *config.Config) (*App, error) {
	// 第 1 步：建三个存储目录。先建目录再开库——目录不存在时失败点会推迟到
	// 第一条日志写入，那时机器已经在装了（计划任务 1「实现流程」第 1 步）。
	if err := ensureDir("store.db_path 所在目录", filepath.Dir(cfg.Store.DBPath)); err != nil {
		return nil, err
	}
	if err := ensureDir("store.artifact_path", cfg.Store.ArtifactPath); err != nil {
		return nil, err
	}
	if err := ensureDir("store.log_path", cfg.Store.LogPath); err != nil {
		return nil, err
	}

	// 第 2 步：开 SQLite 并跑迁移。dsn 形如 "file:/path/to/db.sqlite"——
	// 不能漏 file: 前缀，sqlite.Open 内部会拼四个必需 pragma（M2 装配说明）。
	dsn := "file:" + filepath.ToSlash(cfg.Store.DBPath)
	db, err := sqlite.Open(dsn)
	if err != nil {
		return nil, fmt.Errorf("cmd/lober: 打开数据库失败（dsn=%s）: %w", dsn, err)
	}

	app, err := buildAppWithDB(cfg, db)
	if err != nil {
		db.Close()
		return nil, err
	}
	return app, nil
}

// buildAppWithDB 是 buildApp 去掉「开库」这一步的版本，供测试用内存
// SQLite（file::memory:?cache=shared 之类）或已经打开的连接直接装配，
// 不需要真的建目录、写文件——七个接缝是否接上与数据库物理位置无关。
func buildAppWithDB(cfg *config.Config, db *sql.DB) (*App, error) {
	// 第 3 步：构造基础件。
	clock := systemClock{}
	st := sqlite.New(db)
	events := provider.NewEventBus()

	logs := newFileLogStore(cfg.Store.LogPath)

	// 第 4 步：构造状态机的四个依赖：token 签发器、precheck 器、日志存储、
	// 事件总线。precheck 器需要制品根目录的真实文件系统（见
	// assemblyPrechecker 文档的契约缺口说明），用两阶段构造打破循环依赖。
	tokens := randomTokenIssuer{}
	precheck := &assemblyPrechecker{artifactRoot: cfg.Store.ArtifactPath}

	sm := statemachine.New(st, clock, tokens, precheck, logs, events)
	precheck.setInner(sm) // 反填：*Machine 实现了 contract.Prechecker。

	// smForHTTP 是修复过 StartBoot 契约裂缝的包装（见
	// stateMachineErrorFixup 文档），供网关与控制台的 Deps.StateMachine
	// 字段使用。precheck.setInner 上面已经用了未包装的 sm——那处需要的是
	// *statemachine.Machine 本身满足 contract.Prechecker 这一事实，与
	// StartBoot 的错误语义无关，不应该经过本包装。
	smForHTTP := stateMachineErrorFixup{inner: sm}

	// 第 5 步：构造渲染器（无依赖，纯函数）。
	renderer := &render.Renderer{}

	// 第 6 步：按配置构造七个 provider。hosts 窄接口由 *sqlite.Store 天然
	// 满足（它实现了 contract.HostStore，是 provider.hostLister 的超集）。
	providers, err := provider.NewProviders(cfg.Provider, st)
	if err != nil {
		return nil, fmt.Errorf("cmd/lober: 构造 provider 失败: %w", err)
	}
	provider.RegisterLifecycleHooks(events, providers.DNS, providers.Notify)

	// 必须接上建任务的同步决策接口（§7.4）。漏了它的后果是 IPAM 失败也能
	// 建出任务，那台机器会带着空的业务 IP 配置装上去，且这个错误不会有
	// 任何报错（计划任务 1「设计要点」明确警示）。
	diskIntent := contract.DiskIntent{
		Policy: string(cfg.Provider.DiskPolicy),
		Params: diskIntentParams(cfg.Provider),
	}
	// 用 resolvedParamsKeyFixup 包一层——见该类型文档：不包这一层的话，
	// 任何走真实建任务路径产出的任务在渲染应答文件这一步会 100% 报错
	// （render/params.go 的 InlineParams 白名单认不出 provider 产出的
	// 键名体系）。
	paramsResolver := resolvedParamsKeyFixup{
		inner: provider.NewTaskParamsResolver(providers.TemplateResolver, providers.DiskPlanner, diskIntent),
	}

	bases := model.BaseURLs{GatewayBase: cfg.Gateway.GatewayBase, ArtifactBase: cfg.Gateway.ArtifactBase}

	// 渲染上下文里的 syslog 指向字段必须是机器能连回来的地址，即网关自己
	// 的 SyslogListen（本进程 syslog UDP 监听地址）——不是 SyslogTarget
	// （本进程把收到的日志转发到的下游集中式日志系统，那是出站地址，与
	// 「告诉装机中的机器往哪发」是两件不同的事，混用会导致 anaconda 把
	// syslog 发到一个它自己够不到、也不会有人监听的下游地址）。
	syslogHost, syslogPort := splitHostPortOrEmpty(cfg.Gateway.SyslogListen)

	// 第 7 步：构造网关与控制台的 handler，各自注入所需接缝。
	gwServer := gateway.NewServer(gateway.Deps{
		TaskStore:        st,
		FactStore:        st,
		EventStore:       st,
		TemplateStore:    st,
		ArtifactSetStore: st,
		HostStore:        st,

		StateMachine: smForHTTP,
		Renderer:     renderer,
		LogStore:     logs,
		Clock:        clock,

		Bases: bases,

		SyslogHost: syslogHost,
		SyslogPort: syslogPort,

		// SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录。
		// 部署级常量，与 Bases/SyslogHost/SyslogPort 同一种"随 Deps 注入、
		// 渲染上下文构造时原样传递"的接缝（见 gateway.Deps.
		// RootSSHPublicKeys 的字段文档）。cfg 在到达这里之前已经通过
		// config.Validate 的校验（非空、每把公钥都是合法白名单格式），
		// buildApp 本身不重复做这层校验。
		RootSSHPublicKeys: cfg.Gateway.RootSSHPublicKeys,
	})

	authUsers := make([]console.AuthUser, 0, len(cfg.Auth.Users))
	for _, u := range cfg.Auth.Users {
		authUsers = append(authUsers, console.AuthUser{Name: u.Name, BcryptHash: u.BcryptHash, Roles: u.Roles})
	}

	consoleServer := console.NewServer(console.Deps{
		Hosts:        st,
		ArtifactSets: st,
		Hooks:        st,
		Templates:    st,
		Tasks:        st,
		Events:       st,

		StateMachine:   smForHTTP,
		Renderer:       renderer,
		ParamsResolver: paramsResolver,
		DiskPlanner:    providers.DiskPlanner,
		EventBus:       events,
		Audit:          newAuditLogger(),
		Clock:          clock,

		Users:      authUsers,
		SessionTTL: cfg.Console.TokenTTL,
	})

	// 第 8 步：组装成一个持有全部组件的结构体返回。
	return &App{
		DB:     db,
		Store:  st,
		Clock:  clock,
		Events: events,

		TokenIssuer:  tokens,
		Prechecker:   precheck,
		StateMachine: sm,
		Renderer:     renderer,

		Providers:      providers,
		ParamsResolver: paramsResolver,

		GatewayServer: gwServer,
		ConsoleServer: consoleServer,
		UIHandler:     consoleui.Handler(),

		artifactRoot: cfg.Store.ArtifactPath,
		cfg:          cfg,
	}, nil
}

// ensureDir 创建目录（含父目录），失败时返回带有「哪个配置项、哪个路径」
// 的错误——构造失败要给出能定位到具体依赖的错误（计划任务 1）。
func ensureDir(label, path string) error {
	if path == "" {
		return fmt.Errorf("cmd/lober: %s 为空，无法创建目录", label)
	}
	if err := os.MkdirAll(path, 0o750); err != nil {
		return fmt.Errorf("cmd/lober: 创建目录失败（%s=%s）: %w", label, path, err)
	}
	return nil
}

// diskIntentParams 把配置里的磁盘策略补充参数（目前只有 by-model 需要的
// 型号串）转成 DiskIntent.Params。
func diskIntentParams(p config.Provider) map[string]string {
	if p.DiskPolicy == model.DiskPolicyByModel {
		return map[string]string{"model": p.DiskModel}
	}
	return map[string]string{}
}

// splitHostPortOrEmpty 把 "host:port" 形式的地址拆成渲染上下文需要的
// 两个字段。addr 为空或格式不合法时返回空字符串与 0——config.Validate
// 已经保证非空的 SyslogListen 必须是合法的具体 IP:port（Validate 对
// gateway.syslog_listen 的校验分支），因此这里的「格式不合法」分支只在
// SyslogListen 本身为空（未配置 syslog 转发目标，属于合法的可选项）时
// 触发，不是运行期需要额外告警的异常状态。
func splitHostPortOrEmpty(addr string) (host string, port int) {
	if addr == "" {
		return "", 0
	}
	h, p, err := net.SplitHostPort(addr)
	if err != nil {
		return "", 0
	}
	portNum, err := strconv.Atoi(p)
	if err != nil {
		return "", 0
	}
	return h, portNum
}
