# 第 1 层遗留问题清单

日期：2026-10-02
对象：`feat/core-model` 上的 d45fbf3（第 1 层 13 个任务与整层修正）

13 个任务的实现者和评审者共留下 249 条记录，涉及与计划的偏差、顾虑和评审建议。去重合并后剩 168 条，逐条对照 d45fbf3 的代码核实过：

- 已被后续任务解决，或属于合理偏差：64 条
- 纯表述问题，或没有实际影响：30 条
- 需要你拍板：10 条（第 1 节）
- 第 2 层开工前应修：19 条（第 2 节）
- 归后续层处理：15 条（第 3 节）

每条都附有出处（文件与行号，或计划、spec 的章节），以及它合并了哪些原始记录。

## 修补结果

2026-10-12 第 3 层补记：P-2 选 A、T6-3 选 B、T6-6、T8-4、T8-8 已修；T4-3 已记内置内容决定，桌面固件限制仍归第 7 层。S1-6 更新在第 2 层清单原条目。L-8 的全局词表/Subject.Kind 仍归第 5 层。历史条目正文保留原发现情形，以最新「结果」为准；详细验证见 [第 3 层验收](2026-10-12-layer3-acceptance.md)。

2026-10-02 按你的决定修补，计划见 `docs/superpowers/plans/2026-10-02-layer1-fixups.md`。第 1 节 10 条、第 2 节 19 条全部落实（T13-8 随 L-7 一起做）；第 3 节这一轮只并入了 T7-5（随 L-6 进 ISO.Validate），其余 14 条当时仍归后续层（第 3 层最新结果见上段）；后来 L-8 里字段键那一项随修补轮之后的 C-3 修掉了（036f397），见 L-8 下面的结果。每条的做法和测试名写在条目标题下的「结果」一行。各条「出处」里的行号指 d45fbf3，修补后已经变了。

修补轮的提交，都在 `feat/core-model` 上：

| 提交 | 任务 | 内容 |
|---|---|---|
| 822eec6 | U1 | 一次登记 10 个新代码并改写说明；名称只收可打印 ASCII；StorableText 与 ValidLabel |
| 03e0fec | U2 | 参数显示名、长度上限、int 的范围说明、ParseNamed、OfferMissing |
| 5416531 | U3 | ISO 与主机的完整性判定、状态显示名、取值类型的 Valid、插槽适配、golden 测试 |
| fc53fe6 | U4 | 格式显示名、能力表自检、嵌套占位符 |
| 091d3af | U5 | 依赖守卫改白名单，查层内方向与目录覆盖，守卫自身的反例测试 |
| 43aca7b | U6 | 激活判定 |
| c7fe6db | U7 | 参数求值 |
| cc4c636 | U8 | 准入判定 |
| a3dc308 | U9 | 快照构建 |
| 3572304 | U10 | 说明文字与常量、词表对齐 |
| 与本节同一个提交 | U11 | spec、第 1 层原计划与本清单同步 |

逐条：

| 条目 | 结果 | 提交 |
|---|---|---|
| T13-10 | 选 A | 822eec6、03e0fec、5416531、43aca7b |
| T5-3 | 按你的决定：名称只收可打印 ASCII | 822eec6 |
| L-6 | 选 A，不做 C | 5416531、cc4c636 |
| T6-3 | A 已修；选 B，已修（第 3 层） | 03e0fec、28a55e6 |
| T1-1 | 标准库选 C，层内方向选 b | 091d3af |
| L-7 | 选 A | 5416531、43aca7b、c7fe6db、a3dc308 |
| T9-1 | 选 B | 43aca7b |
| L-1 | 选 A | 822eec6、43aca7b |
| L-2 | 选 A，条件放在 model 里共用 | 03e0fec、c7fe6db、cc4c636 |
| T5-1 | 选 B | 822eec6、5416531、cc4c636 |
| L-4 | 已修 | 822eec6、43aca7b |
| T4-1 | 已修 | 822eec6、5416531、43aca7b |
| T6-2 | 已修 | 822eec6、03e0fec |
| T7-4 | 已修 | 5416531 |
| T7-2 | 已修 | 822eec6、03e0fec、5416531、43aca7b |
| L-13b | 已修（注释） | 5416531 |
| T1-3 | 已修 | 091d3af |
| T1-2 | 已修 | 091d3af |
| T9-6 | 已修 | 43aca7b |
| T11-8 | 已修 | 822eec6、03e0fec、c7fe6db |
| T3-1 | 已修 | fc53fe6 |
| T8-1 | 已修 | 822eec6、fc53fe6 |
| T5-4 | 已修 | 822eec6、03e0fec |
| T5-5 | 已修 | fc53fe6、43aca7b、3572304 |
| T13-8 | 已修（随 L-7） | a3dc308 |
| T10-6 | 已修 | 43aca7b |
| T11-10 | 已修 | c7fe6db、cc4c636、a3dc308 |
| T9-4 | 已修（行为不改） | 822eec6、43aca7b |
| T2-1 | 已修 | fc53fe6 |
| T7-5（第 3 节） | 随 L-6 修掉 | 5416531 |

## 修补轮之后新发现的问题

2026-10-03，修补轮的跨任务评审给出以下 12 条建议（评审结论是「通过」，没有必须修改项）。它们都不阻塞合入 dev。其中两条要你拍板，十条并进第 2 层计划开头的清理任务。出处里的行号指 41ab33f。

结果：十条清理都已修，2026-10-03 在 `feat/store` 上一起做成 036f397（第 2 层计划任务 3）。每条的做法与测试名写在条目下的「结果」里，各条出处的行号修补后已经变了。P-1 当时未动；P-2 已在第 3 层选 A 并落实（28a55e6）。

**要你拍板（各自写明了最晚什么时候定）：**

- **P-1　锁定的 enum 取值不在 ISO 的集合里，审批和准入都查不到。** 模板把一个带 Offer 的 enum 锁定在写死的值或默认值上，而某个 ISO 的集合里没有这个值。这时激活判定和准入都放行，建任务时每台主机都报 param_not_offered，可它的下一步是「从 ISO 的选项里选一个」，锁定的格子又改不了。选项：A. 在 model 加一个共用判定，准入时把这种模板筛掉，问题报给模板作者；B. 保持现状，只把 admission 的包注释改准确。锁定且取自主机属性的格子遇到同样的情况，按 T11-8 的精神应改报 param_attr_unusable。最晚在第 5 层做向导之前定。（admission/admission.go:6-9、74-79；params/params.go:218-226）
- **P-2　快照构建不查执行插槽的时机该格式支不支持。** 草稿的 unattend 正文写了 `{{slot:x@post-target}}`（unattend 只支持 pre 与 firstboot），草稿脚本也声明了 post-target 时，Build 照样产出快照。选项：A. Build 补报 slot_timing_unsupported，作为渲染可以依赖的第四条不变式；B. 在 Build 的文档里写明这一条由渲染自己查。最晚在第 3 层开工前定。（snapshot/build.go:191-196）

  结果：选 A，已修（28a55e6）。Build 补报 slot_timing_unsupported；执行时机支持成为渲染可依赖的不变式，并拒绝重名插槽。

**并进第 2 层计划开头的清理任务：**

- **C-1** ISO 的系没勾时，准入在 series_not_ticked 之外还会多报一条 iso_offer_missing，参数求值只报前一条，两边一宽一严。改为系不符时跳过集合检查，并改测试 TestTemplateUsable几个原因一起报。（admission/admission.go:74-79）

  结果：已修（036f397）。准入判定在 ISO 的系没勾时只报 series_not_ticked，不再查 ISO 提供的集合，与参数求值、快照构建一致；TestTemplateUsable几个原因一起报 按「系没勾」与「系勾了、ISO 没提供集合」拆成两组，次序照旧固定。测试：TestTemplateUsableISO的系没勾时不查集合（同时核对 Batch 的 Common 与参数求值报的是同一条）。

- **C-2** 「整理参数引用与子值」在 activation 与 params 各写了一份（activation/hook.go:357-368 的 addRefs 与 params/params.go:284-296 的 refSubs 逐行相同），「被引用即必填」「非脚本的正文才展开」也各写了几处。收进 scan 或 model，只留一份。

  结果：已修（036f397），收进 scan 与 model。scan 的 Result.Refs 整理一份正文对参数的引用与子值，Refs.Merge 并起几份正文的引用，Refs.Required 判定「被引用即必填」；「非脚本的正文才展开」做成 model.Impl.Expanded。activation 的 addRefs、params 的 refSubs 删掉，激活判定、参数求值与快照构建都改调这几个。测试：TestResult的Refs整理参数引用、TestRefs的Merge、TestRefs的Required、TestImplExpanded。

- **C-3** 取值来源的字段键（tpl/、插槽名/序号/参数名）在 activation、params、snapshot 各手写一份，没有跨包测试钉住它们一致。由 params 导出字段键的构造函数，三处共用，或者至少加一条跨包测试（L-8 的一部分）。

  结果：已修（036f397），两样都做了。构造函数放在 model 而不是 params，因为依赖守卫不许 activation 依赖 params：TemplateFieldKey 给出 tpl/<参数名>，InstancePath 给出 <插槽名>/<序号>，InstanceFieldKey 给出 <插槽名>/<序号>/<参数名>，activation、params、snapshot 都改用它们。跨包测试钉住同一格在激活判定与参数求值里的 Subject.Path 相同。测试：Test字段键与实例位置的写法、Test同一格在激活判定与参数求值里标在同一处。

- **C-4** model.Subject 的文档注释没跟上 T9-1：Path 的取值要列全（param/n、impl/n、tpl/参数名、插槽名/n/参数名，以及 ISO 与主机的字段），第 5、6 层要按它解析。（model/problem.go:25-33）

  结果：已修（036f397），只改注释。model.Subject 的注释按修补计划 U6 的表列全了各处的写法：主机的 arch、firmware 与属性键；ISO 的 family、series、arch、version、name 与 offers/<集合名>；hook 的 param/<n> 与 impl/<n>；模板的 param/<n>、tpl/<参数名>、插槽名、<插槽名>/<n> 与 <插槽名>/<n>/<参数名>；参数求值的字段键；正文扫描只带行列。n 都从 1 起。

- **C-5** codes.go 里几处注释没写新的产出方：iso_offer_missing 现在也由准入产出，Subject 不同；series_not_ticked、impl_kind_mismatch、impl_timing_mismatch 现在也由参数求值或快照构建产出。

  结果：已修（036f397），只改注释。iso_offer_missing 补上准入判定，落在模板上，参数是第一个要这个集合的参数的显示名与集合名；series_not_ticked 补上参数求值与快照构建，三处的 Subject 都落在模板上；impl_kind_mismatch、impl_timing_mismatch 补上快照构建，Subject 与激活判定一样落在插槽里的实例上。

- **C-6** 准入时主机登记的平台不合法（比如固件写成 UEFI），仍拿去比平台，报出误导的 firmware_mismatch；应先报 Host.Validate 给出的 host_platform_invalid。优先级低。（admission/admission.go:140-153）

  结果：已修（036f397）。准入比平台之前先看 Host.Validate：登记的架构或固件不合法，这一项报它给出的 host_platform_invalid，不再拿去比；另一项合法的照常比，仍是架构在前、固件在后。原来「固件写成 iPXE 的上报值」期望 firmware_mismatch 的用例挪进了新测试，改为期望 host_platform_invalid。测试：TestISOUsable主机登记的平台不合法时报host_platform_invalid（含 Batch 的 ByHost）。

- **C-7** 状态为空串时，说明显示成「现为「」」。在 codes.go 登记「未知状态」的称呼，或在注释里写明空状态只会来自坏数据。优先级低。（model/registry.go:212-216）

  结果：已修（036f397），选了登记称呼。model 加 StateLabel：认识的状态给中文显示名，不认识的回落到标识，空状态回落到 codes.go 登记的 stateUnknown（「未知状态」）；iso_not_ready 与 template_not_approved 都改用它。测试：TestStateLabel、TestISOUnusable按状态给出原因、TestTemplateUsable不是已审批的版本不能用。

- **C-8** 「选项值是互不重复的安全词，显示名过 ValidLabel」在 model 里写了两份（param.go:131-148 与 registry.go:186-197），抽成一个函数。

  结果：已修（036f397）。抽成 model 的 validOptions，enum 的静态选项（ParamDecl.Validate）与 ISO 提供的集合（ISO.Validate）都调它，registry.go 里的 offeredOptionsOK 删掉。测试：Test选项的规则只有一份。

- **C-9** host_platform_invalid、placeholder_unknown_kind、script_placeholder_not_expanded 的说明里抄了目录取值和占位符种类，补进 codes_align_test。

  结果：已修（036f397），只加测试：逐项核对时说明与目录本来一致，codes.go 的说明没改。codes_align_test 加了三条：TestAlign主机平台的取值与目录一致（host_platform_invalid 列的架构、固件与 iPXE 的换算）、TestAlign脚本占位符说明里的变量名与EnvName一致、TestAlign说明里转义用的系统值每种格式都能用。placeholder_unknown_kind 列的占位符种类登记在 scan，model 的测试引用 scan 会成环，所以在 scan 里对齐：Test种类不认识的说明列出的种类与扫描认的一致。

- **C-10** 修补计划第 34 行说 T13-8 从清单第 3 节并入，实际在第 2 节。只改文字。

  结果：已修（036f397），只改文字：修补计划第 34 行改成第 2 节的 19 条都在本轮修，其中 T13-8 并进 L-7；第 3 节只并入 T7-5。

## 1. 需要你拍板

计划或 spec 本身有缺口、矛盾或取舍。每条列出可选项与代价，并给出建议。

### T13-10　草稿文本可含 NUL 与非法 UTF-8，第 2 层存不下

