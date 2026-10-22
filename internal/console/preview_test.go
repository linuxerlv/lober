package console

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/lober/lober/internal/builtin"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/render"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/domain/snapshot"
)

func previewFixture(t *testing.T, st repository.Store) (model.Template, model.Hook) {
	t.Helper()
	h, err := st.CreateHook(t.Context(), model.Hook{Name: "preview-script", Author: "admin", TimeoutSec: 30, Required: true, Params: []model.ParamDecl{{Name: "msg", Type: "word", Default: "hook-default"}}, Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplScript, Interpreter: "sh", Timings: []catalog.Timing{"post-target"}, Body: "#!/bin/sh\nprintf '%s\\n' \"$LOBER_PARAM_MSG\"\n"}}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "preview", Author: "admin", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "# {{param:message}}\n{{slot:execute@post-target}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n", Params: []model.ParamDecl{{Name: "message", Type: "word", Default: "template-default"}}, Assembly: map[string][]model.Instance{"execute": {{HookID: h.ID, HookVersion: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	return tpl, h
}
func previewPath(tpl model.Template) string {
	return fmt.Sprintf("/console/v1/templates/%s/versions/%d/preview", tpl.ID, tpl.Version)
}
func Test预览真实渲染菜单运行脚本与原始Hook(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, h := previewFixture(t, st)
	response := httpCall(s, "POST", previewPath(tpl), `{"firmware":"uefi","entered":{"tpl/message":"hello","execute/1/msg":"world"}}`, token)
	p := responseData[previewResponse](t, response)
	if len(p.Files) != 4 || len(p.Problems) != 0 || !slices.Contains(p.Sample, "ISO") || !slices.Contains(p.Sample, "主机") || !slices.Contains(p.Sample, "token") {
		t.Fatal(p)
	}
	files := map[string]previewFile{}
	for _, f := range p.Files {
		if f.Name == "" || !f.Sample {
			t.Fatal(f)
		}
		files[f.Kind] = f
	}
	if files["menu"].Name != "menu.ipxe" || !strings.Contains(files["menu"].Body, "sample-token") || files["answer"].Name != "ks.cfg" || !strings.Contains(files["answer"].Body, "# hello") || files["script"].Name != "script-1.sh" || files["hook"].Body != h.Impls[0].Body {
		t.Fatal(files)
	}
	in := params.Input{Template: tpl, Hooks: map[model.HookKey]model.Hook{h.Key(): h}, ISO: SampleISO("rocky"), Host: SampleHost(), Entered: map[params.FieldKey]string{"tpl/message": "hello", "execute/1/msg": "world"}}
	frozen, problems := snapshot.Build(in)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	values := map[catalog.SystemValue]string{"gateway": "https://gateway.example.invalid", "artifacts": "https://artifacts.example.invalid", "task": "sample-task", "token": "sample-token", "finalize-token": "sample-finalize-token", "sn": "SAMPLE-SN", "smb": `\\smb.example.invalid\lober`, "syslog-addr": "udp://syslog.example.invalid:514"}
	for _, item := range []struct {
		kind      catalog.FileKind
		key, slot string
	}{{catalog.FileMenu, "menu", ""}, {catalog.FileAnswer, "answer", ""}, {catalog.FileScript, "script", "execute"}} {
		bytes, problems, err := render.Render(render.Request{Snapshot: frozen, Firmware: "uefi", Values: values, Kind: item.kind, Slot: item.slot})
		if err != nil || len(problems) > 0 || string(bytes) != files[item.key].Body {
			t.Fatal(item, err, problems)
		}
	}
	again := httpCall(s, "POST", previewPath(tpl), `{"firmware":"uefi","entered":{"tpl/message":"hello","execute/1/msg":"world"}}`, token)
	if again.Code != 200 || again.Body.String() != response.Body.String() {
		t.Fatal("preview not deterministic")
	}
}
func Test预览实物身份无回退与不写任务审计(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, _ := previewFixture(t, st)
	item := readyISO(t, st, "Rocky", "rocky")
	item.Artifacts = map[string]string{"kernel": "actual/kernel", "initrd": "actual/initrd", "stage2": "actual/stage2", "repo": "actual/repo"}
	item, err := st.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateHost(t.Context(), model.Host{SN: "SN-1", Arch: "x86_64", Firmware: "bios"}, repository.Audit{Actor: "seed", Action: "host.create", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	p := responseData[previewResponse](t, httpCall(s, "POST", previewPath(tpl), fmt.Sprintf(`{"isoID":%q,"hostSN":"sn-1","firmware":"uefi"}`, item.ID), token))
	if slices.Contains(p.Sample, "ISO") || slices.Contains(p.Sample, "主机") || !slices.Contains(p.Sample, "token") {
		t.Fatal(p.Sample)
	}
	if !strings.Contains(p.Files[0].Body, "actual/kernel") || !p.Files[0].Sample {
		t.Fatal(p.Files[0])
	}
	for _, body := range []string{`{"isoID":"missing"}`, `{"hostSN":"MISSING"}`, fmt.Sprintf(`{"isoID":%q,"hostSN":"MISSING"}`, item.ID)} {
		w := httpCall(s, "POST", previewPath(tpl), body, token)
		if w.Code != 404 || strings.Contains(w.Body.String(), `"files"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	tasks, err := st.ListTasks(t.Context(), repository.TaskQuery{})
	if err != nil || len(tasks.Items) != 0 || len(before.Items) != len(after.Items) {
		t.Fatal("preview wrote data", err)
	}
}
func Test预览输入问题不返回半截文件(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, _ := previewFixture(t, st)
	for _, item := range []struct {
		body, code string
		status     int
	}{{`{"series":"debian"}`, "series_not_ticked", 409}, {`{"entered":{"unknown":"x"}}`, "param_unknown_field", 422}, {`{"entered":{"tpl/message":"two words"}}`, "param_value_invalid", 422}} {
		w := httpCall(s, "POST", previewPath(tpl), item.body, token)
		if w.Code != item.status || !strings.Contains(w.Body.String(), item.code) || strings.Contains(w.Body.String(), `"files"`) {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{"firmware":"UEFI"}`, `{"series":"unknown"}`, `{"entered":"wrong shape"}`} {
		if w := httpCall(s, "POST", previewPath(tpl), body, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	h, err := st.GetHook(t.Context(), repository.HookRefs(tpl)[0])
	if err != nil {
		t.Fatal(err)
	}
	h.Impls[0].Series = []catalog.Series{"debian"}
	if _, err := st.SaveHookDraft(t.Context(), h); err != nil {
		t.Fatal(err)
	}
	w := httpCall(s, "POST", previewPath(tpl), `{}`, token)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "impl_missing_for_series") || strings.Contains(w.Body.String(), `"files"`) {
		t.Fatal(w.Code, w.Body.String())
	}
}

type diskBootConfig struct{ root string }

func (d diskBootConfig) ReadESXiBootConfig(_ context.Context, item model.SnapshotISO) (string, error) {
	root, err := os.OpenRoot(d.root)
	if err != nil {
		return "", err
	}
	defer root.Close()
	bytes, err := root.ReadFile(filepath.Join(item.Artifacts["esxi-tree"], "boot.cfg"))
	return string(bytes), err
}
func Test预览ESXi使用真实原始配置且未知示例不可用(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "esxi-preview", Author: "admin", Family: "esxi", Series: []catalog.Series{"esxi"}, Body: "{{lober:firstboot}}\n{{lober:cleanup}}\n"})
	if err != nil {
		t.Fatal(err)
	}
	w := httpCall(s, "POST", previewPath(tpl), `{}`, token)
	if w.Code != 409 || !strings.Contains(w.Body.String(), "iso_version_missing") {
		t.Fatal(w.Code, w.Body.String())
	}
	item := readyISO(t, st, "ESXi", "esxi")
	item.Version = "8.0.3"
	item.Artifacts = map[string]string{"mboot": "esxi/efi/boot/bootx64.efi", "esxi-tree": "esxi"}
	item, err = st.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "esxi"), 0700); err != nil {
		t.Fatal(err)
	}
	raw := "bootstate=0\ntitle=Original media\nkernel=b.b00\nkernelopt=runweasel\nmodules=jumpstrt.gz --- nested/module.v00\nprefix=/\ncustom=retained\n"
	if err := os.WriteFile(filepath.Join(root, "esxi", "boot.cfg"), []byte(raw), 0600); err != nil {
		t.Fatal(err)
	}
	s.server.deps.BootConfig = diskBootConfig{root}
	p := responseData[previewResponse](t, httpCall(s, "POST", previewPath(tpl), fmt.Sprintf(`{"isoID":%q,"firmware":"uefi"}`, item.ID), token))
	found := false
	for _, f := range p.Files {
		if f.Kind == "aux" {
			found = true
			if !strings.Contains(f.Body, "custom=retained") || !strings.Contains(f.Body, "nested/module.v00") || !strings.Contains(f.Body, "Original media") {
				t.Fatal(f.Body)
			}
		}
	}
	if !found {
		t.Fatal(p)
	}
	if err := os.Remove(filepath.Join(root, "esxi", "boot.cfg")); err != nil {
		t.Fatal(err)
	}
	w = httpCall(s, "POST", previewPath(tpl), fmt.Sprintf(`{"isoID":%q}`, item.ID), token)
	if w.Code != 500 || strings.Contains(w.Body.String(), "modules=") {
		t.Fatal(w.Code, w.Body.String())
	}
}
func Test预览UbuntuWindows附属文件与多执行插槽(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	for _, item := range []struct {
		series   catalog.Series
		expected string
	}{{"ubuntu", "meta-data"}, {"windows-server", "startnet.cmd"}, {"windows-desktop", "startnet.cmd"}} {
		tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: string(item.series), Author: "admin", Family: item.series.Family(), Series: []catalog.Series{item.series}, Body: "{{slot:one@firstboot}}\n{{slot:two@firstboot}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"})
		if err != nil {
			t.Fatal(err)
		}
		p := responseData[previewResponse](t, httpCall(s, "POST", previewPath(tpl), `{}`, token))
		script, aux := 0, false
		for _, f := range p.Files {
			if f.Kind == "script" {
				script++
			}
			if f.Name == item.expected {
				aux = true
			}
		}
		if script != 2 || !aux {
			t.Fatal(item, p)
		}
	}
}

func Test预览实际内置模板的全部已声明系(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	seedBuiltinContent(t, st)
	count := 0
	for _, tpl := range builtin.Templates() {
		for _, series := range tpl.Series {
			count++
			if series == catalog.SeriesESXi {
				w := httpCall(s, "POST", previewPath(tpl), fmt.Sprintf(`{"series":%q}`, series), token)
				if w.Code != 409 || !strings.Contains(w.Body.String(), "iso_version_missing") {
					t.Fatal(w.Code, w.Body.String())
				}
				continue
			}
			hooks, err := st.GetHooks(t.Context(), repository.HookRefs(tpl))
			if err != nil {
				t.Fatal(err)
			}
			form := params.Evaluate(params.Input{Template: tpl, Hooks: hooks, ISO: SampleISO(series), Host: SampleHost()})
			entered := map[string]string{}
			for _, field := range form.Fields {
				if !field.Required || field.Prefill != "" || field.Locked {
					continue
				}
				value := "sample"
				switch field.Type {
				case "pwhash":
					value = "$6$preview$abcdefghijkl"
				case "word":
					value = "SamplePassword"
				case "sshkey":
					value = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIDNaJi8uw2oGwAug7HWX7cvFNvum8Lk5qwBmH3KVfWx3 ops@lober-test"
				case "enum":
					if len(field.Options) == 0 {
						t.Fatal(series, field)
					}
					value = field.Options[0].Value
				default:
					t.Fatal("fixture needs a valid literal", series, field)
				}
				entered[string(field.Key)] = value
			}
			body, err := json.Marshal(previewRequest{Series: series, Firmware: catalog.FirmwareUEFI, Entered: entered})
			if err != nil {
				t.Fatal(err)
			}
			p := responseData[previewResponse](t, httpCall(s, "POST", previewPath(tpl), string(body), token))
			kinds := map[string]bool{}
			for _, file := range p.Files {
				kinds[file.Kind] = true
				if !file.Sample || file.Body == "" {
					t.Fatal(series, file)
				}
			}
			if !kinds["menu"] || !kinds["answer"] {
				t.Fatal(series, kinds)
			}
		}
	}
	if count != 13 {
		t.Fatal("builtin coverage changed", count)
	}
}
func Test预览与插槽对五角色都是只读权限(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	tpl, _ := previewFixture(t, st)
	for i, role := range model.Roles() {
		name := fmt.Sprintf("preview%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		responseData[previewResponse](t, httpCall(s, "POST", previewPath(tpl), `{}`, token))
		responseData[slotList](t, httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/slots", tpl.ID), "", token))
	}
}
