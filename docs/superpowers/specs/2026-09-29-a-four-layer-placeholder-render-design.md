# A 四层模型与占位符渲染设计

日期：2026-09-29
状态：设计定稿，待实现
关联：SPEC §2.1、§2.2、§2.3、§2.6、§2.7、§4.7、§5.1、§5.2、§5.3、§11、§12
取代：`2026-09-28-component-hook-design.md`（commit `49956df`，差异见 §10）
后续：`2026-09-29-b-console-api-wizard-design.md`、`2026-09-29-c-iso-source-design.md`

## 1. 目标与验收判据

把三级抽象（任务 → 模板 → hook）改成四层解耦（任务 →（ISO + 模板）→ hook），
并把应答文件的**全部业务内容**从渲染器硬编码搬进用户可写的模板正文。

**核心转向**：系统不再理解任何应答文件的语法。渲染器从「按家族生成完整应答
文件」退化为「在用户写的正文里做占位符展开」。分区怎么分、装哪些包、locale
取什么值，全部由模板作者写在正文里，系统一个字节都不解析。

### 1.1 验收判据

1. `scripts/e2e.sh`（RHEL 基线九阶段）与 `scripts/e2e-debian.sh` 改造后全绿，
   且走的是**自带骨架模板**而非渲染器硬编码。
2. `internal/render` 六个家族渲染器不再含任何业务字面量（分区方案、locale、
   包集合、镜像源、时区、键盘布局）。判据是 §7.2 那张字面量清单逐条清零。
3. golden 测试的目标产物 **= 改造前的 golden 输出，逐字节相同**。这是本设计
   唯一可信的正确性锚点，理由见 §12 风险 1。

### 1.2 不在本设计范围

| | 理由 |
|---|---|
| 控制台端点与前端向导 | 见 spec B。本设计只改 model / store / render 三层与自带骨架 |
| ISO 来源（上传、远程下载） | 见 spec C。与本设计仅在制品集创建端点相接 |
| 引导菜单渲染（`internal/render/menu.go`） | 菜单是 iPXE 脚本，不是应答文件。格式归系统掌握，用户不写它 |

## 2. 四层关系

```
装机任务 (Task)        选 ISO + 选模板（两个独立选择）
  ├─ artifactSetId     ISO 制品：族 + 家族 + 版本 + 路径 + manifest
  └─ templateId        模板：族 + 每家族一份正文
       └─ 正文          用户自写全文，内含 {{stage:名字}} 占位符
            └─ 阶段     = 正文里 stage 占位符的出现顺序，数量不限
                 └─ hook  每家族一份实现（正文 + 解释器）
```

### 2.1 族与家族两层并存

**族**（`Family`，3 值）是用户创建 ISO 时选的分组标签。**家族**（`OSFamily`，
6 值，已存在）是导入时从 ISO 元数据识别出的格式归属。

| 族 | 允许的家族 | 各家族的应答文件格式 |
|---|---|---|
| `linux` | `rhel` `debian` `ubuntu` `suse` | 四种**互不相通**的格式，见 §5.2 |
| `windows` | `windows` | unattend.xml |
| `esxi` | `esxi` | ESXi 方言 kickstart |

**族不能顶替家族。** `linux` 族里那四家的应答文件不是四种方言，是四种不同
的文件种类——kickstart 是行式加 `%section` 块，preseed 是 debconf 键值，
autoinstall 是 YAML，autoyast 是 XML。没有任何写法能让同一段文本同时合法。
族只承担分组、筛选与模板归属；格式分派、`AnswerFileName`（`render.go:107`）、
`firmwareCompat`（`inventory.go:109`）、`repoIndexPattern`（`inventory.go:162`）
四处继续由家族驱动，一行不改。

**交叉校验**：导入时识别出的家族必须落在用户选的族的允许集合里，否则**拒绝
导入**并置 `FailReason`。选了 `windows` 族却识别出 `rhel`，是运维选错了文件，
静默接受会让后面的一切都对不上。方向与 `FirmwareCompatible` 对未知家族的处置
一致（`inventory.go:122`）：不静默接受。

### 2.2 模板：族 + 每家族一份正文

模板声明**族**（3 值）。正文是一个映射：家族 → `TemplateBody`。`linux` 族的
模板最多 4 份正文，`windows` 与 `esxi` 族各最多 1 份。**允许只填部分家族。**

建任务时按 ISO 的家族取对应那份正文；缺失则拒绝，错误文案给可行下一步
（「模板 tpl-x 未提供 suse 家族的正文，可先补齐该正文或改用其它模板」）。

**空模板**（需求 8）= 族已选、零份正文。零份正文即零个阶段，符合「空模板默认
没有字符内容、没有阶段」。空模板可以存为 `draft`，但不能 `active`——激活校验
要求至少一份正文且该正文的系统占位符齐备（§3.4）。

**两个模板内容一样是允许的**（需求 10）。模板的唯一性只在 `name` 上，正文不
参与去重。这条刻意如此：`almalinux` 与 `rockylinux` 的 kickstart 实际相同，
强制去重会迫使运维把两个业务含义不同的模板合成一个。

### 2.3 hook：一个功能，每家族一份实现

一个 hook 是**一个功能 / 一个 action**（需求 7），内部按家族分多份实现，每份
自带解释器与正文。「装监控 agent」在 `rhel` 下是 `dnf install`，在 `ubuntu` 下
是 `apt install`，在 `windows` 下是 powershell 的 `msiexec`——同一个功能，三份
实现。允许只填部分家族；插入到某家族正文时若缺该家族实现，**拒绝并报出缺哪个
家族**。

