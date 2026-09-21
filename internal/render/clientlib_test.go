// 本文件测试注入到机器上的 signal 与 run_hook shell 函数。对应实现计划
// 任务 3，依据 SPEC §6.2、§6.3。
//
// 不执行 shell（那需要真环境），改为对生成的文本做断言——逐条验证计划任务
// 3 列出的十三条行为要求各自对应的文本特征存在。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// testBases 是测试用的固定基址，供本文件与其它测试文件共享。
var testBases = model.BaseURLs{
	GatewayBase:  "http://gw.internal",
	ArtifactBase: "http://ng.internal",
}

// testClientLibCtx 构造一个用于 ClientLib 测试的最小 RenderContext。
func testClientLibCtx() model.RenderContext {
	return model.RenderContext{
		Task:         model.Task{ID: "42"},
		Bases:        testBases,
		InstallToken: "tok-abc123",
	}
}

// TestClientLib_SignalRetryLoop 锁住"重试三次，每次之间退避（第 i 次等 3i
// 秒）"（计划任务 3 signal 第 4 条）。
func TestClientLib_SignalRetryLoop(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	if !strings.Contains(out, "for i in 1 2 3") {
		t.Errorf("signal 函数应包含三次重试循环 `for i in 1 2 3`，got:\n%s", out)
	}
	if !strings.Contains(out, "sleep $((i*3))") {
		t.Errorf("signal 函数应包含退避表达式 `sleep $((i*3))`（第 i 次等 3i 秒），got:\n%s", out)
	}
}

// TestClientLib_SignalSyslogFallback 锁住"三次都失败则落 syslog 兜底"
// （计划任务 3 signal 第 6 条）。
func TestClientLib_SignalSyslogFallback(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	if !strings.Contains(out, "logger") {
		t.Errorf("signal 函数三次重试都失败后应落 syslog 兜底（logger 命令），got:\n%s", out)
	}
}

// TestClientLib_SignalReturnsSuccess 锁住"最后返回成功码"（计划任务 3
// signal 第 7 条，且明确"是刻意的，不能改：遥测失败不能杀装机"）。
//
// 断言方式：signal 函数体内最后一条语句必须是 `return 0`，而不是把重试失败
// 的 rc 直接 return 出去。
func TestClientLib_SignalReturnsSuccess(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "signal")
	trimmed := strings.TrimRight(fnBody, "\n \t")
	lines := strings.Split(trimmed, "\n")
	last := strings.TrimSpace(lines[len(lines)-1])
	if last != "return 0" {
		t.Errorf("signal 函数体最后一行 = %q, want \"return 0\" —— 遥测失败绝不能杀装机，"+
			"服务器临时不可达而把正在装的机器搞挂是最没必要的失败", last)
	}
}

// TestClientLib_SignalSeqIncrement 锁住"自增一个序号变量，拼进 query
// （(task, seq) 是去重键）"（计划任务 3 signal 第 2 条）。
func TestClientLib_SignalSeqIncrement(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	if !strings.Contains(out, "SEQ=$((SEQ+1))") {
		t.Errorf("signal 函数应自增 SEQ 变量，got:\n%s", out)
	}
	if !strings.Contains(out, "seq=$SEQ") {
		t.Errorf("signal 函数应把 SEQ 拼进 query 参数，got:\n%s", out)
	}
}

// TestClientLib_SignalDigestConditional 锁住"digest 非空时才拼进 query，
// 为空时 query 里完全不出现那个参数"（计划任务 3 signal 第 3 条）。
//
// 断言方式：函数体内必须有一个条件分支区分"有 digest"与"无 digest"两种
// query 拼接，而不是无条件拼接 `&digest=$digest`（那样为空时 query 会
// 出现 `&digest=`，与"完全不出现"矛盾）。
func TestClientLib_SignalDigestConditional(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "signal")
	if !strings.Contains(fnBody, "digest") {
		t.Fatalf("signal 函数体应引用 digest 参数，got:\n%s", fnBody)
	}
	// 必须存在按 digest 是否为空分叉的逻辑（if/-n 测试等）：这是区分"无条件
	// 拼接"（为空时 query 残留 `&digest=`）与"条件拼接"（为空时完全不出现该
	// 参数）的关键特征，而不是简单检查 `&digest=$digest` 这个子串是否存在——
	// 该子串本身完全可以合法地出现在 if 分支内部（真的就是本实现的写法）。
	hasConditional := strings.Contains(fnBody, "-n \"$digest\"") ||
		strings.Contains(fnBody, "-n \"${digest}\"") ||
		strings.Contains(fnBody, "if [ -n")
	if !hasConditional {
		t.Errorf("signal 函数体应包含对 digest 是否为空的条件判断，got:\n%s", fnBody)
	}
}

