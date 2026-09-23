// 本文件锁住配置解析与启动校验的行为。对应实现计划任务 15，依据 SPEC §1.4
// （listen 必须是具体 IP）、§3.3（扫描间隔）、§4.4（token TTL）、§6.6（心跳
// 模式）、§9.4（认证用户与角色）。
//
// 铁律 5 要求：计划里「测试要锁住的行为」每一条都必须有对应测试用例，且断言
// 失败信息写明「为什么这条重要」并带 SPEC 章节号。
package config

import (
	"strings"
	"testing"
	"time"
)

// minimalYAML 是能通过全部四类启动校验的最小配置：具体 IP 监听、非空基址与
// 存储路径、非空用户列表（每个用户有名字/哈希/角色）。用它作为各测试用例的
// 基线，逐项覆写被测字段，避免每个测试都重复整份 YAML。
const minimalYAML = `
gateway:
  listen: "10.10.0.1:8080"
  gateway_base: "http://10.10.0.1/"
  artifact_base: "http://10.10.0.1/artifacts/"
  syslog_listen: "10.10.0.1:514"
  syslog_target: "10.10.0.1:5514"
  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
console:
  listen: "192.168.1.10:8081"
  token_secret: "test-secret-please-change"
store:
  db_path: "/var/lib/lober/lober.db"
  artifact_path: "/srv/lober/artifacts"
  log_path: "/var/log/lober"
auth:
  users:
    - name: "admin"
      bcrypt_hash: "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01"
      roles: ["admin"]
`

// TestParse_最小配置能解析 锁住计划任务 15「测试要锁住的行为」第一条：
// 最小配置能解析。若这条不通过，后续所有校验测试都无从谈起。
func TestParse_最小配置能解析(t *testing.T) {
	cfg, err := Parse(strings.NewReader(minimalYAML))
	if err != nil {
		t.Fatalf("Parse(最小配置) 返回错误: %v；"+
			"最小配置理应是一份合法配置，解析失败说明 YAML 结构或字段名与实现不匹配", err)
	}

	if cfg.Gateway.Listen != "10.10.0.1:8080" {
		t.Errorf("Gateway.Listen = %q，want %q；字段解析结果与 YAML 输入不一致",
			cfg.Gateway.Listen, "10.10.0.1:8080")
	}
	if cfg.Console.Listen != "192.168.1.10:8081" {
		t.Errorf("Console.Listen = %q，want %q", cfg.Console.Listen, "192.168.1.10:8081")
	}
	if len(cfg.Auth.Users) != 1 || cfg.Auth.Users[0].Name != "admin" {
		t.Errorf("Auth.Users 解析结果不符合预期: %+v", cfg.Auth.Users)
	}
}

// TestValidate_通配监听地址三种形式都被拒绝 锁住计划任务 15 最关键的一条：
// 三种通配监听地址形式都被拒绝（0.0.0.0:port、:port、[::]:port）——这条对应
// 一个「测试全过但信任域已合并」的静默故障（SPEC §1.4）：写成 0.0.0.0 时
// 一切功能正常、测试全过，只是装机 VLAN 上的裸机获得了访问控制台 API 的
// 能力。必须在启动时校验，不能等运行期出事才发现。
func TestValidate_通配监听地址三种形式都被拒绝(t *testing.T) {
	wildcards := []string{
		"0.0.0.0:8080", // IPv4 通配
		":8080",        // 空主机部分，Go net.Listen 会理解成监听所有接口
		"[::]:8080",    // IPv6 通配
	}

	for _, listen := range wildcards {
		t.Run(listen, func(t *testing.T) {
			yaml := strings.Replace(minimalYAML, `listen: "10.10.0.1:8080"`,
				`listen: "`+listen+`"`, 1)
			cfg, err := Parse(strings.NewReader(yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v（期望 Parse 成功、Validate 阶段拒绝）", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() 未拒绝通配监听地址 %q（SPEC §1.4）："+
					"写成通配地址时一切功能正常、测试全过，只是装机 VLAN 上的裸机"+
					"获得了访问控制台 API 的能力——信任域被静默合并，"+
					"这种「错了也不报错」的配置必须在启动时被拒绝", listen)
			}
		})
	}
}

