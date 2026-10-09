# 重写第 3 层：渲染与内置内容 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 把快照渲染成机器要的文件，并随程序带上七个内置模板与五个内置 hook。

**架构：** 两个新包。渲染是纯函数：输入快照加本次请求的系统值，输出四类文件之一（引导菜单、应答文件、附属文件、某个执行插槽的运行脚本），同样的输入逐字节产出同样的结果（C35）。内置内容是数据加装载入口，正确性靠渲染的 golden 守着。两者都只依赖层 1，不读库、不碰时钟、不做文件与网络 IO（spec §6.1）。

**技术栈：** Go，标准库。`//go:embed` 带内置内容的正文。golden 测试。内置内容的升级路径用例连测试环境的 PostgreSQL（经 `pgstore/pgtest`，spec §6.4）。

**依据：**
- spec `docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`：§2.1（格式决定四件事）、§2.4–§2.6（片段与脚本、占位符、受限类型）、§2.8（内置内容与七种格式的表）、§4.3（本层定义）、§6.1（依赖方向与不读库）、§6.2 第 2、3、5、11 条、§7.2 第 3 条（出口判据）、§7.3（验收介质表）、附录 A 归渲染层的 14 条回归用例、§8（Ubuntu 内存、Agama 用 Jsonnet、Windows SSH 公钥只保证 2025）
- 第 1 层计划 `plans/2026-09-30-rewrite-layer1-core-model.md`「留给后续层的约定」六条，第 22 行（catalog 要加的三份数据），第 366 行（EnvName 是唯一约定）
- 第 2 层计划 `plans/2026-10-03-rewrite-layer2-store.md` 第 514 行（内置名字前缀与升级路径用例）
- 两份遗留问题清单归第 3 层的条目：P-2、T6-3 的 B、T6-6、T8-4、T8-8、L-8、T4-3、S1-6、S2-12、S2-13、S2-22、S5-1
- 旧系统的渲染产出：`git show main:internal/render/testdata/`，16 份 `.golden`

---

## 范围

本层要做的：渲染（四类输出）、七个内置模板、五个内置 hook、catalog 要补的三份数据、守卫扩到全模块。

推迟到第一个用到它的层：
- 网关怎么决定给不给某个文件（看下发清单与任务状态）——第 7 层（spec §4.7）
- 预览接口与示例值的标注——第 5 层（spec §5.2 第 4 步）
- 装配点调 `EnsureBuiltin*` 装载内置内容——第 5 层（spec §7.4）
- 按程序当前带的内置清单过滤向导里的旧身份——第 5 层（第 2 层计划第 523 行）

推迟是安全的：渲染是纯函数，调用方只需要凑齐快照和系统值；内置内容是数据，装载入口的契约已由第 2 层定好。

不在本层：L-8 的全局词表与 `Subject.Kind` 常量（归第 5 层）、S3-10（挂在全局词表上）、T4-3 的另一条路（第 7 层真装后把 windows-desktop 限为 uefi）。

## 已定的决定

| 决定 | 由谁定 |
|---|---|
| 重名插槽回第 1 层让 `snapshot.Build` 拒绝，渲染可以信「正文里的插槽名与快照一一对应」 | 用户，2026-10-09 |
| P-2 选 A：`Build` 补报 `slot_timing_unsupported`，成为渲染可以依赖的第四条不变式 | 用户，2026-10-09 |
| T6-3 选 B：拒收以 `-` 或 `@` 开头的 word/enum 取值 | 用户，2026-10-09 |
| 守卫改成全模块列包，不属于任何已知层的包报违规 | 用户，2026-10-09 |
| 按旧系统有无 golden 分批：先做有旧版可对照的，无旧版的放在后面 | 用户，2026-10-09 |
| S2-22 选 ①：给每个内置版本记一份序列化 golden 指纹，不改第 2 层的比较方式 | 用户，2026-10-09 |
| 渲染不自己遍历「模板→插槽→实例→hook→实现」，只做「正文→插槽」这一段 | 本计划 |
| 四类输出共用一个入口，按请求的文件种类分支，不是四个独立函数 | 本计划 |
| 内置内容的正文用 `//go:embed` 带进来，不写成 Go 字符串字面量 | 本计划 |

## 包与文件

| 包 | 职责 | 可以依赖 |
|---|---|---|
| `internal/render` | 渲染：快照加系统值 → 一个文件的字节 | 标准库白名单、`internal/core/...` |
| `internal/builtin` | 七个内置模板与五个内置 hook 的数据，以及取出它们的入口 | 标准库白名单、`embed`、`internal/core/...` |

为什么这样切：

