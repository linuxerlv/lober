// 本文件实现 firstboot 脚手架与凭据清理（对应实现计划任务 4，依据 SPEC
// §5.2、§5.8、§9.5）。
//
// `%post` 虽已 chroot，但跑的还是 installer 的内核，目标系统 systemd 未
// 启动——因此需要完整 boot 才能做的事必须推到首次启动后（SPEC §5.2）。
// 本文件生成的文本全部落在 `%post` 段，职责是"埋点"：写文件、建 unit、
// enable 它；真正的执行留给 firstboot 阶段的 systemd oneshot。
package render

import (
	"fmt"
	"sort"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// firstbootHookRefsSorted 返回 ctx.Hooks 中 Stage 为 firstboot 的引用，按
// Order 升序排列。不复用 model.Template.HooksForStage——渲染器拿到的是
// RenderContext.Hooks（已由上游按阶段与 order 排好序的完整列表），这里只
// 需要按 Stage 过滤，排序对已排好序的输入是稳定操作。
func firstbootHookRefsSorted(hooks []model.HookRef) []model.HookRef {
	var matched []model.HookRef
	for _, h := range hooks {
		if h.Stage == model.HookStageFirstboot {
			matched = append(matched, h)
		}
	}
	sort.SliceStable(matched, func(i, j int) bool { return matched[i].Order < matched[j].Order })
	return matched
}

// hookScriptFileName 按 SPEC §5.8 的命名规则返回该 firstboot hook 落地后
// 的文件名：`<三位序号>-<digest>.sh`，序号取自 Order（从 010 起、每条间隔
// 10，与信号表序号"留间隔便于插入"的设计精神一致，但这里的间隔只是命名
// 美观，不参与任何比较逻辑）。
func hookScriptFileName(ref model.HookRef, position int) string {
	return fmt.Sprintf("%03d-%s.sh", (position+1)*10, ref.HookDigest)
}

// FirstbootScaffold 渲染 `%post` 阶段的 firstboot 脚手架与凭据清理逻辑
// （SPEC §5.8、§9.5、计划任务 4）。
//
// 五样东西（全部落在 %post）：
//  1. 目标系统里的工作目录 /var/lib/lober
//  2. finalize token 文件，权限 0600
//  3. 参数文件，权限 0600
//  4. 用户的 firstboot hook 脚本与参数，落进目标系统
//  5. systemd oneshot unit 与它的执行脚本，并 enable 它
//
// 契约缺口（详见函数内注释与 contractGaps）：model.HookRef 不携带 hook 正
// 文文本，只有 content-addressed digest。本实现在 %post 阶段（重启前，
// 装机 VLAN 保证可达）用与 run_hook 相同的"下载 + sha256sum -c 校验"机制
// 把脚本落进目标系统，而不是把正文明文内嵌进 kickstart——这仍然满足
// "必须在 %post 就落进目标系统、不能等重启后再取"的安全意图，因为落地
// 动作确实发生在重启之前，只是落地手段是"校验下载"而非"直接内嵌明文"。
func FirstbootScaffold(ctx model.RenderContext) string {
	hooks := firstbootHookRefsSorted(ctx.Hooks)

	var b strings.Builder

	b.WriteString("mkdir -p /var/lib/lober /var/lib/lober/hooks\n\n")

	// 凭据文件：finalize token 与参数文件，权限 0600。用 install -m 600
	// /dev/null 先创建空文件并设权限，再写入内容——避免内容写入与权限设置
	// 之间存在窗口期（虽然 %post 是单线程脚本，没有真正的并发风险，但这个
	// 写法与 SPEC §5.8 给出的参考实现一致，保持可读性上的一致）。
	b.WriteString("install -m 600 /dev/null /var/lib/lober/finalize.token\n")
	b.WriteString(fmt.Sprintf("echo '%s' > /var/lib/lober/finalize.token\n\n", ctx.FinalizeToken))

	b.WriteString("install -m 600 /dev/null /var/lib/lober/params.env\n")
	b.WriteString("cat > /var/lib/lober/params.env <<'LOBER_PARAMS_EOF'\n")
	b.WriteString(InjectFacts(ctx.Facts))
	b.WriteString("LOBER_PARAMS_EOF\n\n")

	// 用户 firstboot hook：按 digest 下载并用 sha256sum -c 校验后落地到
	// /var/lib/lober/hooks/，同时生成 manifest 记录 digest/required/order。
	// 下载动作复用与 run_hook 一致的校验方式，即使 %post 阶段并未注入
	// run_hook 函数本身（那是安装阶段的注入点），这里直接展开等价的
	// curl + sha256sum -c 两步。
	b.WriteString("cat > /var/lib/lober/hooks/manifest <<'LOBER_MANIFEST_EOF'\n")
	for i, h := range hooks {
		fileName := hookScriptFileName(h, i)
		fmt.Fprintf(&b, "%s digest=%s required=%t order=%d\n", fileName, h.HookDigest, h.Required, h.Order)
	}
	b.WriteString("LOBER_MANIFEST_EOF\n\n")

	for i, h := range hooks {
		fileName := hookScriptFileName(h, i)
		dest := "/var/lib/lober/hooks/" + fileName
		artifactURL := ctx.Bases.ArtifactURL("hooks", h.HookDigest, "script.sh")
		fmt.Fprintf(&b, "curl -fsS -o %s '%s'\n", dest, artifactURL)
		fmt.Fprintf(&b, "echo '%s  %s' | sha256sum -c -\n", h.HookDigest, dest)
		fmt.Fprintf(&b, "chmod 700 %s\n\n", dest)
	}

	// systemd unit 与执行脚本。ConditionPathExists + 成功后删标记文件保证
	// 只跑一次，比脚本末尾 systemctl disable 好——脚本中途死掉时标记仍在，
	// 可看出未跑完，重启会重试，也可人工 touch 重新挂上（SPEC §5.8）。
	b.WriteString(initScript(ctx, hooks))

	b.WriteString("cat > /etc/systemd/system/lober-init.service <<'LOBER_UNIT_EOF'\n")
	b.WriteString("[Unit]\n")
	b.WriteString("Description=Lober OS init\n")
	b.WriteString("After=network-online.target\n")
	b.WriteString("Wants=network-online.target\n")
	b.WriteString("ConditionPathExists=/var/lib/lober/init.pending\n")
	b.WriteString("[Service]\n")
	b.WriteString("Type=oneshot\n")
	b.WriteString("ExecStart=/usr/local/sbin/lober-init.sh\n")
	b.WriteString("StandardOutput=journal+console\n")
	b.WriteString("[Install]\n")
	b.WriteString("WantedBy=multi-user.target\n")
	b.WriteString("LOBER_UNIT_EOF\n\n")

	// 反直觉但关键：systemctl enable 在 %post 中可用（本质只是建符号链接），
	// 而 systemctl start 不可用（无 running systemd）。这正是 firstboot
	// 机制的基础——%post 负责埋点，firstboot 负责执行。
	b.WriteString("systemctl enable lober-init.service\n")
	b.WriteString("touch /var/lib/lober/init.pending\n")

	return b.String()
}

// initScript 渲染写入 /usr/local/sbin/lober-init.sh 的 heredoc 块，包含
// SPEC §5.8 规定的信号顺序与 §9.5 规定的凭据清理三条。
//
// 流程：发 booted → 发 init-start → 顺序跑用户 firstboot hook → 发
// init-done → 发 ready → 清理凭据 → 删标记文件。即使信号发不出，也要把
// 本地工作做完——signal 函数本身已保证遥测失败不会中断调用方（任务 3），
// 因此这里的调用之间不加 `|| exit` 之类的短路退出。
func initScript(ctx model.RenderContext, hooks []model.HookRef) string {
	var b strings.Builder

	b.WriteString("cat > /usr/local/sbin/lober-init.sh <<'LOBER_INIT_EOF'\n")
	b.WriteString("#!/bin/bash\n")
	b.WriteString(". /var/lib/lober/params.env\n\n")
	b.WriteString(ClientLib(ctx))
	b.WriteString("\n")

	b.WriteString(`signal "booted" 0` + "\n")
	b.WriteString(`signal "init-start" 0` + "\n\n")

	for i, h := range hooks {
		fileName := hookScriptFileName(h, i)
		path := "/var/lib/lober/hooks/" + fileName
		fmt.Fprintf(&b, "bash %s > /tmp/fb-%s.log 2>&1\n", path, h.HookDigest)
		fmt.Fprintf(&b, "signal \"hook-report\" \"$?\" /tmp/fb-%s.log \"%s\"\n\n", h.HookDigest, h.HookDigest)
	}

	b.WriteString(`signal "init-done" 0` + "\n")
	b.WriteString(`signal "ready" 0` + "\n\n")

	b.WriteString(credentialCleanup())
	b.WriteString("LOBER_INIT_EOF\n")
	b.WriteString("chmod 755 /usr/local/sbin/lober-init.sh\n\n")

	return b.String()
}

// credentialCleanup 渲染凭据清理的三条动作（SPEC §9.5）：
//  1. 安全擦除参数文件与 finalize token（shred -u 而非 rm）
//  2. 删标记文件
//  3. 从安装记录里删掉密码哈希与带 token 的引导参数行
//
// inst.ks= 带 token 的完整 URL 会进入目标机的 /proc/cmdline、anaconda 日志、
// /root/anaconda-ks.cfg，同时也进 nginx access log。因 token 短时效且装完
// 即废，影响有界，但顺手清理值得做。
func credentialCleanup() string {
	var b strings.Builder
	b.WriteString("shred -u /var/lib/lober/params.env /var/lib/lober/finalize.token\n")
	b.WriteString("rm -f /var/lib/lober/init.pending\n")
	b.WriteString(`sed -i '/rootpw\|--iscrypted\|inst.ks=/d' /root/anaconda-ks.cfg 2>/dev/null` + "\n")
	return b.String()
}
