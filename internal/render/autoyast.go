// 本文件实现 SUSE AutoYaST 应答文件（autoinst.xml）的组装（依据 SPEC
// §0.3、§4.5、§5.1、§5.2、§5.6、附录 B）。
//
// **SUSE 用 AutoYaST，绝不是 kickstart。** 引导参数是 `autoyast=`（linux +
// initrd + autoyast=<URL>，SPEC §0.3），不是 `inst.ks=`；应答文件是独立的
// XML 格式（model.AnswerFileName 已规定文件名为 autoinst.xml），根元素是
// `<profile>` 而不是任何 kickstart 风格的段落式纯文本。两者只是都被口头
// 称作"ks"，语法完全不同、产出的文件安装器根本不认——本文件不复用
// kickstart.go 的任何模板拼接逻辑，只复用 skeleton.go/clientlib.go/
// params.go/firstboot.go 里与"应答文件语法无关"的共用内容（hook 调用行、
// signal/run_hook 函数正文、firstboot 脚手架文本），这与 preseed.go/
// autoinstall.go 复用同一套共用内容的方式一致。
//
// ---------------------------------------------------------------------------
// AutoYaST script 阶段名称的核实来源（附录 B"各 OS 内核参数拼写"待核实项）
// ---------------------------------------------------------------------------
//
// 本文件的 AutoYaST 段落名称与结构已对照官方文档核实（curl 直连
// documentation.suse.com 与 doc.opensuse.org 均可达），核实来源：
//  1. 官方 SUSE AutoYaST Guide（SLES 15 SP4 PDF，
//     documentation.suse.com/sles/15-SP4/pdf/book-autoyast_en.pdf）
//     §4.31"Custom user scripts"，含 §4.31.1 Pre-scripts、§4.31.3
//     Chroot environment scripts、§4.31.4 Post-scripts、§4.31.5 Init
//     scripts、§4.31.6 Script XML representation、§4.31.7 Script
//     example（含完整可运行的 XML 示例）。
//  2. 独立第三方示例交叉验证（GitHub gist、golinuxhub 教程），结构与
//     官方文档一致：<profile xmlns="http://www.suse.com/1.0/yast2ns"
//     xmlns:config="http://www.suse.com/1.0/configns">。
//
// 核实到的关键事实——**AutoYaST 实际有五种脚本类型，不是四种**：
// pre-scripts（分区与安装包之前）、postpartitioning-scripts（分区完成、
// /mnt 已挂载、RPM 安装之前，本实现不使用）、chroot-scripts（package
// installation 之后、首次重启之前，通过 <chrooted> 布尔子元素区分是否
// 已 chroot 进 /mnt 并安装引导加载器）、post-scripts（"during the first
// boot of the installed system, no services running"）、init-scripts
// （"when all services are running"，但**不由 YaST 执行**，由
// /usr/lib/YaST2/bin/autoyast-initscripts.sh 执行，XML 结构也不同，
// 只支持 location/source/filename/rerun 四个子元素，不支持
// chrooted/interpreter/feedback）。
//
// **假同源陷阱**：post-scripts 与 init-scripts 这两个名字表面上像是
// kickstart %post 与 firstboot 的对应物，但语义都不匹配：
//   - post-scripts 在首次启动时执行但服务全部未起，不满足 Lober post
//     阶段"能装包、写配置、建用户、systemctl enable"的能力边界（那需要
//     chroot 环境，不是"已启动但服务未起"的环境）；
//   - init-scripts 虽然语义上最接近 Lober firstboot（网络已初始化、
//     服务已起），但它不由 YaST 执行、XML 结构不支持
//     interpreter/chrooted/feedback，且"只支持单个默认文件名"的限制与
//     Lober 需要按 Order 顺序执行任意数量 firstboot hook 的要求不匹配。
//
// 因此本实现遵循与 RHEL/Ubuntu/Debian 完全一致的既有模式（firstboot 不是
// 系统 hook 的专属段落，而是嵌进"已 chroot、可写文件"的那个阶段里的一段
// 自举脚手架）：Lober 的四个 HookStage 映射为
//
//	pre            → <pre-scripts>
//	post-nochroot  → <chroot-scripts>，<chrooted>false</chrooted>
//	post           → <chroot-scripts>，<chrooted>true</chrooted>
//	firstboot      → 内嵌进 <chrooted>true</chrooted> 的 chroot-script 里
//	                 复用的 FirstbootScaffold（写 systemd oneshot unit，
//	                 真正执行留给目标系统首次真实启动，与 kickstart %post/
//	                 preseed late_command in-target/autoinstall
//	                 late-commands in-target 完全同一套机制，只是这里的
//	                 载体是 AutoYaST 的 chrooted chroot-script）
//
// 未核实到官方文档明确给出的部分（供人工复核，contractGaps 已同步记录）：
//   - <feedback>/<notification> 等交互式弹窗子元素本实现完全不使用
//     （MVP 场景是无人值守批量装机，弹窗类元素反而会阻塞安装）。
//   - <interpreter> 子元素默认值为 shell（官方文档："Optional; default
//     is shell"），本实现显式写出 shell 而不依赖默认值，理由与
//     post-nochroot 显式写 <chrooted>false</chrooted> 一致：显式声明比
//     依赖默认值更不容易被后续维护者误改。
package render

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// autoyastXMLDeclaration 是固定的 XML 声明与 DOCTYPE 行，不经过
// html/template（与 unattend.go 的 xmlDeclaration 同一个理由：
// html/template 的 HTML 语法状态机不认识 `<?xml ... ?>` 处理指令语法，
// 会把开头的 `<` 误转义成 `&lt;`。DOCTYPE 行取自官方 AutoYaST Guide
// §4.31.7 示例的写法 `<!DOCTYPE profile>`——不带外部 DTD 引用，因为本系统
// 不依赖 YaST 自带的 DTD 文件校验，只依赖"输出是合法 XML"这一条更弱但
// 足够的保证（TestAutoYaST_OutputIsValidXML 用 encoding/xml 验证）。
const autoyastXMLDeclaration = "<?xml version=\"1.0\"?>\n<!DOCTYPE profile>\n"

