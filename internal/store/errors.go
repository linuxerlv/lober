package store

import (
	"errors"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/lober/lober/internal/core/admission"
	"github.com/lober/lober/internal/core/model"
)

// 哨兵错误是仓储错误的类别：调用方用 errors.Is 判断，第 5 层据此映射 HTTP 状态码与说明。错误的文字只给日志
// 和开发者看。带类型的错误（DuplicateError、ProblemsError、BatchError）的 Unwrap 返回其中之一，细节用
// errors.As 取。
var (
	// ErrNotFound：按键找不到。
	ErrNotFound = errors.New("store：找不到")
	// ErrConflict：Rev 或期望的状态对不上，或者已有一个未审批的版本又要新开一个。调用方重读之后再判断
	// （spec §6.2 第 4 条）。还有一种：写入撞上了名字或 SHA256 的唯一约束，再查时占着它的那一方刚好变了（导入失败、
	// 删掉、改了名），报不出是谁；重读之后再写，得到的必然是成功或 DuplicateError。
	ErrConflict  = errors.New("store：Rev 或状态对不上，重读之后再判断")
	ErrLastAdmin = fmt.Errorf("%w: at least one enabled administrator is required", ErrConflict)
	// ErrDuplicate：唯一键撞了，总是包在 DuplicateError 里。
	ErrDuplicate = errors.New("store：唯一键重复")
	// ErrImmutable：改已审批、已废弃或内置的版本，或者改内置的身份（C23、C25）。系统更新（UpdateISO）碰到已废弃的
	// ISO 也报它：废弃是要审计的决定，不审计的更新不能把它撤掉。
	ErrImmutable = errors.New("store：不能修改")
	// ErrReferenced：删除被引用的对象：主机或 ISO 被任务引用，hook 草稿被模板版本引用（C26）。
	ErrReferenced = errors.New("store：仍被引用，不能删除")
	// ErrAuditRequired：要审计的操作没收到审计（Audit 是零值），一条都不写。
	ErrAuditRequired = errors.New("store：要审计的操作没有收到审计")
	// ErrInvalid：入参违反仓储的前提，比如 ID 格式、时间、快照与任务字段不一致、一批里的 ISO 或模板不同。
	// 能用原因代码表达的（记录本身没过 model 的校验、批里 SN 重复）包在 ProblemsError 里，问题列在 Problems 里。
	ErrInvalid = errors.New("store：入参不合法")
	// ErrRejected：在事务里对加锁后的最新数据重跑第 1 层的判定，没有通过：审批时重跑激活判定，建任务时重跑
	// 准入判定。总是包在 ProblemsError 或 BatchError 里。
	ErrRejected = errors.New("store：重跑判定没有通过")
	// ErrBuiltinMismatch：装载内置内容时，程序自带的与库里的对不上（spec §6.2 第 11 条），有两种情形。一是同一个
	// ID 与版本的内容不同：有人改了内置内容却没有升版本号。二是名字不同：名字记在身份上，随身份定下，以后的版本
	// 不能换名，换了名照样报它，只在装过旧版本的库上才报得出来。要改名只能换一个新 ID，旧身份和它的版本留在库里。
	ErrBuiltinMismatch = errors.New("store：内置内容与库里同一版本的不同")
)

// DuplicateError 是唯一键撞了。Field 是撞上的键：sn、name 或 sha256；Existing 是已有的那一个，主机给 SN，
const (
	DuplicateFieldSN       = "sn"
	DuplicateFieldName     = "name"
	DuplicateFieldSHA256   = "sha256"
	DuplicateFieldUsername = "username"
	TargetHost             = model.KindHost
	TargetISO              = model.KindISO
	TargetHook             = model.KindHook
	TargetTemplate         = model.KindTemplate
	TargetTask             = "task"
	TargetUser             = UserSubjectKind
)

// ReferencedError identifies deterministic blockers while preserving ErrReferenced.
type ReferencedError struct{ References []model.Subject }

func (e *ReferencedError) Error() string { return fmt.Sprintf("%v：%v", ErrReferenced, e.References) }
func (e *ReferencedError) Unwrap() error { return ErrReferenced }

// DuplicateError identifies the conflicting unique field and existing record.
// 其余给 ID。Unwrap 返回 ErrDuplicate。
type DuplicateError struct {
	Field    string
	Existing string
}

func (e *DuplicateError) Error() string {
	return fmt.Sprintf("%v：%s 撞上了已有的 %q", ErrDuplicate, e.Field, e.Existing)
}

func (e *DuplicateError) Unwrap() error { return ErrDuplicate }

// ProblemsError 是能用原因代码表达的问题。Kind 是它包着的哨兵值：ErrInvalid（入参有问题，比如记录没过 model
// 的校验）或 ErrRejected（在事务里重跑判定没通过）；Problems 的说明与下一步都来自 model 的代码表。Unwrap 返回
// Kind。
type ProblemsError struct {
	Kind     error
	Problems []model.Problem
}

func (e *ProblemsError) Error() string { return describe(e.Kind, e.Problems) }

func (e *ProblemsError) Unwrap() error { return e.Kind }

// BatchError 是建任务时在事务里重跑准入判定没通过（C30），整批一个都没建。Result 与 admission.Batch 的返回值
// 同形：Common 是 ISO 与模板的问题，ByHost 逐台列出主机的问题，没登记的主机报 host_not_registered；第 5 层
// 可以原样逐台返回。Unwrap 返回 ErrRejected。
type BatchError struct {
	Result admission.BatchResult
}

// Error 先写 Common，再按 SN 的顺序写各台的问题：ByHost 是 map，排过序，同一个结果每次写出来才一样。
func (e *BatchError) Error() string {
	ps := slices.Clone(e.Result.Common)
	for _, sn := range slices.Sorted(maps.Keys(e.Result.ByHost)) {
		ps = append(ps, e.Result.ByHost[sn]...)
	}
	return describe(ErrRejected, ps)
}

func (e *BatchError) Unwrap() error { return ErrRejected }

// describe 是带问题的错误的文字：类别的说法，后面依次接上每条问题的 Error。kind 为 nil 是构造时漏填了，
// 照样写得出来：拼错误文字的地方不能再出错。
func describe(kind error, ps []model.Problem) string {
	var b strings.Builder
	if kind != nil {
		b.WriteString(kind.Error())
	} else {
		b.WriteString("store：有问题")
	}
	for i, p := range ps {
		if i == 0 {
			b.WriteString("：")
		} else {
			b.WriteString("；")
		}
		b.WriteString(p.Error())
	}
	return b.String()
}
