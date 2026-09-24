// 本文件是全仓唯一的装配点：把 M2-M8 七个模块接到一起，构造出一个持有
// 全部组件的 App 结构体。对应实现计划任务 1，依据 SPEC §1.1、§1.6、§7.4。
//
// 构造顺序有依赖（计划任务 1「实现流程」）：
//  1. 建三个存储目录（先建目录再开库——目录不存在时失败点会推迟到第一条
//     日志写入，那时机器已经在装了）
//  2. 连 PostgreSQL 并跑 AutoMigrate
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
	"encoding/base64"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"time"

	"gorm.io/gorm"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/console"
	consoleui "github.com/lober/lober/internal/console/ui"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/gateway"
	"github.com/lober/lober/internal/provider"
	"github.com/lober/lober/internal/render"
	"github.com/lober/lober/internal/statemachine"
	"github.com/lober/lober/internal/store/postgres"
)

// App 持有装配完成后的全部组件，字段全部是 contract 的接口类型（除了
// db/store 本身与两个 HTTP handler），供 main.go 与测试逐个断言「这个
// 接缝接上了」（计划任务 1「装配结构体的字段类型全部是 contract 的接口」）。
//
// 字段导出：测试需要逐个断言七个接缝非空。
type App struct {
	DB *gorm.DB

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

// autoRetryScanDecorator 是本装配点为把 Phase 4 新增的自动重试
// （internal/statemachine/scan.go 的 AutoRetryFailedTasks）接进现有的
// 30 秒周期扫描循环而构造的 contract.StateMachine 包装。
//
// 为什么需要这一层装配点包装，而不是让 cmd/lober/scanner.go 直接调用
// AutoRetryFailedTasks：scanner.go 与 main.go 都不在本任务的文件归属
// 范围内（铁律 1），本任务只允许在 cmd/lober/wire.go 里加代码。scanner.go
// 现有的 runScanLoop 只依赖一个窄接口 timeoutScanner{ScanTimeouts}，
// main.go 的 serve() 把 app.StateMachine（本类型的一个实例）传给它——
// 因此让 ScanTimeouts 这一个方法在做完超时扫描之后再多做一件事（跑一轮
// 自动重试判定），就能在不新开一个 goroutine、不改 scanner.go 或
// main.go 任何一行的前提下，让自动重试搭上已经存在的扫描节奏。
//
// 两者共享同一个 30 秒节奏纯粹是「不需要为了这一个新增动作再起一条
// 后台循环」的装配便利，不代表业务上两者有先后依赖——AutoRetryFailedTasks
// 内部查询「当前处于 failed 状态的任务」，与 ScanTimeouts 内部查询
// 「当前阶段预算已过期的非终态任务」是两个完全独立的候选集合，互不影响，
// 顺序对调也不会改变结果（本类型选择先跑超时扫描、再跑自动重试，仅仅是
// 与 ScanTimeouts 本身在 §3.3 里的既有语义顺序一致：先发现新的失败，
// 下一轮才可能捕捉到刚产生的失败候选——但由于是查库判定而不是内存传递，
// 即使颠倒顺序，同一轮里刚被 ScanTimeouts 判失败的任务最早也要等下一轮
// AutoRetryFailedTasks 扫描时才会被看到，不会在本轮内被自动重试，这与
// 计划任务 4「自动重试的对象是已经是 failed 状态的任务」的语义完全一致）。
//
// 自动重试出错不影响 ScanTimeouts 本身的返回值：与 ScanTimeouts 内部
// 「单个任务处置失败不中断整轮」的既有取舍一致（scan.go 文档），本装饰器
// 对 AutoRetryFailedTasks 的错误只记日志，不让它污染 ScanTimeouts 原本
// 就要返回给调用方（scanner.go 的 scanOnce，进而是 slog 日志）的
// (failedCount, err) 语义——调用方原本的错误处理路径是「记录并跳过，
// 下一轮再试」，自动重试的错误处理需要同一条路径，不需要一个新的错误
// 通道。
type autoRetryScanDecorator struct {
	inner *statemachine.Machine
	cfg   config.Scan
}

// newAutoRetryScanDecorator 构造一个 autoRetryScanDecorator。cfg 是
// config.Scan 的值拷贝（Go 的值类型，装配阶段读一次即固定），不是指针——
// 自动重试的开关与次数上限预期只在启动装配阶段确定一次，与
// AdmissionLimit 需要跨越 StartBoot 调用点与装配点两个不同代码位置共享、
// 因此需要 atomic 包级变量的场景不同：本装饰器持有的 cfg 只在自己内部
// 的 ScanTimeouts 方法里被读取，不存在跨包共享的需求。
func newAutoRetryScanDecorator(inner *statemachine.Machine, cfg config.Scan) *autoRetryScanDecorator {
	return &autoRetryScanDecorator{inner: inner, cfg: cfg}
}

func (d *autoRetryScanDecorator) StartBoot(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
	return d.inner.StartBoot(ctx, taskID, actualArch, actualFirmware)
}

func (d *autoRetryScanDecorator) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	return d.inner.MarkAnswerFetched(ctx, taskID)
}

