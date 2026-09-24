// 本文件实现应答文件端点 GET /v1/answer/{task}/{token}/{file}。对应实现
// 计划任务 6，依据 docs/contracts/http-machine.md 端点 3、SPEC §4.4、§4.5、
// §5.1。
//
// 应答文件是装机的实际指令。它的暴露控制用状态门控（§4.4）：一次性失效会
// 直接掐死正常重试（installer 可能重复取应答文件，例如 Ubuntu 的
// cloud-init 会取 user-data 与 meta-data 两个文件），因此仅当状态在
// model.AnswerFileServable 的集合内时提供，其余回 403。
//
// 错误响应形态严格按 docs/contracts/http-machine.md（冻结契约）：token
// 无效/状态不满足/task 不匹配 → 403 空响应；{file} 与
// RenderContext.AnswerFileName() 不一致 → 404；渲染出错 → 500 + 简短纯
// 文本原因 + 一条 level=error 的 TaskEvent。这与实现计划任务 6 文字描述的
// 「渲染失败 → 200 加注释文本」不同——冻结契约文档优先，偏差已记入本模块
// 返回值的 contractGaps。
//
// ESXi 家族的 boot.cfg 请求走一条专属分支（esxi_bootcfg.go）：它需要读盘
// 取出导入时保存的原始引导参数文件模板再做三处改写，不能走
// s.deps.Renderer.RenderAnswerFile 那条零 IO 的通用路径——错误响应形态
// 与通用路径完全一致，只是渲染这一步换了实现，见 handleAnswer 内部分支。
package gateway

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/lober/lober/internal/core/model"
)

// handleAnswer 处理 GET /v1/answer/:task/:token/:file（对外 URL 仍是
// /v1/answer/{task}/{token}/{file}，:param 只是 gin 路由声明语法）。
func (s *Server) handleAnswer(c *gin.Context) {
	ctx := c.Request.Context()
	pathTaskID := c.Param("task")
	token := c.Param("token")
	file := c.Param("file")

	// 认证：token 存在、TokenPurpose == TokenPurposeInstall、未过期、与
	// 路径中的 {task} 匹配、且 model.AnswerFileServable(task.State) 为真。
	// GetTaskByToken 对「不存在/过期/用途不符」统一返回 ErrNotFound
	// （§4.4：不泄露这三者的区分），因此这里只需要判断 err 非 nil 即可
	// 回 403，不需要区分具体原因。
	task, err := s.deps.TaskStore.GetTaskByToken(ctx, token, model.TokenPurposeInstall)
	if err != nil {
		c.Status(http.StatusForbidden)
		return
	}

	// 交叉校验路径里的任务 id 与 token 解出的任务一致。这一步不能省：
	// 不校验的后果是可以拿 A 任务的 token 配 B 任务的 id 去取应答文件，
	// 路径里那个 id 会被用于构造渲染上下文，不一致时会渲染出张冠李戴的
	// 内容。
	if task.ID != pathTaskID {
		c.Status(http.StatusForbidden)
		return
	}

	// 状态门控：仅 booting 与 installing 可服务。用状态而非一次性失效——
	// 一次性失效会直接掐死正常重试。
	if !model.AnswerFileServable(task.State) {
		c.Status(http.StatusForbidden)
		return
	}

	// 构造渲染上下文（内部会查出 ArtifactSet，从而能算出该任务的应答
	// 文件名）。actualFirmware 传空串——应答文件渲染不依赖机器本次上报的
	// 固件模式，那是 /v1/boot 菜单渲染专属的字段（§4.6 校验发生在
	// /v1/boot，应答文件请求时不会再次带 ${platform}）。
	renderCtx, err := s.buildRenderContext(ctx, task, "")
	if err != nil {
		// 渲染上下文本身构造不出来（模板/制品/主机查不到，基础设施性
		// 问题）：这不是「token 无效」，但机器拿不到任何可用内容，按
		// 403 处理最接近契约文档「token 校验失败」这一档——不满足
		// AnswerFileServable 判定所需的完整任务数据，视为不满足服务
		// 条件。
		c.Status(http.StatusForbidden)
		return
	}

	// 文件名校验：{file} 段必须与 RenderContext.AnswerFileName() 的返回值
	// 逐字符相等，不接受模糊匹配或按扩展名猜测（约定 4）。文件名校验交给
	// 渲染器决定「用哪个模板」是 M1 契约写明的渲染器职责，但「路径段是否
	// 等于合法文件名」这一判断——判断的结果导致 404 而不是渲染错误的
	// 500——必须在网关层做，否则网关无法区分「文件名根本不对」与「文件名
	// 对但渲染本身出错」这两种契约要求不同状态码的情形。
	wantFile, ok := renderCtx.AnswerFileName()
	if !ok || !answerFileNameAllowed(renderCtx, file, wantFile) {
		c.Status(http.StatusNotFound)
		return
	}

	// 调状态机标记应答文件已取（它幂等：booting → installing；已经在
	// installing 或之后视为已处理过，直接返回当前任务、不报错）。
	if _, err := s.deps.StateMachine.MarkAnswerFetched(ctx, task.ID); err != nil {
		c.Status(http.StatusForbidden)
		return
	}

	// ESXi 的 boot.cfg 是本次改动新增的专属分支：它是 mboot.efi 的引导
	// 参数文件本身，需要读出导入时保存的原始模板做三处改写（prefix=/
	// kernelopt= 的 ks=/modules= 去前导斜杠），这份改写逻辑需要读盘，
	// 不能走 s.deps.Renderer.RenderAnswerFile 那条零 IO 的路径——
	// ks.cfg（同一家族的第二候选文件名，指向独立的 kickstart 方言正文）
	// 不受影响，仍走下面的通用分支（见 esxi_bootcfg.go 文件头注释）。
	var content string
	if renderCtx.Artifact.OSFamily == model.OSFamilyESXi && file == "boot.cfg" {
		content, err = s.renderESXiBootCfgResponse(ctx, renderCtx)
	} else {
		content, err = s.deps.Renderer.RenderAnswerFile(renderCtx, file)
	}
	if err != nil {
		// 渲染本身出错（模板/参数问题）：500，body 为简短纯文本原因；
		// 同时记一条 TaskEvent（level=error）。
		_, _ = s.deps.EventStore.AppendEvent(ctx, model.TaskEvent{
			TaskID: task.ID, Phase: task.CurrentPhase, Level: model.EventLevelError,
			Message: "应答文件渲染失败: " + err.Error(),
			Source:  model.EventSourceProvider,
		})
		c.Data(http.StatusInternalServerError, "text/plain; charset=utf-8",
			[]byte("应答文件渲染失败，请联系运维核查任务与模板配置\n"))
		return
	}

	// 记录本次请求的客户端 IP 与任务的关联，供 syslog 接收器归档使用
	// （见 syslog.go 文件头注释）。
	s.rememberClientTask(clientIP(c.Request), task.ID, task.CurrentPhase)

	// content 是应答文件正文（kickstart/preseed/autoyast），必须用 c.Data
	// 而不是 c.String(code, content)——kickstart 的 %packages/%post 段会被
	// c.String 当格式串解析，产出一份损坏但不报错的应答文件（迁移契约 §6
	// 明确点名的最容易静默出错的一点）。
	c.Data(http.StatusOK, answerFileContentType(renderCtx.Artifact.OSFamily), []byte(content))
}

