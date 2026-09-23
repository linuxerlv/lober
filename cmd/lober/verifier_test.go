// 本文件测试 artifactVerifier：重算 manifest 校验完整性的生产实现。对应
// 本轮任务「测试要锁住」的行为，依据 SPEC §8.2、§10 运维点一。
package main

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/imports"
)

// fakeVerifierStore 是 artifactVerifierStore 的可控测试替身：内存里存一
// 份制品集记录，记录每次 UpdateArtifactSetState 调用的参数——不需要真的
// 搭 sqlite.Store 或 fake.Store 的完整 contract.ArtifactSetStore（那要求
// 实现七个本类型完全不需要的方法）。
type fakeVerifierStore struct {
	mu sync.Mutex

	sets map[string]model.ArtifactSet

	// updateCalls 记录每次 UpdateArtifactSetState 被调用时的参数，供测试
	// 断言「校验通过时确实刷新了 ManifestSHA256，且 State 没被改变」、
	// 「校验失败时完全没有调用这个方法」。
	updateCalls []fakeUpdateCall
}

type fakeUpdateCall struct {
	ID             string
	State          model.ArtifactSetState
	ManifestSHA256 string
}

func newFakeVerifierStore(sets ...model.ArtifactSet) *fakeVerifierStore {
	m := make(map[string]model.ArtifactSet, len(sets))
	for _, s := range sets {
		m[s.ID] = s
	}
	return &fakeVerifierStore{sets: m}
}

func (f *fakeVerifierStore) GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sets[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return s, nil
}

func (f *fakeVerifierStore) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256 string) (model.ArtifactSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.sets[id]
	if !ok {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	s.State = state
	s.ManifestSHA256 = manifestSHA256
	f.sets[id] = s
	f.updateCalls = append(f.updateCalls, fakeUpdateCall{ID: id, State: state, ManifestSHA256: manifestSHA256})
	return s, nil
}

func (f *fakeVerifierStore) getUpdateCalls() []fakeUpdateCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]fakeUpdateCall, len(f.updateCalls))
	copy(out, f.updateCalls)
	return out
}

// writeFakeArtifactTree 在 root/artifactSetID 下造一棵与 ExtractRHEL 产出
// 布局一致的假制品树（vmlinuz、initrd.img、repo/repodata/repomd.xml），
// 返回对应的 model.ArtifactPaths 与该树此刻的 manifest sha256（供测试
// 把 ArtifactSet.ManifestSHA256 设成「与磁盘内容匹配的值」这一校验通过
// 的前提条件）。
func writeFakeArtifactTree(t *testing.T, root, artifactSetID string) (model.ArtifactPaths, string) {
	t.Helper()
	destDir := filepath.Join(root, artifactSetID)

	files := map[string]string{
		"vmlinuz":                  "fake-kernel-content",
		"initrd.img":               "fake-initrd-content",
		"repo/repodata/repomd.xml": "<repomd/>",
		"repo/Packages/a.rpm":      "fake-rpm-content",
	}
	for rel, content := range files {
		full := filepath.Join(destDir, rel)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("创建目录失败 %q: %v", filepath.Dir(full), err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatalf("写文件失败 %q: %v", full, err)
		}
	}

	manifest, err := imports.GenerateManifest(destDir)
	if err != nil {
		t.Fatalf("生成基准 manifest 失败: %v", err)
	}

	paths := model.ArtifactPaths{
		Kernel: "vmlinuz",
		Initrd: []string{"initrd.img"},
		Repo:   "repo/",
	}
	return paths, manifest.SHA256
}

// TestArtifactVerifier_RunVerify_PassesWhenTreeMatchesManifest 锁住
// 「校验通过时：制品集状态与 manifest 不变」——刷新写回的 manifest sha256
// 与磁盘重算值相等（因为磁盘内容未变），State 原样传回。
func TestArtifactVerifier_RunVerify_PassesWhenTreeMatchesManifest(t *testing.T) {
	root := t.TempDir()
	paths, sha := writeFakeArtifactTree(t, root, "set-ok")

	store := newFakeVerifierStore(model.ArtifactSet{
		ID: "set-ok", Name: "rhel9-x86_64", State: model.ArtifactSetReady,
		Paths: paths, ManifestSHA256: sha,
	})
	v := newArtifactVerifier(store, root)

	v.runVerify(t.Context(), "set-ok")

	calls := store.getUpdateCalls()
	if len(calls) != 1 {
		t.Fatalf("校验通过应恰好写回 1 次确认结果，实际 %d 次: %+v", len(calls), calls)
	}
	if calls[0].State != model.ArtifactSetReady {
		t.Errorf("校验通过后 State 应保持 ready 不变，实际 %q", calls[0].State)
	}
	if calls[0].ManifestSHA256 != sha {
		t.Errorf("校验通过后 ManifestSHA256 应等于磁盘重算值 %q，实际 %q", sha, calls[0].ManifestSHA256)
	}
}

