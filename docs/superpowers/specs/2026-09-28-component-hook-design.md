# 组件化 hook 与四层解耦设计

日期：2026-09-28
状态：设计定稿，待实现
关联：SPEC.md §2.2、§2.3、§2.6、§2.7、§4.7、§5.1、§5.2

## 1. 目标与验收判据

让 Ubuntu / SUSE / Windows / ESXi / Debian / Rocky / AlmaLinux 七张 ISO
（对应六个家族枚举值，alma 与 rocky 同属 `rhel`）都能**通过控制台 API** 跑通
真实装机。

**验收判据**：QEMU 虚拟机真实装机，状态机走到 `done`，且能用注入的 SSH 公钥
登入新装好的系统。API 级断言（preview / dryrun / precheck）是辅助手段，不是
验收判据。

**约束**：不能违背既有三级抽象（装机任务 → 模板 → hook），不能靠硬编码绕开
设计另搞一套。

## 2. 设计结论

### 2.1 四层关系

```
装机任务 (Task)      选 iso + 选模板（两个独立选择）
  ├─ artifactSetId   镜像，独立实体。family + os_version + 路径 + manifest
  └─ templateId      模板：无家族、无镜像绑定，纯「组件清单 + 安装方案要素」
       └─ hooks[]    组件：跨家族、跨 ISO 版本、跨模板复用
```

**家族归属只存在于 ISO 这一层。** ISO 属于家族，家族下有任意多个版本的 ISO
（debian 12 / 13 / 8 …）。模板与组件**没有家族**——只有选好 ISO 之后，才能给
这个 ISO 选模板。

**模板是「怎么装」的方案，ISO 是「装什么」的源。** 一个 `debian` 模板同时服务
debian 12 与 debian 13；一个 `rhel` 模板同时服务 rocky 9.4 / alma 9.4 /
opencloudos 9.6。版本差异全部由导入期 facts 与任务级 facts 承载，不进模板。

**流程**：创建装机任务时选 ISO，再为这个 ISO 选模板，装机即可开始。机器引导后
执行模板里挂的 hook 功能组件，从而完成特定操作。

与 Vue 3 的类比：组件是 `.vue` 文件，模板是引用组件的页面，ISO 是数据源。
组件可以在任何模板里使用；组件内部按平台分支由「家族实现」承担。

### 2.2 hook = 组件（中立 props + 家族实现）

hook 拆成两层：**组件**（中立，跨家族）与**实现**（带家族）。「分区」这个功能
在 kickstart 里是 `part /` 段，在 preseed 里是 `partman-auto/*`，在 autoinstall
里是 `storage:` 键，在 autoyast 里是 `<partitioning>`，在 unattend 里是
`<DiskConfiguration>`。这些差异全部收在实现里，组件只声明中立 props。

两类实现，对应 Vue 组件的两种写法：

- `kind: fragment`（声明式）：body 是该家族应答文件的一段，渲染期由 adapter
  合并进骨架。分区、包集合、locale、用户、网络、镜像源属于这一类。
- `kind: script`（可执行）：body 是脚本，机器侧按 digest 落盘后执行。装驱动、
  配业务网、写业务配置属于这一类。

**为什么必须分两类**：声明式内容用 shell 脚本根本写不出来（shell 改不了
preseed 的 partman 方法），而脚本内容塞不进 YAML 键值。现在两类被塞进同一个
`stage` 字段，模板作者只能靠猜。

### 2.3 outlet 词表

fragment 实现必须声明自己落到应答文件的哪个位置。outlet 是**中性功能名**：

```
locale  network  storage  packages  users  identity  repo  scripts
```

每个 outlet 声明**容量**，这是 outlet 词表的一部分：

| outlet | 容量 | 理由 |
|---|---|---|
| `storage` `locale` `users` `identity` `repo` `network` | 单占 | 这些是全局唯一决策，两个组件同时声明必然有一个是错的 |
| `packages` `scripts` | 多占 | 多个组件各出一部分包或脚本是正常需求 |

单占 outlet 出现两个组件时**报错**，不静默后者覆盖——静默覆盖正是本设计要
消灭的「降级到装得上但装错了」那一类问题。

组件实现声明自己落哪个 outlet；每个家族 adapter 声明本家族有哪些 outlet。
两边对上就渲染，对不上就报错：

