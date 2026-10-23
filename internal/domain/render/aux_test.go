package render

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

const originalESXiBootConfig = "bootstate=0\ntitle=Loading ESXi installer\ntimeout=5\nprefix=\nkernel=/b.b00\nkernelopt=runweasel\nmodules=/jumpstrt.gz --- /useropts.gz --- /k.b00 --- /chain.b00\nbuild=8.0.0-2000000\nupdated=0\n"

func TestAuxGolden(t *testing.T) {
	for _, tc := range []struct {
		series catalog.Series
		name   string
	}{{"ubuntu", "meta-data.golden"}, {"esxi", "esxi-bootcfg.golden"}} {
		t.Run(string(tc.series), func(t *testing.T) {
			r := menuFixture(tc.series, "uefi")
			r.Kind = catalog.FileAux
			r.ESXiBootConfig = originalESXiBootConfig
			want, err := os.ReadFile("testdata/" + tc.name)
			if err != nil {
				t.Fatal(err)
			}
			got, p, err := Render(r)
			if err != nil || len(p) != 0 || !bytes.Equal(got, want) {
				t.Fatalf("%s: got %s, %v %v; want %s", tc.name, got, p, err, want)
			}
			again, p, err := Render(r)
			if err != nil || len(p) != 0 || !bytes.Equal(got, again) {
				t.Fatal("nondeterministic auxiliary file")
			}
		})
	}
}

func TestUbuntuNoCloudDirectoryAndMetadata(t *testing.T) {
	r := menuFixture("ubuntu", "uefi")
	menu, p, err := Render(r)
	if err != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, err)
	}
	seed := "http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/"
	if !strings.Contains(string(menu), "s="+seed+" ") {
		t.Fatal("seed URL")
	}
	answerName, _ := catalog.DeliveryFileName(r.Snapshot.Format, catalog.FileAnswer)
	auxName, _ := catalog.DeliveryFileName(r.Snapshot.Format, catalog.FileAux)
	if answerName != "user-data" || auxName != "meta-data" {
		t.Fatal("seed sibling routes")
	}
	r.Kind = catalog.FileAux
	delete(r.Values, "gateway")
	delete(r.Values, "token")
	delete(r.Values, "artifacts")
	got, p, err := Render(r)
	if err != nil || len(p) != 0 || string(got) != "instance-id: lober-task-42\n" {
		t.Fatalf("metadata should need only task: %q %v %v", got, p, err)
	}
	delete(r.Snapshot.Template.Params, "hostname")
	if got, p, err := Render(r); err != nil || len(p) != 0 || string(got) != "instance-id: lober-task-42\n" {
		t.Fatalf("metadata without hostname: %q %v %v", got, p, err)
	}
	r = menuFixture("ubuntu", "bios")
	r.Kind = catalog.FileAux
	delete(r.Values, "task")
	assertRenderProblem(t, r, "render_system_value_missing")
}

func TestESXiPreservesFullModulePathsOrderAndUnknownFields(t *testing.T) {
	r := menuFixture("esxi", "uefi")
	r.Kind = catalog.FileAux
	r.ESXiBootConfig = "# vendor build\r\nbootstate=0\r\nprefix=/old\r\nkernel=/nested/b.b00\r\nkernelopt=runweasel ks=http://old/ks.cfg noList=1\r\nmodules=/z/vendor.v00 option=1 --- /a/module.v00 --- /z/other.v00\r\nvendor-extra=unchanged\r\n"
	original := r.ESXiBootConfig
	got, p, err := Render(r)
	if err != nil || len(p) != 0 {
		t.Fatalf("%v %v", p, err)
	}
	want := "# vendor build\r\nbootstate=0\r\nprefix=http://ng.internal/artifacts/esxi8\r\nkernel=nested/b.b00\r\nkernelopt=runweasel ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg noList=1\r\nmodules=z/vendor.v00 option=1 --- a/module.v00 --- z/other.v00\r\nvendor-extra=unchanged\r\n"
	if string(got) != want {
		t.Fatalf("lost original bootcfg data: %q; want %q", got, want)
	}
	if original != r.ESXiBootConfig {
		t.Fatal("mutated immutable config")
	}
	if strings.Contains(string(got), "ks="+"http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/boot.cfg") {
		t.Fatal("kickstart must use ks.cfg")
	}
}

func TestESXiMalformedOriginalIsContentProblem(t *testing.T) {
	malformed := []string{"", "   ", "kernel=b.b00\n", strings.Replace(originalESXiBootConfig, "modules=/jumpstrt.gz --- /useropts.gz --- /k.b00 --- /chain.b00\n", "", 1), originalESXiBootConfig + "kernel=/duplicate\n", strings.Replace(originalESXiBootConfig, "/chain.b00", "", 1), strings.Replace(originalESXiBootConfig, "/chain.b00", "/../escape", 1), strings.Replace(originalESXiBootConfig, "title=Loading", "malformed\ntitle=Loading", 1), originalESXiBootConfig + "bad=\x00\n"}
	for _, body := range malformed {
		r := menuFixture("esxi", "uefi")
		r.Kind = catalog.FileAux
		r.ESXiBootConfig = body
		assertRenderProblem(t, r, "render_esxi_bootcfg_invalid")
	}
	for _, key := range []catalog.SystemValue{"gateway", "artifacts", "task", "token"} {
		r := menuFixture("esxi", "uefi")
		r.Kind = catalog.FileAux
		r.ESXiBootConfig = originalESXiBootConfig
		delete(r.Values, key)
		assertRenderProblem(t, r, "render_system_value_missing")
	}
	r := menuFixture("esxi", "uefi")
	r.Kind = catalog.FileAux
	r.ESXiBootConfig = originalESXiBootConfig
	delete(r.Snapshot.ISO.Artifacts, "esxi-tree")
	assertRenderProblem(t, r, "render_artifact_missing")
}

