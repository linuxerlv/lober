# B 控制台 API 与向导 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 把四层模型暴露成控制台 API 与可用界面：建任务走 4 步向导，模板与 hook 编辑器按家族分 tab，模板可派生。

**架构：** 四组端点契约变更（制品集加 `family`、模板改 `family`+`bodies`、hook 改 `impls`、建任务加 `artifactSetId`），新增 3 个端点（`GET /templates` 补现存缺口、`derive`、`stages`）。前端向导第 ② 步插入选 ISO，模板与 hook 编辑器改多 tab，阶段列表由服务端解析正文得出——前端不自己解析占位符。

**技术栈：** Go 1.27 + gin、Vue 3 + Element Plus（`web/apps/web-ele`）

**设计依据：** `docs/superpowers/specs/2026-09-29-b-console-api-wizard-design.md`

**前置依赖：** plan A（`2026-09-29-a-four-layer-placeholder-render.md`）**全部 17 个任务
必须先完成**。本计划的每个端点都读写 plan A 定义的 model 类型，A 未完成时本计划一行
都编译不过。

---

## 架构约束（每个任务都必须遵守）

1. **依赖方向**：`internal/console` 不得 import 其它六个实现模块（`archtest/deps_test.go:273`
   守卫，**含测试 import**）。跨模块能力用「调用方定义窄接口 + 装配点注入」这套接缝，
   现成范例是 `console.ArtifactImporter`（`internal/console/artifact.go:48`）。
2. **Server 用 `Deps` 注入窄接口**（`contract.TemplateStore` 而不是整个 `contract.Store`），
   测试桩只实现真正用到的方法。
3. **响应统一 `{"code":0,"data":...,"message":""}` 且同时给真实 HTTP 状态码。** 两者
   不冗余：状态码给 curl/网关/监控，`code` 区分同一状态码下的不同业务分支。码表在
   `internal/apierr/apierr.go`。
4. **错误信息用中文且给可行下一步**，沿用 `internal/console/task.go:63-76` 的风格。
5. **状态转换不在 handler 里判定**——`internal/statemachine` 是 `task.state` 的唯一所有者。
6. **提交信息用中文**，格式 `类型(范围): 简述`。

## 契约变更纪律

本计划改的是 `docs/contracts/http-console.md` 里**冻结**的端点。每个契约变更任务的末尾
都要在该文档追加一条变更记录（文档头已有 2 条，按同样格式续写）。**不要在多个任务里
攒着最后一起写**——契约文档与代码不一致的窗口期越短越好。

## 测试命令约定

```bash
export LOBER_TEST_PG_DSN='postgres://lober_test:lober_test_pw@localhost:5432/lober_test?sslmode=disable'
make check                                   # 后端收口
go test ./internal/console/ -count=1 -v      # 单包
```

前端（本机 node 不在 PATH、没有 pnpm、`node_modules/.bin/` 的 shim 用不了）：

```bash
export PATH="/c/Program Files/nodejs:$PATH"
cd web/apps/web-ele && node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck
cd web && node node_modules/eslint/bin/eslint.js --fix <文件>
```

**不要跑 `vite build`** 除非确实要验证打包：outDir 指向 `internal/console/ui/dist/`，
构建会**清空**它，而那里有两个被 git 跟踪的占位文件（`index.html`、
`_placeholder-keeps-embed-underscore-mode.js`），少了它们 `go:embed` 编译不过。若跑了，
跑完 `git restore internal/console/ui/dist/`。

**回归判据：零失败**，只有一个例外（见下）。基线实测（2026-09-29，`feat/four-layer`
工作树）`go test ./internal/...` 全绿。早期计划稿提到的「4 个已知既存失败用例」那份记录
已过期——它们全部通过。

**唯一的已知 flake**：`internal/provider/loader` 的
`TestCallWithRetry_BackoffIsExponential`（`protocol_test.go:75-99`），20ms 量级的墙钟计时
断言，整套 `-race` 并发争用下偶发误报。2026-09-30 在基线提交上隔离重跑 5 次全 PASS。
看到它失败时隔离重跑三次，全绿即判为 flake 继续走并在汇报里注明。**不要因此放宽整体
判据，也不要顺手修它。**

---

## 文件结构

### 为什么这样切

**后端沿用现有的一文件一资源。** `internal/console/` 已经是 `artifact.go` / `template.go` /
`hook.go` / `task.go` 各管一组端点，本计划不重新分层——四组契约变更各自落在对应文件里，
一个任务改一个文件，评审时 diff 自然聚焦。

**占位符解析只在服务端一处。** `GET /templates/:id/stages` 让前端不必自己解析正文。正文
语法归用户、占位符解析归服务端，两边各解析一遍迟早不一致（这与 plan A 把排序收敛在
`TemplateBody.HooksForStage` 是同一条理由）。

**前端按视图切，模板编辑器独立成文件。** `views/templates/` 现在只有 `index.vue` 列表页
（487 行）。多 tab 编辑器加阶段面板加 hook 插入弹窗塞进列表页会让那个文件翻倍，所以
新建 `edit.vue`，并把 tab 内的正文编辑器与阶段面板各抽成组件——正文编辑器将来要加语法
高亮，独立文件才改得动。

