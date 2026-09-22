// 本文件提供测试桩：StateMachine、Renderer、TaskParamsResolver、
// DiskPlanner、AuditLogger 的可控假实现，以及构造测试用 Server 的公共
// helper。对应实现计划「文件结构」表 stub_test.go 一行。
//
// 不 import internal/statemachine、internal/render、internal/provider——
// 全部桩只实现 contract 里对应的窄接口，编译期由 archtest 的依赖方向
// 守卫保证本包没有偷偷引用别的实现模块。
package console

import (
	"context"
	"fmt"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/fake"
	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// StateMachine 桩
// ---------------------------------------------------------------------------

// stubStateMachine 是 contract.StateMachine 的可控假实现。测试按需设置
// 各方法的返回值/回调，未设置的方法调用会触发 t.Fatalf——这是有意的
// 「未预期调用即失败」设计，防止某个测试误触发了没打算测的路径却因为
// 桩返回零值而被悄悄放过。
type stubStateMachine struct {
	t *testing.T

	retryFunc  func(ctx context.Context, taskID string) (model.Task, error)
	cancelFunc func(ctx context.Context, taskID string) (model.Task, error)
}

func (m *stubStateMachine) StartBoot(ctx context.Context, taskID string, arch model.Arch, fw model.FirmwareMode) (model.Task, error) {
	m.t.Fatalf("测试桩未预期调用 StartBoot——控制台不应该触碰引导路径")
	return model.Task{}, nil
}

func (m *stubStateMachine) MarkAnswerFetched(ctx context.Context, taskID string) (model.Task, error) {
	m.t.Fatalf("测试桩未预期调用 MarkAnswerFetched")
	return model.Task{}, nil
}

func (m *stubStateMachine) ApplySignal(ctx context.Context, taskID string, signal model.SignalKind, seq int) (contract.SignalOutcome, error) {
	m.t.Fatalf("测试桩未预期调用 ApplySignal")
	return contract.SignalOutcome{}, nil
}

func (m *stubStateMachine) IssueFinalizeToken(ctx context.Context, taskID string) (model.Task, error) {
	m.t.Fatalf("测试桩未预期调用 IssueFinalizeToken")
	return model.Task{}, nil
}

func (m *stubStateMachine) Fail(ctx context.Context, taskID string, reason model.FailureReason, detail model.FailureDetail) (model.Task, error) {
	m.t.Fatalf("测试桩未预期调用 Fail")
	return model.Task{}, nil
}

func (m *stubStateMachine) Cancel(ctx context.Context, taskID string) (model.Task, error) {
	if m.cancelFunc == nil {
		m.t.Fatalf("测试未设置 cancelFunc 却调用了 Cancel")
	}
	return m.cancelFunc(ctx, taskID)
}

func (m *stubStateMachine) Retry(ctx context.Context, taskID string) (model.Task, error) {
	if m.retryFunc == nil {
		m.t.Fatalf("测试未设置 retryFunc 却调用了 Retry")
	}
	return m.retryFunc(ctx, taskID)
}

func (m *stubStateMachine) ScanTimeouts(ctx context.Context, asOf int64) (int, error) {
	m.t.Fatalf("测试桩未预期调用 ScanTimeouts——控制台不跑扫描循环")
	return 0, nil
}

var _ contract.StateMachine = (*stubStateMachine)(nil)

// ---------------------------------------------------------------------------
// Renderer 桩
// ---------------------------------------------------------------------------

// stubRenderer 是 contract.Renderer 的可控假实现，供 preview 端点测试。
type stubRenderer struct {
	menuFunc   func(ctx model.RenderContext) (string, error)
	answerFunc func(ctx model.RenderContext, fileName string) (string, error)
}

func (r *stubRenderer) RenderMenu(ctx model.RenderContext) (string, error) {
	if r.menuFunc != nil {
		return r.menuFunc(ctx)
	}
	return "#ipxe\necho fake menu\n", nil
}

func (r *stubRenderer) RenderAnswerFile(ctx model.RenderContext, fileName string) (string, error) {
	if r.answerFunc != nil {
		return r.answerFunc(ctx, fileName)
	}
	return "# fake answer file\n", nil
}

var _ contract.Renderer = (*stubRenderer)(nil)

// ---------------------------------------------------------------------------
// TaskParamsResolver 桩
// ---------------------------------------------------------------------------

// stubResolver 是 contract.TaskParamsResolver 的可控假实现，供建任务测试
// 验证「参数决策失败时库里不出现任务」这条硬要求。
type stubResolver struct {
	resolveFunc func(ctx context.Context, formData, facts map[string]string) (map[string]string, error)
}

func (r *stubResolver) Resolve(ctx context.Context, formData, facts map[string]string) (map[string]string, error) {
	if r.resolveFunc != nil {
		return r.resolveFunc(ctx, formData, facts)
	}
	out := make(map[string]string, len(formData))
	for k, v := range formData {
		out[k] = v
	}
	return out, nil
}

var _ contract.TaskParamsResolver = (*stubResolver)(nil)

// ---------------------------------------------------------------------------
// DiskPlanner 桩
// ---------------------------------------------------------------------------

type stubDiskPlanner struct {
	planFunc func(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error)
}

func (p *stubDiskPlanner) Plan(ctx context.Context, disks []contract.Disk, intent contract.DiskIntent) (contract.Layout, error) {
	if p.planFunc != nil {
		return p.planFunc(ctx, disks, intent)
	}
	return contract.Layout{}, nil
}

var _ contract.DiskPlanner = (*stubDiskPlanner)(nil)

// ---------------------------------------------------------------------------
// EventBus 桩
// ---------------------------------------------------------------------------

// stubEventBus 记录全部 Publish 调用，供测试断言「任务创建后确实发布了
// task.created 事件」，同时验证「只是通知，不是决策点」——不阻塞、不返回
// 错误。
type stubEventBus struct {
	mu        sync.Mutex
	published []contract.LifecycleEvent
}

func (b *stubEventBus) Publish(event contract.LifecycleEvent, task model.Task) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.published = append(b.published, event)
}

