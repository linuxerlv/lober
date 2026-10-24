package main

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/domain/repository"
)

func Test空库必须显式初始口令已有用户不再要求(t *testing.T) {
	a, cfg := assembled(t)
	// A second empty schema tests actual config loading, rather than bypassing
	// the loader that previously substituted a known password.
	empty := assemblyConfig(t)
	t.Setenv("LOBER_PG_DSN", empty.PG.DSN)
	loaded, err := loadConfig(configFile(t, "listen: '127.0.0.1:8080'\n"))
	if err != nil {
		t.Fatal(err)
	}
	loaded.ISO = empty.ISO
	b, cleanup, err := newApp(t.Context(), loaded, a.Clock, a.Logger)
	if err != nil {
		t.Fatal("assembly failed")
	}
	defer cleanup()
	if err := b.Initialise(); err == nil {
		t.Fatal("missing bootstrap secret enabled a known account")
	}
	users, err := b.Store.ListUsers(t.Context(), repository.UserQuery{})
	if err != nil || len(users.Items) != 0 {
		t.Fatal("failed bootstrap persisted user", len(users.Items), err)
	}
	b.Config.SeedAdmin = cfg.SeedAdmin
	if err := b.Initialise(); err != nil {
		t.Fatal(err)
	}
	b.Config.SeedAdmin = ""
	if err := b.Initialise(); err != nil {
		t.Fatal("populated database still required bootstrap secret", err)
	}
}

type startupBuiltin struct {
	calls *[]string
	err   error
	ctx   context.Context
}

func (b *startupBuiltin) Initialise(ctx context.Context) error {
	b.ctx = ctx
	*b.calls = append(*b.calls, "builtin")
	return b.err
}

type startupIdentity struct {
	calls *[]string
	err   error
	ctx   context.Context
	seed  string
}

func (b *startupIdentity) Initialise(ctx context.Context, seed string) error {
	b.ctx = ctx
	b.seed = seed
	*b.calls = append(*b.calls, "identity")
	return b.err
}

// 旧组装循环仍能完成，避免以空指针崩溃冒充行为失败。
type startupLegacyStore struct{ repository.Store }

func (*startupLegacyStore) EnsureBuiltinHook(context.Context, model.Hook) error         { return nil }
func (*startupLegacyStore) EnsureBuiltinTemplate(context.Context, model.Template) error { return nil }
func Test启动先内置内容后身份初始化(t *testing.T) {
	failure := errors.New("内置内容失败")
	identityFailure := errors.New("身份失败")
	for _, tc := range []struct {
		name                    string
		builtinErr, identityErr error
		want                    []string
		wantErr                 error
	}{
		{"成功顺序", nil, nil, []string{"builtin", "identity"}, nil},
		{"内置失败不创建管理员", failure, nil, []string{"builtin"}, failure},
		{"身份错误原样返回", nil, identityFailure, []string{"builtin", "identity"}, identityFailure},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var calls []string
			b := &startupBuiltin{calls: &calls, err: tc.builtinErr}
			i := &startupIdentity{calls: &calls, err: tc.identityErr}
			ctx := t.Context()
			a := &app{Builtin: b, Identity: i, Context: ctx, Config: config{SeedAdmin: "supplied-secret"}, Store: &startupLegacyStore{}}
			err := a.Initialise()
			if err != tc.wantErr || !reflect.DeepEqual(calls, tc.want) {
				t.Fatal(err, calls)
			}
			if b.ctx != ctx {
				t.Fatal("内置初始化上下文被替换")
			}
			if tc.builtinErr == nil && (i.ctx != ctx || i.seed != "supplied-secret") {
				t.Fatal("身份初始化参数被替换")
			}
		})
	}
}
