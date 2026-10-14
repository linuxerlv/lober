package model

// codeSpec 是代码表的一项。说明与下一步是 fmt 模板，动词一律写成 %[n]v：两者共用
// 同一组参数、各取所需，%v 又能接住调用方传来的字符串和整数。
type codeSpec struct {
	category Category
	args     int // NewProblem 要传的参数个数，与模板里用到的 %[1]v…%[n]v 一一对应
	message  string
	next     string
}

// codeTable 按计划附录「原因代码总表」一次登记全部代码，分组照定义该代码的任务。
// 每项上方的注释写何时产生，以及各个参数是什么。
//
// 全层的中文说明只写在本文件里，参数只放数据：目录里的值（族、系、格式、时机、解释器、
// 部件、系统值、架构、固件）传标识，作者在正文和占位符里写的就是它，对照报错好找；状态传
// StateLabel 给的称呼（ISOState、VersionState 的中文显示名；不认识的状态没有显示名，回落到标识；
// 空状态回落到文件末尾的 stateUnknown）；其余是名字或显示名、取值、数字、占位符原文。一个代码有几种
// 原因时，说明把条件写全；只有产出它的函数在本包里、可选的措辞又是固定几种的，才把每种措辞登记在文件
// 末尾，由那个函数选一条作参数。
// 之后的任务只产出这些代码，不在别处写中文说明；确需新代码时追加在本表末尾。
var codeTable = map[Code]codeSpec{
	// —— 任务 5：标识规范化 ——

	// SN 为空、超长、含不可打印字符或连续空格。参数：原因，取文件末尾的 snReason* 之一。
	"sn_invalid": {CategoryInvalid, 1,
		"SN 不合法：%[1]v",
		"SN 要 1–64 个可打印 ASCII 字符，中间最多隔一个空格；照机器铭牌或 BIOS 里的序列号重新填写"},
	// SN 是主板出厂的占位值，或由同一个字符组成。参数：规范化后的 SN。
	"sn_placeholder": {CategoryInvalid, 1,
		"SN「%[1]v」是主板出厂时的占位值，不能用来区分机器",
		"到 BIOS 或 BMC 里查这台机器真实的序列号再登记；厂商没写序列号的，先给机器刷入唯一的 SN"},
	// 模板、hook 的名称为空、超长，或含可打印 ASCII（0x21–0x7E）以外的字符：空格等空白、中文、
	// 全角字符、控制字符与不可见字符，以及非法的 UTF-8 字节（T5-3）。
	// 参数：原因，取文件末尾的 nameReason* 之一。
	"name_invalid": {CategoryInvalid, 1,
		"名称不合法：%[1]v",
		"名称要 1–64 个英文字母、数字或美式键盘上可见的符号，不能有空格、中文或其他字符；" +
			"单词之间可以用 - 或 _ 连接，比如 Rocky-Base"},

	// —— 任务 6：参数类型与取值 ——

	// 参数声明本身不合法（T5-4②）：名字不是标识符或重名；类型不认识，或 text 声明成列表；默认值
	// 通不过校验，或不是原样的规范形式；enum 既没给 Options 也没给 Offer、两样都给，或 Offer 不是
	// 标识符；选项值不是安全词或有重复；Options、Offer 给了 enum 以外的类型，Min、Max 给了 int 以外
	// 的类型，或下限大于上限；显示名不合格（ValidLabel：参数的显示名可以留空，选项的不能）。
	// 参数：参数名。
	"param_decl_invalid": {CategoryInvalid, 1,
		"参数「%[1]v」的声明不合法：名字、显示名、类型、是否列表、默认值、选项或上下限有不符合规则的地方",
		"名字要以小写字母开头，只含小写字母、数字和下划线，最长 32 个字符，且不与其他参数重名；" +
			"类型只能是 word、int、enum、ip、cidr、hostname、diskpath、sshkey、pwhash、text，text 不能声明成列表；" +
			"默认值要能通过这个参数自己的校验，并且原样写成规范形式：整数不带正号和前导零，主机名小写，" +
			"IPv6 用小写的压缩写法，sshkey 的各段只隔一个空格，除 text 外不带首尾空白，列表每行一项、不留空行、末尾不带换行；" +
			"enum 要给静态选项或 ISO 提供的集合，只能给一种，集合名的写法同参数名，选项值是互不重复的安全词；" +
			"静态选项和集合只能给 enum，上下限只能给 int，下限不能大于上限；" +
			"参数的显示名可以留空，选项的显示名不能为空，显示名最多 64 个字，首尾不能是空白，不能含控制字符或不可见字符"},
	// 取值通不过类型校验，或不在静态选项里。参数：参数的称呼（参数名；参数求值经 ParseNamed 传
	// 显示名）、期望的格式（按参数类型取文件末尾 paramValueFormats 里的那一条，int 声明了上下限的
	// 再接上 intRange* 之一）。
	"param_value_invalid": {CategoryInvalid, 2,
		"参数「%[1]v」的取值不合法：应为%[2]v",
		"按说明里的格式重新填写；列表参数每行一项，共 1–256 项，整个列表合计不超过 32767 字节（不算空行和每项首尾的空白）"},
	// 子值或 |join 用在了不支持它的值上（IPv6 取 netmask、非 cidr 取子值、非列表用 |join）。
	// 参数：写出的子值名或 |join。
	"param_accessor_invalid": {CategoryInvalid, 1,
		"「%[1]v」用法不对：只有 cidr 参数有 addr、prefix、netmask 子值，netmask 只适用于 IPv4；|join 只能用于列表参数",
		"改正引用的写法，或换成支持它的参数类型"},

	// —— 任务 7：主机、ISO、hook 与模板 ——

	// 属性键不是标识符，或用了保留键 sn、arch、firmware。参数：属性键。
	"attr_key_invalid": {CategoryInvalid, 1,
		"属性键「%[1]v」不能用：它不是合法的标识符，或者是保留的 sn、arch、firmware",
		"属性键要以小写字母开头，只含小写字母、数字和下划线，最长 32 个字符，且不能是 sn、arch、firmware"},

	// —— 任务 8：正文扫描（Subject 的 Kind 为 body，带行列） ——
	// {{ 后紧跟「小写字母加冒号」，但种类不是 slot、lober、sys、param。参数：写出的种类。
	"placeholder_unknown_kind": {CategoryInvalid, 1,
		"不认识的占位符种类「%[1]v」",
		"lober 的占位符只有 slot、lober、sys、param 四种；要原样输出 {{，写成 {{sys:lbrace}}"},
	// 名字、时机、子值或 |join 的写法不对。参数：占位符原文。
	"placeholder_malformed": {CategoryInvalid, 1,
		"占位符「%[1]v」写法不对：名字、时机、子值或 |join 不合语法",
		"对照占位符语法改正，例如 {{slot:分区}}、{{slot:装后配置@post-target}}、{{param:addr.netmask}}、{{param:pkgs|join}}；" +
			"插槽名是 1–32 个字母（含中文）、数字、_ 或 -，参数名以小写字母开头，只含小写字母、数字和下划线"},
	// 同一行里，在 }} 或下一个 {{ 之前没有闭合（T8-1）：同一行里没有 }}，或者 }} 之前又出现了
	// {{。lober 占位符的内容不可能合法地含有 {{，扫描就在那里截断，从内层的 {{ 接着扫。
	"placeholder_unclosed": {CategoryInvalid, 0,
		"占位符没有闭合：同一行里、下一个 {{ 之前没有 }}",
		"在同一行里补上 }}，占位符不能跨行，里面也不能再套 {{；要原样输出 {{，写成 {{sys:lbrace}}"},
	// 执行插槽 @ 后的时机不认识。参数：写出的时机。
	"timing_unknown": {CategoryInvalid, 1,
		"不认识的执行时机「%[1]v」",
		"执行时机只能是 pre、post-installer、post-target、firstboot 之一"},
	// {{lober:…}} 的部件不认识。参数：写出的部件名。
	"part_unknown": {CategoryInvalid, 1,
		"不认识的系统部件「%[1]v」",
		"系统部件只有 syslog、firstboot、cleanup 三个"},
	// {{sys:…}} 的系统值不认识。参数：写出的系统值名。
	"sysvalue_unknown": {CategoryInvalid, 1,
		"不认识的系统值「%[1]v」",
		"系统值只有 repo、iso、smb、gateway、task、sn、lbrace 七个"},

	// —— 任务 9：hook 的激活判定 ——
	// hook 一份实现都没有。
	"hook_no_impl": {CategoryIncomplete, 0,
		"hook 还没有任何实现",
		"至少写一份实现：选一个族和该族下的系，再写片段或脚本"},
	// 实现的族不合法，没勾系，系不合法、不属于该族，或同一个系勾了两次（T9-4）。
	"impl_series_invalid": {CategoryIncomplete, 0,
		"实现的族或系不对：族不合法、没勾系，勾了不合法、不属于这个族的系，或重复勾选了同一个系",
		"给这份实现选一个合法的族，再勾该族下的一个或多个系，每个系只勾一次"},
	// 同一个系出现在不止一份实现里。参数：系。
	"impl_series_overlap": {CategoryIncomplete, 1,
		"系「%[1]v」出现在不止一份实现里",
		"一个系只能由一份实现覆盖；从其余的实现里去掉它"},
	// 片段覆盖的系不同格式。参数：涉及的格式（调用方用顿号连好）。
	"fragment_mixed_formats": {CategoryIncomplete, 1,
		"片段覆盖的系不是同一种格式：%[1]v",
		"片段按一种格式的语法写；不同格式的系各写一份片段，或改用脚本"},
	// 片段里出现了插槽或系统部件。参数：占位符原文。
	"fragment_forbidden_placeholder": {CategoryIncomplete, 1,
		"片段里不能写「%[1]v」：插槽和系统部件只能出现在模板正文里",
		"删掉这个占位符；hook 只有一层，片段里只能引用系统值和参数"},
	// 片段或模板正文引用的系统值在该格式不可用。参数：系统值、格式。
	"sysvalue_not_allowed": {CategoryIncomplete, 2,
		"系统值「%[1]v」在 %[2]v 格式里不可用",
		"删掉这个引用，或换成 %[2]v 格式可用的系统值"},
	// 引用的参数没有声明；脚本里按字面出现的 LOBER_PARAM_* 查。参数：引用的参数名或变量名。
	"param_undeclared": {CategoryIncomplete, 1,
		"引用的参数「%[1]v」没有声明（脚本只认字面出现的 LOBER_PARAM_* 变量名，动态拼出的名字查不到）",
		"在参数声明里加上它，或改正引用的名字"},
	// text 参数进了片段或模板正文。参数：参数名。
	"param_text_in_fragment": {CategoryIncomplete, 1,
		"参数「%[1]v」是 text 类型，不能进正文或片段",
		"text 只能经环境变量交给脚本；改用受限类型，或把用到它的内容挪进脚本"},
	// 一行里有两个逐行重复的列表参数，或这一行里还有插槽、系统部件。
	// 参数：冲突的那个列表参数名。
	"list_repeat_conflict": {CategoryIncomplete, 1,
		"列表参数「%[1]v」会逐行重复，它所在的行里不能再有别的逐行重复的列表参数、插槽或系统部件",
		"一行里只放一个逐行重复的列表参数，这一行也不能有插槽或系统部件；要在一行里写完就用 |join"},
	// 脚本正文为空，或没声明解释器、时机。
	"script_incomplete": {CategoryIncomplete, 0,
		"脚本实现不完整：正文为空，或者没有声明解释器、执行时机",
		"脚本要有正文，并声明解释器和至少一个执行时机"},
	// 脚本声明的时机，覆盖的某个系的格式不支持。参数：系、时机。
	"script_timing_unsupported": {CategoryIncomplete, 2,
		"系「%[1]v」不支持执行时机「%[2]v」",
		"从脚本里去掉这个时机，或把这个系移到另一份实现里"},
	// 脚本的解释器在某个系的某个时机不可用。参数：系、时机、解释器。
	"script_interpreter_unavailable": {CategoryIncomplete, 3,
		"系「%[1]v」在「%[2]v」时机没有解释器 %[3]v",
		"换成该时机可用的解释器（比如 Debian 装前只有 busybox sh），或调整时机与覆盖的系"},
	// 有脚本实现时超时不在 1–21600 秒。参数：当前的超时秒数。
	"timeout_invalid": {CategoryIncomplete, 1,
		"超时 %[1]v 秒不在 1–21600 秒之间",
		"把超时改成 1 到 21600 秒（6 小时）之间的整数"},

	// —— 任务 10：模板的激活判定与审批人 ——
	// 模板没选族、没勾系，系不合法、不属于该族，或同一个系勾了两次（T9-4）。
	"template_series_invalid": {CategoryIncomplete, 0,
		"模板的族或系不对：没选族、族不合法、没勾系，勾了不合法、不属于这个族的系，或重复勾选了同一个系",
		"先选族，再勾该族下至少一个系，每个系只勾一次；没勾系的模板不能激活"},
	// 模板勾的系不同格式。参数：涉及的格式（调用方用顿号连好）。
	"template_mixed_formats": {CategoryIncomplete, 1,
		"模板勾的系不是同一种格式：%[1]v",
		"一个模板只能勾同一种格式的系，正文按这种格式写；其他格式的系另建模板"},
	// 插槽名重复，不论类型；每多出现一次报一条，落在后出现的那一处。快照构建也报它（第 3 层任务 1）：
	// 草稿照样能预览，渲染却要依赖正文里的插槽名与快照一一对应。两处的 Subject 相同：Kind 为 slot，
	// ID 为「模板 ID@版本」，Path 为插槽名，带行列。参数：插槽名。
	"slot_duplicate": {CategoryIncomplete, 1,
		"插槽「%[1]v」在正文里出现了不止一次",
		"插槽名在一个模板里要唯一；给其中一个改名"},
	// 执行插槽的时机该格式不支持。快照构建也报它，理由与 Subject 同 slot_duplicate。参数：时机、格式。
	"slot_timing_unsupported": {CategoryIncomplete, 2,
		"%[2]v 格式不支持执行时机「%[1]v」",
		"把这个执行插槽改成 %[2]v 格式支持的时机"},
	// 片段插槽没有独占一行。参数：插槽名。
	"fragment_slot_not_alone": {CategoryIncomplete, 1,
		"片段插槽「%[1]v」没有独占一行",
		"片段插槽会展开成多行，所在行除了缩进不能有别的内容；把它单独放一行"},
	// 必需的系统部件没出现。参数：部件、格式。
	"part_missing": {CategoryIncomplete, 2,
		"正文缺少系统部件「%[1]v」，%[2]v 格式必需它",
		"在正文里合适的位置写一次 {{lober:%[1]v}}"},
	// 系统部件出现了不止一次。参数：部件。
	"part_duplicate": {CategoryIncomplete, 1,
		"系统部件「%[1]v」出现了不止一次",
		"每个系统部件在正文里只能出现一次；删掉多余的"},
	// 正文里出现了该格式没有的系统部件。参数：部件、格式。
	"part_not_in_format": {CategoryIncomplete, 2,
		"%[2]v 格式没有系统部件「%[1]v」",
		"删掉这个部件占位符"},
	// Bindings 指向了没声明的参数。参数：参数名。
	"binding_param_unknown": {CategoryIncomplete, 1,
		"取值来源指向了没有声明的参数「%[1]v」",
		"删掉这条取值来源，或先声明这个参数"},
	// 装配表里的插槽正文里没有。参数：插槽名。
	"assembly_slot_unknown": {CategoryIncomplete, 1,
		"装配表里的插槽「%[1]v」在正文里不存在",
		"在正文里加上这个插槽，或从装配表里删掉它"},
	// 引用的 hook 版本不存在（参数求值、快照构建遇到结构性缺失时也用它）。参数：hook ID、版本。
	"hook_ref_missing": {CategoryIncomplete, 2,
		"引用的 hook %[1]v@%[2]v 不存在",
		"改成引用一个存在的 hook 版本"},
	// 引用的 hook 版本还没审批。参数：hook ID、版本。
	"hook_ref_unapproved": {CategoryIncomplete, 2,
		"引用的 hook %[1]v@%[2]v 还没审批",
		"已审批的模板只能引用已审批的 hook 版本；先审批它，或改用它已审批的版本"},
	// 引用的 hook 版本已废弃。参数：hook ID、版本。
	"hook_ref_deprecated": {CategoryIncomplete, 2,
		"引用的 hook %[1]v@%[2]v 已废弃",
		"废弃的版本不能再被新引用；改用这个 hook 的其他已审批版本"},
	// 实例的 hook 没有覆盖某个系的实现（参数求值、快照构建也用它）。参数：hook ID、版本、系。
	"impl_missing_for_series": {CategoryIncomplete, 3,
		"hook %[1]v@%[2]v 没有覆盖系「%[3]v」的实现",
		"给这个 hook 补上覆盖该系的实现，或从模板里去掉这个系"},
	// 实现种类与插槽类型不一致（Impl.FitsSlot）。激活判定按模板所勾的每个系查，快照构建按 ISO 的系
	// 查（L-7）；两边的 Subject 都落在插槽里的实例上：Kind 为 slot，ID 为「模板 ID@版本」，Path 为
	// <插槽名>/<n>。参数：hook ID、版本、系、实现的种类（原样传，不认识的种类也算不一致）。
	"impl_kind_mismatch": {CategoryIncomplete, 4,
		"hook %[1]v@%[2]v 覆盖系「%[3]v」的实现是 %[4]v，与插槽类型不一致",
		"片段只能放进片段插槽，脚本只能放进执行插槽；换一个插槽，或换一个 hook"},
	// 执行插槽的时机不在脚本声明的时机里（Impl.FitsSlot）。产出方与 Subject 同 impl_kind_mismatch；
	// 插槽的时机该格式不支持时，激活判定只报 slot_timing_unsupported、不报它（T9-6③）。
	// 参数：hook ID、版本、系、插槽的时机。
	"impl_timing_mismatch": {CategoryIncomplete, 4,
		"hook %[1]v@%[2]v 覆盖系「%[3]v」的脚本没有声明时机「%[4]v」",
		"放进时机与脚本声明一致的执行插槽，或给这个 hook 的脚本加上该时机"},
	// 审批人为空。
	"approver_missing": {CategoryInvalid, 0,
		"没有指定审批人",
		"指定一位作者以外的审批人"},
	// 审批人就是作者。参数：审批人。
	"approver_is_author": {CategoryState, 1,
		"审批人「%[1]v」是这个版本的作者，不能自己审批",
		"换一位作者以外的人来审批"},

	// —— 任务 11：参数求值（Subject 的 Kind 为 param，ID 为主机 SN，Path 为字段键） ——
	// 必填字段最后没有值。参数：字段的显示名。
	"param_required": {CategoryInvalid, 1,
		"参数「%[1]v」必填，但没有值",
		"在向导里给这台主机填上它，或在模板的装配表里给它指定取值来源"},
	// 操作员填了锁定的字段。参数：字段的显示名。
	"param_locked": {CategoryInvalid, 1,
		"参数「%[1]v」已被模板锁定，不能填写",
		"清掉填的值；确实要改，先在模板的装配表里把它设成可改"},
	// 取值不在 ISO 提供的集合里。参数：字段的显示名、填的值、集合名。
	"param_not_offered": {CategoryInvalid, 3,
		"参数「%[1]v」的取值「%[2]v」不在这个 ISO 提供的 %[3]v 里",
		"从这个 ISO 实际提供的选项里选一个"},
	// Entered 里有表单中不存在的字段键。参数：字段键。
	"param_unknown_field": {CategoryInvalid, 1,
		"表单里没有字段「%[1]v」",
		"刷新向导后重新填写；模板或 ISO 可能已经换过了"},
	// 锁定、必填的参数取自主机属性，而这台主机没登记它。参数：字段的显示名、属性键。
	"param_attr_missing": {CategoryIncompatible, 2,
		"参数「%[1]v」锁定取自主机属性「%[2]v」，但这台主机没登记这个属性",
		"到主机登记里给这台主机补上属性「%[2]v」"},
	// 参数要的选项集合 ISO 没有提供，或给的是空集合（ParamDecl.OfferMissing）。参数求值报它时落在那一格
	// 上，参数：字段的显示名、集合名。准入判定筛候选时也报它（admission.TemplateUsable，L-2），只看可用、
	// 系也勾了的 ISO（C-1），落在模板上：Kind 为 template，ID 为「模板 ID@版本」，Path 为空；缺几个集合报
	// 几条，参数：第一个要这个集合的参数的显示名（没写时用参数名）、集合名。
	"iso_offer_missing": {CategoryIncompatible, 2,
		"参数「%[1]v」的选项取自 ISO 的 %[2]v，但这个 ISO 没有提供",
		"换一个提供了 %[2]v 的 ISO，或重新导入这个 ISO"},

	// —— 任务 12：准入判定与候选筛选 ——

	// 主机已有未结束的任务。
	"host_has_open_task": {CategoryState, 0,
		"这台主机已有未结束的任务",
		"等那个任务结束，或先取消它"},
	// ISO 不可用：获取中、导入中、已废弃，或状态不认识；导入失败不归这里，另报 iso_import_failed
	// （T5-1）。参数：状态的称呼（StateLabel：中文显示名，不认识的回落到标识，空状态回落到
	// stateUnknown，C-7）。已废弃的 ISO 不能再被新引用（C26），所以下一步要把它与「等它导入完成」分开说。
	"iso_not_ready": {CategoryState, 1,
		"ISO 不可用：现为「%[1]v」",
		"等它导入完成后再用，或换一个可用的 ISO；已废弃的 ISO 不能再用来建任务"},
	// 模板版本不是已审批。参数：状态的称呼（StateLabel，规则同 iso_not_ready，T5-1、C-7）。
	"template_not_approved": {CategoryState, 1,
		"模板的这个版本不是已审批状态（现为「%[1]v」）",
		"先审批这个版本；向导里每个模板只提供它当前的已审批版本"},
	// ISO 的架构与主机登记的不符。参数：ISO 架构、主机架构。
	"arch_mismatch": {CategoryIncompatible, 2,
		"ISO 的架构是 %[1]v，这台主机登记的是 %[2]v",
		"换一个架构相符的 ISO，或核对这台主机登记的架构"},
	// 系不支持主机登记的固件。参数：系、主机固件。
	"firmware_mismatch": {CategoryIncompatible, 2,
		"系「%[1]v」不支持用 %[2]v 固件引导",
		"把这台主机改成该系支持的固件引导并更新登记（ESXi 只走 UEFI），或换一个其他系的 ISO"},
	// 模板引用了已废弃的 hook 版本。参数：hook ID、版本。
	"template_blocked_by_deprecated": {CategoryIncompatible, 2,
		"模板引用的 hook %[1]v@%[2]v 已废弃，不能再用来建任务",
		"在模板里把 %[1]v 升级到它的已审批新版本，审批后再建任务"},
	// ISO 的系不在模板所勾的系里。准入判定、参数求值与快照构建都报它（L-7），Subject 都落在模板上：
	// Kind 为 template，ID 为「模板 ID@版本」，Path 为空。三处报了它就不再查因系而起的问题：参数求值与
	// 快照构建只报这一条（快照构建遇到不是目录里的系也报它），准入判定不再查 ISO 提供的集合（C-1）。
	// 参数：ISO 的系。
	"series_not_ticked": {CategoryIncompatible, 1,
		"模板没有勾 ISO 的系「%[1]v」",
		"换一个勾了这个系的模板，或换一个其他系的 ISO"},
	// 同一个 SN 在一批里出现了两次，两处都报。
	"host_duplicate_in_batch": {CategoryInvalid, 0,
		"这台主机在本批里出现了不止一次",
		"每台主机在一批里只选一次"},

	// —— 计划外追加（计划「并行执行」：确需新代码时追加在本表末尾） ——

	// 整层验收追加：模板正文或按 ISO 的系选中的片段引用了 {{sys:sn}}，而主机 SN 不是安全词。
	// SN 的登记规则收空格和任意可打印 ASCII，渲染又不解析语法、只能原样代入（C18）。由快照构建
	// 产出，Subject 的 Kind 为 host，ID 为 SN。参数：SN。
	"sn_unsafe_in_body": {CategoryIncompatible, 1,
		"主机 SN「%[1]v」不是安全词，原样代入模板正文或所选片段里的 {{sys:sn}} 可能改变应答文件的结构",
		"能代入 {{sys:sn}} 的 SN 只能由字母、数字和 ._:/@^+=- 组成，不能有空格，也不能以 - 或 @ 开头；给这台主机换一个不引用 {{sys:sn}} 的模板，" +
			"或在模板正文与片段里去掉 {{sys:sn}}、改用取自主机属性的 word 参数，审批后再建任务"},

	// —— 第 1 层修补轮追加（plans/2026-10-02-layer1-fixups.md「新增代码」，括号里是遗留问题清单的条目） ——

	// 自由文本存不下（StorableText 为假，T13-10）：不是合法的 UTF-8，或含 NUL。PostgreSQL 的 text 与
	// jsonb 都不收 NUL，encoding/json 又会把非法字节换成 U+FFFD，存进去再取出来就不是原来的字节。
	// 激活判定查正文与说明，ISO 与主机的校验查名称与属性值，第 5 层保存时查全部自由文本；Subject
	// 指向那个字段。
	"text_not_storable": {CategoryInvalid, 0,
		"文本存不下：不是合法的 UTF-8，或者含有 NUL 字符",
		"改成 UTF-8 编码的文本再保存，并去掉其中的 NUL 字符；Windows 上编辑的 .ps1、.cmd 常存成 UTF-16 或 GBK，要先转成 UTF-8"},
	// ISO 的记录不合法（ISO.Validate 不通过，L-6）：族、系、架构、版本不合法，或提供的集合里集合名
	// 不是标识符、选项值不是安全词或有重复、选项的显示名不合格；名称存不下另报 text_not_storable。
	// ready 却不合法的 ISO 在准入时也报它。参数：字段的中文称呼，按 Subject.Path 里的字段名取文件
	// 末尾 fieldLabels 里的那一条。
	"iso_incomplete": {CategoryState, 1,
		"ISO 的记录不完整：%[1]v不合法",
		"重新导入这个 ISO，仍然不行就删掉后重新创建；也可以换一个可用的 ISO"},
	// 主机登记的架构或固件非空，却不是合法取值（T4-1）：大小写不对、写成了别名，或存了 iPXE 的
	// 上报值。参数：字段的中文称呼（取 fieldLabels）、登记的取值。
	"host_platform_invalid": {CategoryInvalid, 2,
		"主机登记的%[1]v「%[2]v」不是合法取值",
		"架构只能填 x86_64 或 aarch64，固件只能填 bios 或 uefi，区分大小写；iPXE 上报的 i386、arm64 分别对应 x86_64、aarch64，" +
			"pcbios、efi 分别对应 bios、uefi。不确定就留空：留空的建任务时不比对，引导时按机器实际上报的值校验"},
	// 实现的种类既不是 fragment，也不是 script，含空串（T7-2）。界面上选不出这种值，只会来自直接
	// 调接口或库里的坏数据。参数：种类。
	"impl_kind_unknown": {CategoryIncomplete, 1,
		"实现的种类「%[1]v」不认识",
		"实现只有片段（fragment）和脚本（script）两种；给这份实现选其中一种"},
	// 取值来源既不是 fixed、attr，也不是 default，含零值（比如 JSON 里漏了 Source，T7-2）。
	// 参数：参数名、来源。
	"binding_source_unknown": {CategoryIncomplete, 2,
		"参数「%[1]v」的取值来源「%[2]v」不认识",
		"取值来源只有写死的值（fixed）、主机属性（attr）和声明的默认值（default）三种；给这个参数重新选一种"},
	// 参数按参数求值的规则必填（被正文或所选片段引用，或是脚本声明的 Required），取值来源却锁定在
	// 默认值上、声明里又没有默认值（L-4）：向导里这一格锁着填不了，每台主机都会报 param_required。
	// 参数：参数名。
	"param_locked_without_value": {CategoryIncomplete, 1,
		"参数「%[1]v」必填，取值来源却锁定在默认值上，而它的声明里没有默认值",
		"给这个参数的声明补上默认值，或在装配表里改用写死的值、主机属性，或者设成向导里可改"},
	// 脚本正文里出现了 slot、lober、sys、param 四种占位符，写错的同种类也算（L-1）：脚本按原样
	// 下发，这些占位符永远不会被替换。参数：占位符原文。
	"script_placeholder_not_expanded": {CategoryIncomplete, 1,
		"脚本里的「%[1]v」不会被替换：脚本按原样下发，不展开 lober 的占位符",
		"参数改从环境变量取，变量名是 LOBER_PARAM_ 加大写的参数名（{{param:disk}} 对应 LOBER_PARAM_DISK），其余占位符删掉；" +
			"确实要写出这串字符，就把 {{ 拆开拼接，{{sys:lbrace}} 在脚本里同样不会展开"},
	// 锁定、取自主机属性的格子，这台主机登记的值通不过参数的类型校验，或取不出正文与所选片段要用
	// 的子值（如登记的是 IPv6，片段取 .netmask，T11-8）。这一格操作员改不了，要改的是主机登记。
	// 参数：参数的显示名、属性键。
	"param_attr_unusable": {CategoryIncompatible, 2,
		"参数「%[1]v」锁定取自主机属性「%[2]v」，这台主机登记的值用不了：格式不对，或取不出模板要用的部分",
		"到主机登记里改正这台主机的属性「%[2]v」"},
	// 可改的格子填的值取不出正文与所选片段要用的子值（如填了 IPv6，模板取 .netmask，T11-8）。
	// 参数：参数的显示名、子值名。
	"param_sub_unavailable": {CategoryInvalid, 2,
		"参数「%[1]v」填的值取不出模板要用的「%[2]v」",
		"改填一个取得出「%[2]v」的值（netmask 只有 IPv4 地址才有）"},
	// ISO 处于导入失败状态（T5-1）。导入失败的可以直接重新导入，不用重新获取字节（spec §5.1 第 5 步），
	// 所以不与 iso_not_ready 共用「等它导入完成」的下一步。
	"iso_import_failed": {CategoryState, 0,
		"ISO 导入失败，不能用来建任务",
		"重新导入这个 ISO，不用重新获取字节；仍然不行就删掉后重新创建，或换一个可用的 ISO"},

	// —— 第 2 层追加（plans/2026-10-03-rewrite-layer2-store.md 任务 1） ——

	// 建任务时批里的 SN 没有登记：从没登记过，或者在向导里选中之后被删了。准入判定的入参只有登记过的
	// 主机，查不出这种情况，由仓储在事务里重跑准入时补报（store.BatchProblems）。Subject 的 Kind 为 host，
	// ID 为 SN；SN 只在 Subject 里，所以不带参数。
	"host_not_registered": {CategoryState, 0,
		"这台主机还没登记，或者已经被删了",
		"到主机列表里重新登记它，再建任务"},

	// —— 第 2 层修补轮追加（plans/2026-10-08-layer2-fixups.md 任务 U5，S10-2） ——

	// 用户新建、派生或改名时，名称用了只给内置内容用的前缀（NameReserved，spec C12），比较不分大小写。名称先要合
	// name_invalid 的规则，不合的只报 name_invalid。由仓储的用户入口（store.NormalizeName）产出，Subject 同名称的其他
	// 问题：新建与派生时为 {hook 或 template, 空}，改名时为 {hook 或 template, 身份 ID}。参数：规范化之后的名称。
	"name_reserved": {CategoryInvalid, 1,
		"名称「%[1]v」不能用：以 lober: 开头的名称只给 lober 自带的内置内容用",
		"换一个不以 lober: 开头的名称，大写的写法也算在内；照着内置内容起名时，要把这个前缀去掉"},

	// 第 3 层渲染骨架：请求没给正文或拼装需要的系统值。参数：系统值标识。
	"render_system_value_missing": {CategoryInvalid, 1,
		"渲染请求缺少系统值「%[1]v」",
		"由调用方补齐本次请求需要的「%[1]v」；制品地址要通过 artifacts 基地址提供"},
	// repo、iso 只有制品基地址与快照里的同名角色这一份来源。参数：保留键。
	"render_system_value_reserved": {CategoryInvalid, 1,
		"系统值「%[1]v」由渲染根据制品记录拼出，不能由调用方另填",
		"移除「%[1]v」，提供 artifacts 基地址，并在 ISO 制品记录里补齐同名角色"},
	// 请求用到了 repo 或 iso，但快照没有对应的制品路径。参数：角色。
	"render_artifact_missing": {CategoryInvalid, 1,
		"快照缺少制品角色「%[1]v」的路径，无法拼出地址",
		"检查 ISO 导入记录，补齐「%[1]v」制品后重新构建任务快照"},
	// FileScript 没指定执行插槽，或片段展开找不到对应片段插槽。参数：插槽名。
	"render_slot_invalid": {CategoryInvalid, 1,
		"渲染请求指定的插槽「%[1]v」不存在或类型不符",
		"运行脚本请求要指定快照中的执行插槽，片段占位符要对应片段插槽"},
	// 拼装器未提供执行插槽或系统部件的单行替换。参数：占位符原文。
	"render_replacement_missing": {CategoryInvalid, 1,
		"渲染缺少「%[1]v」的单行内容",
		"由该格式的拼装器提供执行插槽命令或系统部件内容"},
	// 单行替换里出现格式不能容纳的字符。参数：占位符原文。
	"render_line_invalid": {CategoryInvalid, 1,
		"「%[1]v」的替换内容不是可内联的安全单行",
		"替换内容不能含换行、引号、<>&、反斜杠、# 或冒号加空格；检查拼装器与本次请求的系统值"},
	// Actual firmware is required by menu rendering; registered metadata is optional.
	"render_firmware_invalid": {CategoryInvalid, 1,
		"引导渲染请求的实际固件「%[1]v」无效或缺失",
		"由调用方将本次 iPXE 平台换算为 bios 或 uefi 后填写，不能使用登记值兜底"},
	// Series determines both family and template format.
	"render_boot_snapshot_invalid": {CategoryIncompatible, 0,
		"引导快照的系、族或模板格式不一致",
		"按 ISO 的系重新构建与模板格式和族一致的快照"},
	// Original immutable media config must be supplied by the caller.
	"render_esxi_bootcfg_invalid": {CategoryInvalid, 0,
		"原始 ESXi boot.cfg 缺失、格式错误或文件路径不属于发布的制品树",
		"由调用方提供该 ISO 不可变版本的原始 boot.cfg，保留内核、模块列表与选项，检查发布树中的完整路径"},
	"render_windows_env_limit": {CategoryInvalid, 1,
		"Windows 目标解释器无法完整保留参数「%[1]v」的环境变量值",
		"缩短该参数取值，使环境变量完整长度不超过目标解释器的限制"},
	// 第 4 层：Subject.Kind=iso，ID=ISO.ID。识别问题 Path=media/series/version/arch，
	// 来源问题 Path=source.<role>（主介质为 source）；制品问题 Path=artifacts.<role>，工具问题 Path=tools.<name>。
	"iso_media_unrecognized":   {CategoryIncompatible, 0, "认不出安装介质的格式", "换一个支持的完整安装 ISO"},
	"iso_series_unrecognized":  {CategoryIncompatible, 1, "认出了 %[1]v 格式，但认不出内置系", "换一个支持的安装 ISO，或核实介质内的产品信息"},
	"iso_series_mismatch":      {CategoryIncompatible, 2, "声明的系 %[1]v 与介质的系 %[2]v 不一致", "按介质实际的系重新创建 ISO"},
	"iso_content_mismatch":     {CategoryIncompatible, 2, "介质摘要不符：期望 %[1]v，实际 %[2]v", "核对下载链接或上传文件，再重试获取"},
	"iso_version_missing":      {CategoryIncompatible, 1, "介质内取不出有效的%[1]v", "换一个带完整产品信息的安装 ISO；不能按文件名猜版本"},
	"iso_extra_source_invalid": {CategoryIncompatible, 1, "附加来源 %[1]v 缺失、不完整或与主介质的平台和版本不符", "登记同版本、同架构的附加来源，获取完成后重新导入"},
	"iso_repo_incomplete":      {CategoryIncompatible, 1, "安装仓库缺少 %[1]v", "换一个带完整仓库的安装 ISO，再重新导入"},
	"iso_artifact_missing":     {CategoryIncompatible, 1, "安装制品缺少 %[1]v", "换一个完整安装 ISO，再重新导入"},
	"iso_tool_missing":         {CategoryIncompatible, 1, "Windows 工具目录缺少 %[1]v", "把对应工具放入配置的工具目录，再重新导入"},
	"iso_wim_compressed":       {CategoryIncompatible, 0, "WIM 的 XML 资源使用不支持的压缩方式", "换一个 XML 资源未压缩或使用 XPRESS 的 install.wim，再重新导入"},
	"iso_source_rejected":      {CategoryInvalid, 1, "来源不合法：%[1]v", "核对链接或上传范围；本地目录受限时请运维调整允许的目录"},
	"iso_space_insufficient":   {CategoryIncomplete, 2, "暂存区只剩 %[1]v 字节，还差 %[2]v 字节", "清理暂存区，或改用允许目录内的服务器本地路径，再登记来源"},
	// 用户由存储契约定义，Subject.Kind 不属于核心的八个领域对象。
	"user_field_invalid": {CategoryInvalid, 1, "用户字段「%[1]v」不合法", "用户名用 1–32 个小写字母、数字或下划线并以字母开头；角色从内置五种中选择；新建时必须提供口令"},
}

