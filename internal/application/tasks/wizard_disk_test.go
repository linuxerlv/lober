package tasks

import (
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
	"github.com/lober/lober/internal/infrastructure/builtin"
)

func TestWizardTargetDiskMatchesSubmittedAnswer(t *testing.T) {
	for _, tc := range []struct{ id, param, mode, entered, value, answer string }{
		{"builtin-unattend", "disk_index", "windows-index", "", "0", "<DiskID>0</DiskID>"},
		{"builtin-kickstart", "disk", "device-or-path", "/dev/disk/by-id/wwn-123", "/dev/disk/by-id/wwn-123", "--ondisk=/dev/disk/by-id/wwn-123"},
		{"builtin-esxi", "disk", "first-disk-rule", "", "local", "--firstdisk='local'"},
		{"builtin-preseed", "disk", "device", "vdb", "vdb", "partman-auto/disk string /dev/vdb"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			f, deps, _, _ := applicationFixture()
			for _, tpl := range builtin.Templates() {
				if tpl.ID == tc.id {
					f.template, f.versions = tpl, []model.Template{tpl}
					deps.Manifest.Templates = []model.TemplateKey{{ID: tpl.ID, Version: tpl.Version}}
				}
			}
			for _, h := range builtin.Hooks() {
				for i := range h.Params {
					h.Params[i].Label = "已重命名"
				}
				f.hooks[h.Key()] = h
				deps.Manifest.Hooks = append(deps.Manifest.Hooks, h.Key())
			}
			f.iso.Series = f.template.Series[0]
			f.iso.Family = f.template.Family
			f.iso.Offers = map[string][]model.Option{"editions": {{Value: "4", Label: "4"}}}
			f.iso.Artifacts = map[string]string{}
			for _, role := range catalog.RequiredRoles(f.iso.Series) {
				f.iso.Artifacts[role] = "media/" + role
			}
			host := f.hosts["SN-1"]
			host.Attrs = map[string]string{"inv_disks": "sda:1000000000:first\nsdb:2000000000:second"}
			f.hosts[host.SN] = host
			req := ParamsRequest{Selection: Selection{ISOID: f.iso.ID, TemplateID: f.template.ID, TemplateVersion: f.template.Version}, HostSNs: []string{host.SN}}
			w := NewWizard(deps)
			initial, err := w.Params(t.Context(), operator, req)
			if err != nil || len(initial.Hosts) != 1 {
				t.Fatalf("%+v %v", initial, err)
			}
			key := model.InstanceFieldKey("partition", 1, tc.param)
			values := map[string]string{}
			samples := map[string]string{"edition": "4", "password": "Fixture_Pass9", "key": "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA fixture", "pwhash": "$6$fixture$test"}
			if f.iso.Family == "esxi" {
				samples["packages"] = "https://depot.example.test/test.vib"
			}
			for _, field := range initial.Hosts[0].Fields {
				if !field.Locked {
					parts := strings.Split(string(field.Key), "/")
					if v := samples[parts[len(parts)-1]]; v != "" {
						values[string(field.Key)] = v
					}
				}
			}
			if tc.entered != "" {
				values[key] = tc.entered
			}
			req.Entered = map[string]map[string]string{host.SN: values}
			preview, err := w.Params(t.Context(), operator, req)
			if err != nil {
				t.Fatal(err)
			}
			row := preview.Hosts[0]
			if len(row.Problems) != 0 || len(preview.Common) != 0 {
				t.Fatal(row.Problems, preview.Common)
			}
			disk := row.TargetDisk
			if disk == nil || string(disk.Key) != key || disk.Mode != tc.mode || disk.Value != tc.value {
				t.Fatalf("向导目标盘 = %+v", disk)
			}
			if tc.mode == "windows-index" {
				for _, field := range row.Fields {
					if field.Key == "partition/1/disk" || (string(field.Key) == key && !field.Locked) {
						t.Fatalf("Windows 分区参数 %+v", field)
					}
				}
			}
			created, err := w.Submit(t.Context(), operator, SubmitRequest{Selection: req.Selection, Hosts: []SubmitHost{{SN: host.SN, Entered: values}}})
			if err != nil {
				t.Fatal(err)
			}
			snap := created[0].Task.Snapshot
			slot := slices.IndexFunc(snap.Slots, func(s model.SnapshotSlot) bool { return s.Name == "partition" })
			if slot < 0 || snap.Slots[slot].Instances[0].Params[tc.param].Join() != disk.Value {
				t.Fatal("快照目标盘和向导不一致")
			}
			answer, ps, err := render.Render(render.Request{Snapshot: snap, Firmware: "uefi", Kind: catalog.FileAnswer, Values: map[catalog.SystemValue]string{"gateway": "http://gw.example.test", "artifacts": "http://artifacts.example.test", "task": "t1", "token": "fixture", "finalize-token": "finish", "syslog-addr": "192.0.2.1:514", "smb": "//files.example.test/windows"}})
			if err != nil || len(ps) > 0 || !strings.Contains(string(answer), tc.answer) {
				t.Fatalf("安装应答缺少 %q: %v %v", tc.answer, ps, err)
			}
		})
	}
}

func TestCustomWizardDiskNamesRemainOrdinaryFields(t *testing.T) {
	f, deps, selection, _ := applicationFixture()
	f.template.Params = append(f.template.Params, model.ParamDecl{Name: "disk", Label: "目标磁盘", Type: "diskpath", Default: "/dev/disk/by-id/example"})
	w := NewWizard(deps)
	result, err := w.Params(t.Context(), operator, ParamsRequest{Selection: selection, HostSNs: []string{"SN-1"}})
	if err != nil {
		t.Fatal(err)
	}
	row := result.Hosts[0]
	if row.TargetDisk != nil || len(row.Fields) != 2 || row.Fields[1].Prefill != "/dev/disk/by-id/example" {
		t.Fatal(row)
	}
}
