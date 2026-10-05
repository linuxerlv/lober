package store

import (
	"errors"

	"github.com/lober/lober/internal/core/model"
)

// 主机与 ISO 的入参检查，两份实现调用同一份：规则写两遍就会走偏（计划「包与文件」）。要查库里数据的那一半（SN 登没
// 登记、SHA256 被谁占着）由实现在事务里查，判定的条件仍在这里。

// CheckHosts 查要写入的一批主机：CreateHost、UpdateHost 是只有一台的批，ImportHosts 一批至少一台。SN 已经规范化，记录
// 过 Host.Validate，批里 SN 不重复。
//
// 能用原因代码表达的问题都收进 ProblemsError(ErrInvalid)，与 model 的校验完全相同：按入参的顺序，每台先报批内重复
// （只在这个 SN 第一次出现时报一条），再报 NormalizeSN 与 Host.Validate 的问题。SN 合法却不是规范化之后的写法，
// 是调用方漏了规范化，model 的校验对它没有问题可报，报不带问题的 ErrInvalid；空的一批同样。
func CheckHosts(hs []model.Host) error {
	if len(hs) == 0 {
		return invalidf("一批至少要有一台主机")
	}
	count := make(map[string]int, len(hs))
	for _, h := range hs {
		count[h.SN]++
	}
	var ps []model.Problem
	for _, h := range hs {
		if count[h.SN] > 1 {
			ps = append(ps, model.NewProblem("host_duplicate_in_batch", model.Subject{Kind: "host", ID: h.SN}))
			count[h.SN] = 0 // 报过了，后面再遇到它不再报
		}
		n, err := model.NormalizeSN(h.SN)
		var p model.Problem
		switch {
		case errors.As(err, &p):
			ps = append(ps, p)
		case n != h.SN:
			return invalidf("SN %q 不是规范化之后的写法 %q：调用方要先经 model.NormalizeSN", h.SN, n)
		}
		ps = append(ps, h.Validate()...)
	}
	if len(ps) > 0 {
		return &ProblemsError{Kind: ErrInvalid, Problems: ps}
	}
	return nil
}

// CheckHostQuery 查 ListHosts 的入参，返回实际要取的条数：分页的规则同 CheckQuery；AttrKey 与 AttrValue 要么都给，
// 要么都不给，否则报 ErrInvalid。
func CheckHostQuery(q HostQuery) (int, error) {
	limit, err := CheckQuery(q.Cursor, q.Limit)
	if err != nil {
		return 0, err
	}
	if (q.AttrKey == "") != (q.AttrValue == "") {
		return 0, invalidf("AttrKey 与 AttrValue 要么都给，要么都不给")
	}
	return limit, nil
}

// CheckNewISO 查 CreateISO 的入参：只填 Name、Family、Series，其余字段由仓储与第 4 层的导入填，调用方填了，多半是把
// 别的记录当成了新建的入参；系要属于族（C2），两者都是目录里的原样写法；名称存得下。不合的报 ErrInvalid。
func CheckNewISO(iso model.ISO) error {
	switch {
	case iso.ID != "" || iso.Version != "" || iso.Arch != "" || iso.State != "" || iso.SHA256 != "" ||
		iso.Artifacts != nil || iso.Offers != nil || iso.Rev != 0:
		return invalidf("新建 ISO 只填 Name、Family、Series，其余由仓储与导入填")
	case !iso.Family.Valid() || !iso.Series.Valid() || iso.Series.Family() != iso.Family:
		return invalidf("族 %q 与系 %q 对不上：两者都要是目录里的原样写法，系要属于族（C2）", iso.Family, iso.Series)
	case !model.StorableText(iso.Name):
		return invalidf("ISO 的名称存不下：不是合法的 UTF-8，或含 NUL")
	}
	return nil
}

// CheckISOUpdate 是 UpdateISO 动数据之前的检查：ID 合格式（CheckID）；目标状态是 model 认得的，并且不是 deprecated：
// 废弃是要审计的决定，走 DeprecateISO，不审计的系统更新做不了。不合的报 ErrInvalid。
func CheckISOUpdate(iso model.ISO) error {
	if err := CheckID(iso.ID); err != nil {
		return err
	}
	switch {
	case iso.State == model.ISODeprecated:
		return invalidf("系统更新不能把 ISO 置为已废弃：废弃要审计，走 DeprecateISO")
	case !iso.State.Valid():
		return invalidf("ISO 的状态 %q 不认识", iso.State)
	}
	return nil
}