// ---------------------------------------------------------------------------
// html/template 用于 XML 转义的适用性说明（SPEC §5.6："unattend.xml 用
// html/template（获得 XML 转义）"，任务要求"判断是否需要自己做 XML 转义，
// 把理由写进注释"）
// ---------------------------------------------------------------------------
//
// html/template 是为 HTML 文档设计的上下文感知转义器，但本文件仍复用它
// （而非 text/template）渲染 XML，理由与 unattend.go 完全一致：
//
//  1. html/template 在纯文本节点位置对 `<`、`>`、`&` 的转义
//     （`&lt;`、`&gt;`、`&amp;`）与 XML 的转义规则完全相同——这三个字符在
//     XML 文本节点里必须转义，HTML 文本节点里同样必须转义，两者在这一点
//     上没有分歧。
//  2. html/template 在属性值位置额外转义 `"` 为 `&#34;`、`'` 为
//     `&#39;`——XML 规范里这两个字符只在"属性值刚好用同种引号包裹"时才
//     必须转义（用双引号包裹的属性值里，单引号不转义也合法），
//     html/template 的处理是更严格的超集（总是转义，不管外层用的是哪种
//     引号），不会产生"该转义但没转义"的欠转义，只会产生"不必转义但也
//     转义了"的过度转义——过度转义不破坏 XML 合法性，只是不够精简，
//     属于可接受的保守选择。
//  3. 唯一不等价的场景是 CDATA 段内部——html/template 不认识 XML 的
//     CDATA 语法，会把 CDATA 分隔符 `<![CDATA[`/`]]>` 本身当作普通文本
//     转义掉，导致 CDATA 语法失效。本文件的应对方式是**绝不把任何经过
//     html/template 变量替换的用户输入放进 CDATA 块内**——CDATA 块
//     （<source> 元素内的 shell 脚本正文）只承载渲染器自己生成的固定
//     文本（ClientLib 的函数定义、run_hook 调用行，这些内容不含任何未经
//     guard 白名单校验的原始表单值），CDATA 定界符本身作为模板里的字面
//     常量写出，不经过 `{{.Xxx}}` 变量替换，因此不会被转义器触碰。
//  4. 与 unattend.go 相同的已知限制：`<?xml ... ?>` 处理指令语法不能放进
//     模板正文，必须作为不经过模板引擎的固定前缀单独拼接（见
//     autoyastXMLDeclaration）。
//
// 两层防护仍然缺一不可（SPEC §5.6"模板渲染的注入防护"）：html/template
// 的转义只防"值破坏 XML 结构"，防不了"值是一个合法但恶意的主机名"——
// 本文件在喂给模板之前先用 InlineParams 的 guard 白名单校验一遍
// ctx.Task.ResolvedParams（与 unattend.go RenderUnattend 的校验顺序一致）。

