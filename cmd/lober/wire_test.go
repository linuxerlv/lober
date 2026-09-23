package main

import (
	"context"
	"io"
	"io/fs"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/statemachine"
	"github.com/lober/lober/internal/store/sqlite"
)

// testConfig 构造一份能通过 config.Config.Validate 的最小配置，供
// buildApp 系列测试使用。三个存储路径指向 t.TempDir()，不污染真实磁盘、
// 不需要手工清理（t.TempDir 由测试框架自动回收）。
func testConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	return &config.Config{
		Gateway: config.Gateway{
			Listen:       "127.0.0.1:18080",
			GatewayBase:  "http://127.0.0.1:18080",
			ArtifactBase: "http://127.0.0.1:18081",
			SyslogListen: "127.0.0.1:18514",
			RootSSHPublicKeys: []string{
				"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
			},
		},
		Console: config.Console{
			Listen:      "127.0.0.1:18090",
			TokenSecret: "test-secret",
		},
		Store: config.Store{
			DBPath:       filepath.Join(dir, "db", "lober.db"),
			ArtifactPath: filepath.Join(dir, "artifacts"),
			LogPath:      filepath.Join(dir, "log"),
		},
		Auth: config.Auth{
			Users: []config.User{
				{Name: "admin", BcryptHash: "$2a$10$7EqJtq98hPqEX7fNZaFWoOhi6vjuLpk3wJ0Qe0KcJvJ8G7ZbtG9Ia", Roles: []string{"admin"}},
			},
		},
		Provider: config.Provider{
			IPAM: "static", DNS: "noop", Resolver: "rule-based",
			DiskPolicy: model.DiskPolicyLargest, HeartbeatMode: "off",
		},
	}
}

// TestBuildApp_AllSevenSeamsWired 断言七个接缝逐个非空，特别是建任务的
// 同步决策接口（漏了它的后果是 IPAM 失败也能建出任务，见计划任务 1
// 「设计要点」）。
func TestBuildApp_AllSevenSeamsWired(t *testing.T) {
	cfg := testConfig(t)
	app, err := buildApp(cfg)
	if err != nil {
		t.Fatalf("buildApp 失败: %v", err)
	}
	defer app.DB.Close()

	checks := []struct {
		name string
		val  any
	}{
		{"Store", app.Store},
		{"Clock", app.Clock},
		{"Events", app.Events},
		{"TokenIssuer", app.TokenIssuer},
		{"Prechecker", app.Prechecker},
		{"StateMachine", app.StateMachine},
		{"Renderer", app.Renderer},
		{"GatewayServer", app.GatewayServer},
		{"ConsoleServer", app.ConsoleServer},
		{"UIHandler", app.UIHandler},
		// 建任务的同步决策接口：这是计划任务 1 特别点名要锁住的一条。
		{"ParamsResolver", app.ParamsResolver},
	}
	for _, c := range checks {
		if c.val == nil || isNilInterfaceValue(c.val) {
			t.Errorf("接缝 %s 为空——七个模块的装配存在遗漏（计划任务 1「测试要锁住的行为」）", c.name)
		}
	}

	// 七个 provider 也逐一非空（Providers 结构体本身不是接口，逐字段检查）。
	providerChecks := []struct {
		name string
		val  any
	}{
		{"Providers.IPAM", app.Providers.IPAM},
		{"Providers.DNS", app.Providers.DNS},
		{"Providers.Inventory", app.Providers.Inventory},
		{"Providers.Notify", app.Providers.Notify},
		{"Providers.Heartbeat", app.Providers.Heartbeat},
		{"Providers.TemplateResolver", app.Providers.TemplateResolver},
		{"Providers.DiskPlanner", app.Providers.DiskPlanner},
	}
	for _, c := range providerChecks {
		if c.val == nil || isNilInterfaceValue(c.val) {
			t.Errorf("provider 接缝 %s 为空", c.name)
		}
	}
}

