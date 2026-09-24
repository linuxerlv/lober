// 本文件测试 ESXi boot.cfg 请求的读盘改写路径（esxi_bootcfg.go）。对应
// worktree m1-esxi-bootcfg-wire 的任务，依据 docs/contracts/http-machine.md
// 端点 3、internal/render/esxi.go 文件头注释（RenderESXiBootCfg 的三处
// 改写语义）。
package gateway

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// rawBootCfgFixture 是测试用的原始 boot.cfg 模板文本（真实 ESXi 格式的
// 精简版，含 prefix=/kernelopt=/modules= 三个必须改写的键，modules= 里
// 故意混一个带前导斜杠、一个不带的路径，覆盖"最容易漏"的那条改写）。
const rawBootCfgFixture = `bootstate=0
title=Loading ESXi installer
timeout=5
prefix=
kernel=b.b00
kernelopt=cdromBoot ks=cdrom:/KS.CFG
modules=/b.b00 --- jumpstrt.gz --- useropts.gz
build=8.0.0
updated=0
`

// newESXiBootCfgFixture 构造一个处于 booting 状态、家族为 esxi 的任务
// fixture，并把 rawBootCfgFixture 写到 t.TempDir() 下
// <artifactRoot>/<artifactSetID>/boot.cfg，模拟导入时落盘的原始模板。
//
// 返回的 deps.ArtifactRoot 已指向该临时目录，deps.RenderESXiBootCfg 默认
// 注入一个直接复用 internal/render 三处改写规则的桩实现（测试无需 import
// internal/render——桩自己实现同样的字符串替换规则，因为 gateway 包不能
// import render 包，这正是本次改动要解决的架构约束，测试桩必须独立验证
// 网关侧的读盘/拼参数/调用/错误处理逻辑，不依赖渲染模块的真实实现）。
func newESXiBootCfgFixture(t *testing.T, writeTemplate bool) answerFixture {
	t.Helper()
	fx := newAnswerFixture(t, model.TaskStateBooting)

	if _, err := fx.store.SetArtifactSetContent(t.Context(), "art-1",
		model.OSFamilyESXi, model.ArtifactPaths{Extra: map[string]string{"boot.cfg": "boot.cfg"}}); err != nil {
		t.Fatalf("把制品集改成 ESXi 失败: %v", err)
	}

	root := t.TempDir()
	if writeTemplate {
		setDir := filepath.Join(root, "art-1")
		if err := os.MkdirAll(setDir, 0o755); err != nil {
			t.Fatalf("创建制品集目录失败: %v", err)
		}
		if err := os.WriteFile(filepath.Join(setDir, "boot.cfg"), []byte(rawBootCfgFixture), 0o644); err != nil {
			t.Fatalf("写入原始 boot.cfg 模板失败: %v", err)
		}
	}

	fx.deps.ArtifactRoot = root
	fx.deps.RenderESXiBootCfg = stubRenderESXiBootCfg
	fx.deps.StateMachine = newStateMachineStub()
	fx.deps.Renderer = newRendererStub()
	return fx
}

// stubRenderESXiBootCfg 是测试专用的三处改写实现，规则与
// internal/render.RenderESXiBootCfg 一致（prefix= 整行替换、kernelopt=
// 追加 ks=、modules= 去掉每项前导斜杠），但本文件不 import internal/render，
// 独立实现是为了不让 gateway 包的测试依赖渲染模块——这与生产环境
// cmd/lober/wire.go 里 `RenderESXiBootCfg: render.RenderESXiBootCfg` 的
// 注入是同一个函数类型契约，只是测试用自己的实现验证"网关侧调用这个
// 注入点时传参是否正确、返回值是否原样返回"，不重新验证三处改写规则本身
// 的正确性（那是 internal/render/esxi_test.go 的职责）。
func stubRenderESXiBootCfg(template, bootCfgURLBase, answerFileURL string) (string, error) {
	lines := strings.Split(template, "\n")
	for i, line := range lines {
		switch {
		case strings.HasPrefix(line, "prefix="):
			lines[i] = "prefix=" + bootCfgURLBase
		case strings.HasPrefix(line, "kernelopt="):
			lines[i] = "kernelopt=cdromBoot ks=" + answerFileURL
		case strings.HasPrefix(line, "modules="):
			val := strings.TrimPrefix(line, "modules=")
			parts := strings.Split(val, " --- ")
			for j, p := range parts {
				parts[j] = strings.TrimPrefix(strings.TrimSpace(p), "/")
			}
			lines[i] = "modules=" + strings.Join(parts, " --- ")
		}
	}
	return strings.Join(lines, "\n"), nil
}

