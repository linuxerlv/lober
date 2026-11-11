package authoring

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// 只实现内置写入；初始化若索取审计、通知或其他仓储能力就无法接入此夹具。
type builtinStore struct {
	calls    []string
	contexts []context.Context
	hook     func(model.Hook) error
	template func(model.Template) error
}

func (s *builtinStore) EnsureBuiltinHook(ctx context.Context, h model.Hook) error {
	s.calls = append(s.calls, "hook:"+h.ID)
	s.contexts = append(s.contexts, ctx)
	if s.hook != nil {
		return s.hook(h)
	}
	return nil
}
func (s *builtinStore) EnsureBuiltinTemplate(ctx context.Context, v model.Template) error {
	s.calls = append(s.calls, "template:"+v.ID)
	s.contexts = append(s.contexts, ctx)
	if s.template != nil {
		return s.template(v)
	}
	return nil
}

func Test内置初始化保留顺序四次冲突与取消(t *testing.T) {
	t.Run("按输入顺序先全部组件再全部模板", func(t *testing.T) {
		s := &builtinStore{}
		b := NewBootstrapper(s, []model.Hook{{ID: "h2"}, {ID: "h1"}}, []model.Template{{ID: "t2"}, {ID: "t1"}})
		if len(s.calls) != 0 {
			t.Fatal("构造时提前写入", s.calls)
		}
		ctx := t.Context()
		if err := b.Initialise(ctx); err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(s.calls, []string{"hook:h2", "hook:h1", "template:t2", "template:t1"}) {
			t.Fatal(s.calls)
		}
		for _, got := range s.contexts {
			if got != ctx {
				t.Fatal("上下文被替换")
			}
		}
	})
	for _, kind := range []string{"hook", "template"} {
		t.Run(kind, func(t *testing.T) {
			for _, tc := range []struct {
				name      string
				failures  int
				failure   error
				wantCalls int
			}{
				{"三次冲突后成功", 3, fmt.Errorf("竞争: %w", repository.ErrConflict), 4},
				{"第四次原错误", 4, fmt.Errorf("最后竞争: %w", repository.ErrConflict), 4},
				{"非冲突不重试", 1, errors.New("写入失败"), 1},
			} {
				t.Run(tc.name, func(t *testing.T) {
					calls := 0
					s := &builtinStore{}
					operation := func() error {
						calls++
						if calls <= tc.failures {
							return tc.failure
						}
						return nil
					}
					s.hook = func(model.Hook) error { return operation() }
					s.template = func(model.Template) error { return operation() }
					var hs []model.Hook
					var ts []model.Template
					if kind == "hook" {
						hs = []model.Hook{{ID: "h"}}
					} else {
						ts = []model.Template{{ID: "t"}}
					}
					err := NewBootstrapper(s, hs, ts).Initialise(t.Context())
					if calls != tc.wantCalls {
						t.Fatal(calls, err)
					}
					if tc.failures == 4 || !errors.Is(tc.failure, repository.ErrConflict) {
						if err != tc.failure {
							t.Fatal("错误没有原样返回", err)
						}
					} else if err != nil {
						t.Fatal(err)
					}
					if tc.failures == 4 && (calls != 4 || !errors.Is(err, repository.ErrConflict)) {
						t.Fatal(calls, err)
					}
				})
			}
		})
	}
	t.Run("已取消不调用", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		s := &builtinStore{}
		err := NewBootstrapper(s, []model.Hook{{ID: "h"}}, []model.Template{{ID: "t"}}).Initialise(ctx)
		if err != context.Canceled || len(s.calls) != 0 {
			t.Fatal(err, s.calls)
		}
	})
	t.Run("冲突后取消不再尝试", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s := &builtinStore{hook: func(model.Hook) error { cancel(); return repository.ErrConflict }}
		err := NewBootstrapper(s, []model.Hook{{ID: "h"}}, nil).Initialise(ctx)
		if err != context.Canceled || len(s.calls) != 1 {
			t.Fatal(err, s.calls)
		}
	})
	t.Run("组件失败不启动模板", func(t *testing.T) {
		failure := errors.New("组件失败")
		s := &builtinStore{hook: func(model.Hook) error { return failure }}
		err := NewBootstrapper(s, []model.Hook{{ID: "h"}}, []model.Template{{ID: "t"}}).Initialise(t.Context())
		if err != failure || !reflect.DeepEqual(s.calls, []string{"hook:h"}) {
			t.Fatal(err, s.calls)
		}
	})
	t.Run("成功写入后不追加取消错误", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s := &builtinStore{template: func(model.Template) error { cancel(); return nil }}
		err := NewBootstrapper(s, nil, []model.Template{{ID: "t"}}).Initialise(ctx)
		if err != nil || len(s.calls) != 1 {
			t.Fatal(err, s.calls)
		}
	})
	t.Run("下一对象调用前检查取消", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		s := &builtinStore{hook: func(model.Hook) error { cancel(); return nil }}
		err := NewBootstrapper(s, []model.Hook{{ID: "h1"}, {ID: "h2"}}, nil).Initialise(ctx)
		if err != context.Canceled || !reflect.DeepEqual(s.calls, []string{"hook:h1"}) {
			t.Fatal(err, s.calls)
		}
	})
	t.Run("空清单无写入", func(t *testing.T) {
		s := &builtinStore{}
		if err := NewBootstrapper(s, nil, nil).Initialise(t.Context()); err != nil || len(s.calls) != 0 {
			t.Fatal(err, s.calls)
		}
	})
}

func Test内置初始化保留早先成功且可重复调用(t *testing.T) {
	saved := map[string]bool{}
	fail := true
	failure := errors.New("模板失败")
	s := &builtinStore{hook: func(h model.Hook) error { saved[h.ID] = true; return nil }, template: func(v model.Template) error {
		if fail {
			return failure
		}
		saved[v.ID] = true
		return nil
	}}
	b := NewBootstrapper(s, []model.Hook{{ID: "h"}}, []model.Template{{ID: "t"}})
	if err := b.Initialise(t.Context()); err != failure || !saved["h"] || saved["t"] {
		t.Fatal(err, saved)
	}
	fail = false
	for range 2 {
		if err := b.Initialise(t.Context()); err != nil {
			t.Fatal(err)
		}
	}
	if len(saved) != 2 || !reflect.DeepEqual(s.calls, []string{"hook:h", "template:t", "hook:h", "template:t", "hook:h", "template:t"}) {
		t.Fatal(saved, s.calls)
	}
}