// isNilInterfaceValue 判断一个 any 包装的接口值本身是否为 nil——直接用
// == nil 在 val 是「类型非 nil、值为 nil 指针」时会误判为非 nil（Go 的
// 经典陷阱），本测试的字段全部是接口类型，用反射避免这个陷阱。
func isNilInterfaceValue(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Interface, reflect.Map, reflect.Slice, reflect.Func, reflect.Chan:
		return rv.IsNil()
	default:
		return false
	}
}

// TestBuildApp_SchemaReadyAfterAssembly 断言装配后 schema 已就绪——用一次
// 查询验证，而不是等第一个请求来了才发现库是空的（计划任务 1「测试要锁住
// 的行为」）。
func TestBuildApp_SchemaReadyAfterAssembly(t *testing.T) {
	cfg := testConfig(t)
	app, err := buildApp(cfg)
	if err != nil {
		t.Fatalf("buildApp 失败: %v", err)
	}
	defer app.DB.Close()

	// 用 ListTasks 这个真实的 Store 查询验证 schema 已经就绪：若表不存在，
	// 这里会直接返回 SQL 层的错误（"no such table"），而不是空切片。
	if _, err := app.Store.ListTasks(context.Background(), contract.TaskFilter{}); err != nil {
		t.Errorf("装配后查询任务表失败，schema 未就绪: %v", err)
	}
}

// TestBuildApp_StorageDirsAutoCreated 断言两个存储目录（制品、日志）被
// 自动创建——数据库所在目录同理，由 sqlite.Open 隐式验证（打不开数据库
// 文件本身就会导致 buildApp 报错）。
func TestBuildApp_StorageDirsAutoCreated(t *testing.T) {
	cfg := testConfig(t)
	app, err := buildApp(cfg)
	if err != nil {
		t.Fatalf("buildApp 失败: %v", err)
	}
	defer app.DB.Close()

	for _, dir := range []string{cfg.Store.ArtifactPath, cfg.Store.LogPath, filepath.Dir(cfg.Store.DBPath)} {
		info, err := os.Stat(dir)
		if err != nil {
			t.Errorf("目录 %s 未被自动创建: %v", dir, err)
			continue
		}
		if !info.IsDir() {
			t.Errorf("路径 %s 存在但不是目录", dir)
		}
	}
}

// TestBuildApp_ConstructFailureIsLocatable 断言构造失败时的错误信息能
// 定位到具体依赖——用一个明显会失败的配置（provider 选择项非法）验证
// 错误信息里包含足以定位问题的线索，而不是一句笼统的"装配失败"。
func TestBuildApp_ConstructFailureIsLocatable(t *testing.T) {
	cfg := testConfig(t)
	cfg.Provider.IPAM = "no-such-ipam-kind"

	_, err := buildApp(cfg)
	if err == nil {
		t.Fatal("期望非法的 provider 选择项导致构造失败，但成功了")
	}
	if !strings.Contains(err.Error(), "IPAM") {
		t.Errorf("错误信息 %q 未能定位到具体依赖（应提及 IPAM）", err.Error())
	}
}

