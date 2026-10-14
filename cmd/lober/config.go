package main

import (
	"errors"
	"io"
	"math"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type config struct {
	Listen          string    `yaml:"listen"`
	PG              pgConfig  `yaml:"pg"`
	Log             logConfig `yaml:"log"`
	TLS             tlsConfig `yaml:"tls"`
	SessionTTLHours int64     `yaml:"sessionTTLHours"`
	// SeedAdmin is the bootstrap password for username admin. The stored user
	// must change it before any operational API is available.
	SeedAdmin string        `yaml:"seedAdmin"`
	ISO       isoConfig     `yaml:"iso"`
	Preview   previewConfig `yaml:"preview"`
}
type pgConfig struct {
	DSN      string `yaml:"dsn"`
	MaxConns int    `yaml:"maxConns"`
}
type logConfig struct {
	Level string `yaml:"level"`
}
type tlsConfig struct {
	Cert string `yaml:"cert"`
	Key  string `yaml:"key"`
}
type isoConfig struct {
	Staging    string   `yaml:"staging"`
	Artifacts  string   `yaml:"artifacts"`
	SMB        string   `yaml:"smb"`
	Tools      string   `yaml:"tools"`
	LocalRoots []string `yaml:"localRoots"`
	Workers    int      `yaml:"workers"`
	PollMS     int      `yaml:"pollMS"`
}
type previewConfig struct {
	Gateway    string `yaml:"gateway"`
	Artifacts  string `yaml:"artifacts"`
	SMB        string `yaml:"smb"`
	SyslogAddr string `yaml:"syslogAddr"`
}

func loadConfig(path string) (config, error) {
	var c config
	file, err := os.Open(path)
	if err != nil {
		return c, errors.New("无法读取配置文件")
	}
	defer file.Close()
	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	if err := decoder.Decode(&c); err != nil {
		return config{}, errors.New("配置 YAML 不合法或包含未知字段")
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return config{}, errors.New("配置只能包含一份 YAML 文档")
	}
	if dsn := os.Getenv("LOBER_PG_DSN"); dsn != "" {
		c.PG.DSN = dsn
	}
	if strings.TrimSpace(c.PG.DSN) == "" {
		return config{}, errors.New("缺少 pg.dsn 或 LOBER_PG_DSN")
	}
	_, port, err := net.SplitHostPort(c.Listen)
	n, parseErr := strconv.ParseUint(port, 10, 16)
	if err != nil || parseErr != nil || n == 0 {
		return config{}, errors.New("listen 必须是带非零端口的监听地址")
	}
	if c.SessionTTLHours == 0 {
		c.SessionTTLHours = 12
	}
	if c.SessionTTLHours < 0 || c.SessionTTLHours > math.MaxInt64/3600 {
		return config{}, errors.New("sessionTTLHours 超出可用范围")
	}
	if c.PG.MaxConns == 0 {
		c.PG.MaxConns = 16
	}
	if c.PG.MaxConns < 1 {
		return config{}, errors.New("pg.maxConns 必须为正数")
	}
	if c.SeedAdmin == "" {
		c.SeedAdmin = "admin"
	}
	if c.Log.Level == "" {
		c.Log.Level = "info"
	}
	switch c.Log.Level {
	case "debug", "info", "warn", "error":
	default:
		return config{}, errors.New("log.level 必须是 debug、info、warn 或 error")
	}
	if (c.TLS.Cert == "") != (c.TLS.Key == "") {
		return config{}, errors.New("tls.cert 与 tls.key 必须同时提供")
	}
	if c.ISO.Staging == "" {
		c.ISO.Staging = "/var/lib/lober/staging"
	}
	if c.ISO.Artifacts == "" {
		c.ISO.Artifacts = "/var/lib/lober/artifacts"
	}
	if c.ISO.SMB == "" {
		c.ISO.SMB = "/var/lib/lober/smb"
	}
	if c.ISO.Tools == "" {
		c.ISO.Tools = "/usr/lib/lober/tools"
	}
	for _, dir := range append([]string{c.ISO.Staging, c.ISO.Artifacts, c.ISO.SMB, c.ISO.Tools}, c.ISO.LocalRoots...) {
		if !filepath.IsAbs(dir) || strings.ContainsRune(dir, 0) {
			return config{}, errors.New("ISO 目录和本地介质允许根必须是绝对路径")
		}
	}
	if c.ISO.Workers == 0 {
		c.ISO.Workers = 2
	}
	if c.ISO.Workers < 1 {
		return config{}, errors.New("iso.workers 必须为正数")
	}
	if c.ISO.PollMS == 0 {
		c.ISO.PollMS = 1000
	}
	if c.ISO.PollMS < 1 || int64(c.ISO.PollMS) > math.MaxInt64/1000000 {
		return config{}, errors.New("iso.pollMS 超出可用范围")
	}
	return c, nil
}
