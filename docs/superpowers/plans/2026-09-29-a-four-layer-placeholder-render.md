# A 四层模型与占位符渲染 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 把三级抽象改成四层解耦（任务 →（ISO + 模板）→ hook），并让应答文件的全部业务内容从渲染器硬编码搬进用户自写的模板正文，系统只做占位符展开。

**架构：** ISO 同时持族（3 值，用户选）与家族（6 值，导入识别），两者交叉校验。模板声明族、内部每家族一份正文，正文是用户自写全文。阶段 = 正文里 `{{stage:}}` 占位符的出现顺序。hook 每家族一份实现，自己不带阶段。渲染器从「六个家族生成器」退化为「一个与家族无关的展开引擎 + 一张每家族的系统部件文本表」。镜像直写通道整条删除。

**技术栈：** Go 1.27、GORM/PostgreSQL、`gopkg.in/yaml.v3`（seed 定义）、纯字符串替换（**不用** text/template，理由见设计 §3.2）

**设计依据：** `docs/superpowers/specs/2026-09-29-a-four-layer-placeholder-render-design.md`（commit `051179c`）

---

## 架构约束（每个任务都必须遵守）

1. **`internal/core/model` 不得 import 任何项目内包**（`internal/archtest/deps_test.go:145` 守卫）。
2. **七个模块（store / statemachine / render / gateway / console / provider / imports）两两之间不得互相 import**（`deps_test.go:273` 守卫，**含测试 import**）。跨模块只在 `cmd/lober` 装配。
3. **渲染器是纯函数**：不查库、不碰时钟、不做 IO。`Body` 与 `Impls` 由调用方注入 `RenderContext`。
4. **实现代码里不得出现 `time.Now()`**，时间全部来自注入的 `contract.Clock`。
5. **错误信息用中文且给可行下一步**，沿用 `internal/console/task.go:63-76` 的风格。
6. **fail loudly，不静默降级。**
7. **注释解释「为什么」并引用 SPEC 小节**，包括为什么没选另一条路。
8. **提交信息用中文**，格式 `类型(范围): 简述`。

## 契约变更流程

`internal/core/contract` 里已发布的签名改动是独立的「契约变更」任务：改接口 → 改 conformance 套件 → 改内存实现 → 改 postgres 实现。**顺序不可颠倒**——`internal/core/contract/conformance` 是库代码（不是 `_test.go`），两个实现包都 import 它，先加断言才能让「套件变绿即集成完成」这条机械规则成立。

## 测试命令约定

```bash
export LOBER_TEST_PG_DSN='postgres://lober_test:lober_test_pw@localhost:5432/lober_test?sslmode=disable'
make check          # 收口：fmt → vet → test → race，四步全绿才算做完
go build ./...      # 每个任务末尾必须绿
```

**基线是零失败。** 2026-09-29 在 `feat/four-layer` 工作树实测（`go1.27.1 windows/amd64`、
PostgreSQL 17.10）：`go test ./internal/...` 全部包 `ok`，`go test ./internal/render/
./cmd/... -count=1` 强制重跑亦全绿。

**回归判据：任何失败都是回归。** 早期计划稿曾把 4 个 `internal/render` 的 inspect/exec
用例记成「已知既存失败」，那份记录**已过期**——这 4 个现在全部通过：

```
TestInspect_DiskCollectionFailureStillProducesNICOutput
TestInspect_DiskFunctionOutputExactKeyFormat
TestInspect_NICFunctionOutputExactKeyFormat
TestExec_RequiredHookFailureAbortsSectionEvenWithLaterHooks
```

**不要容忍它们失败。** 若在实现过程中看到其中任何一个变红，那是本次改动引入的，不是环境
问题。（其中 `TestExec_...` 会随任务 15 删除 `kickstart.go` 一并消失，那是文件被删、用例
不再存在，与「用例失败」是两件事。）

### 唯一的已知 flake（2026-09-30 查实）

`internal/provider/loader` 的 `TestCallWithRetry_BackoffIsExponential`
（`protocol_test.go:75-99`）在**整套 `-race` 并发争用下**偶发失败，症状形如
「第二段退避间隔 = 43.068ms, 第一段 = 29.1499ms —— want 第二段显著大于第一段」。

**判据**：它是墙钟计时断言，`RetryBackoff` 只有 20ms，而测试注释自己写明「精确倍数类断言
在 CI 环境下容易因调度抖动误报」，所以只断言 1.5 倍而非 2 倍 —— 20ms 量级在满载 `-race`
下抖动超过 50% 是这类断言的固有脆弱性。已在基线提交 `ccd7f90` 上隔离重跑 `-race` 5 次全
PASS，确认与四层解耦无关。

**处置**：看到它失败时，**隔离重跑一次**（`go test ./internal/provider/loader/ -run
TestCallWithRetry_BackoffIsExponential -race -count=3`）。三次全绿即可判为 flake 继续往下
走，并在任务汇报里注明。**不要因此放宽整体的零失败判据** —— 除这一个用例之外，任何失败
都是回归。也不要顺手去修它（修一个计时断言需要换掉计时方式，那是独立的任务，不在本计划
范围内）。

---

## 文件结构

### 为什么这样切

**model 层按「概念」切，不按「改动批次」切。** `Family` 单独一个文件而不是塞进
`inventory.go`：它是本次新增的第一个概念，且被制品集与模板两边消费，独立成文件让
「族与家族的关系」这套规则（映射表、允许集合、校验）有一个明确的归属地。

**渲染层从六个家族文件收敛成三个职责文件。** 现状是六个家族各一个文件、各自从头生成
完整应答文件；改造后家族差异只剩「系统部件的文本形态」一张表，于是切分从「按家族」
改成「按职责」：展开引擎（家族无关）、系统部件表（家族差异的唯一去处）、入口分派。

**seed 是新包且只被装配点 import。** 骨架正文要落库，而七个实现模块两两不得互相
import（`deps_test.go:273`），所以它不能放进 `internal/console` 或 `internal/store`。

| 文件 | 职责 |
|---|---|
| **model 层** | |
| 创建 `internal/core/model/family.go` | `Family` 类型、家族→族映射、允许集合与校验 |
| 修改 `internal/core/model/inventory.go` | `ArtifactSet` 加 `Family`；删通道与镜像全部类型 |
| 修改 `internal/core/model/authoring.go` | `Template`/`TemplateBody`/`Hook`/`HookImpl`/`HookRef`/`DerivedRef` 重构；删 `HookStage` |
| 修改 `internal/core/model/render.go` | `RenderContext` 加 `Body`/`Impls` |
| **契约与存储层** | |
| 修改 `internal/core/contract/store.go` | `ArtifactSetStore`/`TemplateStore`/`HookStore` 签名调整 |
| 修改 `internal/core/contract/conformance/store.go` | 新行为的可执行断言（**先于两个实现**） |
| 修改 `internal/core/fake/store.go` | 内存实现补齐 |
| 修改 `internal/store/postgres/{artifact,template,hook}/row.go` | GORM 行类型与列 |
| 修改 `internal/store/postgres/schemacheck.go` | 索引结构断言随列改动同步 |
| **渲染层** | |
| 创建 `internal/render/expand.go` | 占位符展开引擎，**与家族无关** |
| 创建 `internal/render/systemparts.go` | 每家族的系统部件文本表，家族差异的唯一去处 |
| 修改 `internal/render/renderer.go` | 入口：取正文 → 调展开引擎；删通道分叉 |
| 修改 `internal/render/skeleton.go` | 从三家族扩到六家族，产出整理成 `systemParts` 的输入 |
| 删除 `internal/render/{kickstart,preseed,autoinstall,autoyast,unattend}.go` | 业务字面量搬进骨架正文，剩余部分并入上面两个新文件 |
| 修改 `internal/render/esxi.go` | 只保留 `RenderESXiBootCfg`（boot.cfg 归系统） |
| 删除 `internal/render/imagewrite.go` | 镜像直写整条删除 |
| **seed** | |
| 创建 `internal/seed/seed.go` | 读 YAML 定义、落库（`draft` 态）、幂等；**先 hook 后模板** |
| 创建 `internal/seed/hooks/partition.yaml` | 分区 hook，仅 rhel 一份实现（§14.1） |
| 创建 `internal/seed/templates/{linux,windows,esxi}.yaml` | 3 份骨架模板定义 |
| **装配与文档** | |
| 修改 `cmd/lober/wire.go` | 注入 `Body`/`Impls` 的查库逻辑；接 seed |
| 修改 `SPEC.md`、`docs/contracts/{http-console,filesystem}.md` | 按设计 §11 同步 |

