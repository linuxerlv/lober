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

// artifactSetUpdater 是 Job 依赖的最窄写回接口：只需要
// contract.ArtifactSetStore 的两个写方法（状态更新与内容写回），不需要
// 完整接口的 Create/Get/List/Delete——窄依赖让测试替身只实现这两个方法。
//
// 状态更新与内容写回分成两个方法调用（对应 contract.ArtifactSetStore 的
// 设计）：导入失败时只调 UpdateState，不调 SetContent——失败时 Paths
// 可能尚未写入或只写了一半，绝不能被状态更新覆盖成半成品（任务 11：
// 失败时已写入的路径不被清空）。
//
// UpdateArtifactSetState 的第五个参数（reason）是本次契约变更新增
// （见 contract.ArtifactSetStore 的文档与 t01 记录的契约缺口）：本文件
// 的 fail 方法藉此把具体失败原因写回持久化层，供控制台展示给运维——
// 这是「让导入失败的原因能被运维看到」这个任务的最后一步（改接口 → 改
// conformance 断言 → 改内存实现 → 改 SQLite 实现 → 接上调用方，本接口
// 与下面的 fail 方法就是第五步）。
type artifactSetUpdater interface {
	UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error)
	SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error)
}

// Job 是单次 ISO 导入的执行体。
//
// 并发控制：同一时刻只允许一个导入 job 运行（任务 11：解压是 IO 密集的，
// 并发两个只会让两个都变慢；运维一次导入一个 ISO 是常态）。用带缓冲为 1
// 的信号量实现，第二个请求排队而不是拒绝——Job 的每个方法调用都会获取
// 同一个包级信号量，跨多个 Job 实例共享（否则「同一时刻只允许一个」这条
// 约束就只在单个 Job 实例内成立，两个不同 Job 实例仍能并发跑，与设计
// 要点矛盾）。
type Job struct {
	store     artifactSetUpdater
	extractor Extractor
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

	// reason 传空字符串：成功路径没有失败原因，且这一步顺带清空了此前
	// 若有过失败重试留下的旧 FailReason——制品集导入成功后，运维不应再
	// 在控制台看到一条属于上一次失败尝试的陈旧原因文本。
	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, manifest.SHA256, ""); err != nil {
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

	// reason 传空字符串，理由与 Run 方法同一处调用点相同：成功路径没有
	// 失败原因，顺带清空上一次失败尝试留下的陈旧 FailReason。
	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetReady, manifest.SHA256, ""); err != nil {
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
// reason 现在同时经两条路径向上传播（本次契约变更前只有下面这一条）：
//  1. 持久化进制品集的 FailReason 字段（经 UpdateArtifactSetState 的第五
//     个参数），供控制台 GET /artifact-sets 读出、供运维在页面上直接看到
//     ——这是本次契约变更新增的路径，解决 t01 记录的缺口（导入失败原因
//     此前无处承载，运维只能看到一个没有信息量的 "failed"）。
//  2. 本方法的返回 error（调用方目前是 cmd/lober/importrunner.go，写进
//     slog 供运维查日志兜底）——继续保留，双路径互为补充，日志路径不因为
//     有了持久化路径就删除：进程重启后 slog 历史仍可能比数据库记录更早
//     被运维发现。
//
// manifest_sha256 字段不复用为承载原因文本：该字段的语义是清单校验值，
// 塞入不相关的错误文本会让将来某次「校验完整性」端点意外读到一段错误
// 消息当作 sha256 使用——这条约束在新增 FailReason 字段后依然成立，
// FailReason 有了专属位置，manifest_sha256 更没有理由再被挪用。
func (j *Job) fail(ctx context.Context, artifactSetID, reason string) error {
	if _, err := j.store.UpdateArtifactSetState(ctx, artifactSetID, model.ArtifactSetFailed, "", reason); err != nil {
		return fmt.Errorf("imports: 导入失败（原因: %s），且写回 failed 状态本身也失败: %w", reason, err)
	}
	return fmt.Errorf("imports: 导入失败: %s", reason)
}

// 编译期断言：本包定义的三个抽取相关类型满足其对应接口（与 provider 包
// 保持同样的自检习惯，虽然本文件用到的是内部窄接口，没有导出的接口
// 断言目标，这里留一个空块位置提示读者：Extractor/artifactSetUpdater
// 两个接口的实现者分别是 MountExtractor（extract.go）与真实 Store，
// 不需要在这里重复断言）。
