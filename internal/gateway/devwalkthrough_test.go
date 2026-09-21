// 本文件是计划任务 9「手工验证服务器」的替代实现。计划原文要求一个独立
// 的 cmd/gateway-devserver 小程序，但本模块只拥有 internal/gateway/ 下的
// 文件（编排方的铁律：只写你拥有的文件），无法创建 cmd/ 目录下的文件；
// 任务说明明确指示这种情形改成「httptest 内跑」并在 notes 里说明，本文件
// 即该说明的落地。
//
// 用一个真的会做状态转换的简易状态机（不是桩）驱动 M1 的内存 Store，
// 起一个 httptest.Server，预置一台主机与一个 pending 任务，走完整条 curl
// 等价序列：取首跳脚本 → 取菜单（拿 token）→ 取应答文件 → 按顺序打十一个
// 信号 → 确认状态是 done → 再取一次菜单确认拿到 no_task。
//
// 为什么不用桩：桩返回固定值，看不到状态真的在推进。要观察
// pending → booting → installing → rebooting → initializing → done
// 真实发生，需要一个会改内存 Store 的实现。这个简易实现只服务于本文件的
// 手工/自动化验证走查，不导出、不被生产代码路径引用。
//
// syslog 这条通道本测试覆盖不到（它是 UDP，这里走的是 HTTP），由
// syslog_test.go 的自动化测试单独覆盖——不假装测过了。
package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// devStateMachine 是一个真的会做状态转换的简易 contract.StateMachine 实现，
// 直接读写传入的 *fake.Store。只实现本文件走查序列需要的行为，不追求
// 覆盖 contract.StateMachine 文档的每一条边界情况（那是 M3 的职责与测试
// 范围）。
// devStateMachine 不持有可控时钟：token 过期判据依赖真实 time.Now()（见
// StartBoot 内部注释），而不是 model.Task 各时间字段本身的记录方式，
// 因此这个简易实现直接用标准库 time.Now()，不需要 fake.Clock。
type devStateMachine struct {
	store *fake.Store
}

var _ contract.StateMachine = (*devStateMachine)(nil)

func (d *devStateMachine) StartBoot(ctx context.Context, taskID string, actualArch model.Arch, actualFirmware model.FirmwareMode) (model.Task, error) {
	current, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if _, err := d.store.CASTaskState(ctx, taskID, current.State, current.Version, model.TaskStateBooting); err != nil {
		return model.Task{}, err
	}
	token := "dev-install-token"
	// TokenExpiresAt 用真实 time.Now()，不用 d.clock（可控时钟）：
	// fake.Store.GetTaskByToken 的过期判据硬编码用真实 time.Now().Unix()
	// （见 fake/store.go 该方法的注释），本测试驱动的是真实 HTTP 往返，
	// 若这里改用可控时钟的固定起点（2026-01-01），在固定起点早于运行时刻
	// 的真实日期时会导致 token 刚签发就被判定为已过期——这正是本测试第
	// 一次运行时踩到的坑，记录在此避免复现。
	expiresAt := time.Now().Unix() + int64(model.InstallTokenTTL.Seconds())
	attempt := current.AttemptCount + 1
	return d.store.MutateTaskFields(ctx, taskID, model.TaskMutation{
		Token: &token, TokenExpiresAt: &expiresAt, AttemptCount: &attempt,
	})
}

func (d *devStateMachine) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	current, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if current.State != model.TaskStateBooting {
		return current, nil // 幂等：已经在 installing 或之后，视为已处理过
	}
	return d.store.CASTaskState(ctx, taskID, model.TaskStateBooting, current.Version, model.TaskStateInstalling)
}

