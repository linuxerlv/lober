// 本文件测试 SUSE AutoYaST 应答文件（autoinst.xml）的组装（依据 SPEC
// §0.3、§4.5、§5.1、§5.2、§5.6、附录 B）。
//
// SUSE 用 AutoYaST，绝不是 kickstart：引导参数是 autoyast= 而不是
// inst.ks=，应答文件是独立的 XML 格式（model.AnswerFileName 已规定文件名
// 为 autoinst.xml）。本文件的测试与生产代码均不复用任何 kickstart 的模板
// 拼接逻辑——两者语法完全不同。
package render

import (
	"encoding/xml"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// autoyastTestCtx 构造一个用于 AutoYaST 单元测试的最小合法 RenderContext
// （不含 hook，含一把合法 SSH 公钥）。
func autoyastTestCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:     "88",
			HostSN: "SN-SUSE-0001",
			ResolvedParams: map[string]string{
				"hostname": "suse-01",
				"biz_ip":   "10.20.0.30",
			},
			FinalizeToken: "finalize-tok-suse",
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilySUSE,
			OSVersion: "15.5",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "sles15.5/linux",
				Initrd: []string{"sles15.5/initrd"},
				Repo:   "sles15.5/repo/",
			},
		},
		Facts: map[string]string{
			model.FactTaskID:      "88",
			model.FactSN:          "SN-SUSE-0001",
			model.FactHostname:    "suse-01",
			model.FactBizIP:       "10.20.0.30",
			model.FactDiskPrimary: "/dev/disk/by-id/wwn-0x5000c500suse0001",
			model.FactOSFamily:    string(model.OSFamilySUSE),
		},
		Bases:         testBases,
		InstallToken:  "install-tok-suse",
		FinalizeToken: "finalize-tok-suse",
		SyslogHost:    "10.10.0.1",
		SyslogPort:    514,
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
	}
}

// ---------------------------------------------------------------------------
// 输出是合法 XML（用 encoding/xml 解析器验一次，不只对比文本）
// ---------------------------------------------------------------------------

// TestAutoYaST_OutputIsValidXML 锁住"完整 AutoYaST XML 结构正确，能被 XML
// 解析器解析"（任务测试要求第一条）。
func TestAutoYaST_OutputIsValidXML(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastTestCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}

	var doc struct {
		XMLName xml.Name `xml:"profile"`
	}
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("输出不是合法 XML: %v，got:\n%s", err, out)
	}
}

// TestAutoYaST_RootElementIsProfile 验证根元素是 <profile>，带官方要求的
// 两个命名空间（yast2ns 与 configns，见官方 AutoYaST Guide §4.31.7 示例）
// ——AutoYaST 解析器按这两个命名空间识别 config:type 属性，缺失会导致
// config:type="list"/"boolean" 之类的类型标注被当作普通字符串失效。
func TestAutoYaST_RootElementIsProfile(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastTestCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	if !strings.Contains(out, `<profile xmlns="http://www.suse.com/1.0/yast2ns" xmlns:config="http://www.suse.com/1.0/configns">`) {
		t.Errorf("输出应含带两个官方命名空间的 <profile> 根元素，got:\n%s", out)
	}
}