func (b *stubEventBus) Subscribe(event contract.LifecycleEvent, callback func(model.Task)) {}

func (b *stubEventBus) events() []contract.LifecycleEvent {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]contract.LifecycleEvent, len(b.published))
	copy(out, b.published)
	return out
}

var _ contract.EventBus = (*stubEventBus)(nil)

// ---------------------------------------------------------------------------
// AuditLogger 桩
// ---------------------------------------------------------------------------

// stubAuditLogger 记录全部 Log 调用，供审计中间件测试断言五项内容；同时
// 在 store 非 nil 时把每条记录写透到一个真正的 contract.AuditStore
// ——这是为了让测试环境下 Deps.Audit（本桩）与 Deps.AuditQuery（通常是
// 同一个 fake.Store）之间的关系，与生产环境下 dbAuditLogger 委托给
// AuditStore.AppendAudit、AuditQuery 直接是同一个 sqlite.Store 的关系
// 保持一致：如果本桩只记进自己的 entries 切片、不写透到 store，
// GET /hooks/{id}/audit 之类依赖 AuditQuery 的测试就会拿到空结果，
// 与真实部署下的行为脱节（entries 与 all() 仍然保留，供只需要断言
// 「中间件确实调用了 Log」的既有测试使用，不强迫它们改成查 store）。
type stubAuditLogger struct {
	mu      sync.Mutex
	entries []auditEntry
	store   contract.AuditStore
	clock   contract.Clock

	// failNext 为真时下一次 Log 调用返回错误——供测试验证「审计写入失败
	// 不影响主流程」。写透到 store 的那一步不受 failNext 影响：failNext
	// 模拟的是「本桩返回给调用方的错误」，与「记录是否已经落进可查询的
	// store」是两件事，现实中 dbAuditLogger 落库失败时也不会因此撤回
	// 已经写入 slog 的兜底记录。
	failNext bool
}

type auditEntry struct {
	Actor  string
	Action string
	Target string
	Detail string
}

func (a *stubAuditLogger) Log(ctx context.Context, actor, action, target, detail string) error {
	a.mu.Lock()
	a.entries = append(a.entries, auditEntry{Actor: actor, Action: action, Target: target, Detail: detail})
	failed := a.failNext
	a.failNext = false
	store := a.store
	clock := a.clock
	a.mu.Unlock()

	if store != nil {
		ts := int64(0)
		if clock != nil {
			ts = clock.Now()
		}
		// 错误被有意吞掉：与 dbAuditLogger（cmd/lober/auditlog.go）的降级
		// 语义一致，写透失败不应该让本桩的 Log 调用方看到一个与
		// failNext 无关的额外错误来源。
		_ = store.AppendAudit(ctx, contract.AuditRecord{TS: ts, Actor: actor, Action: action, Target: target, Detail: detail})
	}

	if failed {
		return fmt.Errorf("stub: 模拟审计写入失败")
	}
	return nil
}

func (a *stubAuditLogger) all() []auditEntry {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := make([]auditEntry, len(a.entries))
	copy(out, a.entries)
	return out
}

var _ contract.AuditLogger = (*stubAuditLogger)(nil)

// ---------------------------------------------------------------------------
// 空文件系统（供 dry-run/preview 之外不需要真实 fs.FS 的测试占位）
// ---------------------------------------------------------------------------

// emptyFS 是一个不含任何文件的 fs.FS，本模块目前没有端点直接需要
// io/fs.FS（Prechecker 属于网关/状态机路径），保留这个占位是为了防止
// 未来某个测试误以为需要构造真实文件系统。
type emptyFS struct{}

