// 本文件是导入 job 的生产装配与异步执行：把 internal/imports 的 Job 接到
// console.ArtifactImporter 这个窄接口上，供 buildAppWithDB 注入
// console.Deps.ArtifactImporter。对应「把 ISO 导入管线真正接上」这一轮的
// 核心任务，依据 SPEC §2.1、§11 Phase 2、§12.6。
//
// cmd/lober 是全仓唯一允许同时 import internal/console 与
// internal/imports 的包（装配点豁免，见 wire.go 文件头注释），本文件正是
// 这条豁免存在的理由：console 包本身绝不能 import internal/imports
// （那会被 archtest 的依赖方向守卫拦住），但导入触发的具体实现必须调用
// internal/imports 的类型，因此只能放在装配点，实现 console 包内定义的
// 窄接口后再注入回去。
package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/imports"
)

// importRunner 是 console.ArtifactImporter 的生产实现：每次 TriggerImport
// 调用另起一个 goroutine 执行 imports.Job.Run，方法本身立即返回——
// 与 console.ArtifactImporter 的接口文档要求的「异步触发，立即返回」
// 语义一致，不能阻塞 HTTP handler。
type importRunner struct {
	store contract.ArtifactSetStore

	// extractor 通常是 imports.NewMountExtractor 的实例；测试注入可控假
	// 实现，不依赖真实 mount(8) 调用。
	extractor imports.Extractor

	// artifactRoot 是制品根目录（cfg.Store.ArtifactPath），与
	// assemblyPrechecker.artifactRoot（wire.go）取同一个配置值——两处
	// 都需要把「制品根目录」与「具体某个制品集的 ID」拼成该制品集自己的
	// 子目录，只是拼接的时机不同（precheck 在引导请求到达时拼、本类型
	// 在导入触发时拼）。
	artifactRoot string

	// windowsSourcesBase 是 cfg.Store.WindowsSourcesPath（本轮 m2 轨新增）：
	// 每次 TriggerImport 构造 Job 时，都会用这个值构造一个
	// fileWindowsSourcesResolver 并调用 job.ConfigureWindows——解除
	// internal/imports/job.go runWindows「尚未调用 ConfigureWindows」这个
	// MVP 封锁错误（见该文件的契约缺口说明）。
	//
	// 可能是空字符串（未配置 store.windows_sources_path，config.go 允许
	// 这种情况——不是每个部署都导入 Windows）：这不会导致
	// TriggerImport 本身报错或 panic，空字符串会被原样传给
	// fileWindowsSourcesResolver，真正的报错发生在 ResolveWindowsSources
	// 被调用的那一刻（只有实际识别到 Windows 家族时才会触发），与
	// job.go「不静默成功、不 panic，明确报错」的一贯设计一致——本类型
	// 不提前用一个「必填」校验重复拦这条路径（那是 config.Validate 的
	// 职责边界，见 config.go 的 Store.WindowsSourcesPath 字段文档：
	// 该字段本身是可选的，只有真正为空又被调用时才报错）。
	windowsSourcesBase string
}

// newImportRunner 构造一个 importRunner。extractor 为 nil 时兜底为
// imports.NewMountExtractor("")（生产环境的真实挂载式抽取器；该实现
// 目前对 Open 调用返回「未实现真实挂载逻辑」的错误——见
// internal/imports/extract.go 的 MountExtractor 文档，这是部署环境适配
// 阶段留下的已知缺口，不是本文件引入的新问题，本构造函数不试图掩盖它）。
//
// windowsSourcesBase 对应 cfg.Store.WindowsSourcesPath（wire.go 的装配点
// 负责传入），供每次 TriggerImport 构造 Job 时接上 Windows 的两个依赖
// （见 windowsSourcesBase 字段文档）。
func newImportRunner(store contract.ArtifactSetStore, extractor imports.Extractor, artifactRoot, windowsSourcesBase string) *importRunner {
	if extractor == nil {
		extractor = imports.NewMountExtractor("")
	}
	return &importRunner{
		store:              store,
		extractor:          extractor,
		artifactRoot:       artifactRoot,
		windowsSourcesBase: windowsSourcesBase,
	}
}

// TriggerImport 实现 console.ArtifactImporter。
//
// destDir 按 filepath.Join(artifactRoot, artifactSetID) 拼出（与
// assemblyPrechecker.Check 拼接制品集子目录的方式一致，docs/contracts/
// filesystem.md 硬约束 1：ArtifactSet.Paths 里存的相对路径是相对于该
// 制品集自己的根目录 /srv/lober/artifacts/<artifact_set_id>/）。
//
// ctx 由调用方（console 的 handler）传入时已经用 context.WithoutCancel
// 剥离了 HTTP 请求生命周期的取消信号（见 internal/console/artifact.go
// 的调用点）——本方法自己不重复剥离，若调用方传入一个会话请求结束就取消
// 的 ctx，goroutine 中途看到 ctx.Done() 只会影响 imports.Extractor.Open
// 与其内部实现是否读取 ctx（Job.Run 当前的实现不会主动检查 ctx 取消，
// 抽取过程本身也不是可中断的，这与「进程退出时在途导入会中断」是同一类
// 已知限制，记入 notes）。
func (r *importRunner) TriggerImport(ctx context.Context, artifactSetID, isoPath string) {
	destDir := filepath.Join(r.artifactRoot, artifactSetID)
	job := imports.NewJob(r.store, r.extractor)

	// 接上 Windows 导入所需的两个依赖（本轮 m2 轨新增，解除
	// internal/imports/job.go runWindows 的 MVP 封锁）：resolver 按目录
	// 约定找 wimboot/BCD/boot.sdi/boot.wim 四个外部文件
	// （winsources.go），detector 信任 artifact_set 登记时声明的固件
	// （wimarch.go，见该文件头注释的契约缺口说明）。RHEL/Debian/Ubuntu/
	// SUSE/ESXi 五个已支持家族的导入路径完全不读这两个依赖（job.go
	// 的 Run 只在识别为 windows 时才用到它们），因此对每次导入无条件
	// 调用 ConfigureWindows 不会影响其它家族的既有行为——这与
	// ConfigureWindows 本身"可选注入，未调用时才报错"的设计并不矛盾，
	// 生产装配点选择"总是配置好，让 Job 自己决定用不用"，而不是"按
	// 家族条件性配置"，因为 TriggerImport 在这一刻还不知道 isoPath
	// 对应的 ISO 是什么家族（家族要等 job.Run 内部 Identify 之后才知道）。
	resolver := newFileWindowsSourcesResolver(r.windowsSourcesBase)
	detector := newStoreBackedWimArchDetector(r.store)
	job.ConfigureWindows(resolver, detector)

	go func() {
		if err := job.Run(ctx, artifactSetID, isoPath, destDir); err != nil {
			// 错误已经被 Job.Run 自己写回为制品集的 failed 状态（见
			// internal/imports/job.go 的 fail 方法），这里只记日志供运维
			// 排障——不重复写状态，也不吞掉这条信息（否则「为什么这个
			// 制品集失败了」只能靠翻 GET /artifact-sets 的 state 猜，
			// 没有具体原因文本可查，见 job.go 文件末尾的契约缺口说明）。
			slog.Error("导入 job 执行失败",
				slog.String("artifact_set_id", artifactSetID),
				slog.String("iso_path", isoPath),
				slog.Any("error", err))
			return
		}
		slog.Info("导入 job 执行成功",
			slog.String("artifact_set_id", artifactSetID),
			slog.String("iso_path", isoPath))
	}()
}

var _ console.ArtifactImporter = (*importRunner)(nil)