## 任务依赖顺序

```
1-2   删除镜像直写（纯删，先做，后面每段少一个维度）
3-6   model 层（中间态：新字段已加、旧字段未删，刻意如此）
7-9   契约 → conformance → fake → postgres
10-12 展开引擎 + 系统部件表（可与 7-9 并行）
13-15 骨架正文 + 六渲染器改写（依赖 3-12 全部）
16-17 装配点 + 文档
```

---

# 阶段一：删除镜像直写通道

### 任务 1：删除 model 层的通道与镜像类型

**目标：** 把 `DeliveryChannel` 这一维与镜像制品三项从领域类型里移除，选集回到四元组。

**文件：**
- 修改：`internal/core/model/inventory.go` —— 删 `DeliveryChannel` 类型与 2 常量、
  `Normalize`/`IsInstaller`/`IsImageWrite`/`Valid`、`deliveryChannels` 白名单、
  `ArtifactSet.Channel`、`ArtifactSet.Image`、`MatchesChannel`、`ImageArtifact`、
  `PartitionLayout`、`Partition`、4 个 `PartitionRole*` 常量、`RootPartition()`
- 修改：`internal/core/model/authoring.go` —— 删 `Template.Channel`
- 修改：`internal/core/model/inventory_test.go` —— 删对应用例

**不要误删：** `internal/imports/iso9660.go:388-415` 的 `rockRidgeSignatureNM` 与签名
无关（Rock Ridge Interchange Protocol 的字段标识）。

- [ ] **红：** 本任务是纯删除，红灯形式是「删掉类型后 `go build ./internal/core/model/`
      失败」——先删类型定义，记录编译器报出的全部引用点，那份清单就是任务 2 的工作量。
      预期报错集合应与设计 §6.1 的 9 个生产文件清单一致；若出现清单外的文件，停下来
      核对是不是误删了别的东西。

- [ ] **绿：** 删完 model 层两个文件里的全部目标符号，`go build ./internal/core/model/`
      与 `go test ./internal/core/model/ -count=1` 绿。

- [ ] **重构 + commit：** 检查 `inventory.go` 文件头注释里对镜像直写的描述是否需要
      同步删除。commit（`refactor(model)!: 删除镜像直写通道的类型与字段`）。

**风险：** `ArtifactSet.Channel` 的字段注释里论证了「零值等价于 installer 是兼容性硬
要求」。那段论证随字段一起删，但**删除本身不需要兼容处理**——库里那一列直接不再读。

---

### 任务 2：删除其余 8 个生产文件里的通道引用

**目标：** 让 `go build ./...` 在没有 `DeliveryChannel` 的前提下全绿。

**文件：**
- 删除：`internal/render/imagewrite.go`、`internal/render/imagewrite_test.go`
- 删除：`internal/imports/signature.go`、`internal/imports/signature_test.go`
- 删除：`internal/console/channel_test.go`
- 删除：`pipeline/`（整个目录）
- 删除：`docs/superpowers/plans/2026-09-20-lober-phase3-03-image-pipeline.md`
- 修改：`internal/render/renderer.go:46-47` —— 删通道分叉，`RenderAnswer` 只走一条路
- 修改：`internal/console/template.go` —— 删 `channel` 入参与校验
- 修改：`internal/console/task.go` —— 删建任务的通道一致性交叉校验
- 修改：`internal/core/fake/store.go`、`internal/core/contract/conformance/store.go`
- 修改：`internal/store/postgres/{artifact,template}/row.go` —— 删通道列与镜像列
- 修改：`internal/config/config.go:232-247` —— 删 `ImageSignaturePublicKeyPath`
- 修改：`deploy/lober.yaml.example` —— 删对应配置项

- [ ] **红：** `go build ./...`，预期失败，报错列表即待改清单。

- [ ] **绿：** 逐个改到 `go build ./...` 绿。`internal/config` 删字段时同步删
      `Validate` 里对它的校验与相关测试。

- [ ] **重构 + commit：** `make check` 全绿（**零失败**）。确认
      `grep -rn "DeliveryChannel\|ImageArtifact\|PartitionLayout\|ChannelImageWrite" --include="*.go" internal/ cmd/`
      只剩空结果。commit（`refactor!: 删除镜像直写通道的全部实现与配置`）。

**风险：** schemacheck 里若有镜像列参与的索引断言，同步删。删列不需要写迁移——
`AutoMigrate` 不会删已存在的列，那些列留在库里不影响读写（这是开发阶段可接受的，
生产升级另论，写进 commit 信息）。

---

# 阶段二：model 层

### 任务 3：`model.Family` 族类型

**目标：** 引入族这一层，并把「族与家族的关系」这套规则收敛在一处。

**文件：**
- 创建：`internal/core/model/family.go`
- 测试：`internal/core/model/family_test.go`

**接口契约：**

```go
type Family string

const (
    FamilyLinux   Family = "linux"
    FamilyWindows Family = "windows"
    FamilyESXi    Family = "esxi"
)

func (f Family) Valid() bool
func FamilyOf(os OSFamily) (Family, bool)
func FamilyAllows(f Family, os OSFamily) bool
func (f Family) OSFamilies() []OSFamily
```

- `Valid`：空串**不合法**（与 `DeliveryChannel.Valid` 那种「空串等价于默认值」相反——
  族没有合理默认值，猜一个会让 ISO 落到错的分组里）。未知值不静默接受。
- `FamilyOf`：`rhel`/`debian`/`ubuntu`/`suse` → `linux`，`windows` → `windows`，
  `esxi` → `esxi`。未知家族 `ok=false`。
- `FamilyAllows`：族或家族任一非法时返回 `false`。
- `OSFamilies`：返回该族允许的家族列表，**顺序稳定**（供 UI 的 tab 顺序与错误文案
  用，不稳定会让 golden 与快照测试随机失败）。返回新分配的切片。

- [ ] **红：** 为四类输入写失败测试：合法族×允许家族、合法族×不允许家族（如 `linux`
      × `windows`）、非法族（空串、拼错）、非法家族。`FamilyOf` 覆盖 6 个已知家族加
      一个未知值。运行 `go test ./internal/core/model/ -run TestFamily -v`，预期 FAIL。

- [ ] **绿：** 实现至测试通过。映射表用 map 字面量，与 `firmwareCompat`
      （`inventory.go:109`）同一风格。

- [ ] **重构 + commit：** 注释写明「族不能顶替家族」的理由（`linux` 族内四家是四种
      不同文件种类，不是四种方言）并引用设计 §2.1。commit（`feat(model): 新增 OS 族
      类型与族-家族映射`）。

**风险：** 不要让 `Family` 与 `OSFamily` 在类型上可互换（都是 `string` 底层类型，
传错不报错）。测试里要有一个「把 `OSFamily` 值当 `Family` 传会被 `Valid` 拒绝」的
用例兜住这个。

---

### 任务 4：`ArtifactSet.Family` 与导入交叉校验规则

**目标：** 让制品集携带族，并把「族与识别出的家族必须相符」做成可执行判定。

**文件：**
- 修改：`internal/core/model/inventory.go` —— `ArtifactSet` 加 `Family Family` 字段
- 修改：`internal/core/model/inventory_test.go`

**接口契约：**

```go
// 判定该制品集的族与家族是否自相符。用于导入完成写回前的最后一道校验。
func (a ArtifactSet) FamilyConsistent() bool
```

- 族为空时返回 `false`（不是「跳过校验」——空族的制品集无法被模板筛选到，等于一个
  建不了任务的死制品集，必须在导入阶段就拒绝）。
- 家族为空时返回 `false`。

- [ ] **红：** 覆盖四类：族与家族相符、不符（`windows` 族 × `rhel` 家族）、族为空、
      家族为空。运行 `go test ./internal/core/model/ -run TestArtifactSetFamily -v`，
      预期 FAIL。

- [ ] **绿：** 实现至通过。

- [ ] **重构 + commit：** 字段注释写明这个字段是**用户创建时选的**、而 `OSFamily` 是
      **导入时识别的**，两者来源不同因此需要交叉校验。commit（`feat(model): 制品集
      新增族字段与自相符判定`）。

**风险：** 真正的拒绝动作在 `internal/imports` 与 `internal/console`（本任务只给判定
函数），两处接线分别在任务 16 与 spec B。本任务不改那两个包。

---

