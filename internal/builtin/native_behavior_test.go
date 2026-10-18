package builtin

import (
	"encoding/xml"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/core/params"
	"github.com/lober/lober/internal/core/snapshot"
	"github.com/lober/lober/internal/render"
)

func withInput(t *testing.T, r render.Request, in params.Input) render.Request {
	t.Helper()
	s, ps := snapshot.Build(in)
	if len(ps) != 0 {
		t.Fatalf("snapshot: %v", ps)
	}
	r.Snapshot = s
	return r
}

func TestKickstartExecutesFirmwarePartitionBranches(t *testing.T) {
	tpl := templateFor(t, "kickstart")
	in := fixtureInput(t, tpl)
	in.Entered["partition/1/disk"] = "nvme0n1"
	r := withInput(t, fixtureRequest(t, tpl, "bios"), in)
	answer := renderFixture(t, r)
	pre := strings.Split(strings.Split(answer, "%pre --interpreter=/bin/sh --erroronfail\n")[1], "%end")[0]
	for _, uefi := range []bool{false, true} {
		t.Run(strconv.FormatBool(uefi), func(t *testing.T) {
			target := filepath.Join(t.TempDir(), "partition.ks")
			code := strings.ReplaceAll(pre, "/tmp/lober-partition.ks", target)
			// Replace only the firmware hardware probe with a shell function; execute
			// the exact generated branch and writes with both supported Linux shells.
			status := 1
			if uefi {
				status = 0
			}
			probe := fmt.Sprintf("test() { [ \"$1\" = -d ] && [ \"$2\" = /sys/firmware/efi ] || exit 99; return %d; }\n", status)
			for _, env := range []string{"LOBER_TEST_BASH", "LOBER_TEST_BUSYBOX"} {
				tool := os.Getenv(env)
				if tool == "" {
					t.Fatalf("%s required", env)
				}
				args := []string{}
				if env == "LOBER_TEST_BUSYBOX" {
					args = []string{"sh"}
				}
				cmd := exec.Command(tool, args...)
				cmd.Stdin = strings.NewReader(probe + code)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("partition branch: %v %s", err, out)
				}
				data, err := os.ReadFile(target)
				if err != nil {
					t.Fatal(err)
				}
				got := string(data)
				want := "part biosboot --fstype=biosboot --size=2 --ondisk=nvme0n1\n"
				if uefi {
					want = "part /boot/efi --fstype=efi --size=600 --ondisk=nvme0n1\n"
				}
				want += "part /boot --fstype=xfs --size=1024 --ondisk=nvme0n1\npart / --fstype=xfs --size=8192 --grow --ondisk=nvme0n1\n"
				if got != want {
					t.Fatalf("native partition output %q, want %q", got, want)
				}
			}
		})
	}
}

func TestWindowsNativePartitionBranchesAndTargetAgree(t *testing.T) {
	r := fixtureRequest(t, templateFor(t, "unattend"), "bios")
	if !strings.Contains(renderFixture(t, r), "<DiskID>0</DiskID><PartitionID>3</PartitionID>") {
		t.Fatal("installation target changed")
	}
	var source string
	for _, slot := range r.Snapshot.Slots {
		if slot.Name == "partition" {
			source = slot.Instances[0].Body
			if got := slot.Instances[0].Params["disk_index"].EnvValue(); got != "0" {
				t.Fatalf("pre disk %s differs from InstallTo", got)
			}
		}
	}
	for _, want := range []string{"wpeutil UpdateBootInfo", "PEFirmwareType", `if "%firmware%"=="0x2" goto uefi`, `if "%firmware%"=="0x1" goto bios`, "exit /b 65", "diskpart /s X:\\lober\\partition.txt"} {
		if !strings.Contains(source, want) {
			t.Errorf("missing native firmware/dispatch %q", want)
		}
	}
	// Native CMD cannot run in this Ubuntu test. Inspect the actual diskpart
	// programs separately; do not claim this simulates Windows Setup.
	cases := []struct {
		label, end string
		want       []string
	}{
		{"uefi", "goto apply", []string{"select disk %LOBER_PARAM_DISK_INDEX%", "clean", "convert gpt", "create partition efi size=260", "format quick fs=fat32 label=System", "create partition msr size=16", "create partition primary", "format quick fs=ntfs label=Windows"}},
		{"bios", ":apply", []string{"select disk %LOBER_PARAM_DISK_INDEX%", "clean", "convert mbr", "create partition primary size=750", "format quick fs=ntfs label=Recovery", "set id=27", "create partition primary size=550", "format quick fs=ntfs label=System", "active", "create partition primary", "format quick fs=ntfs label=Windows"}},
	}
	for _, tc := range cases {
		block := strings.Split(strings.Split(source, ":"+tc.label+"\n")[1], tc.end)[0]
		var got []string
		for _, line := range strings.Split(block, "\n") {
			if _, cmd, ok := strings.Cut(line, " echo "); ok {
				got = append(got, cmd)
			}
		}
		if !reflect.DeepEqual(got, tc.want) {
			t.Fatalf("%s partition commands: %q", tc.label, got)
		}
		n := 0
		for _, cmd := range got {
			if strings.HasPrefix(cmd, "create partition") {
				n++
			}
		}
		if n != 3 || got[len(got)-1] != "format quick fs=ntfs label=Windows" {
			t.Fatal("OS is not partition3")
		}
	}
}