// CheckISOUpdateFrom 是 UpdateISO 在事务里读到现有的记录 old（Rev 已经比对过）之后的检查，依次是：
//
//   - old 已废弃报 ErrImmutable：废弃是要审计的决定，不审计的系统更新不能把它撤掉。
//   - 族、系与名称不能经系统更新改，iso 与 old 的不同报 ErrInvalid。
//   - 要置为 ready 的必须通过 ISO.Validate（C5），否则报 ProblemsError(ErrInvalid)，问题原样带出。
//   - 写进来的文本都要存得下（CheckISOText），不论置为什么状态：Validate 不看制品与 SHA256，别的状态也不跑它。
//
// 都过了，实现再查 SHA256 有没有被别的 ISO 占着（HoldsSHA256）。
func CheckISOUpdateFrom(old, iso model.ISO) error {
	switch {
	case old.State == model.ISODeprecated:
		return errorf(ErrImmutable, "ISO %s 已废弃，不再接受系统更新", iso.ID)
	case iso.Family != old.Family || iso.Series != old.Series || iso.Name != old.Name:
		return invalidf("系统更新不能改 ISO %s 的族、系与名称", iso.ID)
	}
	if iso.State == model.ISOReady { // C5：导入完成的 ISO 必须通过完整性判定
		if ps := iso.Validate(); len(ps) > 0 {
			return &ProblemsError{Kind: ErrInvalid, Problems: ps}
		}
	}
	return CheckISOText(iso)
}

// CheckISOText 查系统更新写进来的文本都存得下（model.StorableText，spec §3 总则）：版本、架构、SHA256、制品的角色与
// 路径、提供的集合名、选项的值与显示名，存不下的报 ErrInvalid。PostgreSQL 的 text 与 jsonb 都不收 NUL，encoding/json
// 还会把非法的 UTF-8 换成 U+FFFD，读回来的就不是写进去的。名称、族与系不能经系统更新改，新建时已经查过。
func CheckISOText(iso model.ISO) error {
	bad := func(what string) error {
		return invalidf("ISO %s 的%s存不下：不是合法的 UTF-8，或含 NUL", iso.ID, what)
	}
	switch {
	case !model.StorableText(iso.Version):
		return bad("版本")
	case !model.StorableText(string(iso.Arch)):
		return bad("架构")
	case !model.StorableText(iso.SHA256):
		return bad(" SHA256 ")
	}
	for role, path := range iso.Artifacts {
		if !model.StorableText(role) || !model.StorableText(path) {
			return bad("制品")
		}
	}
	for name, opts := range iso.Offers {
		if !model.StorableText(name) {
			return bad("提供的集合名")
		}
		for _, o := range opts {
			if !model.StorableText(o.Value) || !model.StorableText(o.Label) {
				return bad("提供的选项")
			}
		}
	}
	return nil
}

// HoldsSHA256 报告 ISO 占不占着它的 SHA256（C4）：空的 SHA256 与导入失败的 ISO 都不占位，所以导入失败的 ISO 可以记下
// 与别的 ISO 相同的内容，指向那一个（spec §5.1 第 3 步）。系统更新写下的 SHA256 撞上另一个占着它的 ISO，报
// DuplicateError。pgstore 的部分唯一索引 idx_isos_sha256 的谓词是同一个条件。
func HoldsSHA256(iso model.ISO) bool {
	return iso.SHA256 != "" && iso.State != model.ISOFailed
}

// CheckISODeprecation 是 DeprecateISO 在事务里读到 ISO id 现在的状态 state 之后的检查：只有 ready 的能废弃，其他状态
// 报 ErrConflict，调用方重读之后再判断。
func CheckISODeprecation(id string, state model.ISOState) error {
	if state != model.ISOReady {
		return errorf(ErrConflict, "ISO %s 现为 %s，只有 ready 的能废弃", id, state)
	}
	return nil
}