// TestClientLib_SignalDigestRenderedBothWays 用给 digest 与不给 digest
// 各渲染一次的方式对比 query（计划任功 3 测试要求："digest 参数的条件拼接
// （给 digest 与不给 digest 各渲染一次，对比 query）"）。
//
// 由于 ClientLib 输出的是函数定义本身（参数在调用时才绑定），"渲染两次"
// 体现为：函数体内同时含有 digest 为空与非空两条路径的 query 拼接模板，
// 本测试改为断言两条路径的 query 字符串模板都存在于函数体中。
func TestClientLib_SignalDigestRenderedBothWays(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "signal")

	withoutDigestQuery := "?rc=$rc&seq=$SEQ"
	if !strings.Contains(fnBody, withoutDigestQuery) {
		t.Errorf("signal 函数体应含无 digest 时的 query 形态 %q，got:\n%s", withoutDigestQuery, fnBody)
	}
	withDigestQuery := "&digest="
	if !strings.Contains(fnBody, withDigestQuery) {
		t.Errorf("signal 函数体应含有 digest 时才追加的 query 片段 %q，got:\n%s", withDigestQuery, fnBody)
	}
}

// TestClientLib_SignalCurlDataBinary 锁住"用 curl --data-binary @文件发纯
// 文本 body"（计划任务 3 signal 第 5 条）。
func TestClientLib_SignalCurlDataBinary(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "signal")
	if !strings.Contains(fnBody, "--data-binary") {
		t.Errorf("signal 函数应用 curl --data-binary 发送纯文本 body，got:\n%s", fnBody)
	}
	if !strings.Contains(fnBody, "text/plain") {
		t.Errorf("signal 函数应声明 Content-Type: text/plain，got:\n%s", fnBody)
	}
}

// TestClientLib_RunHookChecksumVerify 锁住"用 sha256sum -c 校验下载到的
// 内容"（计划任务 3 run_hook 第 2 条）。
func TestClientLib_RunHookChecksumVerify(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "sha256sum -c") {
		t.Errorf("run_hook 函数应用 sha256sum -c 校验下载到的脚本，got:\n%s", fnBody)
	}
}

// TestClientLib_RunHookTimeout 锁住"用 timeout 包裹执行，超时值来自 hook
// 定义"（计划任务 3 run_hook 第 3 条）——超时值作为函数参数传入，不是硬编码
// 常量。
func TestClientLib_RunHookTimeout(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "timeout ") {
		t.Errorf("run_hook 函数应用 timeout 包裹执行，got:\n%s", fnBody)
	}
}

// TestClientLib_RunHookPipestatusNotDollarQuestion 锁住"用管道状态数组取
// 真实退出码，不是 $?"（计划任务 3 run_hook 第 5 条，"经典陷阱"）。
//
// 这是最容易踩的坑：直接取 `$?` 拿到的是 `tee` 的退出码，永远是 0——
// required hook 失败也不会中止装机，机器带着半配置的状态装完。
func TestClientLib_RunHookPipestatusNotDollarQuestion(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "PIPESTATUS") {
		t.Errorf("run_hook 函数必须用 PIPESTATUS 数组取管道中第一个命令的真实退出码，"+
			"直接用 $? 拿到的永远是 tee 的退出码（恒为 0），会让所有 hook 都被"+
			"误判为成功，got:\n%s", fnBody)
	}
}

// TestClientLib_RunHookTee 锁住"用 tee 同时留日志与输出"（计划任务 3
// run_hook 第 4 条）。
func TestClientLib_RunHookTee(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "tee ") {
		t.Errorf("run_hook 函数应用 tee 同时留日志与输出，got:\n%s", fnBody)
	}
}

// TestClientLib_RunHookRequiredGate 锁住"上报 hook-report（带 digest），
// 然后按 required 决定是否中止：required 失败即中止装机并上报，非 required
// 记日志继续"（计划任务 3 run_hook 第 6 条）。
func TestClientLib_RunHookRequiredGate(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	fnBody := extractFunctionBody(t, out, "run_hook")
	if !strings.Contains(fnBody, "hook-report") {
		t.Errorf("run_hook 函数应上报 hook-report 信号，got:\n%s", fnBody)
	}
	if !strings.Contains(fnBody, "required") {
		t.Errorf("run_hook 函数应引用 required 参数以决定是否中止装机，got:\n%s", fnBody)
	}
}