func (d *devStateMachine) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	entry, ok := model.LookupSignal(signal)
	if !ok {
		return contract.SignalOutcome{}, &stubError{"devStateMachine: 未知信号"}
	}
	current, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return contract.SignalOutcome{}, err
	}
	if current.State.IsTerminal() || entry.Seq <= current.LastSeq {
		return contract.SignalOutcome{Accepted: true, StateChanged: false, Task: current}, nil
	}
	if entry.Seq == 0 {
		// hook-report：永不推进水位，只记事件（这里没有事件表写入，网关
		// 自己在 signal.go 里写；本状态机只负责状态推进）。
		return contract.SignalOutcome{Accepted: true, StateChanged: false, Task: current}, nil
	}

	updated, err := d.store.CASTaskState(ctx, taskID, current.State, current.Version, entry.State)
	if err != nil {
		return contract.SignalOutcome{}, err
	}
	newSeq := entry.Seq
	newPhase := entry.Phase
	updated, err = d.store.MutateTaskFields(ctx, taskID, model.TaskMutation{LastSeq: &newSeq, CurrentPhase: &newPhase})
	if err != nil {
		return contract.SignalOutcome{}, err
	}
	return contract.SignalOutcome{Accepted: true, StateChanged: true, Task: updated}, nil
}

func (d *devStateMachine) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	token := "dev-finalize-token"
	// 同 StartBoot：用真实 time.Now()，理由见上方注释。
	expiresAt := time.Now().Unix() + int64(model.FinalizeTokenTTL.Seconds())
	return d.store.MutateTaskFields(ctx, taskID, model.TaskMutation{FinalizeToken: &token, FinalizeExpiresAt: &expiresAt})
}

func (d *devStateMachine) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	current, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	if _, err := d.store.CASTaskState(ctx, taskID, current.State, current.Version, model.TaskStateFailed); err != nil {
		return model.Task{}, err
	}
	return d.store.MutateTaskFields(ctx, taskID, model.TaskMutation{FailureReason: &reason, FailureDetail: &detail})
}

func (d *devStateMachine) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	current, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	return d.store.CASTaskState(ctx, taskID, current.State, current.Version, model.TaskStateFailed)
}

func (d *devStateMachine) Retry(ctx context.Context, taskID string) (model.Task, error) {
	current, err := d.store.GetTask(ctx, taskID)
	if err != nil {
		return model.Task{}, err
	}
	return d.store.CASTaskState(ctx, taskID, current.State, current.Version, model.TaskStatePending)
}

func (d *devStateMachine) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	return 0, nil
}