- 组件在某家族下没有实现 → 「组件 storage 在家族 esxi 下没有实现」
- adapter 缺 outlet → 「槽位 storage 在家族 suse 下未定义，suse adapter 需要
  定义该槽位的合并规则」

**这是整套设计里唯一的固定结构**，共 8 个中性名。它不含任何业务语义，加一个
家族不需要改词表。

### 2.4 stage 挂在实现上，不挂在组件上

组件的切分不受任何固定份数限制：分区、包集合、locale、用户、网络、镜像源、
凭据、驱动、VIB……想加就加，Windows 组件可以不带任何 script 实现。

但 `stage` 字段保留，因为它是**能力边界**而非分类维度（SPEC §5.2 原话：阶段
不是排序，是能力边界）。目标系统还没挂载时 chroot 不可能成立，这不是分类问题。
stage 现在挂在实现上：同一个组件在 rhel 下的 script 实现可以在 `post`，在
esxi 下的 script 实现可以在别的阶段，或者干脆没有 script 实现。

## 3. 数据模型改动

### 3.1 model.Hook：组件

```go
type Hook struct {
    ID          string
    Name        string
    Description string

    // ParamsSchema 是组件的中立 props 声明（JSON Schema）。
    ParamsSchema   string
    ParamsDefaults map[string]string // 组件级默认 props，模板可覆盖

    Requires   HookRequires
    Required   bool
    TimeoutSec int

    Version    int
    State      HookState
    Author     string
    Approver   string
    ApprovedAt int64
}
```

删除的字段及原因：

| 字段 | 原因 |
|---|---|
| `OSFamilies` | 组件无家族。组件不自称支持哪些家族，渲染期由任务的 ISO 家族查有没有实现 |
| `Stage` | 执行环境是实现的属性，不是组件的属性 |
| `Interpreter` | 随 stage 一起下沉 |
| `Body` / `BodySHA256` | 一个组件有多份实现，正文不唯一 |

`SupportsFamily` 方法（`internal/core/model/authoring.go:87-97`）随之作废。

### 3.2 model.HookImpl：家族实现

```go
type HookKind string
const (
    HookKindFragment HookKind = "fragment"
    HookKindScript   HookKind = "script"
)

type HookImpl struct {
    Family OSFamily

    Kind HookKind
    Body  string
    // BodySHA256 保留，用于审计与 hooks/<digest>/ 落盘定位。
    BodySHA256 string

    // 以下三个字段仅 kind == fragment
    Outlet string // 中性功能名，见 2.3

    // 以下两个字段仅 kind == script
    Stage       HookStage
    Interpreter string // bash | powershell | ash
}
```

校验规则：fragment 必须有 Outlet 且无 Stage/Interpreter；script 必须有 Stage
且无 Outlet。两者混填报错。

### 3.3 model.Template：无家族、无镜像绑定

```go
type Template struct {
    ID   string
    Name string

    HookRefs []HookRef

    DiskPolicy         DiskPolicy
    OnFailure          OnFailurePolicy
    KernelArgs         string
    AnswerTemplate     string // 末尾自由文本逃生口，默认空、不解析
    RequiredFormFields []string

    Version int
    State   TemplateState
}
```

删除的字段：

| 字段 | 原因 |
|---|---|
| `ArtifactSetID` | 模板不再钉住某个镜像。这是本次解耦的核心 |
| `Channel` | 通道由 ISO 决定（`ArtifactSet.Channel`），模板侧只能作为默认值 |
| `Family` | 中间讨论过「模板属于家族」，最终按用户决策去掉：家族只存在于 ISO |

`AnswerTemplate` 保留但语义降级为「追加在骨架末尾的自由文本」，默认空、渲染器
不解析它。这样既保留逃生口，又不需要改冻结契约 `docs/contracts/http-console.md`
的字段定义。

`HookRef` 不变（`HookID` / `HookDigest` / `Stage` / `Order` / `Required` /
`ParamsDefaults`）。其中 `Stage` 在新模型下仅对 script 实现有意义，fragment
引用该字段留空。

### 3.4 digest 与 pin 语义（§2.7 浮动引用防护）

`HookDigest(body)` 改为对**组件版本的全部实现**做规范化序列化后取 sha256：

```
规范化序列化 = 按 Family 升序，Family:Kind:Body 三元组以 \n 连接
```

模板 pin 组件 digest，任务 pin 组件 digest 列表（`Task.HookDigests`）。审批过
的整套实现不会被偷换：改动任何一个家族的实现都会产生新 digest，旧模板继续
指向旧实现。

