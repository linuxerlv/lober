# 重写第 1 层：核心模型 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 在删掉旧系统的新树上写出层 1：族系目录、领域类型、正文扫描、激活判定、参数求值、准入判定和快照构建，全部是不做 IO 的纯判定与变换。

**架构：** 七个包放在 `internal/core/` 下，只依赖标准库和彼此。`catalog`（族系目录）是叶子，`model`（领域类型、参数类型、判定结果）建在它上面，`scan`、`activation`、`params`、`admission`、`snapshot` 再建在这两者上。所有判定都返回 `model.Problem` 列表，中文说明只在 `model` 的原因代码表里写一次。

**技术栈：** Go 1.27.1，只用标准库（`net/netip`、`crypto/sha256`、`encoding/json`、`regexp`、`unicode`）。

**依据：** `docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`（下称 spec）。C1–C35 指 spec §3 的契约。

---

## 范围

本层实现 spec §4.1 的六个接口，以及它们共用的领域类型。有两处和 spec 字面不同：

1. 族系目录只放本层判定要用的数据：族、系、格式及其关系，执行时机与次序，各格式支持的时机、解释器、必需系统部件、可用系统值，架构与固件。spec §2.1、§4.1 列的其余数据，由第一个用到它的层加进 `catalog` 包：识别系的规则、仓库完整性判据、附加来源在第 4 层加；下发文件清单、机器面地址格式、制品目录布局在第 3 层加。那些数据要到那一层才有真实的使用方和测试。spec 附录 A 里 65883a5 的回归用例（仓库判据只定义一次）也随这份数据在第 4 层写，测试仍放在 `catalog` 包里。
2. 清场（任务 1）不动 `web/`。vben 的上游代码和 lober 的业务页面混在 `web/apps/web-ele` 里（业务页面是 `src/views/` 下除 `_core` 外的目录、`src/api/console/` 和 `src/router/routes/modules/`），拆开它们要能构建前端才验证得了，这是第 6 层的事。第 1–5 层期间，`web/` 里的旧页面不参与任何构建。

不在本层：仓储与唯一性约束（第 2 层）、渲染与内置内容（第 3 层）、ISO 获取与导入（第 4 层）、HTTP 接口（第 5 层）、状态机与网关（第 7 层）。

## 开工前

- [ ] 主目录（`/e/src/lober`，dev）现有 160 处未提交改动，由用户定去留：81 处已暂存（大批删除、`internal/console/` 下四个新文件、`.gitignore` 的新增），其余是未暂存的删除（`deploy/`、`pipeline/`、`scripts/`、`tools/`）。拍板之前不碰主目录；下面的工作树从 dev 的提交建出，不受这些改动影响。
- [ ] 用户同意后打标签：`git tag -a legacy-final 7355e6a -m "重写前的旧系统"`。之后查旧代码用 `git show legacy-final:<路径>`；没打标签就用 `7355e6a` 代替。
- [ ] 建工作树：`git worktree add .worktree/core-model -b feat/core-model dev`，以后的任务都在它里面做。
- [ ] 在工作树里确认 `go version` 输出 `go1.27.1`。

## 包与文件

| 包 | 文件 | 职责 | 依赖 |
|---|---|---|---|
| `internal/core/catalog` | `doc.go`、`series.go`、`timing.go`、`platform.go` | 族系目录：族、系、格式；时机与能力表；架构、固件与平台兼容 | 无 |
| `internal/core/model` | `problem.go`、`codes.go`、`ident.go` | 判定结果、原因代码表、SN 与名称规范化 | catalog |
| | `param.go` | 参数类型、声明与取值 | |
| | `registry.go`、`authoring.go` | 主机与 ISO；hook 与模板 | |
| | `task.go` | 任务与快照 | |
| `internal/core/scan` | `scan.go` | 正文与片段的占位符扫描 | catalog、model |
| `internal/core/activation` | `hook.go`、`template.go` | 激活判定、审批人校验 | catalog、model、scan |
| `internal/core/params` | `params.go` | 参数求值 | catalog、model、scan |
| `internal/core/admission` | `admission.go` | 准入判定与候选筛选 | catalog、model |
| `internal/core/snapshot` | `build.go` | 快照构建 | catalog、model、scan、params |
| `internal/archtest` | `deps_test.go` | 层 1 依赖守卫 | 无 |

为什么这样切：

- `catalog` 单独成包、不依赖任何东西：它是系统内置的静态事实，第 2–7 层都要读，只读不判定。
- 判定结果、原因代码和参数类型放在 `model`：`scan`、`activation`、`params`、`admission` 都产出或消费它们，放进其中任何一个，都会让另外几个判定包互相依赖。
- 六个接口各占一个包（hook 与模板的激活判定合为一个），包边界就是 spec §4.1 的接口边界。
- `Task` 与 `Snapshot` 放 `model` 而不放 `snapshot`：第 2 层存它们、第 3 层读它们，都应该只依赖 `model`；`snapshot` 包只放构建逻辑。

## 全局约定

- 纯函数：层 1 不读库，不碰时钟、文件和网络（spec §6.1）。任务 1 的依赖守卫机械地检查这一点：禁止 import `os`、`os/exec`、`net`、`net/http`、`database/sql`、`syscall`，非测试源码里不允许出现 `time.Now`。要用当前时间的函数由调用方传入。
- 判定函数返回 `[]model.Problem`，空切片表示通过，不返回 error，也不 panic。规范化函数返回 `(值, error)`，error 的动态类型是 `model.Problem`，可以用 `errors.As` 取出。
- 标识一律是小写 ASCII；给人看的名称（模板名、插槽名、显示名）可以用中文。
- 对外返回的列表都有固定顺序（声明顺序或字典序），同样的输入永远得到同样顺序的输出。返回的切片和 map 都是副本，调用方改了不影响目录和原对象。
- 每个任务一个 commit，提交信息用仓库现有的中文 Conventional Commits 风格。每个任务的重构步骤都要跑一次 `make check`。

---

### 任务 1：清场与构建骨架

**目标：** feat/core-model 的第一个提交删掉旧系统，留下一棵 `make check` 跑得通、带层 1 依赖守卫的空树。

**文件：**
- 删除：`web/` 以外的全部受控文件，只留 `.gitignore`、spec 和本计划。约 500 个文件，包括 `cmd/`、`internal/`、`scripts/`、`deploy/`、`tools/`、`pipeline/`、`SPEC.md`、`go.sum`、根目录的 `.wf*.js`，以及 `docs/` 下的其他文件。
- 重写：`go.mod`，只留 `module github.com/lober/lober` 和 `go 1.27.1`，没有 require。
- 重写：`Makefile`，目标为 `fmt`、`vet`、`test`、`race`、`cross`、`check`。
- 创建：`internal/core/catalog/doc.go`，只有包注释，写明本包职责，让守卫从第一天起就有检查对象。
- 创建：`internal/archtest/deps_test.go`，层 1 依赖守卫。

