package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/application/execution"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/snapshot"
	"github.com/lober/lober/internal/infrastructure/builtin"
	"github.com/lober/lober/internal/interfaces/console"
)

// These are real HTTP/PostgreSQL contract tests. Synthetic media records here
// are deliberately separate from the real guest installation qualification.
func TestLinuxMachineDeliversFrozenFilesAndCompletesProtocol(t *testing.T) {
	for _, tpl := range builtin.Templates() {
		if tpl.Family != "linux" {
			continue
		}
		t.Run(tpl.ID, func(t *testing.T) {
			cfg := assemblyConfig(t)
			cfg.Machine.Enabled = true
			cfg.Preview = previewConfig{Gateway: "http://gateway", Artifacts: "http://artifacts", SyslogAddr: "127.0.0.1:10514"}
			a, cleanup, err := newApp(t.Context(), cfg, console.Clock(func() int64 { return assemblyAt }), slog.New(slog.NewTextHandler(io.Discard, nil)))
			if err != nil {
				t.Fatal(err)
			}
			defer cleanup()
			if err = a.Initialise(); err != nil {
				t.Fatal(err)
			}
			host, err := a.Store.CreateHost(t.Context(), model.Host{SN: "MACHINE-CONTRACT", Arch: "x86_64", Firmware: "bios"}, assemblyAudit("host.create"))
			if err != nil {
				t.Fatal(err)
			}
			iso, err := a.Store.CreateISO(t.Context(), model.ISO{Name: "synthetic-contract-only", Family: "linux", Series: tpl.Series[0]}, assemblyAudit("iso.create"))
			if err != nil {
				t.Fatal(err)
			}
			iso.State = model.ISOReady
			iso.Version = "fixture"
			iso.Arch = "x86_64"
			iso.SHA256 = strings.Repeat("a", 64)
			iso.Artifacts = map[string]string{"kernel": "fixture/linux", "initrd": "fixture/initrd", "repo": "fixture/repo/", "iso": "fixture/iso", "stage2": "fixture/install.img", "live-root": "fixture/squashfs.img"}
			iso.Offers = map[string][]model.Option{"products": {{Value: "Leap", Label: "fixture-only"}}}
			iso, err = a.Store.UpdateISO(t.Context(), iso)
			if err != nil {
				t.Fatal(err)
			}
			hooks := map[model.HookKey]model.Hook{}
			for _, hook := range builtin.Hooks() {
				hooks[hook.Key()] = hook
			}
			in := params.Input{Template: tpl, ISO: iso, Host: host, Hooks: hooks, Entered: map[params.FieldKey]string{}}
			samples := map[string]string{"pwhash": "$6$fixture$notARealPasswordHash", "key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA fixture", "product": "Leap"}
			for _, decl := range tpl.Params {
				if value, ok := samples[decl.Name]; ok {
					in.Entered[params.FieldKey(model.TemplateFieldKey(decl.Name))] = value
				}
			}
			for slot, instances := range tpl.Assembly {
				for i, instance := range instances {
					for _, decl := range hooks[model.HookKey{ID: instance.HookID, Version: instance.HookVersion}].Params {
						if value, ok := samples[decl.Name]; ok {
							in.Entered[params.FieldKey(model.InstanceFieldKey(slot, i+1, decl.Name))] = value
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
			server := httptest.NewServer(a.Handler())
			defer server.Close()
			request := func(method, path string, body []byte) []byte {
				t.Helper()
				req, err := http.NewRequest(method, server.URL+path, bytes.NewReader(body))
				if err != nil {
					t.Fatal(err)
				}
				response, err := server.Client().Do(req)
				if err != nil {
					t.Fatal(err)
				}
				defer response.Body.Close()
				data, err := io.ReadAll(response.Body)
				if err != nil || response.StatusCode != 200 {
					t.Fatalf("machine request %s failed: %d", method, response.StatusCode)
				}
				return data
			}
			menu := request("GET", "/v1/boot?sn="+host.SN+"&arch=i386&firmware=pcbios", nil)
			task, err = a.Store.GetTask(t.Context(), task.ID)
			if err != nil {
				t.Fatal(err)
			}
			install, final := a.Machine.Service.Token(task, execution.Install), a.Machine.Service.Token(task, execution.Finalize)
			if !bytes.Contains(menu, []byte(install)) {
				t.Fatal("menu lacks committed credential")
			}
			format, _ := tpl.Format()
			answer, _ := catalog.DeliveryFileName(format, catalog.FileAnswer)
			request("GET", "/v1/answer/"+task.ID+"/"+install+"/"+answer, nil)
			if aux, ok := catalog.DeliveryFileName(format, catalog.FileAux); ok {
				request("GET", "/v1/answer/"+task.ID+"/"+install+"/"+aux, nil)
			}
			for _, slot := range snap.Slots {
				if !slot.Exec {
					continue
				}
				request("GET", fmt.Sprintf("/v1/answer/%s/%s/scripts/%d.sh", task.ID, install, slot.Seq), nil)
				for _, hook := range slot.Instances {
					got := request("GET", "/v1/answer/"+task.ID+"/"+install+"/hooks/"+hook.Digest, nil)
					if string(got) != hook.Body {
						t.Fatal("hook bytes changed")
					}
				}
			}
			// A real UDP socket verifies credential-based attribution and redaction.
			if format == "kickstart" {
				packet, err := net.ListenPacket("udp", "127.0.0.1:0")
				if err != nil {
					t.Fatal(err)
				}
				ctx, cancel := context.WithCancel(t.Context())
				done := make(chan struct{})
				go func() { defer close(done); a.runSyslog(ctx, packet) }()
				conn, err := net.Dial("udp", packet.LocalAddr().String())
				if err != nil {
					cancel()
					packet.Close()
					<-done
					t.Fatal(err)
				}
				conn.Write([]byte("GET /v1/answer/" + task.ID + "/" + install + "/ks.cfg"))
				conn.Close()
				found := false
				deadline := time.Now().Add(2 * time.Second)
				for !found && time.Now().Before(deadline) {
					events, err := a.Store.ListEvents(t.Context(), task.ID, 0, 100)
					if err != nil {
						t.Fatal(err)
					}
					for _, event := range events {
						if event.Kind == "installer.syslog" {
							data, err := base64.StdEncoding.DecodeString(event.Fields["log_base64"])
							if err != nil || bytes.Contains(data, []byte(install)) || !bytes.Contains(data, []byte("[REDACTED]")) {
								t.Fatal("syslog leaks credential")
							}
							found = true
						}
					}
					time.Sleep(10 * time.Millisecond)
				}
				cancel()
				packet.Close()
				<-done
				if !found {
					t.Fatal("UDP installer log not archived")
				}
			}
			signal := func(token, phase, query string) {
				var body []byte
				if phase == "hook-report" {
					body = []byte("wget /v1/answer/" + task.ID + "/" + install + "/hooks/digest\x00tail")
				}
				request("POST", "/v1/signal/"+task.ID+"/"+token+"/"+phase+query, body)
			}
			signal(install, "installed", "")
			signal(final, "booted", "")
			signal(final, "init-start", "")
			for _, slot := range snap.Slots {
				if !slot.Exec {
					continue
				}
				if slot.Timing != "firstboot" {
					t.Fatal("fixture now needs installer-slot execution")
				}
				q := fmt.Sprintf("?slot=%d&timing=%s&rc=0", slot.Seq, url.QueryEscape(string(slot.Timing)))
				signal(final, "slot-start", q)
				for i, hook := range slot.Instances {
					query := q + fmt.Sprintf("&instance=%d&digest=%s", i+1, hook.Digest)
					signal(final, "hook-start", query)
					signal(final, "hook-report", query)
				}
				signal(final, "slot-end", q)
			}
			signal(final, "init-done", "")
			signal(final, "ready", "")
			task, err = a.Store.GetTask(t.Context(), task.ID)
			if err != nil || task.State != model.TaskDone {
				t.Fatal("HTTP protocol failed to complete", err)
			}
			events, err := a.Store.ListEvents(t.Context(), task.ID, 0, 100)
			if err != nil {
				t.Fatal(err)
			}
			for _, event := range events {
				if event.Kind == "hook.report" {
					body, err := base64.StdEncoding.DecodeString(event.Fields["log_base64"])
					if err != nil || bytes.Contains(body, []byte(install)) || !bytes.Contains(body, []byte("[REDACTED]")) || !bytes.HasSuffix(body, []byte("\x00tail")) {
						t.Fatal("hook report leaked credentials or corrupted binary tail")
					}
				}
			}
			local := request("GET", "/v1/boot?sn="+host.SN+"&arch=x86_64&firmware=bios", nil)
			if bytes.Contains(local, []byte(install)) || !bytes.Contains(local, []byte("sanboot")) {
				t.Fatal("completed task starts another install")
			}
		})
	}
}