// TestAssemblyPrechecker_UsesInjectedFilesystemNotEmptyFS 是本任务最关键
// 的一条契约缺口回归测试：验证装配点注入的 assemblyPrechecker 确实绕开了
// internal/statemachine/boot.go 硬编码的 artifactFilesFS()（空文件系统），
// 使用装配时注入的真实制品根目录文件系统。
//
// 不这样测的后果：StartBoot 路径下的制品完整性校验会对任何非空路径判定
// 为缺失、直接拒绝引导——这是 MVP 的引导路径在装配后仍可能因此拒绝所有
// 引导的阻塞级问题（任务描述明确要求验证这一点，不能悄悄放过）。
func TestAssemblyPrechecker_UsesInjectedFilesystemNotEmptyFS(t *testing.T) {
	cfg := testConfig(t)
	app, err := buildApp(cfg)
	if err != nil {
		t.Fatalf("buildApp 失败: %v", err)
	}
	defer app.DB.Close()

	// 在制品根目录下真实建出一套完整的最小制品文件（内核、initrd、仓库
	// 元数据），与 model.ArtifactSet.Paths 的相对路径约定一致。
	setID := "test-artifact-set"
	setDir := filepath.Join(cfg.Store.ArtifactPath, setID)
	mustWriteFile(t, filepath.Join(setDir, "vmlinuz"), "kernel")
	mustWriteFile(t, filepath.Join(setDir, "initrd.img"), "initrd")
	mustWriteFile(t, filepath.Join(setDir, "repo", "repodata", "repomd.xml"), "<repomd/>")

	artifact := model.ArtifactSet{
		ID: setID, Name: "test", OSFamily: model.OSFamilyRHEL, OSVersion: "9.4",
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
		Paths: model.ArtifactPaths{Kernel: "vmlinuz", Initrd: []string{"initrd.img"}, Repo: "repo/"},
	}

	// 直连 Prechecker.Check（跳过 StartBoot 整条状态转换链，只验证「注入
	// 的文件系统是否真的被使用」这一件事）：如果 assemblyPrechecker 的
	// files 参数被忽略、内部真的换成了 artifactFS，制品完整性这一项就应该
	// 通过（因为磁盘上真实存在这些文件）；如果绕法失效（回退到空 FS 的
	// 行为），制品完整性会报「不存在」。传入一个真正的空 fs.FS（模拟
	// internal/statemachine/boot.go 的 artifactFilesFS()）来证明
	// assemblyPrechecker.Check 确实忽略了这个参数。
	err = app.Prechecker.Check(context.Background(), model.ArchX86_64, model.FirmwareUEFI, artifact, emptyTestFS{})
	if err != nil {
		t.Errorf("Prechecker.Check 应该看到装配时注入的真实制品文件系统而通过校验，但失败: %v"+
			"——如果这里报『制品文件不存在』，说明 assemblyPrechecker 的绕法没有生效，"+
			"StartBoot 路径下的引导会被无条件拒绝（任务描述里的阻塞级问题）", err)
	}
}

// emptyTestFS 是一个不含任何文件的 fs.FS 实现，与
// internal/statemachine/boot.go 的 emptyFS 同构（该类型未导出，本测试
// 无法直接复用，独立声明一份行为等价的替身）。
type emptyTestFS struct{}

func (emptyTestFS) Open(name string) (fs.File, error) {
	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func mustWriteFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o640); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
}

// TestBuildApp_RootSSHPublicKeysWiredToGatewayAnswer 是本轮新增契约通路
// 的端到端验证：cfg.Gateway.RootSSHPublicKeys 必须真的从配置一路传到
// GatewayServer 渲染出的应答文件里（SPEC §1.5）。不用桩渲染器、不直接
// 读取 gateway.Server 的私有字段（它没有导出访问器）——用真实的
// render.Renderer 走一次完整的 HTTP 请求，这是唯一能在 cmd/lober 这一层
// 证明"配置值真的到达了渲染输出"的方式，比断言某个中间接缝非空更接近
// 任务要求的"真的跑起来"。
func TestBuildApp_RootSSHPublicKeysWiredToGatewayAnswer(t *testing.T) {
	cfg := testConfig(t)
	app := buildAppWithDBForTest(t, cfg)

	ctx := context.Background()
	host := model.Host{SN: "SN-WIRE-1", MAC: "aa:bb:cc:dd:ee:ff", UUID: "uuid-wire-1", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := app.Store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	artifact := model.ArtifactSet{
		ID: "art-wire-1", Name: "rocky9.4", OSFamily: model.OSFamilyRHEL, OSVersion: "9.4",
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
		Paths: model.ArtifactPaths{Kernel: "vmlinuz", Initrd: []string{"initrd.img"}, Repo: "repo/"},
	}
	if _, err := app.Store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	tpl := model.Template{ID: "tpl-wire-1", Name: "rocky9", ArtifactSetID: "art-wire-1", Version: 1, State: model.TemplateStateActive}
	if _, err := app.Store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	task := model.Task{
		ID: "task-wire-1", HostSN: "SN-WIRE-1", HostMAC: "aa:bb:cc:dd:ee:ff", HostUUID: "uuid-wire-1",
		TemplateID: "tpl-wire-1", TemplateVersion: 1, ArtifactSetID: "art-wire-1",
		State: model.TaskStateBooting, CurrentPhase: model.PhaseBooting,
		Token:          "install-tok-wire-1",
		TokenExpiresAt: time.Now().Add(time.Hour).Unix(),
	}
	if _, err := app.Store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/answer/task-wire-1/install-tok-wire-1/ks.cfg", nil)
	rec := httptest.NewRecorder()
	app.GatewayServer.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/answer 状态码 = %d, want %d，body:\n%s", resp.StatusCode, http.StatusOK, body)
	}

	wantKey := cfg.Gateway.RootSSHPublicKeys[0]
	if !strings.Contains(string(body), wantKey) {
		t.Errorf("应答文件正文应含配置里的公钥 %q，说明 cfg.Gateway.RootSSHPublicKeys 未能"+
			"一路传到渲染输出，got body:\n%s", wantKey, body)
	}
	if strings.Contains(string(body), "REPLACE-ME") {
		t.Errorf("应答文件正文仍含旧占位公钥标记 REPLACE-ME，说明占位值未被真实配置值替换，"+
			"got body:\n%s", body)
	}
}

