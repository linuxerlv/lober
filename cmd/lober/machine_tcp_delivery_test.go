package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/builtin"
)

// 原生安装器使用 TCP。只打开 UDP 会在真实服务入口连接失败。
func TestNativeTCPInstallerLogsThroughServeListener(t *testing.T) {
	cfg := assemblyConfig(t)
	cfg.Machine.Enabled = true
	// 先确认两种协议都能使用同一个非零端口，不能各取随机端口。
	reserved, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	syslogAddr := reserved.Addr().String()
	packet, err := net.ListenPacket("udp", syslogAddr)
	if err != nil {
		reserved.Close()
		t.Fatal(err)
	}
	packet.Close()
	reserved.Close()
	cfg.Machine.SyslogListen = syslogAddr
	cfg.Preview = previewConfig{Gateway: "http://gateway", Artifacts: "http://artifacts", SyslogAddr: syslogAddr}
	a, cleanup, err := newApp(t.Context(), cfg, func() int64 { return assemblyAt }, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	if err := a.Initialise(); err != nil {
		t.Fatal(err)
	}
	host, err := a.Store.CreateHost(t.Context(), model.Host{SN: "NATIVE-TCP", Arch: "x86_64", Firmware: "bios"}, assemblyAudit("host.create"))
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := a.Store.GetTemplate(t.Context(), model.TemplateKey{ID: "builtin-kickstart", Version: 3})
	if err != nil {
		t.Fatal(err)
	}
	iso, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "synthetic-tcp-contract-only", Family: "linux", Series: "almalinux"}, assemblyAudit("iso.create"))
	if err != nil {
		t.Fatal(err)
	}
	iso.State, iso.Version, iso.Arch = model.ISOReady, "fixture", "x86_64"
	iso.SHA256 = strings.Repeat("a", 64)
	iso.Artifacts = map[string]string{"kernel": "fixture/linux", "initrd": "fixture/initrd", "repo": "fixture/repo/", "stage2": "fixture/install.img"}
	iso, err = a.Store.UpdateISO(t.Context(), iso)
	if err != nil {
		t.Fatal(err)
	}
	hooks := map[model.HookKey]model.Hook{}
	for _, hook := range builtin.Hooks() {
		hooks[hook.Key()] = hook
	}
	in := params.Input{Template: tpl, ISO: iso, Host: host, Hooks: hooks, Entered: map[params.FieldKey]string{}}
	for slot, instances := range tpl.Assembly {
		for i, instance := range instances {
			for _, decl := range hooks[model.HookKey{ID: instance.HookID, Version: instance.HookVersion}].Params {
				if decl.Name == "pwhash" {
					in.Entered[params.FieldKey(model.InstanceFieldKey(slot, i+1, decl.Name))] = "$6$fixture$notARealPasswordHash"
				}
				if decl.Name == "key" {
					in.Entered[params.FieldKey(model.InstanceFieldKey(slot, i+1, decl.Name))] = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA fixture"
				}
			}
		}
	}
	snap, problems := snapshot.Build(in)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	tasks, err := a.Store.CreateTasks(t.Context(), []model.Task{{HostSN: host.SN, ISOID: iso.ID, TemplateID: tpl.ID, TemplateVersion: tpl.Version, Snapshot: snap, CreatedBy: "operator", CreatedAt: assemblyAt}}, assemblyAudit("task.create"))
	if err != nil {
		t.Fatal(err)
	}
	task := tasks[0]
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- serveListener(ctx, a, listener) }()
	defer func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error("服务取消后退出失败", err)
			}
		case <-time.After(3 * time.Second):
			t.Error("服务取消后未关闭日志连接并退出")
		}
	}()
	transport := http.DefaultTransport.(*http.Transport).Clone()
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	request := func(path string) []byte {
		t.Helper()
		response, err := client.Get("http://" + listener.Addr().String() + path)
		if err != nil {
			t.Fatal("真实机器请求失败", err)
		}
		defer response.Body.Close()
		body, err := io.ReadAll(response.Body)
		if err != nil || response.StatusCode != http.StatusOK {
			t.Fatal("真实机器请求未成功", response.StatusCode, err)
		}
		return body
	}
	menu := request("/v1/boot?sn=" + host.SN + "&arch=x86_64&firmware=bios")
	task, err = a.Store.GetTask(t.Context(), task.ID)
	if err != nil {
		t.Fatal(err)
	}
	install, _ := a.Machine.Service.Credentials(task)
	if install == "" || !bytes.Contains(menu, []byte(install)) {
		t.Fatal("引导菜单没有本轮凭据")
	}
	// 真实 HTTP 请求通过凭据检查后，登记来源地址；不直接登记租约。
	request("/v1/answer/" + task.ID + "/" + install + "/ks.cfg")
	conn, err := net.DialTimeout("tcp", syslogAddr, time.Second)
	if err != nil {
		t.Fatal("native TCP installer syslog connection failed", err)
	}
	defer conn.Close()
	conn.SetWriteDeadline(time.Now().Add(time.Second))
	for _, part := range []string{"<14>anaconda: GET /v1/answer/" + task.ID + "/" + install, "/ks.cfg\n<14>anaconda: packages ready\n"} {
		if _, err := io.WriteString(conn, part); err != nil {
			t.Fatal("原生 TCP 日志发送失败", err)
		}
	}
	udp, err := net.Dial("udp", syslogAddr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(udp, "<14>anaconda: UDP retained"); err != nil {
		udp.Close()
		t.Fatal(err)
	}
	udp.Close()
	want := map[string]bool{
		"<14>anaconda: GET /v1/answer/" + task.ID + "/[REDACTED]/ks.cfg": false,
		"<14>anaconda: packages ready":                                   false,
		"<14>anaconda: UDP retained":                                     false,
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		events, err := a.Store.ListEvents(t.Context(), task.ID, 0, 100)
		if err != nil {
			t.Fatal(err)
		}
		count := 0
		for _, event := range events {
			if event.Kind != "installer.syslog" {
				continue
			}
			count++
			body, err := base64.StdEncoding.DecodeString(event.Fields["log_base64"])
			if err != nil || bytes.Contains(body, []byte(install)) {
				t.Fatal("日志凭据未遮盖或保存内容损坏")
			}
			if event.Fields["source_ip"] != "127.0.0.1" {
				t.Fatal("日志没有使用真实连接地址")
			}
			if _, ok := want[string(body)]; !ok {
				t.Fatal("TCP 换行分帧或原 UDP 内容改变")
			}
			want[string(body)] = true
		}
		if count == len(want) && want["<14>anaconda: packages ready"] && want["<14>anaconda: UDP retained"] && want["<14>anaconda: GET /v1/answer/"+task.ID+"/[REDACTED]/ks.cfg"] {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("同一地址的原生 TCP 分帧日志或 UDP 日志未归档", count)
		}
		time.Sleep(10 * time.Millisecond)
	}
}
