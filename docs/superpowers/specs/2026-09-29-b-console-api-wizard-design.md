# B 控制台 API 与向导设计

日期：2026-09-29
状态：设计定稿，待实现
关联：`docs/contracts/http-console.md`、SPEC §2.3、§3.2、§8.3、§12.6
依赖：`2026-09-29-a-four-layer-placeholder-render-design.md`（模型与渲染必须先落地）
并行：`2026-09-29-c-iso-source-design.md`（仅在制品集创建端点相接）

## 1. 目标与验收判据

把 spec A 的四层模型暴露成控制台 API 与可用的界面：装机任务走分步向导（需求 1），
模板与 hook 的编辑器按家族分 tab，模板能派生。

**验收判据**：在浏览器里从零走通一次 debian 装机——创建 ISO（选 `linux` 族）→ 派生
「Linux 基线」模板 → 改一个 hook → 向导建任务 → QEMU 虚拟机装到 `done` → 用注入的
SSH 公钥登入。API 级断言（preview / dryrun / precheck）是辅助手段，不是验收判据。

### 1.1 不在本设计范围

| | 去哪儿 |
|---|---|
| model / store / render 改动 | spec A |
| ISO 来源三选一、上传分片、下载续传、进度条 | spec C |
| 引导菜单与机器面端点 | 不改。`docs/contracts/http-machine.md` 一个字节不变 |

## 2. 端点契约变更

以下全部是 `docs/contracts/http-console.md` 的正式契约变更，走该文档既有的变更记录
流程（文档头已有 2 条记录，本设计追加）。响应继续统一
`{"code":0,"data":...,"message":""}` 且同时给真实 HTTP 状态码，码表在
`internal/apierr/apierr.go`。

### 2.1 制品集

`POST /artifact-sets` 新增必填入参 `family`（`linux` / `windows` / `esxi`）。取值非法
或与导入时识别出的家族不符时拒绝（spec A §2.1 交叉校验）。族不符属于「运维选错了
文件」，报 409 并在 `message` 里同时给出选的族与识别出的家族。

`GET /artifact-sets` 响应新增 `family`，删除 `channel` 与镜像三项。

删除：`channel` 入参与响应字段（spec A §6）。

来源相关入参（`sourceKind` / `url` / 上传）见 spec C。

### 2.2 模板

`POST /templates`、`PUT /templates/{id}` 请求体改动：

| 字段 | 变化 |
|---|---|
| `family` | **新增**，`linux`/`windows`/`esxi`；可空（草稿） |
| `bodies` | **新增**，`{ "<osFamily>": { "text": "...", "hookRefs": [...] } }` |
| `artifactSetId` | **删除** |
| `channel` | **删除** |
| `answerTemplate` | **删除**，被 `bodies.<家族>.text` 取代 |
| `diskPolicy` / `onFailure` / `kernelArgs` / `requiredFormFields` | 不变 |

`hookRefs[].stage` 的类型从枚举字符串变成**自由字符串**（模板正文里的阶段名）。

保存时校验：`bodies` 的键必须落在 `family` 的允许集合里；每条 `hookRef.stage` 必须在
对应正文的 `{{stage:}}` 占位符里出现过；每个被引用的 hook 必须有该家族的实现。

`POST /templates/{id}/activate` 新增校验：至少一份正文；每份正文的
`{{lober:syslog|firstboot|cleanup}}` 各恰好一次（spec A §3.4）。

**新增 `POST /templates/{id}/derive`**：入参只有 `name`。语义是快照复制——复制族、
全部正文、全部 hook 引用，置 `state=draft`、`version=1`、
`derivedFrom={templateId, version}`。之后与源模板无任何联动（spec A §2.5）。

**新增 `GET /templates/{id}/stages?osFamily=<家族>`**：解析该份正文，按出现顺序返回
阶段名列表。这是 UI 的 hook 插入下拉的数据源，避免前端自己解析占位符——正文语法归
用户，占位符解析归服务端，两边各解析一遍迟早不一致。

`POST /templates/{id}/preview` 新增可选入参 `artifactSetId` 与 `osFamily`。给了就用
真实制品集与对应正文渲染，不给则沿用现有假数据行为（现有调用方零破坏）。**preview 是
正文语法的唯一验证手段**（服务端不校验语法，spec A §8），UI 上要显眼，见 §3.3。

### 2.3 hook

`POST /hooks`、`PUT /hooks/{id}` 请求体从单 `body` 改成
`impls: { "<osFamily>": { "interpreter": "...", "body": "..." } }`。

删除入参：`stage`、`osFamilies`、`interpreter`、`body`（四者的去向见 spec A §4.4）。

`GET /hooks/{id}` 响应相应给出 `impls` 映射，每份带 `bodySha256`。

校验：`impls` 非空；每份 `interpreter` 在白名单内（`bash` / `powershell` / `cmd` /
`ash`）；解释器与家族的搭配合理性**不校验**——`windows` 下写 bash 是可能的（装了
WSL 或 busybox 的场景），拦掉会挡住合法用法。

