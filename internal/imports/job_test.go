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
//
// declaredFirmware 与 getArtifactSetCalls/failGetArtifactSet 是本轮
// （n2 轨，Windows 解封）新增字段：GetArtifactSet 是 Job 从制品集读取
// declaredFirmware 的唯一途径（登记时运维填的字段，SPEC §2.1、§0.3），
// 测试通过它模拟「运维登记时选了 uefi 还是 bios」。
type fakeArtifactSetStore struct {
	mu sync.Mutex

	state          model.ArtifactSetState
	manifestSHA256 string
	failReason     string
	osFamily       model.OSFamily
	paths          model.ArtifactPaths

	declaredFirmware model.FirmwareMode

	stateUpdateCalls    int
	contentUpdateCalls  int
	getArtifactSetCalls int

	failUpdateState    error
	failSetContent     error
	failGetArtifactSet error
}

func newFakeArtifactSetStore() *fakeArtifactSetStore {
	return &fakeArtifactSetStore{state: model.ArtifactSetImporting}
}

func (f *fakeArtifactSetStore) GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.getArtifactSetCalls++
	if f.failGetArtifactSet != nil {
		return model.ArtifactSet{}, f.failGetArtifactSet
	}
	return model.ArtifactSet{ID: id, Firmware: f.declaredFirmware}, nil
}

func (f *fakeArtifactSetStore) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.stateUpdateCalls++
	if f.failUpdateState != nil {
		return model.ArtifactSet{}, f.failUpdateState
	}
	f.state = state
	f.manifestSHA256 = manifestSHA256
	f.failReason = reason
	return model.ArtifactSet{ID: id, State: f.state, ManifestSHA256: f.manifestSHA256, FailReason: f.failReason}, nil
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

// TestJob_RunUnsupported_ESXi_NowRejectsAsCallerLogicError 替换了原先锁
// 「ESXi 走 RunUnsupported 应因 MVP 限制失败」的测试 —— 那个封锁已解除。
//
// RunUnsupported 这个兼容入口的语义是「调用前还不知道家族、用它确认内容
// 确实不受支持」。ESXi 现在有了完整的导入路径（runESXi 分支 → ExtractESXi），
// 所以传入 ESXi 的内容意味着调用方在没查过支持情况之前就走错了入口，
// 应明确报「调用方逻辑错误」，而不是按已经不成立的 MVP 限制文案失败。
//
// 六个家族现在全部受支持，所以 RunUnsupported 只剩「识别不出家族」这一条
// 真正会走到的路径。
func TestJob_RunUnsupported_ESXi_NowRejectsAsCallerLogicError(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: fstest.MapFS{
		"boot.cfg":             &fstest.MapFile{Data: []byte("")},
		"efi/boot/bootx64.efi": &fstest.MapFile{Data: []byte("")},
	}}
	job := NewJob(store, extractor)

	err := job.RunUnsupported(context.Background(), "artifact-1", "/tmp/esxi.iso")
	if err == nil {
		t.Fatal("ESXi 已受支持，走 RunUnsupported 应报调用方逻辑错误")
	}
	if !containsSubstring(err.Error(), "调用方逻辑错误") {
		t.Fatalf("失败原因应指出调用方走错入口，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("失败后状态应为 failed，got %s", state)
	}
}

