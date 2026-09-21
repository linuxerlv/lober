// 本文件测试入口分派逻辑与端到端注入防护（对应实现计划任务 8，依据 SPEC
// §5.1、§5.6、§9.5）。
//
// 前者是 contract.Renderer 接口实现的分派正确性；后者是把任务 1 的校验器
// 与最终产物连起来验证——用一批恶意表单值走完整的渲染流程，断言渲染整体
// 失败且恶意内容不出现在任何产出里。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"

	"github.com/lober/lober/internal/core/contract"
)

// injectionTestCtx 构造一个走完整渲染流程用的最小合法 RenderContext（不含
// 恶意值），供各测试用例在其基础上注入恶意 ResolvedParams。
func injectionTestCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            "42",
			HostSN:        "SN-INJECT-0001",
			FinalizeToken: "finalize-tok-inject",
			ResolvedParams: map[string]string{
				"hostname": "web-01",
				"biz_ip":   "10.20.0.15",
			},
		},
		Template: model.Template{
			ID:             "tpl-rocky9-inject",
			AnswerTemplate: "%packages\n@core\n%end\n",
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilyRHEL,
			OSVersion: "9.4",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "rocky9.4/vmlinuz",
				Initrd: []string{"rocky9.4/initrd.img"},
				Repo:   "rocky9.4/repo/",
			},
		},
		Hooks: []model.HookRef{
			{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
			{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
		},
		Facts: map[string]string{
			model.FactTaskID:   "42",
			model.FactSN:       "SN-INJECT-0001",
			model.FactHostname: "web-01",
			model.FactBizIP:    "10.20.0.15",
		},
		Bases:         testBases,
		InstallToken:  "install-tok-inject",
		FinalizeToken: "finalize-tok-inject",
		SyslogHost:    "10.10.0.1",
		SyslogPort:    514,
	}
}

// TestRenderer_ImplementsContract 是编译期接口断言（计划任务 8："编译期
// 接口断言放文件末尾"）。本测试函数本身只是让编译期断言在运行 go test 时
// 也被明确验证一次——真正的断言在 renderer.go 文件末尾的 var _ 声明。
func TestRenderer_ImplementsContract(t *testing.T) {
	var _ contract.Renderer = (*Renderer)(nil)
}

// TestRenderer_AnswerFileNameMismatchRejected 锁住"应答文件名不匹配时返回
// 错误"（计划任务 8 测试要求第一条）。
func TestRenderer_AnswerFileNameMismatchRejected(t *testing.T) {
	r := &Renderer{}
	ctx := injectionTestCtx()

	_, err := r.RenderAnswerFile(ctx, "wrong-name.cfg")
	if err == nil {
		t.Error("RenderAnswerFile 传入与 ctx.AnswerFileName() 不一致的文件名时应返回错误，got nil —— " +
			"该参数来自机器请求的 URL 路径段，不校验会让机器通过改文件名拿到另一种格式的应答文件")
	}
}

// TestRenderer_AnswerFileNameMatchAccepted 验证文件名匹配时正常渲染。
func TestRenderer_AnswerFileNameMatchAccepted(t *testing.T) {
	r := &Renderer{}
	ctx := injectionTestCtx()

	name, ok := ctx.AnswerFileName()
	if !ok {
		t.Fatalf("ctx.AnswerFileName() 应能确定 RHEL 家族的文件名")
	}
	out, err := r.RenderAnswerFile(ctx, name)
	if err != nil {
		t.Fatalf("RenderAnswerFile(%q) 返回错误: %v，want nil", name, err)
	}
	if out == "" {
		t.Error("RenderAnswerFile 应返回非空的应答文件正文")
	}
}

// TestRenderer_UbuntuAcceptsMetaData 锁住"Ubuntu 额外接受 meta-data"
// （计划任务 8 测试要求第二条，SPEC §4.5：NoCloud 要求该目录下两个文件都
// 存在）。
//
// Ubuntu 家族的实际渲染在 MVP 范围内未实现（任务 7 已把 Ubuntu 划为
// "未实现"分支），因此本测试只锁住"文件名校验层面 meta-data 被当作合法
// 文件名接受，不会因为文件名不匹配被 404 拒绝"这一件事——它最终仍会因为
// Ubuntu 未实现而返回错误，但那个错误必须是"未实现"，不能是"文件名不
// 匹配"。两种错误对运维的含义完全不同：前者说"这个功能还没做"，后者说
// "机器在耍花招"，混淆会误导排查方向。
func TestRenderer_UbuntuAcceptsMetaData(t *testing.T) {
	r := &Renderer{}
	ctx := injectionTestCtx()
	ctx.Artifact.OSFamily = model.OSFamilyUbuntu
	ctx.Facts = cloneFacts(ctx.Facts)
	ctx.Facts[model.FactHostname] = "web-01"

	out, err := r.RenderAnswerFile(ctx, "meta-data")
	if err != nil {
		t.Fatalf("Ubuntu 家族的 meta-data 渲染在 phase2-01 已实现，RenderAnswerFile 不应返回错误: %v", err)
	}
	if out == "" {
		t.Error("meta-data 渲染结果不应为空")
	}

	// 对比：一个既不是 user-data 也不是 meta-data 的文件名，才应该在文件名
	// 校验层面被拒绝。
	_, err2 := r.RenderAnswerFile(ctx, "some-other-file")
	if err2 == nil {
		t.Fatal("Ubuntu 家族传入既非 user-data 也非 meta-data 的文件名时应返回错误")
	}
}

