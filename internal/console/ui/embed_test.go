// 本文件测试 embed.go 提供的 go:embed handler。
//
// 三条要锁住的行为（M8 计划任务 14）：
//  1. 内嵌的文件系统里能取到 index.html；
//  2. 未知路径返回 404（不是 panic，也不是返回 index.html —— hash 路由
//     不需要 SPA fallback）；
//  3. 两类资源（index.html 与带内容哈希的资源）的缓存头不同。
//
// 额外锁住 go:embed 模式必须是 `all:dist` 而不是裸 `dist`：真实 Vite 产物
// 可能包含下划线前缀的资源目录，裸 `dist` 模式会静默漏掉这类文件（embed
// 包文档：递归遍历时跳过名字以 '.' 或 '_' 开头的文件与目录）。仓库里的占位
// dist 特意放了一个 _app/config-placeholder.js 来暴露这个坑。
package ui

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDistFS_IndexHTMLIsEmbedded 断言内嵌的文件系统里能直接读到
// index.html 的内容——这是 handler 存在的前提：没有这个文件，后续全部行为
// 都无从谈起。
func TestDistFS_IndexHTMLIsEmbedded(t *testing.T) {
	data, err := fs.ReadFile(distFS, "index.html")
	if err != nil {
		t.Fatalf("从内嵌文件系统读取 index.html 失败: %v —— "+
			"go:embed 指令或占位 dist 文件缺失，handler 的全部行为都建立在"+
			"这个文件存在的前提上，这一步失败说明前提本身就没成立", err)
	}
	if len(data) == 0 {
		t.Fatalf("index.html 读出来是空文件 —— 占位内容应当非空，" +
			"否则无法区分「文件不存在」与「文件存在但是空」两种情况")
	}
}

// TestDistFS_UnderscorePrefixedFileIsEmbedded 断言下划线前缀的文件确实被
// go:embed 嵌进来了。
//
// 为什么这条重要：这条不通过通常意味着 embed.go 里的指令写成了
// `//go:embed dist`（裸模式）而不是 `//go:embed all:dist`——裸模式会静默
// 跳过这个文件，不报编译错误、不报运行时错误，只是运行时该文件访问不到，
// 表现为「某个资源 404 了但看不出为什么」。真实 Vite 产物里存在下划线
// 前缀目录是已观察到的现象，这个坑必须在占位阶段就锁住，不能等真实产物
// 接进来才发现。
func TestDistFS_UnderscorePrefixedFileIsEmbedded(t *testing.T) {
	data, err := fs.ReadFile(distFS, "_app/config-placeholder.js")
	if err != nil {
		t.Fatalf("从内嵌文件系统读取 _app/config-placeholder.js 失败: %v —— "+
			"这说明 go:embed 指令漏掉了下划线前缀的文件，很可能是写成了裸 "+
			"`dist` 模式而不是 `all:dist`；真实前端产物里若也有下划线前缀的"+
			"资源目录，会在生产环境表现为该资源不可访问", err)
	}
	if len(data) == 0 {
		t.Fatalf("_app/config-placeholder.js 读出来是空文件，占位内容应当非空")
	}
}

// TestHandler_ServesIndexHTML 断言访问根路径能拿到 index.html 的内容，
// 且响应头设置正确（Content-Type 是 HTML，且不缓存）。
func TestHandler_ServesIndexHTML(t *testing.T) {
	h := Handler()

	for _, path := range []string{"/", "/index.html"} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			h.ServeHTTP(rec, req)

			if rec.Code != http.StatusOK {
				t.Fatalf("GET %s 状态码 = %d，want %d", path, rec.Code, http.StatusOK)
			}
			if !strings.Contains(rec.Body.String(), "<html") {
				t.Fatalf("GET %s 响应体不像 HTML: %q", path, rec.Body.String())
			}
			ct := rec.Header().Get("Content-Type")
			if !strings.HasPrefix(ct, "text/html") {
				t.Fatalf("GET %s Content-Type = %q，want text/html 前缀", path, ct)
			}
		})
	}
}

// TestHandler_IndexHTMLIsNeverCached 断言 index.html 的缓存头明确禁止缓存。
//
// 为什么这条重要：前端更新后，若运维打开控制台看到的还是浏览器缓存里的旧
// index.html，加载的仍是旧版本的资源文件（新版本的 JS/CSS 文件名哈希已经
// 变了，旧 index.html 里引用的还是老哈希文件名，那些文件在新构建产物里
// 可能已经不存在）——用户会看到一个介于新旧版本之间、行为不一致的页面，
// 且这个问题不会在开发环境暴露（开发环境没有这层缓存），只在生产环境静默
// 发生，运维很难联想到是浏览器缓存的问题。
func TestHandler_IndexHTMLIsNeverCached(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/index.html", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	cc := rec.Header().Get("Cache-Control")
	if cc == "" {
		t.Fatalf("index.html 的 Cache-Control 头为空 —— 必须显式禁止缓存")
	}
	if !strings.Contains(cc, "no-cache") && !strings.Contains(cc, "no-store") {
		t.Fatalf("index.html 的 Cache-Control = %q，want 包含 no-cache 或 no-store", cc)
	}
}