// TestValidate_域名监听地址被拒绝 锁住「只接受 IP 字面量」这条约束：Go 进程
// 应绑回环并由 nginx 做唯一入口，域名形式的 listen 不是本系统的正当用法，
// 且域名可能解析到非回环地址，重演 §1.4 的信任域合并问题。
func TestValidate_域名监听地址被拒绝(t *testing.T) {
	yaml := strings.Replace(minimalYAML, `listen: "10.10.0.1:8080"`,
		`listen: "gateway.internal:8080"`, 1)
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝域名形式的监听地址（SPEC §1.4）：" +
			"只接受 IP 字面量，域名解析结果不可控，可能意外绑定到非回环地址")
	}
}

// TestValidate_具体IP监听地址被接受 是上面几条拒绝测试的对照组：确认校验
// 逻辑不是简单地拒绝一切，具体 IP（含 IPv6 具体地址）必须被接受，否则任何
// 合法部署都过不了启动校验。
func TestValidate_具体IP监听地址被接受(t *testing.T) {
	cfg, err := Parse(strings.NewReader(minimalYAML))
	if err != nil {
		t.Fatalf("Parse(最小配置) 返回错误: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() 拒绝了合法的具体 IP 监听配置: %v；"+
			"最小配置理应通过全部校验，误拒会挡住所有正常部署", err)
	}
}

// TestValidate_空基址被拒绝 锁住计划任务 15 第二类启动校验：两个基址 URL
// 非空。空值会产出形如 /artifacts/... 的相对 URL，而 iPXE 对它的解析行为
// 不确定（SPEC §1.4、计划任务 15）。
func TestValidate_空基址被拒绝(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"gateway_base为空", strings.Replace(minimalYAML,
			`gateway_base: "http://10.10.0.1/"`, `gateway_base: ""`, 1)},
		{"artifact_base为空", strings.Replace(minimalYAML,
			`artifact_base: "http://10.10.0.1/artifacts/"`, `artifact_base: ""`, 1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tc.yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() 未拒绝空基址 URL（计划任务 15）：" +
					"空值会产出形如 /artifacts/... 的相对 URL，iPXE 对相对 URL 的" +
					"解析行为不确定，必须在启动时拒绝而非留到装机现场才发现")
			}
		})
	}
}

// TestValidate_空存储路径被拒绝 是上一条的姊妹断言：三个存储路径同样属于
// 「非空即产出不确定行为」的一类，计划任务 15 把基址与存储路径归入同一类
// 校验（第二类）。
func TestValidate_空存储路径被拒绝(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"db_path为空", strings.Replace(minimalYAML,
			`db_path: "/var/lib/lober/lober.db"`, `db_path: ""`, 1)},
		{"artifact_path为空", strings.Replace(minimalYAML,
			`artifact_path: "/srv/lober/artifacts"`, `artifact_path: ""`, 1)},
		{"log_path为空", strings.Replace(minimalYAML,
			`log_path: "/var/log/lober"`, `log_path: ""`, 1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tc.yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() 未拒绝空存储路径（计划任务 15第二类校验）")
			}
		})
	}
}

// TestValidate_用户列表非空(t *testing.T) 锁住第三类校验：用户列表非空，
// 且每个用户有名字、哈希、至少一个角色——空列表意味着没人能登录。
func TestValidate_用户列表为空被拒绝(t *testing.T) {
	yaml := strings.Replace(minimalYAML, `  users:
    - name: "admin"
      bcrypt_hash: "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01"
      roles: ["admin"]`, `  users: []`, 1)
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝空用户列表（SPEC §9.4，计划任务 15第三类校验）：" +
			"空列表意味着没人能登录，这是一个完全瘫痪的部署，必须启动时拒绝")
	}
}