// TestBuildApp_AdmissionLimitPropagatedToStatemachinePackage 验证装配点
// 把 cfg.Scan.AdmissionLimit 传给了 statemachine.SetGlobalAdmissionLimit
// （Phase 4 新增，SPEC §10 失败模式 11）——这是 StartBoot 的准入检查实际
// 生效的上限来源（见 internal/statemachine/admission.go 的
// globalAdmissionLimit 文档：跨越 StartBoot 调用点与本装配点两个不同
// 代码位置共享同一个值，用包级 atomic 变量传递）。用一个恰好等于配置值
// 的并发场景间接验证：造够配置的上限数量的 booting 任务后，再用
// app.StateMachine.StartBoot 引导下一台，若配置确实生效，这一次必须被
// 准入检查拒绝。
//
// 测试结束前恢复默认值——globalAdmissionLimit 是 internal/statemachine
// 包级状态，跨测试用例共享（本测试文件与该包的其它测试共享同一个进程），
// 若不恢复会让测试顺序影响其它用例的结果。
func TestBuildApp_AdmissionLimitPropagatedToStatemachinePackage(t *testing.T) {
	defer statemachine.SetGlobalAdmissionLimit(20)

	cfg := testConfig(t)
	cfg.Scan.AdmissionLimit = 2
	app := buildAppWithDBForTest(t, cfg)
	ctx := context.Background()

	artifact := model.ArtifactSet{
		ID: "artifact-admission-probe", OSFamily: model.OSFamilyRHEL,
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}
	if _, err := app.Store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	// 造 2 台（等于配置的上限）已在 booting 的任务。
	for i := 0; i < 2; i++ {
		taskID := "task-admission-probe-occupying-" + string(rune('a'+i))
		if _, err := app.Store.CreateTask(ctx, model.Task{
			ID: taskID, ArtifactSetID: artifact.ID, State: model.TaskStatePending, CurrentPhase: model.PhasePending,
		}); err != nil {
			t.Fatalf("CreateTask 失败: %v", err)
		}
		if _, err := app.StateMachine.StartBoot(ctx, taskID, model.ArchX86_64, model.FirmwareUEFI); err != nil {
			t.Fatalf("占位任务 %s 的 StartBoot 失败: %v", taskID, err)
		}
	}

	// 第三台：若 cfg.Scan.AdmissionLimit=2 确实传给了 statemachine 包，
	// 这次 StartBoot 必须被准入检查拒绝；若装配点忘记调用
	// SetGlobalAdmissionLimit（仍是默认值 20），这次会成功，测试失败并
	// 清楚指出「配置的并发上限没有生效」。
	pendingID := "task-admission-probe-pending"
	if _, err := app.Store.CreateTask(ctx, model.Task{
		ID: pendingID, ArtifactSetID: artifact.ID, State: model.TaskStatePending, CurrentPhase: model.PhasePending,
	}); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}
	if _, err := app.StateMachine.StartBoot(ctx, pendingID, model.ArchX86_64, model.FirmwareUEFI); err == nil {
		t.Fatalf("cfg.Scan.AdmissionLimit=2 且已有 2 台在装时，第 3 台 StartBoot 未被准入检查" +
			"拒绝——说明 buildAppWithDB 没有把配置的并发上限传给 statemachine.SetGlobalAdmissionLimit")
	}
}

