package imports

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/lober/lober/internal/core/model"
)

// fakeArtifactSetStore 是 artifactSetUpdater 的内存测试替身，记录每次
// 调用，供测试断言「失败时只调 UpdateState 不调 SetContent」这条分工。
type fakeArtifactSetStore struct {
	mu sync.Mutex

	state          model.ArtifactSetState
	manifestSHA256 string
	osFamily       model.OSFamily
	paths          model.ArtifactPaths

	stateUpdateCalls   int
	contentUpdateCalls int

	failUpdateState error
	failSetContent  error
}

func newFakeArtifactSetStore() *fakeArtifactSetStore {
	return &fakeArtifactSetStore{state: model.ArtifactSetImporting}
}

func (f *fakeArtifactSetStore) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256 string) (model.ArtifactSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateUpdateCalls++
	if f.failUpdateState != nil {
		return model.ArtifactSet{}, f.failUpdateState
	}
	f.state = state
	f.manifestSHA256 = manifestSHA256
	return model.ArtifactSet{ID: id, State: f.state, ManifestSHA256: f.manifestSHA256}, nil
}

func (f *fakeArtifactSetStore) SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.contentUpdateCalls++
	if f.failSetContent != nil {
		return model.ArtifactSet{}, f.failSetContent
	}
	f.osFamily = osFamily
	f.paths = paths
	return model.ArtifactSet{ID: id, OSFamily: osFamily, Paths: paths}, nil
}

func (f *fakeArtifactSetStore) snapshot() (state model.ArtifactSetState, paths model.ArtifactPaths, contentCalls int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.state, f.paths, f.contentUpdateCalls
}

// fakeExtractorForJob 复用 extract_test.go 里定义的 fakeExtractor 类型。

func TestJob_SuccessPath_ImportingToReadyWithManifestRecorded(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: sampleRHELRoot()}
	job := NewJob(store, extractor)
	destDir := t.TempDir()

	err := job.RunRHEL(context.Background(), "artifact-1", "/tmp/whatever.iso", destDir)
	if err != nil {
		t.Fatalf("成功路径不应报错: %v", err)
	}

	state, paths, _ := store.snapshot()
	if state != model.ArtifactSetReady {
		t.Fatalf("成功后状态应为 ready，got %s", state)
	}
	if paths.Kernel != "vmlinuz" {
		t.Fatalf("成功后应写回 Kernel 路径，got %+v", paths)
	}
	if store.manifestSHA256 == "" {
		t.Fatal("成功后应记录 manifest 校验值")
	}
}

func TestJob_ExtractorOpenFailure_SpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{err: errors.New("挂载失败：设备忙")}
	job := NewJob(store, extractor)

	err := job.RunRHEL(context.Background(), "artifact-1", "/tmp/whatever.iso", t.TempDir())
	if err == nil {
		t.Fatal("打开 ISO 失败时应返回错误")
	}
	if !containsSubstring(err.Error(), "打开 ISO 失败") {
		t.Fatalf("错误信息应具体指出是打开 ISO 步骤失败，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("失败后状态应为 failed，got %s", state)
	}
}

func TestJob_IdentifyFailure_SpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: fstest.MapFS{"README.txt": &fstest.MapFile{Data: []byte("no markers")}}}
	job := NewJob(store, extractor)

	err := job.RunRHEL(context.Background(), "artifact-1", "/tmp/whatever.iso", t.TempDir())
	if err == nil {
		t.Fatal("识别不出家族时应返回错误")
	}
	if !containsSubstring(err.Error(), "识别不出") {
		t.Fatalf("错误信息应具体指出是识别步骤失败，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("失败后状态应为 failed，got %s", state)
	}
}

func TestJob_ExtractFailure_SpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	// .treeinfo 存在但格式异常（缺镜像段）——识别为 RHEL 成功，抽取步骤失败。
	extractor := fakeExtractor{root: fstest.MapFS{
		".treeinfo": &fstest.MapFile{Data: []byte("[general]\n")},
	}}
	job := NewJob(store, extractor)

	err := job.RunRHEL(context.Background(), "artifact-1", "/tmp/whatever.iso", t.TempDir())
	if err == nil {
		t.Fatal("抽取失败时应返回错误")
	}
	if !containsSubstring(err.Error(), "抽取") {
		t.Fatalf("错误信息应具体指出是抽取步骤失败，got %q", err.Error())
	}

	state, _, contentCalls := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("失败后状态应为 failed，got %s", state)
	}
	if contentCalls != 0 {
		t.Fatalf("抽取失败时不应调用 SetArtifactSetContent，got %d 次调用", contentCalls)
	}
}

