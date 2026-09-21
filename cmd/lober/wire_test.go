package main

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/config"
	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
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