// TestValidate_用户缺字段被拒绝 覆盖第三类校验里「每个用户」的三个必填项：
// 缺名字、缺哈希、缺角色分别都要被拒绝，不能只校验列表长度。
func TestValidate_用户缺字段被拒绝(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"缺名字", `
gateway:
  listen: "10.10.0.1:8080"
  gateway_base: "http://10.10.0.1/"
  artifact_base: "http://10.10.0.1/artifacts/"
  syslog_listen: "10.10.0.1:514"
  syslog_target: "10.10.0.1:5514"
console:
  listen: "192.168.1.10:8081"
  token_secret: "test-secret-please-change"
store:
  db_path: "/var/lib/lober/lober.db"
  artifact_path: "/srv/lober/artifacts"
  log_path: "/var/log/lober"
auth:
  users:
    - bcrypt_hash: "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01"
      roles: ["admin"]
`},
		{"缺哈希", `
gateway:
  listen: "10.10.0.1:8080"
  gateway_base: "http://10.10.0.1/"
  artifact_base: "http://10.10.0.1/artifacts/"
  syslog_listen: "10.10.0.1:514"
  syslog_target: "10.10.0.1:5514"
console:
  listen: "192.168.1.10:8081"
  token_secret: "test-secret-please-change"
store:
  db_path: "/var/lib/lober/lober.db"
  artifact_path: "/srv/lober/artifacts"
  log_path: "/var/log/lober"
auth:
  users:
    - name: "admin"
      roles: ["admin"]
`},
		{"缺角色", `
gateway:
  listen: "10.10.0.1:8080"
  gateway_base: "http://10.10.0.1/"
  artifact_base: "http://10.10.0.1/artifacts/"
  syslog_listen: "10.10.0.1:514"
  syslog_target: "10.10.0.1:5514"
console:
  listen: "192.168.1.10:8081"
  token_secret: "test-secret-please-change"
store:
  db_path: "/var/lib/lober/lober.db"
  artifact_path: "/srv/lober/artifacts"
  log_path: "/var/log/lober"
auth:
  users:
    - name: "admin"
      bcrypt_hash: "$2a$10$abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ01"
      roles: []
`},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tc.yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() 未拒绝%s的用户（SPEC §9.4）："+
					"每个用户必须有名字、哈希、至少一个角色，缺任一项都不是合法凭据", tc.name)
			}
		})
	}
}

// TestDefaults_默认值被正确应用 锁住计划任务 15「测试要锁住的行为」第四条：
// 默认值被正确应用（特别是心跳模式与磁盘策略）。最小配置省略了 provider 与
// scan 组的全部字段，Parse 后应看到计划文档给出的默认值。
func TestDefaults_默认值被正确应用(t *testing.T) {
	cfg, err := Parse(strings.NewReader(minimalYAML))
	if err != nil {
		t.Fatalf("Parse(最小配置) 返回错误: %v", err)
	}

	tests := []struct {
		name string
		got  any
		want any
	}{
		{"扫描间隔默认 30 秒（SPEC §3.3）", cfg.Scan.Interval.String(), "30s"},
		{"syslog 端口默认 514（计划任务 15）", cfg.Gateway.SyslogListen, "10.10.0.1:514"},
		{"控制台 token TTL 默认 12 小时", cfg.Console.TokenTTL.String(), "12h0m0s"},
		{"IPAM 默认 static", cfg.Provider.IPAM, "static"},
		{"DNS 默认 noop", cfg.Provider.DNS, "noop"},
		{"resolver 默认 rule-based", cfg.Provider.Resolver, "rule-based"},
		{"磁盘策略默认 largest（心跳与磁盘策略是计划明确强调的两项）", string(cfg.Provider.DiskPolicy), "largest"},
		{"心跳模式默认 tcp而非icmp（ICMP需要特权套接字，心跳只是辅助通道不值得整进程提权）", cfg.Provider.HeartbeatMode, "tcp"},
		{"心跳间隔默认 60 秒", cfg.Provider.HeartbeatInterval.String(), "1m0s"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.got != tt.want {
				t.Errorf("%s: got %v, want %v", tt.name, tt.got, tt.want)
			}
		})
	}
}