// ESXi documents prefix and kernelopt as optional. A complete kernel/modules
// config is valid media; HTTP rebasing must supply omitted network settings.
func TestESXiOptionalPrefixAndKernelOptions(t *testing.T) {
	r := menuFixture("esxi", "uefi")
	r.Kind = catalog.FileAux
	r.ESXiBootConfig = "kernel=/nested/b.b00\r\nmodules=/z/vendor.v00 --- /a/module.v00"
	got, p, err := Render(r)
	want := "kernel=nested/b.b00\r\nmodules=z/vendor.v00 --- a/module.v00\r\nprefix=http://ng.internal/artifacts/esxi8\r\nkernelopt=ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg\r\n"
	if err != nil || len(p) != 0 || string(got) != want {
		t.Fatalf("optional fields: %q %v %v; want %q", got, p, err, want)
	}
}

func TestESXiAbsoluteModuleURLsRemainImmutable(t *testing.T) {
	for _, tc := range []struct {
		file string
		ok   bool
	}{
		{"http://ng.internal/artifacts/esxi8/nested/vendor.v00", true},
		{"https://other.internal/nested/vendor.v00", false},
		{"http://ng.internal/artifacts/esxi8/../outside.v00", false},
		{"http://ng.internal/artifacts/esxi8evil/vendor.v00", false},
		{"http://ng.internal/artifacts/esxi8/vendor.v00?version=1", false},
		{"http://ng.internal/artifacts/esxi8/vendor.v00#fragment", false},
		{"//nested/vendor.v00", false},
	} {
		t.Run(tc.file, func(t *testing.T) {
			r := menuFixture("esxi", "uefi")
			r.Kind = catalog.FileAux
			r.ESXiBootConfig = strings.Replace(originalESXiBootConfig, "/chain.b00", tc.file, 1)
			if !tc.ok {
				assertRenderProblem(t, r, "render_esxi_bootcfg_invalid")
				return
			}
			got, p, err := Render(r)
			if err != nil || len(p) != 0 || !strings.Contains(string(got), tc.file) {
				t.Fatalf("absolute module rewritten or rejected: %s %v %v", got, p, err)
			}
		})
	}
	r := menuFixture("esxi", "uefi")
	r.Kind = catalog.FileAux
	r.Values["artifacts"] = "https://ng.internal/artifacts"
	r.ESXiBootConfig = strings.Replace(originalESXiBootConfig, "/chain.b00", "https://ng.internal/artifacts/esxi8/nested/vendor.v00", 1)
	got, p, err := Render(r)
	if err != nil || len(p) != 0 || !strings.Contains(string(got), "https://ng.internal/artifacts/esxi8/nested/vendor.v00") {
		t.Fatalf("compatible https: %s %v %v", got, p, err)
	}
}

func TestESXiHTTPRebaseRemovesOnlyCDROMToken(t *testing.T) {
	r := menuFixture("esxi", "uefi")
	r.Kind = catalog.FileAux
	r.ESXiBootConfig = "prefix=\nkernel=/b.b00\nkernelopt=cdromBoot runweasel cdromBootExtra keep=1 ks=http://old/ks.cfg\nmodules=/a.v00 --- /b.v00\n"
	got, p, err := Render(r)
	want := "prefix=http://ng.internal/artifacts/esxi8\nkernel=b.b00\nkernelopt=runweasel cdromBootExtra keep=1 ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg\nmodules=a.v00 --- b.v00\n"
	if err != nil || len(p) != 0 || string(got) != want {
		t.Fatalf("cdromBoot rebasing: %q %v %v; want %q", got, p, err, want)
	}
}

func TestESXiRebasePreservesOriginalOptionAndPathSpacing(t *testing.T) {
	r := menuFixture("esxi", "uefi")
	r.Kind = catalog.FileAux
	r.ESXiBootConfig = "prefix=\nkernel= /nested/b.b00\nkernelopt=runweasel  keep=1\nmodules= /z/vendor.v00  option=1\t---\t /a/module.v00\n"
	got, p, err := Render(r)
	want := "prefix=http://ng.internal/artifacts/esxi8\nkernel= nested/b.b00\nkernelopt=runweasel  keep=1 ks=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/ks.cfg\nmodules= z/vendor.v00  option=1\t---\t a/module.v00\n"
	if err != nil || len(p) != 0 || string(got) != want {
		t.Fatalf("spacing changed: %q %v %v; want %q", got, p, err, want)
	}
}