// TestBuildApp_AutoRetryWiredIntoScanTimeouts 验证 app.StateMachine 是
// autoRetryScanDecorator（Phase 4 新增，实现计划 phase4-01 任务 4）：
// 调用它的 ScanTimeouts 方法，除了跑既有的超时扫描，还应该按
// cfg.Scan.AutoRetryEnabled/AutoRetryMaxAttempts 驱动一轮
// AutoRetryFailedTasks。用一个已经是 failed、失败原因在白名单内
// （boot_timeout）的任务验证：cfg.Scan.AutoRetryEnabled=true 时，调用
// app.StateMachine.ScanTimeouts 之后该任务应该被重置回 pending。
func TestBuildApp_AutoRetryWiredIntoScanTimeouts(t *testing.T) {
	cfg := testConfig(t)
	cfg.Scan.AutoRetryEnabled = true
	cfg.Scan.AutoRetryMaxAttempts = 1
	app := buildAppWithDBForTest(t, cfg)
	ctx := context.Background()

	artifact := model.ArtifactSet{
		ID: "artifact-autoretry-wire", OSFamily: model.OSFamilyRHEL,
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}
	if _, err := app.Store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	taskID := "task-autoretry-wire"
	if _, err := app.Store.CreateTask(ctx, model.Task{
		ID: taskID, ArtifactSetID: artifact.ID, State: model.TaskStateFailed,
		CurrentPhase: model.PhasePackages, AttemptCount: 1,
		FailureReason: model.FailureReasonBootTimeout,
	}); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	if _, err := app.StateMachine.ScanTimeouts(ctx, app.Clock.Now()); err != nil {
		t.Fatalf("ScanTimeouts 返回了错误: %v", err)
	}

	got, err := app.Store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if got.State != model.TaskStatePending {
		t.Errorf("cfg.Scan.AutoRetryEnabled=true 时，调用 app.StateMachine.ScanTimeouts 之后"+
			"failure_reason=boot_timeout 的任务 State = %q, want %q——说明装配点没有把"+
			"AutoRetryFailedTasks 接进 ScanTimeouts 的调用路径", got.State, model.TaskStatePending)
	}
}

