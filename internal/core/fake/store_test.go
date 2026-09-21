// 本文件锁住内存 Store 的行为。对应实现计划任务 17，依据 SPEC §2.5、§2.6、
// §2.7、§3.3、§3.4、§3.6、§4.3、§4.4、§4.7、§5.5、§12.8。
//
// 主体断言全部委托给 conformance.RunConformanceSuite——它是 Store 的可执行
// 行为契约，内存实现与将来的 SQLite 实现（M2）跑同一套断言，保证 M3-M8
// 用内存实现写的测试换成 SQLite 后行为不变（计划任务 16）。
//
// 本文件额外补的几条断言是 conformance 套件不覆盖的、内存实现自身特有的
// 风险点：深拷贝（计划任务 17 设计要点明确要求，返回内部指针会让一个测试
// 的修改渗进另一个测试，这类 bug 极难定位）。
package fake

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/contract/conformance"
	"github.com/lober/lober/internal/core/model"
)

// TestConformance 把内存 Store 接入 Store 的可执行行为契约。这是本任务
// 「实现流程」第一步要求的入口——先写这个文件（此刻会全红，因为 Store 还
// 不存在），再逐个实现接口方法让套件变绿。
func TestConformance(t *testing.T) {
	conformance.RunConformanceSuite(t, func() contract.Store {
		return NewStore()
	})
}

// TestStore_深拷贝_Task切片字段不被外部修改渗透 锁住计划任务 17 设计要点：
// 所有返回值必须深拷贝，含切片与 map 的字段（hook digests、参数）要逐个拷。
//
// 如果 CreateTask 内部直接持有调用方传入的切片/map 引用（而不是拷贝），
// 调用方后续修改自己手里的切片会悄悄改变 Store 内部状态；反过来，如果
// GetTask 返回的切片/map 是内部存储的引用而非拷贝，调用方修改返回值会污染
// Store 内部状态——两个方向都要挡住，这类 bug 极难定位（计划任务 17 明确
// 指出：一个测试的修改渗入另一个测试）。
func TestStore_深拷贝_Task切片字段不被外部修改渗透(t *testing.T) {
	ctx := context.Background()
	store := NewStore()

	original := model.Task{
		ID:              "task-deepcopy",
		HostSN:          "SN-deepcopy",
		TemplateID:      "tpl-deepcopy",
		TemplateVersion: 1,
		ArtifactSetID:   "artifact-deepcopy",
		HookDigests:     []string{"digest-1", "digest-2"},
		FormData:        map[string]string{"hostname": "node-01"},
		ResolvedParams:  map[string]string{"biz_ip": "10.0.0.1"},
		State:           model.TaskStatePending,
		CurrentPhase:    model.PhasePending,
	}

	created, err := store.CreateTask(ctx, original)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	// 方向一：调用方创建之后修改自己手里的切片/map，不能影响库内已存的值。
	original.HookDigests[0] = "已被外部篡改"
	original.FormData["hostname"] = "已被外部篡改"

	reread, err := store.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if reread.HookDigests[0] != "digest-1" {
		t.Errorf("外部修改 CreateTask 调用方持有的 HookDigests 切片之后，库内值也被改成 %q，want 保持 %q —— "+
			"CreateTask 必须深拷贝传入的切片，否则调用方手里的切片与库内状态共享底层数组，"+
			"任何一方的后续修改都会污染另一方", reread.HookDigests[0], "digest-1")
	}
	if reread.FormData["hostname"] != "node-01" {
		t.Errorf("外部修改 CreateTask 调用方持有的 FormData map 之后，库内值也被改成 %q，want 保持 %q —— "+
			"同上，map 同样必须深拷贝", reread.FormData["hostname"], "node-01")
	}

	// 方向二：调用方拿到 GetTask 的返回值后修改它，不能影响库内状态，
	// 第二次 GetTask 必须仍然读到原始值。
	reread.HookDigests[1] = "已被外部篡改"
	reread.FormData["hostname"] = "已被外部篡改"
	reread.ResolvedParams["biz_ip"] = "已被外部篡改"

	againRead, err := store.GetTask(ctx, created.ID)
	if err != nil {
		t.Fatalf("第二次 GetTask 失败: %v", err)
	}
	if againRead.HookDigests[1] != "digest-2" {
		t.Errorf("外部修改 GetTask 返回值的 HookDigests 切片之后，库内值也被改成 %q，want 保持 %q —— "+
			"GetTask 必须返回深拷贝而不是内部存储的引用，否则一次调用方的误改会永久污染 Store",
			againRead.HookDigests[1], "digest-2")
	}
	if againRead.FormData["hostname"] != "node-01" {
		t.Errorf("外部修改 GetTask 返回值的 FormData 之后，库内值也被改成 %q，want 保持 %q",
			againRead.FormData["hostname"], "node-01")
	}
	if againRead.ResolvedParams["biz_ip"] != "10.0.0.1" {
		t.Errorf("外部修改 GetTask 返回值的 ResolvedParams 之后，库内值也被改成 %q，want 保持 %q",
			againRead.ResolvedParams["biz_ip"], "10.0.0.1")
	}
}

