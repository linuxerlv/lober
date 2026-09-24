// 本文件实现首跳脚本端点 GET /boot.ipxe。对应实现计划任务 3，依据
// docs/contracts/http-machine.md 端点 1、SPEC §4.1、§4.7。
//
// DHCP 给出的 URL 必须是静态的，所以第一个响应是极小的静态引导片，它负责
// 把机器身份塞进第二跳的 query（§4.1）。全网共用一份，不看机器身份、不做
// 任何决策——这正是「加一个新 OS 家族不需要改 DHCP 与 nginx」的原因
// （§4.7：分叉全部收敛在 /v1/boot）。
//
// 五个变量的作用：sn/mac/uuid 三个是身份查找键（§4.3），arch/plat 两个是
// §4.6 一致性校验的输入——它们不是冗余留痕，是第二层分流的必要输入。
package gateway

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
)

// bootScriptTemplate 是 GET /boot.ipxe 的响应体模板：shebang 加一行 chain
// 命令。chain 的 URL 指向引导决策端点，query 里带五个 iPXE 变量。
//
// 用 fmt.Sprintf 做唯一的一处替换（网关基址），而不是 text/template——
// 五个 iPXE 变量本身是 "${serial}" 这样的固件占位符语法，要留给机器在实际
// 发出请求时替换成真实值。fmt.Sprintf 只识别 % 前缀的动词，"${...}" 对它
// 完全是普通字符，不存在被误当成模板语法处理的风险；如果换成 text/template
// 之类的引擎，需要额外小心 "${" 会不会与该引擎自己的定界符冲突——用最朴素
// 的 fmt.Sprintf 从写法上排除这个疑虑。
//
// 失败回退到 shell：chain 命令后跟 " || shell"，chain 本身几乎不会失败
// （它只是发一个 HTTP 请求），但保留这个分支是 iPXE 脚本的标准写法——
// 万一网络第一跳就不可达，好歹留一个交互式提示符而不是脚本静默终止。
const bootScriptTemplate = "#!ipxe\n" +
	"chain %s/v1/boot?sn=${serial}&mac=${net0/mac}&uuid=${uuid}&arch=${buildarch}&plat=${platform} || shell\n"

// handleBootScript 处理 GET /boot.ipxe：全网共用一份、静态文本，不看机器
// 身份、不做任何决策（§4.7：分叉全部收敛在 /v1/boot）。
//
// 本端点不存在运行期错误路径（docs/contracts/http-machine.md 端点 1
// 「错误行为」：它不查库、不做任何判断，渲染失败在部署期就会被发现）。
//
// 用 c.Data 而不是 c.String：body 本身虽然此刻不含 %，但这里的格式化
// （bootScriptTemplate 的唯一一处 %s 替换）已经在 fmt.Sprintf 里完成，
// 交给 c.Data 的是已经拼好的最终字节序列，不需要 gin 再做一次格式化——
// 这与 answer.go/facts.go/signal.go 统一遵循「c.String 的第二参数会被当
// 格式串」这条迁移契约 §6 的红线（gin 迁移最容易静默出错的一点）保持
// 同一种写法，即使本端点当前不含 %，也不给未来的模板改动留隐患。
func (s *Server) handleBootScript(c *gin.Context) {
	// 网关地址从配置的基址来，不写死（§1.5）。
	body := fmt.Sprintf(bootScriptTemplate, s.deps.Bases.GatewayBase)
	c.Data(http.StatusOK, "text/plain; charset=utf-8", []byte(body))
}
