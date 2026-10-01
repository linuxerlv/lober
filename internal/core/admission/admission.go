// Package admission 是准入判定：一台主机、一个 ISO、一个模板能不能配到一起建任务
// （C5、C22、C26、C27、C31，以及 C28 在建任务时的那一次）。三者在建任务之前互不知道
// 对方，能不能配到一起只在这里查（spec §3 总则）。向导前三步筛候选、提交时逐台校验都调
// 本包，两边用的是同一套规则，筛出来的候选和提交时的校验结果不会对不上（spec §4.1）。
package admission

import (
	"cmp"
	"slices"
	"strconv"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// HostSelectable 判断主机能不能选来建任务：已有未结束的任务时不能（C31）。向导照常列出
// 这台主机，只是不能选，并写明原因。两个人同时给同一台主机建任务时，由第 2 层的约束兜底。
func HostSelectable(h model.Host, hasOpenTask bool) []model.Problem {
	if hasOpenTask {
		return []model.Problem{model.NewProblem("host_has_open_task", hostSubject(h))}
	}
	return nil
}

// ISOUsable 判断 ISO 能不能给这几台主机建任务：ISO 要可用（C5；已废弃的不能再被新引用，
// C26），架构与固件要与每台主机登记的相符（C28 建任务时的那一次）。平台冲突指向那台主机，
// 按主机传入的顺序报出，向导据此灰显 ISO 并写明是哪几台。
//
// ISO 不可用时只报 iso_not_ready、不再逐台比平台：获取中、导入中的 ISO 还没识别出架构，
// 拿它去比，会给登记过的主机报出不成立的冲突，而这个 ISO 本来就用不了。
func ISOUsable(iso model.ISO, hosts []model.Host) []model.Problem {
	if !iso.Usable() {
		return []model.Problem{isoNotReady(iso)}
	}
	var out []model.Problem
	for _, h := range hosts {
		out = append(out, platformConflicts(iso, h)...)
	}
	return out
}

// TemplateUsable 判断模板能不能配这个 ISO 建任务。t 必须是调用方用 model.CurrentApproved
// 取出的版本：向导里每个模板只提供它（C23）。
//
// 这里不重做激活判定。批准时已经查过引用的 hook 版本存在且已审批，批准之后它们只可能被
// 废弃（C23 已审批的不可修改，C26 只许废弃、不许删除），所以只查三件事：这个版本是已审批
// 的（C22）；它没有引用已废弃的 hook 版本（C26，经模板间接引用也算）；ISO 的系是它所勾的
// 系之一（C27）。hooks 里缺了某个引用时当作没废弃：那是调用方漏给的，参数求值会报
// hook_ref_missing。
//
// 引用了已废弃的 hook 版本时挡住的就是这个版本，不回落到更早的已审批版本：回落会让操作员
// 选的是当前版本、装出来的却是旧内容。下一步是作者升级引用，审批出新版本。
func TemplateUsable(t model.Template, hooks map[model.HookKey]model.Hook, iso model.ISO) []model.Problem {
	subject := model.Subject{Kind: "template", ID: t.ID + "@" + strconv.Itoa(t.Version)}
	var out []model.Problem
	if t.State != model.VersionApproved {
		out = append(out, model.NewProblem("template_not_approved", subject, string(t.State)))
	}
	for _, k := range deprecatedRefs(t, hooks) {
		out = append(out, model.NewProblem("template_blocked_by_deprecated", subject, k.ID, k.Version))
	}
	if !slices.Contains(t.Series, iso.Series) {
		out = append(out, model.NewProblem("series_not_ticked", subject, string(iso.Series)))
	}
	return out
}

// BatchInput 是一批建任务的输入：每台主机一个任务，同一批用同一个 ISO 和模板（spec §2.7）。
type BatchInput struct {
	Hosts    []model.Host
	OpenTask map[string]bool // SN → 是否已有未结束的任务
	ISO      model.ISO
	Template model.Template // 调用方用 model.CurrentApproved 取出的版本
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
// Common；主机的问题放 ByHost，依次是批内重复、未结束的任务、架构、固件。
//
// 同一个 SN 出现不止一次时，几处都算错，不是第一处照常建、后面的才算重复。几处按 SN 共用
// ByHost 里的一个键，host_duplicate_in_batch 在这个键下报一次；几处是同一条登记，这台主机
// 的其他问题也只按第一处判一次，不会成倍报出。
func Batch(in BatchInput) BatchResult {
	usable := in.ISO.Usable()
	common := []model.Problem{}
	if !usable {
		common = append(common, isoNotReady(in.ISO))
	}
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
	return model.Subject{Kind: "host", ID: h.SN}
}

// isoNotReady 的参数是 ISO 状态的标识，说明据此分得清是哪一种：已废弃的再也不能用，其余的
// 导入完成后还能用。中文只写在 model 的代码表里，所以这里传标识，不另写中文的现状。
func isoNotReady(iso model.ISO) model.Problem {
	return model.NewProblem("iso_not_ready", model.Subject{Kind: "iso", ID: iso.ID}, string(iso.State))
}

// platformConflicts 报 ISO 与一台主机的平台冲突，架构在前、固件在后。架构和固件都没登记的
// 主机不查（C28 登记了才校验）；规则本身在 catalog.PlatformOK 里，网关引导时也用它，
// 两次校验因此是同一套规则。
func platformConflicts(iso model.ISO, h model.Host) []model.Problem {
	if h.Arch == "" && h.Firmware == "" {
		return nil
	}
	archOK, fwOK := catalog.PlatformOK(iso.Series, iso.Arch, h.Arch, h.Firmware)
	var out []model.Problem
	if !archOK {
		out = append(out, model.NewProblem("arch_mismatch", hostSubject(h), string(iso.Arch), string(h.Arch)))
	}
	if !fwOK {
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