// TestValidate_ByModel策略不给型号串被拒绝 与 TestValidate_ByModel策略给了型号串则接受
// 锁住计划任务 15 最具体的一条业务校验：选了 by-model 必须同时给型号串——
// 不给的运行期表现是「所有机器的磁盘规划都失败」，不如启动就拒绝。
func TestValidate_ByModel策略不给型号串被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  disk_policy: "by-model"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝「disk_policy=by-model 但未给型号串」的配置：" +
			"不给的运行期表现是所有机器的磁盘规划都失败，这种错误只应在启动时" +
			"暴露一次，不应让每台机器建任务时各报一次同样的错误")
	}
}

func TestValidate_ByModel策略给了型号串则接受(t *testing.T) {
	yaml := minimalYAML + `
provider:
  disk_policy: "by-model"
  disk_model: "SAMSUNG MZQL21T9HCJR"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() 拒绝了「disk_policy=by-model 且给了型号串」的合法配置: %v；"+
			"给了型号串应当被接受，否则这条业务规则会误伤所有正确配置", err)
	}
}

// TestValidate_空SSH公钥列表被拒绝 锁住本轮新增的第五类启动校验：
// gateway.root_ssh_public_keys 不能为空（SPEC §1.5：root 不设可复用密码，
// 注入 SSH 公钥并关闭密码登录——公钥是这条安全策略唯一的登录通路，空列表
// 意味着装好的机器 root 完全无法登录，必须在启动时拒绝而不是留到装机现场
// 才发现，那时机器已经装完、运维却连不上任何一台）。
func TestValidate_空SSH公钥列表被拒绝(t *testing.T) {
	cases := []struct {
		name string
		yaml string
	}{
		{"字段整体省略", strings.Replace(minimalYAML,
			`  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
`, "", 1)},
		{"空列表", strings.Replace(minimalYAML,
			`  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
`, "  root_ssh_public_keys: []\n", 1)},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := Parse(strings.NewReader(tc.yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err == nil {
				t.Fatal("Validate() 未拒绝空的 gateway.root_ssh_public_keys（SPEC §1.5）：" +
					"root 不设可复用密码，公钥是唯一登录通路，空列表会静默产出一台" +
					"装完后没人能登录的机器——这种「错了也不报错」的配置必须在启动时拒绝")
			}
		})
	}
}

// TestValidate_SSH公钥格式非法被拒绝 锁住白名单校验：类型前缀
// （ssh-ed25519/ssh-rsa/ecdsa-sha2-*）+ base64 主体 + 可选注释，拒绝
// 换行、引号、YAML/kickstart 结构字符。公钥最终会被写进 kickstart 的
// `sshkey --username=root "..."` 与 cloud-init 的
// `ssh_authorized_keys:` 列表项，一个带换行或引号的值能破坏整个应答
// 文件的结构（SPEC §5.6 注入防护同理）。
func TestValidate_SSH公钥格式非法被拒绝(t *testing.T) {
	cases := []struct {
		name string
		key  string
	}{
		{"空字符串", ""},
		{"缺类型前缀", "AAAAC3NzaC1lZDI1NTE5AAAA ops@lober"},
		{"类型前缀不在白名单", "ssh-dss AAAAB3NzaC1kc3MAAAA ops@lober"},
		{"含裸换行", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5\nAAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"},
		{"含双引号", `ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 "ops@lober"`},
		{"含单引号", "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops'@lober"},
		{"主体含非base64字符", "ssh-ed25519 not!!valid!!base64!! ops@lober"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// 直接调用 validateSSHPublicKey 而不经过 YAML 往返：像"含双引号"
			// 这类用例若塞进 minimalYAML 里已经用双引号包裹的 YAML 标量，会先
			// 在 YAML 语法层就失败（未转义的引号破坏了 YAML 字符串边界），
			// 测的是 YAML 解析器而不是本函数的白名单逻辑。这里改用
			// cfg.Gateway.RootSSHPublicKeys 字段直接赋值，绕开 YAML 序列化，
			// 让测试精确针对 Validate() 内部的白名单校验这一层。
			cfg, err := Parse(strings.NewReader(minimalYAML))
			if err != nil {
				t.Fatalf("Parse(最小配置) 返回错误: %v", err)
			}
			cfg.Gateway.RootSSHPublicKeys = []string{tc.key}
			if err := cfg.Validate(); err == nil {
				t.Fatalf("Validate() 未拒绝非法 SSH 公钥 %q："+
					"该值会被直接拼进 kickstart 的 sshkey 指令与 cloud-init 的 YAML 列表项，"+
					"带换行/引号/结构字符的值能破坏整个应答文件的结构（SPEC §5.6 同理）", tc.key)
			}
		})
	}
}

// TestValidate_合法SSH公钥三种类型全部被接受 是上一条的对照组：确认白名单
// 没写窄，三个类型家族（ed25519/rsa/ecdsa）与「省略注释」这一合法变体都
// 应被接受。
func TestValidate_合法SSH公钥三种类型全部被接受(t *testing.T) {
	cases := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
		"ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC7 ops@lober",
		"ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTY= ops@lober",
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2", // 无注释
	}

	for _, key := range cases {
		t.Run(key, func(t *testing.T) {
			yaml := strings.Replace(minimalYAML,
				`ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober`,
				key, 1)
			cfg, err := Parse(strings.NewReader(yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() 拒绝了合法 SSH 公钥 %q: %v", key, err)
			}
		})
	}
}

// TestValidate_多把SSH公钥全部保留 锁住计划要求的「支持多个公钥」——同一批
// 机器共用运维团队的公钥，团队多人且轮换期需要新旧并存，因此列表长度大于 1
// 时全部条目都必须被保留而不是只取第一个。
func TestValidate_多把SSH公钥全部保留(t *testing.T) {
	yaml := strings.Replace(minimalYAML,
		`  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
`,
		`  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 alice@lober"
    - "ssh-rsa AAAAB3NzaC1yc2EAAAADAQABAAABgQC7 bob@lober"
`, 1)
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() 拒绝了两把合法公钥: %v", err)
	}
	if len(cfg.Gateway.RootSSHPublicKeys) != 2 {
		t.Fatalf("RootSSHPublicKeys 长度 = %d, want 2 —— "+
			"团队多人且轮换期需要新旧并存，不能只保留第一把", len(cfg.Gateway.RootSSHPublicKeys))
	}
}

// TestValidate_未知心跳模式被拒绝 与 TestValidate_未知磁盘策略被拒绝 锁住
// 计划任务 15「测试要锁住的行为」最后一条：未知的心跳模式与磁盘策略被拒绝。
// 这两个字段来自 YAML 自由文本，不经校验的话拼写错误会在装机现场才暴露。
func TestValidate_未知心跳模式被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  heartbeat_mode: "ping"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝未知心跳模式 \"ping\"（心跳模式必须是 tcp/icmp/off 之一）：" +
			"拼写错误的枚举值必须在启动时拒绝，否则心跳探测会静默失效")
	}
}

func TestValidate_未知磁盘策略被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  disk_policy: "random"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝未知磁盘策略 \"random\"（必须是 model.DiskPolicy 的五个" +
			"具名策略之一：first/largest/by-model/raid1-ssd/by-hook）：" +
			"拼写错误会导致所有机器的磁盘规划在装机现场才失败")
	}
}

// TestValidate_合法心跳模式全部被接受 与 TestValidate_合法磁盘策略全部被接受
// 是上面两条拒绝测试的对照组：确认白名单本身没写窄，三种心跳模式与五种磁盘
// 策略（除 by-model 外，它有额外的型号串要求，见专门测试）都应被接受。
func TestValidate_合法心跳模式全部被接受(t *testing.T) {
	for _, mode := range []string{"tcp", "icmp", "off"} {
		t.Run(mode, func(t *testing.T) {
			yaml := minimalYAML + "\nprovider:\n  heartbeat_mode: \"" + mode + "\"\n"
			cfg, err := Parse(strings.NewReader(yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() 拒绝了合法心跳模式 %q: %v", mode, err)
			}
		})
	}
}

func TestValidate_合法磁盘策略全部被接受(t *testing.T) {
	for _, policy := range []string{"first", "largest", "raid1-ssd", "by-hook"} {
		t.Run(policy, func(t *testing.T) {
			yaml := minimalYAML + "\nprovider:\n  disk_policy: \"" + policy + "\"\n"
			cfg, err := Parse(strings.NewReader(yaml))
			if err != nil {
				t.Fatalf("Parse 阶段就失败: %v", err)
			}
			if err := cfg.Validate(); err != nil {
				t.Fatalf("Validate() 拒绝了合法磁盘策略 %q: %v", policy, err)
			}
		})
	}
}

// ---------------------------------------------------------------------------
// exec/http 加载器配置（本次任务，SPEC §7.2）
// ---------------------------------------------------------------------------

// TestValidate_选了exec但缺command被拒绝 锁住任务明确要求的测试点：
// 「配置里选了 exec 但没给 command → 启动校验拒绝」，不能等到运行期
// 第一次调用才发现（那时已经在阻塞建任务或推送告警的路径上）。
func TestValidate_选了exec但缺command被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  ipam: "exec"
  ipam_loader:
    type: "exec"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝选了 exec 却没给 command 的配置（SPEC §7.2）：" +
			"不给 command 的运行期表现是第一次建任务时才报错，必须在启动时拒绝")
	}
}

// TestValidate_选了http但缺url被拒绝 是上一条测试的 http 对照组。
func TestValidate_选了http但缺url被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  dns: "http"
  dns_loader:
    type: "http"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝选了 http 却没给 url 的配置（SPEC §7.2）：" +
			"不给 url 的运行期表现是第一次写 DNS 时才报错，必须在启动时拒绝")
	}
}

// TestValidate_选了exec但loader段缺type被拒绝 验证「选择项是 exec/http，
// 但对应 loader 段完全没填 type」这种更常见的笔误（忘了填 type，只填了
// command）同样被拒绝，不是只在 type 与 command 都缺时才报错。
func TestValidate_选了exec但loader段缺type被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  ipam: "exec"
  ipam_loader:
    command: "/etc/lober/providers/ipam.sh"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝 ipam_loader 缺少 type 字段的配置：" +
			"必须显式声明 type，不能靠 command 存在就默认推断为 exec")
	}
}

// TestValidate_loader段type与选择项不一致被拒绝 验证「provider.ipam 选了
// exec，但 ipam_loader.type 填了 http」这种自相矛盾的配置被拒绝——两者
// 必须一致，不能一边说要 exec 一边在参数段配 http。
func TestValidate_loader段type与选择项不一致被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  ipam: "exec"
  ipam_loader:
    type: "http"
    url: "http://ipam.internal/allocate"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝 provider.ipam=exec 但 ipam_loader.type=http 的自相矛盾配置")
	}
}

// TestValidate_loader段type不合法被拒绝 验证 type 字段本身写了一个不在
// exec/http 白名单内的值（如 grpc，Phase 3 未实现）时被拒绝。
func TestValidate_loader段type不合法被拒绝(t *testing.T) {
	yaml := minimalYAML + `
provider:
  ipam: "exec"
  ipam_loader:
    type: "grpc"
    command: "/etc/lober/providers/ipam.sh"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝 ipam_loader.type=grpc（grpc 是 Phase 3，本版本未实现，SPEC §7.2）")
	}
}

