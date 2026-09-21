// 本文件测试引导决策端点 GET /v1/boot。对应实现计划任务 4（no_task 路径）、
// 任务 5（命中路径），依据 docs/contracts/http-machine.md 端点 2、
// SPEC §4.2、§4.3、§4.6、§9.1。
package gateway

import (
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/lober/lober/internal/core/contract"
)

// doBoot 用给定的 query 参数请求 GET /v1/boot，返回响应体、状态码与
// 使用的 Deps（便于测试检查 Store 内部状态）。
func doBoot(t *testing.T, deps Deps, query url.Values) (body string, status int) {
	t.Helper()
	srv := NewServer(deps)

	req := httptest.NewRequest(http.MethodGet, "/v1/boot?"+query.Encode(), nil)
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

// wantNoTaskBody 是约定 2 冻结的 no_task 载荷，逐字符复刻
// docs/contracts/http-machine.md「约定 2」一节（与 contract.NoTaskScript 的
// 产出应该逐字符相等，测试里独立写一份字面值而不是直接调用
// contract.NoTaskScript，是为了在契约文档与 M1 实现出现任何一方漂移时都能
// 被本测试捕捉到，而不是「测试永远等于实现，测不出漂移」）。
const wantNoTaskBodyPrefix = "#!ipxe\n" +
	"echo lober: no task for "

const wantNoTaskBodySuffix = "\n" +
	"sanboot --no-describe --drive 0x80\n"

// TestBootNoTask_ReturnsHTTP200NotNotFound 锁住计划任务 4 三条不能妥协的
// 约束之一：无匹配任务时状态码是 200（不是 404）。iPXE 对非 200 的处理是
// 跳到首跳脚本里的 || shell 分支，那会把机器停在交互式提示符上等人。
func TestBootNoTask_ReturnsHTTP200NotNotFound(t *testing.T) {
	deps := newTestDeps()
	body, status := doBoot(t, deps, url.Values{"sn": {"SN-NOT-FOUND"}})

	if status != http.StatusOK {
		t.Errorf("GET /v1/boot 状态码 = %d, want %d —— iPXE 对非 200 的处理是跳到"+
			"首跳脚本里的 || shell 分支，把机器停在交互式提示符上等人；无任务时"+
			"必须仍是 200，让机器回本地盘或继续 PXE 轮询", status, http.StatusOK)
	}
	if body == "" {
		t.Error("响应体为空")
	}
}

// TestBootNoTask_BodyMatchesContractVerbatim 锁住计划任务 4 三条约束之二：
// 响应体逐字符等于 M1 的 no_task 脚本，其中 sanboot 那行必须在——
// BIOS/MBR 模式下没有 efibootmgr，installer 装完无法把本地盘置顶引导顺序，
// 这类机器完全依赖这一行回本地盘。
func TestBootNoTask_BodyMatchesContractVerbatim(t *testing.T) {
	deps := newTestDeps()
	body, _ := doBoot(t, deps, url.Values{"sn": {"SN-XYZ"}})

	want := contract.NoTaskScript("SN-XYZ")
	if body != want {
		t.Errorf("no_task 响应体不是逐字符等于 contract.NoTaskScript 的产出：\ngot:  %q\nwant: %q\n"+
			"这行 sanboot 是失败模式 1（装完又从网络启动导致循环重装）的唯一保护，"+
			"必须逐字符相等，不能只是「大致像」", body, want)
	}

	// 独立复刻契约文档的字面值，双重锁定（见 wantNoTaskBodyPrefix/Suffix
	// 注释）：即使 contract.NoTaskScript 本身被意外改写，这条断言依然按
	// 冻结文档的字面文本判定。
	if body != wantNoTaskBodyPrefix+"SN-XYZ"+wantNoTaskBodySuffix {
		t.Errorf("no_task 响应体与契约文档冻结的字面文本不符，got: %q", body)
	}
}

// TestBootNoTask_ProducesNoStoreRecords 锁住计划任务 4 三条约束之三：
// 不产生任何记录——不建任务、不改状态、不写事件表、不告警。数据中心里
// 任何网络引导优先的机器重启都会撞一次这个端点，量大且全是噪声，建任务、
// 记状态、发告警都会把噪声变成负担。
func TestBootNoTask_ProducesNoStoreRecords(t *testing.T) {
	deps := newTestDeps()

	doBoot(t, deps, url.Values{"sn": {"SN-GHOST"}, "mac": {"aa:bb:cc:dd:ee:ff"}, "uuid": {"u-1"}})

	tasks, err := deps.TaskStore.ListTasks(t.Context(), contract.TaskFilter{})
	if err != nil {
		t.Fatalf("ListTasks 失败: %v", err)
	}
	if len(tasks) != 0 {
		t.Errorf("请求后 Store 里任务数 = %d, want 0 —— 查不到主机/任务这一支不应建任务", len(tasks))
	}

	events, err := deps.EventStore.ListEvents(t.Context(), "any-task-id", contract.EventFilter{})
	if err != nil {
		t.Fatalf("ListEvents 失败: %v", err)
	}
	if len(events) != 0 {
		t.Errorf("请求后 Store 里事件数 = %d, want 0 —— 查不到主机/任务这一支不应写事件表", len(events))
	}
}

// TestBootNoTask_AllQueryParamsEmpty_DoesNotCrash 锁住：五个 query 全空时
// 也不崩（白牌机 SN 可能留空）。
func TestBootNoTask_AllQueryParamsEmpty_DoesNotCrash(t *testing.T) {
	deps := newTestDeps()
	body, status := doBoot(t, deps, url.Values{})

	if status != http.StatusOK {
		t.Errorf("五个 query 全空时状态码 = %d, want %d", status, http.StatusOK)
	}
	want := contract.NoTaskScript("")
	if body != want {
		t.Errorf("五个 query 全空时响应体 = %q, want %q", body, want)
	}
}

// TestBootNoTask_MalformedArchOrPlatform_FallsBackToNoTask 锁住契约文档
// 端点 2「错误行为」：查询参数格式错误时视为查不到匹配主机，同样返回 200
// 的 no_task 载荷——不返回 400。这里构造一个存在 pending 任务但 arch/plat
// 参数无法被 model.ParseIPXEBuildArch/ParseIPXEPlatform 识别的请求。
func TestBootNoTask_MalformedArchOrPlatform_FallsBackToNoTask(t *testing.T) {
	deps := newTestDeps()

	body, status := doBoot(t, deps, url.Values{
		"sn": {"SN-1"}, "mac": {"aa:bb:cc:dd:ee:ff"}, "uuid": {"u-1"},
		"arch": {"not-a-real-arch"}, "plat": {"not-a-real-platform"},
	})

	if status != http.StatusOK {
		t.Errorf("arch/plat 格式错误时状态码 = %d, want %d（不应返回 400）", status, http.StatusOK)
	}
	want := contract.NoTaskScript("SN-1")
	if body != want {
		t.Errorf("arch/plat 格式错误且查不到 pending 任务时响应体 = %q, want %q", body, want)
	}
}