// TestJob_RunUnsupported_Windows_NowRejectsAsCallerLogicError 替换了旧版
// TestJob_UnsupportedFamily_Windows_FailsWithMVPReason（旧版锁的是「Windows
// 走 RunUnsupported 应因 MVP 限制失败」，这正是本轮 n2 轨要解除的封锁行为）。
//
// Windows 解封后有了专门的抽取实现（ExtractWindows，走 Run 的分派分支），
// RunUnsupported 这个兼容入口的语义是「调用前还不知道家族、用来确认它确实
// 不受支持」——如果传入的内容实际识别为 Windows（现在已支持），说明调用方
// 在还没查过支持情况之前就走错了入口，RunUnsupported 自身的 case err == nil
// 分支会明确报「调用方逻辑错误」，而不是允许静默地按旧的 MVP 限制文案失败。
func TestJob_RunUnsupported_Windows_NowRejectsAsCallerLogicError(t *testing.T) {
	store := newFakeArtifactSetStore()
	extractor := fakeExtractor{root: fstest.MapFS{
		"sources/install.wim": &fstest.MapFile{Data: []byte("")},
	}}
	job := NewJob(store, extractor)

	err := job.RunUnsupported(context.Background(), "artifact-1", "/tmp/win.iso")
	if err == nil {
		t.Fatal("Windows 现在有对应抽取实现，调用 RunUnsupported 本身是调用方逻辑错误，应报错")
	}
	if !containsSubstring(err.Error(), "调用方逻辑错误") {
		t.Fatalf("失败原因应说明是调用方选错了入口（Windows 已有实现），got %q", err.Error())
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

// ---------------------------------------------------------------------------
// Windows 的导入分派（本轮 n2 轨新增：解除 mvpUnsupportedFamilies 对 Windows
// 的封锁后，Run 需要一条专门的分派路径把识别结果接到 ExtractWindows）。
//
// 设计取舍说明见 extract_windows.go 顶部契约缺口章节与本文件 Job 结构体
// 注释：ExtractWindows 比 familyExtractors 表的行类型
// func(fs.FS, string) (ArtifactPaths, error) 多三个参数
// （WindowsExternalSources / declaredFirmware / WimArchDetector），选择
// 「不进查表，Run 内单独判一次」而不是改表的行类型或用闭包在建表时绑定——
// 因为固件与外部文件路径必须在每次 Run 调用时动态解析（同一 Job 实例的
// 不同调用可能对应不同的 artifactSetID，绑定在建表时的闭包做不到这一点），
// 且局部改动不影响 RHEL/Debian/Ubuntu/SUSE 已有的表驱动分派，与 n3 轨在
// 同一文件里给 ESXi 加类似分支互相独立（各自往 Run 的 switch 里加一个
// case，不共享同一段代码，合并时是两处独立的插入而非同一处的修改冲突）。
// ---------------------------------------------------------------------------

// windowsExternalFilesOnDisk 复用 extract_windows_test.go 的
// windowsExternalFiles 辅助函数（同包），在磁盘上写出四个外部文件。

// fakeWindowsSourcesResolver 是 WindowsSourcesResolver 的测试替身：按预设
// 固定返回值响应，记录调用时传入的 artifactSetID 供断言。
type fakeWindowsSourcesResolver struct {
	sources WindowsExternalSources
	err     error

	mu           sync.Mutex
	lastCalledID string
	calls        int
}

func (f *fakeWindowsSourcesResolver) ResolveWindowsSources(ctx context.Context, artifactSetID string) (WindowsExternalSources, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	f.lastCalledID = artifactSetID
	if f.err != nil {
		return WindowsExternalSources{}, f.err
	}
	return f.sources, nil
}

// TestJob_Run_Windows_DispatchesToExtractWindows_UEFI 锁住核心新增行为：
// Windows 的 ISO 树被 Identify 识别后，Run 能走到 ExtractWindows，且固件
// 信息真的从 GetArtifactSet 传到了抽取器——制品集声明 uefi、探测器也判定
// 是 uefi 时应导入成功并落盘对应位宽的 WinPE 制品键。
func TestJob_Run_Windows_DispatchesToExtractWindows_UEFI(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.declaredFirmware = model.FirmwareUEFI
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor)
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	resolver := &fakeWindowsSourcesResolver{sources: ext}
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}
	job.ConfigureWindows(resolver, detector)
	destDir := t.TempDir()

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", destDir)
	if err != nil {
		t.Fatalf("Windows 声明 uefi 且探测结果一致时应导入成功，got err: %v", err)
	}

	state, paths, _ := store.snapshot()
	if state != model.ArtifactSetReady {
		t.Fatalf("成功后状态应为 ready，got %s", state)
	}
	if store.osFamily != model.OSFamilyWindows {
		t.Fatalf("写回的 OSFamily 应为 windows，got %s", store.osFamily)
	}
	if _, ok := paths.Extra["boot.wim"]; !ok {
		t.Fatalf("落盘路径应含 boot.wim 键（uefi 位宽的 WinPE 制品），got %+v", paths.Extra)
	}
	if resolver.calls != 1 {
		t.Fatalf("应调用一次 ResolveWindowsSources，got %d 次", resolver.calls)
	}
	if resolver.lastCalledID != "artifact-1" {
		t.Fatalf("ResolveWindowsSources 应收到当前制品集 ID，got %q", resolver.lastCalledID)
	}
}

