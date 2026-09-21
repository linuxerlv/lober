// Package ui 内嵌控制台 WebUI 的构建产物，提供一个 http.Handler 供
// cmd/lober（M9）挂载。本包是 M8 计划里唯一的 Go 文件，前端源码与构建流程
// 见 web/README.md。
//
// 内嵌的 dist 树在仓库里当前只是一个最小占位产物（index.html + 两个用来
// 锁住 embed 模式与缓存策略测试的占位文件）；真实产物由 web/apps/web-ele
// 的 vite build 产生，outDir 直接指向本包的 dist/ 目录，构建后整体覆盖
// 这里的占位文件（见 web/UPSTREAM_PIN.md 与 web/README.md）。
package ui

import (
	"embed"
	"io/fs"
	"mime"
	"net/http"
	"path"
	"strings"
)

// distRaw 内嵌 dist/ 整棵树。
//
// 用 `all:dist` 而不是裸 `dist`：Go 的 embed 包对裸目录模式有一条隐藏规则，
// 递归遍历时会跳过名字以 '.' 或 '_' 开头的文件与目录（`go doc embed`）。
// Vite 的真实构建产物里可能出现下划线前缀的资源目录，裸模式会静默漏掉这类
// 文件——不报编译错误、不报运行时错误，只是运行时该资源访问不到，表现为
// 一个查不出原因的 404。`all:` 前缀关掉这条跳过规则。
//
//go:embed all:dist
var distRaw embed.FS

// distFS 是 distRaw 去掉外层 "dist" 前缀后的子文件系统，使内部路径与浏览器
// 请求路径直接对应（请求 "/index.html" 对应 distFS 里的 "index.html"，
// 不用写成 "dist/index.html"）。
//
// distRaw 的目录结构由 go:embed 保证包含 "dist" 这一层（占位 index.html
// 已提交在仓库的 internal/console/ui/dist/ 下），fs.Sub 在这里不会失败；
// 万一未来有人误删占位目录导致 go:embed 连编译都过不去，问题会在编译期
// 就暴露，不会拖到这里的 panic——因此这里选择 must 语义而不是把错误一路
// 传播给调用者：调用者（cmd/lober）没有任何办法修复「内嵌数据缺失」这种
// 编译期就该发现的问题，把它做成运行时错误只会让故障出现得更晚、更难定位。
var distFS = must(fs.Sub(distRaw, "dist"))

func must(f fs.FS, err error) fs.FS {
	if err != nil {
		panic("internal/console/ui: 内嵌的 dist 目录结构异常: " + err.Error())
	}
	return f
}

// indexHTML 是固定文件名，hash 路由（VITE_ROUTER_HISTORY=hash，见
// web/UPSTREAM_PIN.md 任务 1 的核对结果）下浏览器只会请求 "/" 或
// "/index.html"，不需要为其它路径做 SPA fallback。
const indexHTML = "index.html"

// Handler 返回控制台 WebUI 的静态资源 handler，供 cmd/lober 挂载（通常挂在
// "/" 或某个前缀下，与 docs/contracts/http-console.md 的 "/console/v1" API
// 前缀不冲突——WebUI 走静态资源，API 走那个前缀，二者路径空间不重叠）。
//
// 行为（对应 M8 计划任务 14 的三项要求）：
//   - "/" 与 "/index.html" 返回内嵌的 index.html，Cache-Control 明确禁止
//     缓存（前端更新后运维不应该还看到浏览器缓存的旧版本）；
//   - 其它已知路径按内嵌文件系统实际内容返回，非 index.html 的文件设置
//     长期不可变缓存（这类文件名由 Vite 按内容哈希生成，内容变则文件名变，
//     长期缓存不会导致过期内容问题）；
//   - 未知路径返回 404，不做 SPA fallback，不 panic —— hash 路由下所有
//     应用内导航都发生在浏览器端，服务器不会收到需要 fallback 才能处理的
//     路径；返回 index.html 或 200 会把「资源确实不存在」这个信息抹掉。
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		serveFile(w, r, cleanRequestPath(r.URL.Path))
	})
}

// cleanRequestPath 把请求路径转换成 distFS 里的相对路径。
//
// path.Clean 处理 ".."：清理后若结果逃出根目录（以 ".." 开头，或本身就是
// ".."），说明这是一次路径遍历尝试，统一映射到一个 fs.ValidPath 会拒绝的
// 值，让后续 Open 稳定走「文件不存在」分支——不单独写一条「疑似攻击」的
// 特殊处理逻辑，因为效果（返回 404）与「文件不存在」完全一致，没有必要为
// 相同的外部可观察行为编写两条代码路径。
func cleanRequestPath(urlPath string) string {
	p := strings.TrimPrefix(path.Clean(urlPath), "/")
	if p == "." || p == "" {
		return indexHTML
	}
	if p == ".." || strings.HasPrefix(p, "../") {
		return "\x00invalid" // 保证 fs.ValidPath 判假、Open 判「不存在」
	}
	return p
}

// serveFile 从 distFS 读出 name 对应的内容并写入响应，未命中时写 404。
func serveFile(w http.ResponseWriter, r *http.Request, name string) {
	data, err := fs.ReadFile(distFS, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}

	setCacheControl(w, name)
	w.Header().Set("Content-Type", contentTypeFor(name))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}

// setCacheControl 区分两类资源的缓存策略：index.html 不缓存，其它资源长期
// 缓存。
//
// 只按「是不是 index.html」二分，不尝试用正则或路径规则去猜「哪些文件名
// 含内容哈希」——本包不知道、也不应该知道 Vite 具体用什么哈希算法或文件名
// 格式（那是前端构建工具的实现细节，属于 web/ 那一侧，本包只负责把内嵌
// 文件系统里除 index.html 之外的一切都当作「内容寻址、可长期缓存」处理）。
// 这个二分对当前唯一的调用方（vite 的标准构建产物）成立：index.html 是
// 唯一一个内容会变但文件名不变的文件，其它一切资源文件名都随内容哈希变化。
func setCacheControl(w http.ResponseWriter, name string) {
	if name == indexHTML {
		w.Header().Set("Cache-Control", "no-cache, no-store, must-revalidate")
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
}

// contentTypeFor 按文件扩展名猜测 Content-Type。
//
// 用标准库 mime.TypeByExtension 而不是手写扩展名表：它在大多数平台已经
// 覆盖了 .js/.css/.html/.svg/.png/.woff2 等前端构建产物的常见类型，
// 手写一份等价表只是重复标准库已经做好的事，且容易在遗漏某个扩展名时
// 悄悄退化成 application/octet-stream 而不被注意到。
//
// mime.TypeByExtension 在少数平台上会读系统 mimetype 配置，返回值可能带
// 系统特定的参数（如 "; charset=..."）——这里不做归一化处理，因为
// Content-Type 头本身允许携带参数，浏览器按 MIME 主类型渲染，不受额外
// 参数影响。
func contentTypeFor(name string) string {
	ext := path.Ext(name)
	if ct := mime.TypeByExtension(ext); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
