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
  ( set -euo pipefail; timeout "$timeout_sec" bash /tmp/h.sh ) 2>&1 | tee /tmp/h.log
  local rc=${PIPESTATUS[0]}
  signal "hook-report" "$rc" /tmp/h.log "$digest"
  [ "$rc" -eq 0 ] || [ "$required" != "true" ]
}
`, gw, art, ctx.Task.ID, ctx.InstallToken)
}
