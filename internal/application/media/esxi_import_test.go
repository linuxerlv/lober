package media_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

func TestImportESXiReadyAndBootConfiguration(t *testing.T) {
	files := map[string][]byte{
		"boot.cfg":             []byte("title=Loading ESXi installer\nbuild=8.0.3-0.102.25595708\nprefix=\nkernel=/b.b00\nmodules=/jumpstrt.gz --- /driver.v00\nkernelopt=runweasel cdromBoot\n"),
		"efi/boot/bootx64.efi": []byte("loader"), "b.b00": []byte("kernel"),
		"jumpstrt.gz": []byte("module"), "driver.v00": []byte("driver"),
	}
	name := filepath.Join(t.TempDir(), "installer.iso")
	if err := os.WriteFile(name, isotest.Image(files), 0600); err != nil {
		t.Fatal(err)
	}
	checkESXiImport(t, name, "8.0.3-0.102.25595708")
}

func TestImportESXiInvalidBootFilesCannotBecomeReady(t *testing.T) {
	for _, tc := range []struct {
		name, config string
		code         model.Code
	}{
		{"missing-modules", "kernel=/b.b00\n", "iso_artifact_missing"},
		{"traversal", "kernel=/b.b00\nmodules=/../driver.v00\n", "iso_artifact_missing"},
		{"external-module", "kernel=/b.b00\nmodules=https://example.invalid/driver.v00\n", "iso_artifact_missing"},
		{"duplicate-kernel", "kernel=/b.b00\nkernel=/b.b00\nmodules=/driver.v00\n", "iso_version_missing"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, db := importSetup(t)
			item, err := db.CreateISO(t.Context(), model.ISO{Name: "ESXi 无效启动配置", Family: "esxi", Series: "esxi"}, importAudit())
			if err != nil {
				t.Fatal(err)
			}
			original := isotest.Image(map[string][]byte{
				"boot.cfg":             []byte("build=8.0.3-0.102.25595708\n" + tc.config),
				"efi/boot/bootx64.efi": []byte("loader"), "b.b00": []byte("kernel"), "driver.v00": []byte("driver"),
			})
			importUpload(t, s, item, "", original)
			err = s.Import(t.Context(), item.ID)
			var problem model.Problem
			// 重复设置在识别阶段就会被拒绝，其余情况在制订提取计划时拒绝。
			if !errors.As(err, &problem) || problem.Code != tc.code {
				t.Fatalf("应拒绝无效配置，实际：%v", err)
			}
			got, err := db.GetISO(t.Context(), item.ID)
			if err != nil || got.State != model.ISOFailed || len(got.Artifacts) != 0 {
				t.Fatalf("无效镜像不应就绪或发布文件：%+v，%v", got, err)
			}
		})
	}
}

// 真实镜像只从本地读取，不下载、不修改，也不放入仓库。
func TestImportRealESXi(t *testing.T) {
	for _, tc := range []struct{ name, env, version string }{
		{"7", "LOBER_TEST_ESXI7_ISO", "7.0.3-0.135.24585291"},
		{"8", "LOBER_TEST_ESXI8_ISO", "8.0.3-0.102.25595708"},
		{"9", "LOBER_TEST_ESXI9_ISO", "9.0.2-0100.25595025"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			name := os.Getenv(tc.env)
			if name == "" {
				t.Skip(tc.env + " 未设置，未执行真实镜像验证")
			}
			checkESXiImport(t, name, tc.version)
		})
	}
}

func checkESXiImport(t *testing.T, name, version string) {
	t.Helper()
	f, err := os.Open(name)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	h := sha256.New()
	size, err := io.Copy(h, f)
	if err != nil {
		t.Fatal(err)
	}
	digest := hex.EncodeToString(h.Sum(nil))
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		t.Fatal(err)
	}
	s, db := importSetup(t)
	item, err := db.CreateISO(t.Context(), model.ISO{Name: "ESXi 测试镜像", Family: "esxi", Series: "esxi"}, importAudit())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Register(t.Context(), item, "upload", "", "", size, digest, importAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Upload(t.Context(), item.ID, "", 0, f, importAudit()); err != nil {
		t.Fatal(err)
	}
	if err := s.Import(t.Context(), item.ID); err != nil {
		t.Fatal(err)
	}
	got, err := db.GetISO(t.Context(), item.ID)
	if err != nil || got.State != model.ISOReady || got.Version != version || got.Arch != "x86_64" || got.SHA256 != digest || len(got.Validate()) != 0 {
		t.Fatalf("镜像没有正确就绪：%+v，%v", got, err)
	}
	snap := model.SnapshotISO{ID: got.ID, Arch: got.Arch, Artifacts: got.Artifacts}
	raw, err := iso.NewBootConfigReader(s.dirs.Artifacts).ReadESXiBootConfig(t.Context(), snap)
	if err != nil {
		t.Fatal(err)
	}
	files, ok := mediarules.ESXiBootFiles(raw)
	if !ok {
		t.Fatal("导入后的原始配置无效")
	}
	for _, name := range files {
		st, err := os.Stat(filepath.Join(s.dirs.Artifacts, got.Artifacts["esxi-tree"], filepath.FromSlash(name)))
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			t.Fatalf("安装文件缺失：%s，%v", name, err)
		}
	}
	r := render.Request{Kind: catalog.FileMenu, Firmware: "uefi", ESXiBootConfig: raw,
		Values:   map[catalog.SystemValue]string{"gateway": "http://127.0.0.1:58080", "artifacts": "http://127.0.0.1:58080/artifacts", "task": "42", "token": "install-tok-fixed-0000000000000000"},
		Snapshot: model.Snapshot{Schema: model.SnapshotSchema, Family: "esxi", Series: "esxi", Format: "esxi-ks", ISO: snap},
	}
	menu, problems, err := render.Render(r)
	if err != nil || len(problems) != 0 || !strings.Contains(string(menu), got.Artifacts["mboot"]) || !strings.Contains(string(menu), "/boot.cfg") {
		t.Fatalf("启动菜单：%s，%v，%v", menu, problems, err)
	}
	r.Kind = catalog.FileAux
	config, problems, err := render.Render(r)
	if err != nil || len(problems) != 0 || !strings.Contains(string(config), "ks=http://127.0.0.1:58080/") || strings.Contains(string(config), "cdromBoot") || !strings.Contains(string(config), "build="+version) {
		t.Fatalf("网络启动配置：%s，%v，%v", config, problems, err)
	}
	t.Logf("version=%s sha256=%s bytes=%d modules=%d；导入、数据库就绪、启动菜单和配置均通过；未执行客户机安装", version, digest, size, len(files)-1)
}