// injectionPayloads 是端到端注入测试要求覆盖的恶意值样本（计划任务 8：
// "恶意值至少覆盖：命令分隔符、命令替换的两种写法、换行加 kickstart 段落
// 标记、超长值、含引号的值"）。
var injectionPayloads = []struct {
	label string
	value string
}{
	{"命令分隔符", "web01; rm -rf /"},
	{"反引号命令替换", "web`rm -rf /`01"},
	{"$(...) 命令替换", "web$(rm -rf /)01"},
	{"换行加段落标记", "web01\n%end\n%post\nrm -rf /\n%end"},
	{"超长值", strings.Repeat("a", 10000)},
	{"含引号的值", `web'"01`},
}

// TestInjection_EndToEndRendersErrorNotPartialSuccess 锁住"渲染整体返回
// 错误（不是部分成功）"（计划任务 8 端到端注入测试要求第一条）。
func TestInjection_EndToEndRendersErrorNotPartialSuccess(t *testing.T) {
	r := &Renderer{}
	for _, payload := range injectionPayloads {
		t.Run(payload.label, func(t *testing.T) {
			ctx := injectionTestCtx()
			ctx.Task.ResolvedParams = map[string]string{
				"hostname": payload.value,
				"biz_ip":   "10.20.0.15",
			}

			name, _ := ctx.AnswerFileName()
			out, err := r.RenderAnswerFile(ctx, name)
			if err == nil {
				t.Errorf("恶意值类别 %q（hostname=%q）应导致 RenderAnswerFile 整体返回错误，"+
					"got nil error, out=%q", payload.label, payload.value, out)
			}
			if out != "" {
				t.Errorf("恶意值类别 %q 应导致渲染失败时不返回任何内容（不是部分成功），"+
					"got 非空输出:\n%s", payload.label, out)
			}
		})
	}
}

// TestInjection_MaliciousContentNeverLeaksIntoOutput 锁住"恶意内容不出现
// 在任何产出里——包括菜单与应答文件"（计划任务 8 端到端注入测试要求第二
// 条）。这一条抓的是"校验器漏了某个字段"或"某处绕过了校验器直接内插"。
//
// 由于上一条测试已经确认恶意值会导致整体渲染失败（output 为空字符串），
// 本测试从另一个角度加固同一个不变量：即使未来某次修改让渲染在恶意输入
// 下意外"成功"（这正是本测试要防的回归），产出中也绝不能出现恶意值的
// 原始文本。因此本测试直接比较 out 与 payload.value 的子串关系，而不是
// 依赖"err != nil"这个间接推论。
func TestInjection_MaliciousContentNeverLeaksIntoOutput(t *testing.T) {
	r := &Renderer{}
	for _, payload := range injectionPayloads {
		t.Run(payload.label+"_应答文件", func(t *testing.T) {
			ctx := injectionTestCtx()
			ctx.Task.ResolvedParams = map[string]string{
				"hostname": payload.value,
				"biz_ip":   "10.20.0.15",
			}
			name, _ := ctx.AnswerFileName()
			out, _ := r.RenderAnswerFile(ctx, name)
			if out != "" && strings.Contains(out, payload.value) {
				t.Errorf("恶意值类别 %q 的原始文本出现在应答文件产出中——校验器可能漏了"+
					"某个字段，或某处绕过校验器直接内插，got:\n%s", payload.label, out)
			}
		})

		t.Run(payload.label+"_菜单", func(t *testing.T) {
			ctx := injectionTestCtx()
			ctx.Task.ResolvedParams = map[string]string{
				"hostname": payload.value,
				"biz_ip":   "10.20.0.15",
			}
			out, _ := r.RenderMenu(ctx)
			if out != "" && strings.Contains(out, payload.value) {
				t.Errorf("恶意值类别 %q 的原始文本出现在菜单产出中，got:\n%s", payload.label, out)
			}
		})
	}
}

// TestInjection_BizIPMaliciousRejected 单独用非法 biz_ip（IP 类字段）走一遍
// 端到端流程，确认不同校验器类型都被端到端覆盖，不只是 hostname 一个字段。
func TestInjection_BizIPMaliciousRejected(t *testing.T) {
	r := &Renderer{}
	ctx := injectionTestCtx()
	ctx.Task.ResolvedParams = map[string]string{
		"hostname": "web-01",
		"biz_ip":   "10.0.0.1; rm -rf /",
	}
	name, _ := ctx.AnswerFileName()
	out, err := r.RenderAnswerFile(ctx, name)
	if err == nil {
		t.Error("非法 biz_ip 应导致 RenderAnswerFile 返回错误，got nil")
	}
	if out != "" {
		t.Errorf("非法 biz_ip 应导致渲染失败时不返回任何内容，got:\n%s", out)
	}
}
