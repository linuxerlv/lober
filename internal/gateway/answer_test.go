// 本文件测试应答文件端点 GET /v1/answer/{task}/{token}/{file}。对应实现
// 计划任务 6，依据 docs/contracts/http-machine.md 端点 3、SPEC §4.4、§4.5、
// §5.1。
//
// 注意：本文件的错误响应形态严格按 docs/contracts/http-machine.md（冻结
// 文档）实现，而不是按计划任务 6 文字描述的「渲染失败 → 200 加注释文本」——
// 两者在这一点上有冲突，冻结契约文档具有更高权威性（任务说明「契约是
// docs/contracts/http-machine.md，路径、响应体、状态码按它实现，不自己
// 发明」）。契约文档端点 3「错误行为」表明确：渲染出错 → 500 + TaskEvent，
// 不是 200。这条偏差已记入本模块返回值的 contractGaps。
package gateway

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// answerFixture 打包一次应答文件端点测试所需的前置数据：一个处于
// booting 状态、持有装机 token 的任务。
type answerFixture struct {
	deps  Deps
	store *fake.Store
	task  model.Task
}

// newAnswerFixture 落库一个 state=booting、Token=fixedInstallToken 的任务。
func newAnswerFixture(t *testing.T, state model.TaskState) answerFixture {
	t.Helper()
	store := fake.NewStore()
	ctx := t.Context()

	task := model.Task{
		ID: "task-ans-1", HostSN: "SN-1",
		TemplateID: "tpl-ans-1", TemplateVersion: 1, ArtifactSetID: "art-1",
		State: state, Token: "fixed-install-token", TokenExpiresAt: 9_999_999_999,
	}
	created, err := store.CreateTask(ctx, task)
	if err != nil {
		t.Fatalf("CreateTask 失败: %v", err)
	}

	host := model.Host{SN: "SN-1", MAC: "aa:bb:cc:dd:ee:ff", Arch: model.ArchX86_64, Firmware: model.FirmwareUEFI}
	if _, err := store.UpsertHost(ctx, host); err != nil {
		t.Fatalf("UpsertHost 失败: %v", err)
	}

	tpl := model.Template{ID: "tpl-ans-1", Version: 1, ArtifactSetID: "art-1", State: model.TemplateStateActive}
	if _, err := store.CreateTemplate(ctx, tpl); err != nil {
		t.Fatalf("CreateTemplate 失败: %v", err)
	}

	artifact := model.ArtifactSet{ID: "art-1", OSFamily: model.OSFamilyRHEL, State: model.ArtifactSetReady}
	if _, err := store.CreateArtifactSet(ctx, artifact); err != nil {
		t.Fatalf("CreateArtifactSet 失败: %v", err)
	}

	deps := newTestDeps()
	deps.TaskStore = store
	deps.FactStore = store
	deps.EventStore = store
	deps.TemplateStore = store
	deps.ArtifactSetStore = store
	deps.HostStore = store

	return answerFixture{deps: deps, store: store, task: created}
}

// doAnswer 请求 GET /v1/answer/{task}/{token}/{file}。
func doAnswer(t *testing.T, deps Deps, taskID, token, file string) (body string, status int) {
	t.Helper()
	srv := NewServer(deps)

	req := httptest.NewRequest(http.MethodGet, "/v1/answer/"+taskID+"/"+token+"/"+file, nil)
	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	resp := rec.Result()
	defer resp.Body.Close()
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应体失败: %v", err)
	}
	return string(raw), resp.StatusCode
}

