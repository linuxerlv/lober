# 第 1 层修补 实现计划

> **面向 AI 代理的工作者：** 必需子技能：使用 superpowers:subagent-driven-development（推荐）或 superpowers:executing-plans 逐任务实现此计划。步骤使用复选框（`- [ ]`）语法来跟踪进度。
> 本计划提供接口契约与验收标准，不提供源码。测试与实现体由你在 TDD 循环中写出，以真实测试输出为准。

**目标：** 落实用户 2026-10-02 对第 1 层遗留问题的拍板，修掉第 2 层开工前应修的缺口，让第 1 层可以合入 dev。

**架构：** 仍是 `internal/core/` 下的七个纯判定包加 `internal/archtest`。新增的代码一次登记在 `model/codes.go`（任务 U1）；ISO、插槽适配、ISO 提供集合这三处共用判定放进 `model`，由 `activation`、`params`、`admission`、`snapshot` 共同调用，不在各包里各写一份。

**技术栈：** Go 1.27.1，只用标准库。

**依据：**
- 遗留问题清单：`docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`（下称「清单」）。条目编号（T13-10、L-6 等）指清单里的同名条目，问题、影响、出处都以清单为准，本计划只写决定和契约。
- 第 1 层原计划：`docs/superpowers/plans/2026-09-30-rewrite-layer1-core-model.md`（下称「原计划」）。全局约定、并行执行规则、安全词定义、占位符语法照旧。
- spec：`docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`。

---

## 用户的决定

| 条目 | 决定 | 落在 |
|---|---|---|
| T13-10 | A：model 提供「可存文本」判定，激活判定查正文与说明，显示名另按显示名规则查 | U1、U2、U3、U6 |
| T5-3 | 名称（模板名、hook 名）只能由大小写英文字母、数字和美式键盘上可见的符号组成，即可打印 ASCII 0x21–0x7E，不含空格；其余一律非法 | U1 |
| L-6 | A：model 加 ISO.Validate，Usable 要求它通过，ready 却不合法的 ISO 报 iso_incomplete；不做第 2 层约束（C） | U3、U8 |
| T6-3 | lober 只部署在 Linux 上。清单说的 Windows 陷阱出在被装的 Windows 目标机上，与 lober 部署在哪无关，这一点已向用户说明。本轮只做 A：补文档；B（拒收以 - 或 @ 开头的取值）留到第 3 层写内置模板前再定 | U2、U11 |
| T1-1 | 标准库用 C（白名单），层内方向用 b（只守关键不变式） | U5 |
| L-7 | A：快照构建与参数求值补查前提，渲染可以依赖三条不变式 | U3、U7、U9 |
| T9-1 | B：Subject.Path 统一按位置写 | U6 |
| L-1 | A：脚本正文里的 lober 占位符在激活时报错 | U6 |
| L-2 | A：准入判定查 ISO 提供的集合 | U2、U7、U8 |
| T5-1 | B：目录值传标识，状态传中文显示名 | U1、U3、U8 |

清单第 2 节的 19 条都在本轮修，分到 U1–U10。第 3 节只并入两条：T7-5（ISO 提供集合的选项值必须是安全词）并进 L-6 的 ISO.Validate，T13-8（Build 丢掉查找结果）并进 L-7。其余留给对应的层。

另外按用户的要求：lober 只部署在 Linux 上，所以本轮的整体验收除了在本机跑 `make check`，还要在 QEMU 跑的 Linux 云镜像里跑一遍测试，见「整体验收」。

## 并行执行

照原计划「并行执行」一节的做法：每个任务在 `.tmp/wf/u<N>` 的本地克隆里做，评审通过后由串行的并入步骤压成一个提交，快进并入主目录的 `feat/core-model`。

依赖关系：U1、U4、U5 先开工，互不依赖；U2、U3 依赖 U1；U6 依赖 U1–U4；U7、U8 依赖 U1–U3；U9 依赖 U3、U7；U10 依赖 U1–U4；U11 在 U1–U10 全部并入后做。

- 只改本任务「文件」一节列出的文件。
- 原因代码由 U1 按下面的「新增代码」表一次登记，其他任务只产出、只测试，不改 `codes.go` 与 `problem_test.go`。确需计划外的代码时，追加在两者的末尾，并在报告里说明。
- 名称规则改了以后，activation 的测试里用中文起名的 hook 和模板会失败，所以由 U1 顺手改掉这几个名字（只改 Name 字段，见 U1）。其他包的测试不调用 NormalizeName，名字不用改。

## 新增代码

U1 追加在 `codes.go` 末尾，并登记进 `problem_test.go` 的 extraCodes。