func (d *autoRetryScanDecorator) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	return d.inner.ApplySignal(ctx, taskID, signal, seq)
}

func (d *autoRetryScanDecorator) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	return d.inner.IssueFinalizeToken(ctx, taskID)
}

func (d *autoRetryScanDecorator) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	return d.inner.Fail(ctx, taskID, reason, detail)
}

func (d *autoRetryScanDecorator) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	return d.inner.Cancel(ctx, taskID)
}

func (d *autoRetryScanDecorator) Retry(ctx context.Context, taskID string) (model.Task, error) {
	return d.inner.Retry(ctx, taskID)
}

// ScanTimeouts 先跑既有的超时扫描（原样返回它的 failedCount/err，本装饰
// 器不改变 scanner.go 观察到的既有行为），再跑一轮自动重试判定——自动
// 重试的次数与错误都不汇入返回值，只记日志（见类型文档）。
func (d *autoRetryScanDecorator) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	failedCount, err := d.inner.ScanTimeouts(ctx, asOf)

	retried, retryErr := d.inner.AutoRetryFailedTasks(ctx, statemachine.AutoRetryConfig{
		Enabled:        d.cfg.AutoRetryEnabled,
		MaxAutoRetries: d.cfg.AutoRetryMaxAttempts,
	})
	if retryErr != nil {
		slog.Error("自动重试本轮出错", slog.Any("error", retryErr))
	} else if retried > 0 {
		slog.Info("自动重试本轮完成", slog.Int("retried_count", retried))
	}

	return failedCount, err
}

var _ contract.StateMachine = (*autoRetryScanDecorator)(nil)

// 本装配点历史上在此处定义过 resolvedParamsKeyFixup，一个包一层
// contract.TaskParamsResolver 的转换器：internal/provider/resolve.go 的
// taskParamsResolver.Resolve 曾经产出 fact 命名空间键名
// （model.FactHostname="LOBER_HOSTNAME"、model.FactBizIP="LOBER_BIZ_IP"
// 等）外加 template_id/template_version 两个 bookkeeping 键，而
// internal/render/params.go 的 InlineParams（六个家族的渲染器都用它
// 渲染 ctx.Task.ResolvedParams）只认一张裸键白名单
// {"hostname","biz_ip","role"}——两者的键名体系不对齐，任何非空
// ResolvedParams 的任务在渲染应答文件这一步都会 100% 报错。
// resolvedParamsKeyFixup 曾经在装配点事后把已知键映射回裸键、剔除认不出
// 的键，兜住这个裂缝。
//
// 本轨（n5-resolve-keys）改为在裂缝的源头修复：resolve.go 的 Resolve
// 现在直接读表单裸键（"hostname"，与 static IPAM 一直在用的 "biz_ip"/
// "biz_gateway"/"biz_mask" 同一种裸键约定对齐），且只把 InlineParams
// 白名单认得的裸键（hostname/biz_ip）放进返回值——biz_gw/biz_mask/
// disk_primary/nic_install_mac/template_id/template_version 六项内部
// 计算出的决策结果不再进入返回值（原因与去向见 resolve.go 的 Resolve
// 方法末尾注释）。修复后 provider.NewTaskParamsResolver 的产出已经天然
// 满足 InlineParams 的白名单约束，resolvedParamsKeyFixup 这一层包装
// 不再有事可做——移除前已确认它兜的两件事（键名转换、bookkeeping 键
// 剔除）现在都由 resolve.go 自己完成，且验证过继续保留这一层反而会
// 出错：resolvedParamsKeyFixup 的 factKeyToInlineParamKey 表只认
// model.FactHostname/model.FactBizIP 这两个源键名，resolve.go 修复后
// 已经不再产出这两个键（直接产出裸键 hostname/biz_ip），若不移除这层
// 包装，它会把 resolve.go 新产出的裸键当成"不认识的键"整体剔除，导致
// ResolvedParams 变成永远的空 map——这不是"多余但无害"的包装，是修复
// 表单键读取偏差之后必须同步移除的另一半，否则会制造一个新的、比原来
// 更隐蔽的故障（不再是 500，而是任务建成但 hostname/biz_ip 静默丢失）。
//
// 已确认的另一件事：template_id/template_version 本就已经以
// model.Task.TemplateID/TemplateVersion 字段的形式存在
// （handleCreateTask 从 tpl.ID/tpl.Version 直接赋值），resolve.go 不再
// 产出它们不会造成信息丢失。
//
// buildAppWithDB 现在直接使用 provider.NewTaskParamsResolver 的返回值，
// 不再包装。