**接口契约：**

```make
cross:
	GOOS=linux GOARCH=amd64 go build ./...

check: fmt vet test race cross
```

`fmt`、`vet`、`test`、`race` 四个目标照 `legacy-final:Makefile` 的写法（它避开了 `rm -rf`、`find` 这类 POSIX 专属工具）。spec §7.1 里「比对生成代码」那一步，等第一个代码生成器出现时（第 5 层）再加进 `check`。

依赖守卫对 `go list -json ./...` 列出的每个 `internal/core/` 下的包检查：
- 只 import 标准库（路径第一段不含 `.`）和 `github.com/lober/lober/internal/core/` 下的包；
- 不 import「全局约定」列出的六个标准库包；
- 非测试源文件里不出现 `time.Now`；
- 检查过的层 1 包数为 0 时直接失败，防止 `go list` 出错或路径写错时空转成绿。

失败信息要写出违规的包和它 import 的路径。可参考 `legacy-final:internal/archtest/deps_test.go`，它用 `go env GOMOD` 定位模块根，不硬编码路径。

- [ ] **红：** 先写守卫，再做一次证伪：临时建 `internal/tmpother`（空包）和 `internal/core/tmpbad`（import 前者，并调用一次 `time.Now`），运行 `go test ./internal/archtest/ -v`，预期 FAIL，失败信息同时点出这两处违规。
- [ ] **绿：** 删掉两个临时包，再跑同一条命令，预期 PASS。
- [ ] **重构 + commit：** 删除旧文件；跑 `go mod tidy` 后不应重新生成 `go.sum`；`make check` 全绿；`git ls-files | grep -v '^web/'` 只剩上面列出的七个文件。commit：`refactor!: 删除旧系统，从层 1 的空骨架重新开始`，正文写明证伪实验的结果和 `legacy-final` 的用法。

**风险：** 不要在主目录跑 `git clean`、`git checkout .` 或 `git stash`，那里有用户还没定去留的改动；所有操作只在工作树里做。`.gitignore` 里指向已删路径的条目先留着，不影响构建。

### 任务 2：族、系与格式

**目标：** 目录能回答「这个系属于哪个族、用哪种格式」，并对来自用户的标识做白名单校验（C1，以及 C2 的数据基础）。

**文件：**
- 创建：`internal/core/catalog/series.go`
- 测试：`internal/core/catalog/series_test.go`

**取值表**（标识字面量本身就是决策，会进数据库和 URL；右侧是界面显示名）：

| 族 | 格式 | 系 |
|---|---|---|
| `linux` Linux | `kickstart` | `rhel` RHEL、`rocky` Rocky Linux、`almalinux` AlmaLinux、`openeuler` openEuler、`opencloudos` OpenCloudOS |
| | `preseed` | `debian` Debian |
| | `autoinstall` | `ubuntu` Ubuntu |
| | `autoyast` | `sles15` SLES 15、`leap15` openSUSE Leap 15 |
| | `agama` | `sles16` SLES 16、`leap16` openSUSE Leap 16 |
| `windows` Windows | `unattend` | `windows-server` Windows Server、`windows-desktop` Windows 桌面版 |
| `esxi` ESXi | `esxi-ks` | `esxi` ESXi |

格式的显示名就是它的标识，`esxi-ks` 显示为「ESXi ks」。

**接口契约：**

```go
type Family string
type Format string
type Series string

func (f Family) Valid() bool   // Format、Series 同样各有 Valid 与 Label
func (f Family) Label() string
func (s Series) Family() Family // 未知的系返回 ""
func (s Series) Format() Format // 未知的系返回 ""
func Families() []Family            // 声明顺序
func SeriesOf(f Family) []Series    // 声明顺序
func SeriesOfFormat(f Format) []Series
```

- 标识区分大小写，不做规范化：传进来的值必须与表里完全一致，否则 `Valid` 为 false。
- 返回的切片是副本。