| 代码 | 类别 | 参数 | 何时产生 | 产出 |
|---|---|---|---|---|
| `text_not_storable` | invalid | 0 | 自由文本不是合法的 UTF-8，或含 NUL | U3、U6；第 5 层保存时 |
| `iso_incomplete` | state | 1：字段的中文称呼 | ISO 的记录不合法（ISO.Validate 不通过） | U3 |
| `host_platform_invalid` | invalid | 2：字段的中文称呼、取值 | 主机登记的架构或固件非空，却不是合法取值 | U3 |
| `impl_kind_unknown` | incomplete | 1：种类 | 实现的种类既不是 fragment，也不是 script | U6 |
| `binding_source_unknown` | incomplete | 2：参数名、来源 | 取值来源不是 fixed、attr、default（含零值） | U6 |
| `param_locked_without_value` | incomplete | 1：参数名 | 必填参数锁定在默认值上，却没有默认值 | U6 |
| `script_placeholder_not_expanded` | incomplete | 1：占位符原文 | 脚本正文里出现 slot、lober、sys、param 四种占位符，写错的也算 | U6 |
| `param_attr_unusable` | incompatible | 2：参数显示名、属性键 | 锁定取自主机属性的格子，登记的值解析不了，或取不出要的子值 | U7 |
| `param_sub_unavailable` | invalid | 2：参数显示名、子值名 | 可改的格子填的值取不出模板要的子值（如填了 IPv6，模板取 .netmask） | U7 |
| `iso_import_failed` | state | 0 | ISO 处于导入失败状态 | U3 |

字段的中文称呼由 `codes.go` 里的称呼表给出，产出它的函数都在 model 包内，照现有 paramValueFormats 的做法查表。

U1 不改任何现有代码的参数个数：别的包正按现有个数调用 NewProblem，个数一变，它们在 U1 并入时就会 panic。所以导入失败另立新代码，而不是给 iso_not_ready 加参数。

U1 同时改写这几条现有代码：