func TestJob_SetContentFailure_PathsNotClearedOnFailure(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.failSetContent = errors.New("数据库写入失败")
	extractor := fakeExtractor{root: sampleRHELRoot()}
	job := NewJob(store, extractor)
	destDir := t.TempDir()

	err := job.RunRHEL(context.Background(), "artifact-1", "/tmp/whatever.iso", destDir)
	if err == nil {
		t.Fatal("写回内容失败时应返回错误")
	}

	state, paths, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("失败后状态应为 failed，got %s", state)
	}
	// 关键断言（任务 11：失败时已写入的路径不被清空）：本次调用 SetContent
	// 本身失败了（store 从未真正接受这次写入），fail 方法只调用了
	// UpdateArtifactSetState，没有用某个「清空 Paths」的调用去覆盖——
	// paths 字段维持在 fakeArtifactSetStore 的初始零值，这正好验证了
	// UpdateArtifactSetState 的实现不触及 paths 字段。
	if paths.Kernel != "" || len(paths.Initrd) != 0 || paths.Repo != "" {
		t.Fatalf("fail 方法不应意外写入 Paths，got %+v", paths)
	}
}

func TestJob_UnsupportedFamily_ESXi_FailsWithMVPReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: fstest.MapFS{
		"boot.cfg":  &fstest.MapFile{Data: []byte("")},
		"mboot.efi": &fstest.MapFile{Data: []byte("")},
	}}
	job := NewJob(store, extractor)

	err := job.RunUnsupported(context.Background(), "artifact-1", "/tmp/esxi.iso")
	if err == nil {
		t.Fatal("ESXi 应导致导入失败")
	}
	if !containsSubstring(err.Error(), "MVP 不支持") {
		t.Fatalf("失败原因应说明是 MVP 限制，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("失败后状态应为 failed，got %s", state)
	}
}

func TestJob_UnsupportedFamily_Windows_FailsWithMVPReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: fstest.MapFS{
		"sources/install.wim": &fstest.MapFile{Data: []byte("")},
	}}
	job := NewJob(store, extractor)

	err := job.RunUnsupported(context.Background(), "artifact-1", "/tmp/win.iso")
	if err == nil {
		t.Fatal("Windows 应导致导入失败")
	}
	if !containsSubstring(err.Error(), "MVP 不支持") {
		t.Fatalf("失败原因应说明是 MVP 限制，got %q", err.Error())
	}
}

// trackingExtractor 是能记录「同时有多少个 Open 调用在执行中」的
// Extractor 测试替身，用于证明两个并发提交的 job 从未同时进入抽取阶段——
// 单纯验证「两个 job 最终都成功」不足以证明串行执行，必须实际测量重叠度。
type trackingExtractor struct {
	root fs.FS

	mu          sync.Mutex
	current     int
	maxObserved int
}

func (e *trackingExtractor) Open(ctx context.Context, isoPath string) (fs.FS, error) {
	e.mu.Lock()
	e.current++
	if e.current > e.maxObserved {
		e.maxObserved = e.current
	}
	e.mu.Unlock()

	// 人为放慢，扩大两个 goroutine 可能重叠执行的时间窗口——如果并发控制
	// 失效，这段睡眠会让重叠现象更容易被 maxObserved 捕捉到，而不是恰好
	// 因为两次调用太快而错过。
	time.Sleep(50 * time.Millisecond)

	e.mu.Lock()
	e.current--
	e.mu.Unlock()

	return e.root, nil
}

func TestJob_ConcurrentSubmissionsRunSerially(t *testing.T) {
	store1 := newFakeArtifactSetStore()
	store2 := newFakeArtifactSetStore()
	extractor := &trackingExtractor{root: sampleRHELRoot()}
	job1 := NewJob(store1, extractor)
	job2 := NewJob(store2, extractor)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_ = job1.RunRHEL(context.Background(), "artifact-1", "/tmp/a.iso", filepath.Join(t.TempDir(), "a"))
	}()
	go func() {
		defer wg.Done()
		_ = job2.RunRHEL(context.Background(), "artifact-2", "/tmp/b.iso", filepath.Join(t.TempDir(), "b"))
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("两个并发导入 job 在 10 秒内未完成——可能信号量没有正确释放导致死锁")
	}

	extractor.mu.Lock()
	maxObserved := extractor.maxObserved
	extractor.mu.Unlock()
	if maxObserved > 1 {
		t.Fatalf("同一时刻观测到 %d 个 job 在执行抽取步骤，want 最多 1 个——"+
			"计划任务 11 要求同一时刻只允许一个导入 job 运行，第二个应该排队而不是并发跑",
			maxObserved)
	}

	state1, _, _ := store1.snapshot()
	state2, _, _ := store2.snapshot()
	if state1 != model.ArtifactSetReady {
		t.Errorf("job1 最终状态应为 ready，got %s", state1)
	}
	if state2 != model.ArtifactSetReady {
		t.Errorf("job2 最终状态应为 ready，got %s", state2)
	}
}

