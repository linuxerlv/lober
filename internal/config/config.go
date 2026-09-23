// Package config 定义 Lober 的 YAML 配置结构、默认值与启动校验。
// 对应实现计划任务 15，依据 SPEC §1.4、§3.3、§4.4、§6.6、§9.4。
//
// 配置里最要紧的一条是 §1.4 的「listen 写具体 IP」：写成 0.0.0.0 时一切功能
// 正常、测试全过，只是装机 VLAN 上的裸机获得了访问控制台 API 的能力——
// 信任域被静默合并。这种「错了也不报错」的配置必须在启动时校验，因此本包把
// 解析（Parse）与校验（Validate）分成两步：Parse 只做 YAML 反序列化与默认值
// 填充，永远不失败于业务规则；Validate 做四类启动校验，专门捕获「语法正确
// 但语义危险」的配置。
package config

import (
	"fmt"
	"io"
	"net"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/lober/lober/internal/core/model"
)

// 默认值。数值依据计划任务 15：扫描间隔 30 秒（SPEC §3.3）、syslog 端口 514、
// 控制台 token TTL 12 小时、IPAM static、DNS noop、resolver rule-based、
// 磁盘策略 largest、心跳模式 tcp、心跳间隔 60 秒。
const (
	defaultSyslogPort        = "514"
	defaultScanInterval      = 30 * time.Second
	defaultConsoleTokenTTL   = 12 * time.Hour
	defaultIPAM              = "static"
	defaultDNS               = "noop"
	defaultResolver          = "rule-based"
	defaultDiskPolicy        = model.DiskPolicyLargest
	defaultHeartbeatMode     = "tcp"
	defaultHeartbeatInterval = 60 * time.Second
)

// heartbeatModes 是心跳模式的白名单（SPEC §6.6、计划任务 15）。
//
// 默认 tcp 而非 icmp：ICMP 需要特权套接字（root / CAP_NET_RAW /
// ping_group_range），而心跳只是辅助通道（§6.6）——不值得为它给整个进程
// 提权。off 允许完全关闭心跳探测（例如业务网与管理网不通、无法探测的部署）。
// 新增 exec/http 两个取值（本次任务，SPEC §7.2 通用加载器）：心跳探测
// 也能接一个外部脚本/服务，与 IPAM/DNS 一样按加载器透明替换（对调用方
// 是同一个 contract.HeartbeatProviderV1，无论背后是内置 tcp/icmp 还是
// 子进程/HTTP）。
var heartbeatModes = map[string]bool{
	"tcp":  true,
	"icmp": true,
	"off":  true,
	"exec": true,
	"http": true,
}

// loaderTypes 是 LoaderConfig.Type 的合法取值白名单（SPEC §7.2：exec 与
// http 两种通用加载器，本次任务落地；grpc/starlark 是 Phase 3，不在此
// 白名单内——config 包在 Type 字段这一层就拒绝，不留到 factory.go 才
// 发现「这个类型还没实现」）。
var loaderTypes = map[string]bool{
	"exec": true,
	"http": true,
}

// diskPolicies 是磁盘策略的白名单，取自 model.DiskPolicy 的五个具名取值
// （SPEC §2.3）。config 包不重新定义这五个字符串，直接引用 model 的常量，
// 避免两处拼写漂移。
var diskPolicies = map[model.DiskPolicy]bool{
	model.DiskPolicyFirst:    true,
	model.DiskPolicyLargest:  true,
	model.DiskPolicyByModel:  true,
	model.DiskPolicyRAID1SSD: true,
	model.DiskPolicyByHook:   true,
}

// Config 是解析后的完整配置，分六组，对应计划任务 15 的分组：网关、控制台、
// 存储、扫描、认证、provider。
type Config struct {
	Gateway  Gateway  `yaml:"gateway"`
	Console  Console  `yaml:"console"`
	Store    Store    `yaml:"store"`
	Scan     Scan     `yaml:"scan"`
	Auth     Auth     `yaml:"auth"`
	Provider Provider `yaml:"provider"`
}