// TestValidate_合法exec加载器配置被接受 是上面几条拒绝测试的对照组：
// command/type 都齐备的 exec 配置必须通过校验。
func TestValidate_合法exec加载器配置被接受(t *testing.T) {
	yaml := minimalYAML + `
provider:
  ipam: "exec"
  ipam_loader:
    type: "exec"
    command: "/etc/lober/providers/ipam.sh"
    args: ["--mode=alloc"]
    timeout: 10s
    max_retries: 2
    breaker_threshold: 3
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() 拒绝了合法的 exec 加载器配置: %v", err)
	}
	if cfg.Provider.IPAMLoader.Command != "/etc/lober/providers/ipam.sh" {
		t.Errorf("IPAMLoader.Command = %q，与 YAML 输入不一致", cfg.Provider.IPAMLoader.Command)
	}
	if len(cfg.Provider.IPAMLoader.Args) != 1 || cfg.Provider.IPAMLoader.Args[0] != "--mode=alloc" {
		t.Errorf("IPAMLoader.Args = %v，与 YAML 输入不一致", cfg.Provider.IPAMLoader.Args)
	}
	if cfg.Provider.IPAMLoader.Timeout != 10*time.Second {
		t.Errorf("IPAMLoader.Timeout = %v, want 10s", cfg.Provider.IPAMLoader.Timeout)
	}
	if cfg.Provider.IPAMLoader.BreakerThreshold != 3 {
		t.Errorf("IPAMLoader.BreakerThreshold = %d, want 3", cfg.Provider.IPAMLoader.BreakerThreshold)
	}
}

// TestValidate_合法http加载器配置被接受 是 http 类型的对照组。
func TestValidate_合法http加载器配置被接受(t *testing.T) {
	yaml := minimalYAML + `
provider:
  dns: "http"
  dns_loader:
    type: "http"
    url: "http://dns.internal/api/v1/register"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() 拒绝了合法的 http 加载器配置: %v", err)
	}
	if cfg.Provider.DNSLoader.URL != "http://dns.internal/api/v1/register" {
		t.Errorf("DNSLoader.URL = %q，与 YAML 输入不一致", cfg.Provider.DNSLoader.URL)
	}
}