**hook 自己不带阶段。** hook 创建时不知道自己将来属于哪个模板，因此也无从知道
阶段。阶段是模板插入 hook 时才选的（需求 6），存在 `HookRef.Stage`（该字段已
存在，语义从「四值枚举」改为「模板正文里的阶段名」）。

`model.Hook.Stage`、`Hook.OSFamilies`、`Hook.Interpreter`、`Hook.Body`、
`Hook.BodySHA256` 五个字段随之删除，`Hook.SupportsFamily`（`authoring.go:87`）
作废。

**声明式 hook 这一类整个消失。** `2026-09-28` 设计把 hook 分成 `fragment`
（声明式，按 outlet 合并进骨架）与 `script`（可执行）两类。正文由用户自写之后，
分区方案、包集合、locale 这些声明式内容直接写在正文里，不再是 hook。剩下的
hook 只有可执行一类，8 个 outlet 中性词表与六套家族 adapter 合并规则一并作废。

### 2.4 任务：选 ISO + 选模板

`model.Task` 已同时持有 `TemplateID` 与 `ArtifactSetID`（`task.go:106-109`），
四层解耦在任务层**不需要新增字段**。`Task.HookDigests` 的语义细化见 §4.5。

### 2.5 派生：快照复制（需求 11）

派生 = 把源模板的族、全部正文、全部 hook 引用复制一份，之后两者**完全独立、
无任何联动**。新增 `DerivedFrom` 一个字段（源模板 ID + 版本），仅供展示与审计，
不参与渲染。

「覆盖 a hook 用 b hook」就是在副本里改一条 hook 引用。不做 `parentID +
overrides + 渲染期合并` 那一套：那需要额外的合并函数、父版本升级流程与冲突
处置，而需求 10 已经明确内容重复是允许的——继承机制要解决的问题本身不存在。

代价要说清：源模板修了一处问题，已派生出去的模板不会跟着变，要各自改一遍。

## 3. 占位符体系

这是本设计唯一的固定结构，共 **3 类前缀**。它不含任何业务语义与家族语义，加一
个家族不需要改词表。

### 3.1 三类占位符

| 语法 | 展开成 | 数量 |
|---|---|---|
| `{{stage:名字}}` | 该阶段的 ClientLib + 全部 hook 调用 + 阶段信号上报 | 任意多个 |
| `{{lober:syslog}}` | 该家族的 syslog 指向指令 | 必需，恰好 1 个 |
| `{{lober:firstboot}}` | firstboot 脚手架（unit + 脚本 + token + 参数） | 必需，恰好 1 个 |
| `{{lober:cleanup}}` | 凭据清理（擦 finalize token 与应答文件副本里的密码哈希） | 必需，恰好 1 个 |
| `{{fact:名字}}` | 该 fact 的渲染期值 | 任意多个 |

阶段名（`{{stage:}}` 的参数）与 fact 名的字符集：字母、数字、连字符、下划线、
中文。**不允许空格与 `}`**——前者让「阶段名写错一个空格」变成一个查不出来的
新阶段，后者会让占位符提前闭合。

### 3.2 为什么是纯字符串替换，不是 text/template

**必须是纯字符串替换，且只识别上表三个确切前缀，绝不贪婪匹配任意 `{{...}}`。**

理由一：**cloud-init 原生支持 Jinja 模板**，`{{ ds.meta_data.local_hostname }}`
是 Ubuntu `user-data` 正文里完全合法的内容（以 `## template: jinja` 开头的
user-data 会由 cloud-init 自己求值）。贪婪匹配 `{{...}}` 会把这类内容吞掉或
报「未知占位符」，而模板作者完全有理由写它。

理由二：现有代码已经在三处刻意避开 text/template 并各自写明理由——
`internal/gateway/bootscript.go:23`（用 `fmt.Sprintf` 做唯一一处替换）、
`internal/render/autoinstall.go:5`（用 `yaml.Marshal`，YAML 缩进敏感）、
`internal/render/autoyast.go:99`（渲染 XML，理由同 `unattend.go`）。引入
text/template 会让用户正文里的 `{{if}}`、`{{range}}`、`{{.}}` 被当成模板语法，
而那些在 XML/YAML 正文里都可能是普通字符。

**转义**：要输出字面量 `{{stage:x}}` 时写 `{{lober:literal}}{{stage:x}}`——前者
展开成空串，把后者的前缀打断。刻意不引入 `{{{{` 这类双写转义：双写在 Jinja
正文里本身有含义，会造成第二层歧义。

### 3.3 `{{stage:名字}}` 的展开规则

展开成三段，顺序固定：

```
1. ClientLib（signal 与 run_hook 两个 shell 函数定义）
2. 该阶段全部 hook 引用，按 HookRef.Order 升序，各展开成一行 run_hook 调用
3. 阶段完成信号上报
```

**ClientLib 随每个 stage 占位符无条件展开一份**，这不是冗余。`kickstart.go` 现在
就注入了四次（97、291、344、371 行），代码注释写明了原因：anaconda 的每个
`%pre` / `%post` 段是**独立执行的脚本**，shell 函数定义不跨段。同理适用于
preseed 的两个 command 键、autoyast 的每个 `<script>`、unattend 的每条
`RunSynchronousCommand`。

