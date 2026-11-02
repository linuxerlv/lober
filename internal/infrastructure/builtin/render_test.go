package builtin

import (
	"bytes"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/render"
	"github.com/lober/lober/internal/domain/scan"
	"github.com/lober/lober/internal/domain/snapshot"
)

const fixtureKey = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA fixture"

func fixtureInput(t *testing.T, tpl model.Template) params.Input {
	t.Helper()
	hs := map[model.HookKey]model.Hook{}
	for _, h := range Hooks() {
		hs[h.Key()] = h
	}
	entered := map[params.FieldKey]string{}
	samples := map[string]string{"pwhash": "$6$fixture$notARealPasswordHash", "password": "Fixture_Pass9", "key": fixtureKey, "edition": "4", "product": "SLES"}
	// Linux 和 Windows 的 packages 都有内置默认值，不用手填；ESXi 的 VIB 地址没有默认值。
	if tpl.Family == "esxi" {
		samples["packages"] = "https://depot.example.test/driver.vib"
	}
	for _, d := range tpl.Params {
		if v, ok := samples[d.Name]; ok {
			entered[params.FieldKey(model.TemplateFieldKey(d.Name))] = v
		}
	}
	for slot, instances := range tpl.Assembly {
		for i, in := range instances {
			h := hs[model.HookKey{ID: in.HookID, Version: in.HookVersion}]
			impl, _ := h.ImplFor(tpl.Series[0])
			fragment := impl.Kind == model.ImplFragment
			var refs scan.Refs
			if fragment {
				res, _ := scan.Scan(impl.Body)
				refs = res.Refs()
			}
			for _, d := range h.Params {
				// 片段没引用的参数不进表单，填了会被参数求值当成未知字段。
				if fragment {
					if _, ok := refs[d.Name]; !ok {
						continue
					}
				}
				v, ok := samples[d.Name]
				if !ok {
					continue
				}
				if b, bound := in.Bindings[d.Name]; !bound || b.Editable {
					entered[params.FieldKey(model.InstanceFieldKey(slot, i+1, d.Name))] = v
				}
			}
		}
	}
	iso := model.ISO{ID: "iso-9", Name: "fixture", Series: tpl.Series[0], Version: "fixture", Arch: "amd64", Artifacts: map[string]string{"repo": "media/repo/", "iso": "media/image.iso", "winpe-busybox": "media/busybox.exe", "winpe-curl": "media/curl.exe"}, Offers: map[string][]model.Option{"editions": {{Value: "4", Label: "Server Datacenter"}}, "products": {{Value: "SLES", Label: "SLES"}, {Value: "Leap", Label: "Leap"}}}}
	for _, role := range catalog.RequiredRoles(iso.Series) {
		if _, exists := iso.Artifacts[role]; !exists {
			iso.Artifacts[role] = "media/" + role
		}
	}
	return params.Input{Template: tpl, Hooks: hs, ISO: iso,
		Host:    model.Host{SN: "TASK9-FIXTURE", Arch: "amd64", Attrs: map[string]string{"inv_system_disk": "sda"}},
		Entered: entered}
}

func fixtureRequest(t *testing.T, tpl model.Template, firmware catalog.Firmware) render.Request {
	t.Helper()
	snap, ps := snapshot.Build(fixtureInput(t, tpl))
	if len(ps) != 0 {
		t.Fatalf("snapshot %s: %v", tpl.ID, ps)
	}
	return render.Request{Snapshot: snap, Firmware: firmware, Kind: catalog.FileAnswer, Values: map[catalog.SystemValue]string{"gateway": "http://gw.example.test", "artifacts": "http://artifacts.example.test", "task": "task-9", "token": "boot-fixture", "finalize-token": "finalize-fixture", "syslog-addr": "192.0.2.1:514", "smb": "//files.example.test/windows"}}
}

func renderFixture(t *testing.T, r render.Request) string {
	t.Helper()
	b, ps, err := render.Render(r)
	if err != nil || len(ps) != 0 {
		t.Fatalf("render: %v %v", err, ps)
	}
	return string(b)
}