### 任务 5：`Hook` 与 `HookImpl` 重构

**目标：** 把 hook 从「一段带家族与阶段的脚本」改成「一个功能 + 每家族一份实现」。

**文件：**
- 修改：`internal/core/model/authoring.go`
- 修改：`internal/core/model/authoring_test.go`

**接口契约：**

`Hook` 的改动是**一加五删**：加 `Impls map[OSFamily]HookImpl`，删下表五项。其余字段
（`ID`/`Name`/`Description`/`ParamsSchema`/`ParamsDefaults`/`Requires`/`Required`/
`TimeoutSec`/`Version`/`State`/`Author`/`Approver`/`ApprovedAt`）**原样不动**。

```go
type HookImpl struct {
    Interpreter string // bash | powershell | cmd | ash
    Body        string
    BodySHA256  string
}

func (h Hook) ImplFor(os OSFamily) (HookImpl, bool)  // ok=false 表示缺该家族实现
func (h Hook) ImplFamilies() []OSFamily              // 按家族名升序，顺序稳定
func HookDigest(impls map[OSFamily]HookImpl) string  // 全部实现的规范化序列化 sha256
func ImplDigest(body string) string                  // 单份正文的裸 sha256
func ValidInterpreter(s string) bool
```

`HookDigest` 的规范化定义：按家族名升序，`家族:解释器:正文` 三元组以 `\n` 连接。

**删除：** `Hook.Stage`、`Hook.OSFamilies`、`Hook.Interpreter`、`Hook.Body`、
`Hook.BodySHA256`、`Hook.SupportsFamily`、`HookStage` 类型与四个常量、
`validHookStages`、`HookStage.Valid`。

- **两个 digest 用途不同不可合并**：`HookDigest` 是审批边界（改任何一个家族的实现都
  产生新值，模板 pin 它以防浮动引用，SPEC §2.7）；`ImplDigest` 是内容寻址取回
  （机器侧 `run_hook` 用 `sha256sum -c` 校验，SPEC §6.3，所以不加盐、不加前缀、
  不规范化）。
- `HookDigest` 对空 map 返回空串的 sha256 还是空串：**返回空串**，并让调用方把「没有
  任何实现的 hook」当作非法（一个 digest 为空的 hook 引用无法 pin 任何东西）。
- `ValidInterpreter` 白名单 4 值。**不校验解释器与家族的搭配**——`windows` 下写 bash
  是可能的（装了 WSL 或 busybox），拦掉会挡住合法用法。

- [ ] **红：** 覆盖：`ImplFor` 命中/未命中；`ImplFamilies` 顺序稳定（同一 map 多次调用
      结果相同）；`HookDigest` 对「同内容不同插入顺序的两个 map」产出**相同**值、对
      「改了一个家族正文」产出**不同**值、对空 map 产出空串；`ImplDigest` 与一个已知
      字符串的 sha256 十六进制小写逐字符相符（用 `echo -n x | sha256sum` 事先算出的
      真实值当期望，不要自己用 Go 算一遍再断言——那等于拿实现验证实现）；
      `ValidInterpreter` 4 合法 + 2 非法。
      运行 `go test ./internal/core/model/ -run "TestHook" -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。`HookDigest` 的家族顺序必须排序后拼接（Go map 遍历顺序
      随机，不排序会让同一个 hook 每次算出不同 digest——这个 bug 在单次运行里可能
      偶然不复现，必须靠「同 map 多次调用」的用例兜住）。

- [ ] **重构 + commit：** 注释写明两个 digest 的分工与「hook 不带阶段」的理由（hook
      创建时不知道自己属于哪个模板，设计 §2.3）。commit（`feat(model)!: hook 改为
      每家族一份实现，删除 stage 与 os_families`）。

**风险：** 此任务删掉 `HookStage` 会让 `internal/render`、`internal/console` 大面积
编译失败。**这是刻意的中间态**——不要为了让 `go build ./...` 绿而在本任务里顺手改那
两个包。只保证 `go build ./internal/core/model/` 与该包测试绿；全仓绿在任务 6 末尾。

---

### 任务 6：`Template`、`TemplateBody`、`HookRef`、`DerivedRef`

**目标：** 模板改成「族 + 每家族一份正文」，并让 `go build ./internal/core/model/` 回到
无旧字段的干净状态。

**文件：**
- 修改：`internal/core/model/authoring.go`
- 修改：`internal/core/model/render.go` —— `RenderContext` 加 `Body`/`Impls`
- 修改：`internal/core/model/authoring_test.go`、`render_test.go`

**接口契约：**

`Template` 的改动是**三加三删**：加 `Family Family`、`Bodies map[OSFamily]TemplateBody`、
`DerivedFrom DerivedRef`，删下表三项。其余字段（`ID`/`Name`/`DiskPolicy`/`OnFailure`/
`KernelArgs`/`RequiredFormFields`/`Version`/`State`）**原样不动**。
`HookRef` 只改一个字段的类型：`Stage` 从 `HookStage` 枚举变成 `string`（模板正文里的
阶段名），其余五个字段不动。

```go
type TemplateBody struct {
    Text     string
    HookRefs []HookRef
}

type DerivedRef struct {
    TemplateID string
    Version    int
}