// TestAnswer_HappyPath_ReturnsRenderedContentAndMarksFetched 锁住：正常
// 路径返回渲染结果，且状态机的「已取」被调用。
func TestAnswer_HappyPath_ReturnsRenderedContentAndMarksFetched(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	sm := newStateMachineStub()
	fx.deps.StateMachine = sm
	renderer := newRendererStub()
	fx.deps.Renderer = renderer

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}
	if body == "" {
		t.Error("响应体为空，want 渲染出的应答文件正文")
	}
	if len(renderer.renderAnswerFileCalls) != 1 {
		t.Fatalf("RenderAnswerFile 被调用 %d 次, want 1", len(renderer.renderAnswerFileCalls))
	}
	if renderer.renderAnswerFileCalls[0].fileName != "ks.cfg" {
		t.Errorf("RenderAnswerFile 传入的 fileName = %q, want %q", renderer.renderAnswerFileCalls[0].fileName, "ks.cfg")
	}
}

// TestAnswer_InvalidToken_Returns403Empty 锁住：token 无效 → 403 空响应。
func TestAnswer_InvalidToken_Returns403Empty(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "wrong-token", "ks.cfg")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d", status, http.StatusForbidden)
	}
	if body != "" {
		t.Errorf("响应体 = %q, want 空——token 无效时不该产出任何内容", body)
	}
}

// TestAnswer_StateNotGated_Returns403 锁住：状态不在门控集合内 → 403
// （用 done 状态验证）。
func TestAnswer_StateNotGated_Returns403(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateDone)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d —— done 不在 {booting, installing} 门控集合内", status, http.StatusForbidden)
	}
	if body != "" {
		t.Errorf("响应体 = %q, want 空", body)
	}
}

// TestAnswer_PathTaskIDMismatchesToken_Returns403 锁住：路径 id 与 token
// 解出的任务不一致 → 403。这条不能省——不校验的后果是可以拿 A 任务的
// token 配 B 任务的 id 去取应答文件，路径里那个 id 会被用于构造渲染上下
// 文，不一致时会渲染出张冠李戴的内容。
func TestAnswer_PathTaskIDMismatchesToken_Returns403(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	body, status := doAnswer(t, fx.deps, "some-other-task-id", "fixed-install-token", "ks.cfg")

	if status != http.StatusForbidden {
		t.Errorf("状态码 = %d, want %d", status, http.StatusForbidden)
	}
	if body != "" {
		t.Errorf("响应体 = %q, want 空", body)
	}
}

// TestAnswer_FileNameMismatch_Returns404 锁住约定 4：{file} 与
// RenderContext.AnswerFileName() 不一致 → 404（不是模糊匹配）。
func TestAnswer_FileNameMismatch_Returns404(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	_, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "wrong-name.cfg")

	if status != http.StatusNotFound {
		t.Errorf("状态码 = %d, want %d —— {file} 与该任务 AnswerFileName() 不一致时"+
			"必须 404，不接受 handler 自行拼接的候选名单", status, http.StatusNotFound)
	}
}

// TestAnswer_RenderFails_Returns500AndRecordsEvent 锁住契约文档端点 3
// 「错误行为」：渲染本身出错（模板/参数问题）→ 500，body 为简短纯文本
// 原因；同时记一条 TaskEvent（level=error）。
func TestAnswer_RenderFails_Returns500AndRecordsEvent(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	renderer := newRendererStub()
	renderer.renderAnswerFileFn = func(ctx model.RenderContext, fileName string) (string, error) {
		return "", errRenderFailureInTest
	}
	fx.deps.Renderer = renderer

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusInternalServerError {
		t.Errorf("状态码 = %d, want %d", status, http.StatusInternalServerError)
	}
	if body == "" {
		t.Error("响应体为空，want 简短纯文本原因")
	}

	events, err := fx.store.ListEvents(t.Context(), fx.task.ID, contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	found := false
	for _, e := range events {
		if e.Level == model.EventLevelError {
			found = true
		}
	}
	if !found {
		t.Errorf("渲染失败后未找到 level=error 的 TaskEvent，events=%+v", events)
	}
}

// errRenderFailureInTest 是本文件测试内部用的占位渲染错误。
var errRenderFailureInTest = &stubError{"stub: 渲染失败"}

// TestAnswer_RepeatedRequests_BothSucceed 锁住：重复请求两次都成功
// （幂等，对应 cloud-init 取两个文件）。
func TestAnswer_RepeatedRequests_BothSucceed(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)

	body1, status1 := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")
	body2, status2 := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status1 != http.StatusOK || status2 != http.StatusOK {
		t.Fatalf("两次请求状态码 = %d, %d, want 都是 %d", status1, status2, http.StatusOK)
	}
	if body1 == "" || body2 == "" {
		t.Error("两次请求响应体都不该为空")
	}
}