- 渲染与内置内容分成两个包，因为依赖方向是单向的：内置内容不 import 渲染（spec §6.1 严格读下来，内置内容的非测试代码也不能 import 渲染包）。golden 测试在测试文件里，守卫不查，所以 `internal/builtin` 的测试可以 import `internal/render`。
- 渲染包内按输出种类分文件（菜单、应答文件、附属文件、运行脚本各一个），共用的占位符展开与行处理另成一个文件。四类输出的差异在于取哪些系统值、按什么格式拼，共性在于占位符展开，这样切让共性只有一份。
- 内置内容按格式分目录放正文（`internal/builtin/kickstart/`…），因为七种格式的正文互不相干，而且 `//go:embed` 按目录取文件最自然。
- 内置 hook 的实现按「hook × 格式」是一张稀疏表（同一个 hook 在不同格式里是片段还是脚本都可能不同，spec §2.4 第 80 行），所以按 hook 分文件、每个文件里列出它在各格式的实现，而不是按格式分。

## 渲染契约

### 输入

渲染的输入是 `model.Snapshot` 加一组系统值。快照已经摊平到实例级：`Slots[].Instances[]` 带着 Kind、Body、Params、Interpreter、TimeoutSec、Required、Digest，快照里没有 hooks（`model/task.go:78-80`）。所以渲染不查 hook、不选实现、不碰 `ImplFor`。

渲染可以依赖 `snapshot.Build` 守住的不变式（第 1 层计划第 753 行，本层新增两条）：

1. 执行插槽里只有带摘要的脚本
2. 片段插槽里只有片段
3. Family 与 Format 非空
4. 执行插槽的时机该格式支持（本层新增，P-2 选 A）
5. 正文里的插槽名与 `Slots` 一一对应，没有重名（本层新增）

要依赖更多前提，先回到第 1 层让 `Build` 守住它，不在渲染里另查。

### 系统值

系统值由调用方按本次请求填入，渲染不自己构造地址。现有词表在 `catalog/timing.go:67-75`：repo、iso、smb、gateway、task、sn、lbrace。本层要补 token 与 syslog 地址（任务 2）。

### 展开规则

这几条是第 1 层已经按它校验的，渲染必须照做（第 1 层计划第 746-753 行、第 1 层清单 T8-4）：

- 执行插槽和系统部件各展开成一行，可以写在行内。这一行里不能有引号、`<>&`、反斜杠、`#`，也不能有「冒号加空格」。
- 片段插槽按实例顺序依次展开各片段，必须独占一行，展开出的每一行都套用这一行的前导空白。
- 列表参数默认逐行重复所在的整行；`|join` 以单个空格连接后内联。
- 接受 `名字.子值|join`。
- 同一个列表参数在一行里出现几次、或取它的几个子值，只算一次逐行重复，每份抄本里各处代入同一项，不嵌套展开。
- 带执行插槽或系统部件的行一定不会逐行重复（激活判定已拦）。
- 脚本只经 `model.EnvName` 给出的环境变量拿参数，列表以换行连接。
- 其他 `{{…}}`（如 cloud-init 的 Jinja）原样保留；lober 命名空间下写错的占位符留在原文段里原样输出（`scan.go:64` 保证各段 Raw 拼起来与输入逐字节相同）。

### 输出

| 种类 | 内容 | 由谁取 |
|---|---|---|
| 引导菜单 | iPXE 菜单项，按系出 | 引导链 |
| 应答文件 | 该格式的主应答文件 | 安装器 |
| 附属文件 | 格式要的其余文件（Ubuntu 的 meta-data、ESXi 的 boot.cfg、Windows 的 WinPE 启动脚本） | 安装器 |
| 运行脚本 | 某个执行插槽展开出的脚本，装机时由插槽那一行命令取回 | 目标机 |

hook 脚本不经渲染：网关按快照里的摘要从仓储取出正文直接下发（spec §6.2 第 5 条）。

## 全局约定

- 渲染的函数一律是纯函数：不读库、不碰时钟、不做文件与网络 IO，入参由调用方凑齐（spec §6.1）。
- 中文说明只写在 `codes.go`，渲染的错误用第 1 层的原因代码体系，需要新代码时照例追加到 `extraCodes` 末尾。
- 渲染的产出逐字节确定：map 要排序后遍历，不依赖 Go map 的迭代顺序。
- 内置内容的 ID 固定（`builtin-kickstart` 这类，合 `model.ValidID`），名字必须以 `lober:` 开头（C12）。
- 每个任务只留一个提交，评审修正并入时压成一个。

## 并行执行

本地克隆放在 `.tmp/wf/r<N>`。评审后压成一个提交，快进并入。

