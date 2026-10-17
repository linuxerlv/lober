# 补充检查

这些脚本是在指定测试 VM 上实际执行的只读补充检查，保留供核查结果及复现。
项目的正式入口仍是 `tests/lobertest/enterprise/README.md`，候选身份由本轮清单绑定。

- `check-tiptap-lock.py <lockfile>`：检查 Tiptap 精确版本族。旧提交 78065b6 的锁文件返回 1，
  统一版本后的锁文件返回 0；输出分别保存为 tiptap-peer-red/green.json。
- `check-css.py`：在本轮 VM 固定目录中比较解析器替换前后的完整生产 CSS 文件名及摘要；
  不需要运行应用，也不读取配置秘密。比较通过写入 css-compatibility.json。

候选源码归档、前端归档、二进制及受限数据库连接串保留在 VM，未放入此公开证据目录。
`results/candidate.json` 保存公开摘要及逐文件映射；日志也由各结果 JSON 中的摘要校验。