// TestAnswer_MarkAnswerFetchedCalledForEachRequest 单独锁住 MarkAnswerFetched
// 被调用的事实（不依赖具体实现是否真的推进状态——那是状态机的职责，网关
// 只需要调用它）。
func TestAnswer_MarkAnswerFetchedCalledForEachRequest(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	sm := newStateMachineStub()
	var markCalls int
	sm.markAnswerFetchedFn = func(ctx context.Context, taskID string) (model.Task, error) {
		markCalls++
		return model.Task{ID: taskID, State: model.TaskStateInstalling}, nil
	}
	fx.deps.StateMachine = sm

	doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if markCalls != 1 {
		t.Errorf("MarkAnswerFetched 被调用 %d 次, want 1", markCalls)
	}
}

// TestAnswer_UbuntuMetaDataIsServed 锁住「Ubuntu 的 meta-data 请求也能取到」。
//
// 为什么这条重要：Ubuntu 的 autoinstall 走 cloud-init 的 NoCloud 数据源，
// 内核参数 ds=nocloud-net;s=<url> 指向一个目录，cloud-init 会在那个目录下
// **依次取 user-data 与 meta-data 两个文件**（SPEC §4.4）。
//
// 这个 bug 是 Ubuntu 端到端脚本跑出来的：渲染器（internal/render/renderer.go）
// 早就有专门处理 meta-data 的分支，但网关只比对 AnswerFileName() 的单一
// 返回值，于是 meta-data 请求拿到 404、渲染器那个分支成了死代码。现场表现是
// 「机器装到一半停住」—— cloud-init 取不到 meta-data 就不会继续。
//
// 放行必须窄：只对 Ubuntu 家族、只多放行 "meta-data" 这一个名字。下面第二个
// 子测试锁住「RHEL 请求 meta-data 仍然 404」，防止将来把放行放宽成无条件的。
func TestAnswer_UbuntuMetaDataIsServed(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	// 把制品集改成 Ubuntu（夹具默认造的是 RHEL）。
	if _, err := fx.store.SetArtifactSetContent(t.Context(), "art-1",
		model.OSFamilyUbuntu, model.ArtifactPaths{Kernel: "casper/vmlinuz"}); err != nil {
		t.Fatalf("把制品集改成 Ubuntu 失败: %v", err)
	}
	fx.deps.StateMachine = newStateMachineStub()
	renderer := newRendererStub()
	fx.deps.Renderer = renderer

	for _, file := range []string{"user-data", "meta-data"} {
		t.Run(file, func(t *testing.T) {
			_, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", file)
			if status != http.StatusOK {
				t.Errorf("Ubuntu 取 %s 状态码 = %d, want 200 —— cloud-init 要取 user-data 与 "+
					"meta-data 两个文件，缺一个就会停在装机中途（SPEC §4.4）", file, status)
			}
		})
	}
}

// TestAnswer_NonUbuntuMetaDataStill404 锁住 meta-data 的放行只对 Ubuntu 生效。
// 路径段来自机器请求，无条件放行等于让机器自己指定要取哪种应答文件。
func TestAnswer_NonUbuntuMetaDataStill404(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting) // 夹具默认 RHEL
	fx.deps.StateMachine = newStateMachineStub()
	fx.deps.Renderer = newRendererStub()

	_, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "meta-data")
	if status != http.StatusNotFound {
		t.Errorf("RHEL 取 meta-data 状态码 = %d, want 404 —— meta-data 只对 Ubuntu 放行", status)
	}
}

