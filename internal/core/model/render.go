// 本文件定义渲染上下文与 URL 基址：RenderContext、BaseURLs、MAC 归一化、
// 应答文件命名。对应实现计划任务 9，依据 SPEC §1.5、§4.3、§4.4、§5.1、§9.5。
package model

import "strings"

// BaseURLs 承载网关与制品服务的基址。所有 URL 从这里拼出——SPEC §1.5
// 要求模板中不出现字面 http://，将来上 TLS 只改配置不改代码。
//
// scheme 完全来自配置：GatewayBase 与 ArtifactBase 各自已经包含 scheme
// （如 "http://gw" 或将来的 "https://gw"），本类型不额外拼接或假设 scheme。
type BaseURLs struct {
	GatewayBase  string // 如 "http://gw"
	ArtifactBase string // 如 "http://ng"
}

// joinURL 拼接基址与路径，规范化重复斜杠，但保留结尾斜杠。
//
// Ubuntu 的 ds=nocloud-net;s=<url> 要求 URL 是目录且结尾带 /（§4.4），
// 拼接函数吃掉结尾斜杠会导致 cloud-init 取不到应答文件——这个故障的现场
// 表现是「机器装到一半停住」，极难定位到一个斜杠，所以这条行为必须显式
// 保留而不是交给 path.Join 之类会吃掉结尾斜杠的通用函数。
func joinURL(base string, segments ...string) string {
	trailingSlash := len(segments) > 0 && strings.HasSuffix(segments[len(segments)-1], "/")
	if len(segments) == 0 {
		trailingSlash = strings.HasSuffix(base, "/")
	}

	parts := []string{strings.TrimRight(base, "/")}
	for _, seg := range segments {
		trimmed := strings.Trim(seg, "/")
		if trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	joined := strings.Join(parts, "/")
	if trailingSlash && !strings.HasSuffix(joined, "/") {
		joined += "/"
	}
	return joined
}

// GatewayURL 拼接网关 URL，如 /v1/boot、/v1/signal/... 等机器面端点。
func (b BaseURLs) GatewayURL(segments ...string) string {
	return joinURL(b.GatewayBase, segments...)
}

// ArtifactURL 拼接制品 URL（内核、initrd 等，落在 /artifacts/ 前缀下，
// 与 nginx 的 location 对齐，§1.4）。
func (b BaseURLs) ArtifactURL(segments ...string) string {
	return joinURL(b.ArtifactBase, append([]string{"artifacts"}, segments...)...)
}

// RepoURL 拼接仓库 URL（inst.repo= 等指向的解压后仓库树，同样落在
// /artifacts/ 前缀下）。
//
// 与 ArtifactURL 分成两个方法：将来仓库可能挪到独立 location（那里有
// open_file_cache 差异），拆开后只需改这一个方法（计划任务 9 设计要点）。
func (b BaseURLs) RepoURL(segments ...string) string {
	return joinURL(b.ArtifactBase, append([]string{"artifacts"}, segments...)...)
}

// NormalizeMAC 把任意格式的 MAC 地址归一化为小写冒号分隔形式。
//
// iPXE 发的是小写冒号分隔，discovery 导入可能是大写或连字符分隔，甚至无
// 分隔。归一化为统一格式，方便按 MAC 做身份匹配（§4.3）。
//
// 无法识别时返回空串——这是安全关键的方向：空 MAC 在 §4.3 的双因子校验
// 里会被当作「对不上」，宁可多一次人工介入，也不要让格式错误的输入被
// 静默接受为某个匹配上的值。
func NormalizeMAC(raw string) string {
	cleaned := strings.ToLower(raw)
	cleaned = strings.ReplaceAll(cleaned, "-", "")
	cleaned = strings.ReplaceAll(cleaned, ":", "")
	cleaned = strings.ReplaceAll(cleaned, ".", "")

	if len(cleaned) != 12 {
		return ""
	}
	for _, c := range cleaned {
		isHexDigit := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')
		if !isHexDigit {
			return ""
		}
	}

	var b strings.Builder
	for i := 0; i < 12; i += 2 {
		if i > 0 {
			b.WriteByte(':')
		}
		b.WriteString(cleaned[i : i+2])
	}
	return b.String()
}

// answerFileNames 按 OS 家族给出应答文件名（§5.1、§9.5）。
//
// suse 用 AutoYaST，绝不是 kickstart：引导参数是 autoyast= 而非
// inst.ks=，格式是独立的 XML。不要因为两者都被口头称作「ks」就把 SUSE
// 路由到 kickstart 渲染器——语法完全不同，产出的文件安装器根本不认。
var answerFileNames = map[OSFamily]string{
	OSFamilyRHEL:    "ks.cfg",
	OSFamilyUbuntu:  "user-data",
	OSFamilyDebian:  "preseed.cfg",
	OSFamilySUSE:    "autoinst.xml",
	OSFamilyWindows: "unattend.xml",
	OSFamilyESXi:    "boot.cfg",
}

// AnswerFileName 返回该 OS 家族的应答文件名。ok 为 false 表示未知家族。
func AnswerFileName(family OSFamily) (name string, ok bool) {
	name, ok = answerFileNames[family]
	return name, ok
}

// RenderContext 是渲染的全部输入，渲染器不查库（§5.1 设计要点）。这是
// 控制台 preview 端点能用假数据跑的前提，也是 golden 测试的前提。
type RenderContext struct {
	Task     Task
	Template Template
	Artifact ArtifactSet
	Host     Host

	// Hooks 是已按阶段与 order 排好序的 hook 引用，渲染器直接消费，不再
	// 自己排序（排序逻辑收敛在 Template.HooksForStage）。
	Hooks []HookRef

	Facts map[string]string
	Bases BaseURLs

	InstallToken  string
	FinalizeToken string

	SyslogHost string
	SyslogPort int

	// ActualFirmware 是机器实际上报的固件模式（来自 /v1/boot 的
	// ${platform}），不是任务记录值。Windows/ESXi 菜单据此选制品键，这是
	// §4.6 引导时按实际上报值校验的直接体现——任务里存的是 discovery
	// 阶段的记录值，可能与眼前机器不符。
	ActualFirmware FirmwareMode

	// RootSSHPublicKeys 是要注入本次装机 root 账户的 SSH 公钥列表
	// （SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录——
	// 截获也换不成持久访问），来源是 internal/config.Gateway.
	// RootSSHPublicKeys（部署级常量，装配点在构造渲染上下文时原样传入）。
	//
	// 不放进 Facts：fact 命名空间是面向 hook 的对外 API（§5.4：一旦有
	// 数十个 hook 在用就不能改名，只增不删，废弃字段要双写一个版本
	// 周期），公钥是渲染器内部消费的输入，不是给 hook 读取的运行期事实，
	// 混进 Facts 会把这个内部实现细节永久绑进那份对外契约。
	//
	// 渲染器必须把它当作必需输入：为空时返回错误而不是省略 SSH 公钥
	// 指令（省略会让机器装完后 root 完全无法登录）、更不能写死一个假
	// 公钥占位（那会让人误以为已经配置好了真实的登录凭据，是比报错更
	// 危险的静默失败）。装配点已经在配置校验阶段（internal/config.
	// Validate）拒绝空列表，这里的防御是第二层——万一渲染上下文的构造
	// 路径漏了赋值，也不会静默产出一台登不进去的机器。
	RootSSHPublicKeys []string
}

// AnswerFileName 返回本任务的应答文件名，按 RenderContext.Artifact.OSFamily
// 决定。ok 为 false 表示该家族未知。
func (c RenderContext) AnswerFileName() (name string, ok bool) {
	return AnswerFileName(c.Artifact.OSFamily)
}