依赖图：任务 1、2、3 可并行（都改第 1 层与守卫）；4 依赖 1、2；5 依赖 4；6、7、8 依赖 5；9 依赖 6、7、8；10 依赖 9。

规则：
- 只改本任务「文件」一节列出的文件。
- 新原因代码由本任务登记，追加到 `extraCodes` 末尾，并同步 `codes_align_test` 的断言。
- 未实现的输出种类返回 `errNotImplemented` 占位，让后续任务逐个接上。

---

### 任务 1：第 1 层补三条不变式与取值函数

**目标：** 让渲染能依赖「插槽名一一对应」「时机该格式支持」，并把取值规则收成一份。（重名插槽、P-2、T6-6）

**文件：**
- 修改：`internal/core/snapshot/build.go` —— 补两条检查
- 修改：`internal/core/model/param.go` —— 加 `ParamType.SubType`；`IsSafeWord` 拒收以 `-` 或 `@` 开头（T6-3 选 B）
- 修改：`internal/core/model/ident_test.go` —— 给 ValidID 的「合它的都是安全词」补逐字符断言（S1-6）
- 修改：`internal/core/model/task.go` —— 给 `SnapshotValue` 加三个取值方法
- 修改：`internal/core/model/codes.go` —— 登记新原因代码
- 修改：`internal/core/params/params.go` —— `subOK` 改用 `SubType`
- 测试：`internal/core/snapshot/build_test.go`、`internal/core/model/param_test.go`、`internal/core/model/task_test.go`

**接口契约：**

```go
func (t ParamType) SubType(name string) (ParamType, bool)
func (v SnapshotValue) Sub(name string) (SnapshotValue, bool)
func (v SnapshotValue) Join() string
func (v SnapshotValue) EnvValue() string
```

- `SubType` 从 `cidrAccessors` 派生，`params.subOK` 改调它，取值规则因此只有一份（T6-6）
- `Sub` 取子值（`addr`、`netmask` 这类）；`Join` 以单个空格连接；`EnvValue` 以换行连接（脚本用）
- `Build` 新增两条检查：正文里出现重名插槽报 `slot_duplicate`；执行插槽的时机该格式不支持报 `slot_timing_unsupported`
- 两条都在遍历之前查，和现有的 `series_not_ticked` 一样的位置

**覆盖清单：**
- 重名插槽：同名同类、同名不同类（片段插槽后跟同名执行插槽，即 `build_test.go:845` 现有那一例要改成期望报错）、三个同名
- 时机不支持：unattend 正文写 `{{slot:x@post-target}}`；每种格式各取一个它不支持的时机
- `SubType`：CIDR 的各子值、不支持子值的类型、名字写错
- 三个取值方法：单值、列表、空列表、列表里含空串
- `IsSafeWord` 拒收以 `-` 或 `@` 开头（T6-3 选 B）：两种开头各一例；确认没有合理取值被误杀——int 的负数要单独确认，它本来就以 `-` 开头，取值类型不是 word/enum 时不该受这条限制
- ValidID 与 IsSafeWord 的子集关系（S1-6）：对每个字符拼「字符+a」与「a+字符」，凡 ValidID 判真的同时断言 IsSafeWord 为真。ValidID 本来就拒收首字符 `-`、也不收 `@`，所以只收紧首字符时这条保证仍成立；若将来对结尾字符另加限制，这条断言会先红

- [ ] **红：** 为上述覆盖清单写失败测试。`go -C .tmp/wf/r1 test ./internal/core/... -count=1`，预期 FAIL（新方法未定义、`Build` 不报新代码）。
- [ ] **绿：** 实现到上述测试通过。不要动渲染（还不存在）。预期 PASS。
- [ ] **重构 + commit：** `params.subOK` 的旧实现删掉，确认 `make check` 全绿后 commit（`feat(core): 快照构建补两条不变式，model 提供子值与连接取值`）。

**风险：** `build_test.go:845` 现有用例构造的正是重名插槽且 `Build` 成功，改成期望报错会让那一例反向。要确认没有别的测试依赖「重名插槽能过」。

### 任务 2：catalog 补三份机器面数据

**目标：** 把下发文件清单、机器面地址格式、制品目录布局放进 catalog，渲染据此拼地址。（第 1 层计划第 22 行、spec §6.2 第 3 条）

**文件：**
- 创建：`internal/core/catalog/delivery.go` —— 下发文件清单（格式 → 要下发哪些文件）
- 创建：`internal/core/catalog/address.go` —— 机器面地址格式与制品目录布局
- 修改：`internal/core/catalog/timing.go` —— 系统值词表补 token 与 syslog 地址
- 测试：`internal/core/catalog/delivery_test.go`、`address_test.go`