func (t Template) BodyFor(os OSFamily) (TemplateBody, bool)  // ok=false 表示缺该家族正文
func (t Template) BodyFamilies() []OSFamily                  // 按家族名升序，顺序稳定
func (t Template) FamilyConsistent() bool                    // Bodies 键是否都在 Family 允许集合里
func (b TemplateBody) HooksForStage(stage string) []HookRef   // 按 Order 升序，新分配的切片
```

**删除：** `Template.ArtifactSetID`、`Template.Channel`、`Template.AnswerTemplate`、
`Template.HooksForStage`（方法搬到 `TemplateBody` 上，入参类型从 `HookStage` 改 `string`）。

`RenderContext` 新增两字段：`Body TemplateBody`、`Impls map[string]HookImpl`（键是
`HookRef.HookID`，值已按家族选好）。

- `FamilyConsistent`：族为空且 `Bodies` 非空 → `false`；族为空且 `Bodies` 为空 →
  `true`（那是合法的空模板草稿，设计 §2.2）。
- `HooksForStage`：排序收敛在这一处，渲染器直接消费不再自己排——否则渲染器与控制台
  preview 会各排一遍，迟早不一致（沿用旧 `Template.HooksForStage` 的既有理由）。

- [ ] **红：** 覆盖：`BodyFor` 命中/未命中；`BodyFamilies` 顺序稳定；`HooksForStage`
      过滤正确、按 Order 升序、同 Order 时保持插入顺序（稳定排序）、返回的切片改动
      不影响原 `HookRefs`；`FamilyConsistent` 四类（全合法、有越界键、空族空正文、
      空族有正文）。运行 `go test ./internal/core/model/ -run "TestTemplate|TestRenderContext" -v`，
      预期 FAIL。

- [ ] **绿：** 实现至通过。旧 `HooksForStage` 用的是插入排序（稳定），保持这个性质。

- [ ] **重构 + commit：** `go build ./internal/core/model/` 与
      `go test ./internal/core/model/ -count=1` 全绿。注释写明 `DiskPolicy`/`OnFailure`
      的语义收窄（不再驱动渲染，改为建任务时的决策输入，结果进 `ResolvedParams` 供
      正文用 `{{fact:}}` 引用，设计 §4.3）。commit（`feat(model)!: 模板改为族 + 每家族
      一份正文，派生记录来源`）。

**风险：** 全仓此时仍编译不过（`render`/`console`/`store` 引用了已删字段）。这是计划
第 25 行声明的刻意中间态，任务 7-9 与 13-15 各自收口。

---

# 阶段三：契约与存储层

### 任务 7：conformance 套件加断言（先于两个实现）

**目标：** 把四层模型的存储行为写成可执行契约，让「套件变绿即集成完成」这条规则成立。

**文件：**
- 修改：`internal/core/contract/store.go` —— 若 `ArtifactSetStore`/`TemplateStore`/
  `HookStore` 的签名需要变，在此改；改动本身就是契约变更，注释要说明
- 修改：`internal/core/contract/conformance/store.go`

**要加的断言类别**（每类一个 `t.Run` 子测试）：

| 断言 | 要验证什么 |
|---|---|
| 制品集族往返 | 写入带 `Family` 的制品集，读回后 `Family` 相符 |
| 模板正文往返 | 写入含 2 份正文（各带 hookRefs）的模板，读回后两份都在、`Text` 逐字节相符、`HookRefs` 顺序保持 |
| 模板空正文往返 | `Bodies` 为 nil 与为空 map 两种写入，读回行为一致（不要一个变 nil 一个变空 map——那会让调用方被迫写两套判空） |
| hook 多实现往返 | 写入含 3 个家族实现的 hook，读回后三份都在，每份 `Interpreter`/`Body`/`BodySHA256` 相符 |
| `DerivedFrom` 往返 | 派生字段读写；零值往返后仍是零值 |
| 阶段名非 ASCII | `HookRef.Stage` 写入中文阶段名，读回逐字节相符（数据库列的编码与排序规则问题要在这里暴露，而不是在真机装机时） |
| 正文含特殊字符 | `Text` 含 `{{`、`}}`、单引号、双引号、反斜杠、`%`、换行，读回逐字节相符 |

- [ ] **红：** 按上表加断言。运行
      `go test ./internal/core/fake/ -run TestConformance -count=1`，预期 FAIL
      （fake 尚未实现新字段）。这个 FAIL 就是契约已就位的证据。

- [ ] **绿：** 本任务**不实现**，绿灯在任务 8、9。此处的「绿」是确认套件本身能编译且
      失败原因是「实现缺字段」而非「套件写错」——`go vet ./internal/core/contract/...` 绿。

- [ ] **重构 + commit：** 套件里新增的时间相关断言必须从 `fixedNow` 加减偏移量取时刻，
      不要新引入 `time.Now()`（套件头部注释详述了时间基准漂移问题）。commit
      （`test(contract): conformance 套件补齐四层模型的存储断言`）。

**风险：** 正文可能很长（几 KB 的 kickstart）。断言里的正文夹具用**真实骨架的片段**而
不是 `"foo"`——列类型选错（`varchar(255)` 而非 `text`）只有在写入长文本时才暴露。

---

### 任务 8：fake 内存实现补齐

**目标：** 让 conformance 套件在内存实现上全绿，为并行模块提供可用的测试替身。

**文件：**
- 修改：`internal/core/fake/store.go`

- [ ] **红：** `go test ./internal/core/fake/ -run TestConformance -count=1`，预期 FAIL
      （承接任务 7）。

- [ ] **绿：** 补齐字段读写至套件全绿。**深拷贝**：`Bodies`、`Impls`、`HookRefs`、
      `ParamsDefaults` 都是引用类型，直接存引用会让调用方改一个 map 就改到了「库里」
      的数据——内存实现与真实数据库的行为分叉，而 conformance 套件的全部价值就在于
      两者行为一致。任务 7 的往返断言若没覆盖这一点，在此补一个「读回后修改返回值，
      再读一次，值不变」的用例。

- [ ] **重构 + commit：** `go test ./internal/core/fake/ -count=1` 全绿。commit
      （`feat(fake): 内存 Store 支持四层模型`）。

**风险：** 深拷贝漏一层（拷了 `map[OSFamily]TemplateBody` 但没拷 `TemplateBody.HookRefs`
切片）是最容易犯的错，测试要逐层验证。

---

### 任务 9：postgres 实现与 schemacheck

**目标：** 让 conformance 套件在 PostgreSQL 实现上全绿，索引结构断言随列改动同步。

**文件：**
- 修改：`internal/store/postgres/artifact/row.go` —— 加 `family` 列
- 修改：`internal/store/postgres/template/row.go` —— `bodies` 列（JSONB）、
  `derived_from_*` 列；删 `artifact_set_id`/`channel`/`answer_template`
- 修改：`internal/store/postgres/hook/row.go` —— `impls` 列（JSONB）；删
  `stage`/`os_families`/`interpreter`/`body`/`body_sha256`
- 修改：`internal/store/postgres/schemacheck.go`

**存储形态决策：** `Bodies` 与 `Impls` 存 **JSONB 单列**，不拆子表。理由：两者都只被
「按 ID 取整个模板/hook」这一种方式读，从不按正文内容查询；拆子表会让每次读模板多
一次 join，而换来的查询能力没有任何调用方需要。代价是无法用 SQL 按正文内容检索——
明确接受。

- [ ] **红：** `go test ./internal/store/postgres/ -run TestConformance -count=1`
      （需 `LOBER_TEST_PG_DSN`），预期 FAIL。

- [ ] **绿：** 补齐至全绿。JSONB 的序列化用 `internal/store/postgres/pgtype` 现有约定
      （先看那个包已有的 JSONB 处理方式，跟着走，不要新造一套）。

- [ ] **重构 + commit：** **索引断言必须同步改**——`AutoMigrate` 有两个不报错的失效点
      （复合索引漏标列会静默降级成单列索引；已存在的索引永不更新）。若 `family` 列
      参与任何索引，schemacheck 的 `pg_indexes.indexdef` 结构断言要加对应条目，WHERE
      按 PostgreSQL 规范化后的实际输出写（`!=` 变 `<>`、`''` 变 `''::text`、每层比较
      外加括号），**不要凭猜**——先在 psql 里建一次索引看 `indexdef` 的真实输出。
      `make check` 全绿后 commit（`feat(store): postgres 支持四层模型，同步索引断言`）。

**风险：** 删列时 `AutoMigrate` 不会真删（GORM 不删列），旧列留在表里。开发库可以直接
`DROP TABLE` 重建，这是任务 2 已确立的「存量数据不迁移」策略的延续。若测试夹具依赖
旧列存在，会得到一个令人困惑的「列不存在」错误——夹具每个用例建删独立 schema，所以
实际不会撞上，但要知道这个机制。

---

# 阶段四：占位符展开引擎

### 任务 10：占位符扫描器

**目标：** 从用户正文里识别三类占位符及其位置，不做展开——扫描与展开分开，因为
「模板里有哪些阶段」这个查询（spec B 的 `/stages` 端点）只需要扫描。

**文件：**
- 创建：`internal/render/expand.go`
- 测试：`internal/render/expand_test.go`

**接口契约：**

```go
type PlaceholderKind int
const (
    PlaceholderStage PlaceholderKind = iota
    PlaceholderPart   // {{lober:...}}
    PlaceholderFact
    PlaceholderLiteral // {{lober:literal}}，转义用，展开成空串
)

type Placeholder struct {
    Kind   PlaceholderKind
    Name   string // stage 名 / 部件名 / fact 名；literal 为空
    Start  int    // 在正文中的字节偏移（含 "{{"）
    End    int    // 结束偏移（不含，即 "}}" 之后）
    Indent string // 该占位符所在行的前导空白，供缩进照抄
}

// ScanPlaceholders 按出现顺序返回正文里的全部已识别占位符。
// 未识别的 {{...}}（cloud-init Jinja 等）不返回、不报错。
func ScanPlaceholders(text string) []Placeholder