// TestBuildApp_AutoRetryDisabledByDefaultConfig 是上一条的对照组：
// cfg.Scan.AutoRetryEnabled 保持零值 false（testConfig 不显式设置）时，
// 调用 ScanTimeouts 不应该触发任何自动重试——默认关闭是计划任务 4 刻意
// 的设计决定。
func TestBuildApp_AutoRetryDisabledByDefaultConfig(t *testing.T) {
	cfg := testConfig(t) // 不设置 Scan.AutoRetryEnabled，保持零值 false
	app := buildAppWithDBForTest(t, cfg)
	ctx := context.Background()

	artifact := model.ArtifactSet{
		ID: "artifact-autoretry-disabled-wire", OSFamily: model.OSFamilyRHEL,
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}
	if _, err := app.Store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	taskID := "task-autoretry-disabled-wire"
	if _, err := app.Store.CreateTask(ctx, model.Task{
		ID: taskID, ArtifactSetID: artifact.ID, State: model.TaskStateFailed,
		CurrentPhase: model.PhasePackages, AttemptCount: 1,
		FailureReason: model.FailureReasonBootTimeout,
	}); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	if _, err := app.StateMachine.ScanTimeouts(ctx, app.Clock.Now()); err != nil {
		t.Fatalf("ScanTimeouts 返回了错误: %v", err)
	}

	got, err := app.Store.GetTask(ctx, taskID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if got.State != model.TaskStateFailed {
		t.Errorf("cfg.Scan.AutoRetryEnabled 为默认 false 时，任务 State = %q, want 保持 %q 不变——"+
			"自动重装是需要运维明确同意的行为，默认必须关闭", got.State, model.TaskStateFailed)
	}
}

// TestBuildApp_AlertingNotifyWiredAheadOfBuiltinNotify 验证装配点用
// provider.NewAlertingNotify 包了一层内置 Notify（Phase 4 新增，实现计划
// phase4-01 任务 5），再传给 provider.RegisterLifecycleHooks——这是
// internal/provider/notify.go 自己文档记录的契约缺口的落地方式。
//
// 内置 Notify（builtinNotify）的 Emit 直接调用 log.Printf 落一行结构化
// 日志，没有其它可观察的副作用（它本身不落库、不发 HTTP），因此本测试
// 用 log.SetOutput 捕获标准 log 包的输出来断言：一个不可重装失败原因
// （reboot_timeout）的 task.failed 事件经过完整的装配链路（EventBus →
// RegisterLifecycleHooks 订阅的 onTaskFailed → AlertingNotify.Emit 组装
// 消息 → 内置 Notify.Emit 落日志）之后，日志行里必须含 AlertingNotify
// 组装的「请勿重装」文案——若装配点忘记用 NewAlertingNotify 包一层、直接
// 把裸的 providers.Notify 传给 RegisterLifecycleHooks，日志行只会是
// builtinNotify.Emit 原始打印的 event/task_id/detail 三元组，不会出现
// 「请勿重装」这个由 AlertingNotify.buildMessage 才会产出的措辞。
func TestBuildApp_AlertingNotifyWiredAheadOfBuiltinNotify(t *testing.T) {
	var buf strings.Builder
	prevOutput := log.Writer()
	prevFlags := log.Flags()
	log.SetOutput(&buf)
	log.SetFlags(0)
	defer func() {
		log.SetOutput(prevOutput)
		log.SetFlags(prevFlags)
	}()

	cfg := testConfig(t)
	app := buildAppWithDBForTest(t, cfg)
	ctx := context.Background()

	artifact := model.ArtifactSet{
		ID: "artifact-alerting-wire", OSFamily: model.OSFamilyRHEL,
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}
	if _, err := app.Store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	taskID := "task-alerting-wire"
	created, err := app.Store.CreateTask(ctx, model.Task{
		ID: taskID, ArtifactSetID: artifact.ID, State: model.TaskStateRebooting,
		CurrentPhase: model.PhaseRebooting, PhaseEnteredAt: app.Clock.Now(),
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	failed, err := app.StateMachine.Fail(ctx, created.ID, model.FailureReasonRebootTimeout, model.FailureDetail{Phase: model.PhaseRebooting})
	if err != nil {
		t.Fatalf("Fail 失败: %v", err)
	}
	app.Events.Publish(contract.LifecycleEventTaskFailed, failed)

	logged := buf.String()
	if !strings.Contains(logged, "请勿重装") {
		t.Errorf("task.failed（failure_reason=reboot_timeout）事件经过完整装配链路之后，"+
			"日志输出未含「请勿重装」文案，got log=%q——说明装配点没有用"+
			"provider.NewAlertingNotify 包住传给 RegisterLifecycleHooks 的 Notify 实例"+
			"（只有 AlertingNotify.buildMessage 会产出这个措辞，裸的内置 Notify 只会"+
			"打印 event/task_id/detail 三元组）", logged)
	}
}

// buildAppWithDBForTest 是给其它测试文件（server_test.go 等）复用的辅助：
// 用内存 SQLite 快速装配一个 App，不落磁盘目录。
func buildAppWithDBForTest(t *testing.T, cfg *config.Config) *App {
	t.Helper()
	db, err := sqlite.Open("file::memory:?cache=shared")
	if err != nil {
		t.Fatalf("打开内存数据库失败: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	app, err := buildAppWithDB(cfg, db)
	if err != nil {
		t.Fatalf("buildAppWithDB 失败: %v", err)
	}
	return app
}