这条规则顺带消灭了「模板作者忘记写遥测」这个故障模式：**遥测不是一个可以被删掉
的占位符，它绑在每个阶段上**。用户每定义一个阶段，那个阶段就自动具备上报能力。
这直接继承了 SPEC §5.1 的原意——「系统 hook 作为数据存在时，有权限者可编辑或
删除它，会静默废掉整个遥测」。

### 3.4 三个 `{{lober:}}` 部件占位符

SPEC §5.1 列了四件**物理上只能在目标机器上执行**的事（服务端 provider 跑在服务器
上，无法在目标机器上执行代码）。其中信号函数注入由 §3.3 兜住，剩下三件不是每阶段
的，各需自己的位置：

| 占位符 | 位置要求 | 忘了写的后果 |
|---|---|---|
| `{{lober:syslog}}` | 正文顶层（kickstart 是 `logging --host=` 一行，各家族形态不同） | installer 自身的报错无处可去，`FailureDetail.SyslogTail` 永远为空 |
| `{{lober:firstboot}}` | 能写目标盘的位置（`%post` / `late_command` in-target / `<chrooted>true</chrooted>`） | 任务停在 `initializing` 直到超时 → `init_timeout`，而这是**不可重装**失败态（§3.4） |
| `{{lober:cleanup}}` | 正文里最后一个能写目标盘的位置 | finalize token 留在目标盘上，装机成功、控制台全绿、没有任何地方看得出来 |

**激活时校验三者各恰好出现一次**，缺失或重复都拒绝激活。理由：`cleanup` 缺失是
一个完全静默的凭据泄露，`firstboot` 缺失导向不可重装失败态。两者都属于本项目最近
两周修的六个缺陷的同一类（静默降级导致装机卡住），必须 fail loudly。

**位置正确性无法校验**——系统不解析语法，`{{lober:firstboot}}` 写在 `%pre` 里我们
看不出来。这个残余风险由自带骨架承担：6 份骨架里三个占位符都已在正确位置，派生
出来的模板天然带着（§7）。风险条目见 §12 风险 3。

### 3.5 `{{fact:名字}}` 与现有参数内联的分工

两套机制并存，用途不同，不可互相替代：

| | 时机 | 消费者 | 现有实现 |
|---|---|---|---|
| `{{fact:名字}}` | **渲染期**字符串替换 | 模板正文（分区指令要引用规划出的盘） | 本设计新增 |
| `$LOBER_*` 环境变量 | **运行期**读环境变量 | hook 脚本正文 | `internal/render/params.go`，不改 |

正文里要引用磁盘规划器决策出的主盘，写 `{{fact:disk_primary}}`；hook 脚本里要
同一个值，写 `$LOBER_DISK_PRIMARY`。前者必须是渲染期的——`part / --ondisk=` 是
kickstart 指令，不是 shell，读不了环境变量。

fact 名的可用集合是 `model.FactKeysV1`（SPEC §5.4：fact 命名空间是对外 API，只增
不删）。引用不存在的 fact **报错**，错误文案给出可用前缀提示。

### 3.6 缩进照抄

占位符展开成多行时，**每一行都套用占位符自身所在行的前导空白**。

这是让系统对 YAML 与 XML 保持无知的关键：Ubuntu 的 `user-data` 里
`      - {{stage:装包}}` 展开出的每一行都带 6 个空格，缩进自然正确；autoyast 的
`<source>` 内部同理。系统不需要知道自己在写 YAML 还是 XML，只需要照抄空白。

对占位符独占一行的情况（三个 `{{lober:}}` 与多数 `{{stage:}}`），这条规则等价于
「原地展开」。对占位符前后还有其它内容的情况（如 YAML 列表项的 `- ` 前缀），
第一行保留原有前缀，后续行按前导空白对齐。

### 3.7 未识别占位符与 hook 正文的转义

**未识别的 `{{lober:xxx}}` 报错**（拼错了部件名，静默留在正文里会被 installer 当
普通文本处理，产出的应答文件语法可能仍然合法但行为全错）。**未识别的其它 `{{...}}`
原样保留**（那是 cloud-init Jinja 或用户的其它内容，不归我们管）。

**hook 正文不内联进应答文件。** hook 落盘到 `/artifacts/hooks/<digest>/script.sh`，
正文里只有一行 `run_hook <digest> <timeout>`（SPEC §6.3，`run_hook` 用
`sha256sum -c` 校验下载内容）。

这条是「系统不理解语法」能够成立的前提。若 hook 正文要内联，系统就必须知道正文
格式才能正确转义——多行 shell 折进 preseed 的单个 debconf 值正是 `a570ad5` 那个
缺陷（按行拼分号把 `if` 语句切断，产出非法 shell，pre 阶段一执行就死）。一行命令
在六种格式里都只是一行，转义问题消失。

## 4. 数据模型改动

`internal/core/model` 不得 import 任何项目内包（`archtest/deps_test.go:145` 守卫）。
以下全部是该包内的类型改动。

### 4.1 新增 `model.Family`（族）

```go
type Family string
const (
    FamilyLinux   Family = "linux"
    FamilyWindows Family = "windows"
    FamilyESXi    Family = "esxi"
)

// FamilyOf 返回某家族所属的族。ok 为 false 表示未知家族。
func FamilyOf(os OSFamily) (Family, bool)

// FamilyAllows 判定某族是否允许某家族（§2.1 交叉校验）。
func FamilyAllows(f Family, os OSFamily) bool

// Valid 判定族取值是否合法（来自用户输入，必须白名单校验）。
func (f Family) Valid() bool
```

