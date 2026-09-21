# Lober MVP 实现计划索引

> SPEC `SPEC.md` 0.4 版的 Phase 1 MVP（§11）拆成九份模块计划。每份都能独立开发、独立验证。
> **执行者请从 `2026-09-20-lober-mvp-01-contracts.md` 开始** —— 它是其余八份的前置。

## 这些计划的写法

**计划里不写代码。** 每个任务点给的是四样东西：

| 部分 | 内容 |
|---|---|
| 为什么需要这一步 | 依据哪条 SPEC 要求，不做会怎样 |
| 设计要点 | 每条是一个决策加上为什么这样决定 |
| 实现流程 | 编号步骤、判定分支、顺序上的硬约束及其理由 |
| 测试要锁住的行为 | 每条说清「什么行为」加「不锁住会怎样」 |

加上精确的产出文件路径、验证命令与 commit 信息。

**代码由执行者写。** 计划的价值在于让执行者理解**为什么**这样做 ——
一份塞满代码的计划等于直接写代码，只是多套了一层复选框，照抄完不会留下任何理解。

**遇到计划没说清的细节，按设计要点的意图判断，不要为了「像计划里写的」而牺牲正确性。**
若发现计划与 SPEC 冲突，或某条设计要点在实现时站不住，停下来说明，不要静默绕过。

**目标：** 一台机器全自动装完 Rocky 9，且全过程可见（SPEC §11 Phase 1）。

**拆分原则：** 按模块拆，每个模块自带契约。模块之间只通过 `internal/core/contract`
的接口与 `docs/contracts/` 的 HTTP / 文件系统契约通信，**不互相 import 实现包**。
集成时对契约装配，不需要联调返工。

---

## 依赖拓扑

```
        ┌──────────────────────────────┐
        │ M1 契约与地基（串行前置）      │
        │ model / contract / schema    │
        │ conformance / fake / config  │
        └──────────────┬───────────────┘
                       │  七个模块在此之上完全并行
   ┌────────┬──────────┼──────────┬──────────┬─────────┐
   ▼        ▼          ▼          ▼          ▼         ▼
┌─────┐ ┌──────┐ ┌─────────┐ ┌───────┐ ┌────────┐ ┌──────┐
│ M2  │ │ M3   │ │ M4      │ │ M5    │ │ M6     │ │ M7   │
│存储 │ │状态机│ │渲染器   │ │网关   │ │控制台  │ │插件  │
│     │ │      │ │         │ │机器面 │ │人面    │ │导入  │
└──┬──┘ └──┬───┘ └────┬────┘ └───┬───┘ └───┬────┘ └──┬───┘
   │       │          │          │         │         │
   │       │          │          │         ▼         │
   │       │          │          │     ┌──────┐      │
   │       │          │          │     │ M8   │      │
   │       │          │          │     │WebUI │      │
   │       │          │          │     └──┬───┘      │
   └───────┴──────────┴──────────┴────────┴──────────┘
                       │
                       ▼
        ┌──────────────────────────────┐
        │ M9 装配、部署产物、端到端验证 │
        └──────────────────────────────┘
```

**M1 必须先完成。** M2–M7 之后完全并行 —— 它们都用 M1 的内存 Store 做测试夹具，
不等 M2 的 SQLite 实现。M8 只依赖 `docs/contracts/http-console.md`，可以与 M6 同时开工
（不必等 M6 的 Go 代码）。M9 收口。

---

## 模块清单