func TestAllBuiltinAnswersFromSnapshots(t *testing.T) {
	for _, tpl := range Templates() {
		for _, fw := range []catalog.Firmware{"bios", "uefi"} {
			t.Run(tpl.ID+"/"+string(fw), func(t *testing.T) {
				r := fixtureRequest(t, tpl, fw)
				got := renderFixture(t, r)
				if strings.Contains(got, "{{slot:") || strings.Contains(got, "{{param:") || strings.Contains(got, "{{lober:") {
					t.Fatal("unexpanded builtin placeholder")
				}
				if got != renderFixture(t, r) {
					t.Fatal("non-deterministic answer")
				}
				if r.Snapshot.Host.Firmware != "" {
					t.Fatal("fixture must exercise absent registered firmware")
				}
				body, ps := scan.Scan(tpl.Body)
				if len(ps) != 0 {
					t.Fatalf("scan: %v", ps)
				}
				if len(body.Slots) != len(tpl.Assembly) {
					t.Fatalf("slots/assembly mismatch %d/%d", len(body.Slots), len(tpl.Assembly))
				}
				for _, slot := range r.Snapshot.Slots {
					if slot.Exec {
						q := r
						q.Kind = catalog.FileScript
						q.Slot = slot.Name
						if renderFixture(t, q) == "" {
							t.Fatal("empty execution wrapper")
						}
						for _, in := range slot.Instances {
							if in.Digest == "" || in.TimeoutSec <= 0 {
								t.Fatal("unfrozen runtime hook")
							}
						}
					}
				}
				f, _ := tpl.Format()
				if f == "autoyast" || f == "unattend" {
					d := xml.NewDecoder(strings.NewReader(got))
					for {
						_, err := d.Token()
						if err == io.EOF {
							break
						}
						if err != nil {
							t.Fatalf("invalid native XML: %v", err)
						}
					}
				}
				file := filepath.Join("testdata", "answers", string(f)+"-"+string(fw)+".golden")
				if f == "agama" && tpl.Version >= 3 {
					file = filepath.Join("testdata", "answers", fmt.Sprintf("agama-v%d-%s.golden", tpl.Version, fw))
				}
				if os.Getenv("LOBER_RECORD_BUILTIN") == "1" {
					if _, err := os.Stat(file); err == nil {
						t.Fatalf("refuse to replace existing golden %s", file)
					}
					if err := os.MkdirAll(filepath.Dir(file), 0755); err != nil {
						t.Fatal(err)
					}
					if err := os.WriteFile(file, []byte(got), 0644); err != nil {
						t.Fatal(err)
					}
				} else {
					want, err := os.ReadFile(file)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal([]byte(got), want) {
						t.Fatalf("golden mismatch: %s", file)
					}
				}
			})
		}
	}
}

func templateFor(t *testing.T, f catalog.Format) model.Template {
	t.Helper()
	for _, tpl := range Templates() {
		got, _ := tpl.Format()
		if got == f {
			return tpl
		}
	}
	t.Fatalf("missing template %s", f)
	return model.Template{}
}

