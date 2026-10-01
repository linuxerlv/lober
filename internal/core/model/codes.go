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
// 全层的中文说明只写在本文件里，参数只放数据：标识、名字或显示名、取值、数字、
// 占位符原文。一个代码有几种原因时，说明把条件写全；只有产出它的函数在本包里、
// 可选的措辞又是固定几种的，才把每种措辞登记在文件末尾，由那个函数选一条作参数。
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
	// 名称为空、超长、含控制字符或不可见字符，或不是合法的 UTF-8。
	// 参数：原因，取文件末尾的 nameReason* 之一。
	"name_invalid": {CategoryInvalid, 1,
		"名称不合法：%[1]v",
		"名称要 1–64 个字（一个汉字算一个），不能含控制字符或不可见字符"},

	// —— 任务 6：参数类型与取值 ——

	// 参数声明本身不合法：名字不是标识符或重名、类型不认识、text 声明成列表、默认值通不过
	// 校验、enum 的选项值不是安全词、Options 与 Offer 同给、int 的下限大于上限。参数：参数名。
	"param_decl_invalid": {CategoryInvalid, 1,
		"参数「%[1]v」的声明不合法：名字、类型、是否列表、默认值、选项或上下限有不符合规则的地方",
		"名字要以小写字母开头，只含小写字母、数字和下划线，最长 32 个字符，且不与其他参数重名；" +
			"类型只能是 word、int、enum、ip、cidr、hostname、diskpath、sshkey、pwhash、text，text 不能声明成列表；" +
			"默认值要能通过这个参数自己的校验；enum 的选项值要是安全词，静态选项和 ISO 提供的集合只能给一种；" +
			"int 的下限不能大于上限"},
	// 取值通不过类型校验，或不在静态选项里。
	// 参数：参数名、期望的格式（按参数类型取文件末尾 paramValueFormats 里的那一条）。
	"param_value_invalid": {CategoryInvalid, 2,
		"参数「%[1]v」的取值不合法：应为%[2]v",
		"按说明里的格式重新填写；列表参数每行一项，共 1–256 项"},
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
	// 同一行里没有 }}。
	"placeholder_unclosed": {CategoryInvalid, 0,
		"占位符在这一行里没有闭合",
		"在同一行里补上 }}，占位符不能跨行；要原样输出 {{，写成 {{sys:lbrace}}"},
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
	// 实现的族不合法，没勾系，系不合法或不属于该族。
	"impl_series_invalid": {CategoryIncomplete, 0,
		"实现的族或系不对：族不合法、没勾系，或者勾了不合法、不属于这个族的系",
		"给这份实现选一个合法的族，再勾该族下的一个或多个系"},
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
	// 模板没选族、没勾系，系不合法或不属于该族。
	"template_series_invalid": {CategoryIncomplete, 0,
		"模板的族或系不对：没选族、族不合法、没勾系，或者勾了不合法、不属于这个族的系",
		"先选族，再勾该族下至少一个系；没勾系的模板不能激活"},
	// 模板勾的系不同格式。参数：涉及的格式（调用方用顿号连好）。
	"template_mixed_formats": {CategoryIncomplete, 1,
		"模板勾的系不是同一种格式：%[1]v",
		"一个模板只能勾同一种格式的系，正文按这种格式写；其他格式的系另建模板"},
	// 插槽名重复。参数：插槽名。
	"slot_duplicate": {CategoryIncomplete, 1,
		"插槽「%[1]v」在正文里出现了不止一次",
		"插槽名在一个模板里要唯一；给其中一个改名"},
	// 执行插槽的时机该格式不支持。参数：时机、格式。
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
	// 实现种类与插槽类型不一致。参数：hook ID、版本、系、实现种类（fragment 或 script）。
	"impl_kind_mismatch": {CategoryIncomplete, 4,
		"hook %[1]v@%[2]v 覆盖系「%[3]v」的实现是 %[4]v，与插槽类型不一致",
		"片段只能放进片段插槽，脚本只能放进执行插槽；换一个插槽，或换一个 hook"},
	// 执行插槽的时机不在脚本声明的时机里。参数：hook ID、版本、系、插槽的时机。
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
	// 参数要的选项集合 ISO 没有提供。参数：字段的显示名、集合名。
	"iso_offer_missing": {CategoryIncompatible, 2,
		"参数「%[1]v」的选项取自 ISO 的 %[2]v，但这个 ISO 没有提供",
		"换一个提供了 %[2]v 的 ISO，或重新导入这个 ISO"},

	// —— 任务 12：准入判定与候选筛选 ——

	// 主机已有未结束的任务。
	"host_has_open_task": {CategoryState, 0,
		"这台主机已有未结束的任务",
		"等那个任务结束，或先取消它"},
	// ISO 不可用；已废弃也归这里。参数：现状（获取中、导入中、导入失败、已废弃）。
	"iso_not_ready": {CategoryState, 1,
		"ISO 不可用：%[1]v",
		"等它导入完成后再用，或换一个可用的 ISO；已废弃的 ISO 不能再用来建任务"},
	// 模板版本不是已审批。参数：当前状态。
	"template_not_approved": {CategoryState, 1,
		"模板的这个版本不是已审批状态（现为 %[1]v）",
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
	// ISO 的系不在模板所勾的系里。参数：ISO 的系。
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
		"能代入 {{sys:sn}} 的 SN 只能由字母、数字和 ._:/@^+=- 组成，不能有空格；给这台主机换一个不引用 {{sys:sn}} 的模板，" +
			"或在模板正文与片段里去掉 {{sys:sn}}、改用取自主机属性的 word 参数，审批后再建任务"},
}

