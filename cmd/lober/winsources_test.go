// 本文件锁住 fileWindowsSourcesResolver（imports.WindowsSourcesResolver
// 的生产实现）的行为。对应本轮 m2 轨任务：给 imports.Job.ConfigureWindows
// 提供生产可用的 WindowsSourcesResolver，依据 SPEC §0.3、§2.1、附录 A.4-
// A.6，internal/imports/extract_windows.go 与 internal/imports/job.go 的
// 文件末尾契约缺口说明。
//
// 目录约定（见 winsources.go 文件头的完整论证）：
//
//	<base>/wimboot                          共享，与固件/Windows版本无关
//	<base>/<artifact_set_id>/BCD            制品集专属
//	<base>/<artifact_set_id>/boot.sdi       制品集专属
//	<base>/<artifact_set_id>/boot.wim       制品集专属
package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// setupWindowsSourcesDir 在 t.TempDir() 下按目录约定建好一套完整的四个
// 文件，返回 base 目录与 artifactSetID，供各测试用例覆写某一个文件来
// 模拟缺失场景。
func setupWindowsSourcesDir(t *testing.T, artifactSetID string) (base string) {
	t.Helper()
	base = t.TempDir()

	if err := os.WriteFile(filepath.Join(base, "wimboot"), []byte("fake-wimboot"), 0o644); err != nil {
		t.Fatalf("准备 wimboot 失败: %v", err)
	}
	setDir := filepath.Join(base, artifactSetID)
	if err := os.MkdirAll(setDir, 0o755); err != nil {
		t.Fatalf("创建制品集子目录失败: %v", err)
	}
	for _, name := range []string{"BCD", "boot.sdi", "boot.wim"} {
		if err := os.WriteFile(filepath.Join(setDir, name), []byte("fake-"+name), 0o644); err != nil {
			t.Fatalf("准备 %s 失败: %v", name, err)
		}
	}
	return base
}

// TestFileWindowsSourcesResolver_四个文件都在时能解析出正确路径 是本轮
// 任务要求锁住的第一条行为：目录里四个文件齐全时，Resolve 返回的四个
// 路径必须逐一指向真实存在的文件，且与目录约定的位置一致。
func TestFileWindowsSourcesResolver_四个文件都在时能解析出正确路径(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	base := setupWindowsSourcesDir(t, artifactSetID)
	resolver := newFileWindowsSourcesResolver(base)

	got, err := resolver.ResolveWindowsSources(context.Background(), artifactSetID)
	if err != nil {
		t.Fatalf("ResolveWindowsSources 返回错误: %v；四个文件齐全时应成功解析", err)
	}

	wantWimboot := filepath.Join(base, "wimboot")
	wantBCD := filepath.Join(base, artifactSetID, "BCD")
	wantBootSDI := filepath.Join(base, artifactSetID, "boot.sdi")
	wantBootWIM := filepath.Join(base, artifactSetID, "boot.wim")

	if got.WimbootPath != wantWimboot {
		t.Errorf("WimbootPath = %q, want %q（wimboot 是共享文件，应在 base 目录下，不随 artifact_set_id 变化）",
			got.WimbootPath, wantWimboot)
	}
	if got.BCDPath != wantBCD {
		t.Errorf("BCDPath = %q, want %q", got.BCDPath, wantBCD)
	}
	if got.BootSDIPath != wantBootSDI {
		t.Errorf("BootSDIPath = %q, want %q", got.BootSDIPath, wantBootSDI)
	}
	if got.BootWIMPath != wantBootWIM {
		t.Errorf("BootWIMPath = %q, want %q", got.BootWIMPath, wantBootWIM)
	}

	for _, p := range []string{got.WimbootPath, got.BCDPath, got.BootSDIPath, got.BootWIMPath} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("Resolve 返回的路径 %q 应指向一个真实存在的文件: %v", p, err)
		}
	}
}

// TestFileWindowsSourcesResolver_两个固件各自的制品集互不冲突 锁住 SPEC
// §2.1 的核心场景：同一 Windows 版本按固件导两个 artifact_set 时，两次
// 解析必须各自拿到自己那一套 BCD/boot.sdi/boot.wim（制品集专属），但共享
// 同一个 wimboot（通用）——这正是本轮任务判断「wimboot 该放哪一层」的
// 依据。
func TestFileWindowsSourcesResolver_两个固件各自的制品集互不冲突(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "wimboot"), []byte("shared-wimboot"), 0o644); err != nil {
		t.Fatalf("准备 wimboot 失败: %v", err)
	}
	for _, id := range []string{"win2022-uefi", "win2022-bios"} {
		dir := filepath.Join(base, id)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("创建 %s 失败: %v", dir, err)
		}
		for _, name := range []string{"BCD", "boot.sdi", "boot.wim"} {
			content := id + "-" + name
			if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
				t.Fatalf("准备 %s/%s 失败: %v", id, name, err)
			}
		}
	}
	resolver := newFileWindowsSourcesResolver(base)

	uefi, err := resolver.ResolveWindowsSources(context.Background(), "win2022-uefi")
	if err != nil {
		t.Fatalf("解析 win2022-uefi 失败: %v", err)
	}
	bios, err := resolver.ResolveWindowsSources(context.Background(), "win2022-bios")
	if err != nil {
		t.Fatalf("解析 win2022-bios 失败: %v", err)
	}

	if uefi.WimbootPath != bios.WimbootPath {
		t.Errorf("两个固件各自的 WimbootPath 不同（uefi=%q, bios=%q）"+
			"，wimboot 是 iPXE 提供的通用引导器，与 Windows 版本/固件无关，两次解析应共享同一份文件",
			uefi.WimbootPath, bios.WimbootPath)
	}
	if uefi.BootWIMPath == bios.BootWIMPath {
		t.Errorf("两个固件各自的 BootWIMPath 相同（%q），boot.wim 是制品集专属的 WinPE 镜像，"+
			"uefi 与 bios 两个制品集必须各用各的，不能共享同一个文件", uefi.BootWIMPath)
	}
}

