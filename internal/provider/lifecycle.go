// 本文件实现生命周期订阅：只订阅 task.ready 与 task.failed 两个事件。
// 对应实现计划任务 8，依据 SPEC §7.4、§7.5。
//
// 只订这两个事件的理由（任务 8 明确要求）：这两个才真正匹配
// fire-and-forget 语义——DNS 注册、推通知都是「失败了也不影响任务状态」
// 的操作（SPEC §7.4：DNS/Notify 的失败语义是只告警）。IPAM / TemplateResolver
// / DiskPlanner 这类阻塞式 provider 绝不能挂在事件总线上，它们走
// TaskParamsResolver（resolve.go），在写库之前同步调用——这条边界在
// RegisterLifecycleHooks 里没有、也不应该有任何变通空间：本函数只接受
// EventBus 与 DNS/Notify 两个只告警语义的 provider，签名上就排除了误挂
// 阻塞式 provider 的可能性。
package provider

import (
	"context"
	"log"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// RegisterLifecycleHooks 向 bus 订阅 task.ready 与 task.failed 两个事件：
//   - task.ready：写 DNS（需要业务 IP 与主机名，从 task.ResolvedParams 与
//     task.HostSN 取值）、推通知
//   - task.failed：只推通知（失败场景没有 DNS 记录要写）
//
// 两个回调内部的 DNS/Notify 调用失败只记日志，不向上传播——这与
// contract.EventBus.Subscribe 的文档要求一致（回调不返回错误，回调内部的
// 失败由回调自己决定如何处理），且与 §7.4 的 warn_only 语义对齐（一次
// DNS 抖动不该把已经装好的机器标成失败，而这里已经是「机器已经装好」
// 之后的路径，更没有状态可以被拖累）。
func RegisterLifecycleHooks(bus contract.EventBus, dns contract.DNSProviderV1, notify contract.NotifyProviderV1) {
	bus.Subscribe(contract.LifecycleEventTaskReady, func(tk model.Task) {
		onTaskReady(tk, dns, notify)
	})
	bus.Subscribe(contract.LifecycleEventTaskFailed, func(tk model.Task) {
		onTaskFailed(tk, notify)
	})
}

// onTaskReady 处理 task.ready：写 DNS 注册、推通知。
func onTaskReady(tk model.Task, dns contract.DNSProviderV1, notify contract.NotifyProviderV1) {
	ctx := context.Background()

	// 读裸键 "hostname"/"biz_ip"，与 resolve.go 的 Resolve 写回
	// ResolvedParams 时使用的键名对齐（也是 internal/render/params.go
	// InlineParams 白名单认得的同一组键）——ResolvedParams 现在只可能
	// 携带这两个裸键（外加可能的 role，本处不需要），不再是 fact 命名空间
	// 键 model.FactHostname/model.FactBizIP（LOBER_HOSTNAME/LOBER_BIZ_IP
	// 是注入给机器侧 hook 的环境变量名，属于 Facts 命名空间，不属于
	// ResolvedParams）。
	fqdn := tk.ResolvedParams["hostname"]
	ip := tk.ResolvedParams["biz_ip"]
	if fqdn != "" && ip != "" {
		if err := dns.Register(ctx, fqdn, ip); err != nil {
			log.Printf("provider/lifecycle: task.ready 写 DNS 失败（task_id=%s, fqdn=%s, ip=%s）: %v"+
				"——只告警不影响任务状态（SPEC §7.4）", tk.ID, fqdn, ip, err)
		}
	} else {
		log.Printf("provider/lifecycle: task.ready（task_id=%s）缺少 hostname 或 biz_ip，跳过 DNS 注册", tk.ID)
	}

	if err := notify.Emit(ctx, contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskReady,
		TaskID: tk.ID,
		Detail: map[string]string{"state": string(tk.State)},
	}); err != nil {
		log.Printf("provider/lifecycle: task.ready 推通知失败（task_id=%s): %v", tk.ID, err)
	}
}

// onTaskFailed 处理 task.failed：只推告警通知（没有 DNS 记录要处理——
// 失败的任务没有装完，不存在需要注销的业务 DNS 记录）。
func onTaskFailed(tk model.Task, notify contract.NotifyProviderV1) {
	ctx := context.Background()

	detail := map[string]string{"state": string(tk.State)}
	if tk.FailureReason != "" {
		detail["failure_reason"] = string(tk.FailureReason)
	}

	if err := notify.Emit(ctx, contract.LifecyclePayload{
		Event:  contract.LifecycleEventTaskFailed,
		TaskID: tk.ID,
		Detail: detail,
	}); err != nil {
		log.Printf("provider/lifecycle: task.failed 推通知失败（task_id=%s): %v", tk.ID, err)
	}
}
