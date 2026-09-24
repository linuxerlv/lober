package main

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

// TestRun_VersionFlagPrintsAndExits 断言 -version 打印版本信息后退出，
// 退出码为 0（计划任务 5「验证」：带版本注入构建后 ./lober -version 打印
// 版本）。
func TestRun_VersionFlagPrintsAndExits(t *testing.T) {
	if code := run([]string{"-version"}); code != 0 {
		t.Errorf("run([-version]) 退出码 = %d, want 0", code)
	}
}

// TestRun_CheckModePassesOnValidConfig 断言 -check 在样例配置上通过——
// 只加载并校验配置、跑装配与启动自检，不起服务（计划任务 5「验证」）。
func TestRun_CheckModePassesOnValidConfig(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "lober.yaml")
	writeMinimalValidConfigFile(t, configPath, dir)

	code := run([]string{"-config", configPath, "-check"})
	if code != 0 {
		t.Errorf("run([-config, %s, -check]) 退出码 = %d, want 0", configPath, code)
	}
}

// TestRun_MissingConfigFlagFails 断言缺少 -config 时以非零退出码失败，
// 而不是 panic 或静默使用某个隐藏的默认路径。
func TestRun_MissingConfigFlagFails(t *testing.T) {
	if code := run([]string{}); code == 0 {
		t.Error("run([]) 缺少 -config 时应该失败，但返回了 0")
	}
}

// TestRun_InvalidConfigPathFails 断言配置文件路径不存在时失败，错误信息
// 能定位到"文件打不开"这一具体原因。
func TestRun_InvalidConfigPathFails(t *testing.T) {
	if code := run([]string{"-config", "/no/such/path/lober.yaml", "-check"}); code == 0 {
		t.Error("配置文件路径不存在时应该失败，但返回了 0")
	}
}

// writeMinimalValidConfigFile 写一份能通过 config.Validate 的最小 YAML
// 配置到 path：两个本地路径指向 dir 下的子目录，数据库指向一个用完即删的
// 独立 PG schema（-check 会走完整装配，包括真的连库并跑 AutoMigrate）。
func writeMinimalValidConfigFile(t *testing.T, path, dir string) {
	t.Helper()
	pg := testPostgres(t)
	content := `
gateway:
  listen: "127.0.0.1:18180"
  gateway_base: "http://127.0.0.1:18180"
  artifact_base: "http://127.0.0.1:18181"
  syslog_listen: "127.0.0.1:18614"
  root_ssh_public_keys:
    - "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober"
console:
  listen: "127.0.0.1:18190"
  token_secret: "test-secret"
store:
  postgres:
    host: "` + pg.Host + `"
    port: ` + strconv.Itoa(pg.Port) + `
    user: "` + pg.User + `"
    password: "` + pg.Password + `"
    dbname: "` + pg.DBName + `"
    sslmode: "` + pg.SSLMode + `"
    search_path: "` + pg.SearchPath + `"
  artifact_path: "` + filepath.ToSlash(filepath.Join(dir, "artifacts")) + `"
  log_path: "` + filepath.ToSlash(filepath.Join(dir, "log")) + `"
auth:
  users:
    - name: "admin"
      bcrypt_hash: "$2a$10$7EqJtq98hPqEX7fNZaFWoOhi6vjuLpk3wJ0Qe0KcJvJ8G7ZbtG9Ia"
      roles: ["admin"]
provider:
  ipam: "static"
  dns: "noop"
  resolver: "rule-based"
  disk_policy: "largest"
  heartbeat_mode: "off"
`
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatalf("写配置文件失败: %v", err)
	}
}