// ---------------------------------------------------------------------------
// 缺任一文件时报明确错误（指出缺的是哪个）
// ---------------------------------------------------------------------------

func TestFileWindowsSourcesResolver_缺wimboot时报错指出是wimboot(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	base := setupWindowsSourcesDir(t, artifactSetID)
	if err := os.Remove(filepath.Join(base, "wimboot")); err != nil {
		t.Fatalf("测试准备阶段删除 wimboot 失败: %v", err)
	}
	resolver := newFileWindowsSourcesResolver(base)

	_, err := resolver.ResolveWindowsSources(context.Background(), artifactSetID)
	if err == nil {
		t.Fatal("wimboot 缺失时 ResolveWindowsSources 应返回错误——" +
			"runWindows 需要据此让整个导入失败，不能假设一组默认路径继续")
	}
	if !strings.Contains(err.Error(), "wimboot") {
		t.Errorf("错误信息应明确指出缺失的是 wimboot，got: %v", err)
	}
}

func TestFileWindowsSourcesResolver_缺BCD时报错指出是BCD(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	base := setupWindowsSourcesDir(t, artifactSetID)
	if err := os.Remove(filepath.Join(base, artifactSetID, "BCD")); err != nil {
		t.Fatalf("测试准备阶段删除 BCD 失败: %v", err)
	}
	resolver := newFileWindowsSourcesResolver(base)

	_, err := resolver.ResolveWindowsSources(context.Background(), artifactSetID)
	if err == nil {
		t.Fatal("BCD 缺失时应返回错误")
	}
	if !strings.Contains(err.Error(), "BCD") {
		t.Errorf("错误信息应明确指出缺失的是 BCD，got: %v", err)
	}
}

func TestFileWindowsSourcesResolver_缺bootSDI时报错指出是bootSDI(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	base := setupWindowsSourcesDir(t, artifactSetID)
	if err := os.Remove(filepath.Join(base, artifactSetID, "boot.sdi")); err != nil {
		t.Fatalf("测试准备阶段删除 boot.sdi 失败: %v", err)
	}
	resolver := newFileWindowsSourcesResolver(base)

	_, err := resolver.ResolveWindowsSources(context.Background(), artifactSetID)
	if err == nil {
		t.Fatal("boot.sdi 缺失时应返回错误")
	}
	if !strings.Contains(err.Error(), "boot.sdi") {
		t.Errorf("错误信息应明确指出缺失的是 boot.sdi，got: %v", err)
	}
}

func TestFileWindowsSourcesResolver_缺bootWIM时报错指出是bootWIM(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	base := setupWindowsSourcesDir(t, artifactSetID)
	if err := os.Remove(filepath.Join(base, artifactSetID, "boot.wim")); err != nil {
		t.Fatalf("测试准备阶段删除 boot.wim 失败: %v", err)
	}
	resolver := newFileWindowsSourcesResolver(base)

	_, err := resolver.ResolveWindowsSources(context.Background(), artifactSetID)
	if err == nil {
		t.Fatal("boot.wim 缺失时应返回错误")
	}
	if !strings.Contains(err.Error(), "boot.wim") {
		t.Errorf("错误信息应明确指出缺失的是 boot.wim，got: %v", err)
	}
}

// TestFileWindowsSourcesResolver_制品集子目录整体不存在时报错 覆盖「运维
// 完全没有为这个 artifact_set 准备任何文件」的场景（不是缺单个文件，是
// 整层子目录都没建），错误信息应仍然可读，不panic。
func TestFileWindowsSourcesResolver_制品集子目录整体不存在时报错(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "wimboot"), []byte("x"), 0o644); err != nil {
		t.Fatalf("准备 wimboot 失败: %v", err)
	}
	resolver := newFileWindowsSourcesResolver(base)

	_, err := resolver.ResolveWindowsSources(context.Background(), "no-such-artifact-set")
	if err == nil {
		t.Fatal("制品集子目录不存在时应返回错误")
	}
}

// TestFileWindowsSourcesResolver_基目录为空字符串时报错 锁住「未配置
// store.windows_sources_path 却仍被调用」这条边界：base 为空字符串时，
// 不应拼出一个形如 "/BCD" 的误导性绝对路径，应直接明确拒绝。这与
// job.go runWindows 在 resolver 为 nil 时的拒绝是两层不同的防御——resolver
// 非 nil 但配置本身是空字符串的情况由本测试覆盖。
func TestFileWindowsSourcesResolver_基目录为空字符串时报错(t *testing.T) {
	resolver := newFileWindowsSourcesResolver("")

	_, err := resolver.ResolveWindowsSources(context.Background(), "win2022-uefi")
	if err == nil {
		t.Fatal("base 为空字符串时 ResolveWindowsSources 应返回错误" +
			"（未配置 store.windows_sources_path 却被调用，应明确报错而不是拼出误导性路径）")
	}
}

// TestFileWindowsSourcesResolver_artifactSetID为空字符串时报错 防止调用方
// 传入空 ID 时拼出 "<base>//BCD" 这种畸形路径。
func TestFileWindowsSourcesResolver_artifactSetID为空字符串时报错(t *testing.T) {
	base := setupWindowsSourcesDir(t, "irrelevant")
	resolver := newFileWindowsSourcesResolver(base)

	_, err := resolver.ResolveWindowsSources(context.Background(), "")
	if err == nil {
		t.Fatal("artifactSetID 为空字符串时应返回错误")
	}
}