// TestHandler_HashedAssetIsLongCached 断言带内容哈希的资源文件（assets/
// 目录下）被设置为长期缓存，且与 index.html 的策略不同。
//
// 为什么这条重要：这类文件名里含内容哈希，内容变了文件名必然变——旧文件名
// 永远指向旧内容，长期缓存不会导致运维看到过期内容。反之，若这类文件也被
// 设成不缓存，用户每次打开控制台都要重新下载全部 JS/CSS，在装机现场网络
// 条件不佳时会明显拖慢页面加载。
func TestHandler_HashedAssetIsLongCached(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/assets/index-placeholder0a1b2c3d.js", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("GET /assets/index-placeholder0a1b2c3d.js 状态码 = %d，want %d，body=%q",
			rec.Code, http.StatusOK, rec.Body.String())
	}

	cc := rec.Header().Get("Cache-Control")
	if cc == "" {
		t.Fatalf("带哈希资源的 Cache-Control 头为空 —— 应当设置长期缓存")
	}
	if strings.Contains(cc, "no-cache") || strings.Contains(cc, "no-store") {
		t.Fatalf("带哈希资源的 Cache-Control = %q，不应包含 no-cache/no-store"+
			"（那是 index.html 的策略，两类资源的缓存策略必须不同）", cc)
	}
	if !strings.Contains(cc, "immutable") && !strings.Contains(cc, "max-age") {
		t.Fatalf("带哈希资源的 Cache-Control = %q，want 包含 max-age 或 immutable"+
			"（否则不算「长期缓存」，只是含糊地不是 no-cache）", cc)
	}
}

// TestHandler_UnknownPathReturns404 断言未知路径返回 404，而不是 panic
// 也不是回落到 index.html。
//
// 为什么这条重要：hash 路由（Vite 的 VITE_ROUTER_HISTORY=hash 配置）下，
// 前端路由状态编码在 URL 的 # 片段里，浏览器永远只向服务器请求 "/" 或
// "/index.html"，服务器侧不会收到形如 /tasks/123 这样需要 SPA fallback
// 才能正确处理的请求路径。因此这个 handler 刻意不做 SPA fallback——如果
// 未来有人照抄 history 路由的教程加上 fallback，那是在掩盖一个从未存在
// 的问题，且会让「真正不存在的资源」这种情况被误判为 200 而没人发现。
func TestHandler_UnknownPathReturns404(t *testing.T) {
	h := Handler()

	for _, path := range []string{
		"/tasks/123",
		"/does-not-exist.js",
		"/../embed.go", // 路径遍历尝试，必须也是 404，不能读到 embed.go 本身
		"/assets/../../embed.go",
	} {
		t.Run(path, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, path, nil)
			rec := httptest.NewRecorder()

			func() {
				defer func() {
					if r := recover(); r != nil {
						t.Fatalf("GET %s 触发 panic: %v —— 未知路径必须优雅返回 404，"+
							"panic 会导致整个 HTTP 服务的这次请求处理崩溃"+
							"（视中间件而定，可能拖垒同进程内其它请求）", path, r)
					}
				}()
				h.ServeHTTP(rec, req)
			}()

			if rec.Code != http.StatusNotFound {
				t.Fatalf("GET %s 状态码 = %d，want %d（不做 SPA fallback，"+
					"不应该是 200 也不应该是其它状态码）", path, rec.Code, http.StatusNotFound)
			}
		})
	}
}

// TestHandler_UnknownPathDoesNotReturnIndexHTML 单独锁住「不回落到
// index.html」这一条，用响应体内容直接断言，避免只看状态码漏掉某些实现
// 用非 200 状态码但仍然把 index.html 内容塞进 body 的写法。
func TestHandler_UnknownPathDoesNotReturnIndexHTML(t *testing.T) {
	h := Handler()
	req := httptest.NewRequest(http.MethodGet, "/tasks/123", nil)
	rec := httptest.NewRecorder()

	h.ServeHTTP(rec, req)

	if strings.Contains(rec.Body.String(), "占位页面") {
		t.Fatalf("GET /tasks/123 的响应体包含 index.html 占位页面的标记文字，"+
			"说明 handler 做了 SPA fallback——hash 路由不需要这个行为，"+
			"body=%q", rec.Body.String())
	}
}