| 文件 | 职责 |
|---|---|
| **后端** | |
| 修改 `internal/console/artifact.go:176-185` | 创建入参加 `family`；响应加 `family`、删通道 |
| 修改 `internal/console/template.go` | `family`+`bodies` 入参与响应；`GET /templates` 列表；`state` 转 active 的新校验 |
| 创建 `internal/console/template_derive.go` | `derive` 与 `stages` 两个新端点 |
| 修改 `internal/console/hook.go` | `impls` 入参与响应 |
| 修改 `internal/console/task.go:174-178` | 建任务加 `artifactSetId` 与四道交叉校验 |
| **前端 API 层** | |
| 修改 `web/apps/web-ele/src/api/console/{artifact,template,hook,task}.ts` | 类型与函数随契约变更 |
| **前端视图** | |
| 修改 `web/apps/web-ele/src/views/tasks/new.vue` | 3 步扩 4 步，过滤方向反转 |
| 创建 `web/apps/web-ele/src/views/templates/edit.vue` | 多 tab 编辑器主页面 |
| 创建 `web/apps/web-ele/src/views/templates/components/BodyEditor.vue` | 单份正文编辑器（等宽、行号、占位符高亮） |
| 创建 `web/apps/web-ele/src/views/templates/components/StagePanel.vue` | 阶段与 hook 挂载面板 |
| 修改 `web/apps/web-ele/src/views/hooks/index.vue` | 单 body 改多家族 tab |
| 修改 `web/apps/web-ele/src/router/routes/modules/` | 模板编辑页路由 |
| **导入侧** | |
| 修改 `internal/imports/job.go`、`cmd/lober/importrunner.go` | WinPE 四文件来源解析 |
| 修改 `internal/config/config.go` | `Windows.WinPEDir` 配置项 |

## 任务依赖顺序

```
1     制品集端点（可与 2-3 并行）
2     模板端点（必须先于 4、5、6）
3     hook 端点（必须先于 4——建任务要查 hook 实现）
4     建任务交叉校验（依赖 1、2、3）
5-6   两个新端点 derive / stages（依赖 2）
7     前端 API 层（依赖 1-6 全部）
8-11  前端视图（各依赖 7）
12    Windows WinPE 来源（与 1-11 完全独立，可任意时刻做）
```

---

# 阶段一：端点契约变更

### 任务 1：制品集端点加 `family`

**目标：** 让运维创建 ISO 时选族，并在族与识别出的家族不符时给出可处置的错误。

**文件：**
- 修改：`internal/console/artifact.go:126-140`（`artifactSetItem` 响应形态）、
  `:176-185`（`createArtifactSetRequest`）
- 测试：`internal/console/artifact_test.go`

**接口契约：**

```go
type createArtifactSetRequest struct {
    SourcePath  string `json:"sourcePath"`
    Name        string `json:"name"`
    NetbootPath string `json:"netbootPath"`
    Family      string `json:"family" binding:"required"`
}
```

- `family` 必填，非法值返回 400 并在 `message` 里列出三个合法取值。
- 响应 `artifactSetItem` 加 `family`。**它本来就没有 `channel` 字段**（核实过：
  `artifact.go:126-140` 只有 ID/Name/OSFamily/OSVersion/Arch/Firmware/SourceISOSHA256/
  State/ManifestSHA256/FailReason/Paths），所以这里没有要删的东西——`channel` 在
  **模板**的响应与请求体上（`template.go:69`、`:127`），归任务 2。
- **族与家族不符的拒绝发生在导入侧**（plan A 任务 16 已接），本端点只校验族取值本身
  合法——登记时 ISO 还没解析，家族未知。这一点要写进注释，否则读者会问为什么这里不
  做交叉校验。

- [ ] **红：** 覆盖：合法 family 创建成功且响应含该值；family 缺失返回 400；family 拼错
      返回 400 且 message 含三个合法值。
      运行 `go test ./internal/console/ -run TestCreateArtifactSet -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。

- [ ] **重构 + commit：** 在 `docs/contracts/http-console.md` §3 追加一条变更记录。commit
      （`feat(console)!: 制品集端点新增 family 入参`）。

**风险：** `scripts/e2e*.sh` 七个脚本都用 curl 登记制品集，加必填字段会让它们全部 400。
本任务末尾要 `grep -rn "artifact-sets" scripts/` 找出全部调用点并同步加 `family`，否则
下一个跑 e2e 的人会撞上一片 400 而不知道原因。

---

### 任务 2：模板端点改 `family` + `bodies`，补 `GET /templates`

**目标：** 模板端点承载新形态，并补上那个让建任务向导无法工作的列表端点缺口。

**文件：**
- 修改：`internal/console/template.go:33-45`（路由）、`:56-70`（`templateItem`）、
  `:240`（`handleCreateTemplate`）、`handleUpdateTemplate`、`:530`（`handleTemplateState`）
- 测试：`internal/console/template_test.go`、`template_activate_test.go`

**接口契约：**

```go
type templateBodyPayload struct {
    Text     string         `json:"text"`
    HookRefs []hookRefItem  `json:"hookRefs"`
}

