package authoring

import (
	"bytes"
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
	"reflect"
	"strings"
	"testing"
)

type machineBootConfig struct {
	calls int
	ctx   context.Context
	iso   model.SnapshotISO
	body  string
	err   error
}

func (r *machineBootConfig) ReadESXiBootConfig(ctx context.Context, iso model.SnapshotISO) (string, error) {
	r.calls++
	r.ctx = ctx
	r.iso = iso
	return r.body, r.err
}
func Test机器渲染只用冻结任务且仅ESXi辅助文件读取原配置(t *testing.T) {
	f := frozenPreviewFixture(t)
	values := map[catalog.SystemValue]string{catalog.SysGateway: "https://machine.test", catalog.SysArtifacts: "https://artifacts.test", catalog.SysTask: "frozen-task", catalog.SysToken: "real-install", catalog.SysFinalizeToken: "real-final", catalog.SysSN: "FROZEN-SN", catalog.SysSyslogAddr: "udp://logs.test:514", catalog.SysSMB: `\\smb.test\files`}
	f.Template.Body = "# frozen {{sys:task}} {{sys:token}} {{sys:finalize-token}} {{sys:sn}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"
	f.Slots = []model.SnapshotSlot{{Seq: 2, Name: "chosen", Exec: true, Timing: "post-target"}}
	for _, tc := range []struct {
		name      string
		format    catalog.Format
		kind      catalog.FileKind
		slot      string
		execution *model.Execution
		readErr   error
	}{
		{"应答无需执行固件", f.Format, catalog.FileAnswer, "", nil, nil},
		{"菜单实际固件", f.Format, catalog.FileMenu, "", &model.Execution{Firmware: catalog.FirmwareBIOS}, nil},
		{"菜单缺执行固件", f.Format, catalog.FileMenu, "", nil, nil},
		{"只请求指定脚本", f.Format, catalog.FileScript, "chosen", nil, nil},
		{"普通辅助不读配置", catalog.FormatAutoinstall, catalog.FileAux, "", nil, nil},
		{"ESXi应答不读配置", catalog.FormatESXiKS, catalog.FileAnswer, "", nil, nil},
		{"ESXi菜单不读配置", catalog.FormatESXiKS, catalog.FileMenu, "", &model.Execution{Firmware: catalog.FirmwareUEFI}, nil},
		{"ESXi辅助读配置", catalog.FormatESXiKS, catalog.FileAux, "", nil, nil},
		{"读取失败优先", catalog.FormatESXiKS, catalog.FileAux, "", nil, errors.New("private path")},
		{"无效文件", f.Format, catalog.FileKind("bad"), "", nil, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			frozen := f
			frozen.Format = tc.format
			if tc.format == catalog.FormatESXiKS {
				frozen.Series = catalog.SeriesESXi
				frozen.Family = catalog.FamilyESXi
				frozen.ISO.Artifacts = map[string]string{"esxi-tree": "frozen/tree", "mboot": "frozen/mboot.efi"}
			}
			reader := &machineBootConfig{body: "kernel=b.b00\nmodules=jumpstrt.gz\n", err: tc.readErr}
			req := MachineRenderRequest{Task: model.Task{ID: "frozen-task", Snapshot: frozen, Execution: tc.execution}, Kind: tc.kind, Slot: tc.slot, Values: values}
			fw := catalog.Firmware("")
			if tc.execution != nil {
				fw = tc.execution.Firmware
			}
			want, ps, werr := render.Render(render.Request{Snapshot: frozen, Firmware: fw, ESXiBootConfig: reader.body, Kind: tc.kind, Slot: tc.slot, Values: values})
			got, err := NewMachineRenderer(reader).Render(t.Context(), req)
			shouldRead := tc.format == catalog.FormatESXiKS && tc.kind == catalog.FileAux
			calls := 0
			if shouldRead {
				calls = 1
			}
			if reader.calls != calls {
				t.Fatal("原配置读取次数", reader.calls, calls)
			}
			if shouldRead && (reader.ctx != t.Context() || !reflect.DeepEqual(reader.iso, frozen.ISO)) {
				t.Fatal("读取未透传冻结ISO或上下文")
			}
			wantErr := ""
			if werr != nil || len(ps) > 0 {
				wantErr = "machine rendering rejected"
			}
			if shouldRead && tc.readErr != nil {
				wantErr = "immutable boot configuration unavailable"
			}
			if wantErr != "" {
				if err == nil || err.Error() != wantErr || got != nil {
					t.Fatal("错误或部分结果改变", err, string(got))
				}
				return
			}
			if err != nil || !bytes.Equal(got, want) || len(got) == 0 {
				t.Fatal("未透传请求", err, string(got), string(want))
			}
			if strings.Contains(string(got), "sample-") {
				t.Fatal("机器文件混入示例")
			}
		})
	}
	t.Run("缺值不注入示例", func(t *testing.T) {
		_, err := NewMachineRenderer(nil).Render(t.Context(), MachineRenderRequest{Task: model.Task{Snapshot: f}, Kind: catalog.FileAnswer})
		if err == nil || err.Error() != "machine rendering rejected" {
			t.Fatal(err)
		}
	})
	t.Run("领域问题", func(t *testing.T) {
		v := map[catalog.SystemValue]string{catalog.SysRepo: "reserved"}
		body, err := NewMachineRenderer(nil).Render(t.Context(), MachineRenderRequest{Task: model.Task{Snapshot: f}, Kind: catalog.FileAnswer, Values: v})
		if body != nil || err == nil || err.Error() != "machine rendering rejected" {
			t.Fatal(err)
		}
	})
}
