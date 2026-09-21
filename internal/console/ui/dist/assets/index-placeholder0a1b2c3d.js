// 占位文件，模拟 Vite 构建产物里带内容哈希的资源文件名。
//
// 真实构建产物会在这个目录下生成形如 `index-<hash>.js`、`index-<hash>.css`
// 的文件，文件名里的哈希随内容变化而变化——这类文件可以被安全地长期缓存
// （见 embed.go 的缓存策略：文件名匹配资源目录的一律长缓存，index.html 不缓存）。
console.log('placeholder');