**接口契约：**

```go
func DeliveryFiles(f Format) []FileKind
func AnswerURL(base, task, token, file string) string
func ArtifactPath(isoID string, role string) string
```

- `FileKind` 是带类型的常量（`FileMenu`、`FileAnswer`、`FileAux`、`FileScript`），不用字符串字面量（L-8）
- 地址格式照 spec §8.1：`GET /v1/answer/{task}/{token}/{file}`，token 在路径段里，不用 query string（§4.4）
- 制品目录布局：ISO 记录只存相对路径，渲染据此拼出完整地址；版本已由 `ISO.Validate` 限成 `^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$`，所以不会目录穿越（L-6）
- 七种格式各自的下发清单照 spec §2.1 与旧 SPEC.md 的菜单形态（`git show 7355e6a:SPEC.md` §4.5）

**覆盖清单：**
- 七种格式各自的下发清单，逐一列出
- 地址拼接：正常、task 或 token 含需转义的字符、file 带子路径
- 制品路径：各角色、相对路径的边界
- 能力表自检：`DeliveryFiles` 覆盖七种格式，没有遗漏也没有多余（照 `T8-8` 的做法）

- [ ] **红：** 为覆盖清单写失败测试。`go -C .tmp/wf/r2 test ./internal/core/catalog/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(catalog): 补下发文件清单、机器面地址格式与制品目录布局`）。

**风险：** 旧 SPEC.md 只给了 RHEL 系、Ubuntu、Debian、Windows、ESXi 的菜单形态，SUSE 两种（AutoYaST、Agama）没有。这两种的下发清单要自己定，并在计划外的 spec 里补记——本任务只按已知的写，SUSE 那两格标注依据不足，由任务 8 真写内置内容时定。

### 任务 3：守卫改成全模块列包

**目标：** 让守卫覆盖整个模块，渲染与内置内容的包按「只依赖层 1」检查。（S2-12、S2-13）

**文件：**
- 修改：`internal/archtest/deps_test.go` —— 全模块列包；抽出时钟检查；加第 3 层规则
- 修改：`internal/archtest/guard_test.go` —— 补正反例
- 修改：`internal/store/doc.go` —— 「仓储不读时钟」这条全局约定现在有守卫了，注释里写明（S2-13）

**做法：**

- 遍历范围限在 `internal/` 和以后的 `cmd/`，不碰 `web/`（`node_modules` 很大）
- 不属于任何已知层的包报违规，这样包放错目录查得出来
- 第三方限制从 `storeRule` 提到全模块：只有 pgstore 能用 gorm 与 pgx，pgtest 能用 pgx
- 第 3 层两个包的规则：只许标准库白名单与层 1，不许 store/pgstore，不许第三方；`internal/builtin` 另许 `embed`
- 时钟检查从 `checkFile` 第 396-437 行抽成共用函数，同时套到存储层（S2-13）与第 3 层。规则只禁 `clockFuncs` 九个函数，`time.Duration` 照常放行
- 标准库白名单按渲染实际要用的扩充（评审点：`bytes`、`strings`、`sort`、`path`、`embed`）

- [ ] **红：** 先写新规则的用例（第 3 层包 import store 要报、import 第三方要报、import 层 1 不报；不属于任何层的包要报；存储层 import 时钟函数要报）。`go -C .tmp/wf/r3 test ./internal/archtest/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现。此时 `internal/render` 与 `internal/builtin` 还不存在，规则先靠合成的源码测。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`test(archtest): 守卫改成全模块列包，时钟检查套到存储层与第 3 层`）。

**风险：** `layerSourceDirs` 要覆盖新目录，否则新增包后 `go test` 会复用旧的 PASS。常量与引用它的用例必须在同一次改动里一起加，否则 archtest 编不过（第 2 层删 memstore 时踩过）。

### 任务 4：渲染骨架与占位符展开

**目标：** 把快照和系统值展开成文本，四类输出共用这一层。（spec §2.5、第 1 层计划的展开约定）

**文件：**
- 创建：`internal/render/render.go` —— 入口与种类分支
- 创建：`internal/render/expand.go` —— 占位符展开与行处理
- 创建：`internal/render/doc.go` —— 包说明：纯函数、依赖哪五条不变式
- 测试：`internal/render/expand_test.go`

**接口契约：**

```go
type Request struct {
    Snapshot model.Snapshot
    Values   map[catalog.SystemValue]string // 本次请求的系统值
    Kind     catalog.FileKind
    Slot     string // Kind 为 FileScript 时指定哪个执行插槽
}