// Gateway 是机器面网关的配置：监听地址、两个基址 URL、syslog 监听与目标。
type Gateway struct {
	// Listen 是网关 HTTP 监听地址，必须是具体 IP（SPEC §1.4）——Go 进程应绑
	// 回环并由 nginx 做唯一入口，通配地址没有正当用途。
	Listen string `yaml:"listen"`

	// GatewayBase 与 ArtifactBase 对应 model.BaseURLs 的两个字段，供渲染器
	// 拼接 iPXE 脚本与应答文件里的完整 URL。非空是硬性要求：空值会产出形如
	// /artifacts/... 的相对 URL，iPXE 对它的解析行为不确定。
	GatewayBase  string `yaml:"gateway_base"`
	ArtifactBase string `yaml:"artifact_base"`

	// SyslogListen 是网关接收机器侧 syslog 上报的 UDP 监听地址，同样必须是
	// 具体 IP。SyslogTarget 是网关把接收到的日志转发到的下游目标（可选的
	// 集中式日志系统），不受「必须具体 IP」这条约束——它是出站地址，不是
	// 本进程的监听面，通配或域名形式在这里没有信任域合并的风险。
	SyslogListen string `yaml:"syslog_listen"`
	SyslogTarget string `yaml:"syslog_target"`

	// RootSSHPublicKeys 是要注入每台新装机器 root 账户的 SSH 公钥列表
	// （SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录——
	// 截获也换不成持久访问）。这是部署级常量：同一批机器共用运维团队的
	// 公钥，不是每台机器不同的表单值，因此放在配置而不是任务参数里。
	//
	// 支持多把：团队多人各自持有不同私钥，且公钥轮换期需要新旧并存（撤销
	// 旧公钥前，已用旧公钥登录过的运维不能被立即锁死），因此是列表而非
	// 单一字段。全部条目会被渲染进每台机器的应答文件（kickstart 的
	// `sshkey` 指令、cloud-init 的 `ssh_authorized_keys` 列表），任何一把
	// 都能登录，符合 SSH authorized_keys 文件本身「多行任一匹配即放行」
	// 的语义。
	//
	// 必须非空（见 Validate）：空列表意味着装好的机器 root 既没有可用密码
	// 也没有可用公钥，没人能登进去——这不是「将来再补」的优化项，是 MVP
	// 交付到真机就会立刻暴露的阻塞问题，因此在启动时就拒绝而不是留到装机
	// 现场才发现。
	RootSSHPublicKeys []string `yaml:"root_ssh_public_keys"`
}

// Console 是人面控制台的配置：监听地址、token 密钥与 TTL。
type Console struct {
	// Listen 同样必须是具体 IP（SPEC §1.4）——控制台 API 若能从装机 VLAN
	// 访问到，运维网与装机网的信任域就被静默合并了。
	Listen string `yaml:"listen"`

	// TokenSecret 是签发人面 access token 用的密钥。
	TokenSecret string `yaml:"token_secret"`

	// TokenTTL 默认 12 小时（计划任务 15）。
	TokenTTL time.Duration `yaml:"token_ttl"`
}

// Store 是持久化层的三个路径：数据库、制品、日志。
type Store struct {
	DBPath       string `yaml:"db_path"`
	ArtifactPath string `yaml:"artifact_path"`
	LogPath      string `yaml:"log_path"`
}

// Scan 是超时扫描循环的配置。
type Scan struct {
	// Interval 默认 30 秒（SPEC §3.3：扫描循环独立于请求路径，每 30 s 扫
	// phase_entered_at + budget < now）。
	Interval time.Duration `yaml:"interval"`
}

// Auth 是认证配置：用户列表。用户来自配置文件的 bcrypt 哈希，不做用户管理
// 界面——装机系统的操作员数量是个位数（SPEC §9.4）。
type Auth struct {
	Users []User `yaml:"users"`
}

// User 是一条认证记录：名字、bcrypt 哈希、角色列表。角色对应 SPEC §9.4 与
// §12.5 的权限分级（viewer/operator/hook-author/hook-approver/admin），但
// config 包不校验角色名是否在这五个值之内——角色码的权威定义在控制台模块
// （M6），config 只保证「非空」这一条通用约束，避免两个模块各自维护一份
// 角色白名单、未来新增角色时要改两处。
type User struct {
	Name       string   `yaml:"name"`
	BcryptHash string   `yaml:"bcrypt_hash"`
	Roles      []string `yaml:"roles"`
}

