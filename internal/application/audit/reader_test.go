package audit

import (
	"context"
	"encoding/csv"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type auditReadFunc func(context.Context, repository.AuditQuery) (repository.Page[repository.AuditEntry], error)

func (f auditReadFunc) ListAudit(c context.Context, q repository.AuditQuery) (repository.Page[repository.AuditEntry], error) {
	return f(c, q)
}
func adminActor() ports.Actor { return ports.Actor{Username: "admin", Role: model.RoleAdmin} }

func Test审计应用拒绝后不读仓储(t *testing.T) {
	calls := 0
	reader := NewReader(auditReadFunc(func(context.Context, repository.AuditQuery) (repository.Page[repository.AuditEntry], error) {
		calls++
		return repository.Page[repository.AuditEntry]{}, nil
	}))
	cases := []struct {
		name  string
		actor ports.Actor
		q     Query
		want  error
	}{
		{"零身份", ports.Actor{}, Query{Filter: Filter{From: -1}, Limit: 501}, ports.ErrUnauthenticated},
		{"需改密", ports.Actor{Username: "admin", Role: model.RoleAdmin, MustChangePassword: true}, Query{Filter: Filter{From: -1}}, ports.ErrPasswordChangeRequired},
		{"先范围后分页", adminActor(), Query{Filter: Filter{From: 3, To: 2}, Limit: 501}, ErrBadRequest},
		{"负时间", adminActor(), Query{Filter: Filter{To: -1}}, ErrBadRequest},
		{"分页", adminActor(), Query{Limit: 501}, repository.ErrInvalid},
		{"游标混用", adminActor(), Query{Page: 1, Cursor: "old"}, repository.ErrInvalid},
	}
	for _, role := range model.Roles() {
		if role != model.RoleAdmin {
			cases = append(cases, struct {
				name  string
				actor ports.Actor
				q     Query
				want  error
			}{string(role), ports.Actor{Username: "reader", Role: role}, Query{Filter: Filter{From: -1}}, ports.ErrForbidden})
		}
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			before := calls
			defer func() {
				if calls != before {
					t.Error("拒绝请求读取了仓储")
				}
			}()
			if _, err := reader.List(t.Context(), tc.actor, tc.q); !errors.Is(err, tc.want) {
				t.Fatalf("得到 %v，期望 %v", err, tc.want)
			}
			if tc.q.Limit != 501 && tc.q.Cursor == "" {
				if b, err := reader.ExportCSV(t.Context(), tc.actor, tc.q.Filter); b != nil || !errors.Is(err, tc.want) {
					t.Fatalf("导出 %q %v", b, err)
				}
			}
		})
	}
}

func Test审计应用导出后页失败无部分结果(t *testing.T) {
	for _, outcome := range []string{"后页故障", "取消", "取消时仓储刚返回成功"} {
		t.Run(outcome, func(t *testing.T) {
			ctx, stop := context.WithCancel(t.Context())
			defer stop()
			calls := 0
			failure := errors.New("第二页失败")
			filter := Filter{TargetKind: "host", TargetID: "SN", Actor: "=actor", Action: "host.import", From: 10, To: 20}
			reader := NewReader(auditReadFunc(func(c context.Context, q repository.AuditQuery) (repository.Page[repository.AuditEntry], error) {
				calls++
				want := repository.AuditQuery{TargetKind: "host", TargetID: "SN", Actor: "=actor", Action: "host.import", From: 10, To: 20, Limit: 500}
				if calls > 1 {
					want.Cursor = "older"
				}
				if q != want {
					t.Fatalf("查询改变 %+v", q)
				}
				if calls == 1 {
					return repository.Page[repository.AuditEntry]{Items: []repository.AuditEntry{{ID: 2, Audit: repository.Audit{Actor: "=actor", Action: "host.import", Detail: "首行", At: 20}, TargetKind: "host", TargetID: "SN"}}, Next: "older"}, nil
				}
				if outcome != "后页故障" {
					stop()
					if outcome == "取消时仓储刚返回成功" {
						return repository.Page[repository.AuditEntry]{}, nil
					}
					return repository.Page[repository.AuditEntry]{}, c.Err()
				}
				return repository.Page[repository.AuditEntry]{}, failure
			}))
			b, err := reader.ExportCSV(ctx, adminActor(), filter)
			want := failure
			if outcome != "后页故障" {
				want = context.Canceled
			}
			if b != nil || !errors.Is(err, want) || calls != 2 {
				t.Fatalf("得到部分数据或错误不符: %q %v calls=%d", b, err, calls)
			}
		})
	}
}