// TestAutoYaST_NotKickstartSyntax 锁住"绝不复用 kickstart 语法"这条硬性
// 要求——kickstart 的段落标记（%pre/%post/%end）与真正会被安装器解释执行
// 的指令（sshkey --username=、bootloader --location=、独立一行的
// rootpw 指令）不应出现在 AutoYaST 输出中，出现即说明误用了 kickstart 的
// 模板逻辑。
//
// 不检查裸子串 "rootpw"/"inst.ks="——这两个子串会作为**无害的字面文本**
// 出现在复用的 firstboot.go credentialCleanup() 里（
// `sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg`，一条
// 对不存在的文件生效的 sed 命令，无操作，见 contractGaps 记录的
// "firstboot.go 的凭据清理并非真正 OS 家族无关"这条既有缺口——该缺口同样
// 影响 Ubuntu 的 autoinstall.go，非本轨引入），那一行本身不是"kickstart
// 语法被解释执行"，只是字面出现在一个 sed 正则参数字符串里，检查它会把
// 一个已知的、跨家族共享的既有缺口误判为本文件的新 bug。真正的风险信号
// 是这些指令以"安装器会实际解析执行"的形态出现（比如独立成行的
// `sshkey --username=root "..."`），本测试改为检查这类更精确的模式。
func TestAutoYaST_NotKickstartSyntax(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastTestCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	forbidden := []string{"%pre\n", "%post\n", "%end\n", "sshkey --username", "bootloader --location", "ds=nocloud-net", "preseed/url="}
	for _, f := range forbidden {
		if strings.Contains(out, f) {
			t.Errorf("输出中不应出现 kickstart/其它家族的语法标记 %q——AutoYaST 是独立的 XML 格式，"+
				"绝不能复用 kickstart 模板逻辑，got:\n%s", f, out)
		}
	}
}

// ---------------------------------------------------------------------------
// 四个 hook 阶段各自落到正确的 AutoYaST 段落
// ---------------------------------------------------------------------------

// autoyastCtxWithHooks 在最小上下文基础上加四个 Lober hook 阶段各一个 hook。
func autoyastCtxWithHooks() model.RenderContext {
	ctx := autoyastTestCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "bbbb2222nochroot", Stage: model.HookStagePostNoChroot, Order: 10, Required: false},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	return ctx
}

// TestAutoYaST_PreStageInPreScripts 锁住 Lober pre 阶段落在 AutoYaST
// <pre-scripts> 段落（官方 AutoYaST Guide §4.31.1："Executed before YaST
// does any real change to the system...before partitioning and package
// installation"，与 Lober pre 阶段"installer ramdisk，分区之前"能力边界
// 一致）。
func TestAutoYaST_PreStageInPreScripts(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastCtxWithHooks())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}

	preIdx := strings.Index(out, "<pre-scripts")
	if preIdx == -1 {
		t.Fatalf("输出中找不到 <pre-scripts> 段落，got:\n%s", out)
	}
	preEndIdx := strings.Index(out, "</pre-scripts>")
	if preEndIdx == -1 || preEndIdx < preIdx {
		t.Fatalf("找不到匹配的 </pre-scripts> 闭合标记，got:\n%s", out)
	}
	section := out[preIdx:preEndIdx]
	if !strings.Contains(section, "aaaa1111pre") {
		t.Errorf("<pre-scripts> 段落内应包含 pre 阶段 hook 的 run_hook 调用（含 digest "+
			"aaaa1111pre），got 段落内容:\n%s", section)
	}
	if strings.Contains(section, "bbbb2222nochroot") || strings.Contains(section, "cccc3333post") {
		t.Errorf("<pre-scripts> 段落内不应混入 post-nochroot/post 阶段的 hook，got:\n%s", section)
	}
}

// TestAutoYaST_PostNoChrootStageUsesChrootScriptsNotChrooted 锁住 Lober
// post-nochroot 阶段落在 AutoYaST <chroot-scripts> 段落且 <chrooted> 为
// false（官方 AutoYaST Guide §4.31.3、§4.31.6 chrooted 参数说明："If this
// parameter is set to false, AutoYaST does not run chroot and does not
// install the boot loader at this stage"——与 Lober post-nochroot"目标盘
// 挂在 /mnt/sysroot，用宿主工具"的能力边界完全一致：目标盘已挂但未 chroot）。
func TestAutoYaST_PostNoChrootStageUsesChrootScriptsNotChrooted(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastCtxWithHooks())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}

	idx := strings.Index(out, "bbbb2222nochroot")
	if idx == -1 {
		t.Fatalf("输出中找不到 post-nochroot 阶段 hook 的 digest，got:\n%s", out)
	}

	// 从该 hook 调用往前找最近的 <script> 起始标记，该 <script> 所属的
	// <chrooted> 子元素必须是 false。
	before := out[:idx]
	scriptStart := strings.LastIndex(before, "<script>")
	if scriptStart == -1 {
		t.Fatalf("post-nochroot hook 前找不到 <script> 起始标记，got:\n%s", out)
	}
	between := out[scriptStart:idx]
	if !strings.Contains(between, "<chrooted") {
		t.Errorf("post-nochroot 阶段的 <script> 元素应显式声明 <chrooted> 子元素"+
			"（不能依赖默认值，默认值本身恰好是 false 但显式声明更不易被后续误改），got:\n%s", between)
	}
	if !strings.Contains(between, ">false<") {
		t.Errorf("post-nochroot 阶段的 <chrooted> 应为 false，got:\n%s", between)
	}
}