// POST /templates 与 PUT /templates/{id} 共用
type templateRequest struct {
    Name               string                         `json:"name" binding:"required"`
    Family             string                         `json:"family"`
    Bodies             map[string]templateBodyPayload `json:"bodies"`
    DiskPolicy         string                         `json:"diskPolicy"`
    OnFailure          string                         `json:"onFailure"`
    KernelArgs         string                         `json:"kernelArgs"`
    RequiredFormFields []string                       `json:"requiredFormFields"`
}
```

新增路由：`s.handle("GET", "/templates", "", "", s.handleListTemplates)` —— 权限列与审计
列都是「—」（只要已登录，与 `GET /hooks`、`GET /artifact-sets` 一致：读列表不是危险动作）。
支持 `?family=` 与 `?osFamily=` 两个可选过滤参数，后者筛「`Bodies` 含该家族正文」的模板。

**删除入参与响应字段：** `artifactSetId`、`answerTemplate`，以及 `channel` —— 后者在
`templateItem`（`template.go:69`）、请求体（`:127`）、`handleCreateTemplate` 的赋值
（`:268`、`:304`）与 `validateTemplateChannel`（`:229`）四处，一并删（plan A 任务 2 已删
model 字段，本任务收口 console 层）。响应体**不含** `channel` 键要用 JSON 键存在性断言，
不是断言值为空——删字段和字段为零值是两件事。

**保存时校验**（三条，每条一个独立的错误分支）：
1. `Bodies` 的键必须落在 `Family` 的允许集合里（`model.FamilyAllows`）。
2. 每条 `hookRef.Stage` 必须在对应正文的 `model.StageNames(text)` 结果里出现。
3. 每个被引用的 hook 必须有该家族的实现（`hook.ImplFor(家族)`）。

**`POST /templates/:id/state` 转 `active` 时新增校验**：至少一份正文；每份正文的
`{{lober:syslog}}`、`{{lober:firstboot}}`、`{{lober:cleanup}}` 各**恰好一次**。缺失与
重复都拒绝——`cleanup` 缺失是完全静默的凭据泄露，`firstboot` 缺失导向 `init_timeout`
这个**不可重装**失败态（SPEC §3.4）。

- [ ] **红：** 覆盖：带 2 份正文创建成功、响应含两份且 `text` 逐字节相符；`Bodies` 键
      越界（`windows` 族配 `rhel` 正文）返回 409；`hookRef.Stage` 不在正文里返回 409
      且 message 含该阶段名与正文里现有的阶段名列表；hook 缺该家族实现返回 409 且
      message 含 hook 名与家族名；空族空正文可存为 draft；转 active 时缺 `{{lober:cleanup}}`
      被拒且 message 说明后果；三个部件占位符重复出现也被拒；`GET /templates` 返回全部、
      `?osFamily=debian` 只返回有 debian 正文的。
      运行 `go test ./internal/console/ -run "TestTemplate" -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。`GET /templates` 的响应形态复用 `templateItem`，**正文全文
      一起返回**（不做摘要）——列表页要显示「哪些家族有正文」，而判据就是 `Bodies` 的
      键集合；正文本身几 KB，一个列表几十条，不构成问题。

- [ ] **重构 + commit：** 契约文档追加变更记录，并在 `GET /templates` 那条里注明它补的是
      `api/console/template.ts:82-93` 如实记录的现存缺口。commit（`feat(console)!: 模板
      端点改为族+每家族正文，补 GET /templates`）。

**风险：** `handleCreateTemplate` 硬编码创建为 `draft`（`template.go:240` 附近），而
`handleCreateTask` 硬性要求 `active` —— 这条链路靠 `POST /templates/:id/state` 打通，
`scripts/e2e.sh` 头部注释第 24-29 行记录了它曾经断掉并用 `db_template_active` 直接写库
绕过。改 state 端点的校验时**不要**把这条链路再弄断：新增的三个占位符校验只在转
`active` 时生效，转 `draft`/`deprecated` 不校验（否则一个写坏的模板将无法退回 draft 修复）。

---

### 任务 3：hook 端点改 `impls`

**目标：** hook 端点承载「一个功能 + 每家族一份实现」，删掉 stage 与 osFamilies 入参。

**文件：**
- 修改：`internal/console/hook.go`（`handleCreateHook`、`handleUpdateHook`、
  `handleValidateHook`、响应形态）
- 测试：`internal/console/hook_test.go`、`hook_publish_test.go`

**接口契约：**

```go
type hookImplPayload struct {
    Interpreter string `json:"interpreter" binding:"required"`
    Body        string `json:"body" binding:"required"`
}

type hookRequest struct {
    Name           string                     `json:"name" binding:"required"`
    Description    string                     `json:"description"`
    Impls          map[string]hookImplPayload `json:"impls" binding:"required"`
    ParamsSchema   string                     `json:"paramsSchema"`
    ParamsDefaults map[string]string          `json:"paramsDefaults"`
    Required       bool                       `json:"required"`
    TimeoutSec     int                        `json:"timeoutSec"`
}
```

**删除入参：** `stage`、`osFamilies`、`interpreter`、`body`。

**校验：**
- `impls` 非空（空 map 的 hook 的 `HookDigest` 是空串，无法被模板 pin）。
- 键必须是 6 个合法家族之一。
- 每份 `interpreter` 在白名单内（`bash`/`powershell`/`cmd`/`ash`）。
- **不校验解释器与家族的搭配合理性**——`windows` 下写 bash 是可能的（装了 WSL 或
  busybox），拦掉会挡住合法用法。这条要写进注释说明是刻意放宽。

响应每份实现带 `bodySha256`（单份实现的裸 sha256），hook 顶层带 `digest`（全部实现的
规范化序列化 sha256）。两个字段都要，用途不同（plan A 任务 5 的契约）。

