// 占位文件，用来锁住 go:embed 的匹配模式必须是 `all:dist` 而不是裸 `dist`。
//
// Go 的 embed 包对裸目录模式（不带 `all:` 前缀）有一条隐藏规则：递归遍历时
// 会跳过全部名字以 `.` 或 `_` 开头的文件与目录（见 `go doc embed`）。Vite
// 的真实构建产物里存在以下划线开头的资源目录是已知现象（比如某些插件产出的
// chunk 目录），若 embed.go 的指令写成 `//go:embed dist` 而不是
// `//go:embed all:dist`，这个文件（以及真实产物里对应的下划线目录）会被
// 静默漏掉——测试能通过（因为 index.html 还在），但生产环境加载某个资源时
// 404，且没人会想到是 embed 模式的问题。
//
// embed_test.go 里的 TestHandler_UnderscorePrefixedFileIsEmbedded
// 就是靠这个文件存在来断言 all: 前缀确实生效。
export const placeholder = true;
