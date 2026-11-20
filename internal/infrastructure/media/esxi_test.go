package iso

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/lober/lober/internal/application/media/port"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
)

func esxiTree() fstest.MapFS {
	return fstest.MapFS{
		"boot.cfg":             {Data: []byte("build=8.0.3-0.102.25595708\nprefix=\nkernel=/b.b00\nkernelopt=runweasel cdromBoot\nmodules=/jumpstrt.gz --- /nested/driver.v00 arg=1\n")},
		"efi/boot/bootx64.efi": {Data: []byte("loader")},
		"b.b00":                {Data: []byte("kernel")},
		"jumpstrt.gz":          {Data: []byte("module")},
		"nested/driver.v00":    {Data: []byte("driver")},
	}
}

func TestExtractESXiPreservesMediaAndPublishesBootRoles(t *testing.T) {
	tree, dest := esxiTree(), t.TempDir()
	roles, err := NewImportFiles(Dirs{}).extract(t.Context(), port.ExtractionPlan{Identified: mediarules.Identified{Format: "esxi-ks"}, ESXiFiles: []string{"b.b00", "jumpstrt.gz", "nested/driver.v00"}}, tree, nil, nil, dest)
	if err != nil {
		t.Fatal(err)
	}
	if roles["mboot"] != "install/efi/boot/bootx64.efi" || roles["esxi-tree"] != "install/" {
		t.Fatalf("启动文件地址不正确：%v", roles)
	}
	for name, original := range tree {
		got, err := os.ReadFile(filepath.Join(dest, "install", filepath.FromSlash(name)))
		if err != nil || string(got) != string(original.Data) {
			t.Fatalf("文件被丢失或改写：%s，%v", name, err)
		}
	}
}

func TestExtractESXiRejectsIncompleteAndUnsafeMedia(t *testing.T) {
	for _, name := range []string{"missing-kernel", "empty-module", "missing-loader", "traversal", "remote-module", "missing-plan"} {
		t.Run(name, func(t *testing.T) {
			tree := esxiTree()
			files := []string{"b.b00", "jumpstrt.gz", "nested/driver.v00"}
			switch name {
			case "missing-kernel":
				delete(tree, "b.b00")
			case "empty-module":
				tree["nested/driver.v00"].Data = nil
			case "missing-loader":
				delete(tree, "efi/boot/bootx64.efi")
			case "traversal":
				files = []string{"../b.b00"}
			case "remote-module":
				files = []string{"https://example.invalid/module"}
			case "missing-plan":
				files = nil
			}
			_, err := NewImportFiles(Dirs{}).extract(t.Context(), port.ExtractionPlan{Identified: mediarules.Identified{Format: "esxi-ks"}, ESXiFiles: files}, tree, nil, nil, t.TempDir())
			var fault *port.ExtractionFault
			if !errors.As(err, &fault) || fault.Kind != "artifact" {
				t.Fatalf("应拒绝不完整或不安全的镜像，实际：%v", err)
			}
		})
	}
}

func TestESXiBootConfigReadsPublishedTreeRole(t *testing.T) {
	base := t.TempDir()
	if err := os.MkdirAll(filepath.Join(base, "install"), 0755); err != nil {
		t.Fatal(err)
	}
	raw := esxiTree()["boot.cfg"].Data
	if err := os.WriteFile(filepath.Join(base, "install", "boot.cfg"), raw, 0644); err != nil {
		t.Fatal(err)
	}
	got, err := NewBootConfigReader(base).ReadESXiBootConfig(t.Context(), model.SnapshotISO{Artifacts: map[string]string{"esxi-tree": "install/"}})
	if err != nil || got != string(raw) {
		t.Fatalf("读取导入后的配置：%q，%v", got, err)
	}
}