func TestNativeProposalUsesRequestedDisk(t *testing.T) {
	for f, want := range map[catalog.Format]string{
		"preseed":     "partman-auto/disk string /dev/nvme0n1",
		"autoinstall": "path: '/dev/nvme0n1'",
		"autoyast":    "<device>/dev/nvme0n1</device>",
		"agama":       "search: '/dev/nvme0n1'",
	} {
		t.Run(string(f), func(t *testing.T) {
			tpl := templateFor(t, f)
			in := fixtureInput(t, tpl)
			in.Entered["partition/1/disk"] = "nvme0n1"
			r := withInput(t, fixtureRequest(t, tpl, "bios"), in)
			if got := renderFixture(t, r); !strings.Contains(got, want) {
				t.Errorf("requested disk not selected: %s", want)
			}
			for _, d := range tpl.Params {
				if strings.Contains(d.Name, "firmware") {
					t.Fatal("operator firmware option can disagree with actual boot")
				}
			}
		})
	}
}

func TestLinuxSSHHookConsumesEnvironmentAndWritesProtectedKey(t *testing.T) {
	for _, f := range []catalog.Format{"preseed", "agama"} {
		t.Run(string(f), func(t *testing.T) {
			h := sshkeyHook()
			impl, _ := h.ImplFor(series(f)[0])
			dir := t.TempDir()
			home := filepath.Join(dir, "home with space")
			if err := os.Mkdir(home, 0700); err != nil {
				t.Fatal(err)
			}
			bin := filepath.Join(dir, "bin")
			if err := os.Mkdir(bin, 0700); err != nil {
				t.Fatal(err)
			}
			// Only NSS is replaced. Actual install/chown/chmod and bytes are verified.
			shim := fmt.Sprintf("#!/bin/sh\n[ \"$1\" = passwd ] && [ \"$2\" = fixtureuser ] || exit 1\nprintf 'fixtureuser:x:%d:%d::%%s:/bin/sh\\n' \"$FIXTURE_HOME\"\n", os.Getuid(), os.Getgid())
			if err := os.WriteFile(filepath.Join(bin, "getent"), []byte(shim), 0700); err != nil {
				t.Fatal(err)
			}
			key := strings.TrimSuffix(fixtureKey, "fixture") + "comment=with^safe:chars"
			cmd := exec.Command(os.Getenv("LOBER_TEST_BASH"))
			cmd.Stdin = strings.NewReader(impl.Body)
			cmd.Env = append(os.Environ(), "PATH="+bin+":/usr/bin:/bin", "FIXTURE_HOME="+home, model.EnvName("username")+"=fixtureuser", model.EnvName("key")+"="+key)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("SSH hook: %v %s", err, out)
			}
			path := filepath.Join(home, ".ssh", "authorized_keys")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(b) != key+"\n" {
				t.Fatal("SSH key bytes changed")
			}
			for p, mode := range map[string]os.FileMode{filepath.Dir(path): 0700, path: 0600} {
				st, err := os.Stat(p)
				if err != nil || st.Mode().Perm() != mode {
					t.Fatalf("permissions on %s: %v %v", p, st, err)
				}
			}
		})
	}
}

func TestHookParametersAffectNativeAnswers(t *testing.T) {
	for _, tpl := range Templates() {
		t.Run(tpl.ID, func(t *testing.T) {
			in := fixtureInput(t, tpl)
			in.Entered["network/1/hostname"] = "changed-node"
			in.Entered["account/1/username"] = "changed-user"
			if tpl.Family == "linux" {
				in.Entered["packages/1/packages"] = "vim\ncurl"
			}
			r := withInput(t, fixtureRequest(t, tpl, "uefi"), in)
			got := renderFixture(t, r)
			f, _ := tpl.Format()
			if f != "autoinstall" && !strings.Contains(got, "changed-node") {
				t.Fatal("hostname parameter lost")
			}
			if tpl.Family != "esxi" && !strings.Contains(got, "changed-user") {
				t.Fatal("account username lost")
			}
			if tpl.Family == "linux" {
				switch f {
				case "kickstart":
					if !strings.Contains(got, "vim\ncurl\n") {
						t.Fatal("package lines lost")
					}
				case "preseed":
					if !strings.Contains(got, "openssh-server vim curl") {
						t.Fatal("package list join lost")
					}
				case "autoinstall":
					if !strings.Contains(got, "- 'vim'\n    - 'curl'") {
						t.Fatal("package YAML values lost")
					}
				case "autoyast":
					if !strings.Contains(got, "<package>vim</package>") {
						t.Fatal("package XML lost")
					}
				case "agama":
					if !strings.Contains(got, "'vim',") {
						t.Fatal("package Jsonnet lost")
					}
				}
			}
		})
	}
}