// autoyastBaseTemplateSource 是 <profile> 的基础骨架：命名空间声明、
// 分区（用服务端决策的盘，by-id 标识，SPEC §5.6，与 kickstart
// baseDirectives/preseed preseedPartitioning 同一策略：单分区，MVP 范围）、
// 主机名（<networking><dns><hostname>，AutoYaST 官方字段名）。
//
// 四个 hook 阶段各自的 <scripts> 内容不放进这个模板——原因与
// kickstart.go RenderStageSection/preseed.go earlyCommandValue 完全一致：
// hook 数量是运行期动态的（0 到 N 个），html/template 的 `{{range}}`
// 语法虽然能处理动态数量的子元素，但 hook 调用行本身（ClientLib 定义 +
// run_hook 调用 + 信号点）是格式规整的固定 shell 文本而非需要 XML 转义的用户
// 数据——digest/required/timeout 都是渲染器内部生成的固定格式值，不含
// 任何未经 guard 校验的原始表单值，用 Go 侧字符串拼接（与其它三个家族的
// hook 调用行组装方式一致）比塞进模板变量再转义一遍更直接，也更容易与
// StageHookCallLines/ClientLib 的现有产出对齐验证。
//
// {{.Hostname}} 是本模板唯一一处会经过 html/template 转义的动态内容——
// 该值来自 ctx.Facts[LOBER_HOSTNAME]（正常部署路径下经
// guard.ValidateHostname 白名单校验过，见 InlineParams 对
// ctx.Task.ResolvedParams 的校验）。<scripts> 段落（含 SSH 公钥注入与
// firstboot 脚手架）**不**作为模板字段传入——那些内容是 Go 侧已经拼好的
// 完整 XML 片段字符串（chroot-script 元素，含 CDATA 包裹的 shell
// 脚本），若把它们当作普通字符串类型的模板字段传入，html/template 会
// 按"纯文本"规则把片段里本来合法的 `<chroot-scripts>` 等标签转义成
// `&lt;chroot-scripts&gt;`，产出结构错误的 XML；若改用 html/template
// 类型系统提供的 `template.HTML` 之类"标记为已安全"的包装类型能避免
// 这个问题，但会引入"这份文本被显式标记为绕过转义"的额外心智负担。
// 因此本文件选择更直接的方式：<scripts> 段落完全不进入模板渲染流程，
// 由 AssembleAutoYaST 在 autoyastBaseTemplate.Execute 产出的结果字符串
// 上，用普通字符串拼接把 <scripts> 块插入 </profile> 闭合标记之前
// （见 AssembleAutoYaST 函数体），只有 {{.Hostname}} 这一个真正的
// "叶子文本值"才通过模板变量转义。
const autoyastBaseTemplateSource = `<profile xmlns="http://www.suse.com/1.0/yast2ns" xmlns:config="http://www.suse.com/1.0/configns">
  <general>
    <mode>
      <confirm config:type="boolean">false</confirm>
    </mode>
  </general>
  <partitioning config:type="list">
    <drive>
      <device>{{.DiskByID}}</device>
      <use>all</use>
    </drive>
  </partitioning>
  <networking>
    <dns>
      <hostname>{{.Hostname}}</hostname>
    </dns>
    <dhcp_options>
      <dhclient_client_id/>
    </dhcp_options>
  </networking>
</profile>
`

