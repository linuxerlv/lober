// 本文件实现信号端点 POST /v1/signal/{task}/{token}/{phase}。对应实现
// 计划任务 7，依据 docs/contracts/http-machine.md 端点 4、SPEC §6.1、§6.2、
// §6.4。
//
// 状态机由信号驱动（§6.1）。这个端点是机器侧唯一的主动上报通道。
//
// 成功响应严格按 docs/contracts/http-machine.md（冻结契约）：200 空 body。
// 这与实现计划任务 7 文字描述的「返回 204」不同——冻结契约文档优先，偏差
// 已记入本模块返回值的 contractGaps。
//
// TaskEvent 的 RC/Message/HookDigest 三个字段由网关直接写入（通过
// EventStore.AppendEvent），不经过 StateMachine.ApplySignal——后者的签名
// 是 ApplySignal(ctx, taskID, signal, seq)，没有参数位置能接收 rc、日志
// body、digest 这三项。这是本模块识别到的一个契约缺口（另见 answer.go
// 的状态码偏差），记入 contractGaps；本实现选择的最保守绕法是：网关分别
// 调用 EventStore.AppendEvent（写下带 RC/Message/HookDigest 的完整事件，
// Seq 用 model.SignalEntry.Seq 而不是客户端的 seq 参数，理由见下方注释）
// 与 StateMachine.ApplySignal（推进状态、处理去重/终态判断）——两次调用
// 各自负责契约要求里自己能覆盖的那一半，不在网关这一层重新实现状态推进
// 或去重逻辑（那是状态机的职责）。
package gateway