| 计划 | 模块 | 包路径 | 契约边界 | 独立验证方式 |
|---|---|---|---|---|
| [01](2026-09-20-lober-mvp-01-contracts.md) | 契约与地基 | `internal/core/{model,contract,fake}`、`internal/{apierr,config,archtest}`、`internal/store/sqlite/schema` | 产出契约本身 | `make check` 全绿；conformance 套件在内存实现上全绿 |
| [02](2026-09-20-lober-mvp-02-store.md) | SQLite 存储层 | `internal/store/sqlite` | 实现 `contract.Store` + `TxRunner` | 跑 M1 的 conformance 套件；`-race` 下并发 CAS |
| [03](2026-09-20-lober-mvp-03-statemachine.md) | 状态机与超时扫描 | `internal/statemachine` | 实现 `contract.StateMachine` | 用 fake Store + fake Clock 跑全状态转换与超时 |
| [04](2026-09-20-lober-mvp-04-renderer.md) | 渲染器 | `internal/render` | 实现 `contract.Renderer` | golden file 对比；注入防护用例 |
| [05](2026-09-20-lober-mvp-05-gateway.md) | 网关机器面 | `internal/gateway` | `docs/contracts/http-machine.md` | `httptest` 全端点；curl 模拟机器打点 |
| [06](2026-09-20-lober-mvp-06-console.md) | 控制台人面 | `internal/console` | `docs/contracts/http-console.md` | `httptest` 全端点；RBAC 与 SSE 续传 |
| [07](2026-09-20-lober-mvp-07-providers.md) | Provider 与 ISO 导入 | `internal/provider`、`internal/imports` | 实现 §7.1 七个接口 + 导入 job | 内置策略单测；假 ISO 树导入 |
| [08](2026-09-20-lober-mvp-08-webui.md) | WebUI | `web/`、`internal/console/ui` | `docs/contracts/http-console.md` | `pnpm build` 产出 dist；`go:embed` 可编译 |
| [09](2026-09-20-lober-mvp-09-integration.md) | 装配与端到端 | `cmd/lober`、`deploy/` | 组装全部模块 | 整条链本地跑通，不碰硬件（§11 验证行） |

---

## 集成规则

**契约不许在模块计划里改。** 如果某个模块发现契约缺东西，走这个流程：

1. 停下，不要在本模块里加一个本地变通
2. 改 `internal/core/contract` 的接口
3. 同步改 `contract/conformance` 的断言
4. 同步改 `internal/core/fake` 的实现
5. 通知所有并行模块（契约变更会让别人的编译失败，这是特性不是缺陷）

这个流程刻意做得麻烦。契约漂移是并行开发唯一的致命风险 —— 三个模块各自对
`FindPendingTaskByIdentity` 的语义有不同假设，集成时才发现，那时三边都得改。

**装配点只有一个：`cmd/lober`（M9）。** `internal/archtest/deps_test.go` 会在 CI 里
拦住任何跨模块的直接 import。

---

## SPEC 覆盖度映射

