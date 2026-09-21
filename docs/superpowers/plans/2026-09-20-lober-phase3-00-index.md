# Lober Phase 3 实现计划索引

> **前置：Phase 2 全部完成，三个 Linux 家族在实机装通过。**
>
> 写法与前两期一致：**不写代码**，每个任务给「为什么需要 / 设计要点 / 实现流程 /
> 测试要锁住的行为 / 验证 / 完成标志」。

**目标：** SPEC §11 Phase 3 —— 镜像直写通道、Windows、ESXi、gRPC 加载器、Starlark 决策。

**Phase 3 与前两期的本质差异：前两期都在「installer 驱动」这一套机制里，
Phase 3 引入第二套机制。** Windows 的安装动作由 WinPE 内的工具执行、
ESXi 的引导器是 UEFI 专用的独立二进制、镜像直写完全绕过 installer ——
三者与 kickstart 那套没有共用的部分。

**所以 Phase 3 的主要风险是「用错机制」**：试图把 Windows 塞进 installer 驱动的框架、
或用一套渲染器同时服务 kickstart 与 unattend.xml。各计划的边界一节写明了这条。

---

## 开工前提：两项要先做独立 PoC

SPEC 对 Phase 3 的两项标了 PoC 建议，理由是机制不确定而非工作量大：

| 项 | SPEC 依据 | 为什么先 PoC |
|---|---|---|
| ESXi 的 BIOS 路径 | §11 Phase 3、附录 B | 依赖 iPXE 的 COMBOOT 支持（**仅存在于 BIOS 构建，且部分预编译二进制未开启**）与旧版 syslinux。走不通的话整条路径要换方案 |
| Starlark 决策引擎 | §11 Phase 3、§7.6 | **引入条件是「规则表已长出嵌套条件」** —— 若 Phase 2 跑完规则表还是平的，这一项不做 |

**ESXi 的 BIOS 路径在 MVP 与 Phase 2 都明确排除**（§0.3：仅 UEFI）。
Phase 3 也**默认不做** —— 只在「届时确有 BIOS 机器需装 ESXi」时才做 PoC（§11）。

---

## 计划清单

| 计划 | 内容 | 前置 | 可否并行 |
|---|---|---|---|
| [P3-01](2026-09-20-lober-phase3-01-windows.md) | Windows：wimboot + WinPE + unattend.xml | Phase 2 | 是 |
| [P3-02](2026-09-20-lober-phase3-02-esxi.md) | ESXi：渲染 boot.cfg，仅 UEFI | Phase 2 | 是 |
| [P3-03](2026-09-20-lober-phase3-03-image-pipeline.md) | 镜像封装流水线与直写通道 | Phase 2 | 是 |
| [P3-04](2026-09-20-lober-phase3-04-grpc-starlark.md) | gRPC 加载器 + Starlark（**按需**） | P2-03 | 是 |

四份互不依赖。**P3-04 的两项都有开工前提** —— 依据不足时推迟，那也是有效交付。

---

## 一条贯穿 Phase 3 的架构判断

**镜像直写与 installer 驱动并存，不是替代**（§11 Phase 3 明确写了分工）：

| 通道 | 定位 | 覆盖 |
|---|---|---|
| installer 驱动（已有） | **兜底** | 新 OS 版本、特殊机型、调试 |
| 镜像直写（新增） | **主力** | 批量与一致性要求 |

**这个分工要在数据模型上体现** —— 制品集要能区分两类，而不是给镜像直写另建一套模型。
P3-03 的任务 1 处理这件事。

**为什么兜底通道不能废：** 新 OS 版本发布时镜像还没封装好，而机房可能已经要装它了；
特殊机型的驱动问题往往要在 installer 里现场调；调试时需要能停在 shell 里看
（§2.3 的失败处置策略）。**镜像直写做不到这三件事。**

---

## SPEC 覆盖度映射

| SPEC 章节 | 由哪个计划实现 |
|---|---|
| §0.3 Windows 行（按固件维护两套 WinPE 制品） | P3-01 |
| §0.3 ESXi 行（仅 UEFI） | P3-02 |
| §1.2 固件模式影响引导形态（Windows 与 ESXi） | P3-01、P3-02 |
| §2.1 Windows 与 ESXi 的识别特征文件 | P3-01、P3-02 |
| §2.1 ESXi 的 boot.cfg 由网关渲染 | P3-02 |
| §4.5 Windows 与 ESXi 菜单形态 | P3-01、P3-02 |
| §4.6 固件与 OS 不兼容的硬拒绝（Windows 选制品键） | P3-01 |
| §5.2 Windows 的 firstboot 对应机制 | P3-01 |
| §6.5 ESXi 的 syslog 配置方式 | P3-02 |
| §7.2 gRPC 加载器 | P3-04 |
| §7.3 热更新五项（完整落地） | P3-04 |
| §7.6 Starlark 的引入信号 | P3-04 |
| §9.6 镜像签名（气隙下 key-based） | P3-03 |
| §11 Phase 3 全部七条 | 四份计划 |
| 附录 A.4–A.6 wimboot 事实 | P3-01 |
| 附录 A.7–A.9 ESXi kickstart 事实 | P3-02 |
| 附录 A.11 desync 事实 | P3-03（标注为 Phase 4 的前置调研） |
| 附录 A.14 ESXi BIOS 机制差异 | P3-02 的 PoC 章节 |
| 附录 B 镜像构建工具边界 | P3-03 的任务 1 |

**Phase 3 不做的**（留 Phase 4）：TLS、mTLS、Secure Boot、UEFI HTTP Boot、
并发限流、desync 增量传输、审计报表。