// buildApp 按顺序装配全部组件，返回一个持有全部接缝的 App。
//
// cfg 必须已经通过 config.Config.Validate()——buildApp 本身不重复做那四类
// 启动校验，只负责按已确认合法的配置构造组件；构造失败要给出能定位到
// 具体依赖的错误（计划任务 1「构造失败要给出能定位到具体依赖的错误」）。
func buildApp(cfg *config.Config) (*App, error) {
	// 第 1 步：建两个本地存储目录。数据库不再需要建目录——它现在是外部的
	// PostgreSQL 服务，不是本地文件（db_path 那一步随 SQLite 一起删掉了）。
	// 制品与日志目录仍要先建再开库：目录不存在时失败点会推迟到第一条日志
	// 写入，那时机器已经在装了（计划任务 1「实现流程」第 1 步）。
	if err := ensureDir("store.artifact_path", cfg.Store.ArtifactPath); err != nil {
		return nil, err
	}
	if err := ensureDir("store.log_path", cfg.Store.LogPath); err != nil {
		return nil, err
	}

	// 第 2 步：连 PostgreSQL、跑 AutoMigrate、校验索引。三件事都在
	// postgres.Open 内部完成（见该函数文档）；索引校验会拦住「partial
	// index 静默退化」这类不会被任何业务测试发现的问题。
	db, err := postgres.Open(pgConfig(cfg.Store.Postgres))
	if err != nil {
		return nil, fmt.Errorf("cmd/lober: 打开数据库失败（%s@%s:%d/%s）: %w",
			cfg.Store.Postgres.User, cfg.Store.Postgres.Host,
			cfg.Store.Postgres.Port, cfg.Store.Postgres.DBName, err)
	}

	app, err := buildAppWithDB(cfg, db)
	if err != nil {
		closeDB(db)
		return nil, err
	}
	return app, nil
}

// pgConfig 把 config 包的 Postgres 配置翻译成 store 包的 Config。
//
// 两个结构体字段相同却不共用一个类型：config 包是「配置文件的形状」，
// store 包是「连库需要的参数」，两者恰好一致是当下的事实而不是约定——让
// store 包 import config 包会把一个数据访问层绑到具体的配置来源上，
// 装配点做一次显式翻译是这个项目里 contract 层之外的一贯做法。
func pgConfig(c config.Postgres) postgres.Config {
	return postgres.Config{
		Host:         c.Host,
		Port:         c.Port,
		User:         c.User,
		Password:     c.Password,
		DBName:       c.DBName,
		SSLMode:      c.SSLMode,
		SearchPath:   c.SearchPath,
		MaxOpenConns: c.MaxOpenConns,
	}
}

// closeDB 关闭 GORM 句柄底层的连接池。GORM 的 *gorm.DB 自身没有 Close
// 方法（它是个可复制的会话对象，不是连接的所有者），要先取出底层的
// *sql.DB。取不出来时无事可做——那说明连接池本身已经不可用，关闭它没有
// 意义，也没有调用方能对这个错误做出反应。
func closeDB(db *gorm.DB) {
	if sqlDB, err := db.DB(); err == nil {
		sqlDB.Close() //nolint:errcheck // 关闭失败时没有可执行的补救动作，进程正在退出或已经在错误路径上
	}
}