单个实现的 `BodySHA256` 仍单独保留，用于 `/artifacts/hooks/<digest>/` 落盘
定位与审计。

### 3.5 model.Task：不改

`Task` 已经同时持有 `TemplateID` 与 `ArtifactSetID`（`internal/core/model/task.go:106-109`），
四层解耦在数据模型层面**不需要新增字段**。

## 4. 渲染器改动

### 4.1 RenderContext 新增字段

```go
type RenderContext struct {
    // ...现有字段...
    Artifact ArtifactSet // 已存在，任务级，渲染器从任务拿镜像

    // Impls 是本次渲染要消费的组件实现集合，由 gateway 层按任务的
    // ISO 家族 + 模板的组件 digest 列表查好并注入。渲染器自己不查库
    // （§5.1 纯函数约束）。
    Impls []model.HookImpl
}
```

`Hooks []HookRef` 字段保留（模板的挂载顺序与 props 绑定），`Impls` 携带实现
正文。渲染器按 `HookRef.HookDigest` 与实现的组件 digest 对应。

### 4.2 每个 AssembleXxx 拆成两块

以 RHEL 为例，`AssembleKickstart` 拆成：

- **骨架生成**：空应答文件 + 注入 §5.1 系统部件（clientlib、signal/run_hook、
  firstboot 脚手架、凭据清理、sshkey、syslog）。这部分从现有渲染器剥出，
  **内容大幅缩水**——所有业务内容都搬进组件了。
- **装配**：按 outlet 把组件实现的 body 合并进骨架对应位置。

### 4.3 合并规则是纯结构函数

每个 adapter 内的合并函数只认语法、不认语义：

| 家族 | 骨架类型 | 同 outlet 多个实现的合并规则 |
|---|---|---|
| rhel | kickstart（行式 + `%section`） | 同名 `%section` 追加；`%pre`/`%post` 内 body 按 order 拼接 |
| debian | preseed（`d-i key value`） | 同名 key 合并成一个值，值以换行连接（preseed 语法支持） |
| ubuntu | autoinstall（YAML） | 根映射深合并；单占 outlet 命中重复时报错，不覆盖 |
| suse | autoyast（XML） | `<profile>` 下同标签元素合并；单占 outlet 命中重复时报错 |
| windows | unattend（XML，多 pass） | 按 `pass` 分组，`<unattend>` 下元素追加 |
| esxi | kickstart 方言（行式） | 同 rhel |

这些是**家族应答文件语法的属性**，与任何组件的业务语义无关。adapter 增删家族
= 加/减一个文件，不改任何组件。

### 4.4 变量插值

fragment 实现 body 里的 `{{LOBER_FACT_NAME}}` 用 `RenderContext.Facts` 替换
（`ResolvedFacts` / `DecidedFacts` 已在 `RenderContext.Facts` 里，`MergeFacts`
那条链现成）。

script 实现 body **不做插值**，靠 clientlib 注入的 `$LOBER_*` 环境变量取数
（现有 `InlineParams` 机制，`internal/render/params.go`）。

### 4.5 纯函数约束保持

渲染器不查库、不碰时钟、不做 IO。组件实现由 gateway 层在**渲染之前**查好注入
`RenderContext.Impls`。这样 preview 端点照样能用手写组件跑，golden 测试照样
成立。

## 5. 契约改动

### 5.1 POST /hooks、PUT /hooks/{id}

请求体从单 `body` 变成 `impls: [{family, kind, outlet?, stage?, interpreter?,
body}]`。这是 `docs/contracts/http-console.md` 的正式契约变更，走该文档既有的
变更记录流程（文档头已有 2 条记录，本设计追加第 3 条）。

`GET /hooks/{id}` 响应体相应增加 `impls` 数组。

### 5.2 POST /templates、PUT /templates/{id}

删除 `artifactSetId` 与 `channel` 入参。其余不变。

### 5.3 POST /tasks 新增交叉校验

选模板时按顺序校验：

1. 模板存在且 `active`
2. 任务选的 ISO 存在且 `ready`
3. **组件齐备**：模板的每个组件都要有该 ISO 家族的实现。缺失时报「模板 tpl-x
   的组件 storage 在家族 esxi 下没有实现，可换用支持 esxi 的模板或为该组件
   增加 esxi 实现」
