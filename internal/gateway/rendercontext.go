// 本文件实现渲染上下文构造。对应实现计划任务 5，依据 SPEC §1.5、§4.3、
// §4.4、§5.1。
//
// 渲染上下文的构造是网关的主要职责之一。要取齐：任务、模板的 pin 版本、
// 制品集、主机、已排好序的 hook 引用、fact 表、基址、两个 token、syslog
// 目标、机器实际上报的固件模式。
//
// hook 正文本身不进入渲染上下文——model.RenderContext.Hooks 的类型是
// []model.HookRef（引用：HookID/HookDigest/Stage/Order/Required/
// ParamsDefaults），不是 []model.Hook（正文）。这与 SPEC §6.3 的
// run_hook 客户端封装一致：机器侧用 curl 按 digest 从
// "$ART/hooks/<digest>/script.sh" 拉取脚本本身（由 nginx 直接服务的静态
// 制品，不经过 Go 进程），网关只需要把 digest 与参数传给渲染器去拼这个
// URL，不需要在请求路径上读出并携带完整脚本正文。
package gateway

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/model"
)

// hookStageOrder 是拼接 RenderContext.Hooks 时四个阶段的固定顺序：pre →
// post-nochroot → post → firstboot（SPEC §5.2 的阶段本身是能力边界，不是
// 时间顺序，但这里的顺序反映的正是它们各自实际执行的先后：安装前置钩子、
// chroot 前的 post、chroot 内的 post、首次启动后的 firstboot）。
//
// RenderContext.Hooks 的文档要求「已按阶段与 order 排好序」——排序逻辑
// 收敛在 model.Template.HooksForStage（按 Order 排该阶段内部的顺序），
// 本函数只负责按这个固定的阶段顺序把四段结果依次拼接，不重新排序阶段
// 内部的元素。
var hookStageOrder = []model.HookStage{
	model.HookStagePre,
	model.HookStagePostNoChroot,
	model.HookStagePost,
	model.HookStageFirstboot,
}

// buildRenderContext 取齐 model.RenderContext 的全部字段，供 /v1/boot 命中
// 路径渲染菜单、/v1/answer 渲染应答文件共用。
//
// task 应是已经完成 StateMachine.StartBoot（或更晚的状态转换）之后的最新
// 任务快照——本函数不做任何状态转换，只读取 task 已经携带的字段
// （TemplateID/TemplateVersion/ArtifactSetID/Token/FinalizeToken 等）。
//
// actualFirmware 是本次请求机器实际上报的固件模式（来自 /v1/boot 的
// ${platform}），不是任务记录值——Windows/ESXi 菜单据此选制品键，这是
// §4.6 引导时按实际上报值校验的直接体现。
func (s *Server) buildRenderContext(ctx context.Context, task model.Task, actualFirmware model.FirmwareMode) (model.RenderContext, error) {
	tpl, err := s.deps.TemplateStore.GetTemplate(ctx, task.TemplateID, task.TemplateVersion)
	if err != nil {
		return model.RenderContext{}, fmt.Errorf("gateway: 查模板 %s@%d 失败: %w", task.TemplateID, task.TemplateVersion, err)
	}

	artifact, err := s.deps.ArtifactSetStore.GetArtifactSet(ctx, task.ArtifactSetID)
	if err != nil {
		return model.RenderContext{}, fmt.Errorf("gateway: 查制品集 %s 失败: %w", task.ArtifactSetID, err)
	}

	host, err := s.deps.HostStore.FindHostByIdentity(ctx, task.HostSN, task.HostMAC, task.HostUUID)
	if err != nil {
		return model.RenderContext{}, fmt.Errorf("gateway: 查主机（SN=%s）失败: %w", task.HostSN, err)
	}

	facts, err := s.deps.FactStore.GetFacts(ctx, task.ID)
	if err != nil {
		return model.RenderContext{}, fmt.Errorf("gateway: 查 facts 失败: %w", err)
	}

	hooks := make([]model.HookRef, 0, len(tpl.HookRefs))
	for _, stage := range hookStageOrder {
		hooks = append(hooks, tpl.HooksForStage(stage)...)
	}

	return model.RenderContext{
		Task:     task,
		Template: tpl,
		Artifact: artifact,
		Host:     host,
		Hooks:    hooks,
		Facts:    facts,
		Bases:    s.deps.Bases,

		InstallToken:  task.Token,
		FinalizeToken: task.FinalizeToken,

		SyslogHost: s.deps.SyslogHost,
		SyslogPort: s.deps.SyslogPort,

		ActualFirmware: actualFirmware,
	}, nil
}