// TestJob_Run_Windows_DispatchesToExtractWindows_BIOS 补上 BIOS 侧：同一
// 制品树，制品集声明 bios、探测器也判定为 bios，应同样成功——两个固件各自
// 命中的分支都要覆盖，不能只测 UEFI 那一半。
func TestJob_Run_Windows_DispatchesToExtractWindows_BIOS(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.declaredFirmware = model.FirmwareBIOS
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor)
	ext := windowsExternalFiles(t, []byte("bios-winpe-bytes"))
	resolver := &fakeWindowsSourcesResolver{sources: ext}
	detector := fakeWimArchDetector{firmware: model.FirmwareBIOS}
	job.ConfigureWindows(resolver, detector)
	destDir := t.TempDir()

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", destDir)
	if err != nil {
		t.Fatalf("Windows 声明 bios 且探测结果一致时应导入成功，got err: %v", err)
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetReady {
		t.Fatalf("成功后状态应为 ready，got %s", state)
	}
}

// TestJob_Run_Windows_DeclaredFirmwareAny_Rejected 锁住 §2.1 的硬约束：
// 制品集声明固件为 any 时必须被拒绝——any 意味着运维没想清楚要导哪个位宽，
// 若放行，产出的制品集装机时才会暴露不匹配（那时运维已经在机房了）。
func TestJob_Run_Windows_DeclaredFirmwareAny_Rejected(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.declaredFirmware = model.FirmwareAny
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor)
	ext := windowsExternalFiles(t, []byte("winpe-bytes"))
	resolver := &fakeWindowsSourcesResolver{sources: ext}
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}
	job.ConfigureWindows(resolver, detector)

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", t.TempDir())
	if err == nil {
		t.Fatal("制品集声明固件为 any 时应拒绝导入——SPEC §2.1：Windows 必须取具体固件值")
	}

	state, _, contentCalls := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("声明 any 时状态应为 failed，got %s", state)
	}
	if contentCalls != 0 {
		t.Fatalf("声明 any 被拒绝时不应调用 SetArtifactSetContent，got %d 次", contentCalls)
	}
}

// TestJob_Run_Windows_MissingInstallWIM_FailsWithSpecificReason 锁住「缺
// sources/install.wim 报明确错误」这条要求：即使 Identify 已经确认了家族，
// ExtractWindows 自己也会再核实一遍这个特征文件是否存在（extract_windows.go
// 的设计说明：防止调用方传入识别结果与实际内容不一致的 root）。
func TestJob_Run_Windows_MissingInstallWIM_FailsWithSpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.declaredFirmware = model.FirmwareUEFI
	// 构造一棵会被识别为 windows（通过 sources/install.wim）但随后又在
	// ExtractWindows 内部再次核实时人为移除该文件是不现实的（Identify 与
	// ExtractWindows 读的是同一个 root）——本用例改为验证「resolver 返回
	// 的外部文件缺失」这一等价的具体失败场景（同样是「缺文件报明确错误」
	// 这条要求的一种体现，覆盖 wimboot/BCD/boot.sdi/boot.wim 四者之一缺失
	// 的路径，与 extract_windows_test.go 里针对 ExtractWindows 本身的四个
	// 缺失用例互补：那四个测的是纯函数骨架，这里测的是 Job.Run 的分派
	// 分支真的把 resolver 返回的路径传给了 ExtractWindows，缺失确实会被
	// 一路传播到 Run 的失败结果，不是在分派层被悄悄吞掉）。
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor)
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	ext.BootWIMPath = "" // 模拟 boot.wim 缺失
	resolver := &fakeWindowsSourcesResolver{sources: ext}
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}
	job.ConfigureWindows(resolver, detector)

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", t.TempDir())
	if err == nil {
		t.Fatal("boot.wim 缺失时应返回明确错误")
	}
	if !containsSubstring(err.Error(), "boot.wim") {
		t.Fatalf("错误信息应具体指出缺失的是 boot.wim，got %q", err.Error())
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("缺文件时状态应为 failed，got %s", state)
	}
}