// autoyastBaseTemplate 是解析后的基础骨架模板，包级变量复用（模板解析
// 是纯函数，Parse 一次即可）。
var autoyastBaseTemplate = htmltemplate.Must(htmltemplate.New("autoyast-base").Parse(autoyastBaseTemplateSource))

// autoyastBaseData 是喂给 autoyastBaseTemplate 的字段。
type autoyastBaseData struct {
	DiskByID string
	Hostname string
}

// cdataSafe 把 s 中的 CDATA 结束定界符 "]]>" 拆开，防止脚本正文本身恰好
// 包含这三个字符时提前结束 CDATA 块（一旦提前结束，块内原本被 CDATA
// 保护、不会被 XML 解析的 `<`、`&` 等字符会重新暴露为需要解析的 XML
// 语法，可能拼出结构被篡改的文档）。
//
// 采用 XML 标准的分割写法：把 "]]>" 拆成两个相邻的 CDATA 块
// "]]" + "]]><![CDATA[" + ">"——两个 CDATA 块之间没有任何分隔符时，
// XML 解析器会把它们的内容无缝拼接，效果等价于内容里真的含有连续的
// "]]>" 三个字符，但不会被解析器误认作 CDATA 结束标记。
//
// 本文件里实际调用本函数的场景是防御性的：hook 调用行与 ClientLib 正文
// 全部是渲染器自己按固定格式生成的文本（digest 是十六进制、其它字段是
// 固定字符串），不含任何未经校验的用户输入，正常路径下这段文本不可能
// 出现 "]]>"。加上这层处理的理由与 baseDirectives 对 RootSSHPublicKeys
// 做"防御性拒绝裸换行"一致：不能只依赖"调用方传入的值目前看起来无害"
// 就假设永远无害，尤其是本函数处理的是会被写进 CDATA 块、绕过全部 XML
// 转义的原始文本，一旦未来某处新增的调用路径不小心把未经校验的值传进
// 这里，本函数是最后一道防线。
func cdataSafe(s string) string {
	return strings.ReplaceAll(s, "]]>", "]]]]><![CDATA[>")
}

// scriptSectionKind 区分 pre-scripts 与 chroot-scripts 两种承载 Lober
// hook 的 AutoYaST 段落类型（postpartitioning-scripts 本实现不使用，
// 见文件头注释）。
type scriptSectionKind int

const (
	scriptSectionPre scriptSectionKind = iota
	scriptSectionChroot
)

// autoyastScriptElement 渲染一个 <script> 元素（官方 AutoYaST Guide
// §4.31.6 Script XML representation）。filename 用固定规则生成
// （见 callers），interpreter 固定写 shell（不依赖官方文档"默认即
// shell"的隐含行为，显式声明更不易被后续误改，见文件头注释）。
//
// chrooted 参数只在 kind 为 scriptSectionChroot 时才会被写出
// （<chrooted> 子元素官方文档明确"This option is only available for
// chroot environment scripts"——pre-scripts 元素里出现这个子元素没有
// 意义，AutoYaST 会忽略它，但写出无意义的元素会让人误以为 pre-scripts
// 也受这个开关影响）。
func autoyastScriptElement(kind scriptSectionKind, filename string, chrooted bool, body string) string {
	var b strings.Builder
	b.WriteString("      <script>\n")
	if kind == scriptSectionChroot {
		fmt.Fprintf(&b, "        <chrooted config:type=\"boolean\">%t</chrooted>\n", chrooted)
	}
	fmt.Fprintf(&b, "        <filename>%s</filename>\n", filename)
	b.WriteString("        <interpreter>shell</interpreter>\n")
	b.WriteString("        <source><![CDATA[\n")
	b.WriteString(cdataSafe(body))
	b.WriteString("]]></source>\n")
	b.WriteString("      </script>\n")
	return b.String()
}