// TestDevWalkthrough_FullCurlEquivalentSequence 是任务 9 的替代实现：走完
// 整条 curl 等价序列，观察状态真实推进到 done，最后确认再取一次菜单能拿到
// no_task。
func TestDevWalkthrough_FullCurlEquivalentSequence(t *testing.T) {
	store := fake.NewStore()
	clock := fake.NewClock()
	ctx := t.Context()

	host := model.Host{SN: "DEV-SN-1", MAC: "de:ad:be:ef:00:01", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}
	tpl := model.Template{ID: "dev-tpl", Version: 1, ArtifactSetID: "dev-art", State: model.TemplateStateActive}
	if _, err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}
	artifact := model.ArtifactSet{ID: "dev-art", OSFamily: model.OSFamilyRHEL, Arch: model.ArchX86_64, Firmware: model.FirmwareAny, State: model.ArtifactSetReady}
	if _, err := store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}
	task := model.Task{
		ID: "dev-task-1", HostSN: "DEV-SN-1", HostMAC: "de:ad:be:ef:00:01",
		TemplateID: "dev-tpl", TemplateVersion: 1, ArtifactSetID: "dev-art",
		State: model.TaskStatePending, CurrentPhase: model.PhasePending,
	}
	if _, err := store.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	deps := Deps{
		TaskStore: store, FactStore: store, EventStore: store,
		TemplateStore: store, ArtifactSetStore: store, HostStore: store,
		StateMachine: &devStateMachine{store: store},
		Renderer:     newRendererStub(),
		LogStore:     newLogStoreStub(),
		Clock:        clock,
		Bases:        model.BaseURLs{GatewayBase: "http://dev-gw", ArtifactBase: "http://dev-art"},
	}
	srv := NewServer(deps)
	ts := httptest.NewServer(srv)
	defer ts.Close()

	// 1) 取首跳脚本。
	bootScriptResp, err := http.Get(ts.URL + "/boot.ipxe")
	if err != nil {
		t.Fatalf("GET /boot.ipxe 失败: %v", err)
	}
	bootScriptBody, _ := io.ReadAll(bootScriptResp.Body)
	bootScriptResp.Body.Close()
	if !strings.Contains(string(bootScriptBody), "chain") {
		t.Fatalf("/boot.ipxe 响应不含 chain 命令: %s", bootScriptBody)
	}

	// 2) 取菜单（拿 token）。
	bootQuery := url.Values{"sn": {"DEV-SN-1"}, "mac": {"de:ad:be:ef:00:01"}, "uuid": {""}, "arch": {"x86_64"}, "plat": {"efi"}}
	menuResp, err := http.Get(ts.URL + "/v1/boot?" + bootQuery.Encode())
	if err != nil {
		t.Fatalf("GET /v1/boot 失败: %v", err)
	}
	menuBody, _ := io.ReadAll(menuResp.Body)
	menuResp.Body.Close()
	if menuResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/boot 状态码 = %d, want 200", menuResp.StatusCode)
	}
	afterBoot, err := store.GetTask(ctx, "dev-task-1")
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if afterBoot.State != model.TaskStateBooting {
		t.Fatalf("取菜单后任务状态 = %q, want %q（菜单内容: %s）", afterBoot.State, model.TaskStateBooting, menuBody)
	}
	installToken := afterBoot.Token
	if installToken == "" {
		t.Fatal("取菜单后未签发装机 token")
	}

	// 3) 取应答文件（触发 booting → installing）。
	answerURL := ts.URL + "/v1/answer/dev-task-1/" + installToken + "/ks.cfg"
	answerResp, err := http.Get(answerURL)
	if err != nil {
		t.Fatalf("GET /v1/answer 失败: %v", err)
	}
	answerResp.Body.Close()
	if answerResp.StatusCode != http.StatusOK {
		t.Fatalf("GET /v1/answer 状态码 = %d, want 200", answerResp.StatusCode)
	}
	afterAnswer, err := store.GetTask(ctx, "dev-task-1")
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if afterAnswer.State != model.TaskStateInstalling {
		t.Fatalf("取应答文件后任务状态 = %q, want %q", afterAnswer.State, model.TaskStateInstalling)
	}

	// 4) 按顺序打十一个信号。
	signalSeq := []model.SignalKind{
		model.SignalPreStart, model.SignalPreDone,
		model.SignalPackagesStart, model.SignalPackagesDone,
		model.SignalPostStart, model.SignalPostDone,
		model.SignalRebooting, model.SignalBooted,
		model.SignalInitStart, model.SignalInitDone, model.SignalReady,
	}
	for i, kind := range signalSeq {
		signalURL := ts.URL + "/v1/signal/dev-task-1/" + installToken + "/" + string(kind) +
			"?rc=0&seq=" + strconv.Itoa(i+1)
		req, err := http.NewRequest(http.MethodPost, signalURL, strings.NewReader("dev walkthrough log line"))
		if err != nil {
			t.Fatalf("构造信号请求失败: %v", err)
		}
		req.Header.Set("Content-Type", "text/plain")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST 信号 %s 失败: %v", kind, err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("信号 %s 状态码 = %d, want 200", kind, resp.StatusCode)
		}
	}

	// 5) 确认状态是 done。
	final, err := store.GetTask(ctx, "dev-task-1")
	if err != nil {
		t.Fatalf("GetTask 失败: %v", err)
	}
	if final.State != model.TaskStateDone {
		t.Fatalf("打完十一个信号后任务状态 = %q, want %q", final.State, model.TaskStateDone)
	}

	// 6) 再取一次菜单确认拿到 no_task（任务已终态，不再是 pending，
	// FindPendingTaskByIdentity 查不到）。
	secondMenuResp, err := http.Get(ts.URL + "/v1/boot?" + bootQuery.Encode())
	if err != nil {
		t.Fatalf("第二次 GET /v1/boot 失败: %v", err)
	}
	secondMenuBody, _ := io.ReadAll(secondMenuResp.Body)
	secondMenuResp.Body.Close()
	if secondMenuResp.StatusCode != http.StatusOK {
		t.Fatalf("第二次 GET /v1/boot 状态码 = %d, want 200", secondMenuResp.StatusCode)
	}
	wantNoTask := contract.NoTaskScript("DEV-SN-1")
	if string(secondMenuBody) != wantNoTask {
		t.Errorf("done 之后再取菜单应拿到 no_task，got:\n%s\nwant:\n%s", secondMenuBody, wantNoTask)
	}
}