// TestJob_Run_Windows_NotConfigured_FailsWithSpecificReason 锁住「未调用
// ConfigureWindows 时明确报错，不 panic、不静默成功」这条兜底行为——生产
// 装配点（cmd/lober）目前还没有 WindowsExternalSources 的真实来源（契约
// 缺口，见 notes），本测试确保在这个缺口被补上之前，未配置的 Job 遇到
// Windows 家族会得到一条具体、可诊断的失败原因，而不是让 nil 接口方法
// 调用直接 panic 拖垫整个导入 goroutine。
func TestJob_Run_Windows_NotConfigured_FailsWithSpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.declaredFirmware = model.FirmwareUEFI
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor) // 未调用 ConfigureWindows

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", t.TempDir())
	if err == nil {
		t.Fatal("未配置 Windows 外部文件来源时应报错，不应静默成功或 panic")
	}
	if !containsSubstring(err.Error(), "ConfigureWindows") {
		t.Fatalf("错误信息应指出需要调用 ConfigureWindows 完成配置，got %q", err.Error())
	}

	state, _, contentCalls := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("未配置时状态应为 failed，got %s", state)
	}
	if contentCalls != 0 {
		t.Fatalf("未配置时不应调用 SetArtifactSetContent，got %d 次", contentCalls)
	}
}

// TestJob_Run_Windows_ResolverError_FailsWithSpecificReason 锁住「外部文件
// 来源解析失败时整体拒绝导入」——resolver 可能因为找不到运维声明的外部
// 文件目录而报错，这类失败必须让整个导入 job 置为 failed，不能假装继续。
func TestJob_Run_Windows_ResolverError_FailsWithSpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.declaredFirmware = model.FirmwareUEFI
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor)
	resolver := &fakeWindowsSourcesResolver{err: errors.New("外部文件目录不存在")}
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}
	job.ConfigureWindows(resolver, detector)

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", t.TempDir())
	if err == nil {
		t.Fatal("resolver 返回错误时应导致导入失败")
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("resolver 失败时状态应为 failed，got %s", state)
	}
}

// TestJob_Run_Windows_GetArtifactSetError_FailsWithSpecificReason 锁住读取
// 制品集本身失败（比如 store 挂了）时的行为：不能假设一个默认固件继续。
func TestJob_Run_Windows_GetArtifactSetError_FailsWithSpecificReason(t *testing.T) {
	store := newFakeArtifactSetStore()
	store.failGetArtifactSet = errors.New("数据库连接失败")
	extractor := fakeExtractor{root: sampleWindowsRoot()}
	job := NewJob(store, extractor)
	ext := windowsExternalFiles(t, []byte("uefi-winpe-bytes"))
	resolver := &fakeWindowsSourcesResolver{sources: ext}
	detector := fakeWimArchDetector{firmware: model.FirmwareUEFI}
	job.ConfigureWindows(resolver, detector)

	err := job.Run(context.Background(), "artifact-1", "/tmp/win.iso", t.TempDir())
	if err == nil {
		t.Fatal("GetArtifactSet 失败时应导致导入失败，不能假设一个默认固件继续")
	}

	state, _, _ := store.snapshot()
	if state != model.ArtifactSetFailed {
		t.Fatalf("GetArtifactSet 失败时状态应为 failed，got %s", state)
	}
}
