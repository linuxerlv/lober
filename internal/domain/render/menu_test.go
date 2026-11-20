package render

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func menuFixture(series catalog.Series, firmware catalog.Firmware) Request {
	paths := map[string]string{"kernel": "as-rocky94/vmlinuz", "initrd": "as-rocky94/initrd.img", "stage2": "as-rocky94/install/", "repo": "as-rocky94/repo/", "iso": "as-rocky94/original.iso"}
	if series == "debian" {
		paths["kernel"], paths["initrd"] = "as-debian12/linux", "as-debian12/initrd.gz"
	}
	if series == "ubuntu" {
		paths["kernel"], paths["initrd"], paths["iso"] = "as-ubuntu2404/vmlinuz", "as-ubuntu2404/initrd", "as-ubuntu2404/original.iso"
	}
	if series == "esxi" {
		paths["mboot"], paths["esxi-tree"] = "esxi8/efi/boot/bootx64.efi", "esxi8/"
	}
	return Request{Kind: catalog.FileMenu, Firmware: firmware, Values: map[catalog.SystemValue]string{"gateway": "http://gw.internal", "artifacts": "http://ng.internal/artifacts", "task": "42", "token": "install-tok-fixed-0000000000000000", "syslog-addr": "10.10.0.1:514"}, Snapshot: model.Snapshot{Schema: model.SnapshotSchema, Family: series.Family(), Series: series, Format: series.Format(), Host: model.SnapshotHost{SN: "SN-FIXED-0001"}, ISO: model.SnapshotISO{ID: "iso-1", Arch: "x86_64", Artifacts: paths}, Template: model.SnapshotTemplate{ID: "fixture", Version: 1, Params: map[string]model.SnapshotValue{"hostname": {Type: "hostname", Items: []string{"web-01"}}}}}}
}

func TestMenuGolden(t *testing.T) {
	for _, series := range []catalog.Series{"rocky", "debian", "ubuntu", "esxi"} {
		for _, fw := range []catalog.Firmware{"bios", "uefi"} {
			if series == "esxi" && fw == "bios" {
				continue
			}
			t.Run(string(series)+"-"+string(fw), func(t *testing.T) {
				r := menuFixture(series, fw)
				want, err := os.ReadFile(filepath.Join("testdata", "menu-"+string(series)+"-"+string(fw)+".golden"))
				if err != nil {
					t.Fatal(err)
				}
				got, p, err := Render(r)
				if err != nil || len(p) != 0 {
					t.Fatalf("render: %v %v", p, err)
				}
				if !bytes.Equal(got, want) {
					t.Fatalf("menu mismatch:\n%s\nwant:\n%s", got, want)
				}
				again, p, err := Render(r)
				if err != nil || len(p) != 0 || !bytes.Equal(got, again) {
					t.Fatal("nondeterministic menu")
				}
			})
		}
	}
}

// Each boundary catches a failed install, rather than merely pinning formatting.
func TestMenuRegressionBootAndInstallerArguments(t *testing.T) {
	for _, series := range []catalog.Series{"rocky", "debian", "ubuntu", "esxi"} {
		r := menuFixture(series, "uefi")
		got, p, err := Render(r)
		if err != nil || len(p) != 0 {
			t.Fatalf("%s: %v %v", series, p, err)
		}
		lines := strings.Split(strings.TrimSpace(string(got)), "\n")
		// 99b6ca7 / 4e32ed9: iPXE must recognize the script and execute loaded images.
		if lines[0] != "#!ipxe" || lines[len(lines)-1] != "boot" {
			t.Fatalf("unbootable script: %s", got)
		}
		if series != "esxi" && !strings.Contains(string(got), "console=ttyS0,115200n8") {
			t.Fatal("7d38246: missing serial console")
		}
	}
	r := menuFixture("rocky", "bios")
	got, _, _ := Render(r)
	text := strings.ReplaceAll(string(got), "\\\n", "")
	// 75bdc20 / 7eb805e: stage2 comes from the full installer tree, packages separately.
	for _, arg := range []string{"inst.stage2=http://ng.internal/artifacts/as-rocky94/install/", "inst.repo=http://ng.internal/artifacts/as-rocky94/repo ", "inst.cmdline", "inst.notmux", "inst.syslog=10.10.0.1:514"} {
		if !strings.Contains(text, arg) {
			t.Fatalf("missing installer arg %s", arg)
		}
	}
	if strings.Contains(text, "inst.text") {
		t.Fatal("9a252db: interactive text install")
	}
	// 23442b1 / 2ab744d: remote logging begins at installer startup, tmux cannot hide failure.
	r = menuFixture("ubuntu", "uefi")
	got, _, _ = Render(r)
	if !strings.Contains(string(got), "url=http://ng.internal/artifacts/as-ubuntu2404/original.iso") {
		t.Fatal("Ubuntu needs the original full ISO")
	}
	if !strings.Contains(string(got), "ds=nocloud-net;s=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ ") {
		t.Fatal("NoCloud seed must be answer directory with trailing slash")
	}
}