// TestAutoYaST_PostStageUsesChrootScriptsChrooted 锁住 Lober post 阶段落在
// AutoYaST <chroot-scripts> 段落且 <chrooted> 为 true（能装包、写配置、
// systemctl enable，与 kickstart %post 的能力边界一致）。
func TestAutoYaST_PostStageUsesChrootScriptsChrooted(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastCtxWithHooks())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}

	idx := strings.Index(out, "cccc3333post")
	if idx == -1 {
		t.Fatalf("输出中找不到 post 阶段 hook 的 digest，got:\n%s", out)
	}

	before := out[:idx]
	scriptStart := strings.LastIndex(before, "<script>")
	if scriptStart == -1 {
		t.Fatalf("post hook 前找不到 <script> 起始标记，got:\n%s", out)
	}
	between := out[scriptStart:idx]
	if !strings.Contains(between, "<chrooted") || !strings.Contains(between, ">true<") {
		t.Errorf("post 阶段的 <script> 元素的 <chrooted> 应为 true，got:\n%s", between)
	}
}

// TestAutoYaST_FirstbootHookReachesFirstbootScaffold 锁住 Lober firstboot
// 阶段的 hook 最终通过复用的 FirstbootScaffold 落进目标系统（与
// RHEL/Ubuntu/Debian 同一套 firstboot 机制，不是 AutoYaST 原生的
// post-scripts 或 init-scripts——那两个是假同源陷阱，见文件头与
// AssembleAutoYaST 注释里的详细说明）。
func TestAutoYaST_FirstbootHookReachesFirstbootScaffold(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastCtxWithHooks())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	if !strings.Contains(out, "dddd4444firstboot") {
		t.Errorf("输出中应包含 firstboot 阶段 hook 的 digest（经 FirstbootScaffold 落地），got:\n%s", out)
	}
	if !strings.Contains(out, "/etc/systemd/system/lober-init.service") {
		t.Errorf("输出中应包含 FirstbootScaffold 生成的 systemd unit 路径，got:\n%s", out)
	}
}

// TestAutoYaST_NoNativePostScriptsOrInitScripts 锁住"不误用 AutoYaST 原生
// post-scripts/init-scripts 承载 Lober 的 hook 阶段"这条设计决定——这两个
// 标签是假同源陷阱：post-scripts 在首次启动时执行但服务全部未起
// （官方 Guide §4.31.4"during the first boot...no services running"），
// init-scripts 不由 YaST 执行、XML 结构也不同（§4.31.5），都不满足 Lober
// post-nochroot/post 两个阶段"目标盘已挂/已 chroot，宿主工具或已安装环境
// 可用"的能力边界，也不满足 firstboot 阶段"完整 systemd + 网络可达"的能力
// 边界，因此本渲染器不产出这两个标签。
func TestAutoYaST_NoNativePostScriptsOrInitScripts(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastCtxWithHooks())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	if strings.Contains(out, "<post-scripts") {
		t.Errorf("输出中不应出现 AutoYaST 原生 <post-scripts>——它在首次启动时执行但服务未起，"+
			"不满足 Lober post 阶段的能力边界，got:\n%s", out)
	}
	if strings.Contains(out, "<init-scripts") {
		t.Errorf("输出中不应出现 AutoYaST 原生 <init-scripts>——它不由 YaST 执行且 XML 结构"+
			"不同，不是 Lober firstboot 阶段的正确承载方式，got:\n%s", out)
	}
}

