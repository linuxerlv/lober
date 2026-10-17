# lobertest

> 当前重写版请使用 [enterprise/README.md](enterprise/README.md)。下面是旧架构的历史说明；
> 旧 L1/L2 客户端使用已废弃接口，默认 pytest 也会排除 VM/慢测，不可作为当前产品验收入口。

lober 的三层自动化测试工程：接口层（L1）/ 装机层（L2）/ 前端层（L3），全部跑在
Linux VM 的真实部署上。设计见
`docs/superpowers/specs/2026-09-26-lobertest-automation-design.md`，实现计划见
`docs/superpowers/plans/2026-09-26-lobertest-automation.md`。

## 当前状态

任务 1（uv 工程骨架与两个叶子模块）已完成：`config.py` 与 `envelope.py` 可独立
测试，不依赖 VM。L1/L2/L3 尚未实现。

## 怎么跑

```bash
uv sync
uv run pytest -q
```

## 环境变量

`config.load()` 支持通过环境变量覆盖默认值，前缀 `LOBERTEST_`，例如
`LOBERTEST_SSH_HOST=10.20.20.2`。字段列表见 `src/lobertest/config.py`。
