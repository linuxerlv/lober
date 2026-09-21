// 本文件实现 noop DNS：记日志即返回成功。对应实现计划任务 5，依据
// SPEC §7.1、§7.4。
package provider

import (
	"context"
	"log"

	"github.com/lober/lober/internal/core/contract"
)

// noopDNS 是 DNSProviderV1 的内置实现：注册/注销都只记日志，不做真实的
// DNS 更新。失败语义是只告警（§7.4）——一次 DNS 抖动不能把装好的机器标成
// 失败，noop 实现天然不会失败，这与该失败语义并不矛盾：调用方（生命周期
// 订阅）仍按 warn_only 处理它，只是 noop 实现自己永远不产生需要告警的失败。
type noopDNS struct{}

// newNoopDNS 构造一个 noopDNS。
func newNoopDNS() *noopDNS {
	return &noopDNS{}
}

// Register 记一行日志，返回成功。
func (n *noopDNS) Register(ctx context.Context, fqdn, ip string) error {
	log.Printf("provider/dns(noop): register fqdn=%s ip=%s", fqdn, ip)
	return nil
}

// Deregister 记一行日志，返回成功。
func (n *noopDNS) Deregister(ctx context.Context, fqdn string) error {
	log.Printf("provider/dns(noop): deregister fqdn=%s", fqdn)
	return nil
}

// Ping 直接返回成功。
func (n *noopDNS) Ping(ctx context.Context) error {
	return nil
}

// 编译期断言：noopDNS 必须满足 contract.DNSProviderV1。
var _ contract.DNSProviderV1 = (*noopDNS)(nil)