- [ ] **红：** 覆盖：带 3 个家族实现创建成功、响应三份都在且各自 `bodySha256` 与
      `echo -n <body> | sha256sum` 的真实输出相符；`impls` 为空返回 400；家族键拼错
      返回 400；`interpreter` 非白名单返回 400；`windows` 家族配 `bash` 解释器**创建
      成功**（刻意放宽的那条）；改一个家族的正文后顶层 `digest` 变化。
      运行 `go test ./internal/console/ -run "TestHook" -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。

- [ ] **重构 + commit：** 契约文档追加变更记录。commit（`feat(console)!: hook 端点改为
      每家族一份实现`）。

**风险：** hook 有双人复核流程（`POST /hooks/:id/approve` 与 `/reject`，SPEC §9.4）。
改 hook 形态时**不要动审批流**——`approve` 的语义是「批准这个 hook 的当前版本」，而
当前版本现在包含多份实现，`HookDigest` 已经把全部实现纳入同一个 digest，所以审批粒度
天然还是「一次批一整套」，不需要改。要在注释里写清这一点，否则会有人想加「按家族分别
审批」。

---

### 任务 4：建任务加 `artifactSetId` 与四道交叉校验

**目标：** 任务同时选 ISO 与模板，并在最上游的那个问题上给出错误。

**文件：**
- 修改：`internal/console/task.go:174-178`（`createTaskRequest`）、`:189` 起
  （`handleCreateTask` 的六步顺序变七步）
- 测试：`internal/console/task_test.go`

**接口契约：**

```go
type createTaskRequest struct {
    HostID        string            `json:"hostId" binding:"required"`
    TemplateID    string            `json:"templateId" binding:"required"`
    ArtifactSetID string            `json:"artifactSetId" binding:"required"`
    FormData      map[string]string `json:"formData"`
}
```

**校验顺序**（前一条不过就不做后一条，这样错误文案总是指向最上游的问题）：

| # | 校验 | 失败响应 |
|---|---|---|
| 1 | ISO 存在且 `state=ready` | 409，message 含制品集当前状态与「等导入完成或换一个 ready 的制品集」 |
| 2 | 模板存在且 `state=active` | 409，沿用现有文案 |
| 3 | 模板的 `Bodies` 含该 ISO 家族的正文 | 409，message 含模板名 + 缺哪个家族 + 「补齐该正文或改用其它模板」 |
| 4 | 该正文引用的每个 hook 都有该家族实现 | 409，message 含 hook 名 + 家族名 |
| 5 | 固件与架构兼容 | 沿用现有 `FirmwareCompatible` 与 arch 校验，不变 |

**删除：** 通道一致性校验（plan A 任务 2 已删 model 字段，本任务收口 handler）。

409 下用不同的 `code` 区分（沿用 `apierr` 里 1005–1008 那套分支语义；若现有码不够，
在 `internal/apierr/apierr.go` 新增并在该文件的码表注释里登记）。

`Task.ArtifactSetID` 从请求体取，**不再从模板取**——这是需求 3 解耦的落点。`Task.HookDigests`
pin 的是该家族正文引用的那些 hook 的 `HookDigest`。

- [ ] **红：** 五道校验各一个失败用例 + 一个全部通过的正常路径。正常路径要断言落库的
      `Task.ArtifactSetID` 等于请求里的值（而不是模板里的——那个字段已经不存在了）、
      `HookDigests` 与该家族正文的引用集合相符。第 3 道的用例用「`linux` 族模板只填了
      rhel 正文，ISO 是 debian」构造。
      运行 `go test ./internal/console/ -run TestCreateTask -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。

- [ ] **重构 + commit：** 契约文档追加变更记录。`grep -rn "tasks" scripts/` 找出 e2e
      脚本里的建任务调用点并同步加 `artifactSetId`。commit（`feat(console)!: 建任务同时
      选 ISO 与模板，四道交叉校验`）。

**风险：** `handleCreateTask` 现有注释写明「六步顺序不能变」，其中第 4 步「调参数决策
接口——失败即不建任务」是阻塞式 provider（SPEC §7.2：在写库之前调用，失败即整体失败、
库里不出现任务）。新加的四道校验必须插在**第 4 步之前**——先校验完再调 provider，否则
一个注定要失败的任务会先去调一遍外部 IPAM/DNS，可能已经分配了 IP 却不建任务，留下
脏数据。

---

# 阶段二：两个新端点

### 任务 5：`POST /templates/{id}/derive` 派生

**目标：** 让运维以一个现成模板为起点建新模板，之后两者完全独立。

**文件：**
- 创建：`internal/console/template_derive.go`
- 修改：`internal/console/template.go` 的 `routesTemplate` —— 注册新路由
- 测试：`internal/console/template_derive_test.go`

**接口契约：**

```go
type deriveTemplateRequest struct {
    Name string `json:"name" binding:"required"`
}
```

路由：`s.handleFuncRole("POST", "/templates/:id/derive", roleAdmin, "template.derive", s.handleDeriveTemplate)`
—— 权限与 `POST /templates` 一致（派生等于创建一个新模板，危险程度相同）。

**语义**：复制源模板的 `Family`、`Bodies`（含每份的 `HookRefs`）、`DiskPolicy`、
`OnFailure`、`KernelArgs`、`RequiredFormFields`；新模板 `State=draft`、`Version=1`、
`DerivedFrom={源ID, 源Version}`。响应是新模板的 `templateItem`。

**不复制：** `ID`、`Name`（用入参）、`State`、`Version`。

派生后**无任何联动**：改源模板不影响派生品，反之亦然（设计 §2.5）。

- [ ] **红：** 覆盖：派生成功且新模板的正文与源逐字节相同、`DerivedFrom` 指向源的 ID
      与版本、`State=draft`、`Version=1`；**改源模板的正文后再读派生品，派生品不变**
      （这是「无联动」的核心断言）；源模板不存在返回 404；`name` 与已有模板重名的处置
      与 `POST /templates` 保持一致（查现有实现是拒绝还是允许，跟着走——不要在这里
      引入一条与创建端点不同的重名规则）；派生一个 `draft` 源模板也允许（不要求源必须
      是 active——运维可能想从一个半成品继续）。
      运行 `go test ./internal/console/ -run TestDeriveTemplate -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。复制 `Bodies` 时**深拷贝**——浅拷贝会让两个模板共享同一个
      `HookRefs` 底层数组，改一个动另一个，「无联动」这条当场破掉。

- [ ] **重构 + commit：** 契约文档追加变更记录。commit（`feat(console): 模板派生端点`）。

**风险：** 深拷贝的坑在 `map[OSFamily]TemplateBody` 这一层——`TemplateBody` 是值类型，
map 赋值会拷贝结构体，但结构体里的 `HookRefs` 切片仍共享底层数组。必须逐层拷。测试要
用「改派生品的一条 hookRef 的 Order，再读源模板」验证，不能只验证正文文本。

---

### 任务 6：`GET /templates/{id}/stages` 阶段列表

**目标：** 给前端提供阶段列表，让占位符解析只在服务端一处。

**文件：**
- 修改：`internal/console/template_derive.go`（与 derive 同文件——两者都是模板的附属
  操作，且都不属于原有四端点，放一起让这组新增在一个文件里看全）
- 测试：`internal/console/template_derive_test.go`

**接口契约：**

```
GET /console/v1/templates/{id}/stages?osFamily=<家族>