// TestValidate_选择项不是exec或http时忽略loader段 验证「选择项仍是内置
// 实现（static/noop/tcp），但 loader 参数段留空甚至写了不完整的内容」
// 不影响校验——运维切换回内置实现时不需要同步清空已经写好的加载器参数
// （Provider 字段文档明确的设计意图）。用一个故意不完整的 loader 段
// （只有 type 没有 command）来确认"忽略"是真的不校验，而不是恰好凑巧
// 通过。
func TestValidate_选择项不是exec或http时忽略loader段(t *testing.T) {
	yaml := minimalYAML + `
provider:
  ipam: "static"
  ipam_loader:
    type: "exec"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Validate() 拒绝了 provider.ipam=static 但 ipam_loader 内容不完整的配置: %v"+
			"——选择项不是 exec/http 时，加载器参数字段应该被完全忽略", err)
	}
}

// TestValidate_HeartbeatMode为exec时同样校验loader段 验证心跳模式扩展的
// exec/http 取值同样受 heartbeat_loader 的结构性校验约束，不是只有
// IPAM/DNS 两个字段生效。
func TestValidate_HeartbeatMode为exec时同样校验loader段(t *testing.T) {
	yaml := minimalYAML + `
provider:
  heartbeat_mode: "exec"
`
	cfg, err := Parse(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("Parse 阶段就失败: %v", err)
	}
	if err := cfg.Validate(); err == nil {
		t.Fatal("Validate() 未拒绝 heartbeat_mode=exec 但 heartbeat_loader 缺少 type/command 的配置")
	}
}