// TestAnswer_ESXiBootCfg_ReturnsBootParamsNotKickstart 锁住核心行为：取
// boot.cfg 拿到的是引导参数文件（含 prefix=/kernelopt=），不含 kickstart
// 指令（install /rootpw）。这是本次改动要修的那个"真实部署必然失败的
// 缺口"——此前 boot.cfg 请求被分派到 AssembleESXiKickstart，返回的是
// kickstart 正文，mboot.efi 会把它当引导参数解析，失败发生在引导器读
// 引导参数这一步。
func TestAnswer_ESXiBootCfg_ReturnsBootParamsNotKickstart(t *testing.T) {
	fx := newESXiBootCfgFixture(t, true)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "boot.cfg")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusOK, body)
	}
	if !strings.Contains(body, "prefix=") || !strings.Contains(body, "kernelopt=") {
		t.Errorf("boot.cfg 响应体缺少引导参数行，body=%q", body)
	}
	if strings.Contains(body, "install ") || strings.Contains(body, "rootpw") {
		t.Errorf("boot.cfg 响应体含 kickstart 指令（install /rootpw），"+
			"这正是修复前的错误行为——mboot.efi 会把这段当引导参数解析，"+
			"失败发生在引导器读引导参数这一步，比\"装到一半停住\"更早。body=%q", body)
	}
}

// TestAnswer_ESXiBootCfg_ThreeRewritesApplied 锁住三处改写都生效：
// prefix= 是 HTTP 路径、kernelopt= 的 ks= 指向 ks.cfg 的完整 URL、
// modules= 里每项都没有前导斜杠。
func TestAnswer_ESXiBootCfg_ThreeRewritesApplied(t *testing.T) {
	fx := newESXiBootCfgFixture(t, true)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "boot.cfg")
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusOK, body)
	}

	var prefixLine, kernelOptLine, modulesLine string
	for _, line := range strings.Split(body, "\n") {
		switch {
		case strings.HasPrefix(line, "prefix="):
			prefixLine = line
		case strings.HasPrefix(line, "kernelopt="):
			kernelOptLine = line
		case strings.HasPrefix(line, "modules="):
			modulesLine = line
		}
	}

	if !strings.HasPrefix(prefixLine, "prefix=http://") {
		t.Errorf("prefix= 行 = %q, want 以 http:// 开头的制品 URL", prefixLine)
	}
	if !strings.Contains(kernelOptLine, "ks=http://") || !strings.HasSuffix(kernelOptLine, "/ks.cfg") {
		t.Errorf("kernelopt= 行 = %q, want 含 ks=http://...ks.cfg", kernelOptLine)
	}
	if strings.Contains(modulesLine, "--- /") || strings.HasPrefix(modulesLine, "modules=/") {
		t.Errorf("modules= 行 = %q, want 每个模块路径都没有前导斜杠——"+
			"这条最容易漏，漏了安装器找不到模块", modulesLine)
	}
}

// TestAnswer_ESXiBootCfg_PrefixMatchesMenuArtifactBase 锁住 prefix= 与菜单
// 里的制品 URL 指向同一个基址（含制品集 ID 那一层）。取
// ctx.SetArtifactURL("") 的形态与 internal/render/menu.go 的 renderESXiMenu
// 拼 kernel URL 同一套方法（ctx.SetArtifactURL），确保两条路径对"制品基址
// 怎么拼"没有第二种写法——不一致的后果是菜单指向一个地址、boot.cfg 里的
// prefix 指向另一个。
func TestAnswer_ESXiBootCfg_PrefixMatchesMenuArtifactBase(t *testing.T) {
	fx := newESXiBootCfgFixture(t, true)

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "boot.cfg")
	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d", status, http.StatusOK)
	}

	var prefixLine string
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, "prefix=") {
			prefixLine = strings.TrimPrefix(line, "prefix=")
		}
	}

	// 与 model.RenderContext.SetArtifactURL("") 的拼法一致：
	// <ArtifactBase>/artifacts/<制品集ID>/。
	bases := model.BaseURLs{GatewayBase: fx.deps.Bases.GatewayBase, ArtifactBase: fx.deps.Bases.ArtifactBase}
	want := bases.ArtifactURL("art-1", "")
	if prefixLine != want {
		t.Errorf("prefix= = %q, want %q（与菜单渲染 ctx.SetArtifactURL 同一套拼法，"+
			"含制品集 ID 那一层目录）", prefixLine, want)
	}
}

