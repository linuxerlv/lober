// 本文件是「重算 manifest 校验完整性」的生产装配与异步执行：把
// internal/imports.GenerateManifest 接到 console.ArtifactVerifier 这个
// 窄接口上，供 buildAppWithDB 注入 console.Deps.ArtifactVerifier。对应
// 本轮任务「让 POST /artifact-sets/{id}/verify 真的重算 manifest 校验
// 完整性」，依据 SPEC §8.2、§10 运维点一、docs/contracts/http-console.md。
//
// cmd/lober 是全仓唯一允许同时 import internal/console 与
// internal/imports 的包（装配点豁免，见 wire.go 文件头注释与
// importrunner.go 同一段说明）：console 包本身绝不能 import
// internal/imports（会被 archtest 的依赖方向守卫拦住），但 manifest 的
// 重算算法必须与导入时完全一致（否则任何一次导入产出的 manifest 都会在
// 校验时被误判为不匹配），因此生产实现只能放在装配点，调用
// imports.GenerateManifest 后再实现 console 包内定义的窄接口注入回去。
package main

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/console"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/imports"
)

// artifactVerifierStore 是 artifactVerifier 依赖的最窄接口：只需要读一次
// 制品集（拿到已登记的 ManifestSHA256 与 State 作比对基准）、写一次状态
// （校验通过后刷新 ManifestSHA256）。不用完整的 contract.ArtifactSetStore
// ——测试用假实现时，没有理由被迫多实现 CreateArtifactSet/
// SetArtifactSetContent/DeleteArtifactSet 四个本类型完全不需要的方法。
type artifactVerifierStore interface {
	GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error)
	UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error)
}

// artifactVerifier 是 console.ArtifactVerifier 的生产实现：每次
// TriggerVerify 调用另起一个 goroutine 执行实际的重算与比对，方法本身
// 立即返回——与 console.ArtifactVerifier 的接口文档要求的「异步触发，
// 立即返回」语义一致，不能阻塞 HTTP handler（校验要重算整棵制品树的
// sha256，可能是几 GB 文件，同步跑完会让 HTTP 请求挂起到算完才返回）。
type artifactVerifier struct {
	store artifactVerifierStore

	// artifactRoot 是制品根目录（cfg.Store.ArtifactPath），与
	// importRunner.artifactRoot、assemblyPrechecker.artifactRoot 取同一个
	// 配置值——三处都需要把「制品根目录」与「具体某个制品集的 ID」拼成
	// 该制品集自己的子目录，只是拼接的时机不同。
	artifactRoot string
}

// newArtifactVerifier 构造一个 artifactVerifier。
func newArtifactVerifier(store artifactVerifierStore, artifactRoot string) *artifactVerifier {
	return &artifactVerifier{store: store, artifactRoot: artifactRoot}
}

// TriggerVerify 实现 console.ArtifactVerifier。
//
// ctx 由调用方（console 的 handler）传入时已经用 context.WithoutCancel
// 剥离了 HTTP 请求生命周期的取消信号（见 internal/console/artifact.go 的
// 调用点）——本方法自己不重复剥离。
func (v *artifactVerifier) TriggerVerify(ctx context.Context, artifactSetID string) {
	go v.runVerify(ctx, artifactSetID)
}

