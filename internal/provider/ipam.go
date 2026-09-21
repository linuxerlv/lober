// 本文件实现 static IPAM：从请求与表单直接取值，不访问外部系统。对应实现
// 计划任务 5，依据 SPEC §7.1、§5.7、§7.4。
package provider

import (
	"context"

	"github.com/lober/lober/internal/core/contract"
)

// staticIPAMFormKeys 是 static IPAM 从表单里取值时使用的键名。之所以固定
// 这三个键名而不是让调用方传参决定：static 实现的存在理由就是「表单已经
// 填好了 IP/网关/掩码，不需要真的申请」，键名与建任务表单的字段名对齐是
// 这类实现的唯一职责，可配置反而掩盖了它的定位。
const (
	staticIPAMFormKeyIP      = "biz_ip"
	staticIPAMFormKeyGateway = "biz_gateway"
	staticIPAMFormKeyMask    = "biz_mask"
)

// staticIPAM 是 IPAMProviderV1 的内置实现：从 AllocRequest 未携带、而是
// 由调用方（同步决策编排器）额外注入的表单数据里直接取值（§5.7：业务 IP
// 写配置但不激活，IP 本身在建任务表单里已经填好，static 实现不做任何
// 分配决策，只是把表单值原样传出）。
//
// 释放操作是空实现（任务 5：为将来换成真实 IPAM 时调用点不用改——
// task.failed 之后释放 IP 这条路径届时自动生效，接口完整性先占住位置）。
type staticIPAM struct {
	// form 是建任务表单数据，Allocate 从这里直接取值。构造时注入而不是
	// 通过 AllocRequest 传入：AllocRequest 是 M1 定义的窄接口参数（只有
	// Hostname 与 Role），static 实现需要的完整表单数据在这个接口形状里
	// 放不下，因此改为工厂在装配时把当次请求的表单数据设进去。
	//
	// 见 resolve.go 里 taskParamsResolver.Resolve 对本字段的设置方式——
	// 它在每次决策前构造一个新的 staticIPAM 实例并注入当次 formData，
	// 而不是在多次调用之间共享同一个实例的表单状态（并发安全的简单办法：
	// 不共享可变状态，而不是加锁）。
	form map[string]string
}

// newStaticIPAM 用给定表单数据构造一个 staticIPAM。
func newStaticIPAM(form map[string]string) *staticIPAM {
	return &staticIPAM{form: form}
}

// Allocate 从表单直接取 biz_ip/biz_gateway/biz_mask 三个键传出。
func (s *staticIPAM) Allocate(ctx context.Context, req contract.AllocRequest) (contract.AllocResult, error) {
	return contract.AllocResult{
		IP:      s.form[staticIPAMFormKeyIP],
		Gateway: s.form[staticIPAMFormKeyGateway],
		Mask:    s.form[staticIPAMFormKeyMask],
	}, nil
}

// Release 是空实现（任务 5：接口完整性，将来换真实 IPAM 时调用点不用改）。
func (s *staticIPAM) Release(ctx context.Context, ip string) error {
	return nil
}

// Ping 直接返回成功——static 实现没有外部依赖，永远健康（Provider 契约
// 文档：MVP 的内置实现直接返回成功）。
func (s *staticIPAM) Ping(ctx context.Context) error {
	return nil
}

// 编译期断言：staticIPAM 必须满足 contract.IPAMProviderV1。
var _ contract.IPAMProviderV1 = (*staticIPAM)(nil)
