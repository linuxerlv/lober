package ports_test

import (
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/admission"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

func Test错误说明保留包装优先级与普通值(t *testing.T) {
	p := model.Problem{Code: "sn_invalid", Message: "主机问题", Subject: model.Subject{Kind: "host", ID: "A", Path: "arch", Line: 2, Col: 3}, Next: "修改主机"}
	refs := []model.Subject{{Kind: "task", ID: "T"}}
	referenced := &repository.ReferencedError{References: refs}
	batch := &repository.BatchError{Result: admission.BatchResult{Common: []model.Problem{p}, ByHost: map[string][]model.Problem{"A": {p}}}}
	stored := &repository.ProblemsError{Kind: repository.ErrConflict, Problems: []model.Problem{{Code: "template_not_approved", Message: "状态问题"}}}
	for _, tc := range []struct {
		name string
		err  error
		want ports.Failure
	}{
		{"当前模板先于最后管理员和详情", errors.Join(repository.ErrLastAdmin, referenced, repository.ErrTemplateNotCurrent), ports.Failure{Kind: ports.TemplateNotCurrent}},
		{"最后管理员先于详情", errors.Join(referenced, repository.ErrLastAdmin), ports.Failure{Kind: ports.LastAdmin}},
		{"引用详情先于批量", errors.Join(batch, referenced), ports.Failure{Kind: ports.ReferencedDetail, References: refs}},
		{"批量先于仓储问题", errors.Join(stored, batch), ports.Failure{Kind: ports.BatchRejected, Common: []model.Problem{p}, ByHost: map[string][]model.Problem{"A": {p}}}},
		{"仓储问题先于单条问题", errors.Join(p, stored), ports.Failure{Kind: ports.StoredProblems, Problems: stored.Problems}},
		{"问题值先于指针", errors.Join(&model.Problem{Message: "指针"}, p), ports.Failure{Kind: ports.DomainProblems, Problems: []model.Problem{p}}},
		{"问题值先于普通仓储错误", errors.Join(repository.ErrConflict, p), ports.Failure{Kind: ports.DomainProblems, Problems: []model.Problem{p}}},
		{"问题指针先于普通仓储错误", errors.Join(repository.ErrNotFound, &p), ports.Failure{Kind: ports.DomainProblems, Problems: []model.Problem{p}}},
		{"找不到", repository.ErrNotFound, ports.Failure{Kind: ports.NotFound}},
		{"冲突", repository.ErrConflict, ports.Failure{Kind: ports.Conflict}},
		{"重复", &repository.DuplicateError{Field: "sn", Existing: "A"}, ports.Failure{Kind: ports.Duplicate}},
		{"不可改", repository.ErrImmutable, ports.Failure{Kind: ports.Immutable}},
		{"被引用", repository.ErrReferenced, ports.Failure{Kind: ports.Referenced}},
		{"无效", repository.ErrInvalid, ports.Failure{Kind: ports.Invalid}},
		{"空引用详情", &repository.ReferencedError{}, ports.Failure{Kind: ports.ReferencedDetail}},
		{"空批量详情", &repository.BatchError{}, ports.Failure{Kind: ports.BatchRejected}},
		{"空仓储详情", &repository.ProblemsError{Kind: repository.ErrInvalid}, ports.Failure{Kind: ports.StoredProblems}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, err := range []error{tc.err, fmt.Errorf("包装: %w", tc.err)} {
				got, ok := ports.DescribeFailure(err)
				if !ok || !reflect.DeepEqual(got, tc.want) {
					t.Fatalf("got=%+v ok=%v want=%+v", got, ok, tc.want)
				}
			}
		})
	}
	for _, err := range []error{nil, errors.New("private backend credential"), repository.ErrRejected, repository.ErrAuditRequired} {
		if got, ok := ports.DescribeFailure(err); ok || !reflect.DeepEqual(got, ports.Failure{}) {
			t.Fatalf("未知错误被解释: %+v %v", got, ok)
		}
	}
}

func Test错误说明复制全部可修改详情(t *testing.T) {
	p := model.Problem{Message: "原问题"}
	referenced := &repository.ReferencedError{References: []model.Subject{{ID: "原引用"}}}
	stored := &repository.ProblemsError{Problems: []model.Problem{p}}
	batch := &repository.BatchError{Result: admission.BatchResult{Common: []model.Problem{p}, ByHost: map[string][]model.Problem{"A": {p}, "empty": {}}}}
	for _, err := range []error{referenced, stored, batch} {
		got, ok := ports.DescribeFailure(err)
		if !ok {
			t.Fatalf("未识别 %T", err)
		}
		if len(got.References) > 0 {
			got.References[0].ID = "已改"
		}
		if len(got.Problems) > 0 {
			got.Problems[0].Message = "已改"
		}
		if len(got.Common) > 0 {
			got.Common[0].Message = "已改"
		}
		if got.ByHost != nil {
			got.ByHost["A"][0].Message = "已改"
			delete(got.ByHost, "empty")
			got.ByHost["new"] = nil
		}
	}
	if referenced.References[0].ID != "原引用" || stored.Problems[0].Message != "原问题" || batch.Result.Common[0].Message != "原问题" || batch.Result.ByHost["A"][0].Message != "原问题" || len(batch.Result.ByHost) != 2 {
		t.Fatal("修改说明结果污染了原错误")
	}
}

func Test问题类别只取原第一条(t *testing.T) {
	for _, tc := range []struct {
		name string
		ps   []model.Problem
		want ports.FailureKind
	}{
		{"空", nil, ports.ProblemRejected},
		{"未知", []model.Problem{{Code: "unknown"}}, ports.ProblemRejected},
		{"输入", []model.Problem{{Code: "sn_invalid"}}, ports.ProblemRejected},
		{"不完备", []model.Problem{{Code: "hook_no_impl"}}, ports.ProblemRejected},
		{"不兼容", []model.Problem{{Code: "series_not_ticked"}}, ports.ProblemConflict},
		{"状态", []model.Problem{{Code: "template_not_approved"}}, ports.ProblemConflict},
		{"不能扫描后面的冲突", []model.Problem{{Code: "unknown"}, {Code: "template_not_approved"}}, ports.ProblemRejected},
		{"不能扫描后面的输入", []model.Problem{{Code: "template_not_approved"}, {Code: "sn_invalid"}}, ports.ProblemConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := ports.ProblemClass(tc.ps); got != tc.want {
				t.Fatalf("got=%s want=%s", got, tc.want)
			}
		})
	}
}
