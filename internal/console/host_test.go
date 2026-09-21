// 本文件测试主机清单两个端点。对应实现计划任务 3「测试要锁住的行为」。
package console

import (
	"net/http"
	"testing"

	"github.com/lober/lober/internal/apierr"
)

// TestHost_CreateThenList 锁住「录入后能查出，字段完整」。
func TestHost_CreateThenList(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
		"sn": "SN-0001", "mac": "AA:BB:CC:DD:EE:FF", "uuid": "uuid-1",
		"arch": "x86_64", "firmware": "uefi", "rack": "R1-01",
	})
	if rr.Code != http.StatusOK {
		t.Fatalf("录入应返回 HTTP 200，实际 %d，body=%s", rr.Code, rr.Body.String())
	}

	rr = ts.doJSON("GET", "/console/v1/hosts", tok, nil)
	var env struct {
		Data []hostListItem `json:"data"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if len(env.Data) != 1 {
		t.Fatalf("查询应返回 1 条，实际 %d 条", len(env.Data))
	}
	got := env.Data[0]
	if got.SN != "SN-0001" || got.UUID != "uuid-1" || got.Arch != "x86_64" ||
		got.Firmware != "uefi" || got.Rack != "R1-01" {
		t.Errorf("字段不完整或不匹配: %+v", got)
	}
	if got.MAC != "aa:bb:cc:dd:ee:ff" {
		t.Errorf("MAC 应归一化为小写冒号分隔形式，实际 %q", got.MAC)
	}
}

// TestHost_MACNormalization 锁住「三种 MAC 格式入库后归一化为同一形式」。
func TestHost_MACNormalization(t *testing.T) {
	cases := []struct {
		name string
		mac  string
	}{
		{"大写冒号", "AA:BB:CC:DD:EE:FF"},
		{"小写连字符", "aa-bb-cc-dd-ee-ff"},
		{"无分隔", "AABBCCDDEEFF"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newTestServer(t)
			tok := ts.login("eve-admin")
			rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
				"sn": "SN-MAC-" + c.name, "mac": c.mac, "arch": "x86_64", "firmware": "uefi",
			})
			if rr.Code != http.StatusOK {
				t.Fatalf("录入失败: %d, body=%s", rr.Code, rr.Body.String())
			}
			var env struct {
				Data hostListItem `json:"data"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Data.MAC != "aa:bb:cc:dd:ee:ff" {
				t.Errorf("MAC %q 归一化结果 = %q, want aa:bb:cc:dd:ee:ff", c.mac, env.Data.MAC)
			}
		})
	}
}

// TestHost_InvalidMACRejected 锁住「非法 MAC 回校验错误」。
func TestHost_InvalidMACRejected(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
		"sn": "SN-BAD-MAC", "mac": "not-a-mac", "arch": "x86_64", "firmware": "uefi",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("非法 MAC 应回校验错误，实际 code=0（成功）")
	}
}

// TestHost_MissingArchOrFirmwareRejected 锁住「缺架构或缺固件回校验
// 错误」。
func TestHost_MissingArchOrFirmwareRejected(t *testing.T) {
	cases := []struct {
		name string
		body map[string]string
	}{
		{"缺 arch", map[string]string{"sn": "SN-1", "mac": "aa:bb:cc:dd:ee:ff", "firmware": "uefi"}},
		{"缺 firmware", map[string]string{"sn": "SN-2", "mac": "aa:bb:cc:dd:ee:ff", "arch": "x86_64"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ts := newTestServer(t)
			tok := ts.login("eve-admin")
			rr := ts.doJSON("POST", "/console/v1/hosts", tok, c.body)
			var env struct {
				Code int `json:"code"`
			}
			decodeJSON(t, rr.Body.Bytes(), &env)
			if env.Code == int(apierr.CodeOK) {
				t.Errorf("%s 应回校验错误，实际成功", c.name)
			}
		})
	}
}

// TestHost_UnknownArchRejected 锁住「枚举外的架构值被拒」。
func TestHost_UnknownArchRejected(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
		"sn": "SN-BAD-ARCH", "mac": "aa:bb:cc:dd:ee:ff", "arch": "mips", "firmware": "uefi",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code == int(apierr.CodeOK) {
		t.Errorf("枚举外的 arch 值应被拒，实际成功")
	}
}

// TestHost_SearchMatchesThreeFieldsCaseInsensitive 锁住「搜索对三个字段
// 都生效且大小写不敏感」。
func TestHost_SearchMatchesThreeFieldsCaseInsensitive(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("eve-admin")

	seed := []map[string]string{
		{"sn": "SN-Alpha", "mac": "aa:aa:aa:aa:aa:aa", "arch": "x86_64", "firmware": "uefi", "rack": "R1"},
		{"sn": "SN-Beta", "mac": "bb:bb:bb:bb:bb:bb", "arch": "x86_64", "firmware": "uefi", "rack": "R2-special"},
		{"sn": "SN-Gamma", "mac": "cc:cc:cc:cc:cc:cc", "arch": "x86_64", "firmware": "uefi", "rack": "R3"},
	}
	for _, h := range seed {
		rr := ts.doJSON("POST", "/console/v1/hosts", tok, h)
		if rr.Code != http.StatusOK {
			t.Fatalf("种子数据录入失败: %s", rr.Body.String())
		}
	}

	cases := []struct {
		q       string
		wantLen int
		desc    string
	}{
		{"alpha", 1, "小写匹配 SN 子串"},
		{"BB:BB", 1, "大写匹配 MAC 子串"},
		{"special", 1, "匹配机架位子串"},
		{"sn-", 3, "匹配全部 SN 前缀"},
	}
	for _, c := range cases {
		rr := ts.doJSON("GET", "/console/v1/hosts?q="+c.q, tok, nil)
		var env struct {
			Data []hostListItem `json:"data"`
		}
		decodeJSON(t, rr.Body.Bytes(), &env)
		if len(env.Data) != c.wantLen {
			t.Errorf("%s: q=%q 结果数 = %d, want %d", c.desc, c.q, len(env.Data), c.wantLen)
		}
	}
}

// TestHost_CreateRequiresImportPermission 锁住「录入需要导入权限
// （operator 调用被拒）」。
func TestHost_CreateRequiresImportPermission(t *testing.T) {
	ts := newTestServer(t)
	tok := ts.login("bob-operator")

	rr := ts.doJSON("POST", "/console/v1/hosts", tok, map[string]string{
		"sn": "SN-X", "mac": "aa:bb:cc:dd:ee:ff", "arch": "x86_64", "firmware": "uefi",
	})
	var env struct {
		Code int `json:"code"`
	}
	decodeJSON(t, rr.Body.Bytes(), &env)
	if env.Code != int(apierr.CodeForbidden) {
		t.Errorf("operator 录入主机应被拒 403，实际 code=%d", env.Code)
	}
}