结果：选 A，已落实。model 提供 StorableText（合法 UTF-8、不含 NUL）与 ValidLabel，存不下的报新增的 text_not_storable（822eec6）；参数与选项的显示名按 ValidLabel 查，Option.Label 不能为空（03e0fec）；ISO 的名称与主机的属性值按 StorableText 查（5416531）；激活判定查 hook 的说明、每份实现的正文和模板正文（43aca7b）。保存时对全部自由文本统一拒收，留给第 5 层。spec §3 总则已改成「能存成 UTF-8 文本的草稿都能存」。测试：TestStorableText、TestValidLabel拒收不可见字符首尾空白与超长、TestParamDeclValidate查显示名、Test说明或正文存不下、Test模板正文存不下。

问题：全层只有名称（ident.go:83）、text 参数（param.go:450）和 SN 查合法 UTF-8 与 NUL。模板正文、hook 实现正文、Description、参数显示名 Label、选项显示名 Option.Label 都不查；ParamDecl.Validate 也不看两个显示名，Option.Label 可以为空（向导里选项没有文字），显示名可以含控制字符、零宽字符，也可以超长。

影响：后果都落在第 2 层。①快照经 encoding/json 存取时，非法字节被换成 U+FFFD，库里的 Body 与按原字节算的 Digest 对不上，机器侧 sha256sum 校验失败；预览用现算的快照、网关用读库的快照，两边正文不同，违反 C35。②PostgreSQL 的 text 与 jsonb 都不收 NUL，保存 hook、模板或快照会直接报数据库错误，而不是给出一条判定结果。经 JSON 接口进来的非法 UTF-8 会被 Go 解码成 U+FFFD，但 JSON 转义写法的 NUL 照样进来；按文件上传的 UTF-16 脚本（.ps1、.cmd）两样都带。spec 总则「草稿怎样都能存」与这两条存储限制冲突。

选项：

A. model 提供一个「可存文本」判定（合法 UTF-8、不含 NUL），第 5 层保存时对全部自由文本字段统一拒收，激活判定也对正文查一次；在 codes.go 末尾追加一个代码（如 text_not_storable）并登记进 extraCodes。显示名另在 ParamDecl.Validate 里按 NormalizeName 的规则查：非空的 Label 与每个 Option.Label 不含控制和不可见字符、不超过 64 字，Option.Label 不能为空，仍报 param_decl_invalid。代价：spec 总则要改成「能存成 UTF-8 文本的草稿都能存」，Windows 脚本只能以 UTF-8 上传；比计划严。

B. 第 2 层按字节存正文（bytea，快照里用 base64）。代价：存储与快照格式变复杂，渲染要先解码；含 NUL 的正文照样进不了应答文件和环境变量，问题只是推后。

C. 层 1 不管，只把「所有文本字段必须是不含 NUL 的 UTF-8」写进留给后续层的约定，由第 5 层在接口边界校验。代价：激活判定和快照构建自身不设防，规则分在两处；显示名的其余问题仍无人管。

建议 A。第 2 层开工前必须定。

出处：internal/core/activation/hook.go:281-303、activation/template.go:74-102、snapshot/build.go:147-171（正文都不查）；model/param.go:21-33、78-86（显示名不查）；model/ident.go:83、param.go:450（仅有的两处 UTF-8 检查）；spec §3 总则（第 146 行）、C35

合并自：任务13 should「跨层隐患，不属于本任务的改动范围：快照构建不检查正文是不是合法 UTF-8」；【T6-1】T6 concern「显示名没有校验（ParamDecl.Label 和 Option.Label 都没查）…」；【T6-1】T6 should「ParamDecl.Label 和 Option.Label 都没有校验，实现者已在关切里提出…」；【T6-1】T6 should「显示名完全不校验（实现者已提出）。探测实测…」

### T5-3　名称 key 挡不住变体选择符等不可见字符

结果：按你的决定落实，比选项 A 更严（822eec6）：模板名与 hook 名去掉首尾空白后只收 1–64 个可打印 ASCII（0x21–0x7E），空格、中文、全角字符、é 的两种写法、变体选择符和其他不可见字符一律报 name_invalid，key 是 ASCII 小写。只收 ASCII，合成与分解、全角字母这两类问题也一并没了，不用引入 x/text。activation 测试里用中文起的名字随之改成 ASCII。显示名仍可以用中文，另按 ValidLabel 查。spec C12 与原计划任务 5 已同步。测试：TestNormalizeName只收0x21到0x7E、TestNormalizeName拒收空白中文与不可见字符、TestNormalizeName大小写不同得到同一个key。

问题：invisibleRune 只拦 unicode.IsControl 与 Cf 类（ident.go:106-108）。变体选择符 U+FE0F、IVS U+E0101、组合字形连接符 U+034F（都属 Mn）和韩文填充符 U+3164（Lo）都能通过，名字看着一模一样，key 却不同；:103-105 的注释说能挡住这类字符，说过头了。合成与分解两种写法的 é、全角字母同理，而标准库没有 NFC/NFKC（golang.org/x/text 不在「只用标准库」之内）。

影响：第 2 层按 key 建唯一约束（C12），这些写法能绕过同名约束。key 一旦落库建了唯一索引，事后改规则要迁移旧数据，还可能撞出重复。

选项：

A. 收紧拒收：改成 unicode.In(r, Cc, Cf, Variation_Selector, Other_Default_Ignorable_Code_Point)，补 ident_test 用例。代价：带 VS16 的 emoji（如 ❤️）的名字会被拒；比计划任务 5「不含控制字符」严。

B. 只在算 key 时去掉这些字符，display 保留，不拒收。代价：key 不再是「display 的小写形式」，改了计划任务 5 的契约。

C. 规则不改，把注释收窄成「控制字符与格式字符」，接受 C12 只是近似唯一。

三种都挡不住合成/分解形式和全角字母，要彻底就得引入 x/text 或自带一份 NFKC 表。必须在第 2 层建唯一索引之前定。

出处：internal/core/model/ident.go:76-108（NormalizeName、invisibleRune）；spec C12（第 173 行）；计划任务 5 名称规则（第 272 行）

合并自：T5 should「ident.go:103-108 invisibleRune 的注释说要挡住…」

### L-6　C5「ready 必带版本与架构」在层 1 没有落点

结果：选 A，已落实；C（第 2 层约束）按你的决定不做。model 加 ISO.Validate（族、系、架构、版本、名称与提供的集合，并入 T7-5）与 ISO.Unusable，Usable 就是 Unusable 为空；版本规则改为 ^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$，. 与 .. 都不合法（5416531）。准入判定改用 Unusable，ISO 不可用时不再逐台比平台：ready 却缺架构的报 iso_incomplete，没登记平台的主机不再通过，只登记了架构的主机也不再收到参数为空的 firmware_mismatch，原来 :137 的提前返回删掉了（cc4c636）。第 4 层置 ready 之前调用 Validate。spec C5、原计划任务 7 与任务 12 已同步。测试：TestISOValidate版本的规则、TestISOUnusable可用却记录不全时给出Validate的问题、TestISOUsable可用却缺架构时报iso_incomplete。

问题：ISO.Usable 只看 State 是否为 ready（registry.go:68-71）。Version 的规则 [0-9A-Za-z._-]{1,32} 只写在注释里（:56），没有校验函数，照这条规则 . 和 .. 都合法。platformConflicts 遇到架构、固件都没登记的主机直接放行（admission.go:137），否则调 PlatformOK，而 ISO 的系或架构不合法时 PlatformOK 两项都判不通过。

影响：①ready 却没有合法架构的 ISO，能让没登记平台的主机通过准入，到引导时才失败；只登记了架构的主机会同时收到「ISO 的架构是 ，…」和参数为空的 firmware_mismatch，后者违反 C28「登记了才校验」。:137 的提前返回没有测试，删掉也全绿。②版本由第 4 层从外来的 ISO 内容里识别，测试数据里的制品路径（rocky/9.6/x86_64/…、windows-server/2025/x86_64/…）正是把版本当作一段路径；第 3 层的制品目录布局如果也这样用，.. 会造成目录穿越，. 会让不同的 ISO 落进同一个目录。spec C5 的后半句「导入完成的 ISO 必须带版本和架构」在层 1 没有落点。

选项：

A. 层 1 兜底：model 加 ISO.Validate（系与架构合法，版本首字符为字母或数字，即 ^[0-9A-Za-z][0-9A-Za-z._-]{0,31}$），第 4 层置 ready 之前调用；Usable 也要求它通过，ready 却不合法的 ISO 改报一个追加的代码（如 iso_incomplete），不再逐台比平台。代价：追加代码；改计划任务 7 的版本规则与 Usable 的定义。

B. 交给第 4 层：只收紧版本规则并导出校验函数，注释写明 C5 由第 4 层保证；platformConflicts 改为只报主机登记了的那一项。代价：坏数据一旦进库，没登记平台的主机照样通过准入，其余主机收到的冲突说明也不贴切。

C. 在 A 或 B 之外，由第 2 层加约束：state 为 ready 时 version、arch 非空。代价：第 2 层建表时就得定下来。

建议 A 加 C，并补一个「ready 但架构为空」的准入测试。要选 C 的话，第 2 层开工前定。

出处：internal/core/model/registry.go:49-71；admission/admission.go:25-40、133-149；catalog/platform.go:112-125；spec C5（第 157 行）、C28；计划任务 7 字段语义（第 367 行）

合并自：layerReview_should 第6条「ISO 架构的判定一宽一严」；任务12 should「admission.go:136-148 (platformConflicts): when a ready ISO has an empty or invalid Arch」；任务12 concern「如果 ISO 状态是 ready 却没有合法的架构或系（违反 C5…）」；【T7-3】T7 concern「第 4 层：ISO.Version 的规则 [0-9A-Za-z._-]{1,32} 只写在字段注释里…」；【T7-3】T7 should「registry.go:56、63-65：给第 4 层的约束写得不全…」（版本部分）

### T6-3　安全词可作 shell 选项，Windows 命令行另有陷阱

结果：A 已修（03e0fec）；选 B，已修（28a55e6，2026-10-09）。IsSafeWord 拒收以 - 或 @ 开头的 word/enum 值，int 负数不受影响；sn_unsafe_in_body 与说明对齐。S1-6 实际在第 2 层清单，同步以逐字符断言核实 ValidID ⇒ IsSafeWord。Windows 参数经环境变量与编码载荷传入，目标原生 ABI 仍归第 7 层。

问题：IsSafeWord 允许以 - 开头（--setopt=sslverify=0 能收下），int 也可以是负数；放进 shell 命令的参数位置（如 dnf install -y {{param:pkg}}）会被当成选项。Windows 侧：cmd.exe 里 ^ 是转义符、= 是批处理参数的分隔符；PowerShell 里 @ 开头会被当成展开，- 开头会被当成参数名。IsSafeWord 的文档（param.go:247-248）只提了 YAML 的陷阱。计划说安全词放进 shell 参数「不会改变结构」，这句对结构成立，对语义不成立；计划「风险」要求改字符集前逐格式确认，但没考虑 cmd 与 PowerShell，「留给后续层的约定」里也没写。

影响：操作员在向导里填的 word、enum 值，可以把模板里的一个参数变成选项（比如关掉校验）。第 3 层的 unattend 正文若把参数放进 <CommandLine>，在 cmd、PowerShell 的参数位置不安全。

选项：

A. 字符集不动，把这些陷阱写进 IsSafeWord 的文档和计划「留给后续层的约定」：第 3 层的内置模板里参数一律放在 -- 之后或加引号，unattend 不把参数放进 cmd、PowerShell 的参数位置。代价：靠作者自觉。

B. 收紧：word 与 enum 选项值不许以 - 或 @ 开头（int 的负数不受影响）；改 IsSafeWord 或 parseWord、相关测试，以及 sn_unsafe_in_body 的下一步。代价：以 - 开头的合法取值会被拒，这类取值很少。

C. 去掉 ^ 和 =。代价：会拒掉 console=ttyS0 这类常见的 word 取值。

按计划，改字符集前要在七种格式里逐一确认。建议 A 现在就做，B 在第 3 层写内置模板之前定。

出处：internal/core/model/param.go:239-259（IsSafeWord）；model/codes.go:286-289（sn_unsafe_in_body）；计划任务 6「安全词」（第 297 行）与「风险」（第 354 行）、「留给后续层的约定」（第 677-684 行）

合并自：T6 concern「安全词里的 ^ 和 = 在 Windows cmd.exe 里有特殊含义…」；T6 should「isSafeWord 的文档（param.go:244-245）只列了 YAML 的语义陷阱…」

### T1-1　依赖守卫的范围：标准库清单留有绕法，层内方向不查

结果：标准库选 C（白名单），层内方向选 b（只守关键不变式），已落实（091d3af）。白名单是下面核对出的 14 个包加 encoding/json、regexp；cgo 的 C、unsafe，以及 CgoFiles、IgnoredGoFiles 都算违规；读时钟的检查扩到 time 的 Now、Since、Until、Sleep、After、AfterFunc、Tick、NewTimer、NewTicker。方向：catalog 不依赖层 1，model 只依赖 catalog，判定包之间只许 snapshot 依赖 params。原计划「全局约定」第 1 条与任务 1 的守卫契约已同步。测试：Test守卫逐个文件的检查、Test层1包的文件都参与构建、Test总检查接上了每一项。