// ISO 层选用的固定措辞集中在代码表文件；跨包调用需要导出。
const (
	ISOReasonURL    = "链接只支持 http 与 https，不能指向回环、链路本地或无效地址"
	ISOReasonPath   = "本地路径不在允许登记的目录里"
	ISOReasonChunk  = "分片越界、大小或内容与已登记的字节不符"
	ISOVersionLabel = "版本"
	ISOArchLabel    = "架构"
)

// 下面是代码表的补充：产出代码的函数在本包里、可选的措辞（不合法的原因、各类型
// 期望的格式、字段的称呼）又是固定几种的，每种措辞登记在这里，由那个函数选一条作为
// 参数传给 NewProblem，自己不写中文。

// 控制台非业务错误的展示文字，不把底层错误直接回给客户端。
const (
	ConsoleBadRequest             = "请求的内容不合法"
	ConsoleTemplateCurrent        = "所选模板不是当前已审批版本，请重新选择模板"
	ConsoleTaskControlUnavailable = "第 7 层（状态机）尚未实现，暂不能取消或重试任务"
	ConsoleSampleISO              = "示例 ISO"
	ConsoleRejectReason           = "驳回必须提供非空、可存为 UTF-8 文本的理由"
	ConsoleContentRange           = "Content-Range 必须给出与实际分片及已登记总长一致的范围，如 bytes 0-1048575/5242880"
	ConsoleCSVRead                = "CSV 读不成：%v"
	ConsoleCSVHeader              = "CSV 表头重复或属性键不合法：%q"
	ConsoleCSVNoSN                = "CSV 表头必须有 sn 一列"
	ConsoleCSVEmpty               = "CSV 至少需要一行主机记录"
	ConsoleUnauthorized           = "未登录或会话已失效"
	ConsoleForbidden              = "没有执行这项操作的权限"
	ConsoleNotFound               = "找不到这条记录"
	ConsoleConflict               = "记录已经被改过，重读之后再试"
	ConsoleDuplicate              = "已经有同键的记录了"
	ConsoleImmutable              = "这个版本已经定稿，不能改"
	ConsoleReferenced             = "还有别的记录引用着它，不能删"
	ConsoleInternal               = "服务端内部错误"
	ConsoleRejected               = "这批请求没有通过"
	ConsoleLoginFailed            = "用户名或口令不对"
	ConsolePasswordChangeRequired = "请先为当前账号设置与种子口令不同的新口令，再继续操作"
	ConsoleSelfDisable            = "不能禁用自己的账号"
	UserLabelUsername             = "用户名"
	UserLabelRole                 = "角色"
	UserLabelPassword             = "口令散列"
)