白名单校验、未知值不静默接受——与 `DeliveryChannel.Valid`（即将删除）和
`FirmwareCompatible` 同一方向。

### 4.2 `model.ArtifactSet`

| 改动 | 说明 |
|---|---|
| 新增 `Family Family` | 用户创建时选，参与 §2.1 交叉校验 |
| 删除 `Channel DeliveryChannel` | 见 §6 |
| 删除 `Image *ImageArtifact` | 见 §6 |
| 删除方法 `MatchesChannel` | 见 §6 |

### 4.3 `model.Template` 与 `model.TemplateBody`

```go
type Template struct {
    ID   string
    Name string

    Family Family                       // 族；空值 = 草稿未定，不可激活
    Bodies map[OSFamily]TemplateBody     // 每家族一份；键必须落在 Family 的允许集合里

    DiskPolicy         DiskPolicy        // 保留，语义见下
    OnFailure          OnFailurePolicy   // 保留，语义见下
    KernelArgs         string            // 保留，作用于引导菜单，不作用于正文
    RequiredFormFields []string

    DerivedFrom DerivedRef               // 新增，仅展示与审计（§2.5）

    Version int
    State   TemplateState
}

type TemplateBody struct {
    Text     string     // 用户自写全文，含占位符
    HookRefs []HookRef  // 本家族正文的 hook 挂载；阶段名必须在 Text 里出现
}

type DerivedRef struct {
    TemplateID string
    Version    int
}
```

删除的字段：

| 字段 | 原因 |
|---|---|
| `ArtifactSetID` | 模板不再钉住某个 ISO。本次解耦的核心（需求 3） |
| `Channel` | 通道整条删除，见 §6 |
| `AnswerTemplate` | 被 `Bodies[家族].Text` 取代。旧字段是「单份正文」，无法承载每家族一份 |

**`DiskPolicy` 与 `OnFailure` 保留但语义收窄**：它们不再驱动渲染器生成分区指令或
失败处置指令，而是**建任务时的决策输入**——磁盘规划器（`contract.DiskPlanner`）据
`DiskPolicy` 选盘，结果进 `ResolvedParams`，模板正文用 `{{fact:disk_primary}}`
引用。`OnFailure` 同样降为 fact（`{{fact:on_failure}}`），正文自己决定怎么用它。

这条分工刻意如此：选哪块盘是**服务端决策**（要看主机清单、要可复现、要固化，
SPEC §2.6），而把选中的盘写成 `part / --ondisk=` 是**格式问题**，归模板。

`KernelArgs` 作用于引导菜单（`menu.go`）而非应答文件，不受本次改造影响。

### 4.4 `model.Hook` 与 `model.HookImpl`

```go
type Hook struct {
    ID          string
    Name        string
    Description string

    Impls map[OSFamily]HookImpl   // 每家族一份实现，允许只填部分家族

    ParamsSchema   string          // JSON Schema，声明该功能需要哪些参数
    ParamsDefaults map[string]string
    Requires       HookRequires
    Required       bool
    TimeoutSec     int

    Version    int
    State      HookState
    Author     string
    Approver   string
    ApprovedAt int64
}

type HookImpl struct {
    Interpreter string   // bash | powershell | cmd | ash
    Body        string
    BodySHA256  string   // 单份实现的 digest，用于 hooks/<digest>/ 落盘定位与审计
}
```

删除的字段与方法：

| | 原因 |
|---|---|
| `Hook.Stage` | 阶段是模板插入时才选的（需求 6），hook 创建时不知道自己属于哪个模板 |
| `Hook.OSFamilies` | 被 `Impls` 的键集合取代，「支持哪些家族」= 「有哪些家族的实现」 |
| `Hook.Interpreter` | 下沉到 `HookImpl`：同一功能在 windows 下是 powershell，在 rhel 下是 bash |
| `Hook.Body` / `Hook.BodySHA256` | 一个 hook 有多份实现，正文不唯一 |
| `Hook.SupportsFamily`（`authoring.go:87`） | 作废，改判 `Impls` 有无该键 |
| `model.HookStage` 整个类型与四个常量 | 阶段名现在是用户定义的字符串，不是枚举 |
| `Template.HooksForStage`（`authoring.go:190`） | 改为按 `TemplateBody` 与阶段名查，签名变更 |

`HookKind`（`fragment` / `script`）**不引入**——`2026-09-28` 设计里它区分声明式与
可执行，而声明式那一类已整体消失（§2.3）。

### 4.5 `model.HookRef` 与 `Task.HookDigests`

```go
type HookRef struct {
    HookID         string
    HookDigest     string            // pin 住的组件 digest，见下
    Stage          string            // 模板正文里的阶段名（原为 HookStage 枚举）
    Order          int
    Required       bool
    ParamsDefaults map[string]string
}
```

**digest 语义**（SPEC §2.7 浮动引用防护）：`HookDigest` 是**整个 hook 全部实现**的
规范化序列化后取 sha256，规范化定义为「按家族名升序，`家族:解释器:正文` 三元组以
`\n` 连接」。改动任何一个家族的实现都产生新 digest，旧模板继续指向旧实现——审批过
的整套实现不会被偷换。

