package discovery

import (
	"strings"
	"testing"
)

func TestReport(t *testing.T) {
	good := `{"sections":[{"key":"storage","title":"磁盘","status":"unread","reason":"缺驱动","items":[]}]}`
	for _, tc := range []struct {
		name, body string
		ok         bool
	}{
		{"部分信息可上报", good, true},
		{"不允许额外字段", strings.Replace(good, `"sections"`, `"extra":true,"sections"`, 1), false},
		{"状态必须已知", strings.Replace(good, "unread", "invalid", 1), false},
		{"未知类别", strings.Replace(good, "storage", "other", 1), false},
		{"报告不能为空", `{"sections":[]}`, false},
		{"必须用原因解释未读取", strings.Replace(good, `"reason":"缺驱动",`, "", 1), false},
		{"报告数量有上限", strings.Repeat(" ", MaxReportBytes+1), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, err := ParseReport([]byte(tc.body))
			if (err == nil) != tc.ok {
				t.Fatalf("ParseReport = %v", err)
			}
		})
	}
}
func TestMissingSectionsAreUnread(t *testing.T) {
	report, err := ParseReport([]byte(`{"sections":[{"key":"storage","title":"磁盘","status":"unread","reason":"缺驱动","items":[]}]}`))
	if err != nil || len(report.Sections) != 8 {
		t.Fatalf("缺项未补齐: %+v %v", report, err)
	}
	for _, section := range report.Sections {
		if section.Status != "unread" || section.Reason == "" {
			t.Fatalf("没有解释缺项: %+v", section)
		}
	}
}

func TestMAC(t *testing.T) {
	for input, want := range map[string]string{"52-54-00-12-34-56": "52:54:00:12:34:56", "52:54:00:AB:34:56": "52:54:00:ab:34:56", "ff:ff:ff:ff:ff:ff": "", "00:00:00:00:00:00": "", "52:54:00:12:34:56:11:22": "", "bad": ""} {
		if got := MAC(input); got != want {
			t.Errorf("MAC(%q)=%q, want %q", input, got, want)
		}
	}
}
