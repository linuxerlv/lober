package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/credentials"
)

func TestConsoleInventoryRequestReachesMachineReport(t *testing.T) {
	cfg := assemblyConfig(t)
	cfg.Machine.Enabled = true
	cfg.Preview.Gateway = "http://gateway.invalid"
	cfg.Preview.Artifacts = "http://artifacts.invalid"
	cfg.Preview.InventoryKernel = "inventory/kernel"
	cfg.Preview.InventoryInitrd = "inventory/initrd"
	a, closeApp, err := newApp(t.Context(), cfg, func() int64 { return assemblyAt }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal("装配失败", err)
	}
	t.Cleanup(closeApp)
	hash, err := credentials.NewPasswords().Hash("fixture-password")
	if err != nil {
		t.Fatal(err)
	}
	for user, role := range map[string]model.Role{"operator": model.RoleOperator, "viewer": model.RoleViewer} {
		if _, err := a.Store.CreateUser(t.Context(), model.User{Username: user, DisplayName: user, Role: role, Password: hash, CreatedAt: assemblyAt}, assemblyAudit("user.create")); err != nil {
			t.Fatal(err)
		}
	}
	const sn = "INVENTORY E2E/A+%"
	if _, err := a.Store.CreateHost(t.Context(), model.Host{SN: sn, Attrs: map[string]string{"rack": "r1"}}, assemblyAudit("host.create")); err != nil {
		t.Fatal(err)
	}
	handler := a.Handler()
	call := func(method, path, payload, token string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, path, strings.NewReader(payload))
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, r)
		return w
	}
	login := func(user string) string {
		w := call(http.MethodPost, "/console/v1/auth/login", `{"username":"`+user+`","password":"fixture-password"}`, "")
		var reply struct{ Data struct{ Token string } }
		if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Data.Token == "" {
			t.Fatal("测试登录失败", w.Code)
		}
		return reply.Data.Token
	}
	hostPath := "/console/v1/hosts/" + url.PathEscape(sn)
	requestPath := hostPath + "/inventory/request"
	viewer, operator := login("viewer"), login("operator")
	if w := call(http.MethodPost, requestPath, "", viewer); w.Code != http.StatusForbidden {
		t.Fatal("只读用户请求采集", w.Code)
	}
	if w := call(http.MethodPost, requestPath, "", operator); w.Code != http.StatusOK {
		t.Fatal("发起采集失败", w.Code, w.Body.String())
	}
	// 普通编辑和导入不能清掉这次请求，导入也不能伪造控制列。
	if w := call(http.MethodPut, hostPath, `{"rev":2,"attrs":{"rack":"r2"}}`, operator); w.Code != http.StatusOK {
		t.Fatal("主机编辑失败", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, "/console/v1/hosts/import", "sn,rack\n"+sn+",r3\n", operator); w.Code != http.StatusOK {
		t.Fatal("主机导入失败", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, "/console/v1/hosts/import", "sn,inv_request_id\n"+sn+",forged\n", operator); w.Code != http.StatusBadRequest {
		t.Fatal("采集控制列未拒绝", w.Code)
	}
	boot := call(http.MethodGet, "/v1/boot?sn="+url.QueryEscape("\n"+strings.ToLower(sn)+"\n")+"&arch=x86_64&firmware=bios", "", "")
	match := regexp.MustCompile(`lober\.inventory_token=([0-9a-f]{64})`).FindStringSubmatch(boot.Body.String())
	serial := regexp.MustCompile(`lober\.sn=(~[0-9A-Za-z_-]+) `).FindStringSubmatch(boot.Body.String())
	if boot.Code != http.StatusOK || len(match) != 2 || len(serial) != 2 {
		t.Fatal("机器没有领到采集令牌", boot.Code)
	}
	const report = `{"firmware":"bios","cpu":{"model":"fixture","cores":2},"memory_mib":1024,"disks":[{"name":"sda","bytes":8589934592}]}`
	path := "/v1/inventory/" + serial[1] + "/" + match[1]
	if w := call(http.MethodPost, path, report, ""); w.Code != http.StatusNoContent {
		t.Fatal("采集上报失败", w.Code, w.Body.String())
	}
	if w := call(http.MethodPost, path, report, ""); w.Code != http.StatusForbidden {
		t.Fatal("同一令牌重复提交应被拒绝", w.Code)
	}
	w := call(http.MethodGet, hostPath, "", viewer)
	var reply struct {
		Data struct{ Attrs map[string]string }
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &reply) != nil || reply.Data.Attrs["inv_system_disk"] != "sda" || reply.Data.Attrs["rack"] != "r3" {
		t.Fatal("控制台未读到采集数据和人工属性", w.Code)
	}
	if w := call(http.MethodPost, requestPath, "", operator); w.Code != http.StatusOK {
		t.Fatal("再次请求采集失败", w.Code)
	}
	boot = call(http.MethodGet, "/v1/boot?sn="+url.QueryEscape(sn)+"&arch=x86_64&firmware=bios", "", "")
	match = regexp.MustCompile(`lober\.inventory_token=([0-9a-f]{64})`).FindStringSubmatch(boot.Body.String())
	if len(match) != 2 {
		t.Fatal("缺少本轮采集令牌")
	}
	hardware := "/v1/hardware/" + serial[1]
	const rich = `{"sections":[{"key":"storage","title":"磁盘","status":"partial","reason":"缺少部分字段","items":[{"name":"nvme0n1","fields":{"类型":"磁盘","容量（字节）":"1000","已用寿命（%）":"0","未读取·盘位":"机箱未提供槽位映射"}}]}]}`
	if w := call(http.MethodPost, hardware, rich, ""); w.Code != http.StatusUnauthorized {
		t.Fatal("必须提供凭据", w.Code)
	}
	if w := call(http.MethodPost, hardware, "{}", match[1]); w.Code != http.StatusUnprocessableEntity {
		t.Fatal("缺少部件必须拒收", w.Code)
	}
	if w := call(http.MethodPost, hardware, rich, match[1]); w.Code != http.StatusNoContent {
		t.Fatal("详细报告未保存", w.Code)
	}
	if w := call(http.MethodPost, hardware, rich, match[1]); w.Code != http.StatusForbidden {
		t.Fatal("凭据应只用一次", w.Code)
	}
	w = call(http.MethodGet, hostPath+"/hardware", "", viewer)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "机箱未提供槽位映射") || !strings.Contains(w.Body.String(), "nvme0n1") {
		t.Fatal("主机详情应可读取本机的部分报告", w.Code)
	}
}