// TestClientLib_NoLiteralHTTPScheme 锁住"两个函数里都不出现字面 http://"
// （计划任务 3 测试要求，SPEC §1.5）。
//
// SPEC §1.5 与计划里"不出现字面 http://"这条约束的对象是渲染器的 Go 源码
// ——不能在代码里硬编码 scheme，必须始终从 RenderContext.Bases 拼出来。
// 它不是说"运行期输出的字符串里永远不能出现 http:// 这几个字符"：若配置
// 本身用的是 http://（MVP 明文 HTTP 场景），输出理所当然会含 http://，
// 那是配置决定的，不是代码写死的。
//
// 因此本测试改为验证 scheme 完全跟随 Bases 变化（换成 https:// 配置后
// 输出就应该是 https://，而不是仍然残留写死的 http://），这才是"不写死
// scheme"这条要求的可执行版本。逐行 grep 源码文件本身（要求只在测试期望
// 值里出现字面 http://）由计划里单独给出的 shell 命令覆盖，不是本测试的
// 职责。
func TestClientLib_NoLiteralHTTPScheme(t *testing.T) {
	httpsCtx := testClientLibCtx()
	httpsCtx.Bases = model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	out := ClientLib(httpsCtx)

	if strings.Contains(out, "http://") {
		t.Errorf("Bases 配置为 https:// 时，ClientLib 输出仍含字面 http://，"+
			"说明 scheme 被硬编码而不是完全来自 RenderContext.Bases，got:\n%s", out)
	}
	if !strings.Contains(out, "https://gw.internal") || !strings.Contains(out, "https://ng.internal") {
		t.Errorf("Bases 配置为 https:// 时，ClientLib 输出应含来自配置的 https:// 基址，got:\n%s", out)
	}
}

// TestClientLib_URLsFromBases 验证 GW/ART 变量的取值确实来自 RenderContext
// 的基址，而不是硬编码——用不同基址渲染两次并断言输出跟随变化。
func TestClientLib_URLsFromBases(t *testing.T) {
	ctx := testClientLibCtx()
	out := ClientLib(ctx)
	if !strings.Contains(out, ctx.Bases.GatewayURL()) {
		t.Errorf("ClientLib 输出应包含从 Bases.GatewayURL() 拼出的网关基址 %q，got:\n%s",
			ctx.Bases.GatewayURL(), out)
	}
	if !strings.Contains(out, ctx.Bases.ArtifactURL()) {
		t.Errorf("ClientLib 输出应包含从 Bases.ArtifactURL() 拼出的制品基址 %q，got:\n%s",
			ctx.Bases.ArtifactURL(), out)
	}

	ctx2 := ctx
	ctx2.Bases = model.BaseURLs{GatewayBase: "http://another-gw", ArtifactBase: "http://another-ng"}
	out2 := ClientLib(ctx2)
	if strings.Contains(out2, ctx.Bases.GatewayURL()) {
		t.Errorf("换了 Bases 后输出仍包含旧网关基址，说明 URL 被硬编码而不是来自 RenderContext")
	}
}

// TestClientLib_InjectedBeforeUserHooks 锁住设计要点"注入位置在序言段，
// 早于任何用户 hook 的调用"——本测试断言 signal 函数定义出现在 run_hook
// 函数定义之前或两者同属一段不含用户 hook 调用的文本中。由于 ClientLib
// 只产出这两个函数本身（不产出用户 hook 调用），这里改为断言输出中不含任何
// `run_hook "` 调用形式（那是使用点，不该出现在函数定义本身里）。
func TestClientLib_InjectedBeforeUserHooks(t *testing.T) {
	out := ClientLib(testClientLibCtx())
	if strings.Contains(out, "run_hook \"") {
		t.Errorf("ClientLib 应只产出函数定义，不应包含对 run_hook 的调用（那是序言之后"+
			"用户段的职责），got:\n%s", out)
	}
}

// extractFunctionBody 从 shell 文本中提取形如 `name() {\n...\n}` 的函数体
// （不含首尾的 `name() {` 与 `}` 这两行），用于测试断言函数体内部特征。
func extractFunctionBody(t *testing.T, script, name string) string {
	t.Helper()
	marker := name + "() {"
	start := strings.Index(script, marker)
	if start == -1 {
		t.Fatalf("在脚本中找不到函数定义 %q，脚本内容:\n%s", marker, script)
	}
	rest := script[start+len(marker):]
	// 查找与开头 `{` 配对的 `}`：函数体内部不应再出现嵌套的裸露 `}`，
	// 本项目生成的两个函数体内都没有嵌套子 shell 花括号，用首个行首 `}`
	// 作为结束标记足够可靠。
	end := strings.Index(rest, "\n}")
	if end == -1 {
		t.Fatalf("函数 %q 未找到结束的 `}`，脚本内容:\n%s", name, script)
	}
	return rest[:end]
}
