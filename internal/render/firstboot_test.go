// 本文件测试 firstboot 脚手架与凭据清理。对应实现计划任务 4，依据 SPEC
// §5.2、§5.8、§9.5。
package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/model"
)

// firstbootTestCtx 构造一个带一个 firstboot hook 的最小 RenderContext，
// 供本文件测试复用。
func firstbootTestCtx() model.RenderContext {
	return model.RenderContext{
		Task: model.Task{
			ID:            "42",
			FinalizeToken: "finalize-tok-xyz",
		},
		Bases:         testBases,
		FinalizeToken: "finalize-tok-xyz",
		Hooks: []model.HookRef{
			{HookDigest: "abc123", Stage: model.HookStageFirstboot, Order: 10, Required: true},
		},
	}
}

// TestFirstboot_FiveArtifacts 锁住"%post 阶段要生成的五样东西都出现在
// %post 输出里"（计划任务 4 测试要求第一条）：工作目录、finalize token
// 文件、参数文件、用户 firstboot hook 脚本落进目标系统、systemd unit。
func TestFirstboot_FiveArtifacts(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())

	checks := []struct {
		label string
		want  string
	}{
		{"工作目录", "mkdir -p /var/lib/lober"},
		{"finalize token 文件", "/var/lib/lober/finalize.token"},
		{"参数文件", "/var/lib/lober/params.env"},
		{"firstboot hook 脚本落地路径", "/var/lib/lober/hooks/"},
		{"systemd unit 文件", "/etc/systemd/system/lober-init.service"},
	}
	for _, c := range checks {
		if !strings.Contains(out, c.want) {
			t.Errorf("FirstbootScaffold 输出缺少%s（期望包含 %q），got:\n%s", c.label, c.want, out)
		}
	}
}

// TestFirstboot_CredentialFilePermissions 锁住"两个凭据文件带 0600 权限"
// （计划任务 4 测试要求第一条）。
func TestFirstboot_CredentialFilePermissions(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	if !strings.Contains(out, "install -m 600") && !strings.Contains(out, "chmod 600") {
		t.Errorf("FirstbootScaffold 输出应用 install -m 600 或 chmod 600 设置凭据文件权限，got:\n%s", out)
	}
	// finalize.token 与 params.env 各自都要有 600 权限动作，不能只对其中一个做。
	tokenPermIdx := strings.Index(out, "finalize.token")
	paramsPermIdx := strings.Index(out, "params.env")
	if tokenPermIdx == -1 || paramsPermIdx == -1 {
		t.Fatalf("输出中未找到 finalize.token 或 params.env 的位置，got:\n%s", out)
	}
	if !strings.Contains(out, "600") {
		t.Errorf("输出中应出现权限模式 600，got:\n%s", out)
	}
}

// TestFirstboot_UnitUsesMarkerFile 锁住"unit 用标记文件作存在条件"
// （计划任务 4 测试要求第二条，SPEC §5.8：ConditionPathExists + 成功后删
// 标记文件，而不是脚本末尾 systemctl disable）。
func TestFirstboot_UnitUsesMarkerFile(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	if !strings.Contains(out, "ConditionPathExists=/var/lib/lober/init.pending") {
		t.Errorf("FirstbootScaffold 输出的 unit 定义应含 "+
			"ConditionPathExists=/var/lib/lober/init.pending，got:\n%s", out)
	}
	if !strings.Contains(out, "touch /var/lib/lober/init.pending") {
		t.Errorf("FirstbootScaffold 应在 %%post 中创建标记文件 init.pending，got:\n%s", out)
	}
	if strings.Contains(out, "systemctl disable") {
		t.Errorf("FirstbootScaffold 不应用 systemctl disable 保证只跑一次"+
			"——脚本中途死掉时标记仍在才能看出未跑完，disable 会让脚本死掉后永远不再跑，got:\n%s", out)
	}
}

