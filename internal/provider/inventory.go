// 本文件实现内置 Inventory：查本地主机清单（MVP 没有外部 CMDB）。对应
// 实现计划任务 5，依据 SPEC §7.1、§7.4、§4.3。
package provider

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// hostLister 是 builtinInventory 依赖的最窄接口：只需要按身份查主机
// （SN → MAC → UUID 回退，§4.3），不需要 contract.HostStore 的其余方法
// （UpsertHost/GetHost/ListHosts）。窄依赖让测试替身只实现这一个方法。
type hostLister interface {
	FindHostByIdentity(ctx context.Context, sn, mac, uuid string) (model.Host, error)
}

// builtinInventory 是 InventoryProviderV1 的内置实现：查询本模块所属进程
// 内的主机清单（discovery 同步写入的那张表），不访问任何外部 CMDB
// （任务 5：MVP 没有外部 CMDB，查不到的失败语义是阻塞但允许手工录入
// 兜底——手工录入是调用方的补救路径，本实现只负责「查」这一半）。
type builtinInventory struct {
	hosts hostLister
}

// NewBuiltinInventory 用给定的主机查询依赖构造一个内置 Inventory。
//
// hosts 参数类型是本包定义的窄接口 hostLister，而不是
// contract.HostStore——依赖窄接口是计划任务 10 对 Store 拆分窄接口族的
// 同一条设计原则在调用方的体现：本实现只用得到 FindHostByIdentity 一个
// 方法，没有理由要求调用方传入实现了全部四个 HostStore 方法的对象。
// contract.HostStore 的具体实现（无论是 fake.Store 还是 postgres.Store）
// 天然满足 hostLister，调用方直接传即可，不需要额外适配。
func NewBuiltinInventory(hosts hostLister) contract.InventoryProviderV1 {
	return &builtinInventory{hosts: hosts}
}

// LookupBySN 按 SN 查询本地主机清单（§7.1：按 SN 查询外部清单系统——
// 本实现里「外部」退化为「本进程内的 HostStore」）。查不到时返回一个
// 描述性错误，不是 contract.ErrNotFound 本身：调用方（阻塞式的建任务
// 编排）需要的是「查不到」这个事实与人类可读的原因，不需要用
// errors.Is 判定它是不是 contract 层的某个 sentinel error——Inventory
// 查不到与 Store 层查不到语义不同：前者是「运维填的 SN 在清单里没有」，
// 后者是「这个 id 从未存在过」，混用同一个 sentinel 会让调用方无法区分
// 这两类完全不同的排障方向。
func (b *builtinInventory) LookupBySN(ctx context.Context, sn string) (model.Host, error) {
	host, err := b.hosts.FindHostByIdentity(ctx, sn, "", "")
	if err != nil {
		return model.Host{}, fmt.Errorf("provider/inventory: 本地清单中查不到 SN=%q 的主机"+
			"（MVP 没有外部 CMDB，若确认这台机器存在，请人工录入清单作为兜底，SPEC 计划任务 5）", sn)
	}
	return host, nil
}

// Ping 直接返回成功——本实现只读本进程内数据，没有需要探活的外部依赖。
func (b *builtinInventory) Ping(ctx context.Context) error {
	return nil
}

// 编译期断言：builtinInventory 必须满足 contract.InventoryProviderV1。
var _ contract.InventoryProviderV1 = (*builtinInventory)(nil)