### 2.4 建任务

`POST /tasks` 新增必填入参 `artifactSetId`。校验顺序（前一条不过就不做后一条，这样
错误文案总是指向最上游的那个问题）：

1. ISO 存在且 `state=ready`
2. 模板存在且 `state=active`
3. 模板的 `bodies` 含该 ISO 家族的正文——缺失时报「模板 X 未提供 `<家族>` 家族的
   正文，可补齐该正文或改用其它模板」
4. 该正文引用的每个 hook 都有该家族的实现——缺失时报「hook Y 无 `<家族>` 实现」
5. 固件与架构兼容（沿用现有 `FirmwareCompatible` 与 arch 校验，不变）

删除：通道一致性校验（`internal/console/task.go`，spec A §6）。

第 3、4 项是**新建模板时能通过、建任务时才知道**的约束——选模板时 ISO 还没定，所以
这两道只能放在建任务。409 下用不同的 `code` 区分（沿用 1005–1008 那套分支语义）。

## 3. 前端

`web/apps/web-ele`（Element Plus）。`web/packages/` 下是 vben 上游代码，按迁移契约
不改。构建注意事项见 CLAUDE.md（node 不在 PATH、没有 pnpm、`vite build` 会清掉入库
的占位 dist）。

### 3.1 建任务向导：3 步 → 4 步（需求 1）

现状 `views/tasks/new.vue:327-331` 是三步（选主机 / 选模板 / 填写参数并确认）。改成：

```
① 选主机        SN 必须从已发现主机清单选取，不接受自由文本（现有约束，不变）
② 选 ISO        按主机的架构与固件过滤；只列 state=ready；显示族/家族/版本
③ 选模板        按 ② 选定 ISO 的家族过滤——只列「族包含该家族」且
                「bodies 含该家族正文」的 active 模板
④ 填参数并确认   现有表单 + 目标主机确认框（SPEC §3.2 的必显字段：SN、机架位、
                上次被发现时间、架构/固件）
```

②③ 的顺序是 ISO 在前：模板下拉要按 ISO 的家族过滤，反过来则无法过滤。**格式错配在
UI 层就不可能发生**——preseed 模板根本不会出现在 alma ISO 的候选列表里。第 2.4 节
第 3、4 项的服务端校验仍然保留，它挡的是绕过 UI 直接调 API 的情况。

每步可回退（现有 `backToStep` 机制保留）。回退到 ② 改 ISO 时，③ 已选的模板若不再
兼容要清空并提示，而不是静默保留一个不兼容的选择。

### 3.2 模板编辑器

`views/templates/` 新增详情/编辑页（现在只有 `index.vue` 列表页）。

布局：顶部选族与模板名；中部按家族分 tab（`linux` 族 4 个 tab，`windows`/`esxi` 各
1 个），每个 tab 内左右分栏：

```
┌ 族: linux    名称: [Linux 基线]              [派生] [预览] [激活] ┐
├ [rhel] [debian] [ubuntu*] [suse(未填)]                            │
│ ┌─ 正文编辑器 ───────────────┐ ┌─ 阶段与 hook ──────────────┐ │
│ │ text --plainbmc            │ │ ① 硬件检查                  │ │
│ │ {{lober:syslog}}           │ │    └ 采集磁盘信息   order 10 │ │
│ │ clearpart --all            │ │ ② 装包                      │ │
│ │ part / --ondisk={{fact:…}} │ │    └ 装监控 agent   order 10 │ │
│ │ %pre                       │ │    └ 配 yum 源      order 20 │ │
│ │ {{stage:硬件检查}}          │ │ ③ 配业务网    (空)            │ │
│ │ %end                       │ │                              │ │
│ │ …                          │ │ [插入 hook]                  │ │
│ └────────────────────────────┘ └──────────────────────────────┘ │
└ 未填正文的 tab 标「(未填)」，激活时不强制要求全部填满 ───────────┘
```

**阶段列表不可手工编辑**，它是 `GET /templates/{id}/stages` 的返回——正文里写一个
`{{stage:名字}}` 就多一个阶段，删掉就少一个（spec A §3.1）。这样「两处对不上」这个
故障模式从根上不存在。需求 8 的「空模板需要自己创建阶段」= 在正文里打一个占位符。

正文编辑器要求：等宽字体、行号、**三类占位符语法高亮**（`{{stage:}}` / `{{lober:}}` /
`{{fact:}}` 三色区分），未识别的 `{{lober:xxx}}` 标红。不做语法检查——正文语法归
用户（spec A §8）。

「插入 hook」弹窗：选 hook → 选插到哪个阶段（下拉是本份正文的阶段列表）→ 填 order 与
params。**hook 列表按当前 tab 的家族过滤**，只列有该家族实现的 hook；没有该家族实现
的 hook 灰显并给出原因，而不是从列表里消失——后者会让运维以为 hook 不存在。

### 3.3 预览是语法的唯一验证手段