// sn_invalid 的原因，NormalizeSN 选用。
const (
	snReasonEmpty   = "为空"
	snReasonSpaces  = "中间有连续的空格"
	snReasonChar    = "含不可打印字符或非 ASCII 字符"
	snReasonTooLong = "超过 64 个字符"
)

// name_invalid 的原因，NormalizeName 选用。
const (
	nameReasonEmpty   = "为空"
	nameReasonSpace   = "中间有空格或其他空白"
	nameReasonChar    = "含英文字母、数字和美式键盘上可见的符号以外的字符，比如中文、全角字符或不可见字符"
	nameReasonTooLong = "超过 64 个字符"
)

// paramValueFormats 是 param_value_invalid 说明里「期望的格式」，键是参数类型的标识，
// 内容照计划任务 6 的类型表，由参数取值的 Parse 按声明的类型选用。列表的项数与总长
// 规则写在该代码的下一步里；int 的具体范围见下面的 intRange*。
var paramValueFormats = map[string]string{
	"word":     "由 1–128 个字母、数字或 ._:/@^+=- 字符组成的串，不能以 - 或 @ 开头",
	"int":      "十进制整数",
	"enum":     "列出的选项之一",
	"ip":       "不带 zone 的 IPv4 或 IPv6 地址，IPv4 的各段不带前导零",
	"cidr":     "「地址/前缀」的形式，如 10.0.0.3/8",
	"hostname": "主机名：以点分隔的若干段，每段 1–63 个字母、数字或连字符，不以连字符开头或结尾，总长不超过 253 个字符",
	"diskpath": "以 /dev/ 开头的磁盘路径，共 1–128 个字母、数字或 ._:/@^+=- 字符",
	"sshkey": "「算法 公钥 注释」的形式，注释可省；算法限 ssh-ed25519、ssh-rsa、ecdsa-sha2-nistp256、" +
		"ecdsa-sha2-nistp384、ecdsa-sha2-nistp521、sk-ssh-ed25519@openssh.com、sk-ecdsa-sha2-nistp256@openssh.com；" +
		"公钥主体解码后不超过 16384 字节",
	"pwhash": "以 $6$、$5$ 或 $y$ 开头的 crypt 口令散列，不带 rounds=，不超过 256 字节",
	"text":   "不超过 4096 字节、不含 NUL 的 UTF-8 文本",
}