import (
	"context"
	"io"
	"net/http"
	"strconv"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// isValidHookDigest 校验 digest 是否是 64 位十六进制小写字符串（契约文档
// 约定 5）。不用 regexp 包——这个校验足够简单（定长 + 字符集），手写循环
// 比引入正则表达式引擎更直接，也更容易看出到底校验了什么。
func isValidHookDigest(s string) bool {
	if len(s) != 64 {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		isLowerHex := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isLowerHex {
			return false
		}
	}
	return true
}

// lookupTaskByEitherTokenPurpose 先当装机 token 查，查不到再当 finalize
// token 查（计划任务 7「两种用途都试」），返回命中的任务与对应的 token
// 用途。两者都查不到时返回 TaskStore 的错误（调用方只需要判断非 nil）。
func lookupTaskByEitherTokenPurpose(ctx context.Context, store contract.TaskStore, token string) (model.Task, model.TokenPurpose, error) {
	if task, err := store.GetTaskByToken(ctx, token, model.TokenPurposeInstall); err == nil {
		return task, model.TokenPurposeInstall, nil
	}
	task, err := store.GetTaskByToken(ctx, token, model.TokenPurposeFinalize)
	if err != nil {
		return model.Task{}, "", err
	}
	return task, model.TokenPurposeFinalize, nil
}

// parseSignalQuery 解析 query 里必需的 rc 与 seq 两个整数参数。ok 为 false
// 表示任一参数缺失或不是合法整数（契约文档端点 4「错误行为」：rc 或 seq
// 缺失或非整数 → 400）。
func parseSignalQuery(r *http.Request) (rc, seq int, ok bool) {
	q := r.URL.Query()
	rcRaw, hasRC := q["rc"]
	seqRaw, hasSeq := q["seq"]
	if !hasRC || !hasSeq || len(rcRaw) == 0 || len(seqRaw) == 0 {
		return 0, 0, false
	}

	rc, err := strconv.Atoi(rcRaw[0])
	if err != nil {
		return 0, 0, false
	}
	seq, err = strconv.Atoi(seqRaw[0])
	if err != nil {
		return 0, 0, false
	}
	return rc, seq, true
}

// handleSignal 处理 POST /v1/signal/{task}/{token}/{phase}。
func (s *Server) handleSignal(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	pathTaskID := r.PathValue("task")
	token := r.PathValue("token")
	phase := model.SignalKind(r.PathValue("phase"))

	// 步骤 1（契约文档「处理流程」）：校验 token。按 token 查任务，两种
	// 用途都试：先当装机 token 查，查不到再当 finalize token 查。
	task, purpose, err := lookupTaskByEitherTokenPurpose(ctx, s.deps.TaskStore, token)
	if err != nil {
		w.WriteHeader(http.StatusForbidden)
		return
	}
	if task.ID != pathTaskID {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	// 步骤 2：阶段名查表校验（用 M1 的信号表），表里没有的回 400——信号名
	// 来自 URL 路径段，不查表校验等于让机器指定自己的阶段。
	entry, ok := model.LookupSignal(phase)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// 若命中的是 finalize token，校验该信号在允许集合内，不在则回 403
	// （§6.4：finalize token 写在目标系统的磁盘上、TTL 24 小时，拿到它就
	// 能伪造整个装机流程的信号）。
	if purpose == model.TokenPurposeFinalize && !model.FinalizeCanSend(phase) {
		w.WriteHeader(http.StatusForbidden)
		return
	}

	rc, seq, ok := parseSignalQuery(r)
	if !ok {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	// hook-report 信号额外读 digest 参数：只在 hook-report 信号时读取，
	// 其余信号带了也忽略；格式不合格时忽略该参数（视为未提供），不拒绝
	// 整个信号——上报失败绝不中断装机。
	digest := ""
	if phase == model.SignalHookReport {
		raw := r.URL.Query().Get("digest")
		if isValidHookDigest(raw) {
			digest = raw
		}
	}

	// 读 body 作为日志正文（最近 4 KB 日志尾部，可为空），交给日志存储
	// 落文件。客户端负责把日志截到 4 KB 再发送（SPEC §6.2），网关侧不再
	// 额外截断——若客户端违反约定发来更大的 body，io.ReadAll 仍会把它
	// 完整读出，网关不因此拒绝请求（上报失败绝不中断装机的同一精神：
	// body 偏大不该变成一个新的错误分支）。
	var logBody string
	if r.Body != nil {
		raw, _ := io.ReadAll(r.Body)
		logBody = string(raw)
	}

	_ = s.deps.LogStore.AppendLog(ctx, task.ID, entry.Phase, logBody)

	// 无论是否推进状态，都追加一条 TaskEvent（Source = EventSourceSignal，
	// RC 取自 rc 参数，Message 取自 body，HookDigest 取自校验通过的
	// digest 参数）。
	//
	// Seq 用 model.SignalEntry.Seq（服务端信号表序号）而不是客户端的 seq
	// 参数——契约文档明确这是两个不同的概念：客户端 seq 只用于同一客户端
	// 会话内的请求排序展示，服务端的去重判据是 entry.Seq 与 Task.LastSeq
	// 的比较。EventStore.AppendEvent 对 Seq>0 的事件按 (task_id, seq) 去重，
	// 用 entry.Seq 作为这个去重键与 ApplySignal 内部的去重判据保持同一个
	// 数值空间；hook-report 的 entry.Seq 恒为 0，AppendEvent 对 Seq<=0 的
	// 事件走不去重的分支（改用递减负数序号），这正好符合「hook-report
	// 一个阶段内可来任意多次，永不推进水位」的语义——每次 hook-report 都
	// 应该产生一条新事件，不能被当作与前一次重复。
	_, _ = s.deps.EventStore.AppendEvent(ctx, model.TaskEvent{
		TaskID: task.ID, Seq: entry.Seq, Phase: entry.Phase, Level: model.EventLevelInfo,
		Message: logBody, RC: rc, HookDigest: digest, Source: model.EventSourceSignal,
	})

	// 调状态机应用信号：推进状态/阶段、处理序号去重（Store 层的
	// (task_id, seq) 去重命中）、处理任务已处于终态的幂等分支。忽略
	// 返回值与错误——契约文档明确无论信号是否被接受都回 200。
	_, _ = s.deps.StateMachine.ApplySignal(ctx, task.ID, phase, seq)

	// 记录本次请求的客户端 IP 与任务的关联，供 syslog 接收器归档使用
	// （见 syslog.go 文件头注释）。已知限制：booted 之后信号源 IP 会变
	// （业务网生效，§5.7），此时这里记的关联对 booted 及之后阶段的 syslog
	// 归档没有帮助——这是接受的限制，不是本调用需要规避的错误，重启前
	// installer 阶段发出的信号仍会正确更新映射，那才是 syslog 通道价值
	// 集中的地方。
	s.rememberClientTask(clientIP(r), task.ID, entry.Phase)

	// 返回 200，无论信号是否被接受——机器侧的 signal() 会重试三次，序号
	// 重复是正常现象而不是错误路径。
	w.WriteHeader(http.StatusOK)
}
