// Package admission 是准入判定：一台主机、一个 ISO、一个模板能不能配到一起建任务
// （C5、C22、C26、C27、C31，以及 C28 在建任务时的那一次）。三者在建任务之前互不知道
// 对方，能不能配到一起只在这里查（spec §3 总则）。向导前三步筛候选、提交时逐台校验都调
// 本包，两边用的是同一套规则（spec §4.1）。
//
// 提交时还要逐台跑参数求值（spec §5.3 第 6 步），所以筛出来的候选，提交时仍可能被参数求值拦下：
// 参数的问题大多取决于主机属性和操作员填的值，筛候选时还不知道。只凭模板、hook 与 ISO 就能定下的
// 那一条，即 ISO 没提供参数要的选项集合（C21），这里也查，条件与参数求值共用
// model.ParamDecl.OfferMissing，向导就不会列出注定用不了的模板（L-2）。
package admission

import (
	"cmp"
	"maps"
	"slices"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// HostSelectable 判断主机能不能选来建任务：已有未结束的任务时不能（C31）。向导照常列出
// 这台主机，只是不能选，并写明原因。两个人同时给同一台主机建任务时，由第 2 层的约束兜底。
func HostSelectable(h model.Host, hasOpenTask bool) []model.Problem {
	if hasOpenTask {
		return []model.Problem{model.NewProblem("host_has_open_task", hostSubject(h))}
	}
	return nil
}

// ISOUsable 判断 ISO 能不能给这几台主机建任务：ISO 要可用，即 model.ISO.Unusable 为空（导入完成
// 且记录合法，C5；已废弃的不能再被新引用，C26）；架构与固件要与每台主机登记的相符（C28 建任务时
// 的那一次），登记的取值本身不合法的报 host_platform_invalid（C-6，见 platformConflicts）。平台的
// 问题指向那台主机，按主机传入的顺序报出，向导据此灰显 ISO 并写明是哪几台。
//
// ISO 不可用时只报 Unusable 给的原因、不再逐台比平台：获取中、导入中的 ISO 还没识别出架构，ready
// 却缺架构的记录不全（L-6），拿它们去比，会给登记过的主机报出不成立的冲突，没登记平台的主机反倒
// 通过，而这个 ISO 本来就用不了。
func ISOUsable(iso model.ISO, hosts []model.Host) []model.Problem {
	if reasons := iso.Unusable(); len(reasons) > 0 {
		return reasons
	}
	var out []model.Problem
	for _, h := range hosts {
		out = append(out, platformConflicts(iso, h)...)
	}
	return out
}

// TemplateUsable 判断模板能不能配这个 ISO 建任务。向导筛候选时，t 是调用方用 model.CurrentApproved
// 取出的版本：向导里每个模板只提供它（C23）。第 2 层建任务时在事务里重跑准入，t 是任务里钉住的版本
// （TemplateID、TemplateVersion），不另取当前版本：已被新版本取代、但仍是已审批的旧版本照样通过。
// 这里不查 t 是不是当前版本，那由第 5 层在提交时用 model.CurrentApproved 比对。
//
// 这里不重做激活判定。批准时已经查过引用的 hook 版本存在且已审批，批准之后它们只可能被
// 废弃（C23 已审批的不可修改，C26 只许废弃、不许删除），所以只查四件事，依次报出：这个版本
// 是已审批的（C22）；它没有引用已废弃的 hook 版本（C26，经模板间接引用也算）；ISO 的系是它所勾
// 的系之一（C27）；ISO 提供了模板参数与所引 hook 参数要的选项集合（C21，L-2），缺的集合各报一条，
// 见 offerProblems。hooks 里缺了某个引用时当作没废弃，也不看它的参数：那是调用方漏给的，参数
// 求值会报 hook_ref_missing。
//
// 系没勾时不查集合（C-1）：模板本不为这个系而写，集合缺不缺无从谈起。参数求值与快照构建遇到这种
// 情况也只报 series_not_ticked，这里多报一条 iso_offer_missing，就与它们一宽一严。
//
// ISO 不可用时也不查集合，理由同 ISOUsable 不逐台比平台：获取中、导入中的 ISO 还没有提供的集合，
// 拿它去查会报出不成立的 iso_offer_missing，而这个 ISO 本来就用不了。
//
// 引用了已废弃的 hook 版本时挡住的就是这个版本，不回落到更早的已审批版本：回落会让操作员
// 选的是当前版本、装出来的却是旧内容。下一步是作者升级引用，审批出新版本。
func TemplateUsable(t model.Template, hooks map[model.HookKey]model.Hook, iso model.ISO) []model.Problem {
	subject := model.Subject{Kind: model.KindTemplate, ID: (model.TemplateKey{ID: t.ID, Version: t.Version}).String()}
	var out []model.Problem
	if t.State != model.VersionApproved {
		// 说明里给状态的称呼：中文显示名，不认识的回落到标识，空状态回落到「未知状态」（T5-1、C-7）。
		out = append(out, model.NewProblem("template_not_approved", subject, model.StateLabel(t.State)))
	}
	for _, k := range deprecatedRefs(t, hooks) {
		out = append(out, model.NewProblem("template_blocked_by_deprecated", subject, k.ID, k.Version))
	}
	switch {
	case !slices.Contains(t.Series, iso.Series):
		out = append(out, model.NewProblem("series_not_ticked", subject, string(iso.Series)))
	case iso.Usable():
		out = append(out, offerProblems(subject, paramDecls(t, hooks), iso.Offers)...)
	}
	return out
}

// BatchInput 是一批建任务的输入：每台主机一个任务，同一批用同一个 ISO 和模板（spec §2.7）。
type BatchInput struct {
	Hosts    []model.Host
	OpenTask map[string]bool // SN → 是否已有未结束的任务
	ISO      model.ISO
	// Template 是这一批用的模板版本，见 TemplateUsable。第 2 层在事务里重跑时是任务里钉住的版本，不另取
	// 当前版本；它是不是当前的已审批版本（model.CurrentApproved）由第 5 层在提交时比对。
	Template model.Template
	Hooks    map[model.HookKey]model.Hook
}

// BatchResult 是一批的判定结果，Common 与 ByHost 里全都没有问题才算通过。批量建任务是
// 原子的（C30）：任何一台不通过，一台都不建，所以要逐台列出原因。
//
// 没有问题时给的是空切片而不是 nil：第 5 层把结果编码成 JSON 逐台返回，nil 会编成 null。
type BatchResult struct {
	Common []model.Problem            // ISO 与模板的问题，整批只报一次
	ByHost map[string][]model.Problem // SN → 这台主机的问题，每台主机都有键
}

// Batch 判定一批主机，给提交时的逐台校验用：服务端不信任向导的筛选结果，整批重跑一遍
// （spec §5.3 第 6 步）。规则就是上面三个函数的，只是按对象分开放：ISO 与模板的问题放
// Common；主机的问题放 ByHost，依次是批内重复、未结束的任务、架构、固件（冲突，或登记的取值不合法）。
//
// 同一个 SN 出现不止一次时，几处都算错，不是第一处照常建、后面的才算重复。几处按 SN 共用
// ByHost 里的一个键，host_duplicate_in_batch 在这个键下报一次；几处是同一条登记，这台主机
// 的其他问题也只按第一处判一次，不会成倍报出。
func Batch(in BatchInput) BatchResult {
	isoProblems := in.ISO.Unusable()
	usable := len(isoProblems) == 0
	common := append([]model.Problem{}, isoProblems...)
	common = append(common, TemplateUsable(in.Template, in.Hooks, in.ISO)...)

	seen := make(map[string]int, len(in.Hosts))
	for _, h := range in.Hosts {
		seen[h.SN]++
	}
	byHost := make(map[string][]model.Problem, len(seen))
	for _, h := range in.Hosts {
		if _, done := byHost[h.SN]; done {
			continue
		}
		problems := []model.Problem{}
		if seen[h.SN] > 1 {
			problems = append(problems, model.NewProblem("host_duplicate_in_batch", hostSubject(h)))
		}
		problems = append(problems, HostSelectable(h, in.OpenTask[h.SN])...)
		if usable { // 理由同 ISOUsable：不拿不可用的 ISO 逐台比平台
			problems = append(problems, platformConflicts(in.ISO, h)...)
		}
		byHost[h.SN] = problems
	}
	return BatchResult{Common: common, ByHost: byHost}
}

func hostSubject(h model.Host) model.Subject {
	return model.Subject{Kind: model.KindHost, ID: h.SN}
}

// platformConflicts 报 ISO 与一台主机的平台问题，架构在前、固件在后。iso 必须可用（调用方先查
// Unusable），PlatformOK 才有合法的系与架构可比。规则本身在 catalog.PlatformOK 里：主机没登记的
// 那一项算通过（C28 登记了才校验）；网关引导时也用它，两次校验因此是同一套规则。
//
// 主机登记的架构或固件不合法时（大小写不对、写成别名或 iPXE 的上报值），这一项报 Host.Validate 给出的
// host_platform_invalid，不再拿去比（C-6）：不合法的值比不上任何 ISO，比了只会报出「系不支持用 UEFI
// 固件引导」这类说法，真实原因却是登记写错了。另一项合法的照常比。Host.Validate 的其余问题（属性）与
// 建任务配哪个 ISO 无关，不在这里报。
func platformConflicts(iso model.ISO, h model.Host) []model.Problem {
	invalid := map[string]model.Problem{} // 不合法的那一项（arch 或 firmware）→ Host.Validate 给的问题
	for _, p := range h.Validate() {
		if p.Code == "host_platform_invalid" {
			invalid[p.Subject.Path] = p
		}
	}
	archOK, fwOK := catalog.PlatformOK(iso.Series, iso.Arch, h.Arch, h.Firmware)
	var out []model.Problem
	if p, bad := invalid["arch"]; bad {
		out = append(out, p)
	} else if !archOK {
		out = append(out, model.NewProblem("arch_mismatch", hostSubject(h), string(iso.Arch), string(h.Arch)))
	}
	if p, bad := invalid["firmware"]; bad {
		out = append(out, p)
	} else if !fwOK {
		out = append(out, model.NewProblem("firmware_mismatch", hostSubject(h), string(iso.Series), string(h.Firmware)))
	}
	return out
}

// deprecatedRefs 列出模板装配表引用的已废弃 hook 版本，每个一次，按 ID、版本排序：装配表
// 是 map，遍历顺序不固定；同一个 hook 版本放进几个实例（C17），说的也是同一处要升级。
func deprecatedRefs(t model.Template, hooks map[model.HookKey]model.Hook) []model.HookKey {
	var out []model.HookKey
	for _, insts := range t.Assembly {
		for _, inst := range insts {
			k := model.HookKey{ID: inst.HookID, Version: inst.HookVersion}
			if h, ok := hooks[k]; ok && h.State == model.VersionDeprecated && !slices.Contains(out, k) {
				out = append(out, k)
			}
		}
	}
	slices.SortFunc(out, func(a, b model.HookKey) int {
		return cmp.Or(cmp.Compare(a.ID, b.ID), cmp.Compare(a.Version, b.Version))
	})
	return out
}

// paramDecls 依次列出模板参数，与装配表里各实例所引 hook 的参数：插槽按名字的字典序，实例按在
// 插槽里的顺序。装配表是 map，排过序，同一个模板每次判定的结果才一样。hooks 里缺了的引用没有参数
// 可列。先复制模板参数再追加：直接往 t.Params 上 append，可能写进调用方切片的底层数组。
func paramDecls(t model.Template, hooks map[model.HookKey]model.Hook) []model.ParamDecl {
	decls := slices.Clone(t.Params)
	for _, slot := range slices.Sorted(maps.Keys(t.Assembly)) {
		for _, inst := range t.Assembly[slot] {
			decls = append(decls, hooks[model.HookKey{ID: inst.HookID, Version: inst.HookVersion}].Params...)
		}
	}
	return decls
}

// offerProblems 为 offers 里缺的每个集合报一条 iso_offer_missing，次序是 model.MissingOffers 给的
// 声明顺序。说明里称呼第一个要这个集合的参数，用显示名，没写时用参数名，与参数求值里那一格的称呼
// 相同。
func offerProblems(subject model.Subject, decls []model.ParamDecl, offers map[string][]model.Option) []model.Problem {
	var out []model.Problem
	for _, name := range model.MissingOffers(decls, offers) {
		// MissingOffers 只列出有声明要的集合，这里一定找得到。
		i := slices.IndexFunc(decls, func(d model.ParamDecl) bool { return d.Offer == name && d.OfferMissing(offers) })
		out = append(out, model.NewProblem("iso_offer_missing", subject, cmp.Or(decls[i].Label, decls[i].Name), name))
	}
	return out
}