// runVerify 是 TriggerVerify 派生 goroutine 里真正执行的校验逻辑，拆成
// 独立方法便于测试直接同步调用（测试不需要真的并发，只需要验证「给定
// 制品树的状态，校验结果应该是什么」这条纯逻辑）。
//
// 两层校验，与 internal/statemachine/precheck.go 的 Machine.Check 第三项
// （制品完整性）、cmd/lober/preflight.go 的启动自检同一套两层设计——两者
// 分别检测不同类的问题，缺一层都会漏掉一整类真实故障：
//
//  1. 存在性校验（复用 preflight.go 的 artifactCheckPaths/
//     checkArtifactPathExists）：按 ArtifactPaths 逐条核对文件是否真实
//     在盘上——这一层能报出「具体哪个路径缺失」（任务要求：文件缺失时
//     校验失败且错误指出是哪个路径）。单纯走目录树 walk 生成 manifest
//     做不到这一点：walk 只能看到盘上「存在」的文件，一个按 ArtifactPaths
//     应该存在却已被误删的文件，会被 walk 直接跳过、不会在遍历结果里
//     留下任何痕迹指出「缺了谁」，只会让整体 sha256 算出来对不上、报出
//     一个笼统的「manifest 不匹配」——运维还要再反查一遍才知道缺了哪个
//     文件。
//  2. 内容校验（imports.GenerateManifest + 与已登记 ManifestSHA256 比对）：
//     单纯的存在性检查会漏掉「文件还在但内容被截断/篡改/覆盖」这一类
//     问题（存在性检查只 fs.Stat，不读内容）——这正是 manifest sha256
//     要覆盖的场景，也是 SPEC §8.2 把这个端点定义为「重算 sha256」而不是
//     「检查文件是否存在」的原因。
//
// 校验失败时的状态处理（不改 State，只记日志）：见本文件末尾的设计决策
// 说明。
func (v *artifactVerifier) runVerify(ctx context.Context, artifactSetID string) {
	set, err := v.store.GetArtifactSet(ctx, artifactSetID)
	if err != nil {
		// 调用方（console handler）在触发前已经查过一次「制品集存在」，
		// 这里理论上不会命中——除非在 handler 查过之后、goroutine 真正
		// 跑起来之前，制品集被另一个请求删除（罕见但不是不可能的竞态）。
		// 命中时只能记日志，没有制品集可以把结果写回哪里。
		slog.Error("校验制品集完整性失败：goroutine 启动后查不到该制品集",
			slog.String("artifact_set_id", artifactSetID), slog.Any("error", err))
		return
	}

	destDir := filepath.Join(v.artifactRoot, artifactSetID)

	// 第一层：存在性校验，复用 preflight.go 已经实现过的同一套逻辑
	// （artifactCheckPaths 计算待检查路径列表，checkArtifactPathExists
	// 逐条核对），不重新发明一遍。
	for _, p := range artifactCheckPaths(artifactSetID, set.Paths) {
		if p.relPath == "" {
			continue
		}
		if checkedPath, statErr := checkArtifactPathExistsUnderRoot(v.artifactRoot, p.diskPath); statErr != nil {
			slog.Error("校验制品集完整性失败：文件缺失",
				slog.String("artifact_set_id", artifactSetID),
				slog.String("artifact_set_name", set.Name),
				slog.String("missing_path", checkedPath),
				slog.Any("error", statErr),
			)
			return
		}
	}

	// 第二层：内容校验。算法必须与导入时用的 imports.GenerateManifest
	// 完全一致——这正是本文件必须放在装配点（能 import internal/imports）
	// 而不能放在 console 包内的理由。
	manifest, err := imports.GenerateManifest(destDir)
	if err != nil {
		slog.Error("校验制品集完整性失败：重算 manifest 本身出错",
			slog.String("artifact_set_id", artifactSetID),
			slog.String("artifact_set_name", set.Name),
			slog.Any("error", err),
		)
		return
	}

	if manifest.SHA256 != set.ManifestSHA256 {
		slog.Error("校验制品集完整性失败：manifest 校验值不匹配（文件内容已改变）",
			slog.String("artifact_set_id", artifactSetID),
			slog.String("artifact_set_name", set.Name),
			slog.String("expected_manifest_sha256", set.ManifestSHA256),
			slog.String("actual_manifest_sha256", manifest.SHA256),
		)
		return
	}

	// 校验通过：刷新 ManifestSHA256（此刻应与已登记值相等，写回是幂等
	// 确认，不是改变），State 原样传回、不做任何变更——
	// UpdateArtifactSetState 的签名要求同时给出两个字段，没有「只改一个,
	// 另一个保持原值」的重载，因此显式传回 set.State 而不是某个新值。
	if _, err := v.store.UpdateArtifactSetState(ctx, artifactSetID, set.State, manifest.SHA256, ""); err != nil {
		slog.Error("校验通过但写回确认结果失败",
			slog.String("artifact_set_id", artifactSetID), slog.Any("error", err))
		return
	}
	slog.Info("校验制品集完整性通过",
		slog.String("artifact_set_id", artifactSetID),
		slog.String("manifest_sha256", manifest.SHA256),
	)
}

// checkArtifactPathExistsUnderRoot 是 preflight.go 的
// checkArtifactPathExists 的薄包装：把「制品根目录」与「制品集内相对
// 路径」拼成 os.DirFS(artifactRoot) 视角下的路径后，委托给同一个存在性
// 判定函数——不重复实现「路径以斜杠结尾则视为目录、改查
// repodata/repomd.xml」这条判定规则，preflight.go 已经写过一次。
func checkArtifactPathExistsUnderRoot(artifactRoot, diskPath string) (checkedPath string, err error) {
	return checkArtifactPathExists(os.DirFS(artifactRoot), diskPath)
}

