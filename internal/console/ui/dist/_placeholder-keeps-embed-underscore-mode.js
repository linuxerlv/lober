// 占位文件：锁住 go:embed 必须用 all: 模式。
// 裸 embed 会静默跳过下划线开头的文件，而真实 Vite 产物里有 4 个这样的文件
// （_baseClone、_plugin-vue_export-helper、_app-config 等），漏掉的表现是运行时白屏。
// embed_test.go 的 findUnderscorePrefixedFile 动态查找此类文件，不硬编码文件名。
