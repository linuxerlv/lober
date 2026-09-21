// 本文件实现引导决策端点 GET /v1/boot。对应实现计划任务 4（no_task 路径）、
// 任务 5（命中路径），依据 docs/contracts/http-machine.md 端点 2、
// SPEC §4.2、§4.3、§4.6、§9.1。
//
// 这是 §9.1 的第一道闸门「默认拒绝」，也是 §10 失败模式 1（装完又从网络
// 启动导致循环重装）的拦截点。
package gateway

import (
	"net/http"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// bootProbe 是从 /v1/boot 的五个 query 参数解析出的探测结构（计划任务 4
// 「实现流程」第 1 步）。arch/plat 用 rawArch/rawPlat 保留原始字符串，是否
// 能被 model.ParseIPXEBuildArch/ParseIPXEPlatform 识别留到命中任务之后才
// 判断——查询参数格式错误时视为查不到匹配主机（契约端点 2「错误行为」），
// 而「查不到」这条判断只依赖 sn/mac/uuid 三个身份键，与 arch/plat 是否
// 合法无关，所以这里不在解析阶段就因为 arch/plat 解析失败而提前返回。
type bootProbe struct {
	sn      string
	mac     string
	uuid    string
	rawArch string
	rawPlat string
}

// parseBootProbe 从请求的 query 参数解析出 bootProbe。五个参数全部可能为
// 空（白牌机 SN 可能留空，计划任务 4 测试要点），本函数不做任何校验，
// 校验交给后续步骤按需处理。
func parseBootProbe(r *http.Request) bootProbe {
	q := r.URL.Query()
	return bootProbe{
		sn:      q.Get("sn"),
		mac:     q.Get("mac"),
		uuid:    q.Get("uuid"),
		rawArch: q.Get("arch"),
		rawPlat: q.Get("plat"),
	}
}

// handleBoot 处理 GET /v1/boot：引导决策的唯一入口，默认拒绝（§9.1 第一道
// 闸门）。
//
// 约定 1（不可违反）：始终返回 HTTP 200。本函数的所有分支必须收敛到
// writeNoTaskScript 或渲染菜单成功这两种结果，不存在任何提前 return 出
// 非 200 状态码的路径。
func (s *Server) handleBoot(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	probe := parseBootProbe(r)

	// 步骤 1-2（计划任务 4）：按身份查 pending 任务；查不到 → no_task，
	// 到此为止，不产生任何 Store 记录（约定 2：这一支不写 TaskEvent、
	// 不告警，唯一留痕是 nginx access log）。
	task, err := s.deps.TaskStore.FindPendingTaskByIdentity(ctx, probe.sn, probe.mac, probe.uuid)
	if err != nil {
		// 查询参数缺失、格式错误、查不到主机——按契约端点 2「错误行为」，
		// 三种情形统一视为「查不到」，同样返回 200 的 no_task 载荷，不返回
		// 400（FindPendingTaskByIdentity 对全空键、任意格式的字符串键都
		// 只会返回 contract.ErrNotFound，不会返回别的错误类型，因此这里
		// 不需要区分 err 的具体种类）。
		writeNoTaskScript(w, probe.sn)
		return
	}

	// 步骤 3（计划任务 5）：命中 pending 任务后，先做 MAC 双因子校验
	// （§4.3、§9.1 第二道闸门）。
	//
	// 校验方式：任务上记录的 MAC 与探测带来的 MAC 归一化后比对，不一致则
	// 回 no_task（不是报错）——对机器而言这与「没有任务」无法区分，这正是
	// 想要的：不给攻击者「SN 猜对了」的反馈。SN 已经在上面的
	// FindPendingTaskByIdentity 里按优先级验证过是查找键之一，这里只需
	// 额外校验 MAC；但当查找键命中的是 SN 而非 MAC 时，FindPendingTaskByIdentity
	// 并不会检查 MAC 是否一致——这正是本检查存在的理由：SN 单独可信度不足
	// （白牌机 SN 可能重复），必须与 MAC 联合判定才能签发 token。
	//
	// 归一化用 model.NormalizeMAC：两侧都过一遍归一化再比较，任一侧格式
	// 不可识别（NormalizeMAC 返回空串）都会导致比较失败——这是刻意的
	// 「宁可拒绝也不误判」方向（NormalizeMAC 文档：不把格式错误的输入
	// 静默接受为匹配）。
	normalizedTaskMAC := model.NormalizeMAC(task.HostMAC)
	normalizedProbeMAC := model.NormalizeMAC(probe.mac)
	if normalizedTaskMAC == "" || normalizedProbeMAC == "" || normalizedTaskMAC != normalizedProbeMAC {
		// 这是范围内失败（§4.6 说明：机器已经取到 /v1/boot 并带来了真实
		// 上报值，系统手上有任务、有实际值，必须记录），调用状态机的
		// Fail 把任务置为失败并写事件。不消耗 AttemptCount——Fail 本身
		// 不改 AttemptCount 字段。
		//
		// FailureReason 用 FailureReasonPrecheck：model.FailureReason 的
		// 九个取值里没有专门的「MAC 不匹配」原因，§4.2 的决策流程把 SN/MAC
		// 双因子校验与 §4.6 三项一致性校验并列为「签发 token 前的检查」，
		// 语义上都属于「预检」范畴，因此复用这个最贴近的既有值。这是
		// 契约缺口，已记入本任务返回值的 contractGaps。
		_, _ = s.deps.StateMachine.Fail(ctx, task.ID, model.FailureReasonPrecheck, model.FailureDetail{
			Phase: task.CurrentPhase,
		})
		writeNoTaskScript(w, probe.sn)
		return
	}

	actualArch, archErr := model.ParseIPXEBuildArch(probe.rawArch)
	actualFirmware, platErr := model.ParseIPXEPlatform(probe.rawPlat)
	if archErr != nil || platErr != nil {
		// arch/plat 无法识别：按契约端点 2「错误行为」视为查不到匹配
		// 主机——但此刻已经命中了一个具体任务且通过了 MAC 双因子，是否
		// 应该走「查不到」还是「§4.6 precheck 失败」这两种语义都指向同一
		// 个 no_task 载荷，为避免签发 token 给一个连身份都报不全的探测，
		// 这里选择不调用状态机、直接返回 no_task（与「查询参数格式错误
		// 视为查不到」的错误行为条款保持一致，不产生失败记录）。
		writeNoTaskScript(w, probe.sn)
		return
	}

	s.handleBootGrant(w, r, task, actualArch, actualFirmware)
}

// writeNoTaskScript 写出「约定 2」冻结的 no_task 载荷：HTTP 200、
// Content-Type: text/plain、contract.NoTaskScript(serial) 的产出逐字符
// 原样返回。
//
// serial 用于 echo 行里回显机器序列号，便于运维在 nginx access log 之外，
// 从机器自己的控制台看到这行输出（contract.NoTaskScript 文档）。
func writeNoTaskScript(w http.ResponseWriter, serial string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(contract.NoTaskScript(serial)))
}