var _ console.ArtifactVerifier = (*artifactVerifier)(nil)

// 编译期确认 contract.ArtifactSetStore 是 artifactVerifierStore 的超集
// （装配点传入的是 *postgres.Store，天然满足更宽的 contract.ArtifactSetStore
// 接口，这里只是留一个可读的静态断言，说明「窄接口」这个设计选择不会在
// 装配点造成类型不兼容）。
var _ artifactVerifierStore = (contract.ArtifactSetStore)(nil)

// ---------------------------------------------------------------------------
// 设计决策：校验失败时为什么不把 State 置为 failed
// ---------------------------------------------------------------------------
//
// 任务要求「校验失败时的状态处理要想清楚：把制品集置 failed？还是只报告
// 不改状态？考虑一个场景：制品集正被一个非终态任务 pin 着（§2.6），此时
// 校验失败说明那台机器接下来会取不到文件」——本实现选择的是后者：只报告
// （记结构化日志），不改 State。理由分三层：
//
//  1. 置为 failed 不会给「正被 pin 住的任务」提供任何额外保护。
//     实际的引导时防线是 internal/statemachine/precheck.go 的
//     Machine.Check（§4.6 三项硬拒绝之一），它在每次 /v1/boot 命中时
//     独立重新核对文件是否存在——已确认该函数完全不读 artifact.State
//     这个字段（只用 actualArch/actualFirmware/artifact.Paths 三项输入），
//     internal/gateway/rendercontext.go 的 buildRenderContext 取制品集时
//     同样不按 State 过滤。也就是说，即使本次校验把 State 改成 failed，
//     一台正在 booting 阶段、马上要请求 /v1/answer 或制品文件的机器，
//     并不会因为这个状态变化被多拦下来——它能不能取到文件，本来就已经
//     由 precheck 在它自己发请求的那一刻独立判定过。
//  2. 置为 failed 有明确的副作用，但收益不成比例。
//     model.ArtifactSetFailed 目前唯一被下游读取的地方是
//     cmd/lober/preflight.go 的启动自检（只检查 ready 状态的制品集）——
//     把一个原本 ready 的制品集改成 failed，会让它从下次启动自检的检查
//     范围里消失，这与「本端点的目的是让运维发现问题」这一初衷正好相反：
//     问题被校验发现后，本该更显眼，而不是从下一轮自动检查里退出视野。
//  3. 校验本身可能有噪音，不该单方面拥有改变生命周期状态的权力。
//     SPEC §8.2 把这个端点的职责写成「重算 sha256 校验完整性」——动词是
//     「校验」，不是「判定」或「处置」。对照 internal/imports/job.go 的
//     Job.fail：那里把制品集置为 failed 是合理的，因为那一刻是「第一次
//     确立」这个制品集的状态（导入还没成功过，本来就不是 ready）。而
//     verify 端点校验的对象通常已经是 ready、已经被使用过的制品集——把
//     一次异步、可能受瞬时 I/O 错误影响的重算结果，直接拿来覆盖一个已经
//     在生产使用的状态字段，是本实现认为超出「校验并报告」这个端点职责
//     的动作。
//
// 因此本实现在存在性校验失败、内容校验失败、manifest 重算本身出错这三种
// 失败路径上，都只调用 slog.Error 记录完整上下文（artifact_set_id、
// artifact_set_name、具体缺失路径或 sha256 差异），不调用
// UpdateArtifactSetState。
//
// 契约缺口（详见任务返回值 contractGaps，此处只记落地方式）：
// model.ArtifactSet 没有专门的「最近一次校验结果/时间」字段（结构体只有
// State/ManifestSHA256/Paths 三项，与 internal/imports/job.go 文件末尾
// 记录的「没有导入失败原因字段」是同一类缺口），
// contract.ArtifactSetStore 也没有能表达「记录一次校验尝试，但不改变
// State」的方法。这意味着校验失败的结果目前只能在服务端日志里看到，
// GET /artifact-sets 返回的字段不会体现「上次校验失败」——运维必须去翻
// 日志，而不能仅从控制台 UI 上看出「这个制品集校验失败过」。正解方向：
// 给 ArtifactSet 补一个类似 Task.FailureDetail 的
// LastVerifiedAt/LastVerifyError 字段，UpdateArtifactSetState 或新增的
// 专用方法据此写回，不复用 ManifestSHA256/State 这两个已有明确语义的
// 字段。