data: { "stages": ["硬件检查", "装包", "配业务网"] }
```

路由：`s.handle("GET", "/templates/:id/stages", "", "", s.handleTemplateStages)` —— 权限
与审计列都是「—」（读操作）。

- `osFamily` 必填；缺失返回 400 并列出该模板有正文的家族（这比只说「缺参数」有用得多）。
- 该模板没有这个家族的正文时返回 404，message 含有正文的家族列表。
- 返回顺序 = 正文里 `{{stage:}}` 的**出现顺序**，重复名字去重后保持首次出现位置
  （委托 plan A 任务 10 的 `model.StageNames`，**不要在 console 里重新实现一遍扫描**）。
- 空正文返回空数组，不是 404——空模板零个阶段是合法状态（设计 §2.2）。

- [ ] **红：** 覆盖：含 3 个阶段的正文返回 3 个且顺序正确；重复阶段名去重；空正文返回
      空数组；`osFamily` 缺失返回 400 且 message 含可用家族列表；该家族无正文返回 404；
      正文里含 cloud-init Jinja `{{ ds.xxx }}` 时**不被当成阶段**（这条是 plan A 任务 10
      的行为，在此端点上再验一次——它是前端唯一的阶段数据源，错了会让 UI 显示一个不
      存在的阶段）。
      运行 `go test ./internal/console/ -run TestTemplateStages -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。

- [ ] **重构 + commit：** 契约文档追加变更记录，注明这个端点的存在理由是「占位符解析
      只在服务端一处」。commit（`feat(console): 模板阶段列表端点`）。

**风险：** 这个端点看起来可以用「前端自己 split 正文」替代，省一次请求。**不要那样做**——
阶段名的字符集规则（允许中文、禁止空格、未闭合按未识别处理）在 plan A 任务 10 有 9 类
测试兜着，前端重写一遍必然漂移，而漂移的表现是「UI 上有这个阶段、渲染时找不到」。

---

# 阶段三：前端

### 任务 7：前端 API 层类型与函数

**目标：** 让 TypeScript 类型与新契约一致，为四个视图任务提供已对齐的调用面。

**文件：**
- 修改：`web/apps/web-ele/src/api/console/artifact.ts` —— `ArtifactSetItem` 加 `family`、
  删 `channel`；创建入参加 `family`
- 修改：`web/apps/web-ele/src/api/console/template.ts:48-133` —— `TemplateItem` 与
  `TemplateRequest` 改 `family`+`bodies`；`getTemplates` 加过滤参数；新增 `deriveTemplate`
  与 `getTemplateStages`
- 修改：`web/apps/web-ele/src/api/console/hook.ts` —— `impls` 映射
- 修改：`web/apps/web-ele/src/api/console/task.ts` —— 建任务入参加 `artifactSetId`

**接口契约：**

```ts
export interface TemplateBody { text: string; hookRefs: HookRefItem[] }
export interface TemplateItem {
  id: string; name: string;
  family: string;
  bodies: Record<string, TemplateBody>;
  diskPolicy: string; onFailure: string; kernelArgs: string;
  requiredFormFields: string[];
  derivedFrom?: { templateId: string; version: number };
  version: number; state: string;
}
export interface HookImpl { interpreter: string; body: string; bodySha256: string }

export function getTemplates(params?: { family?: string; osFamily?: string }): Promise<TemplateItem[]>
export function deriveTemplate(id: string, name: string): Promise<TemplateItem>
export function getTemplateStages(id: string, osFamily: string): Promise<string[]>
```

`template.ts:82-93` 那段「GET /templates 是契约缺口」的注释要改写成「该端点已由
plan B 任务 2 补齐」——留着会让下一个读的人以为缺口还在。

- [ ] **红：** 前端无单测框架，红灯形式是类型检查失败：先改类型定义不改调用点，跑
      `cd web/apps/web-ele && node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck`，
      预期报出全部仍在用旧字段（`artifactSetId`、`answerTemplate`、`stage`、`osFamilies`、
      `channel`）的位置。那份报错清单就是任务 8-11 的工作量，抄下来。

- [ ] **绿：** 补齐 API 层，让 `api/console/*.ts` 自身的类型检查通过（视图层仍会报错，
      那是后续任务）。

- [ ] **重构 + commit：** `cd web && node node_modules/eslint/bin/eslint.js --fix src/api/console/`。
      commit（`refactor(web): API 层类型对齐四层模型契约`）。

**风险：** 视图层的类型错误会在本任务后**大面积存在**，`vue-tsc` 不会绿。这是刻意的
中间态——不要为了让它绿而在本任务里顺手改四个视图。全绿在任务 11 末尾。

---

### 任务 8：建任务向导 3 步扩 4 步

**目标：** 向导插入「选 ISO」一步，并把模板过滤的方向反转过来。

**文件：**
- 修改：`web/apps/web-ele/src/views/tasks/new.vue:1-30`（顶部注释）、`:327-331`（步骤条）、
  `:238-239`（`backToStep`）、模板加载与过滤逻辑

**四步：**

