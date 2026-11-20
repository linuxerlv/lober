package inventory

import (
	"errors"
	"strings"
	"testing"
)

const goodBody = `{
  "collected_at": "2026-11-01T10:00:00Z",
  "firmware": "uefi",
  "cpu": {"model": "Intel Xeon Gold 6248R", "cores": 48},
  "memory_mib": 32768,
  "system": {"manufacturer": "Dell Inc.", "product": "PowerEdge R750"},
  "disks": [
    {"name": "sda", "bytes": 10737418240, "model": "PERC H755", "rotational": false},
    {"name": "sdb", "bytes": 8589934592, "model": "QEMU HARDDISK", "rotational": false}
  ]
}`

func Test解析合法上报(t *testing.T) {
	got, problems, err := Parse([]byte(goodBody))
	if err != nil || len(problems) != 0 {
		t.Fatal(err, problems)
	}
	if got.SystemDisk != "sdb" {
		t.Fatalf("系统盘 = %q，想要 sdb（8 GiB 比 10 GiB 小）", got.SystemDisk)
	}
	if got.Attrs[InvSystemMode] != "Dell Inc. PowerEdge R750" {
		t.Fatalf("整机型号 = %q", got.Attrs[InvSystemMode])
	}
}

// 网络字段出现就整体拒收，并且给专门的错误，好让日志说清楚「不是格式错」。
func Test上报里出现网络字段就拒收(t *testing.T) {
	for _, key := range []string{"ip", "mac", "netmask", "gateway", "dns", "interfaces"} {
		body := strings.Replace(goodBody, `"collected_at"`, `"`+key+`": "x", "collected_at"`, 1)
		_, _, err := Parse([]byte(body))
		if !errors.Is(err, ErrForbiddenKey) {
			t.Fatalf("顶层 %q：想要 ErrForbiddenKey，得到 %v", key, err)
		}
	}
	// 嵌套的磁盘项里也不许出现。
	nested := strings.Replace(goodBody, `"rotational": false`, `"rotational": false, "mac": "aa:bb:cc:dd:ee:ff"`, 1)
	if _, _, err := Parse([]byte(nested)); !errors.Is(err, ErrForbiddenKey) {
		t.Fatalf("磁盘项：得到 %v", err)
	}
	// cpu 里也不许。
	inCPU := strings.Replace(goodBody, `"cores": 48`, `"cores": 48, "ip": "10.0.0.1"`, 1)
	if _, _, err := Parse([]byte(inCPU)); !errors.Is(err, ErrForbiddenKey) {
		t.Fatalf("cpu：得到 %v", err)
	}
}

// 不认识的键一律拒收，不做「认识几个收几个」。
func Test不认识的键拒收(t *testing.T) {
	body := strings.Replace(goodBody, `"firmware": "uefi",`, `"firmware": "uefi", "vendor_extra": 1,`, 1)
	if _, _, err := Parse([]byte(body)); !errors.Is(err, ErrMalformed) {
		t.Fatalf("想要 ErrMalformed，得到 %v", err)
	}
}

func Test格式不对拒收(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
	}{
		{"空", ""},
		{"不是 JSON", "hello"},
		{"不是对象", `[1,2,3]`},
		{"字段类型不对", `{"cpu": "not-an-object"}`},
		{"磁盘不是数组", `{"disks": {"name":"sda"}}`},
		{"超长", `{"collected_at":"` + strings.Repeat("x", maxReportBytes) + `"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, _, err := Parse([]byte(tc.body)); !errors.Is(err, ErrMalformed) {
				t.Fatalf("想要 ErrMalformed，得到 %v", err)
			}
		})
	}
}

// 结构对但值不对，走 Validate 的问题清单，不当成格式错。
func Test值不合法返回问题清单(t *testing.T) {
	body := strings.Replace(goodBody, `"cores": 48`, `"cores": 0`, 1)
	_, problems, err := Parse([]byte(body))
	if err != nil {
		t.Fatalf("不该是格式错：%v", err)
	}
	if len(problems) != 1 || problems[0].Code != "inventory_number_invalid" {
		t.Fatalf("得到 %v", problems)
	}
}

func Test整机型号拼接(t *testing.T) {
	for _, tc := range []struct{ manufacturer, product, want string }{
		{"Dell Inc.", "PowerEdge R750", "Dell Inc. PowerEdge R750"},
		{"", "PowerEdge R750", "PowerEdge R750"},
		{"Dell Inc.", "", "Dell Inc."},
		{"QEMU", "QEMU", "QEMU"},
		{"", "", ""},
	} {
		if got := joinModel(tc.manufacturer, tc.product); got != tc.want {
			t.Fatalf("joinModel(%q, %q) = %q，想要 %q", tc.manufacturer, tc.product, got, tc.want)
		}
	}
}
