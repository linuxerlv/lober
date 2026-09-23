// 本文件实现导入 job 的状态机：importing → ready 或 failed。对应实现
// 计划任务 11，依据 SPEC §2.1、docs/contracts/filesystem.md。
//
// 导入是异步 job（SPEC §2.1）：几 GB 的解压要几分钟，不能阻塞 HTTP 请求。
// 本文件只实现 job 的执行逻辑（Run 方法），不实现异步调度本身（不起
// goroutine、不管 HTTP 响应）——调用方（M6 的控制台 handler）负责在收到
// 登记请求后另起 goroutine 调用 Run，本模块不关心调用方用什么并发原语
// 触发它，只保证 Run 内部的状态流转与并发控制正确。
package imports

import (
	"context"
	"errors"
	"fmt"
	"io/fs"

	"github.com/lober/lober/internal/core/model"
)

// artifactSetUpdater 是 Job 依赖的最窄写回/读取接口：只需要
// contract.ArtifactSetStore 的三个方法（读取、状态更新、内容写回），不需要
// 完整接口的 Create/List/Delete——窄依赖让测试替身只实现这三个方法。
//
// GetArtifactSet 是本轮（n2 轨，Windows 解封）新增的方法：Windows 的固件
// 维度是硬约束（SPEC §0.3、§2.1），declaredFirmware 的唯一正确来源是登记时
// 运维填的 model.ArtifactSet.Firmware 字段，而不是某个额外的函数参数——
// 本方法签名与 contract.ArtifactSetStore.GetArtifactSet 完全一致（只读，
// 未修改 internal/core/contract/**），任何真实 Store 天然已经满足，不需要
// 额外适配代码。
//
// 状态更新与内容写回分成两个方法调用（对应 contract.ArtifactSetStore 的
// 设计）：导入失败时只调 UpdateState，不调 SetContent——失败时 Paths
// 可能尚未写入或只写了一半，绝不能被状态更新覆盖成半成品（任务 11：
// 失败时已写入的路径不被清空）。
type artifactSetUpdater interface {
	GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error)
	UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256 string) (model.ArtifactSet, error)
	SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error)
}

// WindowsSourcesResolver 是「按制品集 ID 取 Windows 外部文件路径集合」的
// 窄接口（本轮 n2 轨新增，解除 Windows 的 MVP 封锁）。
//
// wimboot/BCD/boot.sdi/boot.wim 四个文件不在 ISO 里，是运维在登记之外
// 单独准备、放在某处的本地文件（extract_windows.go 的
// WindowsExternalSources 文档）。Job.Run 的固定四参数签名
// (ctx, artifactSetID, isoPath, destDir) 里没有位置携带这四个路径，而
// console.ArtifactImporter.TriggerImport 与 cmd/lober 的装配代码同样没有
// 携带它们的参数（本任务不允许修改 internal/console/** 或 cmd/**）——
// 做成可注入接口，把「按 ID 去哪里找这四个文件」这个决定权交给调用方，
// Job 本身不关心具体实现（本地目录约定、配置文件、数据库表都可以），
// 与 Extractor/WimArchDetector 两个接口的设计理由一致。
//
// 生产环境如何真正把这个接口接到 cmd/lober 的装配点，是本轮遗留的契约
// 缺口（见文件末尾契约缺口说明）：当前没有任何调用方满足这个接口，
// Job 在未配置时会对 Windows 家族返回明确错误（见 Run 方法），不会静默
// 成功或 panic。
type WindowsSourcesResolver interface {
	// ResolveWindowsSources 返回 artifactSetID 对应的 Windows 外部文件
	// 路径集合。找不到或路径无效时返回 error，调用方（Run）据此让整个
	// 导入失败，不能假设一组默认路径继续。
	ResolveWindowsSources(ctx context.Context, artifactSetID string) (WindowsExternalSources, error)
}