// buildAppWithDB 是 buildApp 去掉「开库」这一步的版本，供测试传一个已经
// 打开的连接直接装配，不需要建目录、写文件——七个接缝是否接上与数据库
// 在哪无关。
func buildAppWithDB(cfg *config.Config, db *gorm.DB) (*App, error) {
	// 第 3 步：构造基础件。
	clock := systemClock{}
	st := postgres.New(db)
	events := provider.NewEventBus()

	logs := newFileLogStore(cfg.Store.LogPath)

	// 第 4 步：构造状态机的四个依赖：token 签发器、precheck 器、日志存储、
	// 事件总线。precheck 器需要制品根目录的真实文件系统（见
	// assemblyPrechecker 文档的契约缺口说明），用两阶段构造打破循环依赖。
	tokens := randomTokenIssuer{}
	precheck := &assemblyPrechecker{artifactRoot: cfg.Store.ArtifactPath}

	sm := statemachine.New(st, clock, tokens, precheck, logs, events)
	precheck.setInner(sm) // 反填：*Machine 实现了 contract.Prechecker。

	// 并发准入上限（Phase 4 新增，SPEC §10 失败模式 11）：StartBoot
	// （internal/statemachine/boot.go）在准入检查这一步读取的是包级的
	// statemachine.globalAdmissionLimit，不是 sm 的字段——见 admission.go
	// 顶部「为什么需要一个包级变量」的说明（Machine 的构造签名冻结在
	// machine.go，不在本任务的文件归属范围内，无法给它加第七个依赖）。
	// 这里在装配阶段调用一次，把配置读到的值覆盖进去；未调用时仍然生效
	// 于 statemachine 包自己的保守默认值（20），不会因为忘记装配就静默
	// 变成不限。
	statemachine.SetGlobalAdmissionLimit(cfg.Scan.AdmissionLimit)

	// smForHTTP 是修复过 StartBoot 契约裂缝的包装（见
	// stateMachineErrorFixup 文档），供网关与控制台的 Deps.StateMachine
	// 字段使用。precheck.setInner 上面已经用了未包装的 sm——那处需要的是
	// *statemachine.Machine 本身满足 contract.Prechecker 这一事实，与
	// StartBoot 的错误语义无关，不应该经过本包装。
	smForHTTP := stateMachineErrorFixup{inner: sm}

	// 第 5 步：构造渲染器（无依赖，纯函数）。
	renderer := &render.Renderer{}

	// 第 6 步：按配置构造七个 provider。hosts 窄接口由 *postgres.Store 天然
	// 满足（它实现了 contract.HostStore，是 provider.hostLister 的超集）。
	providers, err := provider.NewProviders(cfg.Provider, st)
	if err != nil {
		return nil, fmt.Errorf("cmd/lober: 构造 provider 失败: %w", err)
	}
	// 用 provider.NewAlertingNotify 包一层内置的 Notify 实现（Phase 4
	// 新增，实现计划 phase4-01 任务 5）——这是 internal/provider/notify.go
	// 自己文档记录的契约缺口的落地方式（见该文件顶部「契约缺口」段落：
	// 「装配点用 NewAlertingNotify(真实实现) 包一层即可获得诊断字段与
	// 降噪能力」）。providers.Notify 目前是 provider.NewBuiltinNotify()
	// 构造的内置「记结构化日志」实现（provider/factory.go），
	// AlertingNotify 组装 §3.5 的诊断字段与 §3.4 的不可重装警示、并对
	// 同一任务的同一失败去重，再委托给内置实现真正落盘/落日志。
	//
	// 装配点在这里包一层而不是在 provider 包内部直接改 NewBuiltinNotify
	// 的返回值：AlertingNotify 与具体的告警传输方式解耦（notify.go 文档：
	// 「真实生产环境会换成 P2-03 的 http 加载器接的外部告警系统」），装配
	// 点是替换传输层实现、同时保留内容组装与降噪能力这两件事该发生的
	// 位置。
	alertingNotify := provider.NewAlertingNotify(providers.Notify)
	provider.RegisterLifecycleHooks(events, providers.DNS, alertingNotify)

	// 必须接上建任务的同步决策接口（§7.4）。漏了它的后果是 IPAM 失败也能
	// 建出任务，那台机器会带着空的业务 IP 配置装上去，且这个错误不会有
	// 任何报错（计划任务 1「设计要点」明确警示）。
	diskIntent := contract.DiskIntent{
		Policy: string(cfg.Provider.DiskPolicy),
		Params: diskIntentParams(cfg.Provider),
	}
	// 不再需要装配点包一层键名转换——provider.NewTaskParamsResolver 的
	// Resolve 现在直接产出 InlineParams 白名单认得的裸键（hostname/
	// biz_ip），见 resolve.go 与本文件上方移除 resolvedParamsKeyFixup
	// 处的说明。
	paramsResolver := provider.NewTaskParamsResolver(providers.TemplateResolver, providers.DiskPlanner, diskIntent)

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

		// SPEC §9.3 Phase 2 项：制品授权。三个字段原样传递自
		// config.Gateway 的同名字段——cfg 在到达这里之前已经通过
		// config.Validate 校验（SecureLinkEnabled 为 true 时
		// SecureLinkSecret 必须非空），buildApp 本身不重复做这层校验。
		// SecureLinkEnabled 默认 false，此时 SecureLinkSecret 即使是
		// 空字符串也不影响任何行为（gateway.handleBootGrant 只在
		// SecureLinkEnabled 为 true 时才使用这个密钥）。
		SecureLinkEnabled: cfg.Gateway.SecureLinkEnabled,
		SecureLinkSecret:  gateway.SecureLinkSecret(cfg.Gateway.SecureLinkSecret),
		SecureLinkTTL:     cfg.Gateway.SecureLinkTTL,

		// ArtifactRoot 与 RenderESXiBootCfg 是本轮任务要接上的断口：ESXi
		// 的 boot.cfg 请求需要读出导入时保存的原始引导参数文件模板再做
		// 三处改写（prefix=/kernelopt= 的 ks=/modules= 去前导斜杠），
		// 渲染器包不能自己读盘（零 IO、纯函数的约束），网关也不能直接
		// import internal/render（跨模块 import，会被 internal/archtest
		// 的 TestModulesDoNotImportEachOther 拦住）——cmd/lober 是全仓
		// 唯一豁免这条限制的包，因此读盘发生在 gateway 层（Deps.
		// ArtifactRoot 提供根目录），改写函数在这里原样注入（签名逐字符
		// 一致，不需要包一层适配），网关本身对 internal/render 零依赖。
		//
		// cfg.Store.ArtifactPath 与 assemblyPrechecker.artifactRoot、
		// importRunner.artifactRoot（本文件下方的 importer 构造处）取
		// 同一个配置值——三处都需要把「制品根目录」与「具体某个制品集的
		// ID」拼成该制品集自己的子目录，只是拼接时机不同。
		ArtifactRoot:      cfg.Store.ArtifactPath,
		RenderESXiBootCfg: render.RenderESXiBootCfg,
	})

	authUsers := make([]console.AuthUser, 0, len(cfg.Auth.Users))
	for _, u := range cfg.Auth.Users {
		authUsers = append(authUsers, console.AuthUser{Name: u.Name, BcryptHash: u.BcryptHash, Roles: u.Roles})
	}

	// importRunner 是 console.ArtifactImporter 的生产实现（见
	// importrunner.go）：登记端点触发导入后，另起 goroutine 调用
	// internal/imports.Job.Run，把制品根目录（cfg.Store.ArtifactPath）
	// 与真实挂载式抽取器接到一起——这是本轮任务要接上的关键断口：此前
	// 登记端点把状态写成 importing 就结束了，没有任何东西触发导入 job。
	importer := newImportRunner(st, nil, cfg.Store.ArtifactPath)

	// verifier 是 console.ArtifactVerifier 的生产实现（见 verifier.go）：
	// 校验端点触发后，另起 goroutine 重算 manifest 并与已登记的
	// ManifestSHA256 比对——把本轮任务要接上的另一个断口接好：此前校验
	// 端点只确认制品集存在就返回接受，没有任何东西真的重算过 sha256。
	verifier := newArtifactVerifier(st, cfg.Store.ArtifactPath)

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
		Audit:          newAuditLogger(st, clock),
		AuditQuery:     st,
		Clock:          clock,

		Users:      authUsers,
		SessionTTL: cfg.Console.TokenTTL,

		// 与 gateway.Deps 取同一个配置值：preview 端点自己构造
		// RenderContext，而渲染器对空公钥列表是硬拒绝（SPEC §1.5）。
		RootSSHPublicKeys: cfg.Gateway.RootSSHPublicKeys,

		ArtifactImporter: importer,
		ArtifactVerifier: verifier,
	})

	// 第 8 步：组装成一个持有全部组件的结构体返回。
	return &App{
		DB:     db,
		Store:  st,
		Clock:  clock,
		Events: events,

		TokenIssuer: tokens,
		Prechecker:  precheck,
		// StateMachine 字段用 autoRetryScanDecorator 包一层 sm——见该类型
		// 文档。main.go 的 serve() 用 app.StateMachine 驱动
		// cmd/lober/scanner.go 现有的 30 秒周期扫描循环（那两个文件都不
		// 在本任务的文件归属范围内，本装配点通过「让 ScanTimeouts 这一个
		// 方法多做一件事」把自动重试接进同一个循环节奏，不需要新开一个
		// goroutine、不需要改 scanner.go 或 main.go 任何一行）。
		StateMachine: newAutoRetryScanDecorator(sm, cfg.Scan),
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
