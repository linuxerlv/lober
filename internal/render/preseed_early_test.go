// 本文件锁住 preseed/early_command 的值是一段合法 shell。
//
// **实测的失败模式（2026-09-26，Debian 那轮）**：d-i 取到了 preseed、netcfg 成功、
// preseed 加载成功，然后 early_command 一执行就死在
//
//	Sep 26 01:37:49 log-output: sh: syntax error: unexpected ";"
//
// 任务停在 installing/pre 直到 10 分钟阶段超时，一个信号都没发出来——连
// pre-start 都没有，因为 signal() 这个函数本身就在语法错误的那一段里。
//
// 根因：early_command 的值此前是把多行 shell 用 `; ` 拼成一行
// （inlineForSingleLine）。那个函数的注释写着「调用方负责保证被替换的文本本身是
// 可以按行分号连接执行的简单赋值/函数体」——而它的调用方传的是 ClientLib(ctx)，
// 里面是 `signal() { ... }` 与 `run_hook() { ... }` 两个含 if/for 的函数定义。
// 按行加分号之后变成
//
//	signal() {; local phase="$1" rc="${2:-0}"; ...
//
// `{` 后面紧跟 `;` 在 POSIX shell 里就是语法错误。这个前提从写下那天起就不成立，
// 只是在真机上跑过才暴露。
//
// late_command 一侧早就用的是 base64 落盘再执行（见 lateCommandValue 的注释：
// 「转义穿透 late_command 单行值的风险远高于直接 base64 编码后用 base64 -d
// 落盘」）。early_command 没跟上这个结论。
package render

import (
	"encoding/base64"
	"os/exec"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// TestPreseedEarlyCommand_IsValidShell 锁住 early_command 的值能被 shell 解析。
//
// 用 `sh -n`（只做语法检查、不执行）验证。这条断言的价值在于它能抓住任何
// 「拼接出非法 shell」的退化，而不只是本次这一种。
func TestPreseedEarlyCommand_IsValidShell(t *testing.T) {
	sh, err := exec.LookPath("sh")
	if err != nil {
		t.Skipf("环境里没有 sh，跳过语法校验: %v", err)
	}

	ctx := minimalPreseedCtx()
	ctx.Facts[model.FactDiskPrimary] = "/dev/disk/by-id/wwn-0xdeadbeef"

	value, err := earlyCommandValue(ctx)
	if err != nil {
		t.Fatalf("渲染 early_command 失败: %v", err)
	}

	cmd := exec.Command(sh, "-n")
	cmd.Stdin = strings.NewReader(value)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("early_command 的值不是合法 shell（%v）:\n%s\n\n值本身:\n%s",
			err, out, value)
	}
}

// TestPreseedEarlyCommand_ClientLibSurvivesIntact 锁住 clientlib 原文不被改写。
//
// base64 落盘的要点正是「正文一个字节都不动」。如果实现退回按行拼接，
// 函数定义里的换行就会变成 `; `，解码后的正文与原文不符。
func TestPreseedEarlyCommand_ClientLibSurvivesIntact(t *testing.T) {
	ctx := minimalPreseedCtx()
	ctx.Facts[model.FactDiskPrimary] = "/dev/disk/by-id/wwn-0xdeadbeef"

	value, err := earlyCommandValue(ctx)
	if err != nil {
		t.Fatalf("渲染 early_command 失败: %v", err)
	}

	decoded := decodeSoleBase64Blob(t, value)

	// clientlib 的函数定义必须以原样的多行形式存在——这是 base64 与按行拼接的
	// 分水岭：拼接会把 `signal() {` 后面的换行变成 `; `。
	if !strings.Contains(decoded, "signal() {\n") {
		t.Errorf("解码后的脚本里 signal() 的函数体不是多行原文——"+
			"说明实现又在按行拼接。解码结果前 500 字:\n%s", head(decoded, 500))
	}
	if strings.Contains(decoded, "{; ") {
		t.Errorf("解码后的脚本里出现 `{; `——`{` 后紧跟 `;` 在 POSIX shell 里是"+
			"语法错误，正是实测那次 `sh: syntax error: unexpected \";\"` 的成因。"+
			"解码结果前 500 字:\n%s", head(decoded, 500))
	}

	// pre 阶段首尾信号必须还在（它们是状态机推进的依据）。
	for _, want := range []string{`signal "pre-start" 0`, `signal "pre-done" 0`} {
		if !strings.Contains(decoded, want) {
			t.Errorf("解码后的脚本缺 %q", want)
		}
	}
}

// decodeSoleBase64Blob 从命令行里取出唯一的 base64 块并解码。
func decodeSoleBase64Blob(t *testing.T, value string) string {
	t.Helper()
	const marker = "echo "
	i := strings.Index(value, marker)
	if i < 0 {
		t.Fatalf("early_command 的值里找不到 %q——期望形如 "+
			"`echo <base64> | base64 -d > ... && sh ...`，实际:\n%s", marker, value)
	}
	rest := value[i+len(marker):]
	j := strings.IndexAny(rest, " |")
	if j < 0 {
		t.Fatalf("取不出 base64 块，值:\n%s", value)
	}
	decoded, err := base64.StdEncoding.DecodeString(rest[:j])
	if err != nil {
		t.Fatalf("base64 解码失败: %v（块: %.80s）", err, rest[:j])
	}
	return string(decoded)
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