// Job 是单次 ISO 导入的执行体。
//
// 并发控制：同一时刻只允许一个导入 job 运行（任务 11：解压是 IO 密集的，
// 并发两个只会让两个都变慢；运维一次导入一个 ISO 是常态）。用带缓冲为 1
// 的信号量实现，第二个请求排队而不是拒绝——Job 的每个方法调用都会获取
// 同一个包级信号量，跨多个 Job 实例共享（否则「同一时刻只允许一个」这条
// 约束就只在单个 Job 实例内成立，两个不同 Job 实例仍能并发跑，与设计
// 要点矛盾）。
//
// windowsSourcesResolver 与 windowsArchDetector 是本轮新增的两个可选字段
// （零值 nil 表示「未配置」），通过 ConfigureWindows 方法注入而不是塞进
// NewJob 的参数列表——这样做的理由：
//  1. NewJob 的签名保持不变，cmd/lober 现有的 imports.NewJob(r.store,
//     r.extractor) 调用点不需要跟着改（那处于 cmd/lober，本任务不拥有）；
//  2. RHEL/Debian/Ubuntu/SUSE 四个家族完全不需要这两个字段，让它们的
//     调用路径（RunRHEL、familyExtractors 查表分派）保持对这两个字段
//     零感知，符合「不接死在必填参数里」的一致设计。
type Job struct {
	store     artifactSetUpdater
	extractor Extractor

	windowsSourcesResolver WindowsSourcesResolver
	windowsArchDetector    WimArchDetector
}

// importSemaphore 是全局导入并发信号量，缓冲为 1。包级变量而非 Job 的
// 实例字段：并发限制的对象是「导入这件事」本身，不是某个特定 Job 实例——
// 两次调用 NewJob 构造出的不同实例，仍必须互相排队，因为它们操作的是
// 同一台服务器上同一份磁盘 IO 带宽。
var importSemaphore = make(chan struct{}, 1)

// NewJob 构造一个导入 job。
func NewJob(store artifactSetUpdater, extractor Extractor) *Job {
	return &Job{store: store, extractor: extractor}
}

// ConfigureWindows 注入 Windows 导入所需的两个额外依赖（本轮 n2 轨新增）：
// resolver 负责按制品集 ID 找到 wimboot/BCD/boot.sdi/boot.wim 四个外部
// 文件的本地路径，detector 负责探测 boot.wim 实际的固件位宽。
//
// 不在 NewJob 里做成必填参数：RHEL/Debian/Ubuntu/SUSE 四个已实现家族的
// 调用方（包括全部既有测试）不需要关心 Windows 的这两个依赖，若做成
// 必填参数，所有既有的 NewJob(store, extractor) 调用都要跟着改——那正是
// 本任务铁律 3 明确禁止的「改动既有函数时不得让任何现存测试变红」。
func (j *Job) ConfigureWindows(resolver WindowsSourcesResolver, detector WimArchDetector) {
	j.windowsSourcesResolver = resolver
	j.windowsArchDetector = detector
}

// familyExtractor 是「按识别结果选抽取函数」这张表里每一行的统一形状：
// 接收 Identify 已确认过的文件系统视图与目标根目录，返回落盘路径。
//
// 只有输入输出都是（fs.FS, destDir）→（ArtifactPaths, error）的抽取函数
// 才能进表——RHEL/Debian/Ubuntu 三者都满足。Windows（还需要
// WindowsExternalSources/declaredFirmware/WimArchDetector 三个额外参数）
// 与 ESXi（还需要 declaredFirmware）的签名不满足这个形状，装它们需要
// job.go 之外先收集这些额外输入，不是简单的「表里加一行」；本表故意只
// 收窄到三个签名一致的家族，另两个family的接线留给能拿到那些额外参数的
// 调用层（见文件末尾契约缺口说明）。
type familyExtractor func(root fs.FS, destDir string) (model.ArtifactPaths, error)