func TestEverySeriesAndMediaChoicesBuild(t *testing.T) {
	for _, tpl := range Templates() {
		for _, s := range tpl.Series {
			t.Run(string(s), func(t *testing.T) {
				in := fixtureInput(t, tpl)
				in.ISO.Series = s
				if s == "leap16" {
					in.Entered["tpl/product"] = "Leap"
				}
				snap, ps := snapshot.Build(in)
				if len(ps) != 0 {
					t.Fatalf("snapshot: %v", ps)
				}
				r := fixtureRequest(t, tpl, "uefi")
				r.Snapshot = snap
				out := renderFixture(t, r)
				if s == "leap16" && !strings.Contains(out, "product: { id: 'Leap' }") {
					t.Fatal("Leap ISO got SLES profile")
				}
			})
		}
	}
	for _, version := range []string{"2016", "2019", "2022", "2025"} {
		tpl := templateFor(t, "unattend")
		in := fixtureInput(t, tpl)
		in.ISO.Version = version
		in.ISO.Offers["editions"] = []model.Option{{Value: "2", Label: "Server Standard"}}
		in.Entered["tpl/edition"] = "2"
		r := withInput(t, fixtureRequest(t, tpl, "uefi"), in)
		if !strings.Contains(renderFixture(t, r), "<Value>2</Value>") {
			t.Fatalf("%s wrong edition", version)
		}
		in.Entered["tpl/edition"] = "4"
		if _, ps := snapshot.Build(in); len(ps) == 0 {
			t.Fatalf("%s accepted absent media edition", version)
		}
	}
	tpl := templateFor(t, "agama")
	in := fixtureInput(t, tpl)
	delete(in.ISO.Offers, "products")
	if _, ps := snapshot.Build(in); len(ps) == 0 {
		t.Fatal("Agama accepted media without product inventory")
	}
}

func TestBuiltinCredentialsHaveNoPublicDefault(t *testing.T) {
	for _, h := range Hooks() {
		for _, d := range h.Params {
			if d.Name == "password" || d.Name == "pwhash" || d.Name == "key" {
				if d.Default != "" {
					t.Fatalf("public credential default in %s", h.ID)
				}
			}
		}
	}
	for _, tpl := range Templates() {
		in := fixtureInput(t, tpl)
		in.Entered = map[params.FieldKey]string{}
		if _, ps := snapshot.Build(in); len(ps) == 0 {
			t.Fatalf("%s installs with no supplied credentials", tpl.ID)
		}
	}
}

func TestAutoYaSTNativeUserAndKey(t *testing.T) {
	tpl := templateFor(t, "autoyast")
	in := fixtureInput(t, tpl)
	in.Entered["account/1/username"] = "native-user"
	r := withInput(t, fixtureRequest(t, tpl, "uefi"), in)
	out := renderFixture(t, r)
	var profile struct {
		Users []struct {
			Name     string   `xml:"username"`
			Password string   `xml:"user_password"`
			Keys     []string `xml:"authorized_keys>listentry"`
		} `xml:"users>user"`
	}
	if err := xml.Unmarshal([]byte(out), &profile); err != nil {
		t.Fatal(err)
	}
	if len(profile.Users) != 2 {
		t.Fatalf("want locked root and native user, got %d", len(profile.Users))
	}
	if profile.Users[0].Name != "root" || profile.Users[0].Password != "!" {
		t.Fatal("root account must be explicitly locked")
	}
	u := profile.Users[1]
	if u.Name != "native-user" || len(u.Keys) != 1 || u.Keys[0] != fixtureKey {
		t.Fatalf("key not attached to configured native user: %+v", u)
	}
}

func TestPOSIXHookSyntaxAndESXiVersions(t *testing.T) {
	for _, h := range Hooks() {
		for _, impl := range h.Impls {
			if impl.Interpreter != "sh" {
				continue
			}
			for _, env := range []string{"LOBER_TEST_BASH", "LOBER_TEST_BUSYBOX"} {
				args := []string{"-n"}
				if env == "LOBER_TEST_BUSYBOX" {
					args = []string{"sh", "-n"}
				}
				cmd := exec.Command(os.Getenv(env), args...)
				cmd.Stdin = strings.NewReader(impl.Body)
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("%s/%s: %v %s", h.ID, impl.Series, err, out)
				}
			}
		}
	}
	for _, version := range []string{"8", "9"} {
		tpl := templateFor(t, "esxi-ks")
		in := fixtureInput(t, tpl)
		in.ISO.Version = version
		r := withInput(t, fixtureRequest(t, tpl, "uefi"), in)
		if !strings.Contains(renderFixture(t, r), "%firstboot --interpreter=busybox") {
			t.Fatalf("ESXi%s lost native lifecycle", version)
		}
	}
}