问题：①标准库：forbiddenStd 只有计划列的六个包（deps_test.go:28-35）；时间只认 time.Now（:189-198）；isStd 按「首段不含点」判断（:113-117），cgo 的伪包 C 和 unsafe 也算标准库。io/ioutil.ReadFile、os/user、os/signal、net/rpc、net/smtp、log/syslog、plugin、cgo，以及 time.Since、Until、Sleep、After、AfterFunc、Tick、NewTimer、NewTicker 都能绕过。②层内方向：checkFile 只分「层 1」与「层 1 以外」（:166-173），计划「包与文件」表规定的层内依赖方向没有任何检查，Go 编译器只拦得住环；admission 去 import scan、activation 去 import params 这类不成环的越级依赖会悄悄进来，正是计划「为什么这样切」想避免的。已用 go list 核对 HEAD：7 个包的非测试代码只用 14 个标准库包（cmp、errors、fmt、maps、slices、strconv、strings、unicode、unicode/utf8、crypto/sha256、encoding/base64、encoding/binary、encoding/hex、net/netip），层内依赖都在表内（params 实际没 import catalog），CgoFiles、IgnoredGoFiles 都为空，下面任何一种收紧都不会让现有代码变红。

影响：spec §4.1、§6.1 要求层 1 和渲染不碰时钟、文件和网络，守卫是唯一的机械检查，上面的绕法只能靠评审发现。第 2 层起 archtest 还要按 spec §6.1 加模块间规则，第 3 层渲染大概率复用这份清单。

选项（标准库）：

A. 维持计划字面。

