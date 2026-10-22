# 数据列表服务端分页 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Independent frontend and backend domains use superpowers:dispatching-parallel-agents.

**Goal:** 为系统主列表及增长中的选择列表提供真实后端分页和统一 Element Plus 页码交互。
**Architecture:** 页码与旧游标并存，仓储统一验证与计数；前端共享分页状态和 ElPagination。接口约定固定后，各端可独立开发。
**Tech Stack:** Go/GORM/PostgreSQL、Vue/Element Plus/Vben、Vitest/Playwright。
**Spec:** `docs/superpowers/specs/2026-10-22-numbered-pagination-design.md`

## Global Constraints
- 本地修改，所有测试同步云端执行；不修改现有产品服务数据。
- 保留此前未提交的角色菜单、用户显示和账号弹窗功能。
- 请求 page/page_size，默认 1/20，上限 500；前端选项 10/20/50/100。
- 旧 cursor/limit 和全量审计导出兼容；仅真正页码模式要求 total。

## Review Focus
- 过滤后的总数不能变成当前页长度或跳过页后的数量。
- hook/模板必须按身份选出正确版本后再计数、分页。
- 并行请求不能用旧响应覆盖新筛选；请求失败不应显示错误页码。
- 删除末页最后一条自动回退；改每页条数和筛选回到第一页。
- 跨页选择不能丢失已选主机，候选可用性筛选不能发生在分页之后。

### Task 1: 后端主列表
Ownership: `internal/store/**`, `internal/console/**`，排除生成前端文件。
- [x] 新增仓储/HTTP 分页回归测试，云端运行观察失败。
- [x] 七类查询加入 Page；Page[T] 加可选 Total。共享分页校验及 GORM 计数/偏移，保留稳定次序。
- [x] 七个 HTTP 列表解析 page/page_size、返回 total；拒绝混用与非法输入，保留游标模式。
- [x] 云端运行分页、旧游标、过滤及导出测试。

### Task 2: 公共前端分页与主列表一组
Ownership: `src/components/console/ListPagination.vue`, `src/composables/numbered-page*`, `src/views/hosts/**`, `src/views/artifact-sets/**`, `src/views/access/users*`, `src/api/console/accounts*`。
Interfaces: `NumberedPage<T>` exposes `page`, `pageSize`, `total`, `items`, `loading`, `setPage(n)`, `setPageSize(n)`, `reset()`, `load(fn)`; fn receives `{page,page_size}` and resolves `{items,total?,next?}`. `load` returns `{items,total}`. `ListPagination` props `currentPage,pageSize,total,disabled`; emits `page-change`, `size-change`.
- [x] 编写分页状态和账号列表分页测试，云端观察失败。
- [x] 实现公共状态与分页条，迁移主机、制品集和账号管理。
- [x] 云端验证页码切换、失败恢复、过期响应、末页回退；保留弹窗功能。

### Task 3: 其余列表与次级选择
Ownership: `src/views/tasks/**`, `src/views/hooks/**`, `src/views/templates/**`, `src/views/audit/**`, `src/views/users/**`, `internal/console/wizard*`, `internal/console/authoring_helpers*`。
- [x] 使用 Task 2 的公共接口迁移任务、hook、模板、审计和旧用户入口。
- [x] 梳理建任务/候选/版本/事件列表，补齐有增长风险的分页并保持跨页选择。
- [x] 云端运行相关 UI 测试；修正断言与接口适配，不回退业务权限和候选验证。

### Task 4: 集成和验证
- [x] 重新生成 OpenAPI 前端类型；全量前端测试、lint、类型与生产构建。
- [x] 云端真实 PostgreSQL 相关仓储与 console 测试、vet；独立实例做多页浏览器验证并保存截图。
- [x] 只读代码审查，修复实质问题，记录兼容性和验证结果。
