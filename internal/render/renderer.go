// 本文件实现 contract.Renderer 的入口分派：两个方法各自的家族分流与端到端
// 注入防护的最后一道校验（对应实现计划任务 8，依据 SPEC §5.1、§5.6）。
//
// 渲染器整体是纯函数——不查库、不碰时钟、不做 IO（SPEC §5.1），这是控制台
// preview 端点能用假数据跑的前提，也是 golden 测试的前提。
package render

import (
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// Renderer 实现 contract.Renderer。不持有任何状态——两个方法都是纯函数，
// 零值 *Renderer 即可直接使用（调用方不需要构造函数）。
type Renderer struct{}

// RenderMenu 按家族分派渲染 iPXE 菜单（contract.Renderer 接口方法）。
//
// 实际的家族分流逻辑在 menu.go 的包级函数 RenderMenu 中——本方法只是把
// 接口调用转发过去，保持 renderer.go 作为"入口分派"这一层的职责单一：
// 它决定"该调用哪个包级函数"，不重复任何家族分支逻辑。
func (r *Renderer) RenderMenu(ctx model.RenderContext) (string, error) {
	return RenderMenu(ctx)
}

// RenderAnswerFile 校验文件名后按家族分派渲染应答文件（contract.Renderer
// 接口方法）。
//
// 校验顺序（计划任务 8"两个入口的职责"）：
//  1. 先校验传入的 fileName 等于 ctx.AnswerFileName() 算出的文件名——
//     该参数来自机器请求的 URL 路径段，不能用它去选模板，否则机器可以
//     通过改文件名让服务端渲染另一种格式的应答文件。Ubuntu 例外：额外
//     接受 "meta-data"（NoCloud 要求该目录下两个文件都存在）。ESXi
//     例外：额外接受 "ks.cfg"（boot.cfg 的 ks= 指向的独立 kickstart
//     方言正文，与 meta-data 同构）。
//  2. 文件名通过后，再按家族分派到具体渲染逻辑。
func (r *Renderer) RenderAnswerFile(ctx model.RenderContext, fileName string) (string, error) {
	if err := validateAnswerFileName(ctx, fileName); err != nil {
		return "", err
	}

	switch ctx.Artifact.OSFamily {
	case model.OSFamilyRHEL:
		return AssembleKickstart(ctx)
	case model.OSFamilyDebian:
		return AssemblePreseed(ctx)
	case model.OSFamilyUbuntu:
		// meta-data 是 validateAnswerFileName 额外放行的第二个文件名
		// （NoCloud 要求该目录下 user-data 与 meta-data 都存在），必须在
		// 这里单独分派到 RenderMetaData——若不判断 fileName，请求
		// meta-data 时会被误当作 user-data 走 AssembleAutoinstall，产出
		// 内容错误但格式仍合法的 YAML，cloud-init 会因为缺少真正的
		// meta-data 内容（实例 id/主机名）而在后续阶段行为异常，且这类
		// 错误不会在渲染层报错——必须在分派处就分清楚两个文件名各自对应
		// 哪个渲染函数。
		if fileName == "meta-data" {
			return RenderMetaData(ctx)
		}
		return AssembleAutoinstall(ctx)
	case model.OSFamilyWindows:
		return RenderUnattend(ctx)
	case model.OSFamilyESXi:
		// fileName == "ks.cfg" 是本次改动新增的第二候选文件名（与 Ubuntu
		// 的 "meta-data" 同构，见 validateAnswerFileName 与
		// gateway/answer.go 的 answerFileNameAllowed）：ESXi 的
		// kernelopt= ks= 指向的正是这份 kickstart 方言正文，
		// AssembleESXiKickstart 是它的正确渲染实现，且是不需要额外磁盘
		// 输入的纯函数，与本方法"零 IO"的约束天然兼容——这条路径是
		// 完全正确的，不是权宜之计。
		//
		// fileName == "boot.cfg"（即 ctx.AnswerFileName() 本来的返回值）
		// 仍然分派到 AssembleESXiKickstart，这一行为**没有变**，且仍然是
		// 错的（见 esxi.go 文件末尾"契约缺口 1"）：真实 boot.cfg 应该调用
		// RenderESXiBootCfg 改写导入时保存的原始引导参数文件模板三处
		// （prefix=/kernelopt= 的 ks=/modules= 去前导斜杠），但那份模板
		// 原文不在本方法能拿到的任何输入里——
		// ArtifactPaths.Extra["boot.cfg"]（RenderContext.Artifact.Paths）
		// 只是磁盘相对路径字符串，不是文件内容（见
		// imports/extract_esxi.go 的 `extra[rel] = rel`），调用
		// RenderESXiBootCfg 需要先读盘，而本方法的签名是
		// (RenderContext, string) (string, error)、渲染器包"零 IO、纯
		// 函数"是文件顶部注释明确的设计前提（preview 端点靠这个前提才能
		// 用假数据跑，golden 测试也靠这个前提），不能在这里悄悄插入一次
		// 磁盘读取去打破它。
		//
		// 本次改动排查过把这份读盘能力接到更外层（gateway 层，它本来就
		// 做别的 IO）的可能性，发现同样接不上，且比 esxi.go 原先记录的
		// 更深一层：gateway.Deps（server.go，本次改动文件归属不含它）
		// 目前没有任何"制品文件系统根目录"字段——它只有
		// Bases.ArtifactBase（给机器用的 HTTP 基址）与一组 Store 接口
		// （查数据库，不读文件）。真正的制品根目录
		// cfg.Store.ArtifactPath（internal/config/config.go）在
		// cmd/lober/wire.go 里确实存在并已被读取，但只传给了
		// importrunner/assemblyPrechecker 等其它装配点，从未传进
		// gateway.Deps{...}（wire.go 该处的构造字面量看得很清楚：
		// TaskStore/FactStore/.../RootSSHPublicKeys 逐一列出，没有任何
		// 制品根目录字段）。要打通这条链路，必须同时改
		// gateway/server.go（新增 Deps 字段）与 cmd/lober/wire.go
		// （把 cfg.Store.ArtifactPath 传进去）——两个文件都不在本次改动
		// 的文件归属内，且后者还是装配点级别的改动，不是"在已有文件里加
		// 新分支"这种增量。完整记录在本文件末尾 contractGaps，供编排方
		// 接线参考。
		return AssembleESXiKickstart(ctx)
	case model.OSFamilySUSE:
		// AutoYaST 的渲染实现（AssembleAutoYaST）在本 worktree 内不存在
		// （已检查 internal/render/ 下所有文件），没有实现可接。SUSE 与
		// kickstart 分支的隔离仍然成立：它落"未实现"分支，绝不会被送进
		// AssembleKickstart——那是 RHEL 专属的 case，SUSE 不会匹配到它。
		return "", unimplementedFamilyError(ctx.Artifact.OSFamily)
	default:
		return "", fmt.Errorf("render: 未知的 OS 家族 %q，无法渲染应答文件", ctx.Artifact.OSFamily)
	}
}

// validateAnswerFileName 校验 fileName 是否是该任务允许的应答文件名。
//
// Ubuntu 家族额外接受 "meta-data"：NoCloud 数据源（ds=nocloud-net）要求
// 目录下同时存在 user-data 与 meta-data 两个文件，cloud-init 会依次取两者
// （SPEC §4.5）。
//
// ESXi 家族额外接受 "ks.cfg"：与 Ubuntu 的 meta-data 同构的放行——
// boot.cfg 的 kernelopt= 行里 ks= 参数指向的正是这份独立的 kickstart
// 方言正文（ctx.AnswerFileName() 固定返回 "boot.cfg"，是 mboot.efi 的
// 引导参数文件本身，两者从来不是同一个文件，见 esxi.go 文件头注释）。
// 放行同样窄：只对 ESXi、只多放行 "ks.cfg" 这一个名字。
//
// 其它家族只接受 ctx.AnswerFileName() 算出的唯一文件名。
func validateAnswerFileName(ctx model.RenderContext, fileName string) error {
	want, ok := ctx.AnswerFileName()
	if !ok {
		return fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	if fileName == want {
		return nil
	}
	if ctx.Artifact.OSFamily == model.OSFamilyUbuntu && fileName == "meta-data" {
		return nil
	}
	if ctx.Artifact.OSFamily == model.OSFamilyESXi && fileName == "ks.cfg" {
		return nil
	}
	return fmt.Errorf("render: 请求的应答文件名 %q 与该任务应有的文件名 %q 不一致"+
		"——文件名来自机器请求的 URL 路径段，不能用它选择渲染哪种格式的应答文件，"+
		"否则机器可以通过改路径段拿到另一种模板内容", fileName, want)
}

// 编译期接口断言（计划任务 8："编译期接口断言放文件末尾"）。
var _ contract.Renderer = (*Renderer)(nil)

// 契约缺口：真实 boot.cfg 三处改写（RenderESXiBootCfg）目前没有任何调用点
// 会执行，比 esxi.go 文件末尾"契约缺口 1"记录的更深一层。
//
// esxi.go 那条记录假设的正解方向是"renderer.go 的 RenderAnswerFile 按
// fileName 分别调用两个函数"——本次改动已经做了这一半（fileName == "ks.cfg"
// 时正确调用 AssembleESXiKickstart），但 fileName == "boot.cfg" 时无法照
// 同样的模式调用 RenderESXiBootCfg，因为它需要"原始 boot.cfg 模板文本"
// 作为输入，而这份文本在渲染器能拿到的任何输入里都不存在：
//
//   - model.RenderContext.Artifact.Paths.Extra["boot.cfg"]
//     （internal/core/model/inventory.go，冻结）是磁盘相对路径字符串
//     （值就是 "boot.cfg" 本身），不是文件内容——imports/extract_esxi.go
//     的 ExtractESXi 写入时就是 `extra[rel] = rel`，只读那个文件确认过，
//     没有改它。
//   - 渲染器包被文件顶部注释明确要求"零 IO、纯函数"（preview 端点用假
//     数据跑、golden 测试都依赖这个前提），不能在 RenderAnswerFile 内部
//     插入一次磁盘读取去满足 RenderESXiBootCfg 的输入需求。
//   - 把读盘能力挪到外层 gateway（它本来就做别的 IO，answer.go 是本次
//     改动确实拥有的文件）看起来是更合理的落点，但排查后发现同样接不
//     上：gateway.Deps（internal/gateway/server.go，本次改动文件归属
//     不含它）目前没有任何"制品文件系统根目录"字段，只有
//     Bases.ArtifactBase（HTTP 基址，给机器用，不是本地磁盘路径）与一组
//     查数据库的 Store 接口。真正的制品根目录
//     cfg.Store.ArtifactPath（internal/config/config.go）在
//     cmd/lober/wire.go 里确实存在、确实被读取，但只传给了
//     importrunner/assemblyPrechecker 等其它装配点（wire.go 第 616、670
//     行等），从未传进 gateway.Deps{...}（wire.go 第 568 行那处构造
//     字面量逐字段列出，没有制品根目录）。
//
// 正解需要同时改两个不在本次改动文件归属内的文件：
//  1. internal/gateway/server.go：给 Deps 加一个字段（例如
//     ArtifactRoot string），语义与 cfg.Store.ArtifactPath 一致。
//  2. cmd/lober/wire.go：在构造 gateway.Deps{...} 时把
//     cfg.Store.ArtifactPath 传进新字段（第 568 行附近，与
//     RootSSHPublicKeys 同一处字面量）。
//
// 有了这两处改动之后，internal/gateway/answer.go 的 handleAnswer 才能在
// fileName == "boot.cfg" 时：用 filepath.Join(deps.ArtifactRoot,
// renderCtx.Artifact.ID, renderCtx.Artifact.Paths.Extra["boot.cfg"]) 读出
// 原始模板文本，算出 bootCfgURLBase（renderCtx.SetArtifactURL("")的目录
// 形态或等价拼法）与 answerFileURL（renderCtx.Bases.GatewayURL("v1",
// "answer", renderCtx.Task.ID, renderCtx.InstallToken, "boot.cfg")，与
// menu.go 的 renderESXiMenu 拼 answerFileURL 同一模式），再调用
// render.RenderESXiBootCfg(template, bootCfgURLBase, answerFileURL)——
// 这一步的读盘与拼参数逻辑天然属于 gateway 层（它已经做别的 IO），不需要
// 再触碰渲染器"零 IO"的约束。
//
// 本次改动没有在没有这两处授权改动的情况下发明变通方案（例如让渲染器
// 偷偷读盘，或在 gateway 层猜测一个制品根目录路径）——那会把一个明确的
// 契约缺口伪装成"看起来能用"，而实际产出的仍是错误内容，且更难被发现。
// 保守选择是 fileName == "boot.cfg" 的行为保持不变（仍是 esxi.go 记录的
// 那个已知缺口，没有变得更差），同时让 fileName == "ks.cfg" 这条本次能
// 完整打通的路径先落地——它是一个独立可用的正确修复：装机时机器会先取
// boot.cfg（拿到当前仍错误的引导参数文件），若这一步在真实部署中被人工
// 修正或将来补上述两处改动，机器接着会去取 ks= 指向的 ks.cfg，届时这条
// 路径已经是正确的。