| SPEC 章节 | 由哪个模块实现 |
|---|---|
| §0.1 范围、§0.2 排除项 | 全局约束，体现在 M3 的状态机与 M5 的 `no_task` 路径 |
| §0.3 目标 OS、固件维度 | M1（兼容矩阵）、M4（菜单形态）、M5（§4.6 校验） |
| §1.1 分包 | M1 的目录结构 + M9 的双 listener 装配 |
| §1.2 引导链、§1.3 DHCP 配置 | M9（部署产物，一次性核对清单） |
| §1.4 nginx、§1.5 无 TLS、§1.6 反代陷阱 | M9（nginx 配置）、M5（`clientIP` 只信 loopback peer） |
| §2.1 artifact_set | M1（类型 + schema）、M7（导入 job） |
| §2.2 hook、§2.3 template | M1（类型 + schema）、M6（CRUD + 校验） |
| §2.4 task、§2.5 event | M1（类型 + schema）、M2（存储） |
| §2.6 pin 语义、§2.7 引用完整性 | M1（conformance 断言）、M2（实现） |
| §3.1 状态转换、§3.2 建任务即授权 | M3 |
| §3.3 分阶段超时 | M1（预算表）、M3（扫描循环） |
| §3.4 禁止重装 | M1（`Reinstallable()`）、M3（`Retry` 拒绝） |
| §3.5 失败带信息、§3.6 并发幂等 | M3、M2 |
| §4.1 两段 HTTP 链、§4.2 `/v1/boot` 契约 | M5 |
| §4.3 主机识别、§4.4 token | M1（归一化 + TTL）、M5（双因子校验） |
| §4.5 菜单形态、§4.6 一致性校验 | M4（菜单）、M5（Prechecker 调用） |
| §4.7 多机异构并发 | M5（单一分流点）+ M9（DHCP 零改动验证） |
| §5.1 渲染器三段、§5.2 阶段映射 | M4 |
| §5.3 参数内联、§5.4 fact 命名空间 | M1（键常量）、M4（注入） |
| §5.5 facts KV | M1（接口）、M5（端点） |
| §5.6 服务端决策、§5.7 写配置不激活 | M7（DiskPlanner）、M4（模板骨架） |
| §5.8 firstboot 脚手架 | M4 |
| §6.1 信号点、§6.2 协议 | M1（信号表）、M5（端点） |
| §6.3 客户端封装、§6.4 token 跨重启 | M4（注入 `signal()` 与 `run_hook`）、M3（finalize token） |
| §6.5 syslog、§6.6 心跳 | M5（UDP receiver）、M7（Heartbeat 实现） |
| §7.1 接口清单、§7.4 失败语义 | M1 |
| §7.2/7.3 加载器与热更新 | **MVP 不做**（§7.6），仅接口保留 |
| §7.5 耦合分层、§7.6 内置实现 | M7 |
| §8.1 机器面 API | M5 |
| §8.2 人面 API、§8.3 表单聚合 | M6 |
| §9.1 三道闸门 | M5（默认拒绝 + attempt 封顶）、M6（SN 从清单选） |
| §9.2 `X-Real-IP` | M5 |
| §9.3 制品授权 | **MVP 仅开放仓库**；`secure_link` 是 Phase 2（§11） |
| §9.4 权限分级 | M6（RBAC） |
| §9.5 凭据清理 | M4（渲染器骨架） |
| §9.6 镜像签名 | **MVP 不做**（Phase 3） |
| §10 失败模式 1/2/2b/4b/5/6/7 | M5（`sanboot`、双因子、§4.6）、M4（by-id、写配置不激活）、M6（建任务确认） |
| §10 失败模式 3/4/8 | M9（部署期一次性核对，运行期不检测） |
| §11 Phase 1 清单 | 本索引即 Phase 1 的分解 |
| §12.1–12.4 选型与精简 | M8 |
| §12.5 认证与响应约定 | M6（端点）、M8（前端对接） |
| §12.6 页面清单、§12.7 表单转换、§12.8 SSE | M8 |
| 附录 B 待验证项 | M9 的实机验证清单；代码里全部做成配置或查表，不散落硬编码 |

**MVP 明确不做**（§11）：TLS、UEFI HTTP Boot、插件加载器、镜像直写、Windows、ESXi、
多播、增量传输。M4 的渲染器为 Windows / ESXi 留了菜单分支的位置但不实现。

---

## 执行建议

**串行段（M1）：** 一个人或一个 agent 做完，约 18 个 commit。这段不要并行 —— 契约必须
出自一个头脑，否则命名与粒度会打架。

**并行段（M2–M8）：** 七个独立工作流。若用 subagent-driven-development，每个模块一个
子代理，每任务后审查。若人工分工，按上表的「包路径」分派，各自在自己的目录里工作，
`internal/archtest` 保证没人越界。

**收口段（M9）：** 需要全部模块就位。它的端到端验证脚本是整个 MVP 的验收标准。

---

## 与 SPEC 的张力：两处需在执行时决断

写计划时发现 SPEC 内部有两处需要明确解读，已在对应模块计划里标注，此处汇总备查：

1. **`booting` 超时后回 `pending` 还是直接 `failed`。**
   §3.3 表里写「允许重试，attempt++」，§3.4 表里把「booting 超时」列为失败态。
   M3 的计划采用：超时后若 `attempt_count < 3` 则回 `pending`（机器可自行再来一次），
   已达上限则 `failed(attempt_exceeded)`。理由是两处都能满足，且符合 §9.1 第三道闸门
   「同一任务窗口内菜单最多取 3 次」的表述。

2. **`FailPrecheck` 是否允许 retry。**
   §4.6 说这类失败「重试必然同样失败」，§8.2 却只要求 retry 拒绝 `rebooting` /
   `initializing` 两类超时。M3 的计划按 §8.2 的显式要求实现（允许 retry），
   由 M8 在 UI 上提示「需先修正任务配置」。不自行扩大拒绝范围。