// int 声明了上下限时，Parse 按 Min、Max 选下面一条，用 fmt 填入具体的数字，整条接在
// paramValueFormats["int"] 后面作为「期望的格式」：只说「在范围之内」，操作员不知道范围是多少
// （T5-4①）。开头已带连接用的逗号，Parse 自己不写中文标点。
const (
	intRangeMin  = "，不小于 %[1]v"          // 只声明了下限
	intRangeMax  = "，不大于 %[1]v"          // 只声明了上限
	intRangeBoth = "，在 %[1]v 到 %[2]v 之间" // 两端都声明了，含两端
)

// stateUnknown 是空状态在说明里的称呼，由 StateLabel 选用（C-7）：空状态没有显示名，也没有标识可以
// 回落，照写会显示成「现为「」」。它与不认识的状态一样，只会来自直接调接口或库里的坏数据。
const stateUnknown = "未知状态"

// fieldLabels 是 iso_incomplete、host_platform_invalid 里字段的中文称呼，键是 Subject.Path 里的
// 字段名；ISO 提供的集合以 offers/<集合名> 定位，称呼查 offers。由 ISO.Validate 与 Host.Validate 选用。
var fieldLabels = map[string]string{
	"family":   "族",
	"series":   "系",
	"arch":     "架构",
	"firmware": "固件",
	"version":  "版本",
	"name":     "名称",
	"offers":   "提供的选项集合",
}