func (emptyFS) Open(name string) (fs.File, error) {
	return nil, fs.ErrNotExist
}

var _ fs.FS = emptyFS{}

// ---------------------------------------------------------------------------
// 测试用 Server 构造 helper
// ---------------------------------------------------------------------------

// testUser 是 newTestServer 默认创建的五个角色各一个账号，密码统一为
// "password123"，供测试直接登录取 token。
type testUser struct {
	Name     string
	Password string
	Roles    []string
}

// defaultTestUsers 覆盖五个角色各一个账号，角色名与 roleXxx 常量一致。
var defaultTestUsers = []testUser{
	{Name: "alice-viewer", Password: "password123", Roles: []string{roleViewer}},
	{Name: "bob-operator", Password: "password123", Roles: []string{roleOperator}},
	{Name: "carol-author", Password: "password123", Roles: []string{roleHookAuthor}},
	{Name: "dave-approver", Password: "password123", Roles: []string{roleHookApprover}},
	{Name: "eve-admin", Password: "password123", Roles: []string{roleAdmin}},
}

// testServer 打包一次测试所需的全部部件，避免每个测试函数各自组装七八个
// 字段。
type testServer struct {
	Server *Server
	Store  *fake.Store
	Clock  *fake.Clock
	Audit  *stubAuditLogger
	SM     *stubStateMachine
	Bus    *stubEventBus

	t *testing.T
}

// newTestServer 构造一个装配好默认依赖的 Server：fake.Store 作全部 Store
// 窄接口、fake.Clock 作时钟、stubAuditLogger 作审计、defaultTestUsers 作
// 认证用户表。调用方可在返回后继续覆盖 ts.Server.deps 的个别字段（测试
// 需要特定 StateMachine/Renderer/Resolver 行为时）。
func newTestServer(t *testing.T) *testServer {
	t.Helper()

	store := fake.NewStore()
	clock := fake.NewClock()
	audit := &stubAuditLogger{store: store, clock: clock}
	sm := &stubStateMachine{t: t}
	bus := &stubEventBus{}

	users := make([]AuthUser, 0, len(defaultTestUsers))
	for _, u := range defaultTestUsers {
		hash, err := bcrypt.GenerateFromPassword([]byte(u.Password), bcrypt.MinCost)
		if err != nil {
			t.Fatalf("生成测试用户 %s 的 bcrypt 哈希失败: %v", u.Name, err)
		}
		users = append(users, AuthUser{Name: u.Name, BcryptHash: string(hash), Roles: u.Roles})
	}

	deps := Deps{
		Hosts:          store,
		ArtifactSets:   store,
		Hooks:          store,
		Templates:      store,
		Tasks:          store,
		Events:         store,
		StateMachine:   sm,
		Renderer:       &stubRenderer{},
		ParamsResolver: &stubResolver{},
		DiskPlanner:    &stubDiskPlanner{},
		EventBus:       bus,
		Audit:          audit,
		AuditQuery:     store,
		Clock:          clock,
		Users:          users,
	}

	return &testServer{
		Server: NewServer(deps),
		Store:  store,
		Clock:  clock,
		Audit:  audit,
		SM:     sm,
		Bus:    bus,
		t:      t,
	}
}

// login 用 defaultTestUsers 里的账号登录，返回 access token。username 必须
// 是 defaultTestUsers 里的某个 Name。
func (ts *testServer) login(username string) string {
	ts.t.Helper()
	var pass string
	found := false
	for _, u := range defaultTestUsers {
		if u.Name == username {
			pass = u.Password
			found = true
			break
		}
	}
	if !found {
		ts.t.Fatalf("login: 用户名 %q 不在 defaultTestUsers 中", username)
	}

	rr := ts.doJSON("POST", "/console/v1/auth/login", "", map[string]string{
		"username": username,
		"password": pass,
	})
	if rr.Code != http.StatusOK {
		ts.t.Fatalf("login: 期望 HTTP 200，实际 %d，body=%s", rr.Code, rr.Body.String())
	}
	var env struct {
		Data struct {
			AccessToken string `json:"accessToken"`
		} `json:"data"`
	}
	decodeJSON(ts.t, rr.Body.Bytes(), &env)
	if env.Data.AccessToken == "" {
		ts.t.Fatalf("login: 响应里没有 accessToken，body=%s", rr.Body.String())
	}
	return env.Data.AccessToken
}

// doJSON 发一个带（可选）JSON body 与（可选）Bearer token 的请求，返回
// httptest.ResponseRecorder。body 为 nil 时不设置请求体。
func (ts *testServer) doJSON(method, path, token string, body any) *httptest.ResponseRecorder {
	ts.t.Helper()
	req := newJSONRequest(ts.t, method, path, body)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rr := httptest.NewRecorder()
	ts.Server.ServeHTTP(rr, req)
	return rr
}