4. 通道一致：任务选 ISO 的通道与模板默认通道不符时提示（沿用现有
   `TestCreateTask_ChannelMismatchGivesActionableError` 的错误文案套路）

第 3 项是**新建模板能用、建任务时才知道**的约束，因为选模板时 ISO 还没定。

### 5.4 POST /templates/{id}/preview 新增可选参数

`artifactSetId`（可选）。给了就用真实制品集 facts 校验组件齐备与插值，不给就
沿用现有假数据行为（现有调用方零破坏）。

### 5.5 filesystem.md 契约

script 实现仍落盘到 `hooks/<BodySHA256>/script.sh`。fragment 实现**不落盘**（机器
侧不取），但其 `BodySHA256` 记入 manifest 供审计。追加一条变更记录说明这一点。

## 6. Windows 外部文件来源

### 6.1 缺口

`wimboot` / `BCD` / `boot.sdi` / `boot.wim` 四个文件不在 Windows ISO 里
（`internal/imports/extract_windows_family.go:28-45`），而 `ImportSource` 只有
`ISOPath` 与 `NetbootPath`（`internal/console/artifact.go:63-80`），装配点也从未
调用 `ConfigureWindows`。**Windows 目前无法通过 API 导入**，是七家族里唯一
完全不通的一条。

### 6.2 方案：API 优先，回落配置约定

```go
type ImportSource struct {
    ISOPath     string
    NetbootPath string
    WinPE       *WinPESources // 新增，可空
}

type WinPESources struct {
    WimbootPath  string
    BCDPath      string
    BootSDIPath  string
    BootWIMPath  string
}
```

装配点实现 `imports.WindowsSourcesResolver`：

1. 请求带了 `WinPE` 且四个路径全非空 → 用它
2. 任一为空 → 回落到配置约定
   `cfg.Windows.WinPEDir/<os_version>-<firmware>/{wimboot,BCD,boot.sdi,boot.wim}`
3. 两处都拿不到 → 失败，错误信息把两条路都写清

同时在 `cmd/lober/wire.go` 调 `job.ConfigureWindows(resolver, archDetector)`。
这一步现在是**必须**的，否则 Windows 导入必然失败在
`internal/imports/job.go:290-296`。

## 7. 组件库

`internal/seed/components/` 下放六套家族实现的组件定义，`cmd/lober` 首次启动时
导入为 `draft` 状态的 hook，运维审批后转 `active`。

**每套约 40 个组件**，覆盖：

```
storage  packages  locale  network  users  identity  repo
sshkey  syslog  hostname  bootloader  timezone  keyboard
fstab  swap  usersgroup  rootpw  proxy  ntp  selinux  firewall
grub-install  biosboot  luks  lvm  raid  multipath  lvm-raid
driver  vib  cluster  domain-join  adk  winpe-post  ...
```

**组件内容就是当前渲染器里那些写死的值**，逐条搬进对应组件的对应家族实现——
一份不丢，一份不留。要搬的典型位置（全部为当前的硬编码字面量）：

| 内容 | 当前位置 |
|---|---|
| Ubuntu locale / username | `internal/render/autoinstall.go:194,196-199` |
| Ubuntu keyboard layout / storage layout / network version | `internal/render/autoinstall.go:214-216` |
| Debian 分区方法 / recipe | `internal/render/preseed.go:168-174` |
| SUSE 基础骨架与软件选择 | `internal/render/autoyast.go:160-181` |
| Windows 架构映射与骨架 | `internal/render/unattend.go:65-136,163-172` |
| RHEL 基础指令与分区 | `internal/render/kickstart.go:191-267,413-470` |
| 镜像源地址 | 各家族渲染器的 repo/mirror 段 |

`internal/render` 六个家族渲染器改完后**不再含任何业务字面量**。

## 8. 渲染期「模板对不对」的校验点

| 校验 | 时机 | 错误文案要点 |
|---|---|---|
| 组件在该家族缺实现 | 建任务 / preview 带 artifactSetId | 组件名 + 家族 + 下一步 |
| 组件缺必填 prop | 渲染 | 组件名 + 家族 + 缺哪个 prop |
| adapter 未定义 outlet | 渲染 | outlet 名 + 家族 + 该 adapter 需要定义合并规则 |
| 单占 outlet 出现两个组件 | 建任务 / 渲染 | 冲突的两个组件名 + outlet 名 |
| 声明式实现引用不存在的 fact | 渲染 | fact 名 + 可用 fact 前缀提示 |
| ISO 家族未知 | 渲染 | 沿用现有「未知的 OS 家族」文案 |