// TestAnswer_ESXiKsCfg_StillReturnsKickstartBody 锁住上一轮的修复没有被
// 改坏：取 ks.cfg 仍然拿到 kickstart 正文。
func TestAnswer_ESXiKsCfg_StillReturnsKickstartBody(t *testing.T) {
	fx := newESXiBootCfgFixture(t, true)
	renderer := newRendererStub()
	renderer.renderAnswerFileFn = func(ctx model.RenderContext, fileName string) (string, error) {
		return "install --firstdisk=mpx.vmhba0 --overwritevmfs\nrootpw --lock\n", nil
	}
	fx.deps.Renderer = renderer

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusOK, body)
	}
	if !strings.Contains(body, "install ") {
		t.Errorf("ks.cfg 响应体应仍含 kickstart 指令，body=%q", body)
	}
	if len(renderer.renderAnswerFileCalls) != 1 || renderer.renderAnswerFileCalls[0].fileName != "ks.cfg" {
		t.Errorf("ks.cfg 请求应仍经过 Renderer.RenderAnswerFile 分派，calls=%+v", renderer.renderAnswerFileCalls)
	}
}

// TestAnswer_ESXiBootCfg_MissingTemplateFile_Returns500AndRecordsEvent 锁住
// 原始模板文件缺失时的降级行为：500 + TaskEvent，不返回 kickstart 正文
// 兜底——那正是修复前的错误行为。
func TestAnswer_ESXiBootCfg_MissingTemplateFile_Returns500AndRecordsEvent(t *testing.T) {
	fx := newESXiBootCfgFixture(t, false) // 不写模板文件，模拟磁盘缺失

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "boot.cfg")

	if status != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusInternalServerError, body)
	}
	if body == "" {
		t.Error("响应体为空，want 简短纯文本原因")
	}
	if strings.Contains(body, "install ") || strings.Contains(body, "rootpw") {
		t.Errorf("500 响应体不该含 kickstart 正文兜底，body=%q", body)
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
		t.Errorf("原始模板缺失后未找到 level=error 的 TaskEvent，events=%+v", events)
	}
}

// TestAnswer_ESXiBootCfg_NoArtifactRoot_Returns500NotKickstart 锁住
// ArtifactRoot 未注入（空字符串）时的降级行为：明确的错误响应，绝不能
// 降级成"返回 kickstart 正文"——那是比 500 更危险的静默失败，会让运维
// 以为装机能正常进行到 kickstart 阶段，实际上引导器在更早的引导参数
// 解析这一步就已经失败。
func TestAnswer_ESXiBootCfg_NoArtifactRoot_Returns500NotKickstart(t *testing.T) {
	fx := newESXiBootCfgFixture(t, true)
	fx.deps.ArtifactRoot = "" // 显式清空，模拟未注入

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "boot.cfg")

	if status != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusInternalServerError, body)
	}
	if strings.Contains(body, "install ") || strings.Contains(body, "rootpw") {
		t.Errorf("500 响应体不该含 kickstart 正文兜底，body=%q", body)
	}
}

// TestAnswer_ESXiBootCfg_NilRenderFunc_Returns500NotKickstart 锁住
// RenderESXiBootCfg 注入函数为 nil（生产装配点漏配、或某些部署形态不需要）
// 时的降级行为，与 ArtifactRoot 为空同一原则：不能 panic，也不能静默
// 返回 kickstart 正文。
func TestAnswer_ESXiBootCfg_NilRenderFunc_Returns500NotKickstart(t *testing.T) {
	fx := newESXiBootCfgFixture(t, true)
	fx.deps.RenderESXiBootCfg = nil

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "boot.cfg")

	if status != http.StatusInternalServerError {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusInternalServerError, body)
	}
	if strings.Contains(body, "install ") || strings.Contains(body, "rootpw") {
		t.Errorf("500 响应体不该含 kickstart 正文兜底，body=%q", body)
	}
}

// TestAnswer_NonESXiBootCfgPathUnaffected 锁住非 ESXi 家族不受影响——
// RHEL 的应答文件名恰好也是 ks.cfg（AnswerFileName(rhel) == "ks.cfg"），
// 不会经过本次新增的 boot.cfg 专属分支。
func TestAnswer_NonESXiBootCfgPathUnaffected(t *testing.T) {
	fx := newAnswerFixture(t, model.TaskStateBooting) // 夹具默认 RHEL
	fx.deps.StateMachine = newStateMachineStub()
	renderer := newRendererStub()
	fx.deps.Renderer = renderer
	// 故意不设置 ArtifactRoot/RenderESXiBootCfg，验证 RHEL 路径完全不
	// 依赖这两个新字段。

	body, status := doAnswer(t, fx.deps, fx.task.ID, "fixed-install-token", "ks.cfg")

	if status != http.StatusOK {
		t.Fatalf("状态码 = %d, want %d, body=%q", status, http.StatusOK, body)
	}
	if len(renderer.renderAnswerFileCalls) != 1 {
		t.Errorf("RHEL 的 ks.cfg 请求应正常经过 Renderer.RenderAnswerFile，calls=%d",
			len(renderer.renderAnswerFileCalls))
	}
}