// TestArtifactVerifier_RunVerify_FailsWhenFileContentChanged 锁住「文件被
// 改动时校验失败」：造好一棵通过基准后，改一个字节，断言不再写回确认
// 结果（校验失败时不调用 UpdateArtifactSetState，State/ManifestSHA256
// 都保持磁盘改动前的登记值——见 verifier.go 文件末尾的设计决策说明）。
func TestArtifactVerifier_RunVerify_FailsWhenFileContentChanged(t *testing.T) {
	root := t.TempDir()
	paths, sha := writeFakeArtifactTree(t, root, "set-tampered")

	// 改一个字节：直接覆盖 vmlinuz 的内容。
	kernelPath := filepath.Join(root, "set-tampered", "vmlinuz")
	if err := os.WriteFile(kernelPath, []byte("TAMPERED-kernel-content"), 0o644); err != nil {
		t.Fatalf("篡改文件失败: %v", err)
	}

	store := newFakeVerifierStore(model.ArtifactSet{
		ID: "set-tampered", Name: "rhel9-x86_64", State: model.ArtifactSetReady,
		Paths: paths, ManifestSHA256: sha, // sha 是篡改前的基准值，此刻与磁盘不匹配
	})
	v := newArtifactVerifier(store, root)

	v.runVerify(t.Context(), "set-tampered")

	if calls := store.getUpdateCalls(); len(calls) != 0 {
		t.Errorf("文件被篡改时不应写回任何确认结果，实际调用了 %d 次: %+v", len(calls), calls)
	}

	// 制品集本身的字段应保持篡改前登记的值不变（校验失败不改状态、不
	// 拿一次可能有噪音的重算结果覆盖已登记值）。
	got, err := store.GetArtifactSet(t.Context(), "set-tampered")
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	if got.State != model.ArtifactSetReady {
		t.Errorf("校验失败后 State 应保持 ready 不变，实际 %q", got.State)
	}
	if got.ManifestSHA256 != sha {
		t.Errorf("校验失败后 ManifestSHA256 应保持登记值 %q 不变，实际 %q", sha, got.ManifestSHA256)
	}
}

// TestArtifactVerifier_RunVerify_FailsWhenFileMissing 锁住「文件缺失时
// 校验失败」——删掉 initrd 后重算，断言不写回确认结果（存在性校验这一层
// 会在内容校验之前就先发现缺失，见 verifier.go 的两层校验设计）。
func TestArtifactVerifier_RunVerify_FailsWhenFileMissing(t *testing.T) {
	root := t.TempDir()
	paths, sha := writeFakeArtifactTree(t, root, "set-missing")

	initrdPath := filepath.Join(root, "set-missing", "initrd.img")
	if err := os.Remove(initrdPath); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}

	store := newFakeVerifierStore(model.ArtifactSet{
		ID: "set-missing", Name: "rhel9-x86_64", State: model.ArtifactSetReady,
		Paths: paths, ManifestSHA256: sha,
	})
	v := newArtifactVerifier(store, root)

	v.runVerify(t.Context(), "set-missing")

	if calls := store.getUpdateCalls(); len(calls) != 0 {
		t.Errorf("文件缺失时不应写回任何确认结果，实际调用了 %d 次: %+v", len(calls), calls)
	}
}

// TestCheckArtifactPathExistsUnderRoot_ReportsExactMissingPath 锁住「文件
// 缺失时错误指出是哪个路径」——本函数是 runVerify 存在性校验这一层实际
// 调用的判定函数，直接测试它比在 runVerify 的日志输出里断言字符串更
// 精确、更不易受日志格式调整影响。
func TestCheckArtifactPathExistsUnderRoot_ReportsExactMissingPath(t *testing.T) {
	root := t.TempDir()
	writeFakeArtifactTree(t, root, "set-precise")

	if err := os.Remove(filepath.Join(root, "set-precise", "initrd.img")); err != nil {
		t.Fatalf("删除文件失败: %v", err)
	}

	checkedPath, err := checkArtifactPathExistsUnderRoot(root, filepath.ToSlash(filepath.Join("set-precise", "initrd.img")))
	if err == nil {
		t.Fatalf("initrd.img 已被删除，checkArtifactPathExistsUnderRoot 应返回错误")
	}
	want := "set-precise/initrd.img"
	if checkedPath != want {
		t.Errorf("checkedPath = %q, want %q——必须精确指出缺失的是哪个路径，不能只说『某个文件缺失』",
			checkedPath, want)
	}
}

// TestArtifactVerifier_RunVerify_GetArtifactSetNotFound_DoesNotPanic 锁住
// 「goroutine 启动后查不到制品集（罕见竞态：查完就被删）时不 panic、
// 不写回任何结果」——见 runVerify 文档对这个竞态窗口的说明。
func TestArtifactVerifier_RunVerify_GetArtifactSetNotFound_DoesNotPanic(t *testing.T) {
	root := t.TempDir()
	store := newFakeVerifierStore() // 空 store，任何 ID 都查不到
	v := newArtifactVerifier(store, root)

	v.runVerify(t.Context(), "does-not-exist") // 不应 panic

	if calls := store.getUpdateCalls(); len(calls) != 0 {
		t.Errorf("制品集查不到时不应写回任何结果，实际调用了 %d 次", len(calls))
	}
}

var _ artifactVerifierStore = (*fakeVerifierStore)(nil)