「预览」按钮调 `POST /templates/{id}/preview`，带上当前 tab 的家族与一个可选的真实
ISO。产出直接展示渲染后的完整应答文件文本，占位符已展开。

这个按钮要显眼（不是藏在下拉菜单里）：服务端不校验正文语法，preview 是模板作者在
装机之前能拿到的唯一反馈。渲染报错（未识别占位符、fact 不存在、hook 缺实现）在这里
以可读文案呈现。

### 3.4 hook 编辑器

`views/hooks/index.vue`（817 行）已有列表与编辑。改动：单 `body` 编辑区改成按家族
分 tab，每个 tab 内一个解释器下拉加一个正文编辑器。tab 可增可删——增一个 tab = 为该
家族加一份实现，删一个 tab = 该家族不再支持。

删掉 UI 上的 `stage` 与 `osFamilies` 两个控件（前者移到模板的 hook 插入弹窗，后者由
tab 集合隐含）。

### 3.5 派生的界面动作

模板编辑页的「派生」按钮 → 弹窗填新名称 → 调 `POST /templates/{id}/derive` → 跳转到
新模板的编辑页。新页面顶部显示「派生自 X（v2）」。

需求 11 的「覆盖 a hook 用 b hook」在这里就是：派生出新模板，在某个 tab 的阶段里删掉
a 的引用、插入 b。没有专门的「覆盖」操作。

## 4. Windows WinPE 四文件来源

这是 `2026-09-28` 设计 §6 已定的方案，原样移入本 spec（它属控制台/导入层，与 spec A 的
渲染改造无关）。

### 4.1 缺口

`wimboot` / `BCD` / `boot.sdi` / `boot.wim` 四个文件**不在 Windows ISO 里**
（`internal/imports/extract_windows_family.go:28-45`），而 `ImportSource` 只有
`ISOPath` 与 `NetbootPath`（`internal/console/artifact.go:63-80`），装配点也从未调用
`ConfigureWindows`。**Windows 目前无法通过 API 导入**，是六家族里唯一完全不通的一条，
必然失败在 `internal/imports/job.go:290-296`。

### 4.2 方案：API 优先，回落配置约定

`ImportSource` 新增可空的 `WinPE *WinPESources`（四个路径字段）。装配点实现
`imports.WindowsSourcesResolver`：

1. 请求带了 `WinPE` 且四个路径全非空 → 用它
2. 任一为空 → 回落配置约定
   `cfg.Windows.WinPEDir/<osVersion>-<firmware>/{wimboot,BCD,boot.sdi,boot.wim}`
3. 两处都拿不到 → 导入失败，错误信息把两条路都写清

同时在 `cmd/lober/wire.go` 调 `job.ConfigureWindows(resolver, archDetector)`。这一步
是**必须**的，否则 Windows 导入必然失败。

## 5. 风险

| # | 风险 | 缓解 |
|---|---|---|
| 1 | 前端工具链本机受限（node 不在 PATH、无 pnpm、`vite build` 清掉入库的占位 dist） | 按 CLAUDE.md 的既有命令走；只在确实要验证打包时 build，跑完 `git restore internal/console/ui/dist/` |
| 2 | 占位符高亮要自己写（Element Plus 没有现成的代码编辑器） | 先用 `<textarea>` 加等宽字体与行号交付可用版本，高亮作为增强项；不为高亮引入重型编辑器依赖 |
| 3 | 向导第 ② 步改 ISO 后第 ③ 步的模板可能不兼容 | 明确清空并提示，不静默保留（§3.1 末段） |
| 4 | 契约变更影响现有 e2e 脚本（`scripts/e2e*.sh` 用 curl 打点） | 脚本里建任务的请求体要加 `artifactSetId`；这批脚本属 `test` 分支职责，改动随本 spec 一起走 |
| 5 | Windows 导入需人工准备 WinPE 四件套 | §4 已写明这是前提；Windows 真机验证排在最后一批 |

## 6. 实现阶段划分

依赖 spec A 全部落地。每段末尾 `make check` 全绿。

| 段 | 内容 | 出口判据 |
|---|---|---|
| B1 | 制品集与模板端点契约变更（`family`、`bodies`、删 `channel`/`artifactSetId`） | `internal/console` 单测覆盖新校验；契约文档同步 |
| B2 | hook 端点 `impls` 契约变更 | 同上 |
| B3 | `derive` 与 `stages` 两个新端点 | 派生后两者独立（改源模板不影响派生品）有测试 |
| B4 | 建任务 `artifactSetId` 与四道交叉校验 | 每道校验各有一个错误文案断言 |
| B5 | 向导 4 步 | 浏览器里走通一次建任务 |
| B6 | 模板编辑器（多 tab + 阶段列表 + hook 插入 + 预览） | 浏览器里派生并改一个 hook |
| B7 | hook 编辑器多 tab | 浏览器里给一个 hook 加一份 suse 实现 |
| B8 | Windows WinPE 来源 + 装配点接线 | Windows ISO 能通过 API 导入到 `ready` |