// Provider 是 provider 相关的选择项与参数：IPAM/DNS/resolver 三个内置实现
// 选择、磁盘策略、心跳模式与间隔、以及 exec/http 加载器的参数。
//
// IPAM/DNS/HeartbeatMode 的选择项是字符串而非强类型枚举，因为合法取值集合
// 不是 config 包能提前穷举封闭的——除了 MVP 阶段的内置实现
// （static/noop/tcp/icmp/off），现在还多了 exec/http 两种通用加载器
// （SPEC §7.2，internal/provider/loader 包，本次任务落地）。config 包只在
// Validate 里做「选了 exec 必须给 command、选了 http 必须给 url」这类
// 结构性校验，不做"选择项字符串必须在某个白名单内"的封闭校验——具体某个
// 字符串是否被工厂认可（factory.go 的 switch）是 internal/provider 的职责，
// config 包不重复维护第二份白名单。Resolver 目前仍只有 rule-based 一种
// 内置实现，没有 exec/http 支持（docs/phase2-loader-需求依据.md 第 4 节：
// TemplateResolver 是纯决策接口，刻意不提供 Ping，不适合套加载器，本任务
// 未对 Resolver 增加加载器选项，与该结论保持一致）。DiskPolicy 与
// HeartbeatMode 的「模式」这一层不同：它们的合法值在 MVP 阶段就是确定且
// 封闭的（前者取自 model.DiskPolicy，加 exec/http 属于扩展 HeartbeatMode
// 的合法值集合），因此在这里做白名单校验（heartbeatModes 已扩展，见下）。
type Provider struct {
	IPAM     string `yaml:"ipam"`
	DNS      string `yaml:"dns"`
	Resolver string `yaml:"resolver"`

	// DiskPolicy 默认 largest（SPEC §2.3、计划任务 15）。
	DiskPolicy model.DiskPolicy `yaml:"disk_policy"`
	// DiskModel 是 DiskPolicy 为 by-model 时必须提供的型号串，用于匹配磁盘
	// 型号。不给的运行期表现是「所有机器的磁盘规划都失败」，因此在
	// Validate 里强制要求。
	DiskModel string `yaml:"disk_model"`

	// HeartbeatMode 默认 tcp（SPEC §6.6、计划任务 15）。新增合法值
	// exec/http（本次任务），与 tcp/icmp/off 平级。
	HeartbeatMode string `yaml:"heartbeat_mode"`
	// HeartbeatInterval 默认 60 秒。
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`

	// IPAMLoader/DNSLoader/NotifyLoader/InventoryLoader/HeartbeatLoader
	// 是对应扩展点选择 "exec" 或 "http" 时使用的加载器参数（SPEC §7.2
	// 示例配置：`dns: { type: exec, command: ..., timeout: 10s }`）。
	// 选择项不是 exec/http 时，这些字段的内容被忽略（不校验、不报错——
	// 运维在切换回内置实现时不需要同步删除已经写好的加载器参数段落，
	// 这与 DiskModel 只在 DiskPolicy=by-model 时才被强制要求是同一种
	// "只在选中对应模式时才生效"的设计）。
	//
	// 五个字段各自独立（不是一个共享的 LoaderConfig 加一个"这是哪个
	// provider"的标签）：不同扩展点在同一份部署里可能一个接 exec 一个
	// 接 http（如 DNS 走 exec 脚本、Notify 走 http webhook），独立字段
	// 让 YAML 里每个扩展点的加载器参数各自成段，比共享一个数组再按
	// 名字查找更直观，且与 IPAM/DNS/HeartbeatMode 这几个选择项本身就是
	// 独立字段的既有风格一致。
	//
	// 没有 ResolverLoader：见 Provider 类型文档，TemplateResolver 是纯
	// 决策接口，本任务未对它增加加载器支持。
	IPAMLoader      LoaderConfig `yaml:"ipam_loader"`
	DNSLoader       LoaderConfig `yaml:"dns_loader"`
	InventoryLoader LoaderConfig `yaml:"inventory_loader"`
	NotifyLoader    LoaderConfig `yaml:"notify_loader"`
	HeartbeatLoader LoaderConfig `yaml:"heartbeat_loader"`
}

// LoaderConfig 是 exec/http 加载器的参数（SPEC §7.2）。字段并集覆盖两种
// 加载器各自的专属参数（exec 的 Command/Args，http 的 URL）与两者共用的
// 超时/重试/熔断参数——用一个并集结构而不是「按类型二选一的嵌套结构」，
// 理由是 YAML 里写起来更直接（不需要 `exec: {command: ...}` 或
// `http: {url: ...}` 这层多余嵌套，`type` 字段本身已经决定了哪些字段
// 有意义），且与 internal/provider/loader 包的 ExecConfig/HTTPConfig/
// Config 三个独立结构体在字段名上一一对应，转换时不需要做名字映射
// （见 factory.go 新增的 case 分支——它只是把这里的字段原样搬进对应的
// loader.XxxConfig）。
type LoaderConfig struct {
	// Type 是 "exec" 或 "http" 之一。留空且选择项不是 exec/http 时无意义
	// （被忽略，见 Provider 字段文档）；选择项是 exec/http 时必须匹配
	// （Validate 强制要求），防止「选了 http 却在 loader 参数段填了
	// type: exec」这种自相矛盾的配置被静默接受。
	Type string `yaml:"type"`

	// Command 与 Args 是 Type=exec 时使用的参数，对应
	// loader.ExecConfig.Command/Args。Type=exec 时 Command 必须非空
	// （否则运行期第一次调用才会发现，见 loader.NewExecLoader 的启动
	// 校验——config.Validate 提前做同样的校验，让运维在配置阶段就发现
	// 而不是等到第一次建任务/DNS 注册才报错）。
	Command string   `yaml:"command"`
	Args    []string `yaml:"args"`

	// URL 是 Type=http 时使用的参数，对应 loader.HTTPConfig.URL。
	// Type=http 时必须非空，理由与 Command 对称。
	URL string `yaml:"url"`

	// Timeout/MaxRetries/RetryBackoff/BreakerThreshold 对应
	// loader.Config 的同名字段，两种类型共用。零值在 Parse 阶段不填充
	// 默认值（不同于 Console.TokenTTL 等字段）——loader.Config.withDefaults
	// 已经会在构造加载器时填充默认值，config 包在这里重复填充只会造成
	// 「配置文件里没写，但 Parse 之后却看到一个非零值」的困惑，两处默认值
	// 逻辑保持单一来源（loader 包）更不容易漂移。
	Timeout          time.Duration `yaml:"timeout"`
	MaxRetries       int           `yaml:"max_retries"`
	RetryBackoff     time.Duration `yaml:"retry_backoff"`
	BreakerThreshold int           `yaml:"breaker_threshold"`
}

// Parse 从 r 读取 YAML 并反序列化为 Config，随后填充默认值。
//
// Parse 本身不做业务规则校验（例如 listen 是否为通配地址）——那是 Validate
// 的职责。分开是为了让调用方能分别处理「配置文件语法错误」与「配置语义危险
// 但语法合法」这两类完全不同的问题：前者是运维笔误，后者是需要运维理解
// 「为什么危险」才能正确修复的安全边界。
func Parse(r io.Reader) (*Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("config: 读取配置内容失败: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("config: 解析 YAML 失败: %w", err)
	}

	applyDefaults(&cfg)
	return &cfg, nil
}

// applyDefaults 填充计划任务 15 列出的默认值。只填充「YAML 未提供」的字段
// （零值判定）——显式写了 0 或空串的字段不会被默认值覆盖，除非该字段本身
// 的零值就是非法值（例如 Interval 为 0 会导致扫描循环忙等，因此零值同样
// 触发默认值填充；这与「用户显式选择了一个合法但非默认的值」不冲突，因为
// 0 从来不是任何这些字段的合法取值）。
func applyDefaults(cfg *Config) {
	if cfg.Gateway.SyslogListen != "" && !hasPort(cfg.Gateway.SyslogListen) {
		cfg.Gateway.SyslogListen = net.JoinHostPort(cfg.Gateway.SyslogListen, defaultSyslogPort)
	}

	if cfg.Console.TokenTTL == 0 {
		cfg.Console.TokenTTL = defaultConsoleTokenTTL
	}

	if cfg.Scan.Interval == 0 {
		cfg.Scan.Interval = defaultScanInterval
	}

	if cfg.Provider.IPAM == "" {
		cfg.Provider.IPAM = defaultIPAM
	}
	if cfg.Provider.DNS == "" {
		cfg.Provider.DNS = defaultDNS
	}
	if cfg.Provider.Resolver == "" {
		cfg.Provider.Resolver = defaultResolver
	}
	if cfg.Provider.DiskPolicy == "" {
		cfg.Provider.DiskPolicy = defaultDiskPolicy
	}
	if cfg.Provider.HeartbeatMode == "" {
		cfg.Provider.HeartbeatMode = defaultHeartbeatMode
	}
	if cfg.Provider.HeartbeatInterval == 0 {
		cfg.Provider.HeartbeatInterval = defaultHeartbeatInterval
	}
}

// hasPort 判定 addr 是否已经带端口号（形如 host:port）。仅用于 syslog_listen
// 的「省略端口时补默认端口 514」这条便利逻辑的判定，不用于监听地址的合法性
// 校验——Validate 里的具体 IP 判定用 net.SplitHostPort 直接处理，那里的
// 「格式不含冒号」情形已经是拒绝对象，不需要这个辅助函数。
func hasPort(addr string) bool {
	_, _, err := net.SplitHostPort(addr)
	return err == nil
}

// Validate 执行计划任务 15 规定的四类启动校验。任一类不通过即返回非 nil
// 错误，错误信息里包含具体哪个字段、为什么不合法，供运维直接定位。
func (c *Config) Validate() error {
	if err := validateListenAddr("gateway.listen", c.Gateway.Listen); err != nil {
		return err
	}
	if err := validateListenAddr("console.listen", c.Console.Listen); err != nil {
		return err
	}
	if c.Gateway.SyslogListen != "" {
		if err := validateListenAddr("gateway.syslog_listen", c.Gateway.SyslogListen); err != nil {
			return err
		}
	}

	if c.Gateway.GatewayBase == "" {
		return fmt.Errorf("config: gateway.gateway_base 不能为空" +
			"（SPEC §1.4：空值会产出形如 /artifacts/... 的相对 URL，iPXE 对它的解析行为不确定）")
	}
	if c.Gateway.ArtifactBase == "" {
		return fmt.Errorf("config: gateway.artifact_base 不能为空" +
			"（SPEC §1.4：空值会产出形如 /artifacts/... 的相对 URL，iPXE 对它的解析行为不确定）")
	}

	if c.Store.DBPath == "" {
		return fmt.Errorf("config: store.db_path 不能为空")
	}
	if c.Store.ArtifactPath == "" {
		return fmt.Errorf("config: store.artifact_path 不能为空")
	}
	if c.Store.LogPath == "" {
		return fmt.Errorf("config: store.log_path 不能为空")
	}

	if len(c.Auth.Users) == 0 {
		return fmt.Errorf("config: auth.users 不能为空" +
			"（SPEC §9.4：空列表意味着没人能登录，这是一个完全瘫痪的部署）")
	}
	for i, u := range c.Auth.Users {
		if u.Name == "" {
			return fmt.Errorf("config: auth.users[%d] 缺少 name", i)
		}
		if u.BcryptHash == "" {
			return fmt.Errorf("config: auth.users[%d](%s) 缺少 bcrypt_hash", i, u.Name)
		}
		if len(u.Roles) == 0 {
			return fmt.Errorf("config: auth.users[%d](%s) 至少需要一个角色（SPEC §9.4）",
				i, u.Name)
		}
	}

	if len(c.Gateway.RootSSHPublicKeys) == 0 {
		return fmt.Errorf("config: gateway.root_ssh_public_keys 不能为空" +
			"（SPEC §1.5：root 不设可复用密码，注入 SSH 公钥并关闭密码登录——" +
			"公钥是唯一登录通路，空列表会静默产出一台装完后没人能登录的机器，" +
			"这种「错了也不报错」的配置必须在启动时拒绝）")
	}
	for i, key := range c.Gateway.RootSSHPublicKeys {
		if err := validateSSHPublicKey(key); err != nil {
			return fmt.Errorf("config: gateway.root_ssh_public_keys[%d] 不是合法的 SSH 公钥: %w"+
				"（该值会被直接拼进 kickstart 的 sshkey 指令与 cloud-init 的 YAML 列表项，"+
				"带换行/引号/结构字符的值能破坏整个应答文件的结构，SPEC §5.6 同理）", i, err)
		}
	}

	if !heartbeatModes[c.Provider.HeartbeatMode] {
		return fmt.Errorf("config: provider.heartbeat_mode 取值 %q 不合法，"+
			"必须是 tcp/icmp/off/exec/http 之一（SPEC §6.6、§7.2）", c.Provider.HeartbeatMode)
	}

	if !diskPolicies[c.Provider.DiskPolicy] {
		return fmt.Errorf("config: provider.disk_policy 取值 %q 不合法，"+
			"必须是 first/largest/by-model/raid1-ssd/by-hook 之一（SPEC §2.3）",
			c.Provider.DiskPolicy)
	}
	if c.Provider.DiskPolicy == model.DiskPolicyByModel && c.Provider.DiskModel == "" {
		return fmt.Errorf("config: provider.disk_policy 为 by-model 时必须同时提供 " +
			"provider.disk_model（不给的运行期表现是所有机器的磁盘规划都失败，" +
			"不如启动就拒绝——计划任务 15）")
	}

	if err := validateLoaderSelection("provider.ipam", c.Provider.IPAM, "ipam_loader", c.Provider.IPAMLoader); err != nil {
		return err
	}
	if err := validateLoaderSelection("provider.dns", c.Provider.DNS, "dns_loader", c.Provider.DNSLoader); err != nil {
		return err
	}
	if err := validateLoaderSelection("provider.heartbeat_mode", c.Provider.HeartbeatMode, "heartbeat_loader", c.Provider.HeartbeatLoader); err != nil {
		return err
	}
	// Inventory/Notify 没有独立的选择项字段（factory.go 现状：MVP 阶段
	// 两者无条件构造内置版本，不通过配置选择——见 factory.go 顶部契约
	// 缺口说明第 3 条）。本次任务在不改动这条既有事实的前提下，仍然给
	// 两者的 LoaderConfig 做结构性校验：只要 Type 字段非空（运维已经
	// 明确填写了加载器参数段，即使当前工厂还不读取它），就按该 Type
	// 校验 command/url 是否齐备——避免运维填了一份"看起来配置好了"但
	// 其实从未被读取、且内容本身还缺字段的加载器参数，日后 factory.go
	// 补上 Inventory/Notify 的选择项字段时，这份配置已经是校验过的。
	if c.Provider.InventoryLoader.Type != "" {
		if err := validateLoaderSelection("provider.inventory_loader.type",
			c.Provider.InventoryLoader.Type, "inventory_loader", c.Provider.InventoryLoader); err != nil {
			return err
		}
	}
	if c.Provider.NotifyLoader.Type != "" {
		if err := validateLoaderSelection("provider.notify_loader.type",
			c.Provider.NotifyLoader.Type, "notify_loader", c.Provider.NotifyLoader); err != nil {
			return err
		}
	}

	return nil
}

// validateLoaderSelection 校验「selection 是 exec/http 时，对应的
// LoaderConfig 必须结构完整」这条规则（本次任务，SPEC §7.2）。
//
// selection 不是 exec 或 http 时直接放行，不校验 loaderCfg 的任何字段——
// 见 Provider.IPAMLoader 等字段的文档：选择项不是 exec/http 时，加载器
// 参数字段的内容被忽略，运维切换回内置实现不需要同步清空已经写好的
// 加载器参数段。
//
// selection 是 exec 或 http 时的三条校验（任务明确要求的测试点「配置里
// 选了 exec 但没给 command → 启动校验拒绝」，http 的 url 同理）：
//  1. loaderCfg.Type 必须非空且与 selection 相同的类型语义匹配——不要求
//     字符串相等（selection 可能是 "ipam"/"dns" 这类选择项本身，而
//     loaderCfg.Type 取值只能是 "exec"/"http"），真正比较的是 selection
//     是否等于 "exec"/"http" 其中之一，与 loaderCfg.Type 是否等于同一个
//     值——见调用处传入的 selection 参数实际就是 c.Provider.IPAM 这个
//     字符串本身（值恰好是 "exec"/"http" 之一时才会走到本函数的严格
//     校验分支，调用处已经隐含了这一点，见下方 fieldPrefix 参数的用法）。
//  2. Type=exec 时 Command 非空。
//  3. Type=http 时 URL 非空。
//
// fieldPrefix 是错误信息里用来指代"选择项在哪个字段"的字符串（如
// "provider.ipam"），loaderFieldName 是加载器参数段在 YAML 里的字段名
// （如 "ipam_loader"）——两者都只用于拼错误信息，不参与判断逻辑。
func validateLoaderSelection(fieldPrefix, selection, loaderFieldName string, loaderCfg LoaderConfig) error {
	if selection != "exec" && selection != "http" {
		return nil
	}

	if loaderCfg.Type == "" {
		return fmt.Errorf("config: %s 选择了 %q，但 %s 缺少 type 字段"+
			"（必须显式填 type: %s，不能留空——留空会让运行期第一次调用才发现"+
			"加载器没有正确初始化，SPEC §7.2）", fieldPrefix, selection, loaderFieldName, selection)
	}
	if !loaderTypes[loaderCfg.Type] {
		return fmt.Errorf("config: %s.type 取值 %q 不合法，必须是 exec/http 之一"+
			"（grpc/starlark 是 Phase 3，本版本未实现，SPEC §7.2）", loaderFieldName, loaderCfg.Type)
	}
	if loaderCfg.Type != selection {
		return fmt.Errorf("config: %s 选择了 %q，但 %s.type 取值 %q 与之不一致"+
			"（两者必须相同，不能一边说要 exec 一边在加载器参数段填 http，"+
			"这种自相矛盾的配置必须在启动时拒绝）", fieldPrefix, selection, loaderFieldName, loaderCfg.Type)
	}

	switch loaderCfg.Type {
	case "exec":
		if loaderCfg.Command == "" {
			return fmt.Errorf("config: %s 缺少 command 字段"+
				"（type 是 exec 时必须提供要执行的命令，不能等到运行期第一次调用才发现，"+
				"SPEC §7.2）", loaderFieldName)
		}
	case "http":
		if loaderCfg.URL == "" {
			return fmt.Errorf("config: %s 缺少 url 字段"+
				"（type 是 http 时必须提供目标地址，不能等到运行期第一次调用才发现，"+
				"SPEC §7.2）", loaderFieldName)
		}
	}

	return nil
}

// sshKeyTypePrefixes 是 SSH 公钥类型前缀的白名单（OpenSSH authorized_keys
// 格式的第一段）。只收 MVP 实际会用到、且现代的三个家族——ed25519（默认
// 推荐）、rsa（历史兼容）、ecdsa 的三种曲线长度。不收 ssh-dss（DSA 已被
// 视为不安全，OpenSSH 新版本默认拒绝）。
var sshKeyTypePrefixes = map[string]bool{
	"ssh-ed25519":         true,
	"ssh-rsa":             true,
	"ecdsa-sha2-nistp256": true,
	"ecdsa-sha2-nistp384": true,
	"ecdsa-sha2-nistp521": true,
}

// validateSSHPublicKey 校验 v 是否为合法的单行 SSH 公钥（OpenSSH
// authorized_keys 惯例格式）：`<类型前缀> <base64主体>[ <注释>]`。
//
// 这不是完整的密钥学校验（不解析 base64 主体是否真的是一个合法的密钥
// 结构），只是一道注入防护白名单——本包的职责边界与 internal/render/guard
// 一致（见该包文档）：拒绝任何能破坏下游文件格式的字符，不承诺"这真的是
// 一把能用的密钥"。三个消费点各自的格式不同（kickstart 的
// `sshkey --username=root "<key>"` 双引号包裹的一行、cloud-init YAML 的
// `ssh_authorized_keys:` 列表项、preseed late_command 里落盘的
// authorized_keys 文件行），但三者有一个共同的危险字符集：换行（能在
// 目标文件里开出新的一行/新的一条指令）、引号（能提前结束 kickstart 的
// 双引号包裹或 YAML 的字符串边界）。在配置校验这一层统一拒绝这个共同
// 集合，比让每个渲染器分别做转义更不容易漏。
//
// 白名单顺序：
//  1. 非空。
//  2. 按空格切成 2 或 3 段（类型前缀、base64 主体、可选注释）——多于 3 段
//     或少于 2 段直接拒绝，这本身就排除了裸换行造成的"看起来像多行"的
//     输入（strings.Fields 会把换行也当作分隔符，若原始值含换行，被换行
//     分开的两部分会被解析成额外的段，从而使段数不等于 2 或 3 而被拒绝；
//     即便凑巧段数吻合，第 5 步的逐字符白名单也会在类型前缀或 base64
//     段上直接失败，因为换行本身不属于任何允许字符集）。
//  3. 类型前缀必须在 sshKeyTypePrefixes 白名单内。
//  4. base64 主体只允许 base64 字母表字符（A-Z/a-z/0-9/+//=），拒绝任何
//     其它字符——这一步本身就拒绝了引号、单引号、冒号等 YAML/kickstart
//     结构字符，因为它们不在 base64 字母表内。
//  5. 若存在第三段（注释），只拒绝其中会破坏下游格式的字符集
//     （换行、双引号、单引号）——注释本身允许比 base64 更宽的字符集
//     （常见形如 "ops@lober" 的邮箱式注释含 @ 与点号），完全禁止到只剩
//     字母数字会误伤大量合法的真实公钥注释。
func validateSSHPublicKey(v string) error {
	if v == "" {
		return fmt.Errorf("guard: SSH 公钥不能为空")
	}

	fields := strings.Fields(v)
	if len(fields) < 2 || len(fields) > 3 {
		return fmt.Errorf("guard: SSH 公钥 %q 必须是「类型前缀 base64主体[ 注释]」的单行格式，"+
			"拆出 %d 段（想要 2 或 3 段）——裸换行会被当作分隔符拆出额外的段，因此这一步同时"+
			"拒绝了换行注入", v, len(fields))
	}

	if !sshKeyTypePrefixes[fields[0]] {
		return fmt.Errorf("guard: SSH 公钥类型前缀 %q 不在白名单内"+
			"（必须是 ssh-ed25519/ssh-rsa/ecdsa-sha2-nistp256/384/521 之一）", fields[0])
	}

	if err := validateBase64Body(fields[1]); err != nil {
		return fmt.Errorf("guard: SSH 公钥 base64 主体不合法: %w", err)
	}

	if len(fields) == 3 {
		if err := validateSSHKeyComment(fields[2]); err != nil {
			return fmt.Errorf("guard: SSH 公钥注释不合法: %w", err)
		}
	}

	return nil
}

// base64BodyAllowedChars 是标准 base64 字母表（含 URL 不安全变体的 + / 与
// 结尾 = 填充）。SSH 公钥主体统一用标准 base64，不是 URL-safe 变体。
const base64BodyAllowedChars = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/="

// validateBase64Body 校验 v 只含标准 base64 字母表字符。不校验长度是否
// 4 的倍数或 padding 是否正确——那是"这是否是一把可解码的合法密钥"的
// 密钥学问题，本函数只负责注入防护这一层（见 validateSSHPublicKey 文档）。
func validateBase64Body(v string) error {
	if v == "" {
		return fmt.Errorf("base64 主体不能为空")
	}
	for _, c := range v {
		if !strings.ContainsRune(base64BodyAllowedChars, c) {
			return fmt.Errorf("含非 base64 字符 %q", c)
		}
	}
	return nil
}

// sshKeyCommentForbiddenChars 是 SSH 公钥注释段禁止出现的字符集：换行
// （能在目标文件里开出新的一行）、双引号（能提前结束 kickstart
// `sshkey --username=root "<key>"` 的双引号包裹）、单引号（同理，防御
// preseed late_command 里可能出现的单引号包裹场景）。注释段本身允许比
// base64 主体宽得多的字符集（邮箱式注释常见 @、点号），因此这里用"黑名单
// 排除危险字符"而不是"白名单只允许字母数字"，避免误伤合法注释。
const sshKeyCommentForbiddenChars = "\n\r\"'"

func validateSSHKeyComment(v string) error {
	if strings.ContainsAny(v, sshKeyCommentForbiddenChars) {
		return fmt.Errorf("含禁止字符（换行/双引号/单引号会破坏下游应答文件的结构）: %q", v)
	}
	return nil
}

// validateListenAddr 校验监听地址是具体 IP 字面量：拒绝空主机部分、拒绝
// 通配地址、拒绝域名（SPEC §1.4）。
//
// 判定流程：
//  1. net.SplitHostPort 拆出 host 与 port；拆不出来（缺冒号、格式错误）直接
//     拒绝——监听地址必须是 host:port 形式。
//  2. host 为空字符串（形如 ":8080"）拒绝——Go 的 net.Listen 会把空主机部分
//     理解成监听所有接口，这正是 §1.4 警告的通配行为之一。
//  3. net.ParseIP 解析 host；解析失败说明 host 是域名而非 IP 字面量，拒绝——
//     Go 进程应绑回环并由 nginx 做唯一入口，域名解析结果不可控。
//  4. IP 解析成功后检查 IsUnspecified()：0.0.0.0 与 [::] 都会被识别为
//     unspecified，这是标准库对「通配地址」概念的直接支持，比手写字符串
//     比较更不容易漏掉 IPv6 的等价写法（如 "::"、"0:0:0:0:0:0:0:0"）。
func validateListenAddr(field, addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("config: %s 取值 %q 不是合法的 host:port 形式: %w"+
			"（SPEC §1.4：监听地址必须是具体 IP）", field, addr, err)
	}

	if host == "" {
		return fmt.Errorf("config: %s 取值 %q 缺少主机部分"+
			"（SPEC §1.4：空主机部分等价于监听所有接口，这是被禁止的通配行为——"+
			"写成通配地址时一切功能正常、测试全过，只是装机 VLAN 上的裸机"+
			"获得了访问控制台 API 的能力，信任域被静默合并）", field, addr)
	}

	ip := net.ParseIP(host)
	if ip == nil {
		return fmt.Errorf("config: %s 取值 %q 的主机部分 %q 不是 IP 字面量"+
			"（SPEC §1.4：只接受 IP 字面量，Go 进程应绑回环并由 nginx 做唯一入口，"+
			"域名解析结果不可控）", field, addr, host)
	}

	if ip.IsUnspecified() {
		return fmt.Errorf("config: %s 取值 %q 是通配地址"+
			"（SPEC §1.4：写成通配地址时一切功能正常、测试全过，只是装机 VLAN 上的"+
			"裸机获得了访问控制台 API 的能力——信任域被静默合并，Go 进程应绑回环并由"+
			"nginx 做唯一入口，通配地址没有正当用途）", field, addr)
	}

	return nil
}