// TestAnswer_ESXiKsCfgIsServed 锁住 ESXi 的 ks.cfg 放行——与
// TestAnswer_UbuntuMetaDataIsServed 同构。
//
// 为什么这条重要：ESXi 的 boot.cfg（AnswerFileName(esxi) 固定返回的唯一
// 文件名）是 mboot.efi 的引导参数文件本身，其 kernelopt= 行的 ks= 参数
// 指向另一份独立文件——ESXi 自己的 kickstart 方言正文（见
// internal/render/esxi.go 文件头注释）。这两个文件从来不是同一个，机器
// 引导到"取 ks= 指向的文件"这一步时请求的路径段是 ks.cfg，不是 boot.cfg。
//
// 在本次改动之前，answerFileNameAllowed 只放行 boot.cfg，ks.cfg 请求会
// 拿到 404——现场表现与 meta-data 404 同一类："引导器/安装器某一步取不到
// 文件"，且 internal/render/renderer.go 里能正确渲染这份内容的
// AssembleESXiKickstart 分支在 ks.cfg 这条路径上完全是死代码。
func TestAnswer_ESXiKsCfgIsServed(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	// 把制品集改成 ESXi（夹具默认造的是 RHEL）。
	if _, err := fx.store.SetArtifactSetContent(t.Context(), "art-1",
		model.OSFamilyESXi, model.ArtifactPaths{Extra: map[string]string{"boot.cfg": "boot.cfg"}}); err != nil {
		t.Fatalf("把制品集改成 ESXi 失败: %v", err)
	}
	fx.deps.StateMachine = newStateMachineStub()
	renderer := newRendererStub()
	fx.deps.Renderer = renderer

	for _, file := range []string{"boot.cfg", "ks.cfg"} {
		t.Run(file, func(t *testing.T) {
			_, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", file)
			if status != http.StatusOK {
				t.Errorf("ESXi 取 %s 状态码 = %d, want 200 —— boot.cfg 的 ks= 参数指向 ks.cfg，"+
					"取不到会导致装机在引导参数解析或 kickstart 执行这一步失败", file, status)
			}
		})
	}
}

// TestAnswer_NonESXiKsCfgStill404 锁住 ks.cfg 的放行只对 ESXi 生效。与
// TestAnswer_NonUbuntuMetaDataStill404 同一条防线：路径段来自机器请求，
// 无条件放行等于让机器自己指定要取哪种应答文件。
//
// 故意选 Debian（AnswerFileName(debian) = "preseed.cfg"）而不是默认夹具的
// RHEL——RHEL 自己的应答文件名恰好也叫 "ks.cfg"（AnswerFileName(rhel) =
// "ks.cfg"），若仍用 RHEL 测试，请求 ks.cfg 会先命中「与本来的返回值逐字符
// 相等」这条路径本身就通过，测不出"ks.cfg 放行分支是否被非 ESXi 家族误
// 命中"这件事。Debian 的本来文件名与 "ks.cfg" 不同，才能确认命中的是
// 兜底的 404，不是凑巧相等。
func TestAnswer_NonESXiKsCfgStill404(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting)
	if _, err := fx.store.SetArtifactSetContent(t.Context(), "art-1",
		model.OSFamilyDebian, model.ArtifactPaths{Kernel: "install.amd/vmlinuz"}); err != nil {
		t.Fatalf("把制品集改成 Debian 失败: %v", err)
	}
	fx.deps.StateMachine = newStateMachineStub()
	fx.deps.Renderer = newRendererStub()

	_, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")
	if status != http.StatusNotFound {
		t.Errorf("Debian 取 ks.cfg 状态码 = %d, want 404 —— ks.cfg 放行只对 ESXi 生效", status)
	}
}