// containsSubstring 是 strings.Contains 的简写别名，让断言语句读起来
// 与「错误信息应包含 xxx」这句话的中文语序更接近。
func containsSubstring(s, substr string) bool {
	return strings.Contains(s, substr)
}

// ---------------------------------------------------------------------------
// Run 的家族分派（本轮新增：查表分派取代逐家族单独暴露一个 RunXxx 方法）
// ---------------------------------------------------------------------------

// sampleUbuntuRoot 构造一份能让 Identify 判定为 Ubuntu、且能让 ExtractUbuntu
// 成功跑完的最小内存文件系统（casper/ 下三个固定文件名）。
func sampleUbuntuRoot() fstest.MapFS {
	return fstest.MapFS{
		".disk/info":                 &fstest.MapFile{Data: []byte("Ubuntu 24.04")},
		"casper/vmlinuz":             &fstest.MapFile{Data: []byte("kernel-bytes")},
		"casper/initrd":              &fstest.MapFile{Data: []byte("initrd-bytes")},
		"casper/filesystem.squashfs": &fstest.MapFile{Data: []byte("squashfs-bytes")},
	}
}

// sampleDebianRoot 构造一份能让 Identify 判定为 Debian、且能让 ExtractDebian
// 成功跑完的最小内存文件系统（dists/ 目录 + installer 子目录下两个固定
// 文件名，findDebianInstallerFile 会跳过 dists/pool 递归查找其余部分）。
func sampleDebianRoot() fstest.MapFS {
	return fstest.MapFS{
		"dists/stable/Release": &fstest.MapFile{Data: []byte("dummy")},
		// Debian 的内核文件名是 linux（不是 RHEL 的 vmlinuz），initrd 是
		// initrd.gz（不是 initrd.img）—— 两者都与 RHEL 不同，抽取器按
		// 真实文件名查找，夹具必须照真实 ISO 的命名造，否则测的是一个
		// 不存在的 ISO 形态。
		"install.amd/linux":     &fstest.MapFile{Data: []byte("kernel-bytes")},
		"install.amd/initrd.gz": &fstest.MapFile{Data: []byte("initrd-bytes")},
	}
}

// TestJob_Run_DispatchesRHELDebianUbuntuToTheirExtractors 锁住「三个已实现
// 家族各自走到对应的抽取器」这条要求：用内存文件系统造出三种家族各自的
// 特征文件，断言 Run 跑完后制品集是 ready，且落盘路径符合各家族抽取函数
// 自己的产出约定（不是随便某个家族的产出）。
func TestJob_Run_DispatchesRHELDebianUbuntuToTheirExtractors(t *testing.T) {
	cases := []struct {
		name       string
		root       fstest.MapFS
		wantFamily model.OSFamily
		wantKernel string
		wantInitrd []string
	}{
		{
			name:       "RHEL",
			root:       sampleRHELRoot(),
			wantFamily: model.OSFamilyRHEL,
			wantKernel: "vmlinuz",
			wantInitrd: []string{"initrd.img"},
		},
		{
			name:       "Debian",
			root:       sampleDebianRoot(),
			wantFamily: model.OSFamilyDebian,
			wantKernel: "linux",
			wantInitrd: []string{"initrd.gz"},
		},
		{
			name:       "Ubuntu",
			root:       sampleUbuntuRoot(),
			wantFamily: model.OSFamilyUbuntu,
			wantKernel: "vmlinuz",
			wantInitrd: []string{"initrd"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeArtifactSetStore()
			extractor := fakeExtractor{root: tc.root}
			job := NewJob(store, extractor)
			destDir := t.TempDir()

			err := job.Run(context.Background(), "artifact-1", "/tmp/whatever.iso", destDir)
			if err != nil {
				t.Fatalf("%s 应成功分派到对应抽取器，got err: %v", tc.name, err)
			}

			state, paths, _ := store.snapshot()
			if state != model.ArtifactSetReady {
				t.Fatalf("%s 成功后状态应为 ready，got %s", tc.name, state)
			}
			if store.osFamily != tc.wantFamily {
				t.Errorf("%s 写回的 OSFamily = %s, want %s", tc.name, store.osFamily, tc.wantFamily)
			}
			if paths.Kernel != tc.wantKernel {
				t.Errorf("%s 写回的 Kernel = %q, want %q——分派到了错误家族的抽取器",
					tc.name, paths.Kernel, tc.wantKernel)
			}
			if len(paths.Initrd) != len(tc.wantInitrd) || (len(paths.Initrd) > 0 && paths.Initrd[0] != tc.wantInitrd[0]) {
				t.Errorf("%s 写回的 Initrd = %v, want %v", tc.name, paths.Initrd, tc.wantInitrd)
			}
			if store.manifestSHA256 == "" {
				t.Errorf("%s 成功后应记录 manifest 校验值", tc.name)
			}
		})
	}
}