// answerFileContentType 按 OS 家族返回应答文件的 Content-Type（契约文档
// 端点 3「成功响应」）：ks.cfg/preseed.cfg/user-data/boot.cfg 用
// text/plain，autoinst.xml/unattend.xml 用 application/xml。
func answerFileContentType(family model.OSFamily) string {
	switch family {
	case model.OSFamilySUSE, model.OSFamilyWindows:
		return "application/xml; charset=utf-8"
	default:
		return "text/plain; charset=utf-8"
	}
}

// answerFileNameAllowed 判定请求路径里的 {file} 段是否是该任务允许取的应答
// 文件名。
//
// 绝大多数家族只有一个合法文件名（AnswerFileName() 的返回值），逐字符相等
// 即可。**Ubuntu 是一个例外**：autoinstall 走 cloud-init 的 NoCloud 数据源，
// 内核参数 ds=nocloud-net;s=<url> 指向一个目录，而 cloud-init 会在那个目录下
// 依次取 user-data 与 meta-data 两个文件（SPEC §4.4）。只放行 user-data 的
// 后果是 meta-data 请求拿到 404 —— 现场表现是「机器装到一半停住」，而
// internal/render/renderer.go 里那个专门处理 meta-data 的分支会成为死代码。
//
// **ESXi 是与 Ubuntu 同构的第二个例外**：boot.cfg（AnswerFileName(esxi) 固定
// 返回的唯一值）是 mboot.efi 的引导参数文件本身，其 kernelopt= 行的 ks= 参数
// 指向另一份独立的文件——ESXi 自己的 kickstart 方言正文，真实部署里这两个
// 文件从来不是同一个（见 internal/render/esxi.go 文件头注释）。只放行 boot.cfg
// 的后果是 ks.cfg 请求拿到 404。boot.cfg 请求本身自 esxi_bootcfg.go 接入后
// 已经改走专属的读盘改写路径（handleAnswer 内部分支），不再触发
// AssembleESXiKickstart——两个文件名各自对应各自正确的内容。
//
// 这两条放行都必须窄：只对各自的家族、且只多放行那一个具体名字。
// 路径段来自机器请求，放宽成模糊匹配或按扩展名猜测等于让机器自己指定要取
// 哪种应答文件。
func answerFileNameAllowed(ctx model.RenderContext, file, wantFile string) bool {
	if file == wantFile {
		return true
	}
	if ctx.Artifact.OSFamily == model.OSFamilyUbuntu && file == "meta-data" {
		return true
	}
	return ctx.Artifact.OSFamily == model.OSFamilyESXi && file == "ks.cfg"
}
