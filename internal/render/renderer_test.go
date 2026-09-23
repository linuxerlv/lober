// 本文件测试 contract.Renderer 的入口分派：文件名校验（validateAnswerFileName）
// 与 RenderAnswerFile 按家族/文件名的分派逻辑，重点覆盖本次改动新增的 ESXi
// ks.cfg 放行（与 renderer.go 已有的 Ubuntu meta-data 放行同构，对应 SPEC
// §4.4、§4.5，与 esxi.go 文件末尾"契约缺口 1"记录的正解方向第 1 条）。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// validateAnswerFileName：ESXi 的 ks.cfg 放行
// ---------------------------------------------------------------------------

// TestValidateAnswerFileName_ESXiBootCfgAllowed 锁住：ESXi 请求
// ctx.AnswerFileName() 本来的返回值 "boot.cfg" 仍然通过校验（放行 ks.cfg
// 是新增的第二条路径，不能顶替掉本来就该放行的第一条）。
func TestValidateAnswerFileName_ESXiBootCfgAllowed(t *testing.T) {
	ctx := esxiKickstartCtx()
	if err := validateAnswerFileName(ctx, "boot.cfg"); err != nil {
		t.Errorf("ESXi 请求 boot.cfg 应通过校验，got err: %v", err)
	}
}

// TestValidateAnswerFileName_ESXiKsCfgAllowed 锁住：ESXi 家族额外放行
// "ks.cfg"——这是本次改动的核心，为什么重要见 renderer.go validateAnswerFileName
// 文档：boot.cfg 的 kernelopt= 行 ks= 参数指向的正是这份独立文件，不放行
// 会导致机器请求 ks.cfg 时拿到 404，装机在"取应答文件"这一步就失败。
func TestValidateAnswerFileName_ESXiKsCfgAllowed(t *testing.T) {
	ctx := esxiKickstartCtx()
	if err := validateAnswerFileName(ctx, "ks.cfg"); err != nil {
		t.Errorf("ESXi 请求 ks.cfg 应通过校验（与 Ubuntu 的 meta-data 同构放行），got err: %v", err)
	}
}

// TestValidateAnswerFileName_NonESXiKsCfgRejected 锁住：ks.cfg 的放行只对
// ESXi 生效——路径段来自机器请求，放宽给其它家族等于让机器自己指定要取
// 哪种应答文件。用 RHEL 验证（它的 AnswerFileName() 恰好也是 "ks.cfg"，
// 这里刻意验证的是"家族必须是 ESXi"这条约束，不是文件名本身长得像什么）。
func TestValidateAnswerFileName_NonESXiKsCfgRejected(t *testing.T) {
	ctx := model.RenderContext{Artifact: model.ArtifactSet{OSFamily: model.OSFamilyDebian}}
	err := validateAnswerFileName(ctx, "ks.cfg")
	if err == nil {
		t.Fatal("Debian 请求 ks.cfg 应被拒绝（ks.cfg 放行只对 ESXi 生效），got nil error")
	}
}

// TestValidateAnswerFileName_ESXiWrongNameRejected 锁住：ESXi 请求既不是
// boot.cfg 也不是 ks.cfg 的第三个名字仍被拒绝——放行必须窄，只多放行这一个
// 具体名字，不是任意名字。
func TestValidateAnswerFileName_ESXiWrongNameRejected(t *testing.T) {
	ctx := esxiKickstartCtx()
	err := validateAnswerFileName(ctx, "kickstart.cfg")
	if err == nil {
		t.Fatal("ESXi 请求 kickstart.cfg 应被拒绝，got nil error")
	}
}

// ---------------------------------------------------------------------------
// RenderAnswerFile：ESXi 分派——取 ks.cfg 拿到 kickstart 正文
// ---------------------------------------------------------------------------