| 代码 | 改法 |
|---|---|
| `name_invalid` | 下一步改成新规则：1–64 个英文字母、数字或美式键盘上可见的符号，不能有空格、中文或其他字符；原因常量相应改 |
| `impl_series_invalid`、`template_series_invalid` | 说明加上「或重复勾选」（T9-4） |
| `placeholder_unclosed` | 含义扩成「同一行里，在 }} 或下一个 {{ 之前没有闭合」（T8-1），说明相应改 |
| `param_decl_invalid` | 下一步写全（T5-4②）：默认值要原样就是规范形式（整数不带正号与前导零、主机名小写、IPv6 用压缩写法、不带首尾空白、列表每行一项且不留空行）；enum 要给 Options 或 Offer 之一，Offer 是标识符；Options 与 Offer 只能给 enum，Min 与 Max 只能给 int；选项值不重复；显示名的规则见 U2 |
| `iso_not_ready` | 参数个数不变，含义改成状态的中文显示名（T5-1）；导入失败不再归它，改报新增的 iso_import_failed，下一步是「重新导入，仍然不行就删掉后重新创建」（spec §5.1 第 5 步） |
| `template_not_approved` | 参数改成状态的中文显示名 |
| `param_value_invalid` 与 paramValueFormats | int 的格式说明不再笼统写「声明了上下限的还要在范围之内」，由 Parse 拼上具体范围，三种措辞「不小于 %v」「不大于 %v」「在 %v 到 %v 之间」登记在 codes.go（T5-4①）；sshkey 补「公钥主体不超过 16384 字节」，pwhash 补「不带 rounds=、不超过 256 字节」（T5-4③、T6-2）；下一步补「列表的规范形式合计不超过 32767 字节」 |

---

### 任务 U1：代码表、名称规则与文本判定

**目标：** 一次登记本轮的全部新代码并改写说明；名称只收可打印 ASCII（T5-3）；提供「可存文本」与「显示名」两个判定（T13-10）。

**文件：**
- 修改：`internal/core/model/codes.go`、`problem_test.go` —— 按上面两张表追加与改写，extraCodes 登记 10 个新代码
- 修改：`internal/core/model/ident.go`、`ident_test.go` —— 名称新规则，新增两个判定
- 修改：`internal/core/activation/hook_test.go`、`template_test.go` —— 只改 hook 与模板的 Name 字段：现在用中文起名的 6 处（「LVM 分区」「Windows 装后」「登录提示」「RHEL 系基线」「最小模板」等）改成 ASCII 名字，其余一概不动

**接口契约：**

```go
func NormalizeName(raw string) (display, key string, err error) // 签名不变，规则改变
func StorableText(s string) bool
func ValidLabel(s string) bool
```

- NormalizeName：去掉首尾空白后，必须是 1–64 个 0x21–0x7E 的字符，即大小写英文字母、数字和美式键盘上可见的 32 个符号。内部的空格、制表符、中文、全角字母、任何非 ASCII 字符都报 name_invalid。key 是 display 的 ASCII 小写，所以「Rocky-Base」与「rocky-base」算同名。
- StorableText：合法的 UTF-8 且不含 NUL。空串返回 true：空草稿可以存。
- ValidLabel（参数、选项、状态这类给人看的显示名，可以用中文）：StorableText 为真；1–64 个 rune；首尾不是空白；不含 Cc、Cf、Variation_Selector、Other_Default_Ignorable_Code_Point 里的字符。空串返回 false，「参数显示名可以留空」由调用方判断。

- [ ] **红：** 写失败测试。代码表：与附录加 extraCodes 逐项一致，共 69 个；每项按声明的参数个数构造，说明与下一步不出现 `%!`。NormalizeName：32 个可见符号逐个通过，字母、数字通过；首尾空白被去掉；内部空格、制表符、中文、全角 Ａ、é（合成与分解两种写法）、U+FE0F、U+034F、U+3164、U+200B 都报 name_invalid；64 与 65 个字符的边界；大小写不同得到同一个 key。StorableText：合法、`\xff`、含 NUL、空串。ValidLabel：「交换分区（MiB）」通过；VS16、零宽空格、U+034F、控制字符、首尾空格、65 个 rune、空串都不通过。运行 `go -C <克隆> test ./internal/core/model/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS；再跑 `go -C <克隆> test ./internal/core/activation/ -count=1`，改名后应仍全绿。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(model)!: 名称只收可打印 ASCII，登记修补轮的新代码`）。

**风险：** 改了说明文字以后，如果别的包有测试逐字断言这些文字而失败，只改那几处断言，并在报告里列出。名称规则以后要放宽（比如允许单个空格），只改 NormalizeName 一处和这条测试。

### 任务 U2：参数声明与取值

**目标：** 参数显示名按显示名规则查（T13-10）；取值加上外部限制决定的长度上限（T6-2）；int 的说明给出具体范围（T5-4①）；提供 ISO 提供集合的共用判定（L-2）与面向操作员的解析入口（T11-8）。

**文件：**
- 修改：`internal/core/model/param.go`、`param_test.go`

**接口契约：**

```go
func (t ParamType) Valid() bool
func (d ParamDecl) ParseNamed(name, raw string) (Value, error)
func (d ParamDecl) OfferMissing(offers map[string][]Option) bool
func MissingOffers(decls []ParamDecl, offers map[string][]Option) []string
```

- Validate：Label 非空时要 ValidLabel；每个 Option.Label 都要 ValidLabel，不能为空。不合格的报 param_decl_invalid。
- Parse：sshkey 的公钥主体解码后不超过 16384 字节（OpenSSH 的 SSH_MAX_PUBKEY_BYTES）；pwhash 不超过 256 字节；列表的规范形式合计不超过 32767 字节。超限报 param_value_invalid。int 声明了上下限时，说明里带出具体范围，措辞取自 U1 登记的三种。
- ParseNamed：与 Parse 相同，只是说明里用 name 称呼参数。参数求值用它传显示名。
- OfferMissing：声明了 Offer 的 enum，ISO 没提供那个集合，或集合为空。条件与 params.Evaluate 现在报 iso_offer_missing 的条件完全一致，以后两边都调用它。MissingOffers 按声明顺序列出缺的集合名，去重。
- ParamType.Valid：十种类型为 true，其余为 false（T7-2）。
- IsSafeWord 的文档补上 T6-3 的陷阱（A）：以 - 开头的取值放进 shell 命令的参数位置会被当成选项；cmd 里的 ^ 和 =、PowerShell 里的 @ 和 - 有特殊含义。所以模板作者应把参数放在 -- 之后或加引号，unattend 不把参数放进 cmd 与 PowerShell 的参数位置。字符集不改。

- [ ] **红：** 写失败测试。显示名：中文通过，控制字符、零宽空格、首尾空格、65 个 rune 不通过，空 Option.Label 不通过，空 ParamDecl.Label 通过。上限的边界：公钥主体 16384 与 16385 字节、pwhash 256 与 257 字节、列表总长 32767 与 32768 字节。int 的说明：只有下限、只有上限、两端都有，各出现具体数字。ParseNamed 的说明里出现传入的名字。OfferMissing：集合缺失、集合为空、集合有值、不是 enum、没声明 Offer。MissingOffers 的顺序与去重。ParamType.Valid 对十种类型和未知值。运行 `go -C <克隆> test ./internal/core/model/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`fix(model): 参数显示名、长度上限与 int 范围说明`）。

**风险：** 显示名规则收紧后，别的包测试夹具里不合格的显示名（比如 enum 选项没写 Label）会让那些测试失败。遇到时只补全夹具，并在报告里列出。

### 任务 U3：ISO、主机与取值类型

**目标：** ISO 的记录有完整性判定，可用必须先完整（L-6，并入 T7-5）；主机登记的平台与属性值有校验（T4-1、T13-10）；状态有中文显示名（T5-1）；各取值类型有 Valid()（T7-2）；插槽适配有一份共用判定（L-7）；落库类型的 JSON 编码有 golden 测试（T7-4）。

**文件：**
- 修改：`internal/core/model/registry.go`、`registry_test.go`、`authoring.go`、`authoring_test.go`、`task.go`、`task_test.go`
- 创建：`internal/core/model/golden_test.go`、`internal/core/model/testdata/*.golden.json`

**接口契约：**

```go
func (iso ISO) Validate() []Problem
func (iso ISO) Unusable() []Problem
func ValidAttrKey(key string) bool
func (s ISOState) Label() string     // 另有 Valid()；VersionState 同样各有 Label 与 Valid
func (k ImplKind) Valid() bool       // BindingSource、TaskState 同样各有 Valid
func (i Impl) FitsSlot(exec bool, timing catalog.Timing) Code
```

- Validate：族合法；系合法且属于该族；架构合法；版本匹配 `^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$`，所以 `.`、`..`、以 - 开头的都不合法；名称是可存文本；Offers 的集合名是标识符，选项值是安全词且同一集合内不重复，选项显示名过 ValidLabel（T7-5）。每个问题报 iso_incomplete，Subject 为 {iso, ID, 字段}，字段取 family、series、arch、version、name、`offers/<集合名>`。名称不可存时报 text_not_storable。
- Unusable：可用时返回 nil。导入失败报 iso_import_failed；其余不是 ready 的状态报 iso_not_ready，参数是状态的显示名；ready 却 Validate 不过的，返回 Validate 的问题。Usable 改为 `len(Unusable()) == 0`，第 4 层置 ready 之前调用 Validate。
- ValidAttrKey：标识符且不是保留键 sn、arch、firmware。Host.Validate 改用它，另外查：属性值是可存文本（text_not_storable，Path 为属性键）；非空的 Arch、Firmware 必须合法（host_platform_invalid，Path 为 arch 或 firmware）。
- 状态显示名：ISOState 为获取中、导入中、可用、导入失败、已废弃；VersionState 为草稿、待审批、已审批、已废弃。未知值的 Label 为空串，调用方回落到标识。显示名表照 catalog 的做法，写在类型所在的文件里。
- Valid()：ImplKind、BindingSource、ISOState、VersionState、TaskState 对各自的常量为 true，其余为 false。
- FitsSlot：执行插槽要求实现是脚本、且 Timings 含该时机；片段插槽要求实现是片段。种类不对（含不认识的种类）返回 impl_kind_mismatch，时机不对返回 impl_timing_mismatch，适配返回空串。activation 与 snapshot 都用它，不再各写一份。
- Task.ID 的注释写明：它经 `{{sys:task}}` 原样代入正文，必须是安全词（model.IsSafeWord），由第 2 层生成时保证（L-13b）。
- golden 测试：Snapshot、Template、Hook、Host、ISO、Task 各取一份有代表性的值，用 encoding/json 编码，与 testdata 里的期望字节逐字节比对；改了字段名立刻报红。允许用 `-update` 标志重写期望文件，文件头注释写明什么时候可以这样做。

- [ ] **红：** 写失败测试。Validate：每个字段的合法与非法各一例，版本覆盖 `9.6`、`2025`、`.`、`..`、`-9` 和 33 个字符；Offers 的四条规则。Unusable：五个状态各一例，ready 却缺架构的一例，并核对 Usable 与它一致。ValidAttrKey 与 Host.Validate 的新检查：大小写不对的 `UEFI`、iPXE 的上报值 `i386` 与 `pcbios` 被拒，空值照常通过，属性值含 NUL 被拒。每个状态都有显示名，未知值为空。各 Valid() 对常量与未知值。FitsSlot 的四种组合加不认识的种类。golden 比对。运行 `go -C <克隆> test ./internal/core/model/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`feat(model): ISO 与主机的完整性判定、状态显示名与插槽适配`）。

**风险：** Usable 变严以后，admission 测试里 ready 却不完整的 ISO 夹具会失败。遇到时只补全那些夹具的字段，并在报告里列出；admission 的判定逻辑留给 U8 改。

### 任务 U4：格式显示名、能力表自检与嵌套占位符

**目标：** 格式显示名与 spec 一致（T2-1）；能力表每一格都有结构性校验（T3-1）；扫描遇到嵌套的 `{{` 不再吞掉同一行后面的占位符（T8-1）。

**文件：**
- 修改：`internal/core/catalog/series.go`、`series_test.go`、`timing_test.go`
- 修改：`internal/core/scan/scan.go`、`scan_test.go`

**接口契约：** 不新增对外接口。

- autoyast、agama 的显示名改成 AutoYaST、Agama，测试里的抄录同步改。
- timing_test：每个受支持的时机下，解释器、必需部件、系统值每一项都 Valid() 且列表内不重复；每种格式的 RequiredParts 都含 cleanup，测试名相应改成带「凭据清理」。
- scan：lober 占位符的内容里遇到 `{{` 就截断，外层报 placeholder_unclosed，从内层的 `{{` 接着扫。
- scan_test 顺带补 T5-5 里归扫描的一条：插槽名超长时，placeholder_malformed 的说明与 32 这个上限一致。

- [ ] **红：** 写失败测试。显示名两处；能力表的两条新断言；扫描三例：`{{foo:x}} {{slot:a}}` 恰好报一条 placeholder_unknown_kind，Slots 为 [a]；`{{slot:a {{slot:b}}` 外层报 placeholder_unclosed，Slots 为 [b]；`{{foo:x {{slot:b}}` 同理。三例拼回原文都逐字节相同。运行 `go -C <克隆> test ./internal/core/catalog/ ./internal/core/scan/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`fix(catalog,scan): 格式显示名、能力表自检与嵌套占位符`）。

### 任务 U5：依赖守卫改白名单并查层内方向

**目标：** 守卫按白名单限定标准库（T1-1 选 C），守住层内的关键依赖方向（T1-1 选 b），不再漏掉整包被构建约束排除的目录（T1-3），自身有自动化反例测试（T1-2）。

**文件：**
- 修改：`internal/archtest/deps_test.go`
- 创建：`internal/archtest/guard_test.go` —— 守卫自身的表驱动测试

**接口契约：** 测试包，不对外。规则：

- 标准库白名单（只看非测试源文件）：cmp、errors、fmt、maps、slices、strconv、strings、unicode、unicode/utf8、crypto/sha256、encoding/base64、encoding/binary、encoding/hex、encoding/json、net/netip、regexp。白名单以外的标准库包都算违规，cgo 的 C 与 unsafe 也算。
- 层 1 的包要求 CgoFiles 与 IgnoredGoFiles 都为空。
- 读时钟的函数检查扩到 time 的 Now、Since、Until、Sleep、After、AfterFunc、Tick、NewTimer、NewTicker。time 不在白名单里，这条检查留给以后把 time 放进白名单时用。
- 层内方向（b）：catalog 不 import 层 1 的任何包；model 只 import catalog；activation、params、admission、snapshot 之间只允许 snapshot 依赖 params。scan 不另作限制。
- 目录覆盖（T1-3）：遍历 internal/core，凡是含非测试 .go 文件的目录（跳过 testdata，以及 . 和 _ 开头的目录），都必须出现在 go list 的结果里，否则报错。
- 违规信息写清包、违规的 import 或文件，以及违反了哪一条。

- [ ] **红：** 先写 guard_test：用内存里的源码字符串调用检查函数，覆盖：白名单以外的标准库（os、io/ioutil、math/rand、time）要报；C 与 unsafe 要报；白名单里的不报；层 1 以外的包要报；internal/coreutils 这类前缀相撞的路径不算层 1；别名导入后的 t.Now 与 t.Since 要报；点导入 time 要报；time.Now 当函数值用要报；注释和字符串里的不报；五条方向规则各一正一反；目录覆盖的判定，用构造的目录清单与 go list 结果测。运行 `go -C <克隆> test ./internal/archtest/ -count=1 -v`，预期 FAIL。
- [ ] **绿：** 改 deps_test.go，预期 PASS；对当前的树也通过（现有代码只用到白名单里的包，层内依赖都在规则之内）。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`test(archtest)!: 层 1 守卫改白名单并查层内方向`）。

**风险：** 其他修补任务如果新 import 了白名单以外的标准库包，并入时守卫会报错。那个任务应改用白名单里的包；确实需要的话，把包加进白名单并在报告里说明，这正是白名单要求的评审点。

### 任务 U6：激活判定

**目标：** 激活判定查正文与说明的可存性（T13-10）、脚本里的 lober 占位符（L-1）、取值来源能不能求出值（L-4）、不认识的种类与来源（T7-2）；Subject.Path 按位置写（T9-1）；消除连带报错（T9-6）；补齐测试（T9-4、T10-6）。

**文件：**
- 修改：`internal/core/activation/hook.go`、`template.go`、`hook_test.go`、`template_test.go`

**接口契约：** `Hook`、`Template`、`Approver` 的签名不变。行为变化：

- 可存性：Hook.Description、每份 Impl.Body、Template.Body 不是可存文本时报 text_not_storable。
- 脚本占位符（L-1）：checkScript 对脚本正文跑一次扫描，slot、lober、sys、param 四种占位符（含写错的同种类）各报一条 script_placeholder_not_expanded，参数为占位符原文，带行列。
- 不认识的种类与来源：Impl.Kind 不 Valid() 时报 impl_kind_unknown，不再借 script_incomplete；Binding.Source 不 Valid()（含零值）时报 binding_source_unknown，不再借 param_value_invalid。
- 取值来源（L-4）：fixed 值，以及 default 来源（含没有 Binding 的）取到的默认值，对正文（模板参数）或每个所勾系选中的片段（实例参数）里引用的子值逐个调 Value.Sub，失败报 param_accessor_invalid。hook 参数的默认值被它自己的片段取了子值，也照此检查。参数按 params.Evaluate 的规则是必填，而 Binding 锁定在默认值上、声明里又没有默认值的，报 param_locked_without_value。attr 来源要到求值时才知道，不在此列。
- 属性键：checkBindings 改用 model.ValidAttrKey，不再造一个 model.Host 去借 Validate。
- 插槽适配：checkInstance 的种类与时机比对改用 model.Impl.FitsSlot。
- Subject.Path（T9-1 选 B）：

| 问题落在 | Kind | ID | Path |
|---|---|---|---|
| hook 整体 | hook | hookID@版本 | 空 |
| hook 的第 n 个参数声明 | hook | 同上 | `param/<n>`，n 从 1 起 |
| hook 的第 n 份实现 | impl | 同上 | `impl/<n>`，正文位置另带行列 |
| 模板整体、模板正文 | template | 模板ID@版本 | 空，正文位置带行列 |
| 模板的第 n 个参数声明 | template | 同上 | `param/<n>` |
| 模板参数的取值来源 | template | 同上 | `tpl/<参数名>`，与 params.FieldKey 一致 |
| 插槽 | slot | 同上 | `<插槽名>` |
| 插槽里的第 n 个实例 | slot | 同上 | `<插槽名>/<n>` |
| 实例参数的取值来源 | slot | 同上 | `<插槽名>/<n>/<参数名>`，与 params.FieldKey 一致 |

  系的列表与参数名只出现在说明里。Hook 与 Template 的文档注释同步改。

- 连带报错（T9-6）：重叠检查跳过不属于本实现族的系；正文扫描有问题时不报 assembly_slot_unknown；执行插槽的时机该格式不支持时，跳过实例的时机比对；list_repeat_conflict 每行每个参数只报一次。
- 有意的放行（T9-4）：重复的时机只查一次；空片段允许，作为不需要做任何事的系的空操作实现。两处都写进注释，并用测试钉住。
- T5-5 里归激活的一条：timeout_invalid 的说明与 1–21600 这个范围一致。

- [ ] **红：** 先改测试辅助：onlyCode 要求条数，或改成比对完整的代码列表（T10-6①）。再为上面每条行为写失败测试，每个反例只改一处，断言恰好报出期望的代码与 Subject。另补 T10-6②–⑤：族或系不对与不依赖格式的问题同时报出；片段混格式时跳过 sysvalue_not_allowed；一行里一个 |join 列表配一个逐行重复的列表不冲突（正例）；slot_duplicate 指向后一处并带行列，slot_timing_unsupported、part_not_in_format、实例的 timeout_invalid 的 Subject 各一例。L-4 补两个正例：IPv4 默认值配 .netmask，IPv6 配 .addr 与 .prefix。运行 `go -C <克隆> test ./internal/core/activation/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`fix(activation)!: 正文可存性、脚本占位符、按位置定位与连带报错`）。

**风险：** Subject 格式一改，现有 Subject 测试都要跟着改，这是预期内的。第 5、6 层按这张表解析，以后不要再随手改。

### 任务 U7：参数求值

**目标：** 先查系再查实例（L-7）；面向操作员的取值问题（T11-8）；补测试（T11-10①）。

**文件：**
- 修改：`internal/core/params/params.go`、`params_test.go`

**接口契约：** `Evaluate` 的签名不变。行为变化：

- ISO 的系不在模板所勾的系里时，只报一条 series_not_ticked，Subject 与 snapshot.Build 报的相同，不再每个实例各报一条 impl_missing_for_series（L-7）。
- iso_offer_missing 的条件改为调用 model 的 OfferMissing（L-2），与准入判定共用一份定义。
- 取值问题改为面向操作员（T11-8）：可改的格子解析失败时，用 ParseNamed 以 Field.Label 称呼参数；锁定且取自主机属性的格子，解析失败或取不出子值时报 param_attr_unusable；可改的格子取不出子值时报 param_sub_unavailable。

- [ ] **红：** 写失败测试：系没勾时恰好一条 series_not_ticked；锁定的 attr 格子登记成 IPv6、片段取 .netmask 时报 param_attr_unusable；可改的格子填 IPv6、片段取 .netmask 时报 param_sub_unavailable；可改的格子填错格式时说明里用显示名。另补 T11-10①：把一处引用改成 .netmask 在前、.addr 在后，钉住逐个子值都会检查。运行 `go -C <克隆> test ./internal/core/params/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`fix(params): 先查系，取值问题改为面向操作员`）。

### 任务 U8：准入判定

**目标：** ISO 不完整时给出准确原因（L-6）；向导不再列出注定用不了的模板（L-2）；状态在说明里显示成中文（T5-1）；补测试（T11-10②）。

**文件：**
- 修改：`internal/core/admission/admission.go`、`admission_test.go`

**接口契约：** 签名不变。行为变化：

- ISOUsable 与 Batch 改用 model 的 ISO.Unusable 判断可用性，ISO 不可用时直接返回它给的问题，不再逐台比平台。这样 ready 却缺架构的 ISO 报 iso_incomplete，不会让没登记平台的主机通过，也不会给只登记了架构的主机报参数为空的 firmware_mismatch。
- TemplateUsable 增查 ISO 提供的集合：模板参数，以及装配表里每个实例所引 hook 的参数，用 model.MissingOffers 判断，缺的集合各报一条 iso_offer_missing，Subject 为模板。admission 的包注释改成与实际一致。
- template_not_approved 的参数改传 VersionState 的显示名，为空时回落到标识。

- [ ] **红：** 写失败测试：ready 却缺架构的 ISO 报 iso_incomplete，并且对只登记了架构的主机不报 firmware_mismatch；导入失败报 iso_import_failed；说明里的状态是中文；要 editions 的模板配不提供 editions 的 ISO 时报 iso_offer_missing。另补 T11-10②：废弃引用的测试数据加上 pkgs@1 与 nic@2 同在、nic@2 与 nic@10 同在两组，分别钉住按 ID 排序与按数值比较版本。运行 `go -C <克隆> test ./internal/core/admission/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`fix(admission): ISO 完整性、提供集合与中文状态`）。

### 任务 U9：快照构建

**目标：** Build 自己守住渲染要依赖的三条不变式（L-7，并入 T13-8）；补测试（T11-10③）。

**文件：**
- 修改：`internal/core/snapshot/build.go`、`build_test.go`

**接口契约：** `Build` 的签名不变。只要有下面任何一个问题，就不产出快照：

- ISO 的系不合法，或推不出族与格式：报 series_not_ticked。
- 每个实例用 model.Impl.FitsSlot 检查，不适配的报对应的 impl_kind_mismatch 或 impl_timing_mismatch，Subject 为 {slot, 模板ID@版本, `<插槽名>/<n>`}。
- instance 查 hook 和实现时不再丢掉 ok：查不到就报 hook_ref_missing 或 impl_missing_for_series，Subject 与 params.Evaluate 一致（T13-8）。

渲染因此可以依赖：执行插槽里只有带摘要的脚本，片段插槽里只有片段，Family 与 Format 非空。这三条写进 Build 的文档注释。

- [ ] **红：** 写失败测试：片段放进执行插槽、脚本放进片段插槽、脚本的时机不含执行插槽的时机、种类不认识，各报对应代码且不产出快照；构造一个 Evaluate 不报、但 instance 查不到 hook 的输入（白盒，直接调包内函数），断言报 hook_ref_missing。另补 T11-10③：把重名的插槽插到正文中间，钉住插槽序号连续。运行 `go -C <克隆> test ./internal/core/snapshot/ -count=1`，预期 FAIL。
- [ ] **绿：** 实现，预期 PASS。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`fix(snapshot): 快照构建补查渲染依赖的前提`）。

### 任务 U10：说明文字与常量、词表对齐

**目标：** codes.go 说明里写死的数字与词表，和代码里的常量、目录一致，以后改一边测试就会报红（T5-5）。

**文件：**
- 创建：`internal/core/model/codes_align_test.go`

**接口契约：** 只加测试。断言：

- 说明里含有各长度常量的数字：SN、名称、标识符、安全词、text、列表项数，以及 U2 新加的三个上限。
- param_decl_invalid 里列出的类型清单，等于参数类型表的全部键；sshkey 的算法清单，等于实际支持的算法；保留键清单，等于 hostReservedAttrs。
- timing_unknown、part_unknown、sysvalue_unknown 下一步里列出的词，等于用 catalog 公开查询（Families → SeriesOf → Format → Timings、RequiredParts、SystemValues）求出的并集。
- firmware_mismatch 里「ESXi 只走 UEFI」与 esxi 系的 Firmwares() 一致。

超时范围与插槽名长度的对齐在 U6、U4 里做。

- [ ] **红：** 先写断言并故意改错一个数字，确认测试会报红；改回来后应该通过。如果某条说明本来就和常量不一致，说明 U1 或 U2 漏改了，在报告里列出并改正说明文字。运行 `go -C <克隆> test ./internal/core/model/ -run Align -count=1 -v`。
- [ ] **绿：** 全部通过。
- [ ] **重构 + commit：** `make check` 全绿后 commit（`test(model): 说明文字与常量、词表对齐`）。

### 任务 U11：同步文档

**目标：** spec、原计划和清单与修补后的代码一致。

**文件：**
- 修改：`docs/superpowers/specs/2026-09-30-lober-rewrite-design.md`
- 修改：`docs/superpowers/plans/2026-09-30-rewrite-layer1-core-model.md`
- 修改：`docs/superpowers/reviews/2026-10-02-layer1-open-issues.md`

**改什么：**

- spec：§3 总则的「草稿怎样都能存」改成「能存成 UTF-8 文本的草稿都能存」；C12 写明名称的字符规则；C5 写明可用必须先通过 ISO 的完整性判定；§5.3 第 3 步的筛选条件加上「ISO 提供了模板参数要的集合」；§2.3 或部署相关处写明 lober 只部署在 Linux 上。
- 原计划：「全局约定」里名称可以用中文的说法改掉（插槽名与显示名仍可用中文）；任务 1 的守卫契约改成白名单加层内方向；任务 2 的格式显示名；任务 7 的版本规则与 Usable 的定义；任务 8 的 placeholder_unclosed；任务 9、10 的 Subject 约定；任务 11、13 的前提检查；任务 12 的检查项；附录总表补上新代码，或注明以 extraCodes 为准；「留给后续层的约定」补两条：T6-3 的安全词陷阱（A），以及 L-7 定下的渲染可以依赖的三条不变式。
- 清单：开头加一段修补结果，逐条写明已在哪个提交里落实；T6-3 写明只做了 A，B 待第 3 层前定。

- [ ] **红/绿：** 文档任务没有测试。改完后用 Grep 核对：spec 与原计划里不再有与新规则矛盾的说法（比如模板名可以用中文、Usable 只看状态）；清单里每条第 1、2 节的条目都标了结果。
- [ ] **commit：** commit（`docs: 第 1 层修补后同步 spec、计划与遗留问题清单`）。

---

## 整体验收

- [ ] 在主目录（`feat/core-model`）跑 `make check`，五步全部通过，退出码为 0。
- [ ] `go list ./...` 仍只列出 internal/core 下的七个包和 internal/archtest；`git diff --stat 7ffecb7 HEAD -- web/` 没有输出。
- [ ] 代码表共 69 个：附录 58 个、sn_unsafe_in_body，加上本轮 10 个；每个都由某个非测试源文件实际产出。
- [ ] 用户的每条决定都能用一个测试证明，例如：NormalizeName("Rocky Base") 报 name_invalid；ready 却缺架构的 ISO 在准入时报 iso_incomplete；脚本里写 `{{param:x}}` 激活时报 script_placeholder_not_expanded；片段放进执行插槽时 Build 不产出快照；要 editions 的模板不会出现在不提供 editions 的 ISO 的候选里；守卫拒收 os 与 math/rand；第二份实现的问题 Path 为 impl/2；说明里的 ISO 状态是中文。逐条写出测试名。
- [ ] 清单第 2 节的 19 条都标了已修，各有测试或注释可指。
- [ ] Linux 实测（主会话在工作流结束后做）：把 HEAD 导出到 QEMU 里的 Debian 13 云镜像虚拟机，装同版本的 Go 与 gcc，跑 `make check`，五步全部通过。

