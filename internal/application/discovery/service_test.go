package discovery

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	data "github.com/lober/lober/internal/domain/discovery"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type testTokens struct{}

func (testTokens) NewToken() (string, error) { return "fixture", nil }

type testRepo struct {
	repository.Discoveries
	host    bool
	attempt data.Attempt
	saved   bool
}

func (r *testRepo) GetHost(context.Context, string) (model.Host, error) {
	if r.host {
		return model.Host{}, nil
	}
	return model.Host{}, repository.ErrNotFound
}
func (r *testRepo) ObserveDiscovery(_ context.Context, a data.Attempt) (data.Attempt, error) {
	r.attempt = a
	return a, nil
}
func (r *testRepo) SaveDiscoveryReport(_ context.Context, mac, id string, _ int64, _ data.Report) error {
	r.saved = r.attempt.MAC == mac && r.attempt.ID == id
	return nil
}

func TestDiscoveryFlow(t *testing.T) {
	r := new(testRepo)
	s := New(r, func() int64 { return 100 }, []byte("key"), "https://lober.example", "https://assets.example", map[catalog.Arch]Image{"x86_64": {"alpine/kernel", "alpine/initramfs"}}, true, testTokens{})
	path := "52-54-00-12-34-56"
	body, err := s.Boot(t.Context(), "", path, "x86_64", "uefi")
	if err != nil || !strings.Contains(string(body), "https://assets.example/alpine/kernel") || !strings.Contains(string(body), "https://lober.example/v1/discovery/52:54:00:12:34:56") || r.attempt.SN != "" {
		t.Fatalf("Boot: %s %v %+v", body, err, r.attempt)
	}
	before, after, ok := strings.Cut(string(body), "lober.discovery_token=")
	if !ok || before == "" {
		t.Fatal("缺少令牌")
	}
	token := strings.Fields(after)[0]
	input := []byte(`{"sections":[{"key":"network","title":"网卡","status":"unread","reason":"未查到","items":[]}]}`)
	if err = s.Submit(t.Context(), "52-54-00-12-34-57", token, input); !errors.Is(err, data.ErrToken) {
		t.Fatalf("令牌应绑定网口: %v", err)
	}
	if err = s.Submit(t.Context(), path, token, input); err != nil || !r.saved {
		t.Fatalf("Submit: %v", err)
	}
	if err = s.Submit(t.Context(), path, token, []byte("{}")); !errors.Is(err, data.ErrReport) {
		t.Fatalf("报告应校验: %v", err)
	}
	body, err = s.Boot(t.Context(), "", path, "aarch64", "uefi")
	if err != nil || len(body) != 0 || r.attempt.Reason == "" {
		t.Fatalf("缺少镜像也要报到: %v, %+v", err, r.attempt)
	}
	r.host = true
	r.attempt = data.Attempt{}
	body, err = s.Boot(t.Context(), "KNOWN", path, "x86_64", "uefi")
	if err != nil || len(body) != 0 || r.attempt.MAC != "" {
		t.Fatalf("已登记主机不走自动发现: %v, %+v", err, r.attempt)
	}
	_, err = s.Register(t.Context(), ports.Actor{Username: "operator", Role: model.RoleOperator}, path, "NEW")
	if !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("只有管理员能确认: %v", err)
	}
}

type bindingRepo struct {
	testRepo
	observed, registered string
}

func (r bindingRepo) RegisteredDiscoverySN(context.Context, string) (string, string, error) {
	return r.observed, r.registered, nil
}

func TestBootIdentity(t *testing.T) {
	const mac = "52:54:00:12:34:56"
	for _, tc := range []struct {
		observed, registered, presented, want string
		conflict                              bool
	}{
		{"FACTORY-A", "HOST-A", "factory-a", "HOST-A", false},
		{"FACTORY-A", "HOST-A", "host-a", "HOST-A", false},
		{"FACTORY-A", "HOST-A", "HOST-B", "", true},
		{"FACTORY-A", "HOST-A", "", "", true},
		{"", "HOST-A", "None", "HOST-A", false},
		{"", "HOST-A", "HOST-B", "", true},
		{"", "", "HOST-B", "", false},
	} {
		s := New(&bindingRepo{observed: tc.observed, registered: tc.registered}, nil, nil, "", "", nil, true, nil)
		got, err := s.RegisteredSerial(t.Context(), mac, tc.presented)
		if got != tc.want || (err != nil) != tc.conflict || tc.conflict && !errors.Is(err, data.ErrIdentityConflict) {
			t.Errorf("登记 %q 报告 %q 得到 %q, %v", tc.observed, tc.presented, got, err)
		}
	}
}