// preScriptsSection 渲染 <pre-scripts> 段落（Lober pre 阶段，官方
// AutoYaST Guide §4.31.1："Executed before YaST does any real change to
// the system...before partitioning and package installation"，与
// kickstart %pre/preseed early_command 的能力边界一致：分区之前、看
// 硬件、动态决策）。
//
// 复用 skeleton.go 的 StageHookCallLines 取得该阶段的 hook 调用行
// （已按 Order 排好序、每行自带 `|| exit 1` 短路退出），与 ClientLib
// 一起拼成脚本正文——每个 pre-script 的 <script> 元素都是独立的执行
// 环境（不同 <script> 元素之间不共享变量/函数定义，与 kickstart 各
// %pre 段落各自独立子进程同一类边界，未在设计期用真实工具验证但基于
// 官方文档"scripts are executed"的措辞与其它三个家族的验证结果保守
// 采用同样的处理方式：每个脚本正文都重新注入一份完整 ClientLib）。
//
// 没有 pre 阶段 hook 时返回空字符串——空段落不产出（与 kickstart 侧
// "%pre 里没东西就不要写 %pre"同一条设计原则）。
func preScriptsSection(ctx model.RenderContext) (string, error) {
	lines := StageHookCallLines(ctx.Hooks, model.HookStagePre)
	if len(lines) == 0 {
		return "", nil
	}

	params, err := InlineParams(ctx.Task.ResolvedParams)
	if err != nil {
		return "", fmt.Errorf("render: 参数内联失败: %w", err)
	}

	var body strings.Builder
	body.WriteString(params)
	body.WriteString(InjectFacts(ctx.Facts))
	body.WriteString("# 用户 hook 从这里开始\n")
	body.WriteString(ClientLib(ctx))
	body.WriteString(`signal "pre-start" 0` + "\n")
	for _, line := range lines {
		body.WriteString(line)
	}
	body.WriteString(`signal "pre-done" 0` + "\n")

	// 只返回 <pre-scripts>...</pre-scripts> 片段本身，不含外层 <scripts>
	// 包装——官方 AutoYaST Guide §4.31.7 的示例把全部脚本类型列表
	// （pre-scripts、chroot-scripts 等）作为兄弟元素放在同一个 <scripts>
	// 父元素下，不是各自套一层独立的 <scripts>。外层包装由
	// AssembleAutoYaST 统一拼接一次，保证输出与官方示例的结构完全一致。
	var b strings.Builder
	b.WriteString("    <pre-scripts config:type=\"list\">\n")
	b.WriteString(autoyastScriptElement(scriptSectionPre, "lober-pre.sh", false, body.String()))
	b.WriteString("    </pre-scripts>\n")
	return b.String(), nil
}

// sshAuthorizedKeysScript 渲染写入 root 账户 authorized_keys 的 chroot
// 脚本正文（SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码
// 登录）。<chrooted>true</chrooted> 环境下 /root 就是最终系统的 /root，
// 不需要 preseed.go sshAuthorizedKeysInTargetCommand 那样额外拼
// in-target 前缀（AutoYaST 的 chroot 边界已经由 <chrooted> 子元素本身
// 处理，YaST 在执行这个脚本之前已经 chroot 进 /mnt）。
//
// heredoc 定界符与转义策略复用 preseed.go 同款设计：定界符加引号
// （<<'EOF'）关闭 shell 对正文的全部展开/解释，公钥里的特殊字符不会被
// 解释成 shell 语法；换行本身已在 AssembleAutoYaST 里被拒绝（公钥值
// 不应含换行），因此这里不需要再处理"公钥内容恰好等于定界符"的逃逸——
// 该逃逸依赖公钥文本里有一行独占内容与定界符相同，换行已被别处拒绝，
// 这条路径不成立。
func sshAuthorizedKeysScript(keys []string) string {
	var b strings.Builder
	b.WriteString("mkdir -p /root/.ssh && chmod 700 /root/.ssh\n")
	b.WriteString("cat >> /root/.ssh/authorized_keys <<'" + sshAuthorizedKeysHeredocDelimiter + "'\n")
	for _, key := range keys {
		b.WriteString(key)
		b.WriteString("\n")
	}
	b.WriteString(sshAuthorizedKeysHeredocDelimiter + "\n")
	b.WriteString("chmod 600 /root/.ssh/authorized_keys\n")
	return b.String()
}

