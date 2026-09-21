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
type artifactSetUpdater interface {
	UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256 string) (model.ArtifactSet, error)
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

// RunRHEL 执行一次完整的 RHEL 系导入流程（任务 11 的 job 执行流程）：
//  1. 打开抽取器得到文件系统视图
//  2. 识别 OS 家族；不支持则置失败
//  3. 按家族抽取（当前只接了 RHEL 一种，其它家族的抽取逻辑不在本模块的
//     计划任务范围内，未来新增家族时在 identify 分支后加对应的 ExtractXxx
//     调用，不改变本方法的整体结构）
//  4. 生成 manifest
//  5. 写回家族与路径
//  6. 置 ready 并记录 manifest 校验值
//
// 任何一步失败都置 failed 并写具体原因，不继续后续步骤（任务 11）。
//
// isoPath 是登记时运维填写的本地路径（气隙环境下以「登记服务器本地路径」
// 为主，SPEC §2.1），destDir 是制品集在磁盘上的根目录。
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