// handleBootGrant 处理命中任务且 MAC 双因子通过之后的推进（计划任务 5）：
// 调状态机的 StartBoot（它内部做 precheck、attempt 计数、签 token、状态
// 转换），失败则按类型处置，成功则构造渲染上下文、调渲染器出菜单。
//
// 接收 *http.Request（而不是只接收 context.Context）是为了取得本次请求的
// clientIP，供 syslog.go 的进程内「客户端 IP → 任务」映射更新使用——见
// syslog.go 文件头注释的契约缺口说明。
func (s *Server) handleBootGrant(w http.ResponseWriter, r *http.Request, task model.Task, actualArch model.Arch, actualFirmware model.FirmwareMode) {
	ctx := r.Context()
	updated, err := s.deps.StateMachine.StartBoot(ctx, task.ID, actualArch, actualFirmware)
	if err != nil {
		// StartBoot 内部已经把 precheck 错误、attempt 超限都处理成状态机
		// 侧的 Fail 调用（contract.StateMachine.StartBoot 文档步骤
		// 1、3）；网关这一层只需要在返回 error 时统一回 no_task 载荷，不
		// 需要再区分错误的具体类型（计划任务 5「失败则按类型处置：
		// precheck 错误与 attempt 超限都回 no_task 载荷，任务已被状态机
		// 置 failed」）。
		writeNoTaskScript(w, task.HostSN)
		return
	}

	// finalize token 在这一步签发（调状态机），因为它要被写进 %post
	// （§6.4）：渲染应答文件时只有渲染上下文可用，若此刻任务上还没有
	// FinalizeToken，%post 里就没有凭据可写，机器重启后的全部信号
	// （booted/init-*/ready/hook-report）都无法上报。
	//
	// 签发失败时不中止整个引导流程——宁可先把菜单发给机器（机器已经拿到
	// 装机 token，能正常开始装机），finalize token 缺失只影响重启后阶段，
	// 留给 IssueFinalizeToken 自身的 TTL/重试机制处理，不应该因为这一步
	// 失败就让机器连装机都开始不了（那会比「重启后阶段暂时上报不了」
	// 更严重）。忽略错误后 updated 仍是 StartBoot 返回的任务快照（可能
	// 不含 FinalizeToken），渲染器会看到空的 FinalizeToken 字段。
	if withFinalize, err := s.deps.StateMachine.IssueFinalizeToken(ctx, updated.ID); err == nil {
		updated = withFinalize
	}

	renderCtx, err := s.buildRenderContext(ctx, updated, actualFirmware)
	if err != nil {
		// 渲染上下文构造失败（模板/制品/主机查不到等基础设施性问题）：
		// 约定 1 要求始终 200，即使装配数据本身不完整，也必须给出机器能
		// 消费的合法脚本，不能让机器停在非 200 上等人。
		writeNoTaskScript(w, task.HostSN)
		return
	}

	menu, err := s.deps.Renderer.RenderMenu(renderCtx)
	if err != nil {
		writeNoTaskScript(w, task.HostSN)
		return
	}

	// 记录本次请求的客户端 IP 与任务的关联，供 syslog 接收器归档使用
	// （见 syslog.go 文件头注释）。装机阶段机器还在装机 VLAN，此时的
	// clientIP 大概率就是稍后 installer 发 syslog 时的源 IP。
	s.rememberClientTask(clientIP(r), updated.ID, updated.CurrentPhase)

	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(menu))
}