B. 扩黑名单：加 io/ioutil，按前缀禁 os/* 与 net/*（放行 net/netip、net/url），再加 log/syslog、database/sql/driver、plugin、C、unsafe；时间检查扩到上面八个函数。代价：约十行；仍不完整，path/filepath 的 Glob、Walk，crypto/x509 读系统证书都还能碰文件。

C. 改白名单：只许上面 14 个，加计划技术栈里的 encoding/json、regexp；要 time.Time 时再放进 time 并保留函数级检查；顺带要求 CgoFiles、IgnoredGoFiles 为空（T1-3 的文件名陷阱也能当场发现）。代价：以后每加一个标准库 import 都要改守卫，评审时会被看到；math/rand、crypto/rand 这类不确定来源也一并挡住。

选项（层内方向）：

a. 把「包与文件」表写成守卫里的白名单 map，越界就报。代价：约 15 行，以后新增一条合理的层内依赖要同时改守卫和计划。

b. 只守关键不变式：catalog 不依赖层 1 任何包，model 只依赖 catalog，判定包之间除 snapshot→params 外互不依赖。代价：scan、activation 的细节方向不受保护。

c. 不做。

不论选哪种，都要改计划「全局约定」第 1 条和任务 1 的守卫契约，新规则进守卫自测（T1-2）。建议标准库选 C、方向选 a，排在 T1-2 之后，可以和第 2 层的模块间规则一起做。

出处：internal/archtest/deps_test.go:28-35、113-117、166-173、189-198；计划「全局约定」第 1 条（第 57 行）、任务 1 守卫契约（第 98-102 行）、「包与文件」表（第 34-46 行）与「为什么这样切」（第 48-53 行）；spec §4.1、§6.1

合并自：T1 concern：守卫只按计划禁了 time.Now；T1 concern：按计划「路径第一段不含点就算标准库」的规则；T1 should：守卫只查计划列出的那几项；T1 should：internal/archtest/deps_test.go:28 与 :191；【T1-4】T1 should：（计划外，由计划作者决定）internal/archtest/deps_test.go:171；【T1-4、L-11】层评审 should 第 11 条「依赖守卫只拦层外依赖和六个标准库包」（两组各报一次）

### L-7　快照构建与参数求值的前提检查比激活宽

结果：选 A，已落实。插槽适配收成 model.Impl.FitsSlot 一份（5416531），激活判定与快照构建都调它（43aca7b、a3dc308），snapshot 不依赖 activation，也没有复制检查；参数求值先查系，没勾时只报一条 series_not_ticked（c7fe6db）；Build 补查系的合法性、插槽适配与查找结果（a3dc308，并入 T13-8）。渲染可以依赖的三条不变式写进了 Build 的文档注释和原计划「留给后续层的约定」，原计划任务 11、13 已同步。测试：TestEvaluateISO的系没勾时只报一条series_not_ticked、TestBuild实现放不进插槽时不产出快照、TestBuild前提不满足时不产出快照。

问题：Build 只先查 series_not_ticked（build.go:36-40），其余前提靠 Evaluate 报 hook_ref_missing、impl_missing_for_series。下面这些草稿（C22、spec §5.2 第 4 步允许预览）Build 照样产出快照：片段放进执行插槽，执行插槽里出现 Kind=fragment、没有 Digest 的实例；脚本放进片段插槽，渲染会把脚本正文贴进应答文件；执行插槽的时机不在脚本声明的时机里；实现种类不认识；勾了不合法的系，Family、Format 为空也不报。Evaluate 也不先查系有没有勾上：ISO 的系没勾时，每个实例各报一条 impl_missing_for_series，准入和 Build 报的却是一条 series_not_ticked。

影响：已审批的模板都过了激活判定，正式建任务不受影响；受影响的是草稿预览（渲染要么出错，要么产出错的文件），以及提交时多出的报错噪声。第 3 层渲染要知道自己能依赖哪些不变式。

选项：

A. Build 补查 impl_kind_mismatch、impl_timing_mismatch（代码都已登记，Subject 沿用「插槽名/序号」）和系的合法性，Evaluate 也像 Build 一样先查 series_not_ticked。渲染就能依赖三条：执行插槽里只有带摘要的脚本，片段插槽里只有片段，格式非空。代价：依赖表不许 snapshot import activation，要把这几项检查挪进 model 共用，或复制一份；改计划任务 11、13 的契约。

B. 层 1 不改，把这三条不变式写进「留给后续层的约定」，由第 3 层渲染时报错。代价：有效性的定义分在两处，预览给出的是渲染错误而不是问题列表。

C. 第 5 层预览草稿时先跑 activation.Template，把激活问题和预览一起返回。代价：Build 本身仍不保证这些不变式。

建议 A，第 3 层开工前定。Build 丢掉查找结果的问题（T13-8）是同一方向上更小的一步，可以先做。

出处：internal/core/snapshot/build.go:22-44、147-171；params/params.go:60-128；计划任务 13 接口契约（第 664-669 行）

合并自：layerReview_should 第7条「快照构建与参数求值的前提检查比激活宽」；任务13 should「小问题：ISO 的系如果目录里不认识」；任务11 concern「Plan sequencing note: Evaluate does not check series_not_ticked」

### T9-1　Subject.Path 定位不到唯一的实现、声明与取值来源

结果：选 B，已落实（43aca7b）。实现为 impl/<n>，参数声明为 param/<n>，模板参数的取值来源为 tpl/<参数名>，实例参数的取值来源为「插槽名/n/参数名」，与 params.FieldKey 对齐；系与参数名只出现在说明里。完整的对照写在原计划任务 9、10（已同步），第 5、6 层按它解析。测试：TestSubject指向出问题的位置、Test模板问题的Subject、Test实例取值来源的问题按参数分开。

问题：三处 Path 会撞。①实现的 Path 是它勾的系，以逗号连接（hook.go:57）：两份实现勾了同一组系，或都还没勾系（草稿里新建两份实现时 Path 都是空串），各自问题的 Subject 完全相同，只差行列。②参数声明的 Path 是参数名（hook.go:82，模板沿用）：重名的两条相同，空名的为空。③实例取值来源的问题一律落在「插槽名/序号」（template.go:205-206）：attr_key_invalid 的说明里只有属性键，同一实例的 disk 和 disk2 写了同一个坏键，得到两条逐字节相同的问题；而模板参数的取值来源以参数名为 Path，params 的字段键是「插槽名/序号/参数名」。

影响：Subject 的用途是让前端把错误标到具体的行或单元格（problem.go:25），这几种情形前端只能猜，标进第一份实现或整个实例。第 5、6 层一旦按现有格式解析，就不好再改。

选项：

A. 保持计划任务 9、10 的写法，接受这几种情形定位不准。

B. 统一用位置：实现用它在 Impls 里的序号，参数声明用「param/序号」（都从 1 起），实例取值来源用「插槽名/序号/参数名」，与 params.FieldKey 对齐；系与参数名只留在说明里。代价：改计划任务 9、10 的 Subject 约定、Hook 与 Template 的文档注释和 Subject 测试；日志里不再直接看出是哪组系。

C. 只在冲突时（重名、系列表相同或为空）附加序号。代价：格式不统一，前端要处理两种写法。

建议 B，在第 5、6 层固定 Subject 格式之前定。

出处：internal/core/activation/hook.go:36-41、57、75-97；activation/template.go:14-23、205-206；model/problem.go:25-33；params/params.go:19-22；计划任务 9 Subject（第 494 行）、任务 10 Subject（第 541 行）

合并自：T9 偏离：Hook-level param_decl_invalid problems set Subject.Path to the parameter name；T9 建议：hook.go:71-72 把 hook 级 param_decl_invalid 的 Path 设为参数名；T9 建议：Subject 区分不了实现，也区分不了重名的参数声明；【T10-3】T10 建议：template.go:206：实例取值来源的问题，Subject.Path 一律是「插槽名/序号」；【L-10】layerReview_should 第10条「实例取值来源的问题不指向参数」

### L-1　脚本正文里的 lober 占位符激活时不查

结果：选 A，已落实。登记 script_placeholder_not_expanded，下一步指向 LOBER_PARAM_*（822eec6）；checkScript 扫描脚本正文，slot、lober、sys、param 四种占位符（写错的同种类也算）各报一条，参数是占位符原文，带行列，其余 {{…}} 与 Jinja、Go 模板的写法不报（43aca7b）。原计划任务 9 已同步。测试：Test脚本里写了lober的占位符。

问题：checkScript 只核对字面出现的 LOBER_PARAM_*，不扫描正文。脚本里写 {{param:disk}}、{{sys:gateway}}、{{slot:x}}，激活判定一条问题都不报。脚本按摘要原样下发、不展开占位符，整层修正也正是据此定下「脚本不查 {{sys:sn}}」，build_test 里有用例锁定。

影响：这些占位符永远不会被替换，作者以为拿到了参数，到装机时才出错。计划的约定是脚本只经 EnvName 给出的环境变量拿参数。

选项：

A. checkScript 对正文跑一次 scan，认出的 slot、lober、sys、param 四种占位符（写错的同种类也算）一律报出；在 codes.go 末尾追加一个 incomplete 代码（如 script_placeholder_not_expanded），说明里指向 LOBER_PARAM_*，登记进 extraCodes。代价：脚本里不能再字面写出这四种「{{种类:」，确需时只能拆开拼（脚本不展开，{{sys:lbrace}} 在这里不起作用）；Jinja、Go 模板这类写法不受影响。

B. 复用 fragment_forbidden_placeholder 或 param_undeclared。代价：说明对不上，前者写的是「片段里不能写」。

C. 不查，只在 hook 编辑器里提示。代价：错误拖到装机时才暴露。

建议 A。它与整层修正不冲突，因为 Build 不重做激活判定。

出处：internal/core/activation/hook.go:281-303（checkScript）；snapshot/build.go:86-87；计划「留给后续层的约定」第 4 条（第 684 行）

合并自：layerReview_should 第1条「约定第 4 条的前提没有查：activation/hook.go:283 的 checkScript…」

### L-2　准入不查 ISO 提供的集合，候选与提交不一致

结果：选 A，已落实，条件只在 model 里写一份：ParamDecl.OfferMissing 与 MissingOffers（03e0fec）；参数求值报 iso_offer_missing 改用它（c7fe6db）；TemplateUsable 在 ISO 可用时查模板参数与所引 hook 的参数，缺的集合各报一条 iso_offer_missing，Subject 为模板，包注释也改准确了（cc4c636）。spec §4.1、§5.3 第 3 步与原计划任务 12 已同步。测试：TestOfferMissing、TestTemplateUsableISO没提供模板参数要的集合、TestTemplateUsableISO没提供hook参数要的集合。

问题：iso_offer_missing 只由 params.Evaluate 报（params.go:175-178）；TemplateUsable 只查三项：已审批、没有废弃引用、勾了 ISO 的系（admission.go:53-66）。ISO 不提供 editions 时，要 editions 的 Windows 模板照样进入向导第 3 步的候选，到第 4 步每台主机都报 iso_offer_missing。admission 的包注释（:1-4）说「筛出来的候选和提交时的校验结果不会对不上」，可提交时还要跑参数求值，这句话说过了头。

影响：向导里出现注定用不了的候选。spec §4.1 把 C21 归给参数求值，§5.3 第 3 步列的筛选条件里也没有这一项，所以加进准入等于扩大 spec 和计划任务 12 写死的三项检查。

选项：

A. TemplateUsable 检查模板参数，以及装配表里各实例所引 hook 的参数：enum、Offer 非空、而 iso.Offers 里对应集合为空的，报 iso_offer_missing，Subject 指向模板。只凭 t、hooks、iso 就能查，不用 scan；对已审批的模板，结果与 Evaluate 一致。代价：同一条规则在 admission 和 params 各写一份（也可以在 model 里加一个共用判定）；要改 spec §5.3 和计划任务 12。

B. 规则不动，只把包注释改准确，接受到第 4 步才报。

建议 A，在第 5 层做向导之前定。

出处：internal/core/admission/admission.go:1-4、53-66；params/params.go:158-179；spec §4.1（第 219 行）、§5.3 第 3 步（第 287 行）；计划任务 12

合并自：layerReview_should 第2条「准入与参数求值在 ISO 提供的集合上一宽一严」

### T5-1　说明参数传标识还是显示名未定，状态显示成英文

结果：选 B，已落实。codes.go 的头注释定下目录值传标识、状态传中文显示名；iso_not_ready 的参数改成状态的显示名，导入失败另报新增的 iso_import_failed，下一步是重新导入（822eec6）；ISOState、VersionState 有 Label 与 Valid，显示名照 catalog 的做法登记成表（5416531）；准入判定改传 Label，不认识的状态回落到标识（cc4c636）。测试：TestISOState的显示名、TestVersionState的显示名、TestISOUsable不可用的ISO按状态报原因、TestTemplateUsable不是已审批的版本不能用。

问题：codes.go 头注释（:15-18）允许参数是「标识、名字或显示名」，没有定死。实现里一律传标识：activation 的系、格式、时机、解释器（如「系统值「smb」在 esxi-ks 格式里不可用」），admission 的系、架构、固件，以及两种状态。iso_not_ready 的参数注释（:252）写的是中文现状（获取中、导入中、导入失败、已废弃），代码却传 string(iso.State)（admission.go:127-131），template_not_approved 也传英文状态（:57）。ISOState、VersionState 没有显示名，计划也没要求。

影响：界面会出现「ISO 不可用：deprecated」「（现为 review）」，State 为空时是「ISO 不可用：」。操作员在下拉框里看到 Rocky Linux、UEFI，在准入说明里看到 rocky、uefi。另外 iso_not_ready 的下一步「等它导入完成后再用」不适用于 failed：spec §5.1 第 5 步说导入失败的可以直接重新导入。层内现在是一致的，风险在后续层追加代码时各选一种。

选项：

A. 全部传标识：只改头注释和这两条的参数注释。代价：说明里中英混排，状态也是英文。

B. 目录值传标识（作者在占位符里写的就是它，激活报错好对照），状态传中文：给 ISOState、VersionState 加 Label()，照 catalog 的做法登记成表并测「每个常量都有显示名」；admission 改传 Label()，为空或不认识时回落标识。要改 model、admission.go，以及 admission_test 里期望的状态参数。

C. 全部传显示名：在 B 之外，activation、admission 也改传 catalog 的 Label()。不合法的值 Label 是空串，必须回落标识（任务 12 正是因此选了标识），改动面最大。

D. 按读者分：激活判定（读者是作者）传标识，准入（读者是操作员）传显示名。代价：同一个系在两处写法不同。

不论选哪项，iso_not_ready 的下一步都要把 failed 单独说清。建议 B，在第 5、6 层之前定。

出处：internal/core/model/codes.go:15-18、252-259；admission/admission.go:57、127-131、143、146；activation/hook.go:218、295、297；model/registry.go:38-47；model/authoring.go:12-21；spec §5.1 第 5 步（第 269 行）

合并自：T5 deviation「template_not_approved takes one argument, the current state…」；T5 should「codes.go:252：iso_not_ready 的参数注释写的是「现状…」」（状态部分）；T5 should「codes.go 的参数注释没有定死目录值和状态该传标识还是显示名…」（前半）；T7 concern「任务 12：iso_not_ready 要传一个参数，说明 ISO 的现状…」；T7 should「任务 12 开工前需要先定下来（这是计划的缺口…）」；T7 should「registry.go:39 与 authoring.go:14：ISOState 和 VersionState 都没有显示名…」；【T9-2】T9 偏离：Arguments are identifiers, not display labels；【T9-2】T9 顾虑：sysvalue_not_allowed shows the format id；【T9-2】T9 建议：hook.go:163 sysvalue_not_allowed 传的是格式标识；【T9-2】T9 建议：Message 里的参数用的是标识而不是显示名；【T12-5】任务12 deviation「iso_not_ready 的参数传的是 ISO 状态的标识」；【T12-5】任务12 deviation「Subject 的约定（计划没写死）」（参数传标识的部分）；【T12-5】任务12 concern「iso_not_ready 在界面上会显示英文状态标识」；【T12-5】任务12 should「iso_not_ready 的参数和 codes.go 的注释对不上」；【T12-5】任务12 should「admission.go:57 and :130: messages shown to users carry English state identifiers」；【T12-5】layerReview_should 第5条「同一格的说明对参数的称呼不一致」（后半：状态标识）

## 2. 第 2 层开工前应修

实际的缺陷或保护缺口。按计划和 spec 能直接判断，不需要你拍板，只需你同意修。

### L-4　激活放行了必然通不过求值的取值来源

结果：已修。登记 param_locked_without_value（822eec6）；激活判定对 fixed 值、default 来源（含没有 Binding 的）取到的默认值，以及被 hook 自己的片段取了子值的参数默认值，逐个调 Value.Sub，取不出报 param_accessor_invalid；必填参数锁在默认值上、声明里又没有默认值的报 param_locked_without_value（43aca7b）。原计划任务 9、10 的判定表已同步。测试：Test取值来源取不出正文或片段要的子值（含 IPv4 配 .netmask、IPv6 配 .addr 与 .prefix 两个正例）、Test锁在默认值上却没有默认值、Test参数的默认值取不出片段要的子值。

问题：checkBindings 对 fixed 值只查 Parse，对 {Source: default} 什么都不查（template.go:221-231）；默认值只过 defaultOK；正文或片段取的子值只用 IPv4 样例按类型检查（hook.go:269-279）。于是两类模板能通过审批，建任务时却每台主机都报错：①fixed 值或默认值是 IPv6 的 cidr，正文或所选片段又取了 .netmask，每台都报 param_accessor_invalid，格子锁定时永远建不了任务，不锁时每台都得手动改；②参数被正文或所选片段引用（或是脚本的 Required 参数），Binding 为 {Source: default, Editable: false}，声明里却没有默认值，每台都报 param_required，而格子锁着填不了。spec C20「最后仍然没有值的必填参数，向导里每台主机都得填」与锁定互相矛盾，计划任务 10 的判定表没有这一项。

影响：已审批的模板注定用不了，要到建任务时才发现。这两类都与主机无关，激活时就能判定；按激活判定「审批前是否完备」的定位，应在这里拦下，不需要另行拍板。

建议：template.go 查取值来源时，对 fixed 值和 default 来源（含没有 Binding 的）取到的默认值，按正文（模板参数）或每个所勾系选中的片段（实例参数）里引用的子值逐个调 Value.Sub，失败报 param_accessor_invalid；锁在 default、没有默认值、而该参数按 Evaluate 的规则必填时报一条，建议在 codes.go 末尾追加专用的 incomplete 代码（借 param_required 也行，但它的下一步前半句是写给操作员的）。hook.go 同理，hook 参数的默认值被它自己的片段取了子值时也用 Sub 查。Subject 沿用现有约定。取自主机属性的来源要到求值时才能查，不在此列（措辞问题见 T11-8）。补测试：两类各一个反例，另加 IPv4 默认值配 .netmask、IPv6 配 .addr 与 .prefix 两个正例。

出处：internal/core/activation/template.go:209-250（checkBindings）、hook.go:77-97、269-279（subOK）；params/params.go:184-219；spec C20（第 184 行）；计划任务 10 判定表

合并自：layerReview_should 第4条「激活对 fixed 值与默认值比参数求值宽」（第 (1) 点：IPv6 配 .netmask）；任务11 should「跨任务提醒（不属于本任务）：锁定的字段如果取值不合法，或没有值」（后半：锁在默认值上却没有默认值）；【T10-2】T10 建议：计划缺口，不在任务 10 的表里（template.go:222）

### T4-1　主机的架构与固件登记值没人拒收

结果：已修。登记 host_platform_invalid（822eec6）；model 导出 ValidAttrKey，Host.Validate 改用它，并查非空的架构、固件是否合法，以及属性值存不存得下（5416531）；checkBindings 改用 ValidAttrKey，不再造 model.Host（43aca7b）。原计划任务 7 那句已同步。测试：TestValidAttrKey、TestHostValidate拒收不合法的架构与固件。

问题：Host.Validate 照计划任务 7「只查属性键」，层 1 没有任何地方拒收非空却不合法的 Host.Arch、Host.Firmware（如 CSV 里写成 UEFI、amd64，或存了 iPXE 上报的 i386、pcbios），代码表里也没有对应的代码。PlatformOK 把这类值判为不通过（这个取舍是对的），于是建任务时每个 ISO 都报 arch_mismatch 或 firmware_mismatch，说明写成「系「rocky」不支持用 UEFI 固件引导」，真实原因却是登记写错了。

影响：spec 总则要求保存时校验不过就拒绝并说清原因、不做静默降级，§4.5 要求控制台不写业务规则，全局约定又要求中文只写在 codes.go，所以第 5 层不动层 1 就做不好单台登记和 CSV 导入。方向由 spec 定死，不需要另行拍板，只需同步改计划任务 7 那句。

建议：

1. 先解耦：activation 的 checkBindings 造一个 model.Host{Attrs: {键: 空串}} 借 Validate 查属性键，并收下它的全部问题（template.go:223-229）。Host.Validate 一旦多查别的字段（比如 SN），每条 attr 绑定都会多出一条无关的问题，所有带 attr 绑定的模板都激活不了。改为 model 导出 ValidAttrKey(key) bool，保留键表仍只在 model 一处，Host.Validate 与 checkBindings 都用它。

2. Host.Validate 增加：非空的 Arch、Firmware 必须 Valid()；在 codes.go 末尾追加一个 invalid 代码（如 host_platform_invalid，参数为字段与取值，下一步写明合法取值、区分大小写），登记进 extraCodes，Subject 为 {host, SN, arch 或 firmware}。

3. 补测试：大小写不对、iPXE 上报值被拒，空值照常通过。

aarch64 配 bios 这类组合，计划和 spec 都没有规则，不加。大小写与别名的规范化（UEFI→uefi）是第 5 层导入时的便利，另议。

出处：internal/core/model/registry.go:21-36（Host.Validate）；activation/template.go:223-229；catalog/platform.go:112-125；model/codes.go:281-290（追加位置）；计划任务 7（第 366、393 行）；spec §3 总则（第 146 行）、C28、§4.5

合并自：T4 should：需要转给任务 7 和第 2、5 层，本提交不用改；T4 concern：PlatformOK 不拒绝 aarch64 配 bios 这种实际不存在的组合；【T7-1】T7 concern「第 5 层：Host.Validate 按计划只查属性键…」；【T10-5】T10 建议：template.go:226：校验 attr 键的方法是造一个 model.Host；【T10-5】T10 偏离：attr binding keys are checked with model.Host.Validate（实现手法部分）

### T6-2　sshkey、pwhash 与列表不限长，导出环境变量会失败

结果：已修。Parse 限定公钥主体解码后不超过 16384 字节、pwhash 不超过 256 字节、列表规范形式合计不超过 32767 字节（03e0fec），说明与下一步同步改了（822eec6）。原计划任务 6 已同步。测试：TestParse公钥主体不超过16384字节、TestParse口令散列不超过256字节、TestParse列表规范形式合计不超过32767字节。

问题：parseSSHKey 只限注释（安全词，至多 128），base64 主体不限长；parsePWHash 不限长；列表只限 256 项、不限总长。照代码读，1.4 MB 的 ssh-rsa 公钥、2 MB 的 $6$ 散列都能通过，256 把 RSA-4096 公钥的规范形式约 189 KB。

影响：脚本经 EnvName 的环境变量拿参数（计划「留给后续层的约定」第 4 条）。Linux 单个环境变量串的上限 MAX_ARG_STRLEN 是 131072 字节，超出时 execve 报 E2BIG；Windows 单个环境变量最多 32767 个字符。错误要到装机执行脚本时才出现，向导里拦不住，违反 spec 总则「不做静默降级」。

建议：在 Parse 里按外部限制设上限，并同步 paramValueFormats 的说明：sshkey 的 blob 不超过 OpenSSH 的 SSH_MAX_PUBKEY_BYTES（16384 字节，base64 约 21848 字符）；pwhash 不超过 256 字节（最长的 yescrypt 散列也远小于它）；列表规范形式的总长不超过 32767 字节（受限类型只含 ASCII，字节数就是字符数，Windows 与 Linux 都放得下）。超限报 param_value_invalid，补边界测试。上限都来自外部限制，不需要另行拍板；若以后要按格式放宽 Linux 的列表上限，Parse 得先知道格式，现在没有这个入参。

出处：internal/core/model/param.go:150-174（parseItems）、360-386（parseSSHKey）、422-442（parsePWHash）；model/codes.go:312-327（paramValueFormats）

合并自：T6 should「sshkey 与 pwhash 没有长度上限（param.go:359、421）…」

### T7-4　落库类型没有 json tag，改字段名会静默丢数据

结果：已修，不加 json tag（5416531）。golden_test.go 把 Snapshot、Template、Hook、Host、ISO、Task 的 JSON 编码钉在 testdata/*.golden.json 里，改了字段名立刻报红；-update 可以重写期望文件，什么时候能这样做写在文件头。原计划任务 7 的风险一节已同步。测试：TestGolden落库类型的JSON编码。

问题：照计划「字段名就是最终的名字」，全层没有一处 json tag（已 grep）。第 2 层按 JSON 存 Snapshot（计划任务 13），也可能这样存 Template、Hook 的 Assembly、Bindings、Params，键名就是 HookID、TimeoutSec 这类 Go 字段名。TestTask连同快照经JSON存取后不变（task_test.go:36）只做往返，字段改了名照样通过，只会逼人改测试里的字段名。

影响：以后谁在 Go 里改了字段名，库里旧数据解码时这个字段静默变成零值，不报错。计划任务 7 的风险一节已点出「一旦存进数据库就不好改」，但没有测试守着。

建议：维持计划，不加 tag；在 model 加 golden 测试，把 Snapshot（以及第 2 层确定按 JSON 存的其他类型）的 JSON 编码钉成一份期望字节，改名立刻报红。若第 2 层的计划决定另建持久化 DTO，golden 测试随之挪到第 2 层。第 2 层写 schema 之前做。

出处：internal/core/model/registry.go、authoring.go、param.go、task.go（全层无 json tag）；model/task_test.go:36-66；计划任务 7 风险（第 399 行）、任务 13（第 669 行）

合并自：T7 deviation「字段的 Go 类型由我定：…结构体都没加 json tag，字段名原样作为键」

### T7-2　取值类型没有 Valid()，不认识的值借用不贴切的代码

结果：已修。ParamType（03e0fec）与 ImplKind、BindingSource、ISOState、VersionState、TaskState（5416531）都有 Valid()；登记 impl_kind_unknown、binding_source_unknown（822eec6），激活判定改报它们（43aca7b）。测试：TestParamTypeValid只认十种类型、TestAuthoring取值类型的Valid只认各自的常量、TestISOStateValid只认五个状态、TestTaskStateValid只认八个状态、Test实现的种类不认识、Test取值来源不认识。

问题：ImplKind、BindingSource、ISOState、VersionState 都没有 Valid()，catalog 的类型都有。不认识的值已经挡得住审批：Impl.Kind 为空串或 macro 时报 script_incomplete（hook.go:122-126，说明是「正文为空，或者没有声明解释器、执行时机」）；Binding.Source 不认识（含零值，比如 JSON 里漏了 source）时对空串调 Parse，报 param_value_invalid（template.go:232-235，说明是「应为<格式>」）；不认识的状态在准入里算不可用或未审批。只是说明都没点出真正原因。这些值界面上选不出来，只会来自直接调接口或库里的坏数据。实现者说「不许改 codes.go」不准确：计划「并行执行」允许在末尾追加。

影响：第 2 层要给这些列加 CHECK 约束、第 5 层要在保存时拒收（草稿不经激活就能保存），都得自己再抄一份取值表。

建议：给这四个类型（顺带 TaskState、ParamType）加 Valid()，测试每个常量都 Valid、未知值都不 Valid，第 2、5 层只用它。再在 codes.go 末尾追加 impl_kind_unknown、binding_source_unknown 两个 incomplete 代码，activation 改报它们并登记进 extraCodes（d45fbf3 之后只需 codes.go 一条加 problem_test.go 一行）。Valid() 要在第 2 层建表之前加上。

出处：internal/core/model/authoring.go:12-21、30-36、108-115；model/registry.go:38-47；model/task.go:5-18；activation/hook.go:117-126；activation/template.go:232-236

合并自：T7 should「authoring.go:30-36、108-124，以及 ISOState、VersionState：这些取值类型都没有 Valid()…」；【T9-3】T9 偏离：An impl whose Kind is neither fragment nor script is reported as script_incomplete；【T9-3】T9 建议：hook.go:112-115 种类不认识的实现报 script_incomplete（种类部分）；【T9-3】T9 建议：种类不认识的实现（Kind 为空串或 macro）报 script_incomplete（种类部分）；【T9-3】T10 偏离：A Binding.Source that is none of fixed, attr or default；【T9-3】T10 顾虑：For an unknown Binding.Source, the param_value_invalid text；【T9-3】T10 建议：template.go:232-236 不认识的 Binding.Source（含零值）；【T9-3】T10 建议：template.go:232-235：来源不认识的 Binding

### L-13b　Task.ID 经 {{sys:task}} 进正文，未约束为安全词

结果：已修（5416531）。Task.ID 的注释写明它经 {{sys:task}} 原样代入正文，必须是安全词，由第 2 层生成时保证；第 2 层的契约测试据此断言。原计划任务 13 已同步。

问题：系统值 task（任务号）在七种格式里都能引用，渲染按 C18 原样代入；model.Task.ID 只是一个 string，哪里都没写它必须是安全词。快照里不带任务号，Build 查不到。整层修正刚给 SN 补上了同类约束（sn_unsafe_in_body）。

影响：第 2 层若选了含空格、引号的 ID 生成规则，会直接改变应答文件的结构。

建议：在 Task.ID 的注释里写明「经 {{sys:task}} 原样代入正文，必须是安全词（model.IsSafeWord），由第 2 层生成时保证」，第 2 层的契约测试据此断言。只改文档，第 2 层开工前补上。repo、gateway 这类地址由第 3、7 层拼出，另当别论。

出处：internal/core/model/task.go:31-46；catalog/timing.go:72、87-158

合并自：layerReview_should 第13条「快照只存子值的原料，没给取法」（后半：Task.ID 经 {{sys:task}} 进正文）

### T1-3　全部文件被构建约束排除的层 1 包会被守卫跳过

结果：已修（091d3af）。守卫遍历 internal/core，含非测试 .go 文件的目录不在 go list 结果里就报错，注释改成了实际覆盖的范围；按 T1-1 另要求 CgoFiles、IgnoredGoFiles 为空。测试：Test含源文件的目录都要出现在go_list里、Test只收集含非测试源文件的目录、Test层1包的文件都参与构建。

问题：守卫的检查对象来自 go list -json ./...（deps_test.go:92），通配模式会静默跳过「所有文件都被当前平台的构建约束排除」的目录：cmd/go 把它当作没有包，不报错。IgnoredGoFiles 只覆盖「包里至少有一个文件在当前平台编译」的情况。所以只含 x_linux.go 的层 1 包在 Windows 上根本进不了检查，评审在探针里复现过（新建 internal/core/probeonly/x_linux.go 并 import os，守卫 PASS）；按只读约束我没有重做探针，读代码并用 git log 确认守卫自 6fa208c 没改过。:134-135 的注释「只在 Linux 上编译的文件，在 Windows 上跑守卫时同样要守规矩」说过头了。make check 的 cross 只编译 Linux 版、不跑测试，这个口子在本机流程里一直开着。

影响：族标识 linux、windows 恰好是 GOOS 名，第 3、4 层往 catalog 加按族命名的文件（如 repo_linux.go）会被 Go 当成平台约束；整包都这样命名时，它在 Windows 上就从守卫里消失了。

建议：trackForTestCache 本来就在 WalkDir internal/core，顺手收集含非测试 .go 文件的目录（跳过 testdata 以及 . 和 _ 开头的目录），有目录不在 go list 结果里就报错；注释改成实际覆盖的范围。要不要进一步要求 CgoFiles、IgnoredGoFiles 为空，归守卫范围那条决策（T1-1）。和 T1-2 一起做。

出处：internal/archtest/deps_test.go:90-107（go list ./...）、119-132（trackForTestCache）、134-135（注释）

合并自：T1 should：internal/archtest/deps_test.go:134（另见 :92）

### T1-2　依赖守卫自身没有自动化反例测试

结果：已修（091d3af）。guard_test.go 用内存里的源码测守卫的每条规则，覆盖建议里列的全部情形和 T1-1 的新规则；守卫的组合逻辑抽成只吃输入的 checkLayer1，组合本身也有测试。测试：Test守卫逐个文件的检查、Test违规信息写出位置、Test总检查接上了每一项。

问题：archtest 只有 Test层1依赖守卫 一个测试，checkFile、inLayer1、isStd 对不对，只靠 6fa208c 提交时那一次手工证伪（计划就是这么规定的）。树现在是干净的，守卫逻辑被改坏，make check 照样全绿。

影响：第 2 层按 spec §6.1 要往 archtest 加模块间引用规则，正是最容易顺手改坏它的时候。

建议：在 internal/archtest 加表驱动测试，用 parser.ParseFile 解析内存里的源码字符串，直接调 checkFile 断言违规条目。至少覆盖：import 禁用的标准库要报；import 层 1 以外的包要报；internal/coreutils 这类前缀相撞的路径不算层 1；别名导入后 t.Now 要报；点导入 time 要报；把 time.Now 当函数值要报；注释和字符串里的 time.Now 不报；干净的文件不报。用内存源码而不用 testdata/*.go：既避开 Makefile fmt 的递归（T1-5），也避开「_test.go 后缀被 go build 静默排除」造成的假绿。纯测试改动，不改守卫行为，也不用改计划。守卫范围若决定收紧（T1-1），新规则一并进这张表。

出处：internal/archtest/deps_test.go:109-117、155-200；计划任务 1 红步骤（第 106 行）

合并自：T1 should：守卫自身的检测逻辑没有进库的回归测试；T1 should：internal/archtest/deps_test.go:155：守卫本身没有自动化的反例测试

### T9-6　激活判定有四处连带报错，下一步会误导作者

结果：已修（43aca7b），四处都照建议改，各有断言条数的测试：①Test重叠检查不比不属于本族的系；②Test正文扫描有问题时不报装配表里的插槽；③Test执行插槽的时机该格式不支持时不比实例的时机；④Test片段的列表规则与子值、Test模板正文的参数引用（每行每个参数只报一次）。原计划任务 9、10 已同步。

问题（逐处读码确认）：①hook 的重叠检查只跳过不认识的系和本份内重复的系，合法却不属于本实现族的系照样记进 covered（hook.go:106-116）。linux 片段误勾了 esxi、另一份 esxi 实现也勾了 esxi 时，前者报 impl_series_invalid，后者再报 impl_series_overlap，下一步写着「从其余的实现里去掉它」，作者可能去改那份本来正确的实现。②模板正文里写坏的插槽占位符（未闭合，或 @post 这种不认识的时机）不进扫描结果的插槽列表，装配表里同名的插槽就再报 assembly_slot_unknown（template.go:150-156），下一步写着「从装配表里删掉它」，照做会丢掉已配好的实例。③执行插槽的时机该格式不支持时，除了 slot_timing_unsupported，插槽里每个实例、每个系还各报一条 impl_timing_mismatch（:193-203），下一步让人给 hook 加一个该格式根本不支持的时机。④同一个列表参数在带插槽或部件的行里出现两次（部件写在前面），或两个列表参数同行而后者出现两次，list_repeat_conflict 报两次（hook.go:226-234、255-265）。

影响：与 hook.go 自己「不连带报错」的取舍不一致，照着下一步改反而改坏。

建议：①循环里一并跳过 s.Family() != impl.Family 的系；②正文扫描有问题时不报 assembly_slot_unknown（代价是别的真不存在的插槽要等扫描错误改完才报，与任务 9 的取舍一致）；③checkInstance 遇到时机不受该格式支持的执行插槽，跳过时机比对；④list_repeat_conflict 每行每个参数只报一次。每处补一条断言条数的测试。

出处：internal/core/activation/hook.go:106-116、226-234、255-265；activation/template.go:150-156、193-203

合并自：T9 建议：hook.go:98-106 的重叠检查只跳过不认识的系；【T10-4】T10 建议：连带重复报错（探针实测）；【T10-4】T10 建议：template.go:152-156 与 :200：有连带报错

### T11-8　参数求值透传的取值问题，称呼与下一步不对操作员

结果：已修。model 加 ParseNamed（03e0fec），登记 param_attr_unusable 与 param_sub_unavailable（822eec6）；参数求值里，可改的格子取值不合法时以 Field.Label 称呼，取不出子值报 param_sub_unavailable，锁定且取自主机属性的格子两样都报 param_attr_unusable，锁定在模板写定的值上的照激活判定报 param_accessor_invalid（c7fe6db）。原计划任务 11 已同步。测试：TestEvaluate可改的格子取值不合法时说明用显示名、TestEvaluate锁定取自主机属性的格子取值用不了、TestEvaluate正文和片段取的子值要取得出。

问题：params 原样透传 model 的两类问题，只换掉 Subject（params.go:194-199、210-219）。①称呼：param_value_invalid 用参数名（param.go:145 传 d.Name），同一格的 param_required、param_locked、param_not_offered、iso_offer_missing、param_attr_missing 用的却是 Field.Label（params.go:151）；向导里同一格一会儿叫「交换分区（MiB）」，一会儿叫「swap_mb」。Field.Label 的注释写明它就是给列头和问题说明用的。②下一步写错了人：锁定、取自主机属性的格子取值有问题时，报 param_value_invalid（「按说明里的格式重新填写」），或 param_accessor_invalid（主机登记的是 IPv6、片段取 .netmask，下一步「改正引用的写法，或换成支持它的参数类型」是写给作者的）；可这一格锁着，操作员填不了，该改的是主机登记。可改的格子填了 IPv6、片段取 .netmask 时，操作员该做的是改填 IPv4，说明同样不对人。只有 param_attr_missing 说对了。

影响：spec 总则要求说清下一步怎么办，这几种情形操作员照着做做不成。方向由 Field.Label 的既有约定和 spec 总则定死，不需要另行拍板。

建议：model 的 Parse、Sub 保持写给作者（激活判定里参数名正是作者在正文里写的）；params 自己构造面向操作员的问题：取值不合法时用 Field.Label 作称呼（model 导出一个按显示名与声明构造 param_value_invalid 的函数，中文仍只在 codes.go）；锁定的 attr 格子 Parse 或 Sub 失败时，改报在 codes.go 末尾追加的 incompatible 代码（如 param_attr_unusable：「参数 x 锁定取自主机属性 k，这台主机登记的值用不了」，下一步「到主机登记里改正属性 k」），登记进 extraCodes；可改的格子取不出子值时，另给一条面向操作员的说明（追加代码，或把 param_accessor_invalid 的下一步扩写成两种读者都适用）。补对应测试，在第 5 层做向导之前完成。

出处：internal/core/params/params.go:34-46、151、194-199、210-219；model/param.go:139-148、213-229；model/codes.go:46-55、237-240

合并自：任务11 should「…params.go:213 IPv6 取 netmask 沿用 param_accessor_invalid，它的「下一步」是写给模板作者的」；任务11 should「跨任务提醒（不属于本任务）：锁定的字段如果取值不合法，或没有值」（前半：主机属性格式不对）；任务11 should「…params.go:194–219：锁定的格子如果取值本身有问题」；任务11 fix「计划外的决定：没有在 codes.go 末尾加专用代码，直接沿用 param_accessor_invalid」；【T11-9】任务11 deviation「param_value_invalid passes through the Problem from ParamDecl.Parse」；【T11-9】任务11 should「…params.go:196：param_value_invalid 原样透传 Parse 给的说明，用的是参数名」；【T11-9】layerReview_should 第5条「同一格的说明对参数的称呼不一致」（前半：参数名与显示名）；【T5-2】T5 should「codes.go 的参数注释没有定死目录值和状态…」（后半：param_value_invalid 的第 1 个参数写的是参数名）

### T3-1　能力表格子里的取值与 cleanup 部件没有结构性校验

结果：已修（fc53fe6），两条断言都照建议加了，后一个测试改名为含凭据清理。原计划任务 3 已同步。测试：Test能力表只登记目录里的值且每格不重复、Test每种格式都支持首次开机时机并要求首次开机与凭据清理部件。

问题：①Test能力表只登记目录里的格式与时机（timing_test.go:327-340）只校验格式键和时机键是否 Valid，不校验格子里的解释器、部件、系统值，也不查列表内部重复；Test能力表逐格与计划一致 比对的是测试里手抄的一份，计划要求第 3、7 层改表时两边一起改，两边一起写错就没人兜。部件有间接保护（activation 的 bareTemplate 按 RequiredParts 拼正文），解释器和系统值没有：误加的 python 能通过 hook 激活（hook.go:296 只查是否在 Interpreters 里），Label() 却是空串；层 1 测试里没有用例引用 {{sys:gateway}}，把它写成 gatway，用到 gateway 的片段和模板都会被判 sysvalue_not_allowed，没人发现。②Test每种格式都支持首次开机时机并要求首次开机部件（:342-353）的注释引了 spec §5.4「最后还要清理目标机上的凭据」，循环里却没查 RequiredParts 含 cleanup；某种格式在表和抄录里同时去掉 cleanup，不会有任何测试报错，装出的机器会把应答文件副本里的密码哈希和首次开机 token 留在盘上。

影响：第 3、7 层真装之后要改这张表，到时这些错误都是静默的。

建议：①在前一个测试里断言每个时机下的解释器、parts、values 每一项都 Valid() 且列表内无重复；②后一个测试加断言 RequiredParts 含 cleanup，测试名相应改成含凭据清理。纯测试改动，十几行，赶在第 3 层改表之前。

出处：internal/core/catalog/timing_test.go:327-353；catalog/timing.go:84-158；activation/hook.go:296；spec §5.4 第 5 步

合并自：T3 should：timing_test.go:327-340 Test能力表只登记目录里的格式与时机；【T3-2】T3 should：timing_test.go:342-353 Test每种格式都支持首次开机时机并要求首次开机部件

### T8-1　扫描遇嵌套 {{ 会吞掉同一行后面的占位符

结果：已修。扫描在占位符内容里遇到 {{ 就截断，外层报 placeholder_unclosed，从内层的 {{ 接着扫，建议的三条测试都补上了（fc53fe6）；placeholder_unclosed 的说明改成「在 }} 或下一个 {{ 之前没有闭合」（822eec6）。原计划任务 8 已同步。T8-2 照建议不改。测试：Test内容里遇到双左花括号就截断外层。

问题：占位符从 {{ 起，到同一行其后第一个 }} 为止（scan.go:144-160）。{{slot:a {{slot:b}} 只报一条 placeholder_malformed，原文一直延伸到 b 的 }}，插槽 b 不进 Slots；{{foo:x {{slot:b}} 报 placeholder_unknown_kind，b 同样被吞。另外「种类不认识之后同一行接着扫」没有测试：现有用例里，种类不认识的占位符要么独占一行，要么同一行后面没有别的占位符（scan_test.go:266-269、439、571），拼回原文的用例只查原文；把 !known 分支改成丢掉这一行余下的部分，测试照样全过，而 {{foo:x}} {{slot:a}} 里的插槽 a 会从 Slots 里消失。

影响：作者少写一个 }}，编辑器里同一行后面的插槽就暂时消失，渲染也找不到它；与计划任务 8「有问题时也返回尽量完整的结果」（第 449 行）有张力。

建议：lober 占位符的内容不可能合法地含有 {{（插槽名、标识、系统值都不含花括号），所以在占位符内容里遇到 {{ 就截断：外层报 placeholder_unclosed，从内层的 {{ 接着扫。这是按计划第 449 行的精神补齐计划没写到的情形，同步改 codes.go 里 placeholder_unclosed 的注释（「在下一个 {{ 之前没有 }}」也算）和计划任务 8 的定义。补三条测试：{{foo:x}} {{slot:a}}（恰好一条 placeholder_unknown_kind，Slots 为 [a]）、{{slot:a {{slot:b}}、{{foo:x {{slot:b}}。评审另提的「{{ 之后的制表符不算空格」（T8-2）是计划明文、测试钉住（scan_test.go:491-497）的取舍，静默的后果在预览里看得到，保持不改。

出处：internal/core/scan/scan.go:131-172、175-190；scan_test.go:266-269、439、491-497、571；model/codes.go:74-77；计划任务 8 识别规则（第 421-426 行）与第 449 行

合并自：T8 建议：可选，需先在计划层面确认：`{{slot:a {{slot:b}}`；T8 建议：scan_test.go 缺「种类不认识之后，同一行接着扫」（第二个用例 `{{foo:x {{slot:b}}`）；T8 顾虑：计划没写到的决策，我选的做法如下（第 (3) 条）；【T8-3】T8 建议：scan_test.go 缺「种类不认识之后，同一行接着扫」的用例（第一个用例）；【T8-2】T8 建议：`{{` 后面的制表符不算空格；【T8-2】T8 顾虑：照计划字面执行的几处，后续层要知道（制表符部分）

### T5-4　参数类的三条说明不够具体，照着改不出

结果：已修。①int 的三种范围措辞登记在 codes.go（822eec6），Parse 按 Min、Max 选一条填好（03e0fec）；②param_decl_invalid 的下一步与注释写全了（822eec6）；③pwhash 的说明补上「不带 rounds=」，不放宽（822eec6）。可选的「给 params.Field 加 Min、Max」没做，第 6 层需要时再加。测试：TestParse的int说明带出具体范围、TestCode的int范围措辞接在格式后面。

问题：①int 有上下限时，param_value_invalid 只说「应为十进制整数，声明了上下限的还要在范围之内」（codes.go:317），不给出范围；给 1–4094 的 vlan 填了 5000，操作员不知道范围是多少，params.Field 也没有 Min、Max。②param_decl_invalid 的下一步（:42-45）和条目注释（:38-39）比实现窄：实现还要求默认值原样就是规范形式（+5、007、Node01、2001:DB8::1、带首尾空白、列表有空行或以换行结尾都会被拒），enum 必须给 Options 或 Offer 之一，Offer 是标识符，Options 与 Offer 只能给 enum，Min 与 Max 只能给 int，选项值不能重复（param.go:88-131）。作者把默认值写成 +5，单独 Parse 能过、Validate 却拒收，照说明找不出错在哪。③pwhash 照计划不收 $6$rounds=5000$…（param.go:422-423），说明只写「以 $6$、$5$ 或 $y$ 开头的 crypt 口令散列」（codes.go:325），被拒的人看不出原因；mkpasswd -m sha-512、openssl passwd -6 默认不带 rounds，只有显式指定轮数才会遇到。

影响：计划任务 6 要求「说明里写清期望的格式」，spec 总则要求说清下一步，这三处都做不到。

建议：①在 codes.go 末尾为 int 登记「不小于 %[1]v」「不大于 %[1]v」「%[1]v 到 %[2]v 之间」三种措辞，Parse 按 Min、Max 选一条、用 fmt 填好作第 2 个参数，中文仍只在 codes.go；补断言说明里带出上下限；可选给 params.Field 加 Min、Max 供第 6 层显示。②把 param_decl_invalid 的下一步和注释写全，如「默认值要写成规范形式：整数不带正号和前导零，主机名小写，IPv6 用压缩写法，不带首尾空白，列表每行一项、不留空行」。③pwhash 的说明补上「不带 rounds=」。三处参数个数都不变，代码表的完整性测试不受影响。建议不放宽 pwhash 去收 rounds=N$：那是改计划的类型表，收益小。

出处：internal/core/model/codes.go:38-50、317、325；model/param.go:88-148、269-276、422-442；params/params.go:34-46

合并自：T5 should「codes.go:307：paramValueFormats[int] 只写了「声明了上下限的还要在范围之内」，没给出具体范围…」；T5 should「codes.go:307 paramValueFormats[int] 只写了…没有给出实际的上下限…」；T6 should「int 声明了上下限时，param_value_invalid 只说…」；【T6-5】T6 deviation「默认值必须原样就是规范形式（Parse(Default).Canonical() == Default）…」；【T6-5】T6 deviation「声明校验里比计划多出的几条：Offer 必须是 ValidIdent…」；【T6-5】T6 should「param_decl_invalid 的下一步（internal/core/model/codes.go:42-45，任务 5 的文件）比实现的规则宽…」；【T6-5】T6 should「默认值从严要求规范形式（param.go:125-131）…」；【T6-4】T6 concern「pwhash 按计划只收 [./A-Za-z0-9$]，带 rounds=N 的 SHA-crypt 散列…」

### T5-5　codes.go 说明里抄的常量与词表没有对齐测试

结果：已修。codes_align_test.go 把说明里的上限、类型清单、算法、保留键、目录词表和「ESXi 只走 UEFI」钉在常量与目录上（3572304）；插槽名长度与超时范围分别在 scan、activation 的测试里对齐（fc53fe6、43aca7b）。逐项核对时说明与常量本来一致，codes.go 没改。测试：以 TestAlign 开头的 9 个测试、Test插槽名超长时说明里的上限与常量一致、Test超时的说明与范围一致。

问题：层 1 的测试里没有一处引用 snMaxLen、nameMaxRunes、identMaxLen、safeWordMaxLen、textMaxBytes、listMaxItems、timeoutMaxSec、slotNameMaxRunes（已 grep）。codes.go 的说明里重复写下的事实有：sn_invalid、name_invalid 及其原因里的 64；param_decl_invalid、attr_key_invalid 的「最长 32 个字符」、类型清单、保留键 sn、arch、firmware；param_value_invalid 的 1–256 项；paramValueFormats 里的 1–128、1–63、253、4096、sshkey 算法表和 pwhash 前缀；sn_unsafe_in_body 与 word、diskpath 说明里的安全词字符；param_accessor_invalid 的 addr、prefix、netmask；timing_unknown、part_unknown、sysvalue_unknown 下一步逐个列出的 4 个时机、3 个部件、7 个系统值；firmware_mismatch 的「ESXi 只走 UEFI」；timeout_invalid 的 1–21600（与 activation/hook.go:21-24 重复）；placeholder_malformed 的插槽名 1–32（与 scan/scan.go:40 重复）。目前逐项核对都一致。

影响：能力表注释写明是初值，第 3、7 层会改；第 3 层还可能新增参数类型。到那时这些说明会悄悄过时。

建议：在 model 加对齐测试：说明里含有 strconv.Itoa(各常量)，类型清单等于 paramTypeTable 的键，算法表等于 sshKeyFields 的键，保留键等于 hostReservedAttrs；目录词表用 catalog 现有的公开查询（Families→SeriesOf→Format→Timings、RequiredParts、SystemValues）求并集再和说明比对，ESXi 那句对照 esxi 系的 Firmwares()。超时与插槽名长度在 activation、scan 的测试里构造对应的 Problem 断言。跨任务评审曾建议改由参数传入列表，那会改变代码的参数个数，不取。赶在第 3 层改目录之前。

出处：internal/core/model/codes.go:22-89、141-144、264-267、286-289、312-327；activation/hook.go:21-24；scan/scan.go:40

合并自：T5 concern「This task depends only on task 1, so the clone has no task 3 catalog tables…」；T5 should「codes.go 里有几处文字和 ident.go、catalog 的事实重复…」；【L-9】layerReview_should 第9条「codes.go 的说明里抄了目录数据和别包的常量」

### T13-8　Build 丢掉查找结果，前提走岔时会静默产出空实例

结果：已修，随 L-7 一起做（a3dc308）。instance 查不到 hook 或实现时报与参数求值相同的 hook_ref_missing、impl_missing_for_series，不产出快照；参数求值通过之后的部分拆成 buildFrom，白盒测试用它模拟两边走岔。原计划任务 13 已同步。测试：TestBuild求值之后查不到hook或实现时不产出快照。

问题：instance 取 hook 和 ImplFor 时丢掉了 ok（build.go:150-151），完全靠 params.Evaluate 先报出 hook_ref_missing、impl_missing_for_series。现在两边遍历的是同一组插槽和实例，TestBuild前提不满足时不产出快照 也守着这一点。

影响：两边以后一旦走岔，Build 会静默产出 Kind、Body、HookName 全空的实例，随任务存进第 2 层，违反 spec 总则「不做静默降级」。

建议：instance 在 !ok 时返回对应的问题，Subject 与 Evaluate 一致（Kind 为 slot，ID 为「模板 ID@版本」，Path 为「插槽名/序号」）；slots 汇总这些问题，由 Build 返回、不产出快照。约十行，可在包内直接调 instance 写白盒测试。优先级低；L-7 若选 A，这一步是其中的一部分。

出处：internal/core/snapshot/build.go:122-171（instance 在 :150-151）

合并自：任务13 concern「Build relies on params.Evaluate to report missing hooks and implementations」；任务13 should「…build.go:100-101：instance 丢掉了 map 查找和 ImplFor 返回的 ok」

### T10-6　激活测试不断言条数，几处跳过规则没钉住

结果：已修，只改测试（43aca7b）。①onlyCode 要求条数；②–⑤ 各补了用例：Test族或系不对时照查不依赖格式的问题、Test模板的族或系不对时照查不依赖格式的问题、Test片段覆盖的系不同格式（混格式时不查系统值）、Test用join的列表与逐行重复的列表同行不冲突、Test模板问题的Subject。

问题：①onlyCode（hook_test.go:78-89）只要求每条都是期望的代码，不查条数，重复报出测不出。读代码可确认：去掉 template.go:243 的 !info.ok 保护后，「类型不认识」用例会多一条 param_decl_invalid，测试照样通过。②没有测试同时断言 impl_series_invalid（或 template_series_invalid）与不依赖格式的问题（如 param_undeclared）一起报出，checkImpl 或 Template 改成报完就提前返回也全绿。③hook 片段混格式时跳过 sysvalue_not_allowed 没钉住：混格式用例只用了 kickstart、autoyast、agama 都有的 task、sn。④一行里一个 |join 列表配一个逐行重复列表，这种不冲突的写法没有正例。⑤Test模板问题的Subject（template_test.go:515-554）只覆盖了 fragment_slot_not_alone 一种插槽问题，slot_duplicate（应指向后一处并带行列）、slot_timing_unsupported、part_not_in_format、实例的 timeout_invalid（Path「插槽名/序号」）都没有。

影响：激活判定「不连带报错」「推不出格式就跳过」这些取舍没有测试守着，T9-6 那几处连带报错就是这样漏过去的。

建议：onlyCode 加期望条数，或把结果确定的用例改成比对完整的代码列表；补上 ②–⑤ 的用例。只改测试。

出处：internal/core/activation/hook_test.go:78-89、173-213；activation/template_test.go:373-398、515-554；activation/template.go:241-250

合并自：T9 建议：测试漏了几处行为，onlyCode 也没检查条数（(a)–(d)）；T10 建议：测试：onlyCode（hook_test.go:79）只断言每条问题都是期望的代码；T10 建议：template_test.go:515-554 Test模板问题的Subject 只断言了 fragment_slot_not_alone

### T11-10　三处测试的数据分不出变异实现

结果：已修，只改测试。①锁定的 attr 登记成 IPv6 的例子，以及「.netmask 在前、.addr 在后」的引用（c7fe6db）；②pkgs@1 与 nic@2、nic@2 与 nic@10 两组（cc4c636）；③重名插槽插到正文中间（a3dc308）。测试：TestEvaluate锁定取自主机属性的格子取值用不了、TestEvaluate正文和片段取的子值要取得出、TestTemplateUsable多个废弃引用按hook排序各报一次、TestBuild重名的插槽与参数只算第一次出现。

问题：①params：TestEvaluate正文和片段取的子值要取得出（params_test.go:387-434）只测了操作员填值的路径，锁定、取自主机属性、登记成 IPv6、片段取 .netmask 的预填路径没有用例（读代码行为正确）；两处引用的 .netmask 都排在最后一个子值的位置，把 refSubs 改成只留最后一个子值，测试照样全过。②admission：废弃引用的测试数据是 lvm@1、nic@1、nic@2（admission_test.go:287-310），先比 ID 与先比版本排出来一样，评审把 deprecatedRefs 改成先比版本测试全过，文档承诺的「按 ID、版本排序」没被钉住。③snapshot：TestBuild重名的插槽与参数只算第一次出现（build_test.go:688-702）的注释说要钉住序号连续，但重名插槽追加在正文末尾，后面再没有别的插槽，把 Seq: len(out)+1 改成按扫描下标编号照样全过。

影响：这几处规则以后被改坏，不会有测试报红。

建议：①补一例锁定的 attr 登记成 IPv6，并把一处引用改成 .netmask 在前、.addr 在后；②加 pkgs@1 与 nic@2 同在、nic@2 与 nic@10 同在两组，分别区分排序键和数值比较；③把重名插槽插到正文中间，如「{{slot:账号}}」那一行之后。只改测试，优先级低。

出处：internal/core/params/params_test.go:387-434；admission/admission_test.go:287-310、admission.go:151-167；snapshot/build_test.go:688-702、build.go:135

合并自：任务11 should「…params_test.go:387 TestEvaluate正文和片段取的子值要取得出 只测了操作员填值的路径」；任务11 should「…params_test.go:392 与 :401：…两处引用里，.netmask 都排在这个参数最后一个子值的位置」；【T12-7】任务12 should「admission_test.go:289-310 (TestTemplateUsable多个废弃引用按hook排序各报一次)」；【T13-9】任务13 should「…build_test.go:543：TestBuild重名的插槽与参数只算第一次出现 的注释说要钉住「序号连续」，实际钉不住」

### T9-4　激活判定几处有意的放行没写明也没钉住

结果：已修，行为不改。两条 *_series_invalid 的说明加上「或重复勾选」（822eec6）；重复的时机只查一次、空片段允许，都写进注释并用测试钉住（43aca7b）。原计划任务 9 已同步。测试：Test重复声明的时机只查一次、Test空片段能通过。

问题：①同一份实现（以及模板）里重复勾的系报 impl_series_invalid 或 template_series_invalid（hook.go:130-143），两条说明都没提「重复」；脚本 Timings 里重复的时机被跳过、0 条问题（:293），这个分支没有测试。seriesValid 注释给的拒收理由「第 2 层会原样存下这份列表」对时机同样成立。②正文为空的片段能通过激活，空脚本会被拦（:285）。spec C8 没要求片段非空，计划任务 9 只对脚本写了「正文非空」；空片段有正当用途：给不需要做任何事的系当空操作实现，免得模板勾了这个系报 impl_missing_for_series。两种重复都只会来自直接调接口。

影响：接口调用方看到「族或系不对」找不到原因；规则不对称又没写明，后人可能把空片段当疏漏去拦。

建议：行为不改。两条 *_series_invalid 的说明加上「或重复勾选」；补一条测试钉住重复的时机只查一次；在 checkFragment 的注释里写明空片段是允许的空操作，并补一条正例。第 5 层可以在接口边界对这两份列表去重或拒收。优先级低。

出处：internal/core/activation/hook.go:130-167、285、293；model/codes.go:96-99、147-150；spec C8

合并自：T9 偏离：A series listed twice within one impl is reported as impl_series_invalid；T9 建议：重复项的处理前后不一致；T9 建议：hook.go:112-115 …hook.go:122-131 同一实现里重复勾的系报 impl_series_invalid（重复部分）；【T9-5】T9 建议：种类不认识的实现（Kind 为空串或 macro）…另外，正文为空或只有空白的片段能通过激活判定（空片段部分）

### T2-1　格式显示名照计划写成小写，与 spec 不一致

结果：已修（fc53fe6）。autoyast、agama 的显示名改成 AutoYaST、Agama，测试抄录同步改；原计划任务 2 那句已同步。测试：Test取值表与计划一致。

问题：计划任务 2 写「格式的显示名就是它的标识，esxi-ks 显示为「ESXi ks」」，实现照做，autoyast、agama 显示为小写（series.go:46-47，series_test.go:39-40 的抄录相同）；spec §2.1 的表和 C1 写的是 AutoYaST、Agama（kickstart、preseed、autoinstall、unattend 在 spec 里本来就是小写）。计划只对 ESXi ks 用了 spec 的写法，像是漏了这两个。

影响：显示名不进库，层 1 非测试代码也没有调用 Label()（已 grep），只影响第 5、6 层界面；界面会与 spec、文档和同表的「ESXi ks」写法不一致。

建议：按 spec 改成 AutoYaST、Agama，series.go 与 series_test.go 各改两处，计划任务 2 那句改成与 spec 一致。计划没给、实现者自定的其余显示名（解释器 sh、bash、cmd、PowerShell；部件「syslog 指向」「首次开机脚手架」「凭据清理」；系统值去掉括注；架构用标识，固件用 BIOS、UEFI）在 spec §2.5 或计划任务 3 都有出处，维持现状。优先级低。

出处：internal/core/catalog/series.go:41-50、series_test.go:39-40；计划任务 2（第 132 行）；spec §2.1 表（第 49-50 行）、C1（第 150 行）

合并自：T2 concern：格式显示名按计划的字面意思实现；T2 should：格式显示名，计划与 spec 写法不一致；T3 deviation：Display names the plan did not give；T3 should：计划没给的显示名是实现者自己定的；T4 deviation：显示名：架构用标识本身

## 3. 归后续层处理

属于后续层的事，或要等后续层才有真实用途。写这一层的计划时纳入即可。修补轮只并入了 T7-5（随 L-6）；之后 L-8 里字段键那一项随 C-3 修掉（036f397），L-8 的全局约定仍归第 5 层；另外 13 条中的第 3 层结果已在本节各条补记。

### T4-2　网关须先检查 iPXE 换算结果，不能传空值

第 7 层。PlatformOK 把主机那一项为空当作通过，这是给建任务时「登记了才校验」（C28）用的；注释（platform.go:115-116）只说「引导时网关传入的是换算后的实际值，不会为空」，计划任务 4（第 232 行）和 ArchFromIPXE 的注释写明换算失败由网关拒绝。如果第 7 层写成 arch, _ := catalog.ArchFromIPXE(x)，换算失败的机器会被 PlatformOK 静默放过，违反 C28「引导时必做」。建议：网关先检查 ArchFromIPXE、FirmwareFromIPXE 的 ok，为 false 就置任务失败、不签发 token，不调用 PlatformOK；网关测试要有「换算失败直接拒绝」的用例。层 1 可顺手在 PlatformOK 的注释里补一句「不能把换算失败的空值传进来」。

出处：internal/core/catalog/platform.go:75-94、112-125；计划任务 4（第 232 行）；spec C28、§5.4 第 2 步

合并自：T4 should：platform.go:112-117 PlatformOK 的注释要把空值的前提写明

### T6-6　子值缺少按类型与按快照的取法，第 3 层要补

结果：已修（28a55e6）。ParamType.SubType 与 SnapshotValue.Sub/Join/EnvValue 共用 cidrAccessors；激活用 SubType，渲染从冻结值取子值、空格连接与换行环境变量。TestParamTypeSubType只有cidr有子值、TestSnapshotValueSub取子值、TestSnapshotValue的Join与EnvValue 覆盖。

第 3 层。①activation 的 subOK 拿 192.0.2.1/24 按声明类型 Parse 再调 Sub（hook.go:269-279），哪些类型有哪些子值仍只在 model 登记一处，今天结果正确：其余受限类型解析不了这个样例，word 能解析但 Sub 只认 cidr。以后给 ip、hostname 这类加子值时样例解析不了，激活会拒掉合法引用（报错是显式的，加子值的人自测能发现）。②SnapshotValue 只有 Type、List、Items（task.go:121-127），Value.Sub 只能在 Parse 得到的 Value 上调用，而 Value 的字段不导出；第 3 层展开 {{param:ip.netmask}}、|join 和环境变量值时，要么借 ParamDecl{Type, List}.Parse 重建 Value（enum 没有 Options、Offer 时 Parse 会失败），要么自己再写一份 netmask 计算，C35 依赖的取值规则就成了两份。建议第 3 层开工时由 model 提供：从 cidrAccessors 派生的 ParamType.SubType(name) (ParamType, bool)，subOK 改用它；从 SnapshotValue 取子值、取 |join 与取环境变量值（换行连接）的函数，复用 cidrAccessors。fixed 值与默认值在激活时当场取子值的部分已归入 L-4。

出处：internal/core/activation/hook.go:269-279（subOK）；model/param.go:186-230（cidrAccessors、Sub）；model/task.go:121-127（SnapshotValue）

合并自：T6 concern「netmask 在激活时判断不了…」；T6 should「没有按类型静态查子值的导出接口（实现者已提出）…」；【T9-7】T9 顾虑：The sub-value check (subOK) parses the IPv4 sample 192.0.2.1/24；【T9-7】T9 建议：subOK（hook.go:210-217）把「只有 cidr 有子值」这条知识藏进了 IPv4 cidr 样例；【L-13】layerReview_should 第13条「快照只存子值的原料，没给取法」（前半：SnapshotValue 取子值）

### T8-4　渲染要照做的列表与子值约定还没写进交接

结果：已修。第 1 层计划交接补齐三条；第 3 层 expand 测试与真实 snapshot 渲染覆盖 子值|join、同行同列表一次重复和执行/系统行不重复。

第 3 层。层 1 已按下面三条校验，但计划的交接一节只写了「列表默认逐行重复，|join 以单个空格连接」：①接受 名字.子值|join，即列表逐项取子值后再以空格连接（scan 解析、activation 放行，与任务 6「Sub 对列表逐项取」一致）；②同一个列表参数在一行里出现几次、或取它的几个子值，只算一次逐行重复，每份抄本里各处代入同一项，不嵌套展开；③带执行插槽或系统部件的行一定不会逐行重复（激活已拦）。建议并入时先补进计划「留给后续层的约定」，第 3 层照做并加测试。

出处：docs/superpowers/plans/2026-09-30-rewrite-layer1-core-model.md:677-684；internal/core/scan/scan.go:222-229；activation/hook.go:255-265

合并自：T8 建议：参数引用接受了计划表以外的组合 `名字.子值|join`；T8 顾虑：写法检查和目录检查分两步（名字.子值|join 部分）；T9 偏离：For list_repeat_conflict I count distinct list parameters on a line；T9 建议：同一个列表参数在一行里出现几次、或取它的几个子值，只算一次逐行重复；T10 顾虑：The template list rule now also reports list_repeat_conflict

### T10-1　审批人比较不分大小写，待第 5 层定身份来源

第 5 层。Approver 先 TrimSpace 再 strings.EqualFold（template.go:54-63），(alice, Alice)、全角的 (Ａlice, ａlice) 都报 approver_is_author，只有空白的审批人报 approver_missing。这比计划字面「与作者相同」严，不会放松安全性；代价是在账号区分大小写的体系里，只差大小写的两个人互相批不了。对不对取决于第 5 层的身份来源（LDAP、AD 一般不分大小写）。建议现在保持，把这条规则写进计划「留给后续层的约定」；第 5 层定下身份来源后，改为传入身份源给的稳定账号 ID 并精确比较（改一行），同步测试。

出处：internal/core/activation/template.go:49-63；计划任务 10 Approver（第 518 行）

合并自：T10 偏离：Approver ignores surrounding whitespace and letter case；T10 顾虑：Comparing approver and author case-insensitively is a policy choice；T10 建议：template.go:54-63 Approver 先 TrimSpace 再 strings.EqualFold

### T7-5　ISO.Offers 的选项值须是安全词，第 4 层导入要守

结果：已随 L-6 修掉（5416531）。ISO.Validate 要求集合名是标识符、选项值是同一集合内不重复的安全词、选项的显示名过 ValidLabel，ISO.Offers 的注释也补上了这条规则；第 4 层导入 editions 时照它写值。原计划任务 7 已同步。测试：TestISOValidate提供的集合不合法。

第 4 层。parseEnum 只收安全词。ISO 提供的集合值如果不是安全词（比如把「Windows Server 2025 Datacenter」存进 Value），这个选项永远选不上，Evaluate 还会对列表里明明有的值报「应为列出的选项之一」，说明自相矛盾。这条要求只写在 Option 的注释里（param.go:18，说的是静态选项），ISO.Offers 的注释（registry.go:63-65）没提，model.ISO 也不校验。d45fbf3 已导出 model.IsSafeWord，第 4 层可以自查。建议第 4 层导入 editions 时 Value 用镜像索引或 ServerDatacenter 这类安全词、Label 放全名，集合名用 ValidIdent，导入时校验；现在可顺手给 ISO.Offers 的注释补一句。L-6 若决定加 ISO.Validate，这条规则一并放进去。

出处：internal/core/model/registry.go:63-65；model/param.go:18-19、278-293；params/params.go:201-209

合并自：T7 should「registry.go:56、63-65：给第 4 层的约束写得不全…」（Offers 部分）；【L-3】layerReview_should 第3条「ISO.Offers 的选项值必须是安全词，这条契约只写在 Option 的注释里」；【L-3】任务11 concern「ISO Offers option values have to be safe words」

### L-4b　fixed 值与默认值不经规范化，预填与快照不一致

第 5 层。defaultOK 要求默认值本身就是规范形式，理由是「向导里预填的、快照里存的是同一个串」（param.go:123-131）；checkBindings 对 fixed 值却只查 Parse 能否通过（template.go:230-231）。fixed 写成 +007 时激活放行，向导预填原样显示 +007（Field.Prefill 约定原样给出），快照和渲染用的是 7。差别只在显示，预览与真实下发都读快照，结果一致。建议第 5 层保存模板与 hook 时，把 fixed 值和默认值都换成 Parse(x).Canonical() 再存，作者写成 +5 的默认值也就不会再被 param_decl_invalid 拦下。层 1 也可以让激活判定要求 fixed 值原样就是规范形式，但 param_value_invalid 的说明解释不了 +007 错在哪，要另配措辞，不如在保存时规范化。

出处：internal/core/activation/template.go:230-231、241-250；model/param.go:123-131；params/params.go:43、223-235

合并自：layerReview_should 第4条「激活对 fixed 值与默认值比参数求值宽」（第 (2) 点：+007 与 defaultOK 不一致）

### T2-3　通过时返回 nil，第 5 层直接编码会得到 null

第 5 层。目录查询遇到未知键（SeriesOf、SeriesOfFormat、四个能力查询、Firmwares），扫描没有任何出现时（Slots、Parts、Sys、Params，空正文时 Segments 也是），以及 ISOUsable、TemplateUsable、HostSelectable、激活判定、Evaluate 通过时，都返回 nil，json.Marshal 编出 null；只有 Batch 与 Build 有意给空集合。BatchResult 的注释（admission.go:80）读起来像全包的保证，「每次返回新切片」也只写在 Families 的注释里。行为本身正确。建议第 5 层的 DTO 统一把 nil 切片编成 []；层 1 可顺手把 BatchResult 注释的理由收窄到 Batch 本身，并在 SeriesOf、SeriesOfFormat 的注释里补「每次返回新切片；未知时返回 nil」。

出处：internal/core/catalog/series.go:120-150、timing.go:216-242、platform.go:96-110；scan/scan.go:78-114；admission/admission.go:16-66、77-84；params/params.go:88-92

合并自：T2 should：series.go:120-121、130、141 「每次返回新切片」只写在 Families 的注释里；【T8-6】T8 建议：没有任何出现时，Result 的四个列表都是 nil 而不是空切片；【T12-9】任务12 should「admission.go:80 vs :18/:31/:53: the BatchResult comment explains the non-nil empty slices」

### T12-6　空批次在 Batch 里算通过

第 5 层。Hosts 为空、ISO 与模板都没问题时，Batch 返回 {Common: [], ByHost: {}}（admission.go:86-121），按 BatchResult 注释「全都没有问题才算通过」就算通过了；附录里没有能报这种情况的代码，Batch 的文档注释也没写。建议第 5 层在入参校验时拒收空批次；层 1 可顺手在 Batch 注释里补一句「调用方须先拒收空 Hosts」。若要层 1 自己拦，可按 extraCodes 的做法追加一个 invalid 代码放进 Common。

出处：internal/core/admission/admission.go:77-121

合并自：任务12 concern「空批次（Hosts 为空）没有可报的代码」；任务12 should「需要告诉第 5 层：Batch 收到空批次（Hosts 为空）时 Common 与 ByHost 都是空的」；任务12 should「admission.go:86-93 (Batch doc): with empty Hosts, Batch returns {Common:[],ByHost:{}}」

### T10-7　Approver 的问题 Subject 为空，要由控制台补

第 5 层。approver_missing、approver_is_author 的 Subject 为空（template.go:49-63）：Approver 由 hook 与模板共用，不知道审批的是哪个版本。第 5 层的审批接口要填上 Kind（hook 或 template）与 ID（「ID@版本」）。

出处：internal/core/activation/template.go:49-63

合并自：T10 顾虑：Approver returns problems with an empty Subject

### T8-5　正文开头的 UTF-8 BOM 算第 1 行的一个字符

第 5 层。以 BOM（U+FEFF）开头的 {{slot:a}} 得到 Col=2、Alone=false，拼回无损（C18），没有测试钉住。后果：第 1 行的片段插槽会被报 fragment_slot_not_alone，作者看着却是独占一行；kickstart、preseed 这类按行解析的格式遇到 BOM 也可能把第一行认错（没有实测）。建议第 5 层保存、导入正文时去掉开头的 BOM（或拒收），写进交接；层 1 可顺手补一条测试把现有行为钉住。

出处：internal/core/scan/scan.go:125-172；第 5 层正文的保存与导入

合并自：T8 建议：正文开头的 UTF-8 BOM 被算作第 1 行的一个字符；T8 顾虑：照计划字面执行的几处，后续层要知道（BOM 部分）

### T8-7　列号按 rune 计，浏览器编辑器按 UTF-16 计

第 5、6 层。Line、Col 按 rune 计（计划任务 8 规定）。Monaco、CodeMirror 按 UTF-16 码元计列，占位符前面每多一个辅助平面字符（emoji、CJK 扩展 B 区的字），标红位置就偏一列。第 5 或第 6 层把 Subject.Col 交给编辑器时要换算。

出处：internal/core/scan/scan.go:57-60、257-268；activation/hook.go:345-350

合并自：T8 建议：给后续层的提醒，不算本任务的缺陷：列号按 rune 计是计划定的

### L-8　标识常量、字段键与 ID@版本在各包各写一遍

结果：字段键一项已修；第 3 层所需机器面标识已有，剩余全局词表、Subject.Kind 与 ID@版本仍归第 5 层（2026-10-08 补记，出自第 2 层清单的 S10-5）。字段键随 C-3 修掉了（036f397）：TemplateFieldKey、InstancePath、InstanceFieldKey 放在 model（problem.go）而不是 params，因为依赖守卫不许 activation 依赖 params；activation、params、snapshot 共用这一份，Test同一格在激活判定与参数求值里标在同一处 钉住两边一致。所以下面建议里「第 5 层做接口时，params 导出字段键构造函数」不用再做。「ID@版本」仍是五处各拼一份：activation/hook.go、admission/admission.go、params/params.go、snapshot/build.go，以及第 2 层的 store.VersionID。要收成一份，可以在 model 给 HookKey、TemplateKey 加 String()，几处都改调它；不是改用 store.VersionID，因为第 1 层不许依赖 store，而 store 本来就依赖第 1 层，反过来引用还会成环。第 3 层新增所需 FileKind 等机器面常量，未把全局词表与 Subject.Kind 常量扩进本层；这两项及 ID@版本合并仍归第 5 层（S3-10 同步）。

第 3、5 层。①catalog 没有为族、格式、系、时机、解释器、部件、系统值、架构、固件导出常量，后续层只能把 rocky 这类标识写成字面量，拼错要等 Valid() 或测试才发现；也没有导出全局词表，第 5、6 层列下拉框只能经 Families、SeriesOf 反推。②字段键（tpl/、插槽名/序号/）在 params 与 snapshot 里各拼一次，「ID@版本」在四个包里各拼一次，Subject.Kind 的八个取值和 ParamType 全是字面量。字段键目前有 TestBuild回归41b2fbe 兜底，一边改了测试会失败，不会静默丢值。model 已为 ISOState、VersionState、ImplKind、BindingSource 导出常量，两种做法并存。建议照计划「由第一个用到它的层加进来」的做法：第 3 层要按格式、类型分支时，导出对应的带类型常量（命名如 FormatKickstart、ParamCIDR），表也改用常量书写；第 5 层做接口时，params 导出字段键构造函数，model 提供 Subject.Kind 常量和 HookKey、TemplateKey 的 String()，catalog 导出全局词表。常量是纯追加，越早加，要迁移的字面量越少。

出处：internal/core/catalog/series.go、timing.go、platform.go；params/params.go:76、84、109；snapshot/build.go:38、64、140；admission/admission.go:54；activation/hook.go:31-34；model/problem.go:27

合并自：layerReview_should 第8条「字段键与对象标识在各包各写一遍」；任务11 concern「Task 13 (snapshot Build) has to map Values back to instances using the same keys」；任务13 concern「The FieldKey format (tpl/<name> and <slot>/<seq>/<name>) is now written out in both」；任务13 should「…build.go:59 与 :90 各自又写了一遍字段键的格式」；任务13 should「…build.go:59、90：字段键的写法…在 params 与 snapshot 里各写了一份」；【T2-2】T2 concern：对外只导出了计划列出的名字

### T4-3　windows-desktop 允许 BIOS，与 Win11 要求不符

结果：第 3 层内置 unattend 只覆盖 Windows Server，不带 Windows 11 绕过设置；桌面版是否限 UEFI 仍归第 7 层真装后决定，届时同步固件表。

第 3 或第 7 层。windows-desktop 照计划登记成 bios、uefi 都支持，固件要求表里只有 esxi（platform.go:41-48）。Windows 11 官方要求 UEFI 与 Secure Boot（另有 TPM 2.0），BIOS 机器要靠安装时的绕过设置才装得上；固件要求按系登记，分不出 Windows 10 和 11。spec §2.8 的内置 unattend 模板只针对 Windows Server，眼下碰不到。第 3 层写 unattend 内置内容时决定要不要带绕过设置，或第 7 层真装验证后决定是否把 windows-desktop 限为 uefi；改这一格要回到计划任务 4 和固件要求表。

出处：internal/core/catalog/platform.go:41-48；计划任务 4（第 227 行）；spec §2.8

合并自：T4 concern：windows-desktop 照计划登记成两种固件都支持

### T8-8　扫描样例与能力表相符这一点没有断言

结果：已修（第 3 层 Task10 自审补漏，非原 Task2）。Test七种格式的样例扫出预期的占位符 现在同时核对排序后的 Parts == RequiredParts、Sys 属于 SystemValues、执行 Slot.Timing 属于 Timings。三种受控违规在旧测试通过，补断言后各自报红；恢复真实样例后 scan 全包通过。之后改能力表仍须同步断言。

第 3、7 层改能力表时。scan_test.go:25-28 的注释说七份样例都按各格式的能力表写，目前逐份相符，但没有测试检查；能力表改了以后样例可能悄悄不合规，注释不再属实（扫描本身的正确性不受影响）。改能力表时在 Test七种格式的样例扫出预期的占位符 里加三条断言：排序后的 parts 等于排序后的 RequiredParts()；sys 都在 SystemValues() 里；执行插槽的 Timing 都在 Timings() 里。现在顺手加也可以。

出处：internal/core/scan/scan_test.go:25-29、114-147

合并自：T8 建议：scan_test.go:25-28 的注释说，七份样例都按各格式的能力表写

### T1-5　fmt 把目录交给 gofmt，实际递归整棵子树

第一个在模块根或 testdata 里放 .go 文件的层。Makefile:13-15 把 go list 列出的目录交给 gofmt -l，gofmt 对目录参数是递归的，实际检查的是这些目录下的整棵子树，包括 go list 会跳过的 testdata 和点开头的目录；:8 的注释「只查 go list 列出的包目录」不准确。现在没有影响：模块根没有包，scan/testdata 里没有 .go 文件。模块根一旦出现 .go 文件，web/node_modules 和 .tmp/wf 下其他代理的克隆都会被扫进来，testdata 里的文件还会重复列出；这种失败是响亮的，不会静默放过。spec §4.8 把装配点放在 cmd/lober，目前没有哪层要在模块根放包。届时把 fmt 改成传文件：用 go list -f 拼出 GoFiles、CgoFiles、TestGoFiles、XTestGoFiles、IgnoredGoFiles 的完整路径再交给 gofmt -l。现在顺手可做的只有把 :8 的注释改准确。

出处：Makefile:8-16；计划任务 1（第 96 行）；spec §4.8

合并自：T1 should：Makefile 的 fmt：gofmt -l <目录> 会递归进所有子目录；T1 should：Makefile:15（注释在 :8）：gofmt -l 对目录参数是递归的