// TestStore_深拷贝_ArtifactPaths不被外部修改渗透 锁住 ArtifactPaths 里
// Initrd 切片与 Extra map 的深拷贝——conformance 套件的
// testArtifactSetContentRoundTrip 只验证「写入什么读出什么」，不验证「读出
// 之后修改返回值是否会污染库内状态」，这条是本文件专门补的方向。
func TestStore_深拷贝_ArtifactPaths不被外部修改渗透(t *testing.T) {
	ctx := context.Background()
	store := NewStore()

	created, err := store.CreateArtifactSet(ctx, model.ArtifactSet{
		ID:       "artifact-deepcopy-paths",
		Name:     "rocky9.4",
		Arch:     model.ArchX86_64,
		Firmware: model.FirmwareAny,
		State:    model.ArtifactSetImporting,
	})
	if err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	paths := model.ArtifactPaths{
		Kernel: "rocky9.4/vmlinuz",
		Initrd: []string{"rocky9.4/initrd.img"},
		Repo:   "rocky9.4/repo/",
		Extra:  map[string]string{"boot_cfg": "rocky9.4/boot.cfg"},
	}
	if _, err := store.SetArtifactSetContent(ctx, created.ID, model.OSFamilyRHEL, paths); err != nil {
		t.Fatalf("SetArtifactSetContent 失败: %v", err)
	}

	got, err := store.GetArtifactSet(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetArtifactSet 失败: %v", err)
	}
	got.Paths.Initrd[0] = "已被外部篡改"
	got.Paths.Extra["boot_cfg"] = "已被外部篡改"

	againGot, err := store.GetArtifactSet(ctx, created.ID)
	if err != nil {
		t.Fatalf("第二次 GetArtifactSet 失败: %v", err)
	}
	if againGot.Paths.Initrd[0] != "rocky9.4/initrd.img" {
		t.Errorf("外部修改 GetArtifactSet 返回值的 Paths.Initrd 之后，库内值也被改成 %q，want 保持 %q —— "+
			"渲染器与导入 job（M4/M7）都会持有 GetArtifactSet 的返回值，"+
			"若这里共享底层数组，一处误改会让所有后续引导该制品集的机器都拿到错误路径",
			againGot.Paths.Initrd[0], "rocky9.4/initrd.img")
	}
	if againGot.Paths.Extra["boot_cfg"] != "rocky9.4/boot.cfg" {
		t.Errorf("外部修改 GetArtifactSet 返回值的 Paths.Extra 之后，库内值也被改成 %q，want 保持 %q",
			againGot.Paths.Extra["boot_cfg"], "rocky9.4/boot.cfg")
	}
}

