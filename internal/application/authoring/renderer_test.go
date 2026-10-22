package authoring

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/snapshot"
)

type failedBootConfig struct{ err error }

func (r failedBootConfig) ReadESXiBootConfig(ctx context.Context, _ model.SnapshotISO) (string, error) {
	if ctx.Err() != nil {
		return "", ctx.Err()
	}
	return "", r.err
}
func frozenPreviewFixture(t *testing.T) model.Snapshot {
	t.Helper()
	p, k, _ := previewEditorFixture()
	f, ps := snapshot.Build(params.Input{Template: p.templates[k], Hooks: map[model.HookKey]model.Hook{}, ISO: sampleISO("rocky"), Host: sampleHost()})
	if len(ps) > 0 {
		t.Fatal(ps)
	}
	return f
}
func Test任务预览只使用冻结快照(t *testing.T) {
	f := frozenPreviewFixture(t)
	f.Template.Body = "# frozen {{sys:task}} {{sys:token}} {{sys:finalize-token}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"
	r := NewPreviewRenderer(nil, ports.PreviewEndpoints{})
	got, labels, ps, err := r.RenderFrozen(t.Context(), f, catalog.FirmwareUEFI, "old-task")
	if err != nil || len(ps) > 0 || len(got) != 2 || !strings.Contains(got[1].Body, "# frozen old-task sample-token sample-finalize-token") || !reflect.DeepEqual(labels, []string{"token", "finalize-token", "gateway", "artifacts", "smb", "syslog-addr"}) {
		t.Fatal(got, labels, ps, err)
	}
	again, _, _, err := r.RenderFrozen(t.Context(), f, catalog.FirmwareUEFI, "old-task")
	if err != nil || !reflect.DeepEqual(got, again) {
		t.Fatal(again, err)
	}
}
func Test文件预览渲染后段失败也不返回文件(t *testing.T) {
	f := frozenPreviewFixture(t)
	f.Template.Body = "{{param:missing}}"
	files, _, ps, err := NewPreviewRenderer(nil, ports.PreviewEndpoints{}).RenderFrozen(t.Context(), f, catalog.FirmwareUEFI, "sample-task")
	if files != nil || len(ps) == 0 || err != nil {
		t.Fatal(files, ps, err)
	}
}
func Test预览ESXi读取失败或取消不返回文件(t *testing.T) {
	f := frozenPreviewFixture(t)
	f.Format = catalog.FormatESXiKS
	fail := errors.New("read failed")
	for _, reader := range []failedBootConfig{{fail}, {context.Canceled}} {
		files, _, _, err := NewPreviewRenderer(reader, ports.PreviewEndpoints{}).RenderFrozen(t.Context(), f, catalog.FirmwareUEFI, "sample-task")
		if !errors.Is(err, reader.err) || files != nil {
			t.Fatal(files, err)
		}
	}
	files, _, _, err := NewPreviewRenderer(nil, ports.PreviewEndpoints{}).RenderFrozen(t.Context(), f, catalog.FirmwareUEFI, "sample-task")
	if err == nil || err.Error() != "ESXi original boot config reader missing" || files != nil {
		t.Fatal(files, err)
	}
}

func Test预览冻结脚本按摘要去重且保持文件次序(t *testing.T) {
	f := frozenPreviewFixture(t)
	digest := strings.Repeat("a", 64)
	instance := model.SnapshotInstance{HookID: "hook", HookVersion: 1, Kind: model.ImplScript, Interpreter: "sh", TimeoutSec: 30, Required: true, Digest: digest, Body: "#!/bin/sh\necho frozen\n"}
	f.Template.Body = "{{slot:one@post-target}}\n{{slot:two@post-target}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"
	f.Slots = []model.SnapshotSlot{{Seq: 1, Name: "one", Exec: true, Timing: "post-target", Instances: []model.SnapshotInstance{instance}}, {Seq: 2, Name: "two", Exec: true, Timing: "post-target", Instances: []model.SnapshotInstance{instance}}}
	files, _, ps, err := NewPreviewRenderer(nil, ports.PreviewEndpoints{Gateway: "https://gateway.test", Artifacts: "https://artifacts.test", SMB: `\\smb.test\lober`, SyslogAddr: "udp://syslog.test:514"}).RenderFrozen(t.Context(), f, catalog.FirmwareUEFI, "old-task")
	if err != nil || len(ps) > 0 || len(files) != 5 {
		t.Fatal(files, ps, err)
	}
	names := []string{}
	for _, file := range files {
		names = append(names, file.Name)
	}
	if !reflect.DeepEqual(names, []string{"menu.ipxe", "ks.cfg", "script-1.sh", "script-2.sh", "hook-" + digest + ".sh"}) || files[4].Digest != digest || files[4].Body != instance.Body || files[4].Slot != "one" {
		t.Fatal(files)
	}
}
