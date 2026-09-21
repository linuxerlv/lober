// 本文件测试 TokenIssuer 实现（任务 2）。锁住：不可预测（一千个无重复）、
// URL 安全字符、长度稳定。
package statemachine

import (
	"context"
	"strings"
	"testing"
)

// TestTokenIssueOneThousandUnique 连续生成一千个 token，验证无重复——
// 抓的是「误用了非加密随机源」或「忘了每次重新读随机字节」这类会导致
// 可预测或重复输出的 bug。
func TestTokenIssueOneThousandUnique(t *testing.T) {
	issuer := &Machine{}
	seen := make(map[string]bool, 1000)
	for i := 0; i < 1000; i++ {
		tok, err := issuer.IssueToken(context.Background())
		if err != nil {
			t.Fatalf("IssueToken 第 %d 次调用失败: %v", i, err)
		}
		if seen[tok] {
			t.Fatalf("第 %d 次生成的 token %q 与之前某次重复 —— "+
				"token 是删掉 armed 后唯一的执行许可（SPEC §4.4），"+
				"可预测或重复的 token 等于没有 token", i, tok)
		}
		seen[tok] = true
	}
}

// TestTokenOutputOnlyURLSafeChars 验证输出只含 URL 安全字符（无 + / =），
// 因为 token 要直接嵌进 URL 路径段（SPEC §4.4：Ubuntu 的 ds=nocloud-net;
// s=<url> 要求整段可以是路径的一部分，标准 base64 的 + 与 / 需要转义）。
func TestTokenOutputOnlyURLSafeChars(t *testing.T) {
	issuer := &Machine{}
	for i := 0; i < 20; i++ {
		tok, err := issuer.IssueToken(context.Background())
		if err != nil {
			t.Fatalf("IssueToken 失败: %v", err)
		}
		if strings.ContainsAny(tok, "+/=") {
			t.Fatalf("token %q 含有非 URL 安全字符（+ / =）—— "+
				"token 走 URL 路径段（SPEC §4.4），标准 base64 的这些字符"+
				"在路径里需要转义，必须用 base64url 编码", tok)
		}
	}
}

// TestTokenLengthStable 验证长度稳定：固定字节数的 crypto/rand 输出经
// base64url 编码后长度应恒定，长度浮动通常意味着编码方式或字节数不稳定。
func TestTokenLengthStable(t *testing.T) {
	issuer := &Machine{}
	first, err := issuer.IssueToken(context.Background())
	if err != nil {
		t.Fatalf("IssueToken 失败: %v", err)
	}
	wantLen := len(first)
	for i := 0; i < 20; i++ {
		tok, err := issuer.IssueToken(context.Background())
		if err != nil {
			t.Fatalf("IssueToken 失败: %v", err)
		}
		if len(tok) != wantLen {
			t.Errorf("第 %d 次生成的 token 长度 = %d, want %d —— 长度必须稳定，"+
				"浮动意味着底层随机字节数或编码方式不稳定", i, len(tok), wantLen)
		}
	}
}
