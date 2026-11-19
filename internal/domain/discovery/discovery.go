package discovery

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/lober/lober/internal/domain/catalog"
)

const MaxReportBytes = 512 * 1024

var sectionKinds = []struct{ key, title string }{
	{"system", "整机与固件"}, {"cpu", "处理器"}, {"memory", "内存"},
	{"storage", "磁盘与阵列"}, {"network", "网卡"}, {"expansion", "扩展卡"},
	{"power", "电源"}, {"cooling", "风扇与温度"},
}

var (
	ErrIdentityConflict = errors.New("启动网口与本次报告的主机序列号不符")
	ErrDisabled         = errors.New("自动发现未启用")
	ErrReport           = errors.New("硬件报告格式无效")
	ErrToken            = errors.New("采集凭据无效或已过期")
)

type Report struct {
	Sections []Section `json:"sections"`
}

type Section struct {
	Key    string `json:"key"`
	Title  string `json:"title"`
	Status string `json:"status"`
	Reason string `json:"reason,omitempty"`
	Items  []Item `json:"items"`
}

type Item struct {
	Name   string            `json:"name"`
	Fields map[string]string `json:"fields"`
}

type Record struct {
	MAC          string           `json:"mac"`
	SN           string           `json:"sn,omitempty"`
	Arch         catalog.Arch     `json:"arch"`
	Firmware     catalog.Firmware `json:"firmware"`
	FirstSeenAt  int64            `json:"first_seen_at"`
	LastSeenAt   int64            `json:"last_seen_at"`
	Status       string           `json:"status"`
	Reason       string           `json:"reason,omitempty"`
	RegisteredSN string           `json:"registered_sn,omitempty"`

	SystemModel string  `json:"system_model,omitempty"`
	ReportAt    int64   `json:"report_at,omitempty"`
	Report      *Report `json:"report,omitempty"`
}

type Attempt struct {
	MAC, SN, ID   string
	Arch          catalog.Arch
	Firmware      catalog.Firmware
	At, ExpiresAt int64
	Reason        string
}

func MAC(raw string) string {
	raw = strings.ToLower(strings.ReplaceAll(raw, "-", ":"))
	if len(raw) != 17 || raw == "00:00:00:00:00:00" {
		return ""
	}
	parts := strings.Split(raw, ":")
	if len(parts) != 6 {
		return ""
	}
	for i, part := range parts {
		if len(part) != 2 {
			return ""
		}
		value, err := hex.DecodeString(part)
		if err != nil || i == 0 && value[0]&1 != 0 {
			return ""
		}
	}
	return raw
}

func ParseReport(body []byte) (Report, error) {
	if len(body) > MaxReportBytes || !utf8.Valid(body) || !json.Valid(body) {
		return Report{}, ErrReport
	}
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.DisallowUnknownFields()
	var report Report
	if dec.Decode(&report) != nil || len(report.Sections) == 0 || len(report.Sections) > 8 {
		return Report{}, ErrReport
	}
	seen := make(map[string]bool, len(report.Sections))
	for i := range report.Sections {
		section := &report.Sections[i]
		if seen[section.Key] || !validSection(*section) {
			return Report{}, ErrReport
		}
		seen[section.Key] = true
		if section.Items == nil {
			section.Items = []Item{}
		}
	}
	// 补上采集镜像完全漏报的类别，详情页能明确显示缺项。
	for _, kind := range sectionKinds {
		if !seen[kind.key] {
			report.Sections = append(report.Sections, Section{Key: kind.key, Title: kind.title, Status: "unread", Reason: "采集镜像未上报此项", Items: []Item{}})
		}
	}
	return report, nil
}

func validSection(s Section) bool {
	switch s.Key {
	case "system", "cpu", "memory", "storage", "network", "expansion", "power", "cooling":
	default:
		return false
	}
	if !plain(s.Title, 128) || strings.TrimSpace(s.Title) == "" || len(s.Items) > 2048 {
		return false
	}
	switch s.Status {
	case "read":
		if len(s.Items) == 0 {
			return false
		}
	case "absent":
		if len(s.Items) != 0 {
			return false
		}
	case "unread":
		if len(s.Items) != 0 || strings.TrimSpace(s.Reason) == "" {
			return false
		}
	case "partial":
		if strings.TrimSpace(s.Reason) == "" {
			return false
		}
	default:
		return false
	}
	if !plain(s.Reason, 2048) {
		return false
	}
	for _, item := range s.Items {
		if !plain(item.Name, 512) || strings.TrimSpace(item.Name) == "" || len(item.Fields) > 64 {
			return false
		}
		for key, value := range item.Fields {
			if strings.TrimSpace(key) == "" || !plain(key, 128) || !plain(value, 4096) {
				return false
			}
		}
	}
	return true
}

func plain(s string, max int) bool {
	return len(s) <= max && !strings.ContainsFunc(s, unicode.IsControl)
}