`HookImpl.BodySHA256` 是**单份实现**的裸正文 sha256，必须与 coreutils `sha256sum`
的输出逐字节一致（不加盐、不加前缀、不规范化），因为机器侧 `run_hook` 用
`sha256sum -c` 校验（SPEC §6.3）。它用于 `hooks/<BodySHA256>/script.sh` 落盘定位。

两个 digest 用途不同不可合并：前者是审批边界，后者是内容寻址取回。

`Task.HookDigests` 继续存组件 digest 列表，语义不变。

### 4.6 `model.RenderContext`

```go
type RenderContext struct {
    Task     Task
    Template Template
    Artifact ArtifactSet
    Host     Host

    // Body 是本次渲染要用的那一份正文（按 Artifact.OSFamily 从 Template.Bodies
    // 取出），由调用方取好注入。渲染器不自己查 map——保持「渲染器只消费已备好的
    // 输入」这条纪律，与 Hooks 字段同一理由。
    Body TemplateBody

    // Impls 是本次渲染要消费的 hook 实现，键为 HookRef.HookID，值已按
    // Artifact.OSFamily 选好。由 gateway 层查库后注入（§5.4 纯函数约束）。
    Impls map[string]HookImpl

    Hooks []HookRef   // 保留：挂载顺序与 params 绑定
    Facts map[string]string
    Bases BaseURLs

    InstallToken  string
    FinalizeToken string
    SyslogHost    string
    SyslogPort    int

    ActualFirmware    FirmwareMode
    RootSSHPublicKeys []string
}
```

## 5. 渲染层改造

### 5.1 从六个生成器到一个展开引擎

现状：`internal/render` 有六个家族渲染器，各自生成一份完整应答文件，业务内容是
Go 源码里的字面量（`kickstart.go` 470 行、`autoinstall.go` 用 `yaml.Marshal` 拼
结构体、`autoyast.go`/`unattend.go` 拼 XML）。

改造后：**一个与家族无关的展开引擎**，加上每家族一张「系统部件文本表」。

```
RenderAnswer(ctx) = Expand(ctx.Body.Text, 展开器集合)

展开器集合 = {
    stage:名字   → ClientLib(ctx) + run_hook 行 + 信号行     家族无关
    lober:syslog → systemParts[家族].Syslog                  每家族一行文本
    lober:firstboot → systemParts[家族].Firstboot            每家族一段
    lober:cleanup   → systemParts[家族].Cleanup              每家族一段
    fact:名字    → ctx.Facts[名字]                            家族无关
}
```

**`systemParts` 是六家族唯一剩下的家族分支**，且只含 SPEC §5.1 那三件物理必需的
事，不含任何业务内容。`ClientLib`（`clientlib.go`）本身已经是家族无关的 shell，
除 `windows`（powershell）与 `esxi`（busybox ash）需要各自一版。

保留不动的文件：`clientlib.go`、`firstboot.go`、`params.go`、`menu.go`、
`inspect.go`、`guard/`。

**`skeleton.go` 复用，不删。** 它现在抽的正是三个家族共用的系统部件内容（参数内联、
`ClientLib` 正文、firstboot 要写入目标系统的文件清单、凭据清理动作清单），且其文件头
已经写明「抽出的是**内容**，不是如何嵌入应答文件——三种应答文件语法的嵌入方式本质
不同」。那句话恰好就是本设计的分工：内容归 `systemParts`，嵌入位置归用户正文里的
占位符。改动是把它从「三个家族共用」扩到六个家族，并把产出整理成 `systemParts` 表。

重写的文件：`kickstart.go`、`preseed.go`、`autoinstall.go`、`autoyast.go`、
`unattend.go`、`esxi.go` —— 六者的业务字面量搬进 §7 的骨架正文，剩下的部分并入
`systemParts` 与展开引擎。

删除的文件：`imagewrite.go`（§6）。

### 5.2 各家族正文归属

| 家族 | 用户正文 = 哪份文件 | 系统仍然生成的 |
|---|---|---|
| rhel | `ks.cfg` 全文 | 引导菜单、`systemParts` 三件 |
| debian | `preseed.cfg` 全文 | 同上 |
| ubuntu | `user-data` 全文（YAML） | **`meta-data`**、菜单、`systemParts` 三件 |
| suse | `autoinst.xml` 全文 | 同上 |
| windows | `unattend.xml` 全文 | 同上，另加 WinPE 四文件的菜单引用 |
| esxi | **ESXi 方言 kickstart 全文**，不是 `boot.cfg` | `boot.cfg` 与菜单 |

**ESXi 与 Ubuntu 各要两份文件，用户只写其中一份。** 这两家的第二份文件都不是「应答
文件的一部分」，而是让 installer 找到应答文件的引导层，格式归系统：

- ESXi：`boot.cfg` 是 mboot.efi 的参数文件、不是 kickstart 语法，且它是**导入时从 ISO
  里保存的原始文件**经三处替换而来（`esxi.go:38-51`）。真正驱动无人值守安装的是
  `boot.cfg` 的 `kernelopt` 行里 `ks=` 指向的那份 ESXi 方言 kickstart——那份才是模板
  正文。`RenderESXiBootCfg` 保留不动。
- Ubuntu：`ds=nocloud-net;s=<url>` 指向的目录里要同时有 `user-data` 与 `meta-data`，
  后者承载 instance-id 等 cloud-init 元数据（现有 `testdata/meta_data.golden`）。
  `meta-data` 归系统生成，用户只写 `user-data`。