```
① 选主机        SN 必须从已发现主机清单选取，不接受自由文本（现有约束，不变）
② 选 ISO        按主机的 arch 与 firmware 过滤；只列 state=ready；显示族/家族/版本
③ 选模板        调 getTemplates({ osFamily: 选定ISO的osFamily })
④ 填参数并确认   现有表单 + 目标主机确认框（SN、机架位、上次被发现时间、架构/固件）
```

**过滤方向反转**：现状是「模板 → `artifactSetId` → 制品集的 arch/firmware」联查
（`new.vue:18-22` 的注释记录了这条路径）。模板不再持 `artifactSetId`，这条路径消失，
改成「ISO 的家族 → 筛模板」。**这不是等价改写**：旧路径一个模板只对应一个制品集，新
路径一个模板服务该家族的全部 ISO 版本——这正是需求 3 要的解耦。顶部注释要改写，不要
留着描述已消失路径的文字。

**回退处置**：回退到 ② 改 ISO 后，若 ③ 已选的模板不在新 ISO 家族的候选列表里，**清空
该选择并提示**，不要静默保留一个不兼容的选择。

- [ ] **红：** 前端无单测，红灯形式是 `vue-tsc` 对本文件的报错清零之前先确认它确实在报
      旧字段。改完后的验证是手动的：起 `go run ./cmd/lober -config <配置>`，浏览器走一遍
      四步，逐条确认——步骤条显示 4 步；② 只列 ready 的 ISO 且显示族与家族；③ 的模板
      下拉只含有该家族正文的；②→③→回退到②→改 ISO 后 ③ 的选择被清空并有提示；提交的
      请求体含 `artifactSetId`（浏览器 DevTools 的 Network 面板看）。

- [ ] **绿：** 实现至上述五条手动检查全过，且
      `node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck` 对本文件
      无报错。

- [ ] **重构 + commit：** eslint --fix。commit（`feat(web): 建任务向导扩为四步，先选 ISO`）。

**风险：** `new.vue` 的三条硬要求（注释第 6-16 行）在本任务里**一条都不能破**：SN 必须
从清单选、提交前弹二次确认且显示三项、请求体传 `hostId` 不传 SN 字符串。加一步很容易
在重排步骤时把二次确认弄丢——它是删掉 `armed` 态之后**唯一的人工确认点**（SPEC §3.2）。

---

### 任务 9：正文编辑器与阶段面板两个组件

**目标：** 把「一份正文 + 它的阶段与 hook 挂载」做成可复用组件，供模板编辑页的每个 tab 用。

**文件：**
- 创建：`web/apps/web-ele/src/views/templates/components/BodyEditor.vue`
- 创建：`web/apps/web-ele/src/views/templates/components/StagePanel.vue`

**组件契约：**

```ts
// BodyEditor
defineProps<{ modelValue: string; osFamily: string }>()
defineEmits<{ 'update:modelValue': [string] }>()

// StagePanel
defineProps<{ stages: string[]; hookRefs: HookRefItem[]; osFamily: string }>()
defineEmits<{ 'update:hookRefs': [HookRefItem[]] }>()
```

**BodyEditor**：等宽字体、行号、占位符高亮（`{{stage:}}` / `{{lober:}}` / `{{fact:}}`
三色区分，未识别的 `{{lober:xxx}}` 标红）。**不做语法检查**——正文语法归用户
（设计 §8）。

**StagePanel**：显示 `stages`（来自 `getTemplateStages`，**只读，不可手工增删**——正文里
写一个 `{{stage:}}` 就多一个阶段，这样「两处对不上」这个故障模式从根上不存在），每个
阶段下列出挂在它上面的 hook 引用（按 order），提供「插入 hook」弹窗。

**插入 hook 弹窗**：hook 列表按 `osFamily` 过滤，**没有该家族实现的 hook 灰显并给出
原因**，而不是从列表里消失——后者会让运维以为 hook 不存在。

- [ ] **红：** 手动验证清单（浏览器）：正文里打一个 `{{stage:测试}}` 后阶段面板出现
      「测试」；删掉后消失；三类占位符各自高亮成不同颜色；`{{lober:xxx}}` 标红；
      cloud-init Jinja `{{ ds.xxx }}` **不高亮**（它不是我们的占位符）；插入 hook 弹窗里
      无该家族实现的 hook 灰显且有原因文字；阶段列表无「添加阶段」按钮。

- [ ] **绿：** 实现至上述清单全过。高亮先用简单实现（`<textarea>` 叠一层带着色 span 的
      `<pre>`，或直接用 Element Plus 的 `ElInput type="textarea"` 加等宽样式先不做高亮），
      **不要为高亮引入重型编辑器依赖**（CodeMirror/Monaco 会让打包体积翻倍，而 dist 是
      要 `go:embed` 进单个二进制的）。

- [ ] **重构 + commit：** eslint --fix。commit（`feat(web): 正文编辑器与阶段面板组件`）。

**风险：** 阶段列表的数据源必须是 `getTemplateStages`（服务端解析），**不要在前端 split
正文**。前端重写扫描规则必然与 plan A 任务 10 的 9 类测试漂移，漂移的表现是「UI 上有这
个阶段、渲染时找不到」。若为了编辑时的即时反馈想要本地解析，做法是「本地解析只用于
高亮，阶段列表始终来自服务端」，两者不共用一套逻辑。

---

### 任务 10：模板编辑页（多 tab）

**目标：** 一个页面里编辑一个模板的全部家族正文，并能派生。

**文件：**
- 创建：`web/apps/web-ele/src/views/templates/edit.vue`
- 修改：`web/apps/web-ele/src/router/routes/modules/` 下模板相关的路由文件 —— 加编辑页路由
- 修改：`web/apps/web-ele/src/views/templates/index.vue` —— 列表页加「编辑」「派生」入口，
  列表列加 `family` 与「有正文的家族」

