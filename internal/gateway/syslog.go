// 本文件实现 syslog UDP 接收器。对应实现计划任务 8，依据
// docs/contracts/http-machine.md UDP :514 一节、SPEC §6.5。
//
// syslog 的不可替代性（§6.5）：当 anaconda 炸在依赖解析、磁盘写坏、仓库
// 404 时，hook 一行都没跑到、信号完全静默，而 syslog 里有确切的错误行。
//
// 归档依据是源 IP 这件事有个已知限制：重启后业务网生效，booted 之后的
// 信号源 IP 会变（§5.7），此时 syslog 若还在发就归档不到。这是可接受的——
// 重启后的诊断主要靠信号，syslog 的价值集中在 installer 阶段。
//
// 契约缺口说明：model.Task 没有「当前客户端 IP」字段，
// contract.TaskStore 也没有按 IP 反查任务的方法——「按源 IP 归档到对应
// 任务」这个要求无法通过 Store 契约表达的查询实现。本实现的绕法：Server
// 自己在进程内维护一份「客户端 IP → 任务 id」映射（clientTaskMap），在
// /v1/boot 命中授权、/v1/answer、/v1/signal 成功处理这三个入口用请求的
// clientIP 更新映射；syslog 接收器查这份映射决定归档目标，查不到则落
// 公共文件（syslogUnmatchedTaskID），不丢弃（计划任务 8「匹配不到的行落
// 一个公共文件，不要丢弃——那些行可能来自一台还没建任务就在报错的机器，
// 是有用的取证材料」）。
//
// 这份映射是进程内存状态，不持久化：进程重启后映射为空，重启前发生的
// IP→任务关联全部丢失，此时全部 syslog 行会落公共文件——这与「找不到
// 任务归属不是错误，只是取证材料变少」的方向一致，不是需要额外处理的
// 故障模式。要改成正解需要在 model.Task 或专门的接口上增加「按客户端 IP
// 查找当前任务」的查询能力，这需要修改契约层，本模块不做这个改动。
package gateway

import (
	"context"
	"log/slog"
	"net"

	"github.com/lober/lober/internal/core/model"
)

// syslogUnmatchedTaskID 是匹配不到任务时使用的占位任务 id，供公共文件
// 与私有任务日志共用同一套 LogStore.AppendLog(taskID, phase, content) 接口
// （LogStore 契约本身不区分「真实任务」与「公共桶」，用一个固定的、真实
// 任务 id 不可能撞上的字符串值即可复用同一接口，不需要给 LogStore 契约
// 加特殊分支）。
const syslogUnmatchedTaskID = "_syslog_unmatched"

// clientTaskEntry 记录一次「客户端 IP → 任务」关联的快照：任务 id 与该
// 关联发生时的阶段（用于落日志文件时的路径分类）。
type clientTaskEntry struct {
	taskID string
	phase  model.Phase
}

// rememberClientTask 记录一次客户端 IP 到任务的关联，供 syslog 接收器
// 后续查询。并发安全：/v1/boot、/v1/answer、/v1/signal 可能被不同 IP 的
// 机器同时触发。
func (s *Server) rememberClientTask(ip, taskID string, phase model.Phase) {
	if ip == "" || taskID == "" {
		return
	}
	s.clientTaskMu.Lock()
	defer s.clientTaskMu.Unlock()
	if s.clientTaskMap == nil {
		s.clientTaskMap = make(map[string]clientTaskEntry)
	}
	s.clientTaskMap[ip] = clientTaskEntry{taskID: taskID, phase: phase}
}

// lookupClientTask 查询某客户端 IP 当前关联的任务。ok 为 false 表示查不到
// （包括进程刚启动、映射为空的情形）。
func (s *Server) lookupClientTask(ip string) (clientTaskEntry, bool) {
	s.clientTaskMu.RLock()
	defer s.clientTaskMu.RUnlock()
	entry, ok := s.clientTaskMap[ip]
	return entry, ok
}

// handleSyslogLine 处理一条 syslog 原始行：按源 IP 归档到对应任务，
// 匹配不到的行落公共文件。本函数不做 RFC 3164/5424 结构化解析——契约
// 文档允许「尽力解析，解析失败仍整行落库」，本实现选择完全不解析（整行
// 原样落库），这本身就是「尽力解析」失败时的行为，不需要额外区分「解析
// 成功」与「解析失败」两条代码路径。
//
// 本函数绝不 panic、绝不阻塞、绝不返回错误给调用方——这是三条辅助通道
// 之一，接收失败或识别不出任务归属绝不能反向影响装机流程或阻塞信号
// 处理（SPEC §6.5）。AppendLog/AppendEvent 的错误被忽略而不是向上传播。
func (s *Server) handleSyslogLine(srcIP string, line []byte) {
	ctx := context.Background()
	content := string(line)

	taskID := syslogUnmatchedTaskID
	phase := model.Phase("")
	if entry, ok := s.lookupClientTask(srcIP); ok {
		taskID = entry.taskID
		phase = entry.phase
	}

	_ = s.deps.LogStore.AppendLog(ctx, taskID, phase, content)

	if taskID != syslogUnmatchedTaskID {
		// 只有匹配到真实任务时才写 TaskEvent——TaskEvent.TaskID 是必需
		// 字段，公共桶没有一个真实存在的任务可以挂靠，写一个假的 taskID
		// 进事件表只会污染那个真实任务（如果未来正好有一个同名任务）或
		// 产生一条永远查不到归属的孤儿事件，两种后果都不如干脆不写。
		_, _ = s.deps.EventStore.AppendEvent(ctx, model.TaskEvent{
			TaskID: taskID, Phase: phase, Level: model.EventLevelInfo,
			Message: content, Source: model.EventSourceSyslog,
		})
	}
}

// ListenAndServeSyslog 在 addr（形如 "10.0.0.1:514"）上监听 UDP，收到的
// 每个数据包整体作为一行交给 handleSyslogLine 处理。这是一个阻塞调用，
// 调用方应在独立的 goroutine 里启动它。
//
// 单条数据包读取失败（网络层错误）时记日志并继续循环，不退出监听——
// 装机 VLAN 上任何一次异常都不应导致整个 syslog 通道永久失效。
func (s *Server) ListenAndServeSyslog(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return err
	}
	defer conn.Close()

	buf := make([]byte, 64*1024)
	for {
		n, srcAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			slog.Default().Warn("syslog: 读取 UDP 数据包失败", slog.Any("error", err))
			continue
		}
		srcIP := ""
		if srcAddr != nil {
			srcIP = srcAddr.IP.String()
		}
		line := make([]byte, n)
		copy(line, buf[:n])
		s.handleSyslogLine(srcIP, line)
	}
}
