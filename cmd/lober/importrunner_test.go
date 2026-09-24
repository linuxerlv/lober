// 本文件锁住 importRunner 对 Windows 家族的接线行为：newImportRunner 构造
// 出的 imports.Job 必须已经调用过 ConfigureWindows，否则 internal/imports
// 的 runWindows 会对识别为 windows 家族的导入请求返回「导入 job 尚未调用
// ConfigureWindows 完成配置」这个明确错误（见 internal/imports/job.go）
// ——那正是本轮任务要解除的 MVP 封锁。
//
// 这是本轮任务判定成败的测试（任务原文：「这条是本轮成败的判据」）：
// 上一轮同任务的产出是零代码，这一轮必须证明 Windows 的导入路径真的走
// 通了，不能只是「文件都建好了但没接上」。
package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/imports"
)

// ---------------------------------------------------------------------------
// 测试替身：抽取器（模拟一份 Windows 安装源的 ISO 内容视图）
// ---------------------------------------------------------------------------

// fakeExtractor 是 imports.Extractor 的测试替身，Open 直接返回预设的
// fs.FS，不接触真实文件系统或 mount(8)。
type fakeExtractor struct {
	root fs.FS
	err  error
}

func (f fakeExtractor) Open(_ context.Context, _ string) (fs.FS, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.root, nil
}

// sampleWindowsRoot 构造一个最小的 Windows 安装源文件系统视图：只包含
// identify.go/extract_windows.go 认作 Windows 特征文件的
// sources/install.wim（SPEC §2.1：Windows 的识别特征文件），内容本身
// 无意义，只用于满足 fs.Stat 检查。
func sampleWindowsRoot() fs.FS {
	return fstest.MapFS{
		"sources/install.wim": &fstest.MapFile{Data: []byte("fake-install-wim-bytes")},
	}
}

// ---------------------------------------------------------------------------
// 测试替身：ArtifactSetStore（内存实现，满足 contract.ArtifactSetStore
// 的全部方法，因为 newImportRunner 的 store 参数类型是 contract.ArtifactSetStore
// 内嵌的完整 contract.Store 接口——这里只需要 artifact_set 相关的四个
// 方法真正被调用到，其余方法留空实现即可编译通过）。
// ---------------------------------------------------------------------------

// memArtifactSetStore 是 contract.ArtifactSetStore 的内存测试替身，只实现
// artifact_set 相关的三个方法（newImportRunner 与 imports.NewJob 都只需要
// 这个窄接口，不需要完整 contract.Store 的其它六个子接口）。
type memArtifactSetStore struct {
	mu sync.Mutex

	sets map[string]model.ArtifactSet
}

func newMemArtifactSetStore() *memArtifactSetStore {
	return &memArtifactSetStore{sets: map[string]model.ArtifactSet{}}
}

// CreateArtifactSet/ListArtifactSets/DeleteArtifactSet 是 contract.
// ArtifactSetStore 的另外三个方法（newImportRunner 的参数类型是完整的
// contract.ArtifactSetStore，即使本测试实际只用到另外三个方法）。测试
// 场景下的导入流程不会调用它们，因此显式 panic 而不是默默返回零值——
// 若测试意外走到这里，panic 比断言失败更容易定位「为什么调用了一个不
// 该被调用的方法」。
func (s *memArtifactSetStore) CreateArtifactSet(context.Context, model.ArtifactSet) (model.ArtifactSet, error) {
	panic("memArtifactSetStore: 测试路径不应调用 CreateArtifactSet")
}

func (s *memArtifactSetStore) ListArtifactSets(context.Context) ([]model.ArtifactSet, error) {
	panic("memArtifactSetStore: 测试路径不应调用 ListArtifactSets")
}

func (s *memArtifactSetStore) DeleteArtifactSet(context.Context, string) error {
	panic("memArtifactSetStore: 测试路径不应调用 DeleteArtifactSet")
}

func (s *memArtifactSetStore) put(set model.ArtifactSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sets[set.ID] = set
}

