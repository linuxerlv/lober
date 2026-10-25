package ports

import (
	"errors"
	"slices"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// FailureKind 是失败的稳定类别，网页状态码由调用入口决定。
type FailureKind string

const (
	TemplateNotCurrent FailureKind = "template_not_current"
	LastAdmin          FailureKind = "last_admin"
	ReferencedDetail   FailureKind = "referenced_detail"
	BatchRejected      FailureKind = "batch_rejected"
	StoredProblems     FailureKind = "stored_problems"
	DomainProblems     FailureKind = "domain_problems"
	NotFound           FailureKind = "not_found"
	Conflict           FailureKind = "conflict"
	Duplicate          FailureKind = "duplicate"
	Immutable          FailureKind = "immutable"
	Referenced         FailureKind = "referenced"
	Invalid            FailureKind = "invalid"
	ProblemConflict    FailureKind = "problem_conflict"
	ProblemRejected    FailureKind = "problem_rejected"
)

// Failure 只带普通数据，不暴露保存数据或网页响应的类型。
type Failure struct {
	Kind       FailureKind
	Problems   []model.Problem
	References []model.Subject
	Common     []model.Problem
	ByHost     map[string][]model.Problem
}

// DescribeFailure 供应用和接口入口读取包装错误，返回独立副本；未知错误返回零值和 false。
// 顺序固定为：当前模板、最后管理员、引用详情、批量问题、仓储问题、领域问题值、
// 领域问题指针，最后才是普通仓储错误；合并错误也遵守这个顺序，不根据错误文字分类。
// 不改输入，不读外部事实，不写数据；重复和并发读取的规则相同，不涉及提交、审计、通知或超时。
func DescribeFailure(err error) (Failure, bool) {
	if errors.Is(err, repository.ErrTemplateNotCurrent) {
		return Failure{Kind: TemplateNotCurrent}, true
	}
	if errors.Is(err, repository.ErrLastAdmin) {
		return Failure{Kind: LastAdmin}, true
	}
	var referenced *repository.ReferencedError
	if errors.As(err, &referenced) {
		return Failure{Kind: ReferencedDetail, References: slices.Clone(referenced.References)}, true
	}
	var batch *repository.BatchError
	if errors.As(err, &batch) {
		var byHost map[string][]model.Problem
		if batch.Result.ByHost != nil {
			byHost = make(map[string][]model.Problem, len(batch.Result.ByHost))
			for sn, ps := range batch.Result.ByHost {
				byHost[sn] = slices.Clone(ps)
			}
		}
		return Failure{Kind: BatchRejected, Common: slices.Clone(batch.Result.Common), ByHost: byHost}, true
	}
	var stored *repository.ProblemsError
	if errors.As(err, &stored) {
		return Failure{Kind: StoredProblems, Problems: slices.Clone(stored.Problems)}, true
	}
	var problem model.Problem
	if errors.As(err, &problem) {
		return Failure{Kind: DomainProblems, Problems: []model.Problem{problem}}, true
	}
	var pointer *model.Problem
	if errors.As(err, &pointer) && pointer != nil {
		return Failure{Kind: DomainProblems, Problems: []model.Problem{*pointer}}, true
	}
	switch {
	case errors.Is(err, repository.ErrNotFound):
		return Failure{Kind: NotFound}, true
	case errors.Is(err, repository.ErrConflict):
		return Failure{Kind: Conflict}, true
	case errors.Is(err, repository.ErrDuplicate):
		return Failure{Kind: Duplicate}, true
	case errors.Is(err, repository.ErrImmutable):
		return Failure{Kind: Immutable}, true
	case errors.Is(err, repository.ErrReferenced):
		return Failure{Kind: Referenced}, true
	case errors.Is(err, repository.ErrInvalid):
		return Failure{Kind: Invalid}, true
	}
	return Failure{}, false
}

// ProblemClass 只按原第一条问题分类，不重排问题；空列表和未知代码都归为拒绝。
// 输入由应用提供，不作修改；结果与调用次数无关，不涉及外部读取、写入、审计、通知或超时。
func ProblemClass(ps []model.Problem) FailureKind {
	if len(ps) > 0 {
		switch ps[0].Code.Category() {
		case model.CategoryIncompatible, model.CategoryState:
			return ProblemConflict
		}
	}
	return ProblemRejected
}
