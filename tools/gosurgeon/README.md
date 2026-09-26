# gosurgeon

对 Go 源码做**行级精确编辑**的小工具，给 vmlab 这一轮的反复改动用。

## 它解决的问题

这一轮要在 `internal/render`、`internal/statemachine` 等文件里反复插入/替换
代码块，而那些块里大量出现 `\n`、`%q`、反引号。此前用 `python3 - <<'EOF'`
的内联 heredoc 做替换，踩了同一个坑六次以上：

bash 的 heredoc 不解析反斜杠，但 Python 的**非 raw 字符串**会把两字符的
反斜杠-n 解成真换行。于是本该写进 Go 源码的

    fmt.Fprintf(&b, "signal %q 0\n", enter)

变成两行、字符串字面量未闭合、编译失败。每次都要再跑 gofmt 与 go build 才
发现，来回好几轮。

gosurgeon 把这类编辑收成显式的行操作：内容文件里用 `@NL@` 占位表示 Go 源码
中的反斜杠-n，工具负责还原。内容文件本身不含任何需要二次转义的东西。

## 用法

```bash
export PATH="/c/Users/v_lvxiaoyang/.local/bin:$PATH"
uv run --project tools/gosurgeon gosurgeon <子命令> [参数]
```

- `replace-range <文件> <起始锚点> <结束锚点> <内容文件>`
  用内容文件替换两个锚点（含）之间的所有行。锚点是子串匹配，必须唯一命中，
  否则报错退出——不猜。
- `insert-after <文件> <锚点> <内容文件>` 在锚点行之后插入。
- `verify <文件>` 纯文本扫描 Go 源码里未闭合的字符串字面量（上面那个坑的
  直接症状），不依赖 go 工具链。

所有子命令写文件时统一用 LF 行尾与 UTF-8，与仓库其余 Go 文件一致。