// chrootScriptsSection 渲染 <chroot-scripts> 段落，承载 Lober
// post-nochroot（chrooted=false）与 post（chrooted=true）两个阶段的
// hook，外加固定的 SSH 公钥注入与 firstboot 脚手架（都归在 chrooted=true
// 的脚本里，因为两者都需要"已 chroot 进目标系统"的能力：写
// /root/.ssh、写 /etc/systemd/system、写 /usr/local/sbin）。
//
// 与官方文档 §4.31.3 的能力边界完全对应："You can execute chroot scripts
// before the installation chroots into the installed system and
// configures the boot loader, or you can execute a script after the
// chroot into the installed system has happened"——chrooted=false 对应
// 前半句（目标盘挂在 /mnt，未 chroot，引导加载器未装），chrooted=true
// 对应后半句（已 chroot，引导加载器已装），与 Lober post-nochroot/post
// 两阶段的能力边界逐字对应，不是勉强凑出来的近似映射。
//
// ctx.RootSSHPublicKeys 为空时返回错误：与 kickstart.go baseDirectives/
// preseed.go lateCommandValue/autoinstall.go AssembleAutoinstall 同一条
// 设计要点，省略会让机器装完后 root 完全无法登录，绝不能静默产出这样的
// 机器（SPEC §1.5）。
func chrootScriptsSection(ctx model.RenderContext) (string, error) {
	if len(ctx.RootSSHPublicKeys) == 0 {
		return "", fmt.Errorf("render: RenderContext.RootSSHPublicKeys 为空，" +
			"无法渲染 AutoYaST——root 不设可复用密码，公钥是唯一登录通路，" +
			"绝不能省略写入 authorized_keys 的动作或写入占位公钥，" +
			"那会静默产出一台没人能登录的机器（SPEC §1.5）")
	}
	for _, key := range ctx.RootSSHPublicKeys {
		if strings.ContainsAny(key, "\n\r") {
			return "", fmt.Errorf("render: RootSSHPublicKeys 中的公钥 %q 含裸换行，"+
				"会在写入 authorized_keys 的 heredoc 正文中间开出意料之外的新行，"+
				"这个值不应该出现在这里，说明上游校验被绕过或数据已损坏（SPEC §5.6 注入防护）", key)
		}
	}

	var scripts strings.Builder

	// post-nochroot 阶段：chrooted=false，与 kickstart %post --nochroot/
	// preseed late_command 不加 in-target 前缀同一能力边界。
	nochrootLines := StageHookCallLines(ctx.Hooks, model.HookStagePostNoChroot)
	if len(nochrootLines) > 0 {
		var body strings.Builder
		body.WriteString(ClientLib(ctx))
		for _, line := range nochrootLines {
			body.WriteString(line)
		}
		scripts.WriteString(autoyastScriptElement(scriptSectionChroot, "lober-post-nochroot.sh", false, body.String()))
	}

	// post 阶段：chrooted=true。固定动作（signal post-start、SSH 公钥
	// 注入、firstboot 脚手架、signal post-done/rebooting）与用户 post
	// hook 一起打包进同一个 <script> 元素——这些固定动作与 kickstart.go
	// AssembleKickstart 里 %post 段落承载的内容完全对应（SSH 公钥注入是
	// baseDirectives 的一部分、firstboot 脚手架来自 FirstbootScaffold）。
	postLines := StageHookCallLines(ctx.Hooks, model.HookStagePost)

	var postBody strings.Builder
	postBody.WriteString(ClientLib(ctx))
	postBody.WriteString(`signal "post-start" 0` + "\n")
	postBody.WriteString(sshAuthorizedKeysScript(ctx.RootSSHPublicKeys))
	for _, line := range postLines {
		postBody.WriteString(line)
	}
	// firstboot 脚手架必须落在 chrooted=true 的段落里（与 kickstart %post
	// 必须落在 %post 段落同一条硬性要求，见 kickstart.go AssembleKickstart
	// 的同等注释）——它需要写文件到目标系统的 /var/lib/lober、
	// /etc/systemd/system、/usr/local/sbin，这些路径只有在已经 chroot 进
	// 目标系统之后才有意义。
	postBody.WriteString(FirstbootScaffold(ctx))
	postBody.WriteString(`signal "post-done" 0` + "\n" + `signal "rebooting" 0` + "\n")

	scripts.WriteString(autoyastScriptElement(scriptSectionChroot, "lober-post.sh", true, postBody.String()))

	// 只返回 <chroot-scripts>...</chroot-scripts> 片段本身，不含外层
	// <scripts> 包装——理由与 preScriptsSection 相同。
	var b strings.Builder
	b.WriteString("    <chroot-scripts config:type=\"list\">\n")
	b.WriteString(scripts.String())
	b.WriteString("    </chroot-scripts>\n")
	return b.String(), nil
}

