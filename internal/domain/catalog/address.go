package catalog

import (
	"net/url"
	"strings"
)

// 下面是机器面的两份约定（spec §4.1、§6.2 第 3 条）。地址格式：渲染按它写地址，网关按它注册路由。制品
// 目录布局：制品目录经 nginx 发布在一个基地址下，ISO.Artifacts 只存相对于它的路径，渲染据此拼出完整地址；
// 目录里怎么分层由第 4 层导入时定，并写进 ISO.Artifacts。两个基地址由装配点从配置构造一次，经系统值
// gateway 与 artifacts 交给渲染，所以这里只拼接，不知道主机名和端口。

// AnswerURL 是机器经网关取文件的地址：{base}/v1/answer/{task}/{token}/{file}（旧 SPEC.md §8.1）。token 放在
// 路径段里，不用查询串（旧 SPEC.md §4.4）：Ubuntu 的 ds=nocloud-net;s= 要以 / 结尾的目录地址，查询串会坏掉。
//
// file 是 DeliveryFileName 给的文件名，也可以带子路径，按 / 切开；为空时给出以 / 结尾的目录地址，就是 Ubuntu
// 的 s= 要的那个。task、token 与 file 的每一段各自转义（url.PathEscape），网关按段解开就拿回原值；base 结尾
// 的 / 去掉。
//
// 转义只保证按段解得回原值，不保证放进内核命令行、iPXE、cmd 或 sh 里不出事：转义出的 % 在 cmd 里会被当成
// 变量，PathEscape 原样留下的 & $ + = : @ 也一样危险，比如 & 会把 cmd 的命令切开，$ 在 sh 里会被展开。所以
// task 与 token 都只用 model.ValidID 的字符集（小写字母、数字与 -）：任务 ID 本来就合它，token 由签发方保证。
func AnswerURL(base, task, token, file string) string {
	return joinURL(base, append([]string{"v1", "answer", task, token}, strings.Split(file, "/")...))
}

// SignalURL follows the inherited POST /v1/signal/{task}/{token}/{phase}
// machine protocol. Transport parameters/log bodies belong to the runtime.
func SignalURL(base, task, token, phase string) string {
	return joinURL(base, []string{"v1", "signal", task, token, phase})
}

// ArtifactURL 是制品的完整地址：{base}/{rel}。rel 取自 ISO.Artifacts，是制品目录下的相对路径。rel 是盘上的
// 路径而不是转义过的地址，所以按 / 切开、逐段转义，结尾的 / 留着（仓库树是目录）；base 结尾的 / 去掉。
//
// rel 的合规（非空、不以 / 开头、没有 . 与 .. 段）由第 4 层写 ISO.Artifacts 时保证，ISO.Validate 不查 Artifacts，
// 这里也不查：rel 为空、以 / 开头或含 . 与 .. 段时照拼不误，既不规范化也不拒收。转义挡不住 ..（%2E%2E 解开
// 还是 ..），防目录穿越只能靠写 rel 的一方。
func ArtifactURL(base, rel string) string {
	return joinURL(base, strings.Split(rel, "/"))
}

// joinURL 把各段转义后依次接在 base 后面，段前各加一个 /。
func joinURL(base string, segs []string) string {
	var b strings.Builder
	b.WriteString(strings.TrimRight(base, "/"))
	for _, s := range segs {
		b.WriteByte('/')
		b.WriteString(url.PathEscape(s))
	}
	return b.String()
}