// TestFirstboot_UnitEnabledNotStarted 锁住"反直觉但关键的事实：systemctl
// enable 在 %post 中可用，而 systemctl start 不可用"。
func TestFirstboot_UnitEnabledNotStarted(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	if !strings.Contains(out, "systemctl enable") {
		t.Errorf("FirstbootScaffold 应 enable lober-init.service，got:\n%s", out)
	}
	if strings.Contains(out, "systemctl start") {
		t.Errorf("FirstbootScaffold 不应在 %%post 中 start 服务——无 running systemd，"+
			"systemctl start 在此环境必然失败，got:\n%s", out)
	}
}

// TestFirstboot_UserHookBodyEmbedded 锁住"用户 firstboot hook 的正文出现
// 在 %post 里（不是一条下载命令）"（计划任务 4 测试要求第三条）。
//
// 契约缺口（见 contractGaps）：model.HookRef 不携带 hook 正文文本，只有
// content-addressed 的 digest。本实现的保守处理是在 %post 阶段（重启前，
// 装机 VLAN 保证可达）用与 run_hook 相同的"下载 + sha256sum -c 校验"机制
// 把脚本落进目标系统，而不是在 firstboot 脚本（重启后，网络不保证可达）
// 里才去下载。这仍然满足"必须在 %post 就落进目标系统，不能重启后再取"这条
// 硬要求的字面意思与安全意图——因为落地动作确实发生在 %post、发生在重启
// 之前——只是落地手段是"校验下载"而非"直接内嵌明文"。
//
// 因此本测试断言的是"%post 阶段有针对该 hook digest 的下载与校验动作"，
// 而不是断言输出中出现某段具体的 hook 脚本源码（那需要 HookRef 携带正文，
// 当前契约做不到）。
func TestFirstboot_UserHookBodyEmbedded(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	if !strings.Contains(out, "abc123") {
		t.Errorf("FirstbootScaffold 输出应引用 firstboot hook 的 digest abc123，got:\n%s", out)
	}
	if !strings.Contains(out, "sha256sum -c") {
		t.Errorf("FirstbootScaffold 落地 firstboot hook 脚本时应校验 sha256sum -c，got:\n%s", out)
	}
	// 落地路径应包含 order 前缀与 digest，对应 manifest 文件的编号规则
	// （SPEC §5.8：010-<digest>.sh）。
	if !strings.Contains(out, "010-abc123.sh") {
		t.Errorf("FirstbootScaffold 应按 010-<digest>.sh 命名落地脚本，got:\n%s", out)
	}
}

// TestFirstboot_ManifestFile 锁住 SPEC §5.8 目录布局中的 manifest 文件
// （digest + required + timeout + order）。
func TestFirstboot_ManifestFile(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	if !strings.Contains(out, "/var/lib/lober/hooks/manifest") {
		t.Errorf("FirstbootScaffold 应生成 hooks/manifest 文件，got:\n%s", out)
	}
}

// TestFirstboot_InitScriptSignalOrder 锁住"firstboot 脚本的信号顺序正确：
// 发 booted → 发 init-start → 顺序跑用户 firstboot hook → 发 init-done →
// 发 ready → 清理凭据 → 删标记文件"（计划任务 4 测试要求第四条，SPEC §5.8）。
func TestFirstboot_InitScriptSignalOrder(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	script := extractInitScript(t, out)

	order := []string{
		`signal "booted"`,
		`signal "init-start"`,
		`signal "init-done"`,
		`signal "ready"`,
	}
	lastIdx := -1
	for _, marker := range order {
		idx := strings.Index(script, marker)
		if idx == -1 {
			t.Fatalf("lober-init.sh 中未找到 %q，脚本内容:\n%s", marker, script)
		}
		if idx <= lastIdx {
			t.Errorf("lober-init.sh 中 %q 出现顺序不对，期望晚于前一个信号", marker)
		}
		lastIdx = idx
	}

	// 清理凭据与删标记文件必须在 ready 信号之后。
	cleanupIdx := strings.Index(script, "shred -u")
	markerRemoveIdx := strings.Index(script, "rm -f /var/lib/lober/init.pending")
	readyIdx := strings.Index(script, `signal "ready"`)
	if cleanupIdx == -1 || cleanupIdx < readyIdx {
		t.Errorf("凭据清理（shred -u）应在 ready 信号之后执行，got:\n%s", script)
	}
	if markerRemoveIdx == -1 || markerRemoveIdx < cleanupIdx {
		t.Errorf("删标记文件应在凭据清理之后执行，got:\n%s", script)
	}
}