// familyExtractors 是「识别到的 OS 家族 → 对应抽取函数」的查找表（任务
// 「不接死 if 分支」的设计要求）：Run 只查表，不知道表里有哪些家族。
//
// 新增一个签名兼容的家族（未来若有）只需要在此加一行，不改 Run 本身的
// 分派逻辑——本仓库当前 Identify 能返回的家族里，只有 RHEL/Debian/Ubuntu
// 三者的 ExtractXxx 签名与 familyExtractor 一致，因此表目前只有三行；
// Windows/ESXi 由 Identify 自身在识别阶段就返回
// ErrUnsupportedOSFamily（identify.go，非本文件职责），不会走到这张表的
// 查找，SUSE 则连 Identify 都识别不出（同样不是本文件职责）——两者都会
// 在 Run 里被下面的 switch 分支正确地判定为「未支持」而非「表里没有」，
// 语义更精确：真正意义上的「表里没有」只留给「Identify 已经判定支持、
// 但本表尚未收录对应抽取函数」这一种情况（比如未来某天 identify.go 学会
// 识别 SUSE 之后，本表还没加对应行的窗口期）。
var familyExtractors = map[model.OSFamily]familyExtractor{
	model.OSFamilyRHEL:   ExtractRHEL,
	model.OSFamilyDebian: ExtractDebian,
	model.OSFamilyUbuntu: ExtractUbuntu,
	model.OSFamilySUSE:   ExtractSUSE,
}

// Run 是导入 job 的统一入口（任务：按识别结果自动分派，取代逐家族单独
// 暴露一个 RunXxx 方法）：
//  1. 打开抽取器得到文件系统视图
//  2. 识别 OS 家族——Identify 已经把 Windows/ESXi（识别出但 MVP 不支持）
//     与完全识别不出这两类情形分别用不同的 sentinel error 区分开
//     （identify.go），Run 据此给出不同的失败原因文案
//  3. 查 familyExtractors 表；查到就调用对应抽取函数，查不到（这种情况
//     目前不会发生，因为 Identify 只会返回表里已经收录的三个家族之一，
//     或是被上面第 2 步拦下的 Windows/ESXi/未识别——保留这个分支是为了
//     不静默假设「Identify 能返回的家族」与「本表收录的家族」永远一一
//     对应，未来任何一侧单独变化都会在这里显式报错而不是 panic）落到
//     明确的「未接线」错误
//  4. 生成 manifest
//  5. 写回家族与路径
//  6. 置 ready 并记录 manifest 校验值
//
// 任何一步失败都置 failed 并写具体原因，不继续后续步骤（任务 11）。
//
// isoPath 是登记时运维填写的本地路径（气隙环境下以「登记服务器本地路径」
// 为主，SPEC §2.1），destDir 是制品集在磁盘上的根目录。
func (j *Job) Run(ctx context.Context, artifactSetID, isoPath, destDir string) error {
	importSemaphore <- struct{}{}
	defer func() { <-importSemaphore }()

	root, err := j.extractor.Open(ctx, isoPath)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("打开 ISO 失败: %v", err))
	}

	family, err := Identify(root)
	switch {
	case err == nil:
		// 继续往下走查表分派。
	case errIsUnsupported(err):
		return j.fail(ctx, artifactSetID, fmt.Sprintf(
			"识别到家族 %s，但 MVP 不支持该家族的导入（SPEC §2.1、计划任务 9）", family))
	default:
		return j.fail(ctx, artifactSetID, fmt.Sprintf("识别不出 OS 家族: %v", err))
	}

	// Windows 单独判一次，不进 familyExtractors 查表（本轮 n2 轨新增）：
	// ExtractWindows 比表的行类型 func(fs.FS, string) (ArtifactPaths, error)
	// 多三个参数（外部文件路径、声明固件、架构探测器），且这三者必须按
	// artifactSetID 动态解析（同一 Job 实例的不同调用对应不同制品集），
	// 用闭包在建表时静态绑定做不到这一点。这个分支必须放在
	// familyExtractors 查表之前——否则 Windows 会先落到「表里没有这一行」
	// 的错误分支，永远走不到这里。
	if family == model.OSFamilyWindows {
		return j.runWindows(ctx, artifactSetID, root, destDir)
	}

	extract, ok := familyExtractors[family]
	if !ok {
		return j.fail(ctx, artifactSetID, fmt.Sprintf(
			"识别到家族 %s，但导入 job 尚未接线对应的抽取函数"+
				"（identify.go 判定为已支持，job.go 的分派表却缺这一行——"+
				"两侧不同步，需要给 familyExtractors 补上这个家族）", family))
	}

	paths, err := extract(root, destDir)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("抽取内核/initrd/仓库树失败: %v", err))
	}

	manifest, err := GenerateManifest(destDir)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("生成 manifest 失败: %v", err))
	}

	if _, err := j.store.SetArtifactSetContent(ctx, artifactSetID, family, paths); err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("写回制品集内容失败: %v", err))
	}

	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, manifest.SHA256); err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("写回就绪状态失败: %v", err))
	}

	return nil
}

