package iso

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func identifyTree(files map[string]string) fstest.MapFS {
	out := fstest.MapFS{}
	for name, data := range files {
		out[name] = &fstest.MapFile{Data: []byte(data)}
	}
	return out
}

func TestIdentifyFormatsAndSeries(t *testing.T) {
	for _, tc := range []struct {
		name, format, version string
		files                 map[string]string
	}{
		{"debian", "preseed", "13.6.0", map[string]string{".disk/info": `Debian GNU/Linux 13.6.0 "Trixie" - Official amd64 NETINST Binary-1 with firmware-1 20250803-13:06`, "dists/trixie/Release": ""}},
		{"ubuntu", "autoinstall", "24.04.3", map[string]string{".disk/info": `Ubuntu-Server 24.04.3 LTS "Noble Numbat" - Release amd64 (20250819)`, "dists/noble/Release": ""}},
		{"sles15", "autoyast", "15.6", map[string]string{"media.1/products": "SLES|15.6|SUSE Linux Enterprise Server", "repodata/repomd.xml": "", "boot/x86_64/loader/linux": ""}},
		{"leap15", "autoyast", "15.6", map[string]string{"media.1/products": "openSUSE|15.6|Leap", "repodata/repomd.xml": "", "boot/x86_64/loader/linux": ""}},
		{"sles16", "agama", "16.0", map[string]string{"media.1/products": "SLES|16.0|Server", "repodata/repomd.xml": "", "boot/x86_64/loader/linux": "", "LiveOS/squashfs.img": ""}},
		{"leap16", "agama", "16.0", map[string]string{"media.1/products": "openSUSE|16.0|Leap", "repodata/repomd.xml": "", "boot/x86_64/loader/linux": "", "LiveOS/squashfs.img": ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Identify(context.Background(), identifyTree(tc.files))
			if err != nil || string(got.Series) != tc.name || string(got.Format) != tc.format || got.Version != tc.version || got.Arch != "x86_64" {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
	for _, tc := range []struct{ name, product string }{{"rhel", "Red Hat Enterprise Linux"}, {"rocky", "Rocky Linux"}, {"almalinux", "AlmaLinux"}, {"openeuler", "openEuler"}, {"opencloudos", "OpenCloudOS"}} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Identify(context.Background(), identifyTree(map[string]string{".treeinfo": "[other]\nversion=1\n[general]\nname=" + tc.product + "\nversion=9.6\narch=aarch64\n", "media.1/products": "SLES|15.6", "repodata/repomd.xml": ""}))
			if err != nil || got.Series != catalog.Series(tc.name) || got.Format != "kickstart" || got.Version != "9.6" || got.Arch != "aarch64" {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func identifyCode(t *testing.T, err error, code model.Code) {
	t.Helper()
	var p model.Problem
	if !errors.As(err, &p) || p.Code != code {
		t.Fatalf("expected %s, got %v", code, err)
	}
}

func TestIdentifyFailures(t *testing.T) {
	for _, tc := range []struct {
		name  string
		files map[string]string
		code  model.Code
	}{
		{"unknown", map[string]string{"unknown": ""}, "iso_media_unrecognized"},
		{"unknown-series", map[string]string{".treeinfo": "[general]\nname=CustomOS\nversion=1\narch=x86_64"}, "iso_series_unrecognized"},
		{"name-outside-general", map[string]string{".treeinfo": "[other]\nname=Rocky Linux\n[general]\nname=CustomOS\nversion=1\narch=x86_64"}, "iso_series_unrecognized"},
		{"unsafe-version", map[string]string{".treeinfo": "[general]\nname=Rocky Linux\nversion=../bad\narch=x86_64"}, "iso_version_missing"},
		{"unknown-arch", map[string]string{".treeinfo": "[general]\nname=Rocky Linux\nversion=9.6\narch=ppc64le"}, "iso_version_missing"},
		{"esxi-version-unresolved", map[string]string{"boot.cfg": "title=Loading ESXi installer 8.0", "efi/boot/bootx64.efi": ""}, "iso_version_missing"},
		{"suse-without-loader", map[string]string{"media.1/products": "SLES|15.6", "repodata/repomd.xml": ""}, "iso_version_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Identify(context.Background(), identifyTree(tc.files))
			identifyCode(t, err, tc.code)
			if tc.name == "unknown-series" && !strings.Contains(err.Error(), "kickstart") {
				t.Fatalf("format missing: %+v, %v", got, err)
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Identify(ctx, fstest.MapFS{}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestIdentifyWindowsEditions(t *testing.T) {
	for _, tc := range []struct{ flag, name, arch, series string }{{"ServerDatacenter", "Windows Server 2025", "9", "windows-server"}, {"Professional", "Windows 11 Pro", "12", "windows-desktop"}, {"Core", "Windows 10 Home", "9", "windows-desktop"}} {
		t.Run(tc.series+tc.arch, func(t *testing.T) {
			xml := `<WIM><IMAGE INDEX="1"><NAME>` + tc.name + `</NAME><FLAGS>` + tc.flag + `</FLAGS><WINDOWS><ARCH>` + tc.arch + `</ARCH><VERSION><MAJOR>10</MAJOR><MINOR>0</MINOR><BUILD>26100</BUILD></VERSION></WINDOWS></IMAGE></WIM>`
			got, err := Identify(context.Background(), fstest.MapFS{"sources/install.wim": &fstest.MapFile{Data: makeWIM([]byte(xml), 0, false, 32768, len(xml))}})
			wantArch := catalog.Arch("x86_64")
			if tc.arch == "12" {
				wantArch = "aarch64"
			}
			if err != nil || string(got.Series) != tc.series || got.Format != "unattend" || got.Version != "10.0" || got.Arch != wantArch || len(got.Editions) != 1 || got.Editions[0] != (model.Option{Value: tc.flag, Label: tc.name}) {
				t.Fatalf("got %+v, %v", got, err)
			}
		})
	}
}

func TestIdentifyWIMRejects32BitArchitecture(t *testing.T) {
	xml := `<WIM><IMAGE INDEX="1"><NAME>Windows 10 Home x86</NAME><FLAGS>Core</FLAGS><WINDOWS><ARCH>0</ARCH><VERSION><MAJOR>10</MAJOR><MINOR>0</MINOR></VERSION></WINDOWS></IMAGE></WIM>`
	b := makeWIM([]byte(xml), 0, false, 32768, len(xml))
	images, err := WIMImages(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(images) != 1 || images[0].Arch != "" {
		t.Fatalf("32-bit WIM incorrectly mapped: %+v, %v", images, err)
	}
	_, err = Identify(context.Background(), fstest.MapFS{"sources/install.wim": &fstest.MapFile{Data: b}})
	identifyCode(t, err, "iso_version_missing")
	var p model.Problem
	if !errors.As(err, &p) || p.Subject.Path != "arch" || !strings.Contains(p.Message, model.ISOArchLabel) {
		t.Fatalf("wrong unsupported architecture subject: %v", err)
	}
}

type identifyBrokenFS struct{}

func (identifyBrokenFS) Open(string) (fs.File, error) { return nil, fs.ErrPermission }

func TestIdentifyDoesNotMaskIOErrors(t *testing.T) {
	_, err := Identify(context.Background(), identifyBrokenFS{})
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("got %v", err)
	}
}

func TestIdentifyWindowsRejectsIncompleteImageAndUnsafeOffers(t *testing.T) {
	image := func(index, meta, flags string) string {
		return `<IMAGE INDEX="` + index + `"><NAME>Windows Pro</NAME><FLAGS>` + flags + `</FLAGS>` + meta + `</IMAGE>`
	}
	meta := `<WINDOWS><ARCH>9</ARCH><VERSION><MAJOR>10</MAJOR><MINOR>0</MINOR></VERSION></WINDOWS>`
	for _, xml := range []string{
		`<WIM>` + image("1", "", "Core") + image("2", meta, "Professional") + `</WIM>`,
		`<WIM>` + image("1", meta, "not safe") + `</WIM>`,
		`<WIM>` + image("1", strings.Replace(meta, "<ARCH>9", "<ARCH>6", 1), "Core") + `</WIM>`,
	} {
		b := makeWIM([]byte(xml), 0, false, 32768, len(xml))
		_, err := Identify(context.Background(), fstest.MapFS{"sources/install.wim": &fstest.MapFile{Data: b}})
		identifyCode(t, err, "iso_version_missing")
	}
	xml := `<WIM>` + image("1", meta, "Core") + image("2", meta, "Core") + `</WIM>`
	b := makeWIM([]byte(xml), 0, false, 32768, len(xml))
	got, err := Identify(context.Background(), fstest.MapFS{"sources/install.wim": &fstest.MapFile{Data: b}})
	if err != nil || len(got.Editions) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
}

func TestIdentifyProductsAndDiskArmArchitecture(t *testing.T) {
	for _, files := range []map[string]string{
		{".disk/info": "Ubuntu-Server 24.04.3 LTS - Release arm64 (20250819)"},
		{"media.1/products": "SLES|15.6|Server", "repodata/repomd.xml": "", "boot/aarch64/loader/linux": ""},
	} {
		got, err := Identify(context.Background(), identifyTree(files))
		if err != nil || got.Arch != "aarch64" {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
}

type identifyCancellingReader struct{ cancel context.CancelFunc }

func (r identifyCancellingReader) Read(b []byte) (int, error) { r.cancel(); return len(b), nil }

func TestIdentifyCancellationDuringWIMSeek(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := identifyForwardReader{ctx: ctx, r: identifyCancellingReader{cancel: cancel}}
	_, err := r.ReadAt(make([]byte, 208), 4096)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("got %v", err)
	}
}