全部 fail loudly，不静默降级。理由是本项目最近两周的 6 个修复缺陷全部属于
「静默降级导致装机卡住」类（菜单缺 boot 命令、hook 正文不落盘、initrd 误标
ready）。

## 9. 测试床

`test` 分支的 lobertest L2 扩展，家族差异全数据化：

```
tests/l2_install/families/
  debian.yaml  ubuntu.yaml  suse.yaml
  rhel.yaml    windows.yaml  esxi.yaml
```

每份声明：ISO 路径、固件、模板 ID、期望信号序列、SSH 判据。`rhel.yaml` 用
`variants:` 表达 rocky / alma / opencloudos 三套 ISO 与三个模板记录共用同一份
期望序列。

一个参数化用例遍历六份，不写七个 if/else。

**ISO 缺口**（本机现有 `D:\lober-isos\debian-netinst.iso`、
`D:\OpenCloudOS-9.6-x86_64-minimal.iso`）：

| 家族 | 需要 | 本机 | 状态 |
|---|---|---|---|
| debian | netinst + netboot.tar.gz | netinst ✓，netboot ✗ | 可做，需补 netboot |
| ubuntu | 24.04 server | ✗ | 阻塞 |
| suse | Leap 15.6 installer | ✗ | 阻塞 |
| rhel | rocky 9.4 / alma 9.4 | 只有 opencloudos 9.6 | 可做，rocky/alma 待补 |
| windows | Server 2022 + WinPE 四件套 + 双固件制品集 | ✗ | 阻塞，工作量最大 |
| esxi | 8.0 UEFI | ✗ | 阻塞 |

### 9.1 批次

**第一批：Debian / Ubuntu / SUSE**。用现有 Debian netinst 先打通，锚点式组件、
契约骨架、Windows 来源、测试床参数化一次做完。需补 Ubuntu 24.04 server、
SUSE Leap 15.6、Debian netboot 三张 ISO。

**第二批：Rocky / Alma / opencloudOS**。需补 rocky 9.4 与 alma 9.4 两张 ISO。

**第三批：Windows / ESXi**。Windows 需人工准备 WinPE 四件套与双固件制品集；
ESXi UEFI-only 需单独 vmlab 配置。

## 10. SPEC.md 同步修改

| 小节 | 改动 |
|---|---|
| §2.2 | hook 拆组件 + 实现；新增 `kind`、`outlet`、`params_defaults`；删除 `os_families`、`stage`、`interpreter` 从组件层 |
| §2.3 | 模板删除 `artifact_set_id` 与 `channel`；新增无家族声明 |
| §2.6 | pin 引用：模板 pin 组件 digest，任务 pin 组件 digest 列表 |
| §2.7 | digest 定义改为组件级规范化序列化 |
| §4.7 | 「加一个新 OS 家族」改为「加一个 adapter + 一批组件实现」，DHCP 与 nginx 仍零改动 |
| §5.1 | 渲染器输入增加组件实现集合；系统 hook 仍不作为 hook 数据存在 |
| §5.2 | 阶段挂在实现上而非组件上；「阶段不是排序，是能力边界」保留 |
| 新增 | outlet 词表一节（8 个中性名） |
| §12 | 阶段划分调整：原 Phase 2 拆为「组件模型」「渲染器拆分」「组件库迁移」三段 |

`docs/contracts/http-console.md` 追加两条变更记录（hook impls、template
去 artifactSetId/channel），`docs/contracts/filesystem.md` 追加一条（fragment
实现不落盘）。

## 11. 风险

| 风险 | 缓解 |
|---|---|
| 组件库迁移漏搬某处硬编码 | `internal/render` 改完后 grep 全部已知字面量（locale 字符串、时区、菜单项名），逐条确认已进组件；golden 测试逐字节对比改前改后 |
| outlet 词表不够用 | 词表可增；加词表项只需在 adapter 补合并规则，不动组件 |
| 组件在某些家族语义无法表达 | 允许该家族缺实现，报错而非降级；必要时该家族用 script 实现绕过 |
| digest 语义变更导致存量任务失效 | 存量 hook 数据按旧规则算出 digest 保持可用；新 digest 仅对新组件生效 |
| Windows WinPE 前置工作 | 单独第三批，且在文档里明确「人工准备」这一前提 |
