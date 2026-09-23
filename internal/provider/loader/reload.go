// 本文件实现 ReloadableLoader：包住一个可被原子替换的 Caller，让「改已有
// 的加载器配置」不需要重启进程（SPEC §7.2 加载器对比表：exec/http 类型
// 「改已有的」列的结论是「不重启（下次调用即生效）」）。
//
// 这条属性对 exec/http 而言本身有两层含义，本文件只处理第二层：
//  1. 外部程序本身的内容变化（运维直接编辑 dns.sh 脚本文件）天然不需要
//     任何 Go 侧机制——每次 Call 都是重新拉起一个新子进程/发一个新
//     HTTP 请求，下一次调用自然读到磁盘上最新的文件内容，这是 exec/http
//     "无状态、每次调用独立" 这个设计本身自带的性质，不需要额外代码。
//  2. **加载器的构造参数变化**（运维想换一个不同的 command 路径、不同的
//     url、不同的 timeout/重试/熔断参数）——这需要重新调用
//     NewExecLoader/NewHTTPLoader 构造一个新实例，而调用方（factory.go
//     装配出的、装进 Providers 结构体字段的那个对象）如果直接持有旧
//     *ExecLoader 或 *HTTPLoader 的值，换新实例就意味着调用方所有持有
//     旧引用的地方都要感知到替换，这正是需要一层间接的原因。
//
// ReloadableLoader 就是这层间接：调用方持有的是 *ReloadableLoader 这个
// 稳定的地址（在整个进程生命周期内不变），它内部指向的具体 Caller
// （exec 或 http 实现）可以被 Swap 原子替换，且替换不影响正在进行中的
// 调用（in-flight 的 Call 已经取到了旧 Caller 的引用，会跑完这一次，
// 不会在执行中途看到指针被换掉——这与 §7.3 grpc 类型 in-flight drain 的
// 目标一致，但做法更简单：exec/http 每次调用都是独立的一次性操作，没有
// 长连接或常驻状态需要"排空"，原子指针替换已经足够）。
//
// 触发 Swap 的具体机制（配置文件监听、管理端点、SIGHUP）不在本文件范围
// ——那需要 cmd/lober 装配点存在后才能接线（见本任务 wiringNeeded 说明），
// 本文件只提供"替换本身是原子且安全"这一底层能力。
package loader

import (
	"context"
	"fmt"
	"sync/atomic"
)

// ReloadableLoader 是 Caller 的一个包装实现：内部持有一个可被原子替换的
// 底层 Caller。
type ReloadableLoader struct {
	current atomic.Pointer[Caller]
}

// NewReloadableLoader 用 initial 作为起始的底层 Caller 构造一个
// ReloadableLoader。initial 为 nil 时返回错误——一个没有底层实现的
// ReloadableLoader 无法响应任何调用，这应该在构造期被拒绝，而不是留到
// 第一次 Call 时才发现「忘了初始化」。
func NewReloadableLoader(initial Caller) (*ReloadableLoader, error) {
	if initial == nil {
		return nil, fmt.Errorf("loader: ReloadableLoader 的初始 Caller 不能为 nil")
	}
	r := &ReloadableLoader{}
	r.current.Store(&initial)
	return r, nil
}

// Call 实现 Caller：转发给当前生效的底层 Caller。
//
// 取指针的方式（先 Load 出 *Caller 再解引用一次）保证的是"这一次 Call
// 调用期间使用的是同一个底层实例"——即使 Swap 在 Call 执行期间被其它
// goroutine 调用，本次调用已经拿到的 caller 局部变量不会变化，正确地
// 体现了"in-flight 调用不受替换影响，只有下一次全新的 Call 才会用到新
// 实例"这条设计目标。
func (r *ReloadableLoader) Call(ctx context.Context, method string, params any, result any) error {
	callerPtr := r.current.Load()
	caller := *callerPtr
	return caller.Call(ctx, method, params, result)
}

// Swap 原子地把底层 Caller 替换为 next，返回替换前的旧 Caller（供调用方
// 需要时做清理，例如旧的是某种持有资源的实现——当前 ExecLoader/HTTPLoader
// 都不持有需要显式释放的资源，但接口层面保留这个返回值，避免将来加入
// 持有资源的加载器类型时需要改动 Swap 的签名）。
//
// next 为 nil 时拒绝替换并返回错误，不静默忽略——把 ReloadableLoader
// 换成一个 nil 底层会让下一次 Call 直接 panic（解引用 nil 接口值），
// 这种错误应该在 Swap 调用处立刻暴露，不能留到下一次业务调用才崩溃。
func (r *ReloadableLoader) Swap(next Caller) (previous Caller, err error) {
	if next == nil {
		return nil, fmt.Errorf("loader: Swap 的新 Caller 不能为 nil" +
			"（换成 nil 会让下一次 Call 在解引用时 panic，必须在替换时立刻拒绝）")
	}
	oldPtr := r.current.Swap(&next)
	return *oldPtr, nil
}

// 编译期断言：ReloadableLoader 必须满足 Caller。
var _ Caller = (*ReloadableLoader)(nil)
