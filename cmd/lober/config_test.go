package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func configFile(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "lober.yaml")
	if err := os.WriteFile(path, []byte(body), 0600); err != nil {
		t.Fatal(err)
	}
	return path
}
func Test配置读取默认值及环境覆盖(t *testing.T) {
	t.Setenv("LOBER_PG_DSN", "")
	path := configFile(t, `listen: ":8080"
pg:
  dsn: "file-private-dsn"
  maxConns: 4
log:
  level: "debug"
tls:
  cert: "/etc/lober/cert.pem"
  key: "/etc/lober/key.pem"
sessionTTLHours: 3
seedAdmin: "seed-private-password"
iso:
  staging: "/srv/lober/staging"
  artifacts: "/srv/lober/artifacts"
  smb: "/srv/lober/smb"
  tools: "/usr/lib/lober/tools"
  localRoots: ["/mnt/iso"]
preview:
  gateway: "https://gateway.example.test"
`)
	cfg, err := loadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Listen != ":8080" || cfg.PG.DSN != "file-private-dsn" || cfg.PG.MaxConns != 4 || cfg.SessionTTLHours != 3 || cfg.SeedAdmin != "seed-private-password" || cfg.ISO.Staging != "/srv/lober/staging" || len(cfg.ISO.LocalRoots) != 1 || cfg.Preview.Gateway != "https://gateway.example.test" {
		t.Fatal("configuration values lost")
	}
	t.Setenv("LOBER_PG_DSN", "environment-private-dsn")
	cfg, err = loadConfig(path)
	if err != nil || cfg.PG.DSN != "environment-private-dsn" {
		t.Fatal("environment override failed")
	}
	cfg, err = loadConfig(configFile(t, "listen: ':8080'\n"))
	if err != nil || cfg.SessionTTLHours != 12 || cfg.SeedAdmin != "" || !filepath.IsAbs(cfg.ISO.Staging) {
		t.Fatal("defaults failed", err)
	}
}
func Test配置拒绝缺失非法字段且错误不含秘密(t *testing.T) {
	t.Setenv("LOBER_PG_DSN", "")
	for _, body := range []string{
		"listen: ':8080'\n",
		"pg: {dsn: 'secret-dsn'}\n",
		"listen: 'bad-listener'\npg: {dsn: 'secret-dsn'}\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\nunknown: 'private-password'\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\nsessionTTLHours: -1\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\nsessionTTLHours: 9223372036854775807\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn', maxConns: -1}\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\nlog: {level: nonsense}\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\ntls: {cert: '/only/cert'}\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\niso: {staging: 'relative'}\n",
		"listen: ':8080'\npg: {dsn: 'secret-dsn'}\n---\nlisten: ':9090'\n",
		"listen: [private-password\n",
	} {
		if _, err := loadConfig(configFile(t, body)); err == nil {
			t.Fatal("bad configuration accepted")
		} else if strings.Contains(err.Error(), "secret-dsn") || strings.Contains(err.Error(), "private-password") {
			t.Fatal("configuration error leaked secret")
		}
	}
	if _, err := loadConfig(filepath.Join(t.TempDir(), "missing.yaml")); err == nil {
		t.Fatal("missing file accepted")
	}
}