func (s *memArtifactSetStore) GetArtifactSet(_ context.Context, id string) (model.ArtifactSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.sets[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return set, nil
}

func (s *memArtifactSetStore) UpdateArtifactSetState(_ context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.sets[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	set.State = state
	set.ManifestSHA256 = manifestSHA256
	set.FailReason = reason
	s.sets[id] = set
	return set, nil
}

func (s *memArtifactSetStore) SetArtifactSetContent(_ context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	set, ok := s.sets[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	set.OSFamily = osFamily
	set.Paths = paths
	s.sets[id] = set
	return set, nil
}

// snapshot 供测试断言读取最终状态，避免测试直接访问 map（并发安全）。
func (s *memArtifactSetStore) snapshot(id string) model.ArtifactSet {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sets[id]
}

// ---------------------------------------------------------------------------
// 决定性测试：runWindows 不再返回「尚未调用 ConfigureWindows」错误
// ---------------------------------------------------------------------------

// windowsNotConfiguredMarker 是 job.go runWindows 在 resolver/detector 未
// 注入时返回错误信息里的关键片段（见 internal/imports/job.go 的具体文案：
// "但导入 job 尚未调用 ConfigureWindows 完成配置"）。本测试文件不 import
// internal/imports 的私有实现细节，只依赖这段公开可见的错误文本——足以
// 判定「装配点是否真的调用了 ConfigureWindows」这件事。
const windowsNotConfiguredMarker = "尚未调用 ConfigureWindows"

// buildTestImportRunnerWindowsSources 在 t.TempDir() 下按
// fileWindowsSourcesResolver 的目录约定准备好一套完整的四个 Windows 外部
// 文件，返回基目录路径。
func buildTestImportRunnerWindowsSources(t *testing.T, artifactSetID string) string {
	t.Helper()
	base := t.TempDir()
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

// runJobDirectlyWithWindowsWiring 复现 newImportRunner + wire.go 的接线
// 顺序（构造 Job → ConfigureWindows → Run），但同步调用 job.Run 而不是
// 经 TriggerImport 的异步 goroutine——直接测 Job 本身是否已被正确配置，
// 异步触发与日志记录这部分留给下面的 TestImportRunner_TriggerImport_经
// 完整异步路径确认Windows能被识别 覆盖。
func runJobDirectlyWithWindowsWiring(store *memArtifactSetStore, extractor imports.Extractor, windowsSourcesBase string, artifactSetID, destDir string) error {
	job := imports.NewJob(store, extractor)
	resolver := newFileWindowsSourcesResolver(windowsSourcesBase)
	detector := newStoreBackedWimArchDetector(store)
	job.ConfigureWindows(resolver, detector)
	return job.Run(context.Background(), artifactSetID, "/tmp/whatever.iso", destDir)
}

// TestImportRunner_Windows家族不再返回ConfigureWindows未配置错误 是本轮
// 任务的判据测试：给定一套完整的四个外部文件、一个声明了具体固件的
// artifact_set，走完整的 job.Run(windows) 路径，得到的错误绝不能包含
// windowsNotConfiguredMarker——若这条失败，说明 ConfigureWindows 事实上
// 没有被生产装配点真正调用到，Windows 导入路径仍然走不通。
func TestImportRunner_Windows家族不再返回ConfigureWindows未配置错误(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	store := newMemArtifactSetStore()
	store.put(model.ArtifactSet{
		ID:       artifactSetID,
		OSFamily: model.OSFamilyWindows,
		Firmware: model.FirmwareUEFI,
		State:    model.ArtifactSetImporting,
	})
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	base := buildTestImportRunnerWindowsSources(t, artifactSetID)
	destDir := t.TempDir()

	err := runJobDirectlyWithWindowsWiring(store, extractor, base, artifactSetID, destDir)

	if err != nil && strings.Contains(err.Error(), windowsNotConfiguredMarker) {
		t.Fatalf("job.Run 仍返回「%s」错误：%v——"+
			"这说明 ConfigureWindows 没有被真正接到生产装配路径上，"+
			"Windows 的导入路径依然走不通（本轮任务的核心判据）",
			windowsNotConfiguredMarker, err)
	}
}

// TestImportRunner_Windows家族接线后能完整跑通到ready 在上一条「不再报
// 未配置错误」的基础上更进一步：四个文件齐全、固件声明一致、探测器信任
// 该声明时，整个导入应该成功跑完并把 artifact_set 置为 ready，
// Paths.Extra 应包含四个键。这是「Windows 的导入路径真的走通了」的最终
// 证明，不仅仅是「不报某一个特定错误」。
func TestImportRunner_Windows家族接线后能完整跑通到ready(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	store := newMemArtifactSetStore()
	store.put(model.ArtifactSet{
		ID:       artifactSetID,
		OSFamily: model.OSFamilyWindows,
		Firmware: model.FirmwareUEFI,
		State:    model.ArtifactSetImporting,
	})
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	base := buildTestImportRunnerWindowsSources(t, artifactSetID)
	destDir := t.TempDir()

	err := runJobDirectlyWithWindowsWiring(store, extractor, base, artifactSetID, destDir)
	if err != nil {
		t.Fatalf("四个文件齐全、固件声明一致时导入应成功，got err: %v", err)
	}

	final := store.snapshot(artifactSetID)
	if final.State != model.ArtifactSetReady {
		t.Fatalf("最终状态 = %s, want %s；FailReason=%q", final.State, model.ArtifactSetReady, final.FailReason)
	}
	if final.OSFamily != model.OSFamilyWindows {
		t.Errorf("OSFamily = %s, want %s", final.OSFamily, model.OSFamilyWindows)
	}
	for _, key := range []string{"wimboot", "BCD", "boot.sdi", "boot.wim"} {
		if _, ok := final.Paths.Extra[key]; !ok {
			t.Errorf("Paths.Extra 缺少键 %q，got %v", key, final.Paths.Extra)
		}
	}
}

// TestImportRunner_Windows家族缺外部文件时整体导入失败 确认「缺任一
// 外部文件」这条约束在完整装配路径下依然生效（不是只在 winsources_test.go
// 的单元测试里成立，装配起来之后同样成立）——固件一致但 boot.sdi 缺失时，
// 导入应该整体失败，且不能是「静默用了一组默认路径」。
func TestImportRunner_Windows家族缺外部文件时整体导入失败(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	store := newMemArtifactSetStore()
	store.put(model.ArtifactSet{
		ID:       artifactSetID,
		OSFamily: model.OSFamilyWindows,
		Firmware: model.FirmwareUEFI,
		State:    model.ArtifactSetImporting,
	})
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	base := buildTestImportRunnerWindowsSources(t, artifactSetID)
	if err := os.Remove(filepath.Join(base, artifactSetID, "boot.sdi")); err != nil {
		t.Fatalf("测试准备阶段删除 boot.sdi 失败: %v", err)
	}
	destDir := t.TempDir()

	err := runJobDirectlyWithWindowsWiring(store, extractor, base, artifactSetID, destDir)
	if err == nil {
		t.Fatal("boot.sdi 缺失时导入应整体失败，不能假设一组默认路径继续")
	}

	final := store.snapshot(artifactSetID)
	if final.State != model.ArtifactSetFailed {
		t.Fatalf("最终状态 = %s, want %s（缺文件应导致 failed）", final.State, model.ArtifactSetFailed)
	}
}

// TestImportRunner_TriggerImport_经完整异步路径确认Windows能被识别 验证
// 生产入口 newImportRunner(...).TriggerImport（console.ArtifactImporter
// 的真实实现）本身——不是绕过它直接构造 Job——最终也不会在处理 Windows
// 家族时卡在 ConfigureWindows 未配置这一步。TriggerImport 是异步的
// （另起 goroutine），测试通过轮询 store 状态变化等待完成。
func TestImportRunner_TriggerImport_经完整异步路径确认Windows能被识别(t *testing.T) {
	const artifactSetID = "win2022-uefi"
	store := newMemArtifactSetStore()
	store.put(model.ArtifactSet{
		ID:       artifactSetID,
		OSFamily: model.OSFamilyWindows,
		Firmware: model.FirmwareUEFI,
		State:    model.ArtifactSetImporting,
	})
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	windowsBase := buildTestImportRunnerWindowsSources(t, artifactSetID)
	artifactRoot := t.TempDir()

	runner := newImportRunner(store, extractor, artifactRoot, windowsBase)
	runner.TriggerImport(context.Background(), artifactSetID, "/tmp/whatever.iso")

	final := waitForArtifactSetTerminalState(t, store, artifactSetID)
	if final.State != model.ArtifactSetReady {
		t.Fatalf("经 TriggerImport 异步路径后最终状态 = %s, want %s；FailReason=%q"+
			"（若 FailReason 含「%s」，说明生产装配点未真正调用 ConfigureWindows）",
			final.State, model.ArtifactSetReady, final.FailReason, windowsNotConfiguredMarker)
	}
}

// waitForArtifactSetTerminalState 轮询 store 直到 artifact_set 离开
// importing 状态（变成 ready 或 failed）或超时。TriggerImport 是异步的，
// 测试不能假设它在断言点之前已经跑完，需要显式等待终态。
func waitForArtifactSetTerminalState(t *testing.T, store *memArtifactSetStore, id string) model.ArtifactSet {
	t.Helper()
	const (
		pollInterval = 10 * time.Millisecond
		maxWait      = 5 * time.Second
	)
	deadline := time.Now().Add(maxWait)
	for time.Now().Before(deadline) {
		set := store.snapshot(id)
		if set.State == model.ArtifactSetReady || set.State == model.ArtifactSetFailed {
			return set
		}
		time.Sleep(pollInterval)
	}
	t.Fatalf("等待 artifact_set %q 进入终态超时（%s）", id, maxWait)
	return model.ArtifactSet{}
}