func TestMenuRejectsMissingValuesAndArtifacts(t *testing.T) {
	for _, series := range []catalog.Series{"rocky", "debian", "ubuntu", "esxi"} {
		keys := []catalog.SystemValue{"gateway", "artifacts", "task", "token"}
		if series == "rocky" {
			keys = append(keys, "syslog-addr")
		}
		for _, key := range keys {
			t.Run(string(series)+"-value-"+string(key), func(t *testing.T) {
				r := menuFixture(series, "uefi")
				delete(r.Values, key)
				assertRenderProblem(t, r, "render_system_value_missing")
			})
		}
		roles := []string{"kernel", "initrd"}
		switch series {
		case "rocky":
			roles = append(roles, "stage2", "repo")
		case "ubuntu":
			roles = append(roles, "iso")
		case "esxi":
			roles = []string{"mboot"}
		}
		for _, role := range roles {
			t.Run(string(series)+"-role-"+role, func(t *testing.T) {
				r := menuFixture(series, "uefi")
				delete(r.Snapshot.ISO.Artifacts, role)
				assertRenderProblem(t, r, "render_artifact_missing")
			})
		}
	}
}

func assertRenderProblem(t *testing.T, r Request, code string) {
	t.Helper()
	got, p, err := Render(r)
	if got != nil || err != nil || len(p) == 0 {
		t.Fatalf("want no output and %s Problem, got %q %v %v", code, got, p, err)
	}
	for _, item := range p {
		if string(item.Code) == code {
			return
		}
	}
	t.Fatalf("want %s, got %v", code, p)
}

func TestMenuActualFirmwareAndSnapshotCompatibility(t *testing.T) {
	for _, fw := range []catalog.Firmware{"", "efi", "pcbios", "UEFI"} {
		r := menuFixture("rocky", fw)
		r.Snapshot.Host.Firmware = "bios"
		assertRenderProblem(t, r, "render_firmware_invalid")
	}
	r := menuFixture("esxi", "bios")
	r.Snapshot.Host.Firmware = "uefi"
	assertRenderProblem(t, r, "firmware_mismatch")
	r = menuFixture("esxi", "uefi")
	r.Snapshot.Host.Firmware = "bios"
	got, p, err := Render(r)
	if len(got) == 0 || len(p) != 0 || err != nil {
		t.Fatalf("registered metadata overrode actual firmware: %v %v", p, err)
	}
	for _, change := range []func(*Request){func(r *Request) { r.Snapshot.Format = "preseed" }, func(r *Request) { r.Snapshot.Family = "esxi" }, func(r *Request) { r.Snapshot.Series = "unknown" }} {
		r := menuFixture("rocky", "uefi")
		change(&r)
		assertRenderProblem(t, r, "render_boot_snapshot_invalid")
	}
}

func TestMenuMachineURLsAreEscapedAndInputIsFrozen(t *testing.T) {
	r := menuFixture("ubuntu", "bios")
	r.Values["gateway"] = "http://gw.internal/base/"
	r.Values["artifacts"] = "http://ng.internal/artifacts/"
	r.Snapshot.ISO.Artifacts["iso"] = "ubuntu/original image.iso"
	got, p, err := Render(r)
	if len(p) != 0 || err != nil {
		t.Fatalf("%v %v", p, err)
	}
	for _, want := range []string{"url=http://ng.internal/artifacts/ubuntu/original%20image.iso", "s=http://gw.internal/base/v1/answer/42/install-tok-fixed-0000000000000000/"} {
		if !strings.Contains(string(got), want) {
			t.Fatal(want)
		}
	}
	if r.Snapshot.ISO.Artifacts["iso"] != "ubuntu/original image.iso" || r.Values["artifacts"] != "http://ng.internal/artifacts/" {
		t.Fatal("mutated request")
	}
	r.Values["iso"] = "http://other/iso"
	assertRenderProblem(t, r, "render_system_value_reserved")
}