func Render(r Request) ([]byte, []model.Problem, error)
```

- 同样的 `Request` 逐字节产出同样的 `[]byte`（C35）
- 入参不合前提（系统值缺项、`Slot` 指的插槽不存在或不是执行插槽）返回 `Problem` 列表，不返回半成品
- `error` 只用于调用方用错接口（比如 `Kind` 不认识），内容问题一律走 `Problem`
- 展开按「展开规则」一节的八条做
- 占位符经 `scan.Scan` 的 Segments 取，不自己认词法（避免抄 `scan` 的第四份）

**覆盖清单：**
- 四类占位符各自的展开：系统值、参数引用、片段插槽、执行插槽、系统部件
- 行处理：片段插槽独占一行且套用前导空白（多行片段、空片段）、执行插槽展开成一行且字符受限
- 列表：默认逐行重复、`|join` 内联、`名字.子值|join`、一行里同一列表出现多次只重复一次、取几个子值只重复一次
- 原样保留：cloud-init 的 Jinja、lober 命名空间下写错的占位符
- 确定性：同一个 `Request` 连渲染两次逐字节相同；含 map 的输入（Params、Artifacts）顺序稳定

- [ ] **红：** 为覆盖清单写失败测试，用手写的小快照当输入（不依赖内置内容，它还不存在）。`go -C .tmp/wf/r4 test ./internal/render/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现展开与行处理；四类输出的具体拼装返回 `errNotImplemented`，留给任务 6、7、8。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(render): 渲染骨架与占位符展开`）。

**风险：** 列表逐行重复要知道行的边界，而 `scan` 的原文段会跨行（`flushText`，`scan.go:325-331`），Segment 只记开头的行列。按 `\n` 切行的规则要和 `scan.go:58-61` 一致（CRLF 的 `\r` 留在行尾）。如果这里要第二份切行逻辑，优先让 `scan` 导出按行的视图，别在渲染里另写。

### 任务 5：内置内容的骨架与装载

**目标：** 把内置内容的数据结构、ID 与名字规则、取出入口定下来，先放一个最小的内置模板跑通装载。（spec §2.8、§6.2 第 11 条、S2-22 选 ①）

**文件：**
- 创建：`internal/builtin/builtin.go` —— 取出入口与清单
- 创建：`internal/builtin/doc.go` —— 包说明：ID 与名字规则、版本怎么升、golden 指纹的意义
- 创建：`internal/builtin/kickstart/` —— 第一个内置模板的正文与 hook 实现（`//go:embed`）
- 测试：`internal/builtin/builtin_test.go`、`internal/builtin/fingerprint_test.go`

**接口契约：**

```go
func Templates() []model.Template
func Hooks() []model.Hook
func Fingerprint(v any) string // 序列化后的指纹，golden 用
```

- 每个内置模板与 hook 的 ID 固定（`builtin-kickstart` 这类），名字以 `lober:` 开头
- 返回的都是深拷贝，调用方改它不影响下一次调用
- 版本号随 lober 版本递增，改了内容必须升版本号——`Fingerprint` 的 golden 就是为了逼出这一步（S2-22 ①）
- 内置内容只引用内置 hook（内置模板引用用户的 hook 不可能，用户 ID 由 `NewID` 随机生成）

**覆盖清单：**
- 每个内置模板与 hook 过 `activation.Template`/`activation.Hook`——`EnsureBuiltin*` 不跑激活判定（`check_authoring.go:220-262` 只查元信息、名字与文本），所以这一关必须在本层的测试里把住
- ID 合 `model.ValidID`；名字合 `lober:` 前缀；同一份内容两次取出相等
- 指纹 golden：七个模板与五个 hook 各一份
- 升级路径：把发布过的每个内置版本依次装进同一个库，证明没被改名或改内容破坏（第 2 层计划第 514 行）。这一条连真实 PostgreSQL，经 `pgstore/pgtest`

- [ ] **红：** 写上述测试，内置内容先只放 kickstart 一个模板和它用到的 hook。`go -C .tmp/wf/r5 test ./internal/builtin/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现取出入口与第一份内置内容。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(builtin): 内置内容的骨架、指纹 golden 与装载用例`）。

**风险：** 升级路径用例要连库，所以 `internal/builtin` 的测试文件会 import `pgstore/pgtest`。测试文件不查 import，守卫不拦，但要在包说明里写明为什么只有测试碰库。另外「发布过的版本」现在只有一个，这条用例的价值要等真有第二个版本才显现——先把骨架搭好。

### 任务 6：有旧版 golden 的应答文件

