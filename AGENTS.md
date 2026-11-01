# Repository Guidelines

## Project Structure & Module Organization

The Go service lives in `cmd/lober` and `internal/` (core, storage, rendering, ISO, console, and architecture checks); tests sit beside packages. `web/` is a pnpm workspace; its Vue app is `web/apps/web-ele` and shared packages are in `web/packages/`. Engineering documentation is in `docs/`.

## Build, Test, and Development Commands

Run root `make check` for Go formatting, vet, tests, race checks, Linux amd64 build, and Wire drift detection. Use `go test ./internal/core/...` for focused tests. PostgreSQL tests require `LOBER_TEST_PG_DSN` or the untracked `.pgtest.dsn`.

From `web/`, use `pnpm install`, `pnpm dev:ele`, and `pnpm build:ele` to install, run, and build the app. Check types with `pnpm --filter @vben/web-ele typecheck`; run frontend tests with `pnpm exec vitest run <path>`.

## Coding Style & Naming Conventions

Format Go with `gofmt`; name tests `*_test.go`. Follow Vue 3/TypeScript patterns and use `.vue` single-file components in `web/apps/web-ele/src`. Use the workspace ESLint/formatter and consult `web/UPSTREAM_PIN.md` before changing shared upstream code.

## Testing Guidelines

Add Go `Test<Behavior>` cases beside changes, including relevant golden, contract, or architecture coverage. Run `make check` for backend changes; for frontend changes run typecheck, focused Vitest tests, and build as relevant.

## Commit & Pull Request Guidelines

Use concise conventional commits such as `feat(console): ...`, `fix(iso): ...`, or `docs: ...`. PRs should describe the change, report validation, link related issues, include UI screenshots, and call out schema, config, or generated-code changes.

## Branch & Release Workflow

Develop each feature or module on its own named branch, then merge it into `develop` for integration testing. If integration testing finds a defect, roll back the problematic feature or module branch; unaffected branches may be submitted again. Fix the failing branch and rerun integration testing until the combined changes pass. Then sync the integrated code to `test` and run the enterprise test suite. After it passes, sync to `preview` for manual acceptance and internal testing. Promote to `main` only after that review passes. For a bug found on `main`, create a `hotfix` branch and follow the patch release flow through testing and preview before merging the fix into `main`.

## Security & Configuration

Never commit credentials or local DSNs. Keep PostgreSQL test credentials in `LOBER_TEST_PG_DSN` or ignored `.pgtest.dsn`; document new required configuration.

## Agent 规则：说人话

### 核心原则

所有回复都要让没学过专业知识的普通人看懂。

### 语言规则

1. **禁止黑话。** 不用专业术语；必须使用时，紧跟一句大白话解释。不用英文缩写；KPI、ROI、MVP 等词要翻译或换成中文。
2. **禁止废话。** 不说“综上所述”“值得注意的是”“需要指出的是”等填充语，不重复用户已经说过的内容，开头直接给答案。
3. **短句优先。** 一句话只说一件事。一段不超过 3 行。能用 10 个字说清的，不用 30 个字。
4. **结构清晰。** 超过 3 个要点时用列表或编号；有步骤就按顺序编号；对比内容用表格。
5. **先说结论，再说原因。** 第一句给答案、结论或建议，解释放在后面。
6. **具体不空洞。** 给例子、数字和可以直接照做的动作。比如，不只说“注意性能”，要说“图片先压缩再上传，不然加载要 5 秒”。
7. **诚实直说。** 不知道就说“不知道”，不编造；有风险就直接指出；用户方案有问题就明确说明。

### 语气规则

像朋友聊天，不像写公文。可以适度口语化，但别刻意卖萌。少用表情符号，最多在关键处点缀。

### 长度规则

简单问题用几句话回答。复杂问题先用一句话总结，再展开细节。用户说“详细讲讲”时再写长篇。

### 代码相关

先说代码的作用和用法，再贴代码。改动说明用一两句话讲清改了什么、为什么改。代码注释也用中文大白话写。
