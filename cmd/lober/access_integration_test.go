package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http/httptest"
	"testing"

	"github.com/lober/lober/internal/console"
)

func Test装配角色菜单及改密重启保留(t *testing.T) {
	cfg := assemblyConfig(t)
	open := func() (*app, func()) {
		t.Helper()
		a, closeApp, err := newApp(t.Context(), cfg, console.Clock(func() int64 { return assemblyAt }), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal("assembly failed")
		}
		if err = a.Initialise(); err != nil {
			closeApp()
			t.Fatal(err)
		}
		return a, closeApp
	}
	a, closeApp := open()
	request := func(method, path, body, token string) *httptest.ResponseRecorder {
		t.Helper()
		r := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		r.Header.Set("Content-Type", "application/json")
		if token != "" {
			r.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		a.Console.Engine.ServeHTTP(w, r)
		return w
	}
	login := func(password string) string {
		t.Helper()
		body, _ := json.Marshal(map[string]string{"username": "admin", "password": password})
		w := request("POST", "/console/v1/auth/login", string(body), "")
		var result struct {
			Data struct {
				Token string `json:"token"`
			} `json:"data"`
		}
		if w.Code != 200 || json.Unmarshal(w.Body.Bytes(), &result) != nil || result.Data.Token == "" {
			t.Fatal("login failed", w.Code)
		}
		return result.Data.Token
	}
	token := login(cfg.SeedAdmin)
	if w := request("POST", "/console/v1/auth/password", `{"currentPassword":"initial password","newPassword":"changed password"}`, token); w.Code != 200 {
		closeApp()
		t.Fatal(w.Code, w.Body.String())
	}
	if w := request("GET", "/console/v1/user/info", "", token); w.Code != 401 {
		closeApp()
		t.Fatal("old session survived")
	}
	token = login("changed password")
	if w := request("PUT", "/console/v1/roles/viewer/menus", `{"menuIds":["hosts"],"rev":0}`, token); w.Code != 200 {
		closeApp()
		t.Fatal(w.Code, w.Body.String())
	}
	closeApp()
	a, closeApp = open()
	defer closeApp()
	token = login("changed password")
	if w := request("GET", "/console/v1/roles/menus", "", token); w.Code != 200 {
		t.Fatal(w.Code, w.Body.String())
	}
	setting, found, err := a.Store.GetRoleMenus(t.Context(), "viewer")
	if err != nil || !found || setting.Rev != 1 || len(setting.MenuIDs) != 1 || setting.MenuIDs[0] != "hosts" {
		t.Fatal("menu configuration lost", setting, err)
	}
	user, err := a.Store.GetUser(t.Context(), "admin")
	if err != nil || user.MustChangePassword {
		t.Fatal("bootstrap password reset on restart")
	}
}