// 下面是代码表的补充：产出代码的函数在本包里、可选的措辞（不合法的原因、各类型
// 期望的格式）又是固定几种的，每种措辞登记在这里，由那个函数选一条作为参数传给
// NewProblem，自己不写中文。

// sn_invalid 的原因，NormalizeSN 选用。
const (
	snReasonEmpty   = "为空"
	snReasonSpaces  = "中间有连续的空格"
	snReasonChar    = "含不可打印字符或非 ASCII 字符"
	snReasonTooLong = "超过 64 个字符"
)

// name_invalid 的原因，NormalizeName 选用。
const (
	nameReasonNotUTF8   = "不是合法的 UTF-8 文本"
	nameReasonEmpty     = "为空"
	nameReasonInvisible = "含控制字符或不可见字符"
	nameReasonTooLong   = "超过 64 个字"
)

// paramValueFormats 是 param_value_invalid 说明里「期望的格式」，键是参数类型的标识，
// 内容照计划任务 6 的类型表，由参数取值的 Parse 按声明的类型选用。列表的项数规则
// 写在该代码的下一步里。
var paramValueFormats = map[string]string{
	"word":     "由 1–128 个字母、数字或 ._:/@^+=- 字符组成的串",
	"int":      "十进制整数，声明了上下限的还要在范围之内",
	"enum":     "列出的选项之一",
	"ip":       "不带 zone 的 IPv4 或 IPv6 地址，IPv4 的各段不带前导零",
	"cidr":     "「地址/前缀」的形式，如 10.0.0.3/8",
	"hostname": "主机名：以点分隔的若干段，每段 1–63 个字母、数字或连字符，不以连字符开头或结尾，总长不超过 253 个字符",
	"diskpath": "以 /dev/ 开头的磁盘路径，共 1–128 个字母、数字或 ._:/@^+=- 字符",
	"sshkey": "「算法 公钥 注释」的形式，注释可省；算法限 ssh-ed25519、ssh-rsa、ecdsa-sha2-nistp256、" +
		"ecdsa-sha2-nistp384、ecdsa-sha2-nistp521、sk-ssh-ed25519@openssh.com、sk-ecdsa-sha2-nistp256@openssh.com",
	"pwhash": "以 $6$、$5$ 或 $y$ 开头的 crypt 口令散列",
	"text":   "不超过 4096 字节、不含 NUL 的 UTF-8 文本",
}
