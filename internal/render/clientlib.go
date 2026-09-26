// 本文件实现注入到目标机器上的两个 shell 函数：signal 与 run_hook（对应
// 实现计划任务 3，依据 SPEC §6.2、§6.3）。
//
// installer ramdisk 里没有 python、没有 jq，curl 也可能只是 busybox 版，
// 因此上报能力必须由渲染器注入的这两个函数承担。函数正文作为 Go 字符串
// 拼出，其中的服务器地址、任务 id、token 从渲染上下文填入——不出现字面
// scheme（SPEC §1.5）。
package render

import (
	"fmt"

	"github.com/lober/lober/internal/core/model"
)

// ClientLib 渲染 signal 与 run_hook 两个函数定义的完整文本，供拼进 kickstart
// 序言段（早于任何用户 hook 调用，计划任务 3 设计要点）。
//
// signal 的七条行为要求：
//  1. 接收阶段名、退出码、日志文件路径三个参数，另加第四个可选的 hook digest
//  2. 自增序号变量 SEQ，拼进 query（(task, seq) 是去重键）
//  3. digest 非空时才拼进 query，为空时 query 里完全不出现该参数
//  4. 重试三次，每次之间退避（第 i 次等 3i 秒）
//  5. 用 curl --data-binary @文件发纯文本 body
//  6. 三次都失败则落 syslog 兜底
//  7. 最后返回成功码（遥测失败绝不能杀装机，这条刻意且不能改）
//
// run_hook 的六条行为要求：
//  1. 按 digest 从制品服务器取脚本
//  2. 用 sha256sum -c 校验下载到的内容
//  3. 用 timeout 包裹执行，超时值来自 hook 定义（调用时传入，不是硬编码常量）
//  4. 用 tee 同时留日志与输出
//  5. 用管道状态数组 PIPESTATUS 取真实退出码（经典陷阱：直接取 $? 拿到的
//     是 tee 的退出码，永远是 0）
//  6. 上报 hook-report（带 digest），按 required 决定是否中止
// 外部命令依赖（这段脚本跑在四种差异很大的环境里，缺一个的症状统一是
// rc=127「command not found」，所以逐个记清楚）：
//
//	curl        必需。signal 与 run_hook 都靠它。anaconda installer、
//	            debian-installer、装好的目标系统都有。
//	sha256sum   必需。run_hook 校验 hook 正文用（内容寻址的完整性保证）。
//	            实测在 anaconda installer 里可用。
//	logger      best-effort。signal 三次重试都失败时往 syslog 记一笔；
//	            没有它只是少一条本地日志，不影响流程（那一行的失败被
//	            signal 的 `return 0` 吞掉）。
//	timeout     **可选**。coreutils 的一部分，anaconda 的 installer ramdisk
//	            里没有（2026-09-25 实测：hook 脚本正确取到并校验通过，
//	            执行时 rc=127，服务端收到的日志正文只有一行
//	            "/tmp/ks-script-xxx: line 27: timeout: command not found"）。
//	            run_hook 因此先 command -v 探测：有就用，没有就裸跑并往
//	            syslog 记一笔——宁可失去超时保护也不能让 hook 整体跑不起来。
//
// 裸跑的代价要说清楚：hook 是运维写的任意 shell，一个 while true 或一次网络
// 长挂会让当前阶段卡住，直到 SPEC §3.3 的阶段预算耗尽（pre 是 10 分钟）。
// 那时失败原因是「阶段超时」而非「hook 超时」，排查要多绕一层。有 timeout 的
// 环境（装好的目标系统、firstboot 阶段）不受这个影响。

func ClientLib(ctx model.RenderContext) string {
	gw := ctx.Bases.GatewayURL()
	art := ctx.Bases.ArtifactURL()

	return fmt.Sprintf(`GW='%s'
ART='%s'
TASK='%s'
TOKEN='%s'
SEQ=0

signal() {
  local phase="$1" rc="${2:-0}" log="${3:-/dev/null}" digest="${4:-}"
  SEQ=$((SEQ+1))
  local q="$GW/v1/signal/$TASK/$TOKEN/$phase?rc=$rc&seq=$SEQ"
  if [ -n "$digest" ]; then
    q="$q&digest=$digest"
  fi
  for i in 1 2 3; do
    curl -fsS -m 10 -X POST "$q" \
      -H 'Content-Type: text/plain' --data-binary "@$log" && return 0
    sleep $((i*3))
  done
  logger -t lober "signal failed: $phase rc=$rc"
  return 0
}

run_hook() {
  local digest="$1" required="$2" timeout_sec="$3"
  curl -fsS -o /tmp/h.sh "$ART/hooks/$digest/script.sh" || return 1
  echo "$digest  /tmp/h.sh" | sha256sum -c - || return 1
  # timeout(1) 是 coreutils 的一部分，anaconda 的 installer ramdisk 里没有
  # （实测 rc=127: timeout: command not found）。有就用，没有就裸跑——
  # 宁可失去超时保护也不能让 hook 整体跑不起来。
  if command -v timeout >/dev/null 2>&1; then
    ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
  else
    logger -t lober "no timeout(1) in this environment, running hook $digest without time limit"
    ( set -euo pipefail; bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
  fi
  local rc=${PIPESTATUS[0]}
  signal "hook-report" "$rc" /tmp/h.log "$digest"
  [ "$rc" -eq 0 ] || [ "$required" != "true" ]
}
`, gw, art, ctx.Task.ID, ctx.InstallToken)
}