两者印证了同一条边界：**能让机器找到应答文件的东西归系统，应答文件本身归用户。**
这与 §5.3「引导菜单不在范围内」是同一条原则的三个实例。

**待核实项（实现计划第一步要确认）**：`model.AnswerFileName(OSFamilyESXi)` 现在返回
`"boot.cfg"`（`render.go:112`），而按上表 `/v1/answer` 对 ESXi 应当返回方言 kickstart。
需要确认网关现在实际返回的是哪一份，以及 `ks=` 指向的 URL 是哪个端点。这决定
`AnswerFileName` 要不要改。

### 5.3 引导菜单不在范围内

`menu.go` 产出的是 iPXE 脚本，不是应答文件。它要指向内核、initrd、应答文件 URL、
`inst.ks=` / `auto url=` / `autoyast=` 这类引导参数——这些是**系统必须掌握的格式**
（机器拿到菜单才知道去哪取应答文件），不交给用户。`Template.KernelArgs` 继续作用
于菜单。

### 5.4 纯函数约束保持

渲染器不查库、不碰时钟、不做 IO（SPEC §5.1）。`Body` 与 `Impls` 由调用方（gateway
的 `/v1/answer`、console 的 `preview`）查好后注入 `RenderContext`。这是控制台
`preview` 端点能用假数据跑通、以及 golden 测试成立的前提。

## 6. 镜像直写通道整条删除

**决定**：删掉 `DeliveryChannel` 这一维与镜像直写的全部代码，选集从五元组（家族、
版本、架构、固件、通道）回到四元组。

**理由**：这条通道从未有过真实制品，也从未真机验证——`pipeline/` 按 CLAUDE.md 自己
的说明「是接口定义加工具核实结论，**不是跑通过的 CI**」。把它接进占位符体系只能按
想象设计（镜像直写没有应答文件，「模板正文」对它无意义），而留着不接就是一条与新
模型不兼容的死代码分支。

**代价**：丢掉 SPEC §11 Phase 3 与实现计划 P3-03 已完成的设计与代码。将来要恢复得
重做一遍。这是用户 2026-09-29 的明确决定。

### 6.1 删除清单（9 个生产文件 + 1 个配置字段）

| 文件 | 删什么 |
|---|---|
| `internal/core/model/inventory.go` | `DeliveryChannel` 类型与 2 常量、`Normalize`/`IsInstaller`/`IsImageWrite`/`Valid`、`ArtifactSet.Channel`、`ArtifactSet.Image`、`MatchesChannel`、`ImageArtifact`、`PartitionLayout`、`Partition`、4 个分区角色常量、`RootPartition()` |
| `internal/core/model/authoring.go` | `Template.Channel` |
| `internal/render/imagewrite.go` | 整个文件 |
| `internal/render/renderer.go` | 第 46-47 行的通道分叉 |
| `internal/imports/signature.go` | 整个文件（`VerifyManifestSignature` 是镜像清单签名专用） |
| `internal/console/template.go` | `channel` 入参与校验 |
| `internal/console/task.go` | 建任务的通道一致性交叉校验 |
| `internal/core/fake/store.go` | 通道与镜像字段 |
| `internal/core/contract/conformance/store.go` | 通道与镜像相关断言 |
| `internal/store/postgres/artifact/row.go`、`template/row.go` | 通道列、镜像列 |
| `internal/config/config.go:232-247` | `ImageSignaturePublicKeyPath` |

配套删除：`internal/console/channel_test.go`、`internal/render/imagewrite_test.go`、
`internal/imports/signature_test.go`、`pipeline/` 整个目录、
`docs/superpowers/plans/2026-09-20-lober-phase3-03-image-pipeline.md`。

注意 `internal/imports/iso9660.go:388-415` 的 `rockRidgeSignatureNM` 与签名无关
（Rock Ridge 协议的字段标识），**不要误删**。

SPEC 同步：`§11 Phase 3` 删掉镜像直写条目、`§1476` 的 MVP 排除项列表相应调整、
`§12` 阶段划分去掉 P3-03。

## 7. 自带骨架模板（需求 12）

新建包 `internal/seed/`（当前不存在），`templates/` 下放 **3 份模板定义**（YAML），
`cmd/lober` 首次启动时导入为 `draft` 状态，运维审批后转 `active`。

该包只被 `cmd/lober` 装配点 import（它要落库，而七个实现模块两两不得互相 import，
`archtest/deps_test.go:273` 守卫）：



| 模板 | 族 | 正文份数 | 覆盖 |
|---|---|---|---|
| 「Linux 基线」 | `linux` | 4（rhel / debian / ubuntu / suse） | almalinux、rockylinux、opencloudos、debian、ubuntu、suse |
| 「Windows 基线」 | `windows` | 1 | Server 2008 – 2022，版本差异走 fact |
| 「ESXi 基线」 | `esxi` | 1 | 7、8 |

3 份而非 6 份是因为模板声明的是**族**——`linux` 那份内部有 4 份正文。需求 12 列的
「ubuntu / almalinux / debian / rockylinux / suse」里 almalinux 与 rockylinux 同属
`rhel` 格式，共用同一份正文；运维若想给两者不同的模板名，按需求 10 派生即可。

### 7.1 骨架正文从哪来