// runWindows 是 Windows 家族的导入分支（本轮 n2 轨新增，被 Run 在识别为
// windows 时调用）。与 Run 主体的表驱动分支共用同样的收尾三步（生成
// manifest、写回内容、置 ready），但前置步骤不同：Windows 需要先从制品集
// 读出运维登记时声明的固件、再向 resolver 要外部文件路径，这两步 RHEL/
// Debian/Ubuntu/SUSE 完全不需要，因此不放进公共路径，避免为了复用而让
// 四个已稳定的家族的分派逻辑跟着变形。
func (j *Job) runWindows(ctx context.Context, artifactSetID string, root fs.FS, destDir string) error {
	if j.windowsSourcesResolver == nil || j.windowsArchDetector == nil {
		return j.fail(ctx, artifactSetID,
			"识别到家族 windows，但导入 job 尚未调用 ConfigureWindows 完成配置"+
				"（需要注入 WindowsSourcesResolver 与 WimArchDetector 两个依赖才能"+
				"继续，见 job.go 契约缺口说明：生产装配点目前还没有这两个依赖的"+
				"真实实现）")
	}

	set, err := j.store.GetArtifactSet(ctx, artifactSetID)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("读取制品集声明的固件失败: %v", err))
	}

	ext, err := j.windowsSourcesResolver.ResolveWindowsSources(ctx, artifactSetID)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("解析 Windows 外部文件来源失败: %v", err))
	}

	paths, err := ExtractWindows(root, ext, destDir, set.Firmware, j.windowsArchDetector)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("抽取 Windows 制品失败: %v", err))
	}

	manifest, err := GenerateManifest(destDir)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("生成 manifest 失败: %v", err))
	}

	if _, err := j.store.SetArtifactSetContent(ctx, artifactSetID, model.OSFamilyWindows, paths); err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("写回制品集内容失败: %v", err))
	}

	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, manifest.SHA256); err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("写回就绪状态失败: %v", err))
	}

	return nil
}

// RunRHEL 是 Run 的兼容入口（保留给既有测试与既有调用方，任务要求「保留
// RunRHEL 作为兼容入口」）：委托给 Run，额外校验一次识别结果确实是
// RHEL——RunRHEL 这个名字本身承诺了「这次调用针对的是 RHEL」，如果实际
// 识别出别的家族，用一个具体指出「家族与调用的方法不符」的原因拒绝，而
// 不是悄悄按 Run 的通用分派逻辑接受任意家族（那会让 RunRHEL 这个名字名不
// 副实，调用方传错了 isoPath 也不会得到任何提示）。
func (j *Job) RunRHEL(ctx context.Context, artifactSetID, isoPath, destDir string) error {
	importSemaphore <- struct{}{}
	defer func() { <-importSemaphore }()

	root, err := j.extractor.Open(ctx, isoPath)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("打开 ISO 失败: %v", err))
	}

	family, err := Identify(root)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("识别不出 OS 家族: %v", err))
	}
	if family != model.OSFamilyRHEL {
		return j.fail(ctx, artifactSetID, fmt.Sprintf(
			"识别到的 OS 家族是 %s，本次调用的是 RunRHEL——家族与调用的抽取方法不符", family))
	}

	paths, err := ExtractRHEL(root, destDir)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("抽取内核/initrd/仓库树失败: %v", err))
	}

	manifest, err := GenerateManifest(destDir)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("生成 manifest 失败: %v", err))
	}

	if _, err := j.store.SetArtifactSetContent(ctx, artifactSetID, family, paths); err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("写回制品集内容失败: %v", err))
	}

	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, manifest.SHA256); err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("写回就绪状态失败: %v", err))
	}

	return nil
}