**布局**（设计 §3.2）：顶部选族与模板名 + 派生/预览/激活三个按钮；中部按家族分 tab
（族决定 tab 集合：`linux` 4 个、`windows`/`esxi` 各 1 个），每个 tab 内左 `BodyEditor`
右 `StagePanel`。未填正文的 tab 标「(未填)」。

**「预览」按钮要显眼**（不是藏在下拉菜单里）：服务端不校验正文语法，preview 是模板作者
在装机之前能拿到的唯一反馈（设计 §3.3）。调 `POST /templates/{id}/preview` 时带上当前
tab 的家族与一个可选的真实 ISO，产出展示渲染后的完整应答文件文本。

**「派生」按钮**：弹窗填新名称 → 调 `deriveTemplate` → 跳转到新模板的编辑页，顶部显示
「派生自 X（v2）」。需求 11 的「覆盖 a hook 用 b hook」在这里就是：派生出新模板，在某个
tab 的阶段里删掉 a 的引用、插入 b。**没有专门的「覆盖」操作。**

- [ ] **红：** 手动验证清单：选 `linux` 族显示 4 个 tab、选 `windows` 显示 1 个；切换族时
      已填的越界正文要提示而不是静默丢弃；未填的 tab 标「(未填)」；预览按钮在主操作区
      可见；派生后跳到新页且顶部显示来源；激活一个缺 `{{lober:cleanup}}` 的模板时显示
      服务端返回的那条含后果说明的错误文案（不是泛泛的「激活失败」）。

- [ ] **绿：** 实现至清单全过，`vue-tsc` 对本文件无报错。

- [ ] **重构 + commit：** eslint --fix。commit（`feat(web): 模板编辑页支持多家族正文与派生`）。

**风险：** 切换族会让 tab 集合变化，已填的正文可能越界（`linux` 族填了 rhel 正文后切成
`windows` 族）。**不要静默丢弃**——那是用户几百行的手写正文。提示「切换族将移除 N 份
不兼容的正文」并要求确认；服务端的 `FamilyAllows` 校验（任务 2）是第二道防线。

---

### 任务 11：hook 编辑器多家族 tab

**目标：** 一个 hook 页面里维护它的全部家族实现，并让前端类型检查回到全绿。

**文件：**
- 修改：`web/apps/web-ele/src/views/hooks/index.vue`（817 行）

**改动：** 单 `body` 编辑区改成按家族分 tab，每个 tab 内一个解释器下拉 + 一个正文编辑器。
tab 可增可删——增一个 tab = 为该家族加一份实现，删一个 tab = 该家族不再支持。

**删掉两个控件**：`stage`（移到模板的 hook 插入弹窗，任务 9）与 `osFamilies`（由 tab 集合
隐含）。

hook 的 6 个家族 tab 是**全集**（hook 不声明族，任何家族都可以有实现），与模板编辑页按
族决定 tab 集合不同。这个差异要在页面上体现得自然：hook 页显示「+ 添加家族实现」下拉，
列出还没有实现的家族。

- [ ] **红：** 手动验证清单：加一份 suse 实现后保存、重新打开仍在；删掉一份实现后保存、
      重新打开确实少了；删到零份时保存被拒（服务端 `impls` 非空校验，任务 3）且显示服务端
      文案；`windows` tab 里选 `bash` 解释器**能保存成功**（刻意放宽的那条，任务 3）；
      每份实现显示自己的 `bodySha256`；页面上没有 stage 与 osFamilies 控件。

- [ ] **绿：** 实现至清单全过。收口检查：
      `cd web/apps/web-ele && node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck`
      **整个项目零报错**（任务 7 起的中间态在此结束）。

- [ ] **重构 + commit：** `cd web && node node_modules/eslint/bin/eslint.js --fix src/`。
      commit（`feat(web): hook 编辑器改为多家族实现 tab`）。

**风险：** 这个文件 817 行且含审批流 UI（approve/reject）。改 body 编辑区时**不要动审批
流**——审批粒度是「一次批一整套实现」（任务 3 的风险条已论证），不要顺手加「按家族分别
审批」的控件。若文件改完超过 1000 行，把审批流那块抽成组件，但那是独立的重构，单独 commit。

---

# 阶段四：Windows 导入

### 任务 12：WinPE 四文件来源解析与装配点接线

**目标：** 让 Windows ISO 能通过 API 导入——这是六家族里唯一完全不通的一条。

**文件：**
- 修改：`internal/console/artifact.go:63-80`（`ImportSource` 加 `WinPE`）、
  `createArtifactSetRequest`（加可选的 winpe 四路径）
- 修改：`internal/imports/job.go:290-296` 附近 —— 消费 resolver
- 修改：`internal/config/config.go` —— 加 `Windows.WinPEDir`
- 修改：`cmd/lober/wire.go` —— 实现 `WindowsSourcesResolver` 并调
  `job.ConfigureWindows(resolver, archDetector)`
- 修改：`deploy/lober.yaml.example` —— 示例配置
- 测试：`internal/imports/job_test.go`、`cmd/lober/` 下 resolver 的测试

**接口契约：**

```go
type WinPESources struct {
    WimbootPath string
    BCDPath     string
    BootSDIPath string
    BootWIMPath string
}

type ImportSource struct {
    ISOPath     string
    NetbootPath string
    WinPE       *WinPESources // 可空
}
```

**解析顺序**（设计 §4.2）：
1. 请求带了 `WinPE` 且四个路径全非空 → 用它
2. 任一为空 → 回落配置约定
   `cfg.Windows.WinPEDir/<osVersion>-<firmware>/{wimboot,BCD,boot.sdi,boot.wim}`
3. 两处都拿不到 → 导入失败，错误信息**把两条路都写清**（请求里可以传什么、配置目录
   应该放什么），不要只说「缺少 WinPE 文件」