func TestNativeInstallerContracts(t *testing.T) {
	cases := map[catalog.Format][]string{
		"kickstart":   {"reqpart", "part /boot --fstype=xfs --size=1024 --ondisk=sda", "part / --fstype=xfs --size=8192 --grow --ondisk=sda", "--disklabel=gpt", "%post --nochroot", "systemd", "openssh-server", "sshkey --username="},
		"preseed":     {"partman-auto/choose_recipe select atomic", "partman-partitioning/default_label string gpt", "partman-auto/disk string /dev/sda", "pkgsel/include string curl systemd openssh-server", "preseed/late_command"},
		"autoinstall": {"#cloud-config", "RAM must exceed the full ISO size", "name: direct", "path: '/dev/sda'", "ptable: gpt", "late-commands:", "install-server: true"},
		"autoyast":    {"<device>/dev/sda</device>", "<disklabel>gpt</disklabel>", "<mount>/</mount>", "<chrooted config:type=\"boolean\">false</chrooted>", "<package>systemd</package>"},
		"agama":       {"product: { id: 'SLES' }", "ptableType: 'gpt'", "configure: true", "generate: 'default'", "chroot: false", "method4: 'auto'"},
		"unattend":    {"<PartitionID>3</PartitionID>", "/IMAGE/INDEX", "<Value>4</Value>", "<AutoLogon>", "<LogonCount>1</LogonCount>", "<ComputerName>lober-node</ComputerName>"},
		"esxi-ks":     {"install --firstdisk='local' --overwritevmfs", "%firstboot --interpreter=busybox", "rootpw --iscrypted", "network --bootproto=dhcp"},
	}
	for f, needles := range cases {
		t.Run(string(f), func(t *testing.T) {
			tpl := templateFor(t, f)
			r := fixtureRequest(t, tpl, "uefi")
			answer := renderFixture(t, r)
			for _, s := range needles {
				if !strings.Contains(answer, s) {
					t.Errorf("missing native contract %q", s)
				}
			}
			bios := r
			bios.Firmware = "bios"
			if renderFixture(t, bios) != answer {
				t.Error("answer must use native firmware detection, not host/request default")
			}
			if f == "agama" && strings.Contains(answer, "init:") {
				t.Fatal("duplicate native firstboot route")
			}
			if f == "unattend" {
				if strings.Count(answer, "<FirstLogonCommands>") != 1 || strings.Count(answer, "<CommandLine>") != 1 {
					t.Fatal("firstboot must use one command chain")
				}
				chain := strings.Split(strings.Split(answer, "<CommandLine>")[1], "</CommandLine>")[0]
				if len(chain) > 1024 || strings.Count(chain, "&amp;&amp;") != 2 {
					t.Fatalf("invalid chain length/order: %d", len(chain))
				}
			}
			if f == "esxi-ks" {
				r.Kind = catalog.FileMenu
				r.Firmware = "bios"
				b, ps, err := render.Render(r)
				if err != nil || len(b) != 0 || len(ps) == 0 {
					t.Fatal("ESXi BIOS was not rejected")
				}
			}
		})
	}
}

func TestAgamaProductAndWindowsEditionComeFromMedia(t *testing.T) {
	for _, f := range []catalog.Format{"agama", "unattend"} {
		tpl := templateFor(t, f)
		name, offer := "product", "products"
		if f == "unattend" {
			name, offer = "edition", "editions"
		}
		found := false
		for _, d := range tpl.Params {
			if d.Name == name {
				found = true
				if d.Type != "enum" || d.Offer != offer || d.Default != "" {
					t.Errorf("media choice has static default: %+v", d)
				}
			}
		}
		if !found {
			t.Errorf("no %s choice", name)
		}
	}
}

func decodedCommands(text string) string {
	var all []string
	for _, line := range strings.Split(text, "\n") {
		for _, piece := range strings.Split(line, "echo ") {
			for _, suffix := range []string{" | base64 -d | sh", " | /usr/libexec/platform-python -m base64 -d | sh"} {
				token, _, ok := strings.Cut(piece, suffix)
				if !ok {
					continue
				}
				b, err := base64.StdEncoding.DecodeString(token)
				if err == nil {
					all = append(all, string(b))
				}
			}
		}
	}
	return strings.Join(all, "\n")
}

func TestLinuxFirstbootIsStagedInInstallerTarget(t *testing.T) {
	for _, f := range []catalog.Format{"kickstart", "preseed", "autoinstall", "autoyast", "agama"} {
		r := fixtureRequest(t, templateFor(t, f), "uefi")
		out := decodedCommands(renderFixture(t, r))
		root := "/target"
		if f == "kickstart" {
			root = "/mnt/sysroot"
		}
		if f == "autoyast" || f == "agama" {
			root = "/mnt"
		}
		for _, want := range []string{root + "/var/lib/lober/firstboot.sh", root + "/etc/systemd/system/lober-firstboot.service", "finalize-fixture", "sh /var/lib/lober/cleanup.sh"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s missing staging %s", f, want)
			}
		}
	}
}