// AssembleAutoYaST 把基础骨架与四个 hook 阶段的 <scripts> 段落组装成完整
// 的 AutoYaST autoinst.xml 文本。
//
// 校验顺序（与 unattend.go RenderUnattend 一致）：
//  1. 先用 InlineParams 的 guard 白名单校验 ctx.Task.ResolvedParams——
//     不合法立即整体拒绝，不进入模板渲染；
//  2. RootSSHPublicKeys 的硬拒绝检查（chrootScriptsSection 内部）；
//  3. 校验通过后，html/template 在渲染 {{.Hostname}} 时做 XML 转义。
//
// 组装顺序：基础骨架（分区/网络/主机名）→ pre-scripts（若有 pre 阶段
// hook）→ chroot-scripts（固定含 SSH 公钥注入与 firstboot 脚手架，外加
// post-nochroot/post 阶段的用户 hook）。两个 <scripts> 块分别独立拼接
// 后插入 </profile> 闭合标记之前——本函数不使用单个 html/template 的
// {{range}} 一次性渲染全部动态段落，理由见文件头"html/template 用于
// XML 转义的适用性说明"：hook 段落的内容本身是 Go 侧拼好的完整 XML
// 片段字符串，不是需要模板引擎逐字段转义的叶子值。
func AssembleAutoYaST(ctx model.RenderContext) (string, error) {
	if _, err := InlineParams(ctx.Task.ResolvedParams); err != nil {
		return "", fmt.Errorf("render: AutoYaST 的表单值未通过 guard 校验器: %w"+
			"——XML 转义能防值破坏文档结构，防不了值本身是一个合法但恶意的取值，"+
			"两道校验都要（SPEC §5.6）", err)
	}

	diskByID := ctx.Facts[model.FactDiskPrimary]
	hostname := ctx.Facts[model.FactHostname]

	data := autoyastBaseData{
		DiskByID: diskByID,
		Hostname: hostname,
	}

	var buf bytes.Buffer
	if err := autoyastBaseTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render: 渲染 AutoYaST 基础骨架模板失败: %w", err)
	}
	base := buf.String()

	// chrootScriptsSection 的 RootSSHPublicKeys 校验必须先跑一遍——即使
	// 没有任何 pre 阶段 hook（preScriptsSection 会提前返回空字符串），
	// 空 SSH 公钥这条硬拒绝也不能被跳过，因此这里不做"先判断有没有 pre
	// hook 再决定要不要继续"之类的短路。
	pre, err := preScriptsSection(ctx)
	if err != nil {
		return "", err
	}
	chroot, err := chrootScriptsSection(ctx)
	if err != nil {
		return "", err
	}

	closeTag := "</profile>\n"
	trimmed := strings.TrimSuffix(base, closeTag)
	if trimmed == base {
		return "", fmt.Errorf("render: AutoYaST 基础骨架模板输出未以 %q 结尾，"+
			"无法在闭合标记前插入 scripts 段落，got:\n%s", closeTag, base)
	}

	// pre 与 chroot 两个片段共用同一个外层 <scripts> 父元素——官方
	// AutoYaST Guide §4.31.7 的示例把 pre-scripts/chroot-scripts/
	// post-scripts/postpartitioning-scripts 全部列为同一个 <scripts>
	// 元素下的兄弟节点，不是各自套一层独立的 <scripts>（早期实现曾对
	// 每个片段各自包一层 <scripts>，产出两个相邻的顶层 <scripts> 元素，
	// 设计评审阶段对照官方示例发现不一致后改为本函数这种"一层 <scripts>
	// 包多个具体脚本类型列表"的写法）。chroot-scripts 恒非空（固定携带
	// SSH 公钥注入与 firstboot 脚手架），因此 <scripts> 父元素本身恒会
	// 被写出，不需要额外判断"两者都空就整体省略"的情况。
	var scriptsBlock strings.Builder
	scriptsBlock.WriteString("  <scripts>\n")
	scriptsBlock.WriteString(pre)
	scriptsBlock.WriteString(chroot)
	scriptsBlock.WriteString("  </scripts>\n")

	var out strings.Builder
	out.WriteString(autoyastXMLDeclaration)
	out.WriteString(trimmed)
	out.WriteString(scriptsBlock.String())
	out.WriteString(closeTag)
	return out.String(), nil
}