func Test审计应用列表保留原文及分页(t *testing.T) {
	total := int64(1)
	reader := NewReader(auditReadFunc(func(c context.Context, q repository.AuditQuery) (repository.Page[repository.AuditEntry], error) {
		if q != (repository.AuditQuery{Page: 2, Limit: 3, Actor: "\x00", From: 10, To: 10}) {
			t.Fatalf("筛选被提前拒绝或改写 %+v", q)
		}
		return repository.Page[repository.AuditEntry]{Items: []repository.AuditEntry{{ID: 8, Audit: repository.Audit{Actor: "=原文", Action: "host.import", Detail: "\t原文", At: 10}, TargetKind: "host", TargetID: "SN"}}, Total: &total}, nil
	}))
	page, err := reader.List(t.Context(), adminActor(), Query{Filter: Filter{Actor: "\x00", From: 10, To: 10}, Page: 2, Limit: 3})
	if err != nil || !reflect.DeepEqual(page, Page{Items: []Entry{{ID: 8, At: 10, Actor: "=原文", Action: "host.import", Detail: "\t原文", TargetKind: "host", TargetID: "SN"}}, Total: &total}) {
		t.Fatalf("列表丢失原文或分页: %+v %v", page, err)
	}
}

func Test审计应用完整CSV与空结果(t *testing.T) {
	for _, empty := range []bool{false, true} {
		t.Run(map[bool]string{false: "全部字段与页", true: "空结果"}[empty], func(t *testing.T) {
			calls := 0
			reader := NewReader(auditReadFunc(func(context.Context, repository.AuditQuery) (repository.Page[repository.AuditEntry], error) {
				calls++
				if empty {
					return repository.Page[repository.AuditEntry]{}, nil
				}
				if calls == 1 {
					return repository.Page[repository.AuditEntry]{Items: []repository.AuditEntry{{ID: 2, Audit: repository.Audit{Actor: "\u200b＝甲", Action: "host.import", Detail: "\rplain", At: 20}, TargetKind: "host", TargetID: "SN-2"}}, Next: "older"}, nil
				}
				return repository.Page[repository.AuditEntry]{Items: []repository.AuditEntry{{ID: 1, Audit: repository.Audit{Actor: "admin", Action: "host.import", Detail: "尾行", At: 10}, TargetKind: "host", TargetID: "SN-1"}}}, nil
			}))
			b, err := reader.ExportCSV(t.Context(), adminActor(), Filter{})
			if err != nil {
				t.Fatal(err)
			}
			rows, err := csv.NewReader(strings.NewReader(string(b))).ReadAll()
			if err != nil {
				t.Fatal(err)
			}
			want := [][]string{{"id", "at", "actor", "action", "target_kind", "target_id", "detail"}}
			if !empty {
				want = append(want, []string{"2", "20", "\t\u200b＝甲", "host.import", "host", "SN-2", "\t\rplain"}, []string{"1", "10", "admin", "host.import", "host", "SN-1", "尾行"})
			}
			if !reflect.DeepEqual(rows, want) || strings.HasPrefix(string(b), "\ufeff") || !strings.HasSuffix(string(b), "\n") {
				t.Fatalf("CSV内容错误 %q", b)
			}
		})
	}
}