// RunUnsupported 是给「识别出但 MVP 不支持」（Windows/ESXi）与「识别不出」
// 两类场景提供的独立入口，供 job.go 的调用方（控制台 handler）在还不知道
// ISO 家族之前先统一走一次识别，再决定调用 RunRHEL 还是直接判定为不支持。
//
// 与 RunRHEL 共享同一个并发信号量与失败写回逻辑，只是不执行任何抽取。
func (j *Job) RunUnsupported(ctx context.Context, artifactSetID, isoPath string) error {
	importSemaphore <- struct{}{}
	defer func() { <-importSemaphore }()

	root, err := j.extractor.Open(ctx, isoPath)
	if err != nil {
		return j.fail(ctx, artifactSetID, fmt.Sprintf("打开 ISO 失败: %v", err))
	}

	family, err := Identify(root)
	switch {
	case err == nil:
		return j.fail(ctx, artifactSetID, fmt.Sprintf(
			"识别到家族 %s 有对应的抽取实现，不应调用 RunUnsupported——调用方逻辑错误", family))
	case errIsUnsupported(err):
		return j.fail(ctx, artifactSetID, fmt.Sprintf(
			"识别到家族 %s，但 MVP 不支持该家族的导入（SPEC §2.1、计划任务 9）", family))
	default:
		return j.fail(ctx, artifactSetID, fmt.Sprintf("识别不出 OS 家族: %v", err))
	}
}

// errIsUnsupported 判定 err 是否是 ErrUnsupportedOSFamily。
func errIsUnsupported(err error) bool {
	return errors.Is(err, ErrUnsupportedOSFamily)
}

// fail 把制品集置为 failed 并记录原因。只调用 UpdateArtifactSetState
// （不调用 SetArtifactSetContent）——失败时已写入的路径不被清空（任务 11
// 明确要求：这两个方法分工，失败路径只改状态，不触及 Paths）。
//
// 具体的失败原因文本目前只能通过本方法的返回 error 向上传播（调用方，
// 即控制台 handler，负责决定这段文本最终展示在哪里）——manifest_sha256
// 字段被复用为「空字符串」，不塞入错误原因文本：该字段的语义是清单校验值，
// 塞入不相关的错误文本会让将来某次「校验完整性」端点意外读到一段错误
// 消息当作 sha256 使用（详见本文件末尾的契约缺口说明）。
func (j *Job) fail(ctx context.Context, artifactSetID, reason string) error {
	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetFailed, ""); err != nil {
		return fmt.Errorf("imports: 导入失败（原因: %s），且写回 failed 状态本身也失败: %w", reason, err)
	}
	return fmt.Errorf("imports: 导入失败: %s", reason)
}

// 编译期断言：本包定义的三个抽取相关类型满足其对应接口（与 provider 包
// 保持同样的自检习惯，虽然本文件用到的是内部窄接口，没有导出的接口
// 断言目标，这里留一个空块位置提示读者：Extractor/artifactSetUpdater
// 两个接口的实现者分别是 MountExtractor（extract.go）与真实 Store，
// 不需要在这里重复断言）。

// 契约缺口：model.ArtifactSet 没有专门的「导入失败原因」字段
// （见 internal/core/model/inventory.go 的 ArtifactSet 结构体：只有
// State/ManifestSHA256/Paths 三项，没有类似 Task.FailureDetail 的字段），
// contract.ArtifactSetStore.UpdateArtifactSetState 的签名也只接受
// (state, manifestSHA256) 两个参数，没有第三个「原因」参数可传。计划
// 任务 11 要求「失败时要把原因写进制品集的错误字段，供 UI 展示」，但
// M1 没有定义这个字段。
//
// 保守处理：本文件不擅自复用 ManifestSHA256 字段承载错误文本（那会让
// 「校验完整性」端点读到一段错误消息当作 sha256 值使用，是错误值污染
// 契约字段的反面教材）。改为让 fail 方法的返回 error 携带完整原因文本，
// 由调用方（控制台 handler，M6/M8）决定原因文本的展示位置——若 M6 需要
// 把这段文本持久化以便 UI 刷新后仍能看到，那需要 M1 给 ArtifactSet 或
// ArtifactSetStore 补一个字段/参数，这正是本契约缺口的正解方向。
