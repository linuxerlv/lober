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
var heartbeatModes = map[string]bool{
	"tcp":  true,
	"icmp": true,
	"off":  true,
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
// 选择、磁盘策略、心跳模式与间隔。
//
// 这里的选择项是字符串而非强类型枚举，因为 IPAM/DNS/resolver 目前只有单一
// 内置实现（static/noop/rule-based），config 阶段不需要也不应该白名单校验
// 它们——一旦通用 provider 加载器（SPEC §7.2，MVP 不做）落地，这些字段的
// 合法值会由 M7 的实现集合决定，而不是 config 包能提前穷举的。DiskPolicy
// 与 HeartbeatMode 不同：它们的合法值在 MVP 阶段就是确定且封闭的（前者取自
// model.DiskPolicy，后者是 tcp/icmp/off 三选一），因此在这里做白名单校验。
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

	// HeartbeatMode 默认 tcp（SPEC §6.6、计划任务 15）。
	HeartbeatMode string `yaml:"heartbeat_mode"`
	// HeartbeatInterval 默认 60 秒。
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
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

	if !heartbeatModes[c.Provider.HeartbeatMode] {
		return fmt.Errorf("config: provider.heartbeat_mode 取值 %q 不合法，"+
			"必须是 tcp/icmp/off 之一（SPEC §6.6）", c.Provider.HeartbeatMode)
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