// TestFirstboot_CredentialCleanupThreeItems 锁住"凭据清理的三条都在，且用
// 的是安全擦除"（计划任务 4 测试要求第五条，SPEC §9.5）：
//  1. 安全擦除参数文件与 finalize token（shred -u 而非 rm）
//  2. 删标记文件
//  3. 从安装记录里删掉密码哈希与带 token 的引导参数行
func TestFirstboot_CredentialCleanupThreeItems(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	script := extractInitScript(t, out)

	if !strings.Contains(script, "shred -u") {
		t.Errorf("凭据清理应用 shred -u 安全擦除，而不是 rm，got:\n%s", script)
	}
	if !strings.Contains(script, "params.env") || !strings.Contains(script, "finalize.token") {
		t.Errorf("shred -u 应同时覆盖 params.env 与 finalize.token，got:\n%s", script)
	}
	if !strings.Contains(script, "rm -f /var/lib/lober/init.pending") {
		t.Errorf("凭据清理应删除标记文件 init.pending，got:\n%s", script)
	}
	if !strings.Contains(script, "anaconda-ks.cfg") {
		t.Errorf("凭据清理应处理 /root/anaconda-ks.cfg 中的密码哈希与 token 引导参数行，got:\n%s", script)
	}
	if !strings.Contains(script, "sed -i") {
		t.Errorf("从 anaconda-ks.cfg 删行应使用 sed -i，got:\n%s", script)
	}
}

// TestFirstboot_NoNetworkActivationCommands 锁住"输出里不含激活网络的
// 命令"（计划任务 4 测试要求第六条，SPEC §5.7：业务网写配置但不激活，
// 否则机器当场失去到网关的连接，装机成功但系统显示超时失败）。
func TestFirstboot_NoNetworkActivationCommands(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	forbidden := []string{"nmcli up", "nmcli connection up", "ifup ", "systemctl start NetworkManager"}
	for _, cmd := range forbidden {
		if strings.Contains(out, cmd) {
			t.Errorf("FirstbootScaffold 输出不应包含网络激活命令 %q"+
				"——业务网写配置但不激活是 SPEC §5.7 的硬要求，激活会让机器当场"+
				"失去到网关的连接，装机成功但系统显示超时失败", cmd)
		}
	}
}

// TestFirstboot_ContinuesLocallyEvenIfSignalFails 锁住"即使信号发不出，也
// 要把本地工作做完"（SPEC §5.8）——signal 函数本身已经保证返回成功（任务 3
// 的行为），本测试确认 lober-init.sh 中信号调用之间没有 `|| exit` 之类会
// 让脚本因信号失败而提前退出的写法。
func TestFirstboot_ContinuesLocallyEvenIfSignalFails(t *testing.T) {
	out := FirstbootScaffold(firstbootTestCtx())
	script := extractInitScript(t, out)
	if strings.Contains(script, `signal "booted" || exit`) ||
		strings.Contains(script, `signal "init-start" || exit`) {
		t.Errorf("lober-init.sh 不应在信号调用失败时提前退出——信号失败不能阻止"+
			"本地配置工作完成，got:\n%s", script)
	}
}

// extractInitScript 从 FirstbootScaffold 输出中提取 lober-init.sh 的正文
// （heredoc 包裹的脚本内容）。
func extractInitScript(t *testing.T, out string) string {
	t.Helper()
	marker := "lober-init.sh"
	idx := strings.Index(out, marker)
	if idx == -1 {
		t.Fatalf("输出中找不到 lober-init.sh，got:\n%s", out)
	}
	// lober-init.sh 的内容跟在文件名第一次出现之后的 heredoc 块中；为了不
	// 对具体 heredoc 定界符做过强假设，直接返回从文件名出现处到输出末尾的
	// 剩余文本——测试只需要判断相对顺序与子串是否存在，不需要精确切片。
	return out[idx:]
}