// TestJob_Run_UnsupportedFamily_FailsWithMVPReason 锁住「未实现家族落明确
// 错误而非静默成功」这条最关键的要求（任务说明：静默成功会产出一个
// ready 但没有 kernel 的制品集，而那要等机器上电取不到内核才暴露）——
// Windows/ESXi 被 Identify 判定为「识别出但 MVP 不支持」，Run 必须把
// 这类情形置为 failed，绝不能因为查表分派而意外放行。
func TestJob_Run_UnsupportedFamily_FailsWithMVPReason(t *testing.T) {
	cases := []struct {
		name string
		root fstest.MapFS
	}{
		{name: "ESXi", root: fstest.MapFS{
			"boot.cfg":  &fstest.MapFile{Data: []byte("")},
			"mboot.efi": &fstest.MapFile{Data: []byte("")},
		}},
		{name: "Windows", root: fstest.MapFS{
			"sources/install.wim": &fstest.MapFile{Data: []byte("")},
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			store := newFakeArtifactSetStore()
			extractor := fakeExtractor{root: tc.root}
			job := NewJob(store, extractor)

			err := job.Run(context.Background(), "artifact-1", "/tmp/whatever.iso", t.TempDir())
			if err == nil {
				t.Fatalf("%s 应导致导入失败，不应静默成功", tc.name)
			}
			if !containsSubstring(err.Error(), "MVP 不支持") {
				t.Fatalf("%s 失败原因应说明是 MVP 限制，got %q", tc.name, err.Error())
			}

			state, paths, contentCalls := store.snapshot()
			if state != model.ArtifactSetFailed {
				t.Fatalf("%s 失败后状态应为 failed，got %s——静默成功会产出一个 ready 但没有"+
					"kernel 的制品集，那个错误要等机器上电取不到内核才暴露", tc.name, state)
			}
			if contentCalls != 0 {
				t.Fatalf("%s 未支持家族不应调用 SetArtifactSetContent，got %d 次调用", tc.name, contentCalls)
			}
			if paths.Kernel != "" {
				t.Fatalf("%s 未支持家族不应写入任何 Kernel 路径，got %q", tc.name, paths.Kernel)
			}
		})
	}
}

// TestJob_Run_NotIdentified_FailsWithSpecificReason 锁住「完全识别不出时
// 明确失败」（与 Windows/ESXi 的「识别出但不支持」是两种不同的失败原因，
// 错误信息要能区分两者，运维看日志才能判断该做什么）。
func TestJob_Run_NotIdentified_FailsWithSpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: fstest.MapFS{"README.txt": &fstest.MapFile{Data: []byte("no markers")}}}
	job := NewJob(store, extractor)

	err := job.Run(context.Background(), "artifact-1", "/tmp/whatever.iso", t.TempDir())
	if err == nil {
		t.Fatal("识别不出家族时应返回错误")
	}
	if !containsSubstring(err.Error(), "识别不出") {
		t.Fatalf("错误信息应具体指出是识别步骤失败，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("识别不出时状态应为 failed，got %s", state)
	}
}

// TestJob_RunRHEL_DelegatesToRunAndRejectsWrongFamily 确认 RunRHEL 兼容
// 入口仍然可用（既有测试 TestJob_SuccessPath_* 等已经覆盖了正常路径，
// 本测试补上「RunRHEL 名字本身承诺了家族」这一条：传入实际是 Ubuntu 的
// 内容时，RunRHEL 必须拒绝，不能因为委托给 Run 而意外接受任意家族）。
func TestJob_RunRHEL_DelegatesToRunAndRejectsWrongFamily(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: sampleUbuntuRoot()}
	job := NewJob(store, extractor)

	err := job.RunRHEL(context.Background(), "artifact-1", "/tmp/whatever.iso", t.TempDir())
	if err == nil {
		t.Fatal("RunRHEL 收到 Ubuntu 内容时应拒绝")
	}
	if !containsSubstring(err.Error(), "家族与调用的抽取方法不符") {
		t.Fatalf("错误信息应指出家族不符，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("家族不符时状态应为 failed，got %s", state)
	}
}