func TestRecordCurrentRelease(t *testing.T) {
	if os.Getenv("LOBER_RECORD_RELEASE") != "1" {
		t.Skip("one-time immutable release recording")
	}
	releases, err := filepath.Glob("testdata/releases/v*.json")
	if err != nil || len(releases) == 0 {
		t.Fatalf("no historical releases: %v", err)
	}
	next := 1
	for _, file := range releases {
		var n int
		if _, err := fmt.Sscanf(filepath.Base(file), "v%d.json", &n); err != nil {
			t.Fatalf("unreadable release fixture %s: %v", file, err)
		}
		if n >= next {
			next = n + 1
		}
	}
	file := fmt.Sprintf("testdata/releases/v%d.json", next)
	r := release{Templates: Templates(), Hooks: Hooks()}
	if err := os.WriteFile(file, append(encode(r), '\n'), 0644); err != nil {
		t.Fatal(err)
	}
	for _, tpl := range r.Templates {
		recordHash(t, "template", tpl.ID, tpl.Version, tpl)
	}
	for _, h := range r.Hooks {
		recordHash(t, "hook", h.ID, h.Version, h)
	}
}
func recordHash(t *testing.T, kind, id string, version int, v any) {
	t.Helper()
	file := fmt.Sprintf("testdata/fingerprints/%s-%s-v%d.sha256", kind, id, version)
	if data, err := os.ReadFile(file); err == nil {
		if want := Fingerprint(v) + "\n"; string(data) != want {
			t.Fatalf("existing fingerprint %s differs: content changed without a version bump", file)
		}
		return
	}
	if err := os.WriteFile(file, []byte(Fingerprint(v)+"\n"), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestBuiltinDeliveryLifecycle(t *testing.T) {
	for _, tpl := range Templates() {
		t.Run(tpl.ID, func(t *testing.T) {
			r := fixtureRequest(t, tpl, "uefi")
			for _, role := range []string{"kernel", "initrd", "stage2", "mboot", "live-root", "wimboot", "bcd", "boot-sdi", "boot-wim", "winpeshl"} {
				r.Snapshot.ISO.Artifacts[role] = "media/" + role
			}
			r.Snapshot.ISO.Artifacts["esxi-tree"] = "media/esxi/"
			r.Kind = catalog.FileMenu
			if !strings.HasPrefix(renderFixture(t, r), "#!ipxe\n") {
				t.Fatal("missing iPXE menu")
			}
			r.Firmware = "bios"
			if r.Snapshot.Family == "esxi" {
				b, ps, err := render.Render(r)
				if err != nil || len(b) != 0 || len(ps) != 1 || ps[0].Code != "firmware_mismatch" {
					t.Fatalf("BIOS rejection: %v %v", err, ps)
				}
			} else {
				renderFixture(t, r)
			}
			if _, ok := catalog.DeliveryFileName(r.Snapshot.Format, catalog.FileAux); ok {
				r.Kind = catalog.FileAux
				r.ESXiBootConfig = "kernel=/b.b00\nmodules=/jumpstrt.gz --- /useropts.gz\nkernelopt=runweasel cdromBoot\n"
				out := renderFixture(t, r)
				if r.Snapshot.Format == "autoinstall" && out != "instance-id: lober-task-task-9\n" {
					t.Fatal("metadata instance-id mismatch")
				}
			}
		})
	}
}

func TestLinuxPackagesComeOnlyFromSnapshot(t *testing.T) {
	parse := map[catalog.Format]func(string) []string{
		"preseed": func(out string) []string {
			for _, line := range strings.Split(out, "\n") {
				if rest, ok := strings.CutPrefix(line, "d-i pkgsel/include string "); ok {
					return strings.Fields(rest)
				}
			}
			return nil
		},
		"kickstart": func(out string) []string {
			block := strings.Split(strings.Split(out, "%packages\n")[1], "%end")[0]
			var pkgs []string
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if line != "" && !strings.HasPrefix(line, "@") {
					pkgs = append(pkgs, line)
				}
			}
			return pkgs
		},
		"autoinstall": func(out string) []string {
			block := strings.Split(strings.Split(out, "  packages:\n")[1], "  late-commands:")[0]
			var pkgs []string
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if rest, ok := strings.CutPrefix(line, "- '"); ok {
					pkgs = append(pkgs, strings.TrimSuffix(rest, "'"))
				}
			}
			return pkgs
		},
		"autoyast": func(out string) []string {
			block := strings.Split(strings.Split(out, "<packages config:type=\"list\">")[1], "</packages>")[0]
			var pkgs []string
			for _, line := range strings.Split(block, "\n") {
				if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "<package>"); ok {
					pkgs = append(pkgs, strings.TrimSuffix(rest, "</package>"))
				}
			}
			return pkgs
		},
		"agama": func(out string) []string {
			block := strings.Split(strings.Split(out, "packages: [")[1], "],")[0]
			var pkgs []string
			for _, line := range strings.Split(block, "\n") {
				line = strings.TrimSpace(line)
				if rest, ok := strings.CutPrefix(line, "'"); ok {
					pkgs = append(pkgs, strings.TrimSuffix(rest, "',"))
				}
			}
			return pkgs
		},
	}
	for _, format := range []catalog.Format{"kickstart", "preseed", "autoinstall", "autoyast", "agama"} {
		t.Run(string(format), func(t *testing.T) {
			tpl := templateFor(t, format)
			in := fixtureInput(t, tpl)
			snap, ps := snapshot.Build(in)
			if len(ps) != 0 {
				t.Fatal(ps)
			}
			var fromSnapshot []string
			for _, slot := range snap.Slots {
				for _, inst := range slot.Instances {
					if inst.HookID == "builtin-packages" {
						fromSnapshot = inst.Params["packages"].Items
					}
				}
			}
			if len(fromSnapshot) == 0 {
				t.Fatal("快照里没有 packages 参数")
			}
			out := renderFixture(t, render.Request{
				Snapshot: snap, Kind: catalog.FileAnswer, Firmware: "uefi",
				Values: fixtureRequest(t, tpl, "uefi").Values,
			})
			got := parse[format](out)
			if !slices.Equal(got, fromSnapshot) {
				t.Fatalf("渲染的包 %q 与快照 %q 不一致", got, fromSnapshot)
			}
			seen := map[string]bool{}
			for _, pkg := range got {
				if seen[pkg] {
					t.Fatalf("包 %q 出现了两次：%q", pkg, got)
				}
				seen[pkg] = true
			}
			for _, want := range []string{"curl", "systemd"} {
				if !slices.Contains(got, want) {
					t.Fatalf("默认包列表缺少 %s：%q", want, got)
				}
			}
			ssh := "openssh-server"
			if format == "autoyast" || format == "agama" {
				ssh = "openssh"
			}
			if !slices.Contains(got, ssh) {
				t.Fatalf("默认包列表缺少 %s：%q", ssh, got)
			}
			// 操作员改填的值必须原样替换，不再偷偷保留基础包。
			in.Entered["packages/1/packages"] = "vim\ncurl"
			over, ps := snapshot.Build(in)
			if len(ps) != 0 {
				t.Fatal(ps)
			}
			out = renderFixture(t, render.Request{
				Snapshot: over, Kind: catalog.FileAnswer, Firmware: "uefi",
				Values: fixtureRequest(t, tpl, "uefi").Values,
			})
			if got := parse[format](out); !slices.Equal(got, []string{"vim", "curl"}) {
				t.Fatalf("改填后仍带出隐藏包：%q", got)
			}
		})
	}
}