// TestAutoYaST_EmptyPreStageOmitted 锁住"没有 pre 阶段 hook 时不产出空的
// <pre-scripts> 段落"（与 kickstart 侧"%pre 里没东西就不要写 %pre"同一条
// 设计原则）。
func TestAutoYaST_EmptyPreStageOmitted(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastTestCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	if strings.Contains(out, "<pre-scripts") {
		t.Errorf("无 pre 阶段 hook 时不应产出 <pre-scripts> 段落，got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// 空 RootSSHPublicKeys 被硬拒绝
// ---------------------------------------------------------------------------

// TestAutoYaST_EmptySSHKeysRejected 锁住"root 不设可复用密码，SSH 公钥为空
// 时必须硬拒绝"（SPEC §1.5，与 kickstart baseDirectives/preseed
// lateCommandValue/autoinstall AssembleAutoinstall 同一条设计要点）——省略
// 会让机器装完后 root 完全无法登录，这比报错更危险，绝不能静默产出这样的
// 机器。
func TestAutoYaST_EmptySSHKeysRejected(t *testing.T) {
	ctx := autoyastTestCtx()
	ctx.RootSSHPublicKeys = nil

	out, err := AssembleAutoYaST(ctx)
	if err == nil {
		t.Errorf("RootSSHPublicKeys 为空时 AssembleAutoYaST 应返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("RootSSHPublicKeys 为空时不应返回任何内容（不是部分成功），got:\n%s", out)
	}
}

// TestAutoYaST_SSHKeysInjectedIntoAuthorizedKeys 验证合法 SSH 公钥确实出现
// 在输出中（写入 /root/.ssh/authorized_keys 的动作）。
func TestAutoYaST_SSHKeysInjectedIntoAuthorizedKeys(t *testing.T) {
	out, err := AssembleAutoYaST(autoyastTestCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	if !strings.Contains(out, "authorized_keys") {
		t.Errorf("输出中应包含写入 authorized_keys 的动作，got:\n%s", out)
	}
	if !strings.Contains(out, "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober") {
		t.Errorf("输出中应包含配置的 SSH 公钥本身，got:\n%s", out)
	}
}

// ---------------------------------------------------------------------------
// 恶意输入不逃逸：XML 结构字符、换行、$(...)、反引号、超长串
// ---------------------------------------------------------------------------

// autoyastInjectionPayloads 复用 injection_test.go 的恶意值样本集合，额外
// 加上 XML 专属的结构字符（AutoYaST 是 XML，除了 shell 层注入还要防 XML
// 层注入——两层转义都要验，见任务测试要求"XML 与 shell 是两层转义，都要
// 验"）。
var autoyastInjectionPayloads = []struct {
	label string
	value string
}{
	{"命令分隔符", "web01; rm -rf /"},
	{"反引号命令替换", "web`rm -rf /`01"},
	{"$(...) 命令替换", "web$(rm -rf /)01"},
	{"超长值", strings.Repeat("a", 10000)},
	{"含引号的值", `web'"01`},
	{"XML 小于号", "web<01"},
	{"XML 大于号", "web>01"},
	{"XML 与号", "web&01"},
	{"XML 双引号", `web"01`},
	{"伪造闭合标签加新元素", "web01</hostname><evil>x</evil>"},
	{"伪造 CDATA 逃逸", "web01]]></source><script><source><![CDATA[rm -rf /"},
}

// TestAutoYaST_InjectionRejectedNotPartialSuccess 锁住"恶意表单值导致整体
// 渲染失败（不是部分成功）"——guard 白名单校验器在值送进模板之前就会拒绝
// 这些 payload（hostname 走 guard.ValidateHostname，字符集只允许字母数字
// 与连字符，全部恶意样本都不在此字符集内）。
func TestAutoYaST_InjectionRejectedNotPartialSuccess(t *testing.T) {
	for _, payload := range autoyastInjectionPayloads {
		t.Run(payload.label, func(t *testing.T) {
			ctx := autoyastTestCtx()
			ctx.Task.ResolvedParams = map[string]string{
				"hostname": payload.value,
				"biz_ip":   "10.20.0.30",
			}
			out, err := AssembleAutoYaST(ctx)
			if err == nil {
				t.Errorf("恶意值类别 %q（hostname=%q）应导致 AssembleAutoYaST 整体返回错误，"+
					"got nil error, out=%q", payload.label, payload.value, out)
			}
			if out != "" {
				t.Errorf("恶意值类别 %q 应导致渲染失败时不返回任何内容，got:\n%s", payload.label, out)
			}
		})
	}
}

// TestAutoYaST_MaliciousContentNeverLeaksIntoOutput 从另一个角度加固同一个
// 不变量：即使未来某次修改让渲染在恶意输入下意外"成功"，产出中也绝不能
// 出现恶意值的原始文本（呼应 injection_test.go 的同名设计）。
func TestAutoYaST_MaliciousContentNeverLeaksIntoOutput(t *testing.T) {
	for _, payload := range autoyastInjectionPayloads {
		t.Run(payload.label, func(t *testing.T) {
			ctx := autoyastTestCtx()
			ctx.Task.ResolvedParams = map[string]string{
				"hostname": payload.value,
				"biz_ip":   "10.20.0.30",
			}
			out, _ := AssembleAutoYaST(ctx)
			if out != "" && strings.Contains(out, payload.value) {
				t.Errorf("恶意值类别 %q 的原始文本出现在产出中，got:\n%s", payload.label, out)
			}
		})
	}
}

// TestAutoYaST_MaliciousSSHKeyRejected 用含裸换行与 XML 结构字符的恶意 SSH
// 公钥走完整流程——渲染器自身不单靠 config.Validate 的上游校验就假设
// ctx.RootSSHPublicKeys 无害（与 kickstart/preseed/autoinstall 同一条纵深
// 防御要求），换行本身即被拒绝（authorized_keys 是逐行文件，公钥值不应该
// 含换行）。
func TestAutoYaST_MaliciousSSHKeyRejected(t *testing.T) {
	ctx := autoyastTestCtx()
	ctx.RootSSHPublicKeys = []string{
		"ssh-ed25519 AAAAtest evil\n<script>rm -rf /</script>",
	}
	out, err := AssembleAutoYaST(ctx)
	if err == nil {
		t.Errorf("含裸换行的恶意 SSH 公钥应导致 AssembleAutoYaST 返回错误，got nil, out=%q", out)
	}
	if out != "" {
		t.Errorf("恶意 SSH 公钥应导致渲染失败时不返回任何内容，got:\n%s", out)
	}
}

// TestAutoYaST_XMLStructureIntactUnderEscaping 验证喂给 html/template 的
// 唯一一个 XML 文本节点叶子值（{{.Hostname}}，来自 ctx.Facts[LOBER_
// HOSTNAME]）若含 XML 特殊字符，html/template 会正确转义，输出仍是合法
// 可解析的 XML。
//
// 特意通过 ctx.Facts 而不是 ctx.Task.ResolvedParams 注入含特殊字符的
// 主机名——ResolvedParams 会先过 guard.ValidateHostname 白名单（只允许
// 字母数字与连字符），任何 XML 特殊字符都会在那一步被拒绝，测试不到
// html/template 转义这一步本身；Facts 命名空间不经过 guard 校验（与
// preseed.go debconfLine 注释记录的同一个事实：facts 是"文本 KV"，不
// 承诺值本身免受结构破坏），直接构造 Facts[LOBER_HOSTNAME] 才能真正
// 触发并验证 AssembleAutoYaST 里 {{.Hostname}} 这一处转义路径本身
// 是否生效（而不是验证"校验器把它挡住了"这个已经被
// TestAutoYaST_InjectionRejectedNotPartialSuccess 覆盖的另一件事）。
func TestAutoYaST_XMLStructureIntactUnderEscaping(t *testing.T) {
	ctx := autoyastTestCtx()
	ctx.Facts = cloneFacts(ctx.Facts)
	ctx.Facts[model.FactHostname] = `suse<evil>&"'01`

	out, err := AssembleAutoYaST(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}

	// 只检查 <hostname> 元素本身的内容是否转义——同一个原始值也会经
	// InjectFacts 出现在 CDATA 包裹的 shell 脚本正文里（LOBER_HOSTNAME
	// 的 shell 变量赋值），那里正确地保持不转义（CDATA 块本身就是为了
	// 不被 XML 解析，赋值内容如果被转义反而是错误行为）。用一个宽松的
	// 断言去检查"全文任何位置都不出现 <evil>"会把这条正确行为误判为
	// 转义失效，因此改为定位 <hostname>...</hostname> 这一个具体元素。
	hostnameStart := strings.Index(out, "<hostname>")
	hostnameEnd := strings.Index(out, "</hostname>")
	if hostnameStart == -1 || hostnameEnd == -1 || hostnameEnd < hostnameStart {
		t.Fatalf("输出中找不到 <hostname>...</hostname> 元素，got:\n%s", out)
	}
	hostnameContent := out[hostnameStart+len("<hostname>") : hostnameEnd]
	if strings.Contains(hostnameContent, "<evil>") {
		t.Errorf("<hostname> 元素内容含未转义的 <evil>，说明 {{.Hostname}} 的 XML 转义未生效，"+
			"got 元素内容: %q", hostnameContent)
	}
	if !strings.Contains(hostnameContent, "&lt;evil&gt;") {
		t.Errorf("<hostname> 元素内容应含转义后的 &lt;evil&gt;，说明转义没有按预期发生，"+
			"got 元素内容: %q", hostnameContent)
	}

	var doc struct {
		XMLName xml.Name `xml:"profile"`
	}
	if err := xml.Unmarshal([]byte(out), &doc); err != nil {
		t.Fatalf("含特殊字符的主机名渲染后输出应仍是合法 XML: %v，got:\n%s", err, out)
	}
}

// ---------------------------------------------------------------------------
// 产出不含字面 http://
// ---------------------------------------------------------------------------

// TestAutoYaST_NoLiteralHTTPScheme 锁住"Lober 自己签发的 URL 不含字面
// http://"（SPEC §1.5：URL 全从基址来，不写死 scheme）。换成 https:// 的
// Bases 重新渲染，断言换了配置之后输出里不再残留任何硬编码的
// gw.internal/ng.internal 域名下的 http://（与 golden_test.go 的
// TestGolden_NoLiteralHTTPScheme 同一设计）。
//
// 例外与 unattend_test.go 的 TestUnattend_NoLoberControlledLiteralHTTPScheme
// 同一个道理：AutoYaST 官方命名空间 URI（xmlns="http://www.suse.com/1.0/
// yast2ns"、xmlns:config="http://www.suse.com/1.0/configns"）是 AutoYaST
// 格式标准本身规定的固定字面值，不是 Lober 配置项——SPEC §1.5"不写死
// scheme"约束的对象是 Lober 自己签发的 URL，不覆盖第三方标准规定的命名
// 空间字符串（这两个 URI 甚至不是可解析访问的真实地址，只是 XML
// 命名空间的标识符，永远不会变成 https://）。因此本测试逐行检查，只有
// 不属于 www.suse.com 命名空间的 http:// 才算违规。
func TestAutoYaST_NoLiteralHTTPScheme(t *testing.T) {
	ctx := autoyastCtxWithHooks()
	ctx.Bases = model.BaseURLs{
		GatewayBase:  "https://gw.internal",
		ArtifactBase: "https://ng.internal",
	}
	out, err := AssembleAutoYaST(ctx)
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	for _, line := range strings.Split(out, "\n") {
		if strings.Contains(line, "http://") && !strings.Contains(line, "www.suse.com") {
			t.Errorf("发现非 AutoYaST 命名空间的字面 http://，可能是硬编码的 Lober URL: %q", line)
		}
	}
}

// ---------------------------------------------------------------------------
// RHEL/Debian/Ubuntu 既有 golden file 不得变化
// ---------------------------------------------------------------------------

// TestAutoYaST_DoesNotAffectOtherFamilyGoldens 是一条防回归提示性测试：
// 本文件新增的 AssembleAutoYaST 是全新的独立函数，不修改 kickstart.go/
// preseed.go/autoinstall.go/skeleton.go 的任何既有函数体，因此 RHEL/
// Debian/Ubuntu 的 golden file 理应不受影响。真正的保证来自"本轨未修改
// 那些文件"这一事实（可用 git diff 核对），本测试只是在渲染包内留一条
// 可执行的哨兵：重新渲染一次 RHEL 最小 kickstart，断言其内容与既有
// golden 文件仍然逐字节一致——若这条失败，说明有人误改了共用骨架
// （skeleton.go/clientlib.go/params.go/firstboot.go）的行为，而不是
// AutoYaST 本身的问题。
func TestAutoYaST_DoesNotAffectOtherFamilyGoldens(t *testing.T) {
	out, err := AssembleKickstart(minimalKickstartCtx())
	if err != nil {
		t.Fatalf("AssembleKickstart 返回错误: %v", err)
	}
	assertGolden(t, "kickstart_minimal.golden", out)
}

// ---------------------------------------------------------------------------
// golden file 覆盖：最小 AutoYaST、带四阶段 hook 的完整版
// ---------------------------------------------------------------------------

// minimalAutoYaSTCtx 构造"无 hook 的最小 AutoYaST"渲染上下文，使用
// golden_test.go 已声明的固定假数据（fixedTaskID 等），保证与其它家族的
// golden 测试用同一套固定输入基准，便于横向比对。
func minimalAutoYaSTCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            fixedTaskID,
			HostSN:        fixedSN,
			FinalizeToken: fixedFinalizeToken,
		},
		Artifact: model.ArtifactSet{
			OSFamily:  model.OSFamilySUSE,
			OSVersion: "15.5",
			Arch:      model.ArchX86_64,
			Paths: model.ArtifactPaths{
				Kernel: "sles15.5/linux",
				Initrd: []string{"sles15.5/initrd"},
				Repo:   "sles15.5/repo/",
			},
		},
		Facts: map[string]string{
			model.FactTaskID:       fixedTaskID,
			model.FactSN:           fixedSN,
			model.FactHostname:     fixedHostname,
			model.FactBizIP:        fixedBizIP,
			model.FactDiskPrimary:  fixedDiskByID,
			model.FactOSFamily:     string(model.OSFamilySUSE),
			model.FactArtifactBase: goldenBases.ArtifactURL(),
			model.FactGatewayBase:  goldenBases.GatewayURL(),
		},
		Bases:         goldenBases,
		InstallToken:  fixedInstallToken,
		FinalizeToken: fixedFinalizeToken,
		SyslogHost:    "10.10.0.1",
		SyslogPort:    514,
		RootSSHPublicKeys: []string{
			"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		},
	}
}

// fullAutoYaSTCtx 在最小上下文基础上加四个 Lober hook 阶段各一个 hook。
func fullAutoYaSTCtx() model.RenderContext {
	ctx := minimalAutoYaSTCtx()
	ctx.Hooks = []model.HookRef{
		{HookDigest: "aaaa1111pre", Stage: model.HookStagePre, Order: 10, Required: true},
		{HookDigest: "bbbb2222nochroot", Stage: model.HookStagePostNoChroot, Order: 10, Required: false},
		{HookDigest: "cccc3333post", Stage: model.HookStagePost, Order: 10, Required: true},
		{HookDigest: "dddd4444firstboot", Stage: model.HookStageFirstboot, Order: 10, Required: true},
	}
	return ctx
}

// TestGolden_MinimalAutoYaST 锁住无 hook 的最小 AutoYaST 输出。
func TestGolden_MinimalAutoYaST(t *testing.T) {
	out, err := AssembleAutoYaST(minimalAutoYaSTCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	assertGolden(t, "autoyast_minimal.golden", out)
}

// TestGolden_FullAutoYaST 锁住带四阶段 hook 的完整 AutoYaST 输出。
func TestGolden_FullAutoYaST(t *testing.T) {
	out, err := AssembleAutoYaST(fullAutoYaSTCtx())
	if err != nil {
		t.Fatalf("AssembleAutoYaST 返回错误: %v", err)
	}
	assertGolden(t, "autoyast_full.golden", out)
}