// TestRenderAnswerFile_ESXiKsCfg_ReturnsKickstartBody 锁住核心行为：请求
// ks.cfg 时 RenderAnswerFile 产出的是 AssembleESXiKickstart 的正文（含
// install 指令等 ESXi kickstart 特征），不是 mboot.efi 的引导参数文件语法。
// 这是"要测试锁住"清单第二条的最小可复现单元测试版本。
func TestRenderAnswerFile_ESXiKsCfg_ReturnsKickstartBody(t *testing.T) {
	r := &Renderer{}
	ctx := esxiKickstartCtx()

	out, err := r.RenderAnswerFile(ctx, "ks.cfg")
	if err != nil {
		t.Fatalf("RenderAnswerFile(ctx, \"ks.cfg\") 返回错误: %v", err)
	}
	if !strings.Contains(out, "install ") {
		t.Errorf("取 ks.cfg 应拿到 kickstart 正文（含 'install ' 指令），got:\n%s", out)
	}
	if strings.Contains(out, "prefix=") || strings.Contains(out, "kernelopt=") {
		t.Errorf("取 ks.cfg 不应含 boot.cfg 的引导参数语法（prefix=/kernelopt=），got:\n%s", out)
	}

	// 与直接调用 AssembleESXiKickstart 的结果一致——RenderAnswerFile 对
	// ks.cfg 只是分派，不应产出不同的内容。
	direct, err := AssembleESXiKickstart(ctx)
	if err != nil {
		t.Fatalf("AssembleESXiKickstart 返回错误: %v", err)
	}
	if out != direct {
		t.Errorf("RenderAnswerFile(ctx, \"ks.cfg\") 的结果应与直接调用 AssembleESXiKickstart 完全一致\ngot:  %q\nwant: %q", out, direct)
	}
}

// TestRenderAnswerFile_ESXiBootCfg_StillReturnsKickstartBody 锁住"仍未修复
// 的已知契约缺口"这一事实本身，防止将来有人以为它已经被静默修好：请求
// boot.cfg（ctx.AnswerFileName() 的本来返回值）目前仍然拿到 kickstart 正文
// 而不是引导参数文件——这不是本次改动引入的新行为，是 renderer.go 文件末尾
// contractGaps 记录的既有缺口，本测试确认它没有被本次改动意外改得更糟（也
// 没有被误当作"顺手就修了"）。
//
// 若将来 gateway.Deps 补上制品根目录字段并接通 RenderESXiBootCfg（见
// renderer.go 文件末尾 contractGaps 的正解方向），本测试需要跟着更新为
// 断言相反的内容——那时它的失败是期望之内的信号，说明修复已经生效。
func TestRenderAnswerFile_ESXiBootCfg_StillReturnsKickstartBody(t *testing.T) {
	r := &Renderer{}
	ctx := esxiKickstartCtx()

	out, err := r.RenderAnswerFile(ctx, "boot.cfg")
	if err != nil {
		t.Fatalf("RenderAnswerFile(ctx, \"boot.cfg\") 返回错误: %v", err)
	}
	if !strings.Contains(out, "install ") {
		t.Errorf("已知契约缺口：boot.cfg 目前仍分派到 AssembleESXiKickstart，"+
			"应含 'install ' 指令，got:\n%s", out)
	}
}

// TestRenderAnswerFile_NonESXiFamily_KsCfgRejected 锁住：非 ESXi 家族请求
// ks.cfg 仍然在文件名校验这一步就被拒绝（不会走到任何家族分支）——这是
// "测试要锁住"清单第四条。
func TestRenderAnswerFile_NonESXiFamily_KsCfgRejected(t *testing.T) {
	r := &Renderer{}
	ctx := model.RenderContext{Artifact: model.ArtifactSet{OSFamily: model.OSFamilyRHEL}}

	_, err := r.RenderAnswerFile(ctx, "ks.cfg")
	if err == nil {
		t.Fatal("RHEL 请求 ks.cfg 应返回错误（ks.cfg 放行只对 ESXi 生效），got nil")
	}
}

// ---------------------------------------------------------------------------
// golden file：RenderAnswerFile 对 ks.cfg 的产出（覆盖两种产出之一）
// ---------------------------------------------------------------------------

// TestGolden_RenderAnswerFile_ESXiKsCfg 覆盖"golden file 覆盖两种产出"这条
// 测试要求里的 ks.cfg 一侧——复用 esxi_test.go 的 esxiKickstartCtx 与
// esxi_kickstart_full.golden（TestGolden_ESXiKickstart 已经锁住的同一份
// golden），确认经 RenderAnswerFile 分派得到的结果与直接调用
// AssembleESXiKickstart 逐字节一致，不需要另存一份 golden 文件。
func TestGolden_RenderAnswerFile_ESXiKsCfg(t *testing.T) {
	r := &Renderer{}
	ctx := esxiKickstartCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}

	out, err := r.RenderAnswerFile(ctx, "ks.cfg")
	if err != nil {
		t.Fatalf("RenderAnswerFile(ctx, \"ks.cfg\") 返回错误: %v", err)
	}
	assertGolden(t, "esxi_kickstart_full.golden", out)
}