- [ ] **红：** 写失败测试，覆盖：每个系的族与格式都合法；同一种格式的系都属于同一个族；每个族、每种格式至少有一个系；七种格式、十四个系一个不少、一个不多；未知值和大小写不同的值 `Valid` 为 false，查询返回空值；改动返回的切片不影响下一次查询。运行 `go test ./internal/core/catalog/ -v`，预期 FAIL（类型未定义）。
- [ ] **绿：** 实现取值表与查询，同一条命令预期全部 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(catalog): 族、系与格式的内置目录`）。

**风险：** 系的标识发布后不能改名，数据库和 URL 里都会用；有疑问先改这张表再往下做。

### 任务 3：执行时机、解释器、系统部件与系统值

**目标：** 目录能回答某种格式支持哪些执行时机、每个时机能用哪些解释器、正文必需哪些系统部件、能引用哪些系统值（C1、C10、C14、C15 的数据基础），并给出时机的次序（C33）。

**文件：**
- 创建：`internal/core/catalog/timing.go`
- 测试：`internal/core/catalog/timing_test.go`

**标识与含义：**

| 类型 | 取值 |
|---|---|
| 时机 | `pre` 装前、`post-installer` 装后（安装环境）、`post-target` 装后（目标系统内）、`firstboot` 首次开机 |
| 解释器 | `sh`（POSIX sh，含 busybox ash）、`bash`、`cmd`、`powershell` |
| 系统部件 | `syslog`、`firstboot`（首次开机脚手架）、`cleanup`（凭据清理） |
| 系统值 | `repo` 安装源地址、`iso` 原始 ISO 地址（Ubuntu 的 `url=`）、`smb` Windows 安装源共享、`gateway` 网关基地址、`task` 任务号、`sn` 主机 SN、`lbrace` 字面量 `{{`（转义用） |

**能力表**（kickstart、preseed 取自旧系统跑过的渲染器，其余是初值。第 3、7 层真装后改这张表时要附带用例）：

| 格式 | 时机与解释器 | 必需系统部件 | 可用系统值 |
|---|---|---|---|
| kickstart | 四个时机都是 sh、bash | syslog、firstboot、cleanup | repo、gateway、task、sn、lbrace |
| preseed | pre、post-installer 只有 sh；post-target、firstboot 是 sh、bash | firstboot、cleanup | repo、gateway、task、sn、lbrace |
| autoinstall | 四个时机都是 sh、bash | firstboot、cleanup | repo、iso、gateway、task、sn、lbrace |
| autoyast | 四个时机都是 sh、bash | firstboot、cleanup | repo、gateway、task、sn、lbrace |
| agama | 四个时机都是 sh、bash | firstboot、cleanup | repo、gateway、task、sn、lbrace |
| unattend | pre 只有 cmd；firstboot 是 cmd、powershell | firstboot、cleanup | smb、gateway、task、sn、lbrace |
| esxi-ks | pre、post-installer、firstboot 都只有 sh | firstboot、cleanup | gateway、task、sn、lbrace |

**接口契约：**

```go
type Timing string
type Interpreter string
type SystemPart string
type SystemValue string // 四种各有 Valid 与 Label

func (t Timing) Rank() int                           // pre=1，两个 post=2，firstboot=3；未知为 0
func (f Format) Timings() []Timing                   // 先按 Rank，再按上表顺序
func (f Format) Interpreters(t Timing) []Interpreter // 不支持的时机返回空
func (f Format) RequiredParts() []SystemPart
func (f Format) SystemValues() []SystemValue
```

- 两个 post 时机同级：它们的先后由正文书写位置决定，推进任务状态时算同一步（spec §2.1、C33）。

- [ ] **红：** 写失败测试，覆盖：能力表逐格与上表一致；每种格式都支持 firstboot 时机、都要求 firstboot 部件；每个受支持的时机至少有一个解释器；Rank 的次序与两个 post 同级；未知格式、不支持的时机返回空；返回的切片是副本。运行 `go test ./internal/core/catalog/ -v`，预期 FAIL。
- [ ] **绿：** 实现，同一条命令预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(catalog): 执行时机、解释器、系统部件与系统值`）。

**风险：** unattend 的 pre 时机跑在安装 ISO 自带的 WinPE 里，既没有 PowerShell 也没有 HTTP 客户端（spec §8），这一格照「只有 cmd」登记。不要因为「以后能解决」就先把 powershell 写上。

### 任务 4：架构、固件与平台兼容

**目标：** 把 iPXE 上报的构建架构与平台换算成系统的架构与固件，并给出一个平台兼容判定，建任务时（本层准入判定）与引导时（第 7 层网关）共用（C28）。

**文件：**
- 创建：`internal/core/catalog/platform.go`
- 测试：`internal/core/catalog/platform_test.go`

**接口契约：**

```go
type Arch string     // x86_64 | aarch64
type Firmware string // bios | uefi；两种各有 Valid 与 Label

func ArchFromIPXE(buildarch string) (Arch, bool)        // x86_64、i386 → x86_64；arm64 → aarch64
func FirmwareFromIPXE(platform string) (Firmware, bool) // pcbios → bios；efi → uefi
func (s Series) Firmwares() []Firmware                   // esxi 只有 uefi，其余两种都有
func PlatformOK(s Series, isoArch, hostArch Arch, hostFW Firmware) (archOK, fwOK bool)
```

- `i386` 映射到 x86_64 是刻意的：BIOS 下的 iPXE 是 32 位构建，在 x86_64 机器上也报 i386（旧系统 d014d50）。
- `PlatformOK` 里，主机的架构或固件为空时，对应那一项算通过：建任务时主机可能没登记这两项（C28「登记了才校验」）。引导时网关传进来的是换算后的实际值，不会为空；换算失败由网关自己拒绝。
- 系或 ISO 架构未知时，两项都不通过。

- [ ] **红：** 写失败测试，覆盖：iPXE 取值换算（含未知字符串、大小写不同）；esxi 配 bios 不通过、配 uefi 通过；Linux 与 Windows 的系两种固件都通过；架构不一致；主机两项为空时都通过；未知的系。运行 `go test ./internal/core/catalog/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(catalog): 架构、固件与平台兼容判定`）。

### 任务 5：判定结果、原因代码与标识规范化

**目标：** 定下全层共用的判定结果形态，建立原因代码表（中文说明只写这一次），并实现 SN、名称和标识符的规范化（C6、C12）。

**文件：**
- 创建：`internal/core/model/problem.go`（判定结果）、`codes.go`（原因代码表）、`ident.go`（SN、名称、标识符）
- 测试：同目录的 `problem_test.go`、`ident_test.go`

**接口契约：**

```go
type Code string
type Category string // invalid | incomplete | incompatible | state
type Subject struct{ Kind, ID, Path string; Line, Col int }
type Problem struct {
	Code          Code
	Subject       Subject
	Message, Next string // 中文说明与下一步
}
func (p Problem) Error() string
func (c Code) Category() Category
func NewProblem(c Code, s Subject, args ...any) Problem
func NormalizeSN(raw string) (string, error)
func NormalizeName(raw string) (display, key string, err error)
func ValidIdent(s string) bool
```

- `Category` 供第 5 层映射 HTTP 状态码：invalid 输入不合法，incomplete 未完备（激活不过），incompatible 组合不兼容（准入不过），state 对象状态不允许。
- `Subject`：Kind 取 host、iso、hook、impl、template、slot、param、body 之一；ID 是 SN、ISO ID 或「hook ID@版本」这类标识；Path 是对象内的位置（参数的字段键、插槽名），可空；Line、Col 是正文位置，从 1 起，不适用时为 0。
- 代码表的每一项写明类别、说明模板、下一步模板，以及模板要几个参数。`NewProblem` 从表里取模板、用 `args` 填入。后面每个任务新增的代码都加到 `codes.go`，别处不写中文说明。
- SN（C6）：去掉首尾空白，ASCII 字母转大写；结果须为 1–64 个可打印 ASCII 字符（0x21–0x7E），中间允许单个空格。以下视为占位值，规范化后比较：`TO BE FILLED BY O.E.M.`、`DEFAULT STRING`、`SYSTEM SERIAL NUMBER`、`CHASSIS SERIAL NUMBER`、`SERIAL NUMBER`、`NOT SPECIFIED`、`NOT APPLICABLE`、`N/A`、`NONE`、`INVALID`、`OEM`、`0123456789`、`123456789`；由同一个字符重复组成的（如 `00000000`、`XXXXXXXX`）也算。
- 名称（C12）：去掉首尾空白，内部连续空白压成一个空格；1–64 个 rune，不含控制字符。`key` 是 display 的小写形式，第 2 层按它做唯一约束，所以「Linux 基线」与「linux 基线」算同名。
- 标识符（参数名、主机属性键）：`^[a-z][a-z0-9_]{0,31}$`。

本任务新增的代码：

| 代码 | 类别 | 何时产生 |
|---|---|---|
| `sn_invalid` | invalid | SN 为空、超长或含不可打印字符 |
| `sn_placeholder` | invalid | SN 是占位值 |
| `name_invalid` | invalid | 名称为空、超长或含控制字符 |

- [ ] **红：** 写失败测试，覆盖：代码表每一项都有类别、说明和下一步，按它声明的参数个数构造一次，说明和下一步里都不出现 `%!`；`Problem` 能经 `errors.As` 取出；SN 的首尾空白与大小写、内部空格、超长、控制字符、每一个占位值、同字符重复；名称的空白压缩、中文、超长、大小写不同时得到同一个 key；标识符的合法与非法样例。运行 `go test ./internal/core/model/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(model): 判定结果、原因代码表与标识规范化`）。

**风险：** 说明模板的占位数和参数个数对不上时，fmt 不报错，而是输出 `%!(EXTRA …)` 这类文字，会原样出现在界面上。上面那条代码表测试就是为此设的，后续任务每加一个代码，它都会自动覆盖到。

### 任务 6：参数类型与取值

**目标：** 定下参数的类型系统：哪些类型能进正文与片段（C19 的「受限类型」），每种类型怎样校验、规范化和取子值，参数声明本身怎样校验。

**文件：**
- 创建：`internal/core/model/param.go`
- 测试：`internal/core/model/param_test.go`

**安全词：** 由 `[A-Za-z0-9._:/@^+=-]` 组成、1–128 个字符的串。它不含引号、空白、`$`、`#`、`%`、`<>&`、反斜杠和花括号，所以放进七种格式里任何带引号的位置、XML 文本、kickstart 与 preseed 的指令值、shell 参数，都不会改变结构。不加引号放进 YAML 时，作者要自己留意格式的特殊规则，比如 YAML 裸值不能以 `@` 开头。

**类型表：**

| 类型 | 合法取值与规范形式 | 可进正文与片段 | 子值 |
|---|---|---|---|
| `word` | 一个安全词 | 是 | — |
| `int` | 十进制整数，可声明上下限；规范形式不带正号和前导零 | 是 | — |
| `enum` | 选项值之一；选项值必须是安全词，另带中文显示名 | 是 | — |
| `ip` | IPv4 或 IPv6 地址，不带 zone；按 `net/netip` 的规范形式输出 | 是 | — |
| `cidr` | 地址/前缀，如 `10.0.0.3/8`，保留主机位 | 是 | `addr`、`prefix`、`netmask`（仅 IPv4） |
| `hostname` | RFC 1123：每个标签 1–63 个 `[a-z0-9-]`，不以 `-` 开头或结尾，总长 ≤253；输出小写 | 是 | — |
| `diskpath` | 以 `/dev/` 开头的安全词 | 是 | — |
| `sshkey` | `算法 base64[ 注释]`，算法限 `ssh-ed25519`、`ssh-rsa`、`ecdsa-sha2-nistp256`、`ecdsa-sha2-nistp384`、`ecdsa-sha2-nistp521`、`sk-ssh-ed25519@openssh.com`、`sk-ecdsa-sha2-nistp256@openssh.com`；注释只含安全词字符；以单个空格分隔 | 是 | — |
| `pwhash` | crypt 格式，以 `$6$`、`$5$` 或 `$y$` 开头，其余为 `[./A-Za-z0-9$]` | 是；放进片段里的 shell 时，作者要加单引号 | — |
| `text` | 任意 UTF-8，≤4096 字节，不含 NUL | 否，只能进脚本 | — |

**参数声明的字段：**

| 字段 | 含义 |
|---|---|
| Name | 标识符（任务 5），在同一个 hook 或模板内唯一 |
| Label | 中文显示名 |
| Type | 上表之一 |
| List | 是否为列表；列表 1–256 项，`text` 不能是列表 |
| Required | 只对脚本生效；被正文或片段引用的参数一律必填（C20） |
| Default | 规范形式的默认值，可空，必须能通过本声明的校验 |
| Options | `enum` 的静态选项 |
| Min、Max | `int` 的上下限，可空 |
| Offer | `enum` 的选项来自 ISO 提供的哪个集合（如 `editions`，C21）；与 Options 二选一 |

**接口契约：**

```go
type ParamType string
type Option struct{ Value, Label string }
type ParamDecl struct{ /* 字段见上表 */ }
type Value struct{ /* 类型与规范形式，字段不导出 */ }

func (t ParamType) Restricted() bool
func (d ParamDecl) Validate() []Problem
func (d ParamDecl) Parse(raw string) (Value, error) // 列表的 raw 以换行分隔
func (v Value) Items() []string                     // 标量为单元素
func (v Value) Canonical() string                   // 列表以换行连接
func (v Value) Sub(name string) (Value, error)      // 子值；列表逐项取
func EnvName(param string) string                   // disk → LOBER_PARAM_DISK
```

- `Parse` 的 error 用代码 `param_value_invalid`，说明里写清期望的格式；`Sub` 遇到不适用的子值（IPv6 取 netmask、非 cidr 类型）返回 `param_accessor_invalid`。
- `EnvName` 是脚本取参数的唯一约定：第 3 层按它导出环境变量，任务 9 按它检查脚本里的引用。

本任务新增的代码：`param_decl_invalid`、`param_value_invalid`、`param_accessor_invalid`，类别都是 invalid。

- [ ] **红：** 写失败测试。每种类型至少三个合法、三个非法样例，非法样例里要有能改变结构的字符（引号、换行、`<`、`#`、`$(`、反引号）；规范化（IPv4 前导零被拒、hostname 转小写、sshkey 多余空格压成一个、int 的 `+5` 与 `007`）；列表的项数上下限与逐项校验；`cidr` 的三个子值与 IPv6 取 netmask 被拒；声明校验（名字非法、默认值不合法、enum 选项不是安全词、Options 与 Offer 同时给、`text` 声明成列表）；`EnvName`。运行 `go test ./internal/core/model/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(model): 参数类型、声明与取值`）。

**风险：** 安全词的字符集是「系统不解析语法也能安全插值」的根基。放宽任何一个字符前，都要逐一确认它在七种格式的文本、引号和 shell 位置里都不会改变结构。Windows 管理员口令在 unattend 里的编码与 crypt 不同，不在本层；第 3 层写 unattend 内置模板时如果需要，照本任务的方式新增类型并附用例。

### 任务 7：主机、ISO、hook 与模板

**目标：** 定下四个聚合的形状，以及它们自带的几个小判定：hook 取覆盖某个系的实现，模板推出自己的格式，取模板的当前已审批版本（C7、C23 的选取部分、C24、C25 的字段）。

**文件：**
- 创建：`internal/core/model/registry.go`（主机与 ISO）、`authoring.go`（hook 与模板）
- 测试：同名的 `_test.go`

**字段语义**（字段的 Go 类型由实现者按语义定，名字就用下面的，第 2 层会原样存它们）：

- `Host`：SN（已规范化）；Arch、Firmware（可空）；Attrs，键为标识符且不能是 `sn`、`arch`、`firmware`，值是原样字符串，被参数引用时才按类型校验。
- `ISO`：ID、Name、Family、Series、Version（`[0-9A-Za-z._-]{1,32}`）、Arch、State（acquiring 获取中、importing 导入中、ready 可用、failed 失败、deprecated 已废弃）、SHA256、Artifacts（角色 → 制品目录下的相对路径，角色由第 4 层定）、Offers（集合名 → 选项列表，如 `editions`）。
- `Hook`：ID、Version（从 1 起）、Name、Description、State（draft、review、approved、deprecated）、Author、Approver、Builtin、Params（参数声明）、TimeoutSec 与 Required（脚本实现的默认值，C11）、Impls。
- `Impl`：Family、Series（非空）、Kind（fragment、script）、Body，以及只对脚本有意义的 Interpreter 与 Timings。
- `Template`：ID、Version、Name、State、Author、Approver、Builtin、DerivedFrom（来源模板的 ID 与版本，可空）、Family 与 Series（都可空）、Body、Params（模板自己的参数声明）、Bindings（模板参数的取值来源）、Assembly（插槽名 → 有序的实例列表）。
- `Instance`：HookID、HookVersion、TimeoutSec 与 Required 的可选覆盖、Bindings。
- `Binding`：Source（fixed 写死、attr 主机属性、default 用声明的默认值）、Value（fixed 时）、Attr（attr 时）、Editable（向导里能不能改）。某个参数没有 Binding 条目时，等同于「default，可改」。

**接口契约：**

```go
type HookKey struct {
	ID      string
	Version int
}
func (h Hook) Key() HookKey
func (h Hook) ImplFor(s catalog.Series) (Impl, bool)
func (i Impl) Digest() string                     // Body 的 sha256，小写十六进制
func (t Template) Format() (catalog.Format, bool) // 所勾的系非空且同格式时为 true
func CurrentApproved(versions []Template) (Template, bool)
func (h Host) Validate() []Problem
func (i ISO) Usable() bool                        // State 为 ready
```

- `Digest` 与 coreutils `sha256sum` 对 Body 字节的输出一致：不加前缀，不规范化换行。机器侧的运行脚本用它校验下载到的 hook 脚本。
- `CurrentApproved` 返回 State 为 approved 的最高版本，没有就返回 false。废弃的版本不算，所以最新版被废弃后会回落到上一个已审批版本（C23、C26）。
- `ImplFor` 按系查；同一个系出现在两份实现里是任务 9 要拦的错误，这里遇到时返回第一份，不另报错。
- `Host.Validate` 只查属性键，代码 `attr_key_invalid`（invalid）；SN 在登记时已经过 `NormalizeSN`。

- [ ] **红：** 写失败测试，覆盖：`ImplFor` 命中、未命中、多份实现各覆盖不同的系；`Digest` 与已知输入的 `sha256sum` 结果一致（含结尾换行和 CRLF 两种输入）；`Template.Format` 的四种情况（没勾系、勾一个、勾同格式的多个、勾了不同格式的）；`CurrentApproved` 在「全是草稿」「v5 已审批」「v5 已废弃而 v4 已审批」「版本乱序传入」下的结果；属性键非法与用了保留键；`Usable` 对五个状态。运行 `go test ./internal/core/model/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(model): 主机、ISO、hook 与模板`）。

**风险：** 这些字段名第 2 层会用作列名或 JSON 键，一旦存进数据库就不好改；本任务就用最终的名字，不要起临时名。

### 任务 8：正文扫描

**目标：** 把模板正文或 hook 片段切成原文与占位符，给出插槽、系统部件、系统值和参数引用，并标出写错的占位符（spec §4.1 的正文扫描；C14 的语法部分；C18）。

**文件：**
- 创建：`internal/core/scan/scan.go`
- 测试：`internal/core/scan/scan_test.go`，以及 `internal/core/scan/testdata/` 下七种格式各一份小样例

**占位符语法**（spec §2.5 留给本层的决策）：

| 写法 | 含义 |
|---|---|
| `{{slot:名字}}` | 片段插槽 |
| `{{slot:名字@时机}}` | 执行插槽，时机取任务 3 的四个标识之一 |
| `{{lober:部件}}` | 系统部件：`syslog`、`firstboot`、`cleanup` |
| `{{sys:名字}}` | 系统值，取任务 3 的七个名字；`{{sys:lbrace}}` 输出字面量 `{{`，用来转义 |
| `{{param:名字}}` | 参数引用：在模板正文里指模板参数，在片段里指该 hook 的参数 |
| `{{param:名字.子值}}` | 取子值，如 `{{param:addr.netmask}}` |
| `{{param:名字\|join}}` | 列表参数以单个空格连接后内联 |

识别规则：

- `{{` 之后跳过空格，若紧接着是 `[a-z]+:`，就当作 lober 占位符，种类必须是上表四种之一，否则报错。其他 `{{…}}` 原样保留，所以 cloud-init 的 `{{ ds.meta_data.local_hostname }}` 不受影响。代价是正文里不能直接写 `{{http://…}}` 这类字面量，要写就用 `{{sys:lbrace}}` 转义。
- 占位符不能跨行，`{{` 与 `}}` 之间允许首尾有空格。
- 插槽名为 1–32 个字母（含中文）、数字、`_` 或 `-`。
- 列表参数默认逐行重复：所在的整行按列表项各重复一次，每次代入一项。这样 YAML 的 `    - {{param:pkgs}}` 和 XML 的 `<package>{{param:pkgs}}</package>` 都能直接写；`|join` 留给 preseed 这类要在一行写完的场合。参数是不是列表，扫描时不知道，由任务 9、10 按声明判断。

扫描结果：

- 按原文顺序排列的片段序列，每段要么是原文，要么是一个占位符。占位符带上行号与列号（按 rune 计，从 1 起）、所在行的前导空白（空格与制表符）、是否独占一行（该行除前导空白、它自己和行尾空白（含 `\r`）外没有别的字符），以及解析出的种类和各项内容。
- 插槽列表（按出现顺序）、系统部件的出现列表、系统值引用列表、参数引用列表。
- 所有片段的原文依次拼起来，与输入逐字节相同（C18 的基础）。

**接口契约：**

```go
type Kind int // Text、Slot、Part、Sys、Param
type Segment struct{ /* 种类、原文、行列、缩进、是否独占一行、解析结果 */ }
type Slot struct {
	Name   string
	Exec   bool
	Timing catalog.Timing // 仅执行插槽
}
type Result struct{ /* Segments、Slots、Parts、Sys、Params */ }

func Scan(src string) (Result, []model.Problem)
```

- 有问题时也返回尽量完整的结果，写错的占位符当作原文段：编辑器要靠它继续显示插槽列表。
- 扫描不看格式。某种格式能不能用某个时机、某个系统值，必需哪些部件，都是任务 9、10 的事。重复的插槽名在这里也不报错，如实列出，由任务 10 报。

本任务新增的代码，类别都是 invalid，Subject 的 Kind 为 body 并带行列：`placeholder_unknown_kind`（种类不是四种之一）、`placeholder_malformed`（名字、时机、子值或 `|join` 的写法不对）、`placeholder_unclosed`（同一行里没有 `}}`）、`timing_unknown`、`part_unknown`、`sysvalue_unknown`。

- [ ] **红：** 写失败测试，覆盖：七种格式的样例各扫出预期的插槽、部件、系统值和参数；拼回原文逐字节相同（含 CRLF 与制表符缩进）；Jinja 的几种写法原样保留；每种错误各一例，行列号正确；首尾带空格的占位符；未闭合；`{{sys:lbrace}}`；中文插槽名；独占一行的判定，含「前面有非空白」「后面有非空白」两种反例。运行 `go test ./internal/core/scan/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(scan): 正文与片段的占位符扫描`）。

**风险：** 行列号是给编辑器标红用的，一律按 rune 计；按字节算的话，中文正文里标出的位置会错开。

### 任务 9：hook 的激活判定

**目标：** 判断一个 hook 版本能不能批准（C8–C11，以及 C19 在片段上的部分）。

**文件：**
- 创建：`internal/core/activation/hook.go`
- 测试：`internal/core/activation/hook_test.go`

**接口契约：**

```go
func Hook(h model.Hook) []model.Problem
```

判定内容（新代码加进 `model/codes.go`；除沿用的 invalid 代码外，类别都是 incomplete）：

| 检查 | 代码 |
|---|---|
| 名称合法 | `name_invalid` |
| 参数声明逐个合法，名字不重复 | `param_decl_invalid` |
| 至少有一份实现 | `hook_no_impl` |
| 每份实现的族合法，系非空、合法且都属于该族 | `impl_series_invalid` |
| 同一个系只出现在一份实现里 | `impl_series_overlap` |
| 片段：覆盖的系同格式 | `fragment_mixed_formats` |
| 片段：扫描无错（任务 8 的问题原样带出） | 任务 8 的代码 |
| 片段：不含插槽和系统部件 | `fragment_forbidden_placeholder` |
| 片段：引用的系统值在该格式可用 | `sysvalue_not_allowed` |
| 片段：引用的参数都已声明，且不是 `text` | `param_undeclared`、`param_text_in_fragment` |
| 片段：一行里最多一个逐行重复的列表参数；子值只用在支持它的类型上；`\|join` 只用于列表 | `list_repeat_conflict`、`param_accessor_invalid` |
| 脚本：正文非空，声明了解释器和至少一个时机 | `script_incomplete` |
| 脚本：对覆盖的每个系，声明的每个时机该格式都支持，且解释器在该时机可用 | `script_timing_unsupported`、`script_interpreter_unavailable` |
| 脚本：正文里字面出现的 `LOBER_PARAM_*` 都对应已声明的参数 | `param_undeclared` |
| 有脚本实现时，超时在 1–21600 秒之间 | `timeout_invalid` |

Subject：hook 整体的问题（名称、参数声明、没有实现、超时）Kind 为 hook，ID 为「hook ID@版本」；实现相关的问题 Kind 为 impl，ID 同上，Path 为这份实现覆盖的系（逗号分隔），涉及正文位置时带上行列。

- [ ] **红：** 先写一个能通过的完整 hook 作正例：一份 rocky 与 almalinux 共用的 kickstart 片段，加一份覆盖 debian 与 ubuntu、时机为 post-target、解释器为 sh 的脚本。每一行检查都在正例上只改一处写反例，确认报出的恰好是那一个代码；其中要有「脚本声明 bash 在 pre 时机、覆盖 debian」（preseed 的 pre 只有 sh）。运行 `go test ./internal/core/activation/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(activation): hook 的激活判定`）。

**风险：** `LOBER_PARAM_*` 只按字面匹配，脚本里动态拼出来的变量名查不到。这是刻意的取舍，写进该代码的说明里（「只认脚本里字面出现的变量名」）。

### 任务 10：模板的激活判定与审批人

**目标：** 判断一个模板版本能不能批准（C13–C17、C19，C22 与 C26 在模板上的部分），并校验审批人（C22）。

**文件：**
- 创建：`internal/core/activation/template.go`
- 测试：`internal/core/activation/template_test.go`

**接口契约：**

```go
func Template(t model.Template, hooks map[model.HookKey]model.Hook) []model.Problem
func Approver(author, approver string) []model.Problem
```

- `hooks` 由调用方凑齐，包含装配表引用的每个 hook 版本。缺的当作引用不存在报错，不去别处查。
- `Approver`：审批人为空报 `approver_missing`（invalid），与作者相同报 `approver_is_author`（state）。内置内容出厂即已审批，不走这里。

判定内容（新代码的类别都是 incomplete）：

| 检查 | 代码 |
|---|---|
| 名称合法 | `name_invalid` |
| 族已选，系非空、合法且属于该族 | `template_series_invalid` |
| 所勾的系同格式 | `template_mixed_formats` |
| 正文扫描无错 | 任务 8 的代码 |
| 插槽名不重复 | `slot_duplicate` |
| 执行插槽的时机该格式支持 | `slot_timing_unsupported` |
| 片段插槽独占一行 | `fragment_slot_not_alone` |
| 该格式的必需部件各恰好出现一次；不属于该格式的部件不能出现 | `part_missing`、`part_duplicate`、`part_not_in_format` |
| 引用的系统值在该格式可用 | `sysvalue_not_allowed` |
| 正文里的参数引用都在模板参数里声明过，不是 `text`；列表规则同任务 9；逐行重复的列表参数所在行里不能有插槽或系统部件 | `param_undeclared`、`param_text_in_fragment`、`list_repeat_conflict`、`param_accessor_invalid` |
| 模板参数声明合法；Bindings 只指向已声明的参数，fixed 的值能通过类型校验，attr 的键是合法标识符 | `param_decl_invalid`、`binding_param_unknown`、`param_value_invalid`、`attr_key_invalid` |
| 装配表里的插槽都在正文里 | `assembly_slot_unknown` |
| 引用的 hook 版本存在、已审批、未废弃 | `hook_ref_missing`、`hook_ref_unapproved`、`hook_ref_deprecated` |
| 对所勾的每个系，每个实例的 hook 都有覆盖它的实现 | `impl_missing_for_series` |
| 实现种类与插槽类型一致；执行插槽的时机在实现声明的时机里 | `impl_kind_mismatch`、`impl_timing_mismatch` |
| 实例的 Bindings 按该 hook 的参数声明校验，规则同上面模板参数那一行 | 同上 |

同一个 hook 在一个模板里出现多次不报错（C17）。装配表里没有实例的插槽是允许的，展开为空，也不报错。Subject：模板整体的问题 Kind 为 template，ID 为「模板 ID@版本」；插槽与实例的问题 Kind 为 slot，Path 为插槽名或「插槽名/序号」。

- [ ] **红：** 先写一个能通过的完整模板作正例：kickstart 正文，勾 rocky 与 almalinux；一个片段插槽放一个分区片段 hook；一个 post-target 执行插槽放同一个脚本 hook 两次、参数不同；三个必需部件各一次；一个模板参数。每一行检查都在正例上只改一处写反例，确认报出的恰好是那一个代码；另测 `Approver` 的三种输入。运行 `go test ./internal/core/activation/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** 与任务 9 重复的检查（参数引用、列表规则、系统值）收成包内一处；`make check` 全绿后 commit（`feat(activation): 模板的激活判定与审批人校验`）。

### 任务 11：参数求值

**目标：** 给定模板、它引用的 hook、选定的 ISO 和一台主机，产出这台主机在向导里的参数表单；带上操作员填的值时，产出最终取值或逐格错误（C19–C21）。

**文件：**
- 创建：`internal/core/params/params.go`
- 测试：`internal/core/params/params_test.go`

**字段键**（前后端和错误定位共用的线上格式）：模板参数为 `tpl/<参数名>`；实例参数为 `<插槽名>/<序号>/<参数名>`，序号是实例在插槽里的位置，从 1 起。插槽名不含 `/`，所以不会有歧义。

**接口契约：**

```go
type FieldKey string
type Input struct {
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
	ISO      model.ISO
	Host     model.Host
	Entered  map[FieldKey]string // 操作员填的值
}
type Field struct{ /* Key、Label、Type、List、Required、Locked、Prefill、Options */ }
type Values map[FieldKey]model.Value
type Result struct{ Fields []Field; Values Values; Problems []model.Problem }

func Evaluate(in Input) Result
```

- `Fields` 按「模板参数，再按插槽在正文里的顺序、实例在插槽里的顺序」排列，同样的输入顺序不变。`Values` 只在没有问题时非空。
- 列表参数的操作员输入是换行分隔的文本，与任务 6 的 `Parse` 一致。

每个字段的取值规则，按以下顺序：

1. 选实现：对每个实例，用 ISO 的系调 `ImplFor`。
2. 必填：被模板正文或所选片段引用的参数必填；只被脚本用到的参数按声明的 Required。
3. 预填：Binding 为 fixed 取它的值；为 attr 取主机属性，主机没登记这个属性就当作没有值，**不回落到声明的默认值**（回落会让「按主机指定系统盘」的模板，在漏登记的主机上悄悄用默认盘）；为 default 或没有 Binding 时取声明的默认值。
4. 锁定：Binding 的 Editable 为 false 时锁定；没有 Binding 时可改。
5. 选项：`enum` 取声明的 Options，或取 ISO 的 Offers 里同名的集合（C21）。
6. 最终值：可改的字段用操作员填的值，没填就用预填值；锁定的字段用预填值。然后按类型解析（任务 6）。

本任务新增的代码（Subject 的 Kind 为 param，ID 为主机 SN，Path 为字段键）：

| 代码 | 类别 | 何时产生 |
|---|---|---|
| `param_required` | invalid | 必填字段最后没有值 |
| `param_locked` | invalid | 操作员填了锁定的字段 |
| `param_attr_missing` | incompatible | 锁定、必填，而引用的主机属性没登记；下一步是去主机登记里补这个属性 |
| `param_not_offered` | invalid | 取值不在 ISO 提供的集合里；静态选项不符时用 `param_value_invalid` |
| `iso_offer_missing` | incompatible | 参数要的集合 ISO 没有提供 |
| `param_unknown_field` | invalid | Entered 里有表单中不存在的字段键 |

本函数假定模板已通过激活判定。遇到结构性缺失（引用的 hook 不在 Hooks 里，或某个实例没有覆盖该系的实现）时报 `hook_ref_missing` 或 `impl_missing_for_series`，跳过那个实例，不 panic。

- [ ] **红：** 写失败测试，覆盖：六条规则与六个代码各至少一例；正文和片段引用的参数没有值时报 `param_required`，而不是得到空串（spec 附录 A 回归 072cf91、7d22dfa）；同一 hook 在一个插槽里放两次，两个实例的字段键和取值互不干扰；主机缺属性时，「可改」与「锁定」两种 Binding 的不同结果；ISO 的 editions 集合作为 enum 选项；同一个模板配 rocky 与 almalinux 的 ISO 时选中不同的实现，必填集合随之不同；字段顺序稳定。运行 `go test ./internal/core/params/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(params): 参数求值：逐台表单与最终取值`）。

### 任务 12：准入判定与候选筛选

**目标：** 判断一台主机、一个 ISO、一个模板能不能建任务，并用同一套规则给向导前三步筛候选（C5、C22、C26、C27、C28 建任务时的那一次、C31）。

**文件：**
- 创建：`internal/core/admission/admission.go`
- 测试：`internal/core/admission/admission_test.go`

**接口契约：**

```go
func HostSelectable(h model.Host, hasOpenTask bool) []model.Problem
func ISOUsable(iso model.ISO, hosts []model.Host) []model.Problem
func TemplateUsable(t model.Template, hooks map[model.HookKey]model.Hook, iso model.ISO) []model.Problem
func Batch(in BatchInput) BatchResult
type BatchInput struct {
	Hosts    []model.Host
	OpenTask map[string]bool // SN → 是否已有未结束的任务
	ISO      model.ISO
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
}
type BatchResult struct{ Common []model.Problem; ByHost map[string][]model.Problem }
```

- `HostSelectable`：已有未结束的任务报 `host_has_open_task`（state）。
- `ISOUsable`：ISO 不可用报 `iso_not_ready`（state；已废弃也归这里，说明里区分）。对每台登记了架构或固件的主机调 `catalog.PlatformOK`，不通过报 `arch_mismatch` 或 `firmware_mismatch`（incompatible），Subject 指向那台主机。向导据此把 ISO 灰显，并写明是哪几台冲突。
- `TemplateUsable`：`t` 必须是调用方用 `CurrentApproved` 取出的版本。不是 approved 报 `template_not_approved`（state）；引用了已废弃的 hook 版本报 `template_blocked_by_deprecated`（incompatible），说明里点出是哪个 hook 的哪个版本，下一步是升级引用；ISO 的系不在所勾的系里报 `series_not_ticked`（incompatible）。这里不重做激活判定。
- `Batch`：ISO 与模板的问题放 `Common`，只出现一次；主机相关的问题放 `ByHost`，每台主机都有键，没问题的对应空切片。同一个 SN 在一批里出现两次，两处都报 `host_duplicate_in_batch`（invalid）。

- [ ] **红：** 写失败测试，覆盖：每个代码各一例；主机没登记架构和固件时 ISO 判定通过；esxi ISO 对一批里唯一一台 bios 主机只报那一台；模板 v5 引用了已废弃的 hook 版本时报阻塞，而不是改看 v4；批内 SN 重复；一批全部通过时 `Common` 为空、每台主机都是空切片。运行 `go test ./internal/core/admission/ -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(admission): 准入判定与候选筛选`）。

### 任务 13：任务、快照与快照构建

**目标：** 定下任务与快照的形状，并把选定的主机、ISO、模板版本、hook 版本和参数取值整理成渲染要用的完整输入（C29，也是 C35 的前提）。

**文件：**
- 创建：`internal/core/model/task.go`（任务与快照）、`internal/core/snapshot/build.go`（构建）
- 测试：`internal/core/model/task_test.go`、`internal/core/snapshot/build_test.go`

**任务 `Task`：** ID、HostSN、ISOID、TemplateID、TemplateVersion、State、Snapshot、CreatedBy、CreatedAt（Unix 秒，由调用方传入）。State 取 pending 等待引导、booting 引导中、installing 安装中、rebooting 重启中、initializing 初始化中、done 完成、failed 失败、cancelled 已取消。第 7 层会再加 token、引导次数、时机进度这类执行字段。

**快照 `Snapshot`**（渲染只读它，所以它必须自足）：

- Schema：快照格式版本，本层为 1。任务要保存很久，格式将来变了，渲染要靠它认出旧快照，而不是读错。
- Family、Series、Format。
- Host：SN、Arch、Firmware、Attrs 的副本。
- ISO：ID、Name、Version、Arch、Artifacts 的副本。
- Template：ID、Version、Name、Body，以及模板参数的最终取值。
- Slots：按正文里出现的顺序，每个带序号（从 1 起，可作 ASCII 标识）、名字、是否执行插槽、时机。每个插槽按装配顺序列出实例：hook 的 ID、版本、名字；所选实现的种类、正文、解释器；生效的超时与是否必需（实例的覆盖优先，否则取 hook 的默认值）；脚本的摘要（`Impl.Digest`）；该实例参数的最终取值。正文里有、装配表里没有实例的插槽也要列出，实例为空。
- 参数的最终取值一律存为「类型、是否列表、规范形式的列表项（标量为单元素）」，不存 `model.Value`：渲染只需要这些，存 `Value` 会把它的内部结构带进数据库。

**接口契约：**

```go
type TaskState string
func (s TaskState) Finished() bool // done、failed、cancelled

func Build(in params.Input) (model.Snapshot, []model.Problem)
```

- `Build` 自己调用 `params.Evaluate(in)`，有问题就原样返回、不产出快照；它也自己重新扫描正文取插槽顺序。这样建任务、预览与这里用的是同一套取值规则，不会有两份结果对不上。
- 前提不满足时返回问题，不产出快照：ISO 的系不在所勾的系里报 `series_not_ticked`，某个实例没有覆盖该系的实现报 `impl_missing_for_series`。正常流程里准入判定已经挡住了这些，这里是最后一道：错的快照要到机器装到一半才会暴露。
- 确定性：同样的输入得到深度相等的快照，用 `encoding/json` 编码两次字节相同。第 2 层按 JSON 存它。

- [ ] **红：** 写失败测试，覆盖：一个完整模板（片段插槽、执行插槽、同一 hook 的两个实例、一个空插槽）配 rocky ISO，构建出的快照逐项符合上面的说明，其中包括全部参数的最终取值（回归 41b2fbe）；超时与是否必需的覆盖与默认；摘要与 `Impl.Digest` 一致；两种前提不满足各一例；参数求值有问题时原样返回；打乱输入里 map 的构造顺序后，JSON 编码仍逐字节相同；`Finished` 对八个状态。运行 `go test ./internal/core/... -v`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(snapshot): 任务、快照与快照构建`）。

---

## 留给后续层的约定

本层按下面的约定做校验，第 3 层渲染时必须照做，改动要回到本层同步改判定：

- 执行插槽和系统部件各展开成一行，可以写在行内。这一行里不能有引号、`<>&`、反斜杠、`#`，也不能有「冒号加空格」，这样放进 JSON 字符串、XML 文本、YAML 裸值和 shell 里都不会改变结构。
- 片段插槽按实例顺序依次展开各片段，必须独占一行，展开出的每一行都套用这一行的前导空白。
- 列表参数默认逐行重复所在的整行；`|join` 以单个空格连接后内联。
- 脚本只经 `EnvName` 给出的环境变量拿参数，列表以换行连接。

## 整体验收

- [ ] 在工作树根目录跑 `make check`，fmt、vet、test、race、cross 五步全部通过，退出码为 0。
- [ ] `go list ./...` 只列出 `internal/core/` 下的七个包和 `internal/archtest`。
- [ ] 依赖守卫通过，任务 1 的证伪实验结果写在该任务的 commit 正文里。
- [ ] `git diff --stat dev...feat/core-model -- web/` 没有输出，`web/` 没被动过。
- [ ] `go test ./internal/core/model/ -run Code -v` 覆盖了代码表里的全部代码，每个都有类别、说明和下一步。
- [ ] 契约覆盖：下表每一行都能指出至少一个正例和一个反例测试。

| 契约 | 本层负责的部分 | 任务 |
|---|---|---|
| C1 | 族、系、格式与能力表 | 2、3 |
| C2 | 系属于族的判定 | 2 |
| C5 | ISO 可用才能建任务 | 7、12 |
| C6 | SN 规范化与占位值 | 5 |
| C7 | 主机属性键 | 7 |
| C8–C11 | hook 的激活判定 | 9 |
| C12 | 名称规范化与唯一键 | 5 |
| C13–C17 | 模板的激活判定 | 10 |
| C18 | 扫描无损 | 8 |
| C19–C21 | 参数类型、求值与 ISO 提供的取值 | 6、11 |
| C22 | 审批人；只引用已审批的 hook | 10、12 |
| C23 | 当前已审批版本的选取 | 7 |
| C24、C25 | 派生来源与内置标记的字段 | 7 |
| C26 | 废弃挡住新引用 | 10、12 |
| C27 | ISO 的系在模板所勾的系里 | 12 |
| C28 | 平台兼容（建任务时的那一次） | 4、12 |
| C29 | 快照完整且确定 | 13 |
| C31 | 已有未结束任务的主机不能选 | 12 |
| C33 | 时机次序 | 3 |

由后续层负责的：C3、C4（第 4 层）；C6 与 C12 的唯一性、C23 的不可修改、C26 的禁止删除、C30、C31 的并发兜底（第 2 层）；C32、C34，以及 C35 的逐字节一致（第 3、7 层）。
