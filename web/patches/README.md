# 依赖安全补丁

`@rollup__plugin-node-resolve@16.0.3.patch` 将插件复制默认配置的唯一一次 `deepmerge({}, defaults)` 调用改为 Node 原生 `structuredClone(defaults)`，覆盖 CommonJS 和 ESM 两个入口。

默认配置只含普通对象、数组、字符串和布尔值。项目要求的 Node 22.18/24.12 均支持原生深复制。通过限定在该插件的依赖覆盖移除 deepmerge，不影响其他包，也不设置扫描忽略规则。

安装由 pnpm 自动应用补丁并核对锁文件中的校验值；升级插件时需重新核对调用方式和补丁。回归测试位于 `scripts/dependency-security.test.mjs`，验证插件两种入口的默认配置及真实包解析构建。