// StageNames 返回正文里 {{stage:}} 的名字，按出现顺序，去重后保持首次出现位置。
func StageNames(text string) []string
```

**识别规则**（设计 §3.1、§3.2）：
- 只识别 `{{stage:`、`{{lober:`、`{{fact:` 三个确切前缀，**绝不贪婪匹配任意 `{{...}}`**。
- 名字字符集：字母、数字、连字符、下划线、中文。**空格与 `}` 不允许**——前者让阶段名
  写错一个空格变成一个查不出来的新阶段，后者会让占位符提前闭合。含非法字符的按
  「未识别」处理（原样保留，不报错）。
- 前缀后无 `}}` 闭合的（`{{stage:abc` 到行尾），按未识别处理。
- `Indent` 取占位符所在行从行首到第一个非空白字符的那段；占位符前有非空白内容时
  （如 YAML 的 `- {{stage:x}}`），`Indent` 仍取行首空白（设计 §3.6）。

- [ ] **红：** 覆盖这些输入类别，每类的期望都要明确写在测试名里：三类前缀各一个；
      一行内两个占位符；**cloud-init Jinja `{{ ds.meta_data.local_hostname }}` 必须
      不被识别**；`{{lober:literal}}{{stage:x}}` 转义序列（前者识别为 Literal，后者
      识别为 Stage——注意这里 Stage **应当**被识别，展开阶段才把它当字面量，见任务 11）；
      名字含中文；名字含空格（不识别）；未闭合；嵌套 `{{fact:{{x}}}}`；空正文。
      运行 `go test ./internal/render/ -run TestScanPlaceholders -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。**不要用正则**——`{{` 与 `}}` 在 YAML/XML 正文里可能以任意
      方式组合，手写扫描器能精确控制「只认三个前缀」这条规则，正则很容易在回溯时把
      Jinja 吞掉。

- [ ] **重构 + commit：** 注释引用设计 §3.2，写明为什么不用 text/template（cloud-init
      原生支持 Jinja，`{{ }}` 是用户正文里的合法内容；现有代码已在三处刻意避开
      text/template 并各自写明理由）。commit（`feat(render): 占位符扫描器`）。

**风险：** `Indent` 对制表符与空格混用的正文要照抄原样，不要归一化——YAML 禁止制表符
缩进，把用户的制表符换成空格会「修好」一个本该报错的正文，而 XML 与 kickstart 里
制表符是合法的。照抄是唯一不会猜错的做法。

---

### 任务 11：展开引擎

**目标：** 把扫描到的占位符替换成实际内容，缩进照抄，未识别的原样保留。

**文件：**
- 修改：`internal/render/expand.go`
- 测试：`internal/render/expand_test.go`

**接口契约：**

```go
// Expander 提供每类占位符的展开内容。展开引擎本身不知道任何家族差异。
type Expander interface {
    // Stage 返回该阶段的完整文本（ClientLib + hook 调用 + 信号上报）。
    Stage(name string) (string, error)
    // Part 返回系统部件文本。name 是 syslog | firstboot | cleanup。
    Part(name string) (string, error)
    // Fact 返回 fact 值。
    Fact(name string) (string, error)
}

// Expand 展开正文里的全部占位符。
func Expand(text string, e Expander) (string, error)
```

**展开规则：**
- 多行内容的**每一行**套用 `Placeholder.Indent`（设计 §3.6）。第一行不重复加缩进
  （它已经在原位置上）。
- `{{lober:literal}}` 展开成空串——它的作用是打断紧随其后的占位符前缀。实现上：
  遇到 Literal 就把它删掉，并**跳过紧随其后的那一个占位符**（原样保留其文本）。
- `Expander` 任一方法返回 error 时，`Expand` 立即返回该 error 并**包上占位符的位置
  信息**（第几行、占位符原文）——一份 300 行的 kickstart 里报「fact 不存在」而不说
  哪一行，运维无从下手。
- 未识别的 `{{...}}` 原样保留。
- **未识别的 `{{lober:xxx}}` 报错**（拼错了部件名；静默保留会让 installer 把它当普通
  文本，产出的应答文件可能语法仍合法但行为全错，设计 §3.7）。这条与上一条的区别：
  `{{lober:` 前缀已经明确是在对我们说话，名字拼错是错误；而裸 `{{...}}` 根本不是在
  对我们说话。

- [ ] **红：** 覆盖：三类占位符各自展开；多行内容的缩进照抄（用一个 6 空格缩进的
      YAML 场景，验证展开出的每行都有 6 空格）；YAML 列表项 `- {{stage:x}}` 场景；
      `{{lober:literal}}{{stage:x}}` 产出字面量 `{{stage:x}}`；未识别 `{{...}}` 原样
      保留；未识别 `{{lober:xxx}}` 报错且错误信息含拼错的名字与三个合法部件名；
      Expander 报错时错误信息含行号；空正文；只有一个占位符的正文。
      运行 `go test ./internal/render/ -run TestExpand -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。用 `strings.Builder` 按扫描结果分段拼接，不要做多轮
      `strings.Replace`——多轮替换会让第一轮展开出的内容被第二轮再次扫描（hook 脚本
      里若含 `{{` 就会被误展开）。**单轮、按偏移分段**是正确性要求，不是优化。

- [ ] **重构 + commit：** commit（`feat(render): 占位符展开引擎`）。

**风险：** 展开出的内容**不再参与扫描**这一点必须有测试兜住：构造一个 Expander，让它
的 `Stage` 返回含 `{{fact:x}}` 的文本，断言产出里那段文本原样保留。这是单轮展开与多轮
替换的行为分水岭，也是安全边界（hook 正文来自用户，不能让它触发二次展开）。

---

### 任务 12：`systemParts` 六家族系统部件表

**目标：** 把 SPEC §5.1 那三件物理必需的事按家族给出文本形态，这是家族差异的唯一去处。

**文件：**
- 创建：`internal/render/systemparts.go`
- 修改：`internal/render/skeleton.go` —— 从三家族扩到六家族
- 测试：`internal/render/systemparts_test.go`

**接口契约：**

```go
// SystemParts 是某家族的三个系统部件文本。
type SystemParts struct {
    Syslog    string // syslog 指向指令（kickstart 是 logging --host= 一行）
    Firstboot string // firstboot 脚手架：unit + 脚本 + token + 参数
    Cleanup   string // 凭据清理：擦 finalize token 与应答文件副本里的密码哈希
}

// BuildSystemParts 按家族与渲染上下文产出三个部件的文本。
// 未知家族返回 error（沿用现有「未知的 OS 家族」文案）。
func BuildSystemParts(ctx model.RenderContext) (SystemParts, error)

// StageText 产出一个阶段的完整文本：ClientLib + 按 Order 排序的 run_hook 调用
// + 阶段完成信号上报。三段顺序固定。
func StageText(ctx model.RenderContext, stage string) (string, error)
```

**`StageText` 的三段**（设计 §3.3）：
1. `ClientLib(ctx)` —— **每个阶段无条件一份**。不是冗余：anaconda 的每个 `%pre`/`%post`
   段是独立执行的脚本，shell 函数定义不跨段（`kickstart.go` 现在就注入了 4 次，
   97/291/344/371 行）。preseed 的两个 command 键、autoyast 的每个 `<script>`、
   unattend 的每条 `RunSynchronousCommand` 同理。
2. 该阶段的 hook 调用，每个一行 `run_hook <digest> <timeout>`。digest 取
   `ctx.Impls[ref.HookID].BodySHA256`；该 `HookID` 不在 `Impls` 里时报错（文案：
   hook 名 + 缺哪个家族的实现 + 「补该家族实现或换 hook」）。
3. 阶段完成信号上报。

`ClientLib` 对 `windows`（powershell）与 `esxi`（busybox ash）需要各自一版——现有
`clientlib.go` 是 bash 版。检查现状：若已有 powershell/ash 版本则复用，否则本任务补齐。

- [ ] **红：** 覆盖：6 个家族各自的 `BuildSystemParts` 三个字段非空；未知家族报错；
      `StageText` 的三段顺序（ClientLib 在最前，信号在最后）；一个阶段挂 3 个 hook 时
      按 Order 升序排列；hook 缺该家族实现时报错且文案含 hook 名与家族名；空阶段
      （无 hook）仍产出 ClientLib 与信号上报。
      运行 `go test ./internal/render/ -run "TestSystemParts|TestStageText" -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。三个部件的文本内容从现有 `kickstart.go`/`preseed.go` 等
      六个渲染器里**原样搬过来**，不要重写——那些文本已经真机验证过（RHEL 与 Debian
      两条链路，见 `docs/real-machine/`）。

- [ ] **重构 + commit：** 注释写明这张表是六家族唯一剩下的家族分支，且只含物理必需的
      三件事、不含任何业务内容（引用 SPEC §5.1 的「物理约束（不可绕过）」那张表）。
      commit（`feat(render): 六家族系统部件表与阶段文本生成`）。

**风险：** `skeleton.go` 的文件头写明「抽出的是内容，不是如何嵌入应答文件——三种应答
文件语法的嵌入方式本质不同」。那句话正是本设计的分工（内容归 `systemParts`，嵌入位置
归用户正文的占位符），所以**复用它、不要删**。它现在覆盖三个家族，本任务扩到六个。

---

# 阶段五：骨架正文与渲染器改写

### 任务 13：确认 ESXi 与 Ubuntu 的双文件边界 —— ✅ 已完成（2026-09-29）

**结论：`AnswerFileName` 不需要改。** 双文件机制已经完整接线，两份文件走同一个
`/v1/answer` 端点靠文件名分派。完整结论与证据行号见设计文档 §5.2.1，不在此重复。

任务 14/15 要用到的三条：

1. **`meta-data` 可以进 `systemParts`**（纯靠任务上下文现算，`autoinstall.go:310-327`）；
   **`boot.cfg` 不能**——它要读盘（原始素材在 `ArtifactPaths.Extra["boot.cfg"]`），
   读盘发生在网关层的专属分支（`internal/gateway/esxi_bootcfg.go`），继续留在那里不动。
2. **ESXi 的模板正文是方言 kickstart**，交付路径已存在（`renderer.go:115` 的
   `AssembleESXiKickstart`），不需要新建交付方式——原先担心的最大风险不成立。
3. **只有这两家双文件。** 唯一放行点是 `answer.go:169-177` 的 `answerFileNameAllowed`，
   函数体只有两个 `if`。其余四家单分支单渲染函数。

**顺带查出两处既存漂移**（非本次改造引入，详见设计 §5.2.2）：

- `docs/contracts/http-machine.md:271-273` 的「文件名逐字符相等」与代码已不一致。
  **处置（用户已定）：按代码现状更新契约文档**，落在任务 17。
- `internal/render/esxi.go:532-565` 的「契约缺口 1」注释已过期（它说的未接线状态已被
  后续改动填上）。任务 15 重写 `esxi.go` 时一并清理。

---

### 任务 14：3 份骨架正文（从 golden 反推）

**目标：** 产出与改造前逐字节等价的骨架正文，这是整个 spec A 唯一可信的正确性锚点。

**文件：**
- 创建：`internal/seed/hooks/partition.yaml` —— 分区 hook，仅 rhel 实现（§14.1、§14.2）
- 创建：`internal/seed/templates/linux.yaml`（4 份正文：rhel/debian/ubuntu/suse）
- 创建：`internal/seed/templates/windows.yaml`、`internal/seed/templates/esxi.yaml`
- 创建：`internal/seed/seed.go` —— 读 YAML、落库为 `draft`、幂等（同名已存在则跳过）
- 测试：`internal/seed/seed_test.go`

**YAML schema：**

```yaml
name: Linux 基线
family: linux
diskPolicy: largest
onFailure: reboot
bodies:
  rhel:
    text: |
      <骨架正文>
    hookRefs: []
  debian:
    text: |
      <骨架正文>
```

**参与反推的是 16 个 golden 里的 8 个**：`kickstart`、`preseed`、`autoinstall`、
`autoyast`、`unattend` 各自的 `_full` 与 `_minimal`，加 `esxi_kickstart_full`。**不参与**
的 5 个：`menu_debian` / `menu_rocky` / `menu_ubuntu`（iPXE 菜单，格式归系统，§5.3）、
`esxi_bootcfg`（mboot.efi 参数文件，归系统）、`meta_data`（cloud-init 元数据，归系统）。

**反推方法**（设计 §7.1，逐步做，每步之后正文都应当仍能产出等价输出）：

1. 取该家族的 golden 输出。
2. 系统部件（ClientLib、syslog 行、firstboot 脚手架、cleanup）替换成对应占位符。
3. hook 调用段替换成 `{{stage:}}`。**阶段名用现有四个阶段的中文名**（`pre` 段叫
   「安装前」等）——骨架是给运维当派生起点的，中文名更可读；但要在 YAML 注释里写明
   这些名字只是默认值、用户可随意改。
4. 任务相关的具体值替换成 `{{fact:}}`。
5. 其余原样保留。

### 14.1 rhel 骨架必须用 by-hook 分区（用户 2026-09-29 决定）

**只有 rhel 需要按固件分支产出分区指令。** `partitionDirectives`
（`internal/render/kickstart.go:462`）按 `ActualFirmware` 二选一：UEFI 出
`part /boot/efi --fstype=efi --size=600`，BIOS 出
`part biosboot --fstype=biosboot --size=1`。给错或都给都会让 anaconda 的存储校验失败。

占位符体系是纯字符串替换、**刻意没有条件语法**，所以一份正文写不出「按固件二选一」。
方案：rhel 骨架用现有的 `DiskPolicy=by-hook`，正文只写 `%include`，真正的分区指令由
`%pre` 阶段的一个自带 hook 用 shell 判固件后写出（`[ -d /sys/firmware/efi ]`）。

**其余五家不需要固件分支**，各自的安装器自己处理，这是查证过的（证据行号一并记下，
免得后来者以为是漏了）：

| 家族 | 分区写法 | 为什么不需要分支 |
|---|---|---|
| debian | `partman-auto/choose_recipe=atomic`（`preseed.go:170`） | partman 自己按固件建 ESP 或 biosboot。且 preseed **没有** `%include` 那种延迟机制（`preseed.go:152-153`），by-hook 对它不可用也不需要 |
| ubuntu | `storage.layout: direct`（`autoinstall.go:92`） | curtin 自己决定 |
| suse | `<use>all</use>`（`autoyast.go:166-171`） | YaST 自己决定 |
| windows | `<Extend>true</Extend>` 单分区（`unattend.go:68-80`） | 不按固件分支（该配置在 UEFI 下是否完备是**既存问题**，不属本次范围，不要顺手改） |
| esxi | 硬拒绝非 UEFI（`esxi.go:218`） | ESXi 只支持 UEFI，是拒绝不是分支 |

**`%include` 的位置是硬约束**：必须排在 `%pre` 段**之后**。anaconda 在**解析**
kickstart 时就展开 `%include`，而 `%pre` 是解析完成后才执行的——放前面等于引用一个还
不存在的文件。实测（2026-09-26）：anaconda 在校验阶段失败退出，分区从未执行，目标盘
只有 192KB，而 `%pre` 明明成功跑完了。骨架正文里位置已摆对，派生天然带着；这一点要
写进 YAML 注释。

### 14.2 自带「分区」hook 也要 seed

骨架模板引用一个 hook，那个 hook 必须同时被 seed，否则模板保存时的「被引用 hook 必须
有该家族实现」校验（plan B 任务 2）直接失败，骨架模板根本存不进去。

- 创建 `internal/seed/hooks/partition.yaml` —— 一个 hook，`rhel` 一份实现
  （bash，按 `[ -d /sys/firmware/efi ]` 分支 echo 出 part 指令到
  `ByHookPartitionIncludePath`）。其余家族**不提供实现**——它们的骨架不引用这个 hook。
- seed 顺序：**先 hook 后模板**。模板的 `HookRef.HookDigest` 要 pin 已落库 hook 的
  digest，反序会让模板引用一个算不出 digest 的 hook。
- hook 正文里的路径常量取自 `render.ByHookPartitionIncludePath`（已存在），**不要在
  YAML 里另写一份字面量**——两处不一致时的症状是「装机以 `%include` 的文件不存在失败」。

- [ ] **红：** 为 seed 写失败测试：导入后库里有 1 个 hook + 3 个 `draft` 模板、族与正文
      份数相符（linux 4 份、windows 1 份、esxi 1 份）；重复导入不产生重复记录（幂等）；
      每份正文的 `{{lober:syslog|firstboot|cleanup}}` 各恰好一次；每个 `hookRef.Stage`
      都在对应正文的 `StageNames` 里；rhel 正文含 `%include` 且它出现在最后一个 `%pre`
      段的 `%end` **之后**（位置约束，§14.1）；rhel 正文**不含** `clearpart --drives=`
      与 `part biosboot`（那些搬进 hook 了）。运行 `go test ./internal/seed/ -v`，预期 FAIL。

- [ ] **绿：** 实现 seed 并写出 1 份 hook + 3 份模板 YAML 至测试通过。

- [ ] **重构 + commit：** commit（`feat(seed): 骨架模板与分区 hook 的首次启动导入`）。

**风险：** `windows`、`suse`、`esxi` 三家的 golden 只有 `_full` 没有真机验证记录，反推
出的骨架正文**正确性弱于** rhel/debian。这一点要写进 YAML 的注释头：哪些家族的骨架
经过真机验证、哪些只经过 golden 比对。不要让运维以为六份骨架的可信度相同。

---

### 任务 15：渲染器入口改写与六个家族生成器删除

**目标：** 让 `RenderAnswer` 走「取正文 → 展开」这一条路，`internal/render` 不再含任何
业务字面量。

**文件：**
- 修改：`internal/render/renderer.go` —— 入口改为调 `Expand`
- 删除：`internal/render/{kickstart,preseed,autoinstall,autoyast,unattend}.go` 及其测试
- 修改：`internal/render/esxi.go` —— 只保留 `RenderESXiBootCfg` 与任务 13 确认的边界
- 修改：`internal/render/golden_test.go` —— golden 断言改为「骨架正文经展开后 == 现有
  golden 文件」

**接口契约：**

```go
// RenderAnswer 渲染本次任务的应答文件。
// Body 与 Impls 必须已由调用方注入（纯函数约束，SPEC §5.1）。
func RenderAnswer(ctx model.RenderContext) (string, error)
```

- `ctx.Body.Text` 为空时返回 error（空正文的模板不该走到渲染——那说明建任务时的
  「模板缺该家族正文」校验被绕过了，静默产出空应答文件会让机器装到一半停住）。
- 内部构造一个实现 `Expander` 的适配器，三个方法分别委托给 `StageText`、
  `BuildSystemParts`、`ctx.Facts` 查表。
- `Fact` 查不到时返回 error，文案含 fact 名与可用 fact 前缀提示。

**要清零的字面量**（设计 §7.2，删文件时天然完成，但 `esxi.go` 与 `skeleton.go` 要逐条
核）：

| 内容 | 原位置 |
|---|---|
| Ubuntu locale / username / keyboard / storage layout / network version | `autoinstall.go:194-216` |
| Debian 分区方法与 recipe | `preseed.go:168-174` |
| SUSE 基础骨架与软件选择 | `autoyast.go:160-181` |
| Windows 架构映射与骨架 | `unattend.go:65-136,163-172` |
| RHEL 基础指令与分区 | `kickstart.go:191-267,413-470` |
| 各家族镜像源地址 | 各渲染器的 repo/mirror 段 |

**golden 锚点分三档，不是一律逐字节**（核实后的结论，理由见下）：

| 档 | 哪些 golden | 判据 |
|---|---|---|
| A 逐字节锁定 | `preseed_full`、`autoinstall_full`、`autoyast_full`、`unattend_full`、`esxi_kickstart_full` | 展开产出与现有文件**逐字节相同**，禁用 `-update` |
| B 重新生成 + 人工审阅 | 五个 `_minimal` | 差异**必须恰好是**「多出若干只含 ClientLib 与信号上报的空段落」，其它任何差异都是 bug |
| C 换锚点 | `kickstart_full`、`kickstart_minimal` | 这两个锁的是 `largest` 策略（直接输出 part 行），而 rhel 骨架改用 by-hook（§14.1）。改为复用 `kickstart_byhook_test.go` 已有的 4 个断言，并新生成一份 `kickstart_byhook.golden` 人工审阅 |

**B 档为什么必然有差异**：`kickstart.go:90` 的 `if len(lines) == 0 && !hasSignals` 让没挂
hook 的阶段**整段不产出**。实测对比：`kickstart_minimal.golden` 有 7 个段落、
`kickstart_full.golden` 有 8 个，差的正是 `%post --nochroot`（minimal 的夹具
`ctx.Hooks` 为空）。新模型下段落由正文**写死**，一份骨架正文只有一种段落结构，因此
「无 hook 时段落消失」这个行为不再存在——这正是用户要的「模板自己定义自己有几个阶段」。
多出来的空段落只含 ClientLib 与一次信号上报，在各家安装器下都合法，且多一个阶段的
可观测性反而是好事。

**C 档为什么换锚点**：rhel 骨架的分区搬进了 by-hook hook，正文里只有 `%include`，与
`largest` 策略的 golden 在分区那几行上必然不同。`kickstart_byhook_test.go` 的 4 个既有
断言（`ByHookEmitsPartitionInclude`、`ByHookDocumentsContractInComment`、
`NonByHookWithEmptyDiskFactFails`、`NonByHookWithDiskFactUnchanged`）覆盖的正是这条
路径，直接复用比硬凑一个逐字节比对可信。

- [ ] **红：** 改 `golden_test.go`：用任务 14 的骨架正文加真实渲染上下文跑 `RenderAnswer`，
      按上表分档断言。运行 `go test ./internal/render/ -run TestGolden -v`，预期 FAIL
      （`RenderAnswer` 还是旧实现）。

- [ ] **绿：** 改 `RenderAnswer` 并删六个家族文件，至 golden 全绿。**A 档逐字节不符时改
      骨架正文，不改 golden**；B/C 档用 `-update` 生成后**必须 `git diff` 逐字符审阅**，
      差异超出上表描述的范围就是 bug（`-update` 不是让测试变绿的开关，`golden_test.go:6`
      与 `:23` 已写明这条纪律）。

- [ ] **重构 + commit：** `grep -rn "locale\|timezone\|keyboard\|partman\|--ondisk"
      --include="*.go" internal/render/ | grep -v _test` 应只剩注释与 fact 键名。
      `make check` 全绿。commit（`refactor(render)!: 六家族生成器改为占位符展开`）。

**风险：** 这是整个计划最危险的一步——RHEL 与 Debian 现在能真机装通靠的正是这些硬编码。
A 档的逐字节比对是主要护栏，而 rhel（C 档）**恰好没有这道护栏**，它的正确性只能靠
真机验证兜——所以整体验收里 RHEL 的 QEMU 装机是必须项而不是可选项。若某个家族始终
对不上，**把那个家族单独拆成一个任务**延后，不要为了让测试变绿而把 A 档降级成「包含
关键行」之类的弱断言——那会让这道锚点失效，而失效的代价是在真机上装到一半停住、只能
到 BMC 控制台看。

`TestExec_RequiredHookFailureAbortsSectionEvenWithLaterHooks` 会随 `kickstart.go` 与
`kickstart_exec_test.go` 一起被删除——**用例不再存在**，不是「用例失败」。回归判据始终是
零失败。三个 `TestInspect_*` 在 `inspect.go`（保留不动），本任务后应当仍然全绿。

---

# 阶段六：装配与文档

### 任务 16：装配点注入与导入侧交叉校验

**目标：** 让 gateway 与 console 的渲染调用能拿到 `Body` 与 `Impls`，并让导入拒绝族与
家族不符的 ISO。

**文件：**
- 修改：`cmd/lober/wire.go` —— 装配 seed 导入；构造渲染上下文时按 `Artifact.OSFamily`
  取 `Template.Bodies[家族]` 与每个 hook 的 `Impls[家族]`
- 修改：`internal/gateway/` 的 `/v1/answer` 路径 —— 调用注入好的构造器
- 修改：`internal/imports/job.go` —— 写回制品集内容前调 `ArtifactSet.FamilyConsistent()`，
  不符则置 `failed` 与 `FailReason`
- 测试：`cmd/lober/` 与 `internal/imports/job_test.go`

**接口契约：** 渲染上下文的构造是**跨模块的**（要查 template、hook、artifact 三个
Store），因此归装配点。gateway 侧只接收一个已注入的构造函数：

```go
// 装配点提供给 gateway 的窄接口。gateway 不认识 Store。
type RenderContextBuilder interface {
    Build(ctx context.Context, taskID string) (model.RenderContext, error)
}
```

- `Build` 内部顺序：取任务 → 取制品集（拿家族）→ 取模板 → `BodyFor(家族)`（缺则 error）
  → 逐个取 hook 并 `ImplFor(家族)`（缺则 error）→ 组装。
- 错误文案给可行下一步（模板名 + 缺哪个家族 + 「补齐该正文或改用其它模板」）。

- [ ] **红：** 为 `Build` 写失败测试（用 fake Store）：正常路径产出的 `Body` 与 `Impls`
      内容正确；模板缺该家族正文时报错且文案含模板名与家族名；hook 缺该家族实现时同理；
      制品集不存在时报错。导入侧：族与家族不符时制品集置 `failed` 且 `FailReason` 非空、
      **不写 Paths**（半成品制品集比没有制品集更危险，它看起来可用）。
      运行 `go test ./cmd/lober/ ./internal/imports/ -count=1 -v`，预期 FAIL。

- [ ] **绿：** 实现至通过。`archtest` 必须仍绿——`RenderContextBuilder` 的接口定义放
      gateway 包内（调用方定义接口，与 `console.ArtifactImporter` 同一种接缝），具体
      实现在 `cmd/lober`。

- [ ] **重构 + commit：** `go test ./internal/archtest/ -count=1` 绿（依赖方向守卫）。
      `make check` 全绿。commit（`feat(cmd): 装配渲染上下文构造与族一致性校验`）。

**风险：** `deps_test.go` 的守卫**含测试 import**。给 gateway 写测试时若为了造夹具
import 了 `internal/store/postgres` 的具体类型，守卫会失败——用 `internal/core/fake`。

---

### 任务 17：SPEC 与冻结契约同步

**目标：** 让文档与代码一致，避免下一个读 SPEC 的人按已废弃的设计动手。

**文件：**
- 修改：`SPEC.md` —— 按设计 §11.1 那张表改 11 个小节，新增 2 节
- 修改：`docs/contracts/http-console.md` —— 追加变更记录（形态细节在 spec B）
- 修改：`docs/contracts/filesystem.md` —— 追加变更记录（一个 hook 有多个
  `hooks/<digest>/` 目录，每份实现一个）
- 修改：`docs/contracts/http-machine.md` 的**约定 4**（`:256-273`）—— 修既存漂移，见下

**机器面契约的这一处修正是补登记既有事实，不是本次改造引入的变更**（任务 13 查出，
用户 2026-09-29 决定按代码现状更新文档）。现在约定 4 写「`{file}` 段必须与
`AnswerFileName()` 的返回值**逐字符相等**，handler 层不做模糊匹配——不一致直接 404」，
而 `internal/gateway/answer.go:169-177` 的 `answerFileNameAllowed` 已放行两个第二文件名。

要补的内容：那六个家族的规范文件名表**保留不动**，在其后加一段说明两个例外——
Ubuntu 额外放行 `meta-data`（cloud-init 的 NoCloud 数据源要求同目录两份都在）、
ESXi 额外放行 `ks.cfg`（`boot.cfg` 的 `kernelopt` 行里 `ks=` 指向它）。并写明两份文件
各自的归属：`user-data` 与 ESXi 的 `ks.cfg` 是**用户模板正文**，`meta-data` 与
`boot.cfg` 是**系统生成**。放行是白名单而非放宽——非 ESXi 家族请求 `ks.cfg` 仍然 404
（`answer_test.go:336-357` 锁住了这条）。

- [ ] **红：** 无红灯（纯文档）。先列出设计 §11.1 那张表的 13 行加上面这条机器面修正，
      逐行打勾确认改到。

- [ ] **绿：** 逐节改写。`§5.2` 的「阶段不是排序，是能力边界」**保留为文档指引**（告诉
      模板作者各家族的执行环境在哪儿、`%post` 里哪些命令必然失败），但不再是代码约束——
      这一条要写清楚，否则后来者会以为它被删了。

- [ ] **重构 + commit：** `grep -n "artifact_set_id\|os_families\|answer_template\|channel\|镜像直写" SPEC.md`
      应只剩变更记录里的历史提及。机器面那处修正**单独一个 commit**（它是既存漂移的
      补登记，与本次改造无关，混在一起会让将来 blame 时看不清）：
      `docs(contract): 约定 4 补登记 Ubuntu 与 ESXi 的第二应答文件名`。
      其余 commit（`docs(spec): 同步四层解耦与占位符体系`）。

**风险：** SPEC 里代码注释引用的小节号（`SPEC §4.6`、`§3.4` 这类）散布全仓。改小节号
会让那些引用失效——**只改小节内容，不重排小节号**。新增的两节追加在末尾或用小数号
（如 §5.6），不要插在中间。

---

## 整体验收

全部任务完成后逐条确认。

### 构建与测试

- [ ] `export LOBER_TEST_PG_DSN='postgres://lober_test:lober_test_pw@localhost:5432/lober_test?sslmode=disable'`
- [ ] `make check` 四步全绿（fmt → vet → test → race），**零失败**。基线本身就是零失败
      （2026-09-29 实测），所以任何失败都是本次改动引入的。
- [ ] 三个 `TestInspect_*`（在保留不动的 `inspect.go` 里）仍然通过。
- [ ] `go test ./internal/archtest/ -count=1` 绿 —— 依赖方向守卫（含测试 import）。
- [ ] `go run ./cmd/lober -config deploy/lober.yaml.example -check` 通过启动自检。

### 规格逐条落地

- [ ] **需求 2/3/9（ISO 独立、模板不指定 ISO）**：`grep -rn "ArtifactSetID" --include="*.go" internal/core/model/`
      只在 `Task` 上命中，`Template` 上无。
- [ ] **需求 4（ISO 选族）**：制品集有 `Family` 字段，`FamilyConsistent` 有测试。
- [ ] **需求 5（模板可选族）**：模板有 `Family`，空族 + 空正文是合法草稿有测试。
- [ ] **需求 6（hook 数量不限、不拘泥四阶段）**：`grep -rn "HookStage" --include="*.go" internal/`
      为空；`StageNames` 对含 7 个 `{{stage:}}` 的正文返回 7 个名字有测试。
- [ ] **需求 7（一个 hook 一个功能）**：`Hook.Impls` 存在，`Hook.Body` 不存在。
- [ ] **需求 8（空模板）**：seed 之外另有一个「空模板可存为 draft、不可 active」的测试。
- [ ] **需求 10（两模板内容相同允许）**：有一个「两个不同名模板持相同正文，均可保存并
      激活」的测试。
- [ ] **需求 11（派生覆盖）**：`DerivedFrom` 字段往返有测试。派生**动作**在 spec B。
- [ ] **需求 12（自带三族模板）**：`internal/seed/templates/` 下 3 个 YAML，导入后库里
      3 个 draft 模板，linux 那份含 4 份正文；`internal/seed/hooks/` 下 1 个分区 hook。
- [ ] **固件差异（§14.1）**：rhel 骨架正文含 `%include` 且不含 `part biosboot` /
      `part /boot/efi`；自带分区 hook 的 rhel 实现含 `/sys/firmware/efi` 判定。
      其余五家的骨架正文**不含**任何固件条件——它们的安装器自己处理。

### 正确性锚点

- [ ] `go test ./internal/render/ -run TestGolden -count=1` 绿。
- [ ] **A 档 5 个 golden 零 diff**：`git diff --stat internal/render/testdata/` 的输出里
      **不含** `preseed_full`、`autoinstall_full`、`autoyast_full`、`unattend_full`、
      `esxi_kickstart_full` 任何一个。这是「改造前后逐字节等价」的证据。
- [ ] **B/C 档的 diff 已人工审阅**：五个 `_minimal` 的 diff **只有**新增的空段落
      （每段只含 ClientLib 与信号上报，无业务内容）；`kickstart_*` 的 diff 符合 by-hook
      的预期形态（分区行换成 `%include`）。在 commit 信息里写明审阅结论。
- [ ] `menu_*.golden` 三个与 `esxi_bootcfg.golden`、`meta_data.golden` 零 diff —— 这 5 个
      不参与本次改造（§14 的「参与反推的 8 个」）。
- [ ] `bash scripts/e2e.sh` 九阶段全绿（RHEL 基线，走骨架模板而非硬编码）。
- [ ] `bash scripts/e2e-debian.sh` 九阶段全绿。
- [ ] 真机 **RHEL 系（必须项，不可用 Debian 替代）**：QEMU 起一台目标机，用骨架模板派生的
      模板装一次，状态机走到 `done`，且能用注入的 SSH 公钥登入。**这是唯一的最终判据**，
      API 级断言只是辅助手段。
      rhel 是 C 档（无逐字节护栏，§15），它的分区走 by-hook + shell 判固件这条新路径，
      只有真机能验证。装完登进去 `lsblk` 确认分区表符合预期（UEFI 有 `/boot/efi`，
      BIOS 有 1MiB 的 biosboot）。
- [ ] 真机 Debian：同上再装一次（A 档有护栏，但 preseed 的 `late_command` 拼接是
      `a570ad5` 那个缺陷的原发位置，值得真机复核一次）。

### 不该变的东西没变

- [ ] `git diff docs/contracts/http-machine.md` 为空（除任务 13 另有结论并已单独上报）。
- [ ] 机器面响应仍是纯文本不套 envelope：手工 `curl` 一次 `/boot.ipxe` 与 `/v1/boot`，
      确认响应体没有 `{"code":0` 前缀。
- [ ] 引导菜单未受影响：`go test ./internal/render/ -run TestMenu -count=1` 绿，且
      `menu_*.golden` 三个文件无 diff。
- [ ] 状态机的「`rebooting`/`initializing` 失败禁止重装」未被削弱：
      `go test ./internal/statemachine/ -count=1` 全绿（本计划不该碰它，若有失败说明
      误改了状态机）。
- [ ] `grep -rn "time.Now()" --include="*.go" internal/render/ internal/core/model/ | grep -v _test`
      为空 —— 时间仍全部来自注入的 `Clock`。

### 镜像直写删除干净

- [ ] `grep -rn "DeliveryChannel\|ImageArtifact\|PartitionLayout\|image-write\|ChannelImageWrite" --include="*.go" internal/ cmd/`
      为空。
- [ ] `ls pipeline/ 2>/dev/null` 为空（目录已删）。
- [ ] `internal/imports/iso9660.go` 的 `rockRidgeSignatureNM` **仍然存在**（不是签名
      相关，不该被误删）。