// TestStore_深拷贝_GetFacts不被外部修改渗透 锁住 FactStore.GetFacts 的深
// 拷贝——conformance 套件的 testMergeFactsIsMergeNotReplace 只验证合并语义
// 本身，不验证「读出的 map 是否与内部存储共享引用」。
func TestStore_深拷贝_GetFacts不被外部修改渗透(t *testing.T) {
	ctx := context.Background()
	store := NewStore()
	created, err := store.CreateTask(ctx, model.Task{
		ID:     "task-facts-deepcopy",
		HostSN: "SN-facts-deepcopy",
		State:  model.TaskStatePending,
	})
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	if err := store.MergeFacts(ctx, created.ID, map[string]string{"raid_model": "PERC-H755"}); err != nil {
		t.Fatalf("MergeFacts 失败: %v", err)
	}

	got, err := store.GetFacts(ctx, created.ID)
	if err != nil {
		t.Fatalf("GetFacts 失败: %v", err)
	}
	got["raid_model"] = "已被外部篡改"

	againGot, err := store.GetFacts(ctx, created.ID)
	if err != nil {
		t.Fatalf("第二次 GetFacts 失败: %v", err)
	}
	if againGot["raid_model"] != "PERC-H755" {
		t.Errorf("外部修改 GetFacts 返回值之后，库内值也被改成 %q，want 保持 %q —— "+
			"多个 hook 会先 GetFacts 再本地处理，若返回的 map 是内部引用，"+
			"一个 hook 的本地误改会污染所有后续读取者看到的 facts（SPEC §5.5）",
			againGot["raid_model"], "PERC-H755")
	}
}

// TestStore_满足组合接口与事务运行接口 是计划任务 17「实现流程」第四步
// 要求的编译期断言：本类型必须满足组合 Store 接口与事务运行接口。写成
// 普通测试函数（而非包级 var _ 断言）是为了在测试输出里显式留痕——
// 但真正的强制力来自 store.go 末尾的编译期断言，这里只是重复确认一次，
// 帮助阅读测试文件的人一眼看到这条约束确实被覆盖。
func TestStore_满足组合接口与事务运行接口(t *testing.T) {
	var s contract.Store = NewStore()
	var tx contract.TxRunner = NewStore()
	if s == nil || tx == nil {
		t.Fatal("NewStore() 返回值不满足 contract.Store 或 contract.TxRunner——不应发生，" +
			"说明编译期断言与本测试的静态类型检查出现了不一致")
	}
}

// TestStore_RunInTx_拒绝嵌套调用 锁住 TxRunner 契约明确要求的行为：实现
// 必须拒绝嵌套调用（fn 内部再次调用 RunInTx）——真实 SQLite 实现下嵌套会
// 死锁；内存实现虽然退化为直接执行、没有真正的锁等待风险，但契约要求所有
// 实现都拒绝嵌套，否则依赖内存实现写测试的模块（M3-M8）观察到的行为会与
// 换成 SQLite 后的行为不一致——这正是 conformance 机制存在的意义，
// 但事务嵌套这条不属于 Store 接口方法，conformance 套件不覆盖，因此在本
// 文件单独锁住。
func TestStore_RunInTx_拒绝嵌套调用(t *testing.T) {
	ctx := context.Background()
	store := NewStore()

	outerErr := store.RunInTx(ctx, func(ctx context.Context, s contract.Store) error {
		innerErr := store.RunInTx(ctx, func(ctx context.Context, s contract.Store) error {
			return nil
		})
		if innerErr == nil {
			t.Error("嵌套调用 RunInTx 未返回错误，want 拒绝——" +
				"contract.TxRunner 明确要求实现必须拒绝嵌套调用，" +
				"真实 SQLite 实现下嵌套会在单写者模型上死锁，" +
				"内存实现必须表现出同样的拒绝行为，否则依赖它写的测试" +
				"在换成 SQLite 后会突然死锁")
		}
		return nil
	})
	if outerErr != nil {
		t.Fatalf("最外层 RunInTx 失败: %v，want 成功（只有嵌套的内层调用应被拒绝）", outerErr)
	}
}