**目标：** 渲染出 kickstart、preseed、autoinstall、autoyast、unattend 五种应答文件，与旧系统的产出逐行对照。（决定「按旧系统有无 golden 分批」）

**文件：**
- 创建：`internal/render/answer.go` —— 应答文件的拼装
- 创建：`internal/render/testdata/` —— golden
- 测试：`internal/render/answer_test.go`

**做法：**

旧系统的产出在 `git show main:internal/render/testdata/`，这五种各有 `full` 与 `minimal` 两份。做法是：
1. 取出旧 golden 当参照，不直接当期望值——新系统的快照结构与旧系统不同。
2. 新 golden 与旧产出逐行对照，差异逐条写明理由（spec §7.2 第 3 条明确要求）。理由写在 `testdata` 旁的一份说明文件里。
3. BIOS 与 UEFI 分开（spec §7.2 第 3 条）。
4. 附录 A 归渲染层的回归用例里，与这五种有关的要各有一条用例钉住：`743e8af`（内置 kickstart 声明安装源）、`9a252db`（菜单与内置 kickstart 用非交互模式）、`2d59bbb`（`%include` 不能排在 `%pre` 之前）、`83584d7`（分区 hook 的 golden 分 BIOS 与 UEFI，BIOS 加 GPT 要有 biosboot）。

**覆盖清单：**
- 五种格式 × （full、minimal） × （BIOS、UEFI）
- 每种格式的必需系统部件都在（照 `catalog/timing.go:87-158` 的能力表）
- 渲染出的每个地址符合机器面地址格式（spec §7.2 第 3 条点名要这条测试）
- 执行插槽展开后只有一行（同上）