**从现有 golden 输出反推。** `internal/render/testdata/*.golden` 是当前六个渲染器
的真实产出，且 RHEL 与 Debian 两条链路已在真机验证过（`docs/real-machine/`）。做法：

1. 取该家族的 golden 输出。
2. 把系统部件（ClientLib、syslog 行、firstboot 脚手架、cleanup）替换成对应占位符。
3. 把 hook 调用段替换成 `{{stage:}}` 占位符。
4. 把任务相关的具体值替换成 `{{fact:}}` 占位符。
5. 其余**原样保留**——那就是骨架正文。

这样骨架正文加展开引擎的产出，理论上与改造前逐字节相同，可直接用现有 golden 断言。

### 7.2 要搬走的字面量清单

改造完成的判据是这些位置不再有业务字面量：

| 内容 | 当前位置 |
|---|---|
| Ubuntu locale / username | `internal/render/autoinstall.go:194,196-199` |
| Ubuntu keyboard layout / storage layout / network version | `autoinstall.go:214-216` |
| Debian 分区方法 / recipe | `preseed.go:168-174` |
| SUSE 基础骨架与软件选择 | `autoyast.go:160-181` |
| Windows 架构映射与骨架 | `unattend.go:65-136,163-172` |
| RHEL 基础指令与分区 | `kickstart.go:191-267,413-470` |
| 各家族镜像源地址 | 各渲染器的 repo / mirror 段 |

## 8. 校验点与错误文案

全部 fail loudly，不静默降级。错误文案给**可行下一步**，沿用
`internal/console/task.go:63-76` 的既有风格。

| 校验 | 时机 | 文案要点 |
|---|---|---|
| 族与识别出的家族不符 | 导入 | 选的族 + 识别出的家族 + 「确认选对了文件」 |
| 模板缺该 ISO 家族的正文 | 建任务 | 模板名 + 缺哪个家族 + 「补齐该正文或换模板」 |
| hook 缺该家族实现 | 插入 hook / 建任务 | hook 名 + 缺哪个家族 + 「补该家族实现或换 hook」 |
| 三个 `{{lober:}}` 占位符缺失或重复 | 模板激活 | 缺/重的是哪个 + 该占位符的作用 + 后果 |
| hook 引用的阶段名在正文里不存在 | 模板保存 | 阶段名 + 正文里现有的阶段名列表 |
| `{{fact:}}` 引用不存在的 fact | 渲染 / preview | fact 名 + 可用 fact 前缀 |
| 未识别的 `{{lober:xxx}}` | 渲染 / preview | 拼错的名 + 三个合法部件名 |
| ISO 家族未知 | 渲染 | 沿用现有「未知的 OS 家族」文案 |

模板保存时**不校验正文语法**（系统不理解语法），只校验占位符。语法错误由 `preview`
端点和真机装机暴露——这是「语法完全交给用户」的必然代价，写在此处以免后来者以为
是漏了校验。

## 9. 存量数据：不迁移

旧字段直接删，不写兼容读取、不写迁移脚本。升级步骤是重建模板与 hook。

理由：纯本地仓库、无远端、开发阶段；且旧 `Template.AnswerTemplate` 本来就是空的
（正文内容在渲染器硬编码里），迁移出来也是空壳。3 份自带骨架让重建的成本很低。

## 10. 与 2026-09-28 设计的差异

`2026-09-28-component-hook-design.md` 未实现任何代码即被本设计取代。差异：

| 项 | 2026-09-28 | 本设计 | 原因 |
|---|---|---|---|
| 应答文件正文 | 系统拥有六家族骨架与合并规则 | 用户自写全文，系统不解析 | 用户 2026-09-29 决定：语法与规则完全交给用户 |
| outlet 词表（8 中性名） | 整套设计唯一的固定结构 | **作废** | 声明式内容进正文，不再需要合并位点 |
| 六套家族 adapter 合并规则 | 每家族一份结构合并函数 | **作废** | 同上 |
| `HookKind`（fragment/script） | 两类实现 | 只剩可执行一类 | 声明式那一类消失 |
| hook 的 stage | 下沉到 `HookImpl` | **删除**，改由模板插入时选 | 用户决定：hook 创建时不知道属于哪个模板 |
| 模板的家族归属 | 无家族 | 声明**族**，内部每家族一份正文 | 自写正文必然绑一种格式，一份文本不能同时是 kickstart 与 YAML |
| 组件库 | 每家族约 40 个组件、共 240 份实现 | **作废**，改 3 份骨架模板 | 组件是为了承载声明式内容而存在 |
| 镜像直写通道 | 保留 | 整条删除 | §6 |
| Windows WinPE 四文件来源 | 设计已定（API 优先、回落配置约定） | **保留该设计**，移入 spec B | 与本设计的渲染改造无关，属控制台/导入层 |
| 族概念 | 不存在 | 新增，ISO 与模板各有 | 需求 4、5 |
| ISO 来源上传/下载 | 不存在 | 见 spec C | 需求 13 |

`2026-09-28-component-hook-implementation.md`（22 任务，未入库）随之作废。

## 11. 契约与 SPEC 同步

### 11.1 SPEC.md