`cmd/lober/wire.go` 必须调 `job.ConfigureWindows(resolver, archDetector)` —— 这一步现在
从未被调用，缺了它 Windows 导入必然失败在 `job.go:290-296`。

- [ ] **红：** 覆盖：四路径全给时用请求值；只给三个时回落配置目录；配置目录也没有时
      导入置 `failed` 且 `FailReason` 同时含两条路径的说明；配置项为空字符串时的错误
      文案要说「未配置 windows.winpe_dir」而不是「文件不存在」（两者的处置完全不同：
      一个是改配置，一个是放文件）。
      运行 `go test ./internal/imports/ ./cmd/lober/ -run "Windows|WinPE" -count=1 -v`，
      预期 FAIL。

- [ ] **绿：** 实现至通过。resolver 的接口定义放 `internal/imports` 包内（它是被调用方，
      定义自己需要的能力），具体实现在 `cmd/lober`。

- [ ] **重构 + commit：** `go test ./internal/archtest/ -count=1` 绿。`make check` 全绿。
      commit（`feat(imports): Windows WinPE 四文件来源解析与装配点接线`）。

**风险：** 四个文件**需要人工准备**（不在 Windows ISO 里），所以这个任务的测试只能用
假文件验证解析逻辑，**真实 Windows 导入无法在本任务验收**。这一点要写进 commit 信息与
`docs/real-machine/` 的记录里，不要让「测试全绿」被读成「Windows 能装机了」。真机
验收排在整体验收的最后一批。

---

## 整体验收

### 构建与测试

- [ ] `export LOBER_TEST_PG_DSN='postgres://lober_test:lober_test_pw@localhost:5432/lober_test?sslmode=disable'`
- [ ] `make check` 四步全绿，**零失败**。
- [ ] `go test ./internal/archtest/ -count=1` 绿 —— `internal/console` 未 import 其它
      实现模块（含测试 import）。
- [ ] `export PATH="/c/Program Files/nodejs:$PATH" && cd web/apps/web-ele && node ../../node_modules/vue-tsc/bin/vue-tsc.js --noEmit --skipLibCheck`
      零报错。
- [ ] `cd web && node node_modules/eslint/bin/eslint.js src/` 零错误。

### 端点逐条

- [ ] `POST /artifact-sets` 带 `family` 成功、不带返回 400。
- [ ] `GET /templates` 存在且 `?osFamily=debian` 正确过滤 —— 这是补上的现存缺口，
      `api/console/template.ts` 里那段「契约缺口」注释已改写。
- [ ] `POST /templates` 带 `bodies` 成功；越界键、阶段名不存在、hook 缺实现三类各返回 409
      且文案含可行下一步。
- [ ] `POST /templates/:id/state` 转 active 时，缺任一 `{{lober:}}` 部件被拒；转 draft
      不校验。
- [ ] `POST /templates/:id/derive` 派生后改源模板，派生品不变。
- [ ] `GET /templates/:id/stages?osFamily=` 返回顺序 = 正文出现顺序；Jinja 不被当阶段。
- [ ] `POST /hooks` 带 `impls` 成功；空 `impls` 返回 400。
- [ ] `POST /tasks` 带 `artifactSetId` 成功；五道校验各返回对应的 409/400 与不同 `code`。
- [ ] `docs/contracts/http-console.md` 的变更记录条数 = 2（原有）+ 本计划新增的每个契约
      变更任务各一条。

### 界面走通（浏览器，不是 API）

- [ ] 从零走通一次 debian 装机的**前半程**：创建 ISO（选 `linux` 族）→ 派生「Linux 基线」
      模板 → 在 debian tab 里改一个 hook 挂载 → 预览看到展开后的完整 preseed → 激活 →
      向导四步建任务成功。
- [ ] 向导第 ② 步改 ISO 后第 ③ 步的不兼容模板选择被清空并有提示。
- [ ] 模板编辑页切换族时，越界正文有确认提示而非静默丢弃。
- [ ] hook 页给一个 hook 加一份 suse 实现，保存后重新打开仍在。

### 真机（最终判据）

- [ ] QEMU 起一台目标机，用上面那个派生出来的模板装一次 Debian，状态机走到 `done`，
      且能用注入的 SSH 公钥登入。**API 级断言（preview / dryrun / precheck）是辅助手段，
      不是验收判据。**

### 不该变的东西没变

- [ ] `git diff docs/contracts/http-machine.md` 为空 —— 机器面契约一个字节不变。
- [ ] 建任务的三条硬要求仍成立：SN 从清单选（页面上无自由文本 SN 输入框）、提交前弹
      二次确认且显示 SN/机架位/上次被发现时间、请求体传 `hostId` 不传 SN。
- [ ] 建任务的阻塞式 provider 仍在新增的四道校验**之后**调用：读一遍
      `handleCreateTask` 确认顺序，或构造一个「模板缺该家族正文 + provider 会记录调用」
      的用例断言 provider 未被调用。
- [ ] hook 双人复核流程未被削弱：`go test ./internal/console/ -run "TestHookApprove|TestHookReject" -count=1` 绿。
- [ ] `internal/console/ui/dist/` 下两个占位文件仍在（`index.html`、
      `_placeholder-keeps-embed-underscore-mode.js`）—— 若跑过 `vite build`，确认已
      `git restore internal/console/ui/dist/`。
- [ ] `go run ./cmd/lober -config deploy/lober.yaml.example -check` 通过启动自检。

### Windows 的诚实结论

- [ ] `docs/real-machine/` 下记录一条：WinPE 四文件来源的**解析逻辑**已测试，但**真实
      Windows 导入与装机未验证**（四个文件需人工准备）。不要让「测试全绿」被读成
      「Windows 能装机了」。