- [ ] **红：** 写 golden 测试，`testdata` 先留空。`go -C .tmp/wf/r6 test ./internal/render/ -count=1`，预期 FAIL（golden 不存在）。
- [ ] **绿：** 实现五种应答文件的拼装，生成 golden，逐行比对旧产出并写差异说明。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(render): 五种有旧版可对照的应答文件`）。

**风险：** 旧系统的 golden 是旧快照结构的产出，逐行对照时会有大量「结构变了」的差异，容易掩盖真正的回归。对照时先把差异分成三类：结构变化（预期）、修过的 bug（预期，指向附录 A 的提交）、无法解释（必须查清）。第三类一条都不能留。

### 任务 7：引导菜单与有旧版的附属文件

**目标：** 渲染出三种有旧版 golden 的菜单，以及 ESXi 的 boot.cfg 与 Ubuntu 的 meta-data。（同上）

**文件：**
- 创建：`internal/render/menu.go` —— 引导菜单
- 创建：`internal/render/aux.go` —— 附属文件
- 测试：`internal/render/menu_test.go`、`aux_test.go`

**做法：**

旧系统有 `menu_debian`、`menu_rocky`、`menu_ubuntu` 三份菜单 golden，以及 `esxi_bootcfg`、`meta_data`。菜单形态照旧 SPEC.md §4.5：RHEL 系 `inst.ks=…/ks.cfg`；Ubuntu `ds=nocloud-net;s=…/`（目录下要有 user-data 与 meta-data）；Debian `preseed/url=…/preseed.cfg`；ESXi `mboot.efi -c …/boot.cfg`，只走 UEFI。

附录 A 与菜单有关的回归用例：`4e32ed9`、`99b6ca7`、`75bdc20`、`686afcd`、`a59a0bb`、`7eb805e`、`7d38246`、`23442b1`、`a570ad5`、`2ab744d` 里凡是讲菜单与引导的，各有一条用例钉住。

**覆盖清单：**
- 三种有旧版的菜单 × （BIOS、UEFI），ESXi 只有 UEFI
- Ubuntu 菜单必须带 `url=` 参数（spec §2.2 第 64 行明说旧系统缺这个参数，那条路径装不通）
- meta-data 与 user-data 落在同一个目录下，地址与菜单里的 `s=` 一致
- 地址都符合机器面地址格式

- [ ] **红：** 写 golden 测试。`go -C .tmp/wf/r7 test ./internal/render/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(render): 三种菜单与 ESXi、Ubuntu 的附属文件`）。

**风险：** Ubuntu 的 `url=` 要指向整张 ISO，所以制品目录布局里必须有「原始 ISO」这个角色（任务 2 的 `ArtifactPath`）。如果任务 2 没定这个角色，这里会卡住——先核对。

### 任务 8：无旧版可对照的格式

**目标：** 补上 Agama、SUSE 两种与 Windows、ESXi 的菜单，以及 WinPE 启动脚本与运行脚本。（同上）

**文件：**
- 修改：`internal/render/menu.go`、`aux.go`、`answer.go`
- 创建：`internal/render/script.go` —— 执行插槽的运行脚本
- 测试：对应的 `_test.go`

**做法：**

这几样旧系统没有产出可对照，所以 golden 只能靠 spec 与各安装器的文档立：
- Agama 的应答文件（spec §8 建议正文用 Jsonnet）
- AutoYaST 与 Agama 的菜单（旧 SPEC.md 没给形态，要自己定并补记进 spec）
- Windows 菜单走 wimboot，加载 `boot/bcd`、`boot/boot.sdi`、`sources/boot.wim`（spec §2.2 第 65 行标注「待真机核实」）
- WinPE 启动脚本（`startnet.cmd`）去取 unattend
- 运行脚本：执行插槽展开出的脚本，用目标环境的 shell 做语法检查（spec §7.2 第 3 条要求 busybox sh、bash）

**覆盖清单：**
- Agama 应答文件、AutoYaST 与 Agama 菜单、Windows 菜单、WinPE 启动脚本各一份 golden
- 运行脚本：片段与脚本混排、多个执行插槽、脚本的环境变量按 `EnvName` 导出、列表以换行连接
- 运行脚本过 shell 语法检查（busybox sh 与 bash 各一遍）
- Windows 的 SSH 公钥 hook 只保证 2025（spec §8），其余版本标注未验证

- [ ] **红：** 写 golden 与语法检查测试。`go -C .tmp/wf/r8 test ./internal/render/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(render): 补齐无旧版可对照的格式与运行脚本`）。

**风险：** 这一批没有旧产出做参照，错了只有真装机能发现。所以 golden 的每一行都要能指到 spec 或安装器文档的依据，指不出来的标注「待真机核实」并记进 spec §8 的风险表。Windows 与 Agama 尤其如此。shell 语法检查需要本机有 busybox 与 bash——`make check` 跑在 Windows 上，这一项要能在没有它们时明确失败而不是跳过（照 PostgreSQL 的 DSN 那条规矩）。

### 任务 9：剩下六个内置模板与五个内置 hook

**目标：** 把七种格式的内置模板与五个内置 hook 补全，每个都有 golden 与激活判定。（spec §2.8）

**文件：**
- 创建：`internal/builtin/preseed/`、`autoinstall/`、`autoyast/`、`agama/`、`unattend/`、`esxi/`
- 创建：`internal/builtin/partition.go`、`packages.go`、`network.go`、`account.go`、`sshkey.go` —— 五个内置 hook 在各格式的实现
- 修改：`internal/builtin/builtin.go` —— 清单补全
- 测试：各自的 `_test.go`

**做法：**

每个模板按 spec §2.8 的表勾系：kickstart（RHEL、Rocky、Alma、openEuler、OpenCloudOS）、preseed（Debian）、autoinstall（Ubuntu）、AutoYaST（SLES 15、Leap 15）、Agama（SLES 16、Leap 16）、unattend（Windows Server，2016–2025 按 edition 参数区分）、ESXi ks（ESXi 8、9）。

五个 hook 按格式写实现，同格式的系共用一份，有差异的系单独一份。同一个 hook 在不同格式里可以是不同种类——SSH 公钥在 kickstart 里是 `sshkey` 片段，在 Debian 里是装后脚本（spec §2.4 第 80 行）。

T6-3 选 B 之后，内置模板里的参数仍要按安全词的陷阱写：参数放在 `--` 之后或加引号，unattend 不把参数放进 cmd 与 PowerShell 的参数位置。

**覆盖清单：**
- 七个模板 × （BIOS、UEFI）的 golden，加五个 hook 各自的指纹 golden
- 每个模板过 `activation.Template`，每个 hook 过 `activation.Hook`
- 分区 hook 的 BIOS 分支加 GPT 时要有 biosboot（`83584d7`）
- Ubuntu 的内存要求写进内置模板的说明（spec §8）——`model.Template` 现在没有 Description 字段，要加的话连带 pgstore 五处同步，见风险
- 每个模板的插槽都在正文里出现过，装配表没有多余的键

- [ ] **红：** 写 golden 与激活判定测试。`go -C .tmp/wf/r9 test ./internal/builtin/ ./internal/render/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现六个模板与五个 hook。预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(builtin): 七个内置模板与五个内置 hook`）。

**风险：** spec §8 要求「目标机内存要求写进内置模板的说明」，但 `model.Template` 没有 Description 字段（`authoring.go:121-144`，Hook 有）。加这个字段要同步五处：model 字段、行类型字段、`toTemplateVersionRow`、行转回 model、`templateContentColumns`，还要改 `TemplateTextProblems`、clone 与 golden，而且列清单没有测试兜底（第 2 层清单记过这件事）。两条路：加字段并顺手补一条「列清单覆盖行类型全部可写字段」的测试；或者把内存要求写在模板正文的注释里，不加字段。选哪条在本任务开工时定，写进提交说明。

### 任务 10：同步文档

**目标：** 把本层定下的约定写进 spec 与计划，更新两份清单里归第 3 层的条目。

**文件：**
- 修改：`docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`
- 修改：`docs/superpowers/plans/2026-09-30-rewrite-layer1-core-model.md` —— 交接一节补 T8-4 的三条
- 修改：`docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`、`2026-10-07-layer2-open-issues.md`

**改什么：**

- spec：§2.1 的能力表补 token 与 syslog 地址；§2.2 第 65 行 Windows 那条按真写出来的内容更新，仍待真机核实的标注清楚；§8 风险表补 Agama 与 AutoYaST 菜单形态依据不足、Windows wimboot 待核实；SUSE 两种的下发清单补进 §2.1
- 第 1 层计划交接一节：补 T8-4 的三条（清单当初就建议补，至今没补）
- 第 1 层清单：P-2 标「选 A，已修」；T6-3 的 B 标「选 B，已修」；S1-6 随 T6-3 B 标已修；T6-6、T8-4 标已修；T4-3 记下 unattend 内置内容的决定；T8-8 标已修
- 第 2 层清单：S2-12、S2-13、S2-22 标已修；S5-1 的两件标已修；S3-10 说明仍归第 5 层

- [ ] **红/绿：** 文档任务没有测试。改完用 Grep 核对：清单里归第 3 层的条目都有「结果」行；spec 里没有与本层实现矛盾的说法。
- [ ] **commit：** `docs: 第 3 层落地后同步 spec、计划与两份清单`

---

## 留给后续层的约定

- 第 4 层：ISO 的获取来源加新表时，制品目录布局（任务 2）已经定下相对路径的写法，导入按它写文件。原始 ISO 要作为一个角色发布（Ubuntu 的 `url=` 要用，spec §2.2 第 64 行）。
- 第 5 层：预览调 `render.Render`，与真实下发同一条路（C35）。没指定主机或 ISO 时用示例值并标明是示例；预览的 token 是示例值（spec §6.2 第 2 条）。装配点调 `store.EnsureBuiltin*` 装载 `builtin.Templates()` 与 `builtin.Hooks()`，报 ErrConflict 时重试。按 `builtin` 当前带的清单过滤向导里的旧身份（S5-1）。
- 第 7 层：网关按 `catalog.DeliveryFiles` 与任务状态决定给不给某个文件（spec §4.7）。hook 脚本按快照里的摘要从仓储取出直接下发，不经渲染（spec §6.2 第 5 条）。改能力表时照 T8-8 补三条断言。真装后若把 windows-desktop 限为 uefi，回到第 1 层改固件要求表（T4-3）。
- 任何一层要改渲染的展开规则，先回到第 1 层同步改判定——规则由第 1 层校验、第 3 层执行，两边必须一致（第 1 层计划第 746 行）。

## 整体验收

- [ ] 在主目录跑 `make check`，五步全部通过，退出码为 0。PostgreSQL 的测试真的跑了（内置内容的升级路径用例），没有因为找不到 DSN 而跳过。
- [ ] `go list ./...` 列出的包里有 `internal/render` 与 `internal/builtin`，且守卫覆盖到它们：构造一个 import store 的反例，`go test ./internal/archtest/` 报违规。
- [ ] 七个内置模板各有 golden，BIOS 与 UEFI 分开（spec §7.2 第 3 条）。
- [ ] 新 golden 与旧系统产出的差异逐条写明理由，没有「无法解释」那一类。
- [ ] 渲染出的每个地址都符合机器面地址格式——有一条测试逐个检查（spec §7.2 第 3 条）。
- [ ] 执行插槽展开后只有一行——有一条测试断言（同上）。
- [ ] 运行脚本过 busybox sh 与 bash 的语法检查（同上）。本机没有这两个 shell 时测试明确失败，不跳过。
- [ ] 同一个 `Request` 连渲染两次逐字节相同（C35）。
- [ ] 六条决定每条都能指到一个测试或一处文档。
- [ ] 第 1 层清单的 P-2、T6-3 B、T6-6、T8-4、T8-8、S1-6 都标了结果；第 2 层清单的 S2-12、S2-13、S2-22、S5-1 都标了结果。
- [ ] 既有行为没变：`go test ./internal/core/... ./internal/store/...` 全绿，契约叶子数不少于 262。
