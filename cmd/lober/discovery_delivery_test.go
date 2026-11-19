package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"regexp"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/discovery"
	"github.com/lober/lober/internal/domain/catalog"
	data "github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/credentials"
)

func TestDiscoveryDelivery(t *testing.T) {
	cfg := assemblyConfig(t)
	cfg.Machine.Enabled = true
	cfg.Machine.Discovery = discoveryConfig{Enabled: true, AllowedNetworks: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}, Images: map[catalog.Arch]discovery.Image{"x86_64": {Kernel: "discovery/kernel", Initrd: "discovery/initramfs"}}}
	cfg.Preview.Gateway = "http://gateway.invalid"
	cfg.Preview.Artifacts = "http://artifacts.invalid"
	now := int64(assemblyAt)
	a, closeApp, err := newApp(t.Context(), cfg, func() int64 { return now }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(closeApp)
	hash, err := credentials.NewPasswords().Hash("fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	for name, role := range map[string]model.Role{"inventoryadmin": model.RoleAdmin, "operator": model.RoleOperator, "viewer": model.RoleViewer} {
		if _, err := a.Store.CreateUser(t.Context(), model.User{Username: name, DisplayName: name, Role: role, Password: hash, CreatedAt: assemblyAt}, assemblyAudit("user.create")); err != nil {
			t.Fatal(err)
		}
	}
	h := a.Handler()
	call := func(method, path, payload, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(payload))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w
	}
	login := func(name string) string {
		w := call("POST", "/console/v1/auth/login", `{"username":"`+name+`","password":"fixture-password"}`, "")
		var reply struct{ Data struct{ Token string } }
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Data.Token == "" {
			t.Fatalf("登录 %s 失败: %d %s", name, w.Code, w.Body)
		}
		return reply.Data.Token
	}
	const mac = "52:54:00:12:34:56"
	const boot = "/v1/boot?sn=&arch=x86_64&firmware=uefi&mac=" + mac
	const list = "/console/v1/discoveries"
	const detail = list + "/" + mac
	w := call("GET", boot, "", "")
	matches := regexp.MustCompile(`lober\.discovery_token=([0-9a-f]{96})`).FindStringSubmatch(w.Body.String())
	if w.Code != http.StatusOK || len(matches) != 2 || !strings.Contains(w.Body.String(), "http://artifacts.invalid/discovery/initramfs") {
		t.Fatalf("报到未下发 Alpine: %d", w.Code)
	}
	if w := call("GET", list, "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("未登录可读清单: %d", w.Code)
	}
	viewer, operator, admin := login("viewer"), login("operator"), login("inventoryadmin")
	if w := call("GET", detail, "", viewer); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"status":"waiting"`) {
		t.Fatalf("报到未显示: %d %s", w.Code, w.Body)
	}
	report := `{"sections":[{"key":"system","title":"整机","status":"read","items":[{"name":"整机","fields":{"厂商":"Dell","型号":"R750"}}]},{"key":"storage","title":"磁盘","status":"unread","reason":"缺驱动","items":[]}]}`
	if w := call("POST", "/v1/discovery/"+mac, report, matches[1]); w.Code != http.StatusNoContent {
		t.Fatalf("上报失败: %d %s", w.Code, w.Body)
	}
	w = call("GET", list, "", viewer)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"system_model":"Dell R750"`) || strings.Contains(w.Body.String(), `"report":`) {
		t.Fatalf("列表应只有摘要: %d %s", w.Code, w.Body)
	}
	w = call("GET", detail, "", viewer)
	var got struct{ Data data.Record }
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Data.Report == nil || got.Data.Report.Sections[1].Reason != "缺驱动" {
		t.Fatalf("详情缺项未显示: %d %s", w.Code, w.Body)
	}
	if w := call("GET", "/console/v1/hosts/PXE-NEW", "", viewer); w.Code != http.StatusNotFound {
		t.Fatalf("发现不得自动登记: %d", w.Code)
	}
	for _, token := range []string{viewer, operator} {
		if w := call("POST", detail+"/register", `{"sn":"PXE-NEW"}`, token); w.Code != http.StatusForbidden {
			t.Fatalf("非管理员能登记: %d", w.Code)
		}
	}
	if w := call("POST", detail+"/register", `{"sn":"PXE-NEW"}`, admin); w.Code != http.StatusOK {
		t.Fatalf("管理员登记失败: %d %s", w.Code, w.Body)
	}
	if w := call("GET", "/console/v1/hosts/PXE-NEW", "", viewer); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"arch":"x86_64"`) {
		t.Fatalf("登记后主机不可读: %d %s", w.Code, w.Body)
	}
	for _, tc := range []struct {
		sn    string
		count int
	}{{"PXE-NEW", 1}, {"OTHER-HOST", 0}} {
		w := call("GET", list+"?registered_sn="+tc.sn, "", viewer)
		var records struct{ Data discovery.Page }
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &records) != nil || len(records.Data.Items) != tc.count {
			t.Errorf("按登记序列号查找 %s 返回 %s", tc.sn, w.Body)
		}
	}
	w = call("GET", "/console/v1/hosts/PXE-NEW/disks", "", viewer)
	var diskReply struct{ Data data.HostDiskReport }
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &diskReply) != nil || diskReply.Data.Status != "none" || diskReply.Data.Disks == nil || diskReply.Data.Notice != "缺驱动" {
		t.Fatalf("确认后的磁盘报告缺项信息应原样保留 %d %s", w.Code, w.Body)
	}
	for _, agent := range []string{"iPXE", "debian-installer"} {
		r := httptest.NewRequest("GET", strings.Replace(boot, "sn=&", "sn=OTHER-HOST&", 1), nil)
		r.Header.Set("User-Agent", agent)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusConflict {
			t.Errorf("%s 遇到网卡和主机身份冲突应停止引导，得到 %d", agent, w.Code)
		}
	}

	if w := call("GET", boot, "", ""); w.Code != http.StatusOK || strings.Contains(w.Body.String(), "lober.discovery_token") {
		t.Fatalf("已确认网口被再次发现: %d", w.Code)
	}
}