// RenderAutoYaSTMenu 渲染 SUSE 家族的 iPXE 菜单文本（menu.go 的
// RenderMenu 尚未接线到本函数，接线指示见本包 wiringNeeded 说明与本轨
// 交付说明——menu.go/renderer.go 的分派 switch 归 t08-dispatch 独占，
// 本文件只提供可供调用的独立导出函数）。
//
// SPEC §0.3 明确 SUSE 引导形态是"linux + initrd + autoyast="——文件名
// 约定与 Debian 同源（linux/initrd，不是 RHEL 的 vmlinuz/initrd.img，
// SUSE 安装内核同样沿用 Linux 上游内核构建产物的传统命名），token 走
// 路径段而不进 query（与 RHEL/Debian 同一条 SPEC §4.4 约束："token 走
// 路径段"，避免内核命令行上出现 & 导致解析歧义）。
func RenderAutoYaSTMenu(ctx model.RenderContext) (string, error) {
	kernelURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Kernel)
	answerFileName, ok := ctx.AnswerFileName()
	if !ok {
		return "", fmt.Errorf("render: 无法确定 OS 家族 %q 的应答文件名", ctx.Artifact.OSFamily)
	}
	autoyastURL := ctx.Bases.GatewayURL("v1", "answer", ctx.Task.ID, ctx.InstallToken, answerFileName)

	if len(ctx.Artifact.Paths.Initrd) == 0 {
		return "", fmt.Errorf("render: SUSE 制品集缺少 initrd 路径")
	}
	initrdURL := ctx.SetArtifactURL(ctx.Artifact.Paths.Initrd[0])

	// 模板声明的额外内核参数（Template.KernelArgs）。为空时 suffix 是空串，
	// 输出与接线之前逐字节一致。
	kargs, err := kernelArgsSuffix(ctx.Template.KernelArgs)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf(
		"kernel %s \\\n"+
			"  autoyast=%s "+linuxConsoleArgs+"%s\n"+
			"initrd %s\n"+
			ipxeBootCommand,
		kernelURL, autoyastURL, kargs, initrdURL,
	), nil
}
