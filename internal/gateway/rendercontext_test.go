// 本文件测试渲染上下文构造（对应实现计划任务 5，依据 SPEC §1.5、§4.3、
// §4.4、§5.1）。
//
// 本轮新增：buildRenderContext 必须把 Deps.RootSSHPublicKeys 原样传给
// RenderContext.RootSSHPublicKeys（SPEC §1.5：root 不设可复用密码，注入
// SSH 公钥并关闭密码登录——渲染器需要这个值才能在应答文件里写入真实公钥，
// 不再写死占位符）。
package gateway

import (
	"context"
	"testing"

	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// rootSSHPublicKeysTestFixture 构造一份能让 buildRenderContext 走完全部
// 四次查库（模板/制品集/主机/facts）的最小前置数据，落在同一个 fake.Store
// 实例里。与 boot_grant_test.go 的 newGrantFixture 是同一种搭法（模板/
// 制品集/主机/任务四件套），本文件独立写一份而不是复用那个辅助函数，
// 保持本文件自成一体，不依赖其它文件的测试辅助函数是否发生变化。
func rootSSHPublicKeysTestFixture(t *testing.T, keys []string) (*Server, model.Task) {
	t.Helper()
	store := fake.NewStore()
	ctx := context.Background()

	host := model.Host{SN: "SN-SSHKEY-1", MAC: "aa:bb:cc:dd:ee:ff", UUID: "uuid-sshkey-1", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	tpl := model.Template{ID: "tpl-sshkey-1", Name: "rocky9", ArtifactSetID: "art-sshkey-1", Version: 1, State: model.TemplateStateActive}
	if _, err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	artifact := model.ArtifactSet{
		ID: "art-sshkey-1", Name: "rocky9.4", OSFamily: model.OSFamilyRHEL, OSVersion: "9.4",
		Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady,
	}
	if _, err := store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	task := model.Task{
		ID: "task-sshkey-1", HostSN: "SN-SSHKEY-1", HostMAC: "aa:bb:cc:dd:ee:ff", HostUUID: "uuid-sshkey-1",
		TemplateID: "tpl-sshkey-1", TemplateVersion: 1, ArtifactSetID: "art-sshkey-1",
		State: model.TaskStatePending, CurrentPhase: model.PhasePending,
	}
	created, err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	deps := newTestDeps()
	deps.TaskStore = store
	deps.FactStore = store
	deps.EventStore = store
	deps.TemplateStore = store
	deps.ArtifactSetStore = store
	deps.HostStore = store
	deps.RootSSHPublicKeys = keys

	return NewServer(deps), created
}

// TestBuildRenderContext_RootSSHPublicKeysPropagated 锁住本轮核心行为：
// Deps.RootSSHPublicKeys 原样传进 RenderContext.RootSSHPublicKeys——这是
// 部署级配置值到渲染器输入之间唯一的传递路径，任何一处漏传都会让渲染器
// 在真正装机时拿到空值，进而报错拒绝渲染（render 包的纵深防御），而不是
// 像本轮修复前那样静默产出一台没人能登录的机器。
func TestBuildRenderContext_RootSSHPublicKeysPropagated(t *testing.T) {
	keys := []string{
		"ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIGZ5ZDhhOWMwZTNiNGY1YzZkN2E4YjljMGUzYjRmNWM2 ops@lober",
	}
	srv, task := rootSSHPublicKeysTestFixture(t, keys)

	got, err := srv.buildRenderContext(context.Background(), task, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("buildRenderContext 返回错误: %v", err)
	}

	if len(got.RootSSHPublicKeys) != 1 || got.RootSSHPublicKeys[0] != keys[0] {
		t.Errorf("RenderContext.RootSSHPublicKeys = %v, want %v —— "+
			"Deps.RootSSHPublicKeys 必须原样传给渲染上下文，这是配置值到渲染器"+
			"输入之间唯一的传递路径", got.RootSSHPublicKeys, keys)
	}
}

// TestBuildRenderContext_MultipleRootSSHPublicKeysPropagated 锁住多把公钥
// 全部传递，不能只传第一把（团队多人且轮换期需要新旧并存）。
func TestBuildRenderContext_MultipleRootSSHPublicKeysPropagated(t *testing.T) {
	keys := []string{
		"ssh-ed25519 AAAAalice alice@lober",
		"ssh-rsa AAAAbob bob@lober",
	}
	srv, task := rootSSHPublicKeysTestFixture(t, keys)

	got, err := srv.buildRenderContext(context.Background(), task, model.FirmwareUEFI)
	if err != nil {
		t.Fatalf("buildRenderContext 返回错误: %v", err)
	}

	if len(got.RootSSHPublicKeys) != 2 {
		t.Fatalf("RenderContext.RootSSHPublicKeys 长度 = %d, want 2, got: %v",
			len(got.RootSSHPublicKeys), got.RootSSHPublicKeys)
	}
}