| 小节 | 改动 |
|---|---|
| §2.1 | 制品集新增 `family`；删除 `channel` 与镜像三项 |
| §2.2 | hook 拆组件 + 每家族实现；删除 `os_families`/`stage`/`interpreter`/`body` |
| §2.3 | 模板：新增 `family` 与每家族正文；删除 `artifact_set_id`/`channel`/`answer_template`；`disk_policy`/`on_failure` 语义收窄为决策输入 |
| §2.6 | pin 引用：模板 pin 组件 digest，任务 pin 组件 digest 列表 |
| §2.7 | digest 定义改为「全部实现的规范化序列化」 |
| §4.7 | 「加一个新 OS 家族」改为「加一份 `systemParts` 条目 + 一份骨架正文」 |
| §5.1 | 三段结构改为占位符展开；四件物理必需的事改由 `{{stage:}}` 与三个 `{{lober:}}` 承载；「系统 hook 不作为 hook 数据存在」保留 |
| §5.2 | 阶段不再是四值枚举，改为模板正文自定义；「阶段不是排序，是能力边界」**保留为文档指引**（告诉模板作者各家族的执行环境在哪儿），不再是代码约束 |
| §5.3 | 参数内联保留；新增 `{{fact:}}` 渲染期插值与 `$LOBER_*` 运行期变量的分工 |
| §11 | Phase 3 删掉镜像直写 |
| §12 | 阶段划分按 spec A/B/C 三段重排 |
| 新增 | 占位符体系一节（3 类前缀、展开规则、缩进照抄、转义） |
| 新增 | 族与家族两层关系一节 |

### 11.2 冻结契约

- `docs/contracts/http-console.md`：追加变更记录（模板 family + bodies、hook impls、
  制品集 family、删除 channel）。端点形态细节在 spec B。
- `docs/contracts/filesystem.md`：追加变更记录（hook 落盘路径由
  `hooks/<BodySHA256>/script.sh` 承载单份实现，一个 hook 有多个这样的目录）。
- `docs/contracts/http-machine.md`：**不变**。机器面端点形态与响应一个字节不改。

## 12. 风险

| # | 风险 | 缓解 |
|---|---|---|
| 1 | **拆掉一个正在工作的东西。** RHEL 与 Debian 现在能真机装通，靠的正是渲染器里的硬编码。骨架正文写错一处，装机在目标机上失败，只能到 BMC 控制台看 | 骨架正文从 golden 输出反推（§7.1），产出与改造前逐字节比对。这条不先立住就不要动渲染器。改完后跑 `scripts/e2e.sh` 与 `scripts/e2e-debian.sh`，再跑一次 QEMU 真机 |
| 2 | 占位符定界符与用户正文冲突 | 只识别 3 个确切前缀，不贪婪匹配 `{{...}}`；cloud-init Jinja 原样透传（§3.2）。为此专门写一个「正文含 Jinja 的 ubuntu 骨架」测试用例 |
| 3 | `{{lober:firstboot}}` / `{{cleanup}}` 位置写错，校验查不出来 | 自带骨架里位置已正确，派生天然带着；文档在 §3.4 表里写明每个占位符的位置要求与写错的后果。残余风险接受——系统不解析语法是本设计的前提 |
| 4 | 用户正文语法错误，模板保存时不报 | `preview` 端点是唯一的语法验证手段，spec B 要让它在 UI 上显眼。真机装机是最终判据 |
| 5 | 删除镜像直写波及面判断错，误删别的代码要用的东西 | §6.1 清单已按 grep 核过（9 个生产文件 + 1 个配置字段），并标出 `iso9660.go` 里同名但无关的 `rockRidgeSignatureNM` 不要误删。删完 `make check` 必须全绿 |
| 6 | `AutoMigrate` 的索引盲区 | 新增 `family` 列若参与索引，按 CLAUDE.md 的既有纪律同步改 `internal/store/postgres/schemacheck.go` 的结构断言，WHERE 按 PostgreSQL 规范化后的实际输出写 |
| 7 | ESXi 的 `AnswerFileName` 返回 `boot.cfg` 与本设计的正文归属对不上 | §5.2 已标为待核实项，实现计划第一步就要确认网关实际返回哪一份，不要基于假设动手 |

## 13. 实现阶段划分

依赖顺序如下，每段末尾 `make check` 必须全绿。中间态（新字段已加、旧字段未删）是
刻意的：模型改动与存储改动必须分开提交，否则一个 commit 同时改 model 与两个 store
实现，评审无法进行。

| 段 | 内容 | 出口判据 |
|---|---|---|
| A1 | 删除镜像直写通道（§6） | `make check` 全绿，`go build ./...` 无残留引用 |
| A2 | model 层：`Family`、`TemplateBody`、`HookImpl`、`DerivedRef`、digest 规则 | 单元测试覆盖 §4 全部新类型与校验 |
| A3 | store 层：conformance 套件加断言 → fake 实现 → postgres 实现 + schemacheck | conformance 套件在两个实现上同时通过 |
| A4 | 展开引擎 + `systemParts` 六家族表 | 引擎单测（含 Jinja 透传、缩进照抄、转义、未识别占位符） |
| A5 | 3 份骨架正文（从 golden 反推）+ seed 导入 | 展开产出与改造前 golden **逐字节相同** |
| A6 | 六个家族渲染器改写 / 删除，`§7.2` 字面量清零 | golden 测试全绿；`scripts/e2e.sh` 与 `e2e-debian.sh` 九阶段全绿 |
| A7 | SPEC 与两份冻结契约同步 | 文档与代码一致，`§11` 全部条目落地 |

A1 放在最前：它是纯删除，先做能让后面每一段都少一个维度。

