package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	domaininv "github.com/lober/lober/internal/domain/inventory"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// fakeHosts 是内存版的主机仓储，只实现采集应用用到的那两个方法。
type fakeHosts struct {
	bySN     map[string]model.Host
	updates  int
	audits   []repository.Audit
	failNext error
}

func newFakeHosts(hosts ...model.Host) *fakeHosts {
	f := &fakeHosts{bySN: map[string]model.Host{}}
	for _, h := range hosts {
		f.bySN[h.SN] = h
	}
	return f
}

func (f *fakeHosts) GetHost(_ context.Context, sn string) (model.Host, error) {
	h, ok := f.bySN[sn]
	if !ok {
		return model.Host{}, repository.ErrNotFound
	}
	return h, nil
}

func (f *fakeHosts) UpdateHost(_ context.Context, h model.Host, a repository.Audit) (model.Host, error) {
	if f.failNext != nil {
		err := f.failNext
		f.failNext = nil
		return model.Host{}, err
	}
	f.updates++
	f.audits = append(f.audits, a)
	h.Rev = f.bySN[h.SN].Rev + 1
	f.bySN[h.SN] = h
	return h, nil
}

// openAlways 是固定答复的打开任务口。
type openAlways struct{ open bool }

func (o openAlways) FindOpenTask(context.Context, string) (model.Task, bool, error) {
	return model.Task{}, o.open, nil
}

// openVar 是可变的打开任务口，测试中途翻转。
type openVar struct{ open *bool }

func (o openVar) FindOpenTask(context.Context, string) (model.Task, bool, error) {
	return model.Task{}, *o.open, nil
}

type fakeTokens struct{ n int }

func (f *fakeTokens) NewToken() (string, error) {
	f.n++
	return strings.Repeat(string(rune('a'+f.n%6)), 64), nil
}

func seededService(host model.Host) (*Service, *fakeHosts, *int64) {
	hosts := newFakeHosts(host)
	now := int64(1000)
	s := New(Deps{Hosts: hosts, Tokens: &fakeTokens{}, Clock: func() int64 { return now }})
	return s, hosts, &now
}

func adminActor() ports.Actor {
	return ports.Actor{Username: "admin", Role: model.RoleAdmin}
}

func sampleHost() model.Host {
	return model.Host{SN: "SN-1", Rev: 1, Attrs: map[string]string{"biz_ip": "10.0.0.5/24"}}
}

const body = `{"firmware":"bios","cpu":{"model":"Xeon","cores":4},"memory_mib":8192,
"system":{"manufacturer":"QEMU","product":"PC"},
"disks":[{"name":"vda","bytes":8589934592},{"name":"vdb","bytes":9663676416}]}`

func Test请求采集写标记(t *testing.T) {
	s, hosts, _ := seededService(sampleHost())
	got, err := s.Request(context.Background(), adminActor(), "SN-1")
	if err != nil {
		t.Fatal(err)
	}
	if got.Attrs[AttrRequested] != "1000" {
		t.Fatalf("请求标记 = %q", got.Attrs[AttrRequested])
	}
	if hosts.updates != 1 {
		t.Fatalf("写库 %d 次，想要 1 次", hosts.updates)
	}
	if hosts.audits[0].Action != "host.inventory_request" {
		t.Fatal(hosts.audits[0])
	}
}

func Test请求采集需要写权限(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	viewer := ports.Actor{Username: "v", Role: model.RoleViewer}
	if _, err := s.Request(context.Background(), viewer, "SN-1"); !errors.Is(err, ports.ErrForbidden) {
		t.Fatalf("想要 ErrForbidden，得到 %v", err)
	}
}

func Test请求采集要求主机已登记(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-NOPE"); !errors.Is(err, ErrUnknownHost) {
		t.Fatalf("想要 ErrUnknownHost，得到 %v", err)
	}
}

func Test有打开的任务时不许请求采集(t *testing.T) {
	hosts := newFakeHosts(sampleHost())
	s := New(Deps{Hosts: hosts, Tokens: &fakeTokens{}, Clock: func() int64 { return 1000 },
		Tasks: openAlways{true}})
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("想要 ErrHostBusy，得到 %v", err)
	}
}

func Test第一次请求采集后进入待采集(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if pending, _ := s.Pending(context.Background(), "SN-1"); pending {
		t.Fatal("没请求就待采集")
	}
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	pending, err := s.Pending(context.Background(), "SN-1")
	if err != nil || !pending {
		t.Fatalf("请求后应待采集：%v %v", pending, err)
	}
	// 第二次问仍是待采集，且不该再写库。
	hosts := s.hosts.(*fakeHosts)
	before := hosts.updates
	if pending, _ := s.Pending(context.Background(), "SN-1"); !pending {
		t.Fatal("进行中应继续待采集")
	}
	if hosts.updates != before {
		t.Fatal("进行中不该重复写库")
	}
}

func Test采过之后没有新请求就不再采集(t *testing.T) {
	s, hosts, _ := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Issue("SN-1")
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if pending, _ := s.Pending(context.Background(), "SN-1"); pending {
		t.Fatal("采过且无新请求，仍在待采集")
	}
	// 再点一次更新配置，又要采。
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := s.Pending(context.Background(), "SN-1"); !pending {
		t.Fatal("新请求后应待采集")
	}
	_ = hosts
}

func Test进行中标记超时后作废(t *testing.T) {
	s, hosts, now := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := s.Pending(context.Background(), "SN-1"); !pending {
		t.Fatal("应待采集")
	}
	// 把时间推过一小时。
	*now += TokenLife + 1
	pending, err := s.Pending(context.Background(), "SN-1")
	if err != nil || pending {
		t.Fatalf("超时后不该再给采集菜单：%v %v", pending, err)
	}
	if _, ok := hosts.bySN["SN-1"].Attrs[AttrInFlight]; ok {
		t.Fatal("超时后应清掉进行中标记")
	}
	// 从没采过的主机，作废时连请求标记一起清掉。
	if _, ok := hosts.bySN["SN-1"].Attrs[AttrRequested]; ok {
		t.Fatal("从没采过的主机，作废后应清掉请求标记")
	}
}

func Test采集上报写入属性并保留人工属性(t *testing.T) {
	s, hosts, _ := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, ok := s.Issue("SN-1")
	if !ok {
		t.Fatal("没发到令牌")
	}
	got, err := s.Submit(context.Background(), "SN-1", token, []byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if got.Attrs[domaininv.InvSystemDisk] != "vda" {
		t.Fatalf("系统盘 = %q", got.Attrs[domaininv.InvSystemDisk])
	}
	if got.Attrs["biz_ip"] != "10.0.0.5/24" {
		t.Fatal("人工填的业务地址被采集覆盖了")
	}
	if _, ok := got.Attrs[AttrCollectedAt]; !ok {
		t.Fatal("没写采集时间")
	}
	if _, ok := got.Attrs[AttrRequested]; ok {
		t.Fatal("采完后应清掉请求标记")
	}
	if _, ok := got.Attrs[AttrInFlight]; ok {
		t.Fatal("采完后应清掉进行中标记")
	}
	if last := hosts.audits[len(hosts.audits)-1]; last.Action != "host.inventory" || !strings.Contains(last.Detail, "vda") {
		t.Fatal(last)
	}
	// 令牌用过就失效。
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(body)); !errors.Is(err, ErrNoToken) {
		t.Fatalf("令牌应已失效，得到 %v", err)
	}
}

func Test令牌不对就拒收(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ name, sn, token string }{
		{"乱写令牌", "SN-1", strings.Repeat("z", 64)},
		{"没发令牌", "SN-1", ""},
		{"别人的 SN", "SN-2", strings.Repeat("z", 64)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := s.Submit(context.Background(), tc.sn, tc.token, []byte(body)); !errors.Is(err, ErrNoToken) {
				t.Fatalf("想要 ErrNoToken，得到 %v", err)
			}
		})
	}
}

func Test令牌超时拒收(t *testing.T) {
	s, _, now := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Issue("SN-1")
	*now += TokenLife
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(body)); !errors.Is(err, ErrTokenExpired) {
		t.Fatalf("想要 ErrTokenExpired，得到 %v", err)
	}
}

func Test上报里有网络字段拒收(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Issue("SN-1")
	bad := strings.Replace(body, `"firmware"`, `"ip":"10.0.0.1","firmware"`, 1)
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(bad)); !errors.Is(err, domaininv.ErrForbiddenKey) {
		t.Fatalf("想要 ErrForbiddenKey，得到 %v", err)
	}
}

func Test值不合法带上问题清单(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Issue("SN-1")
	noDisk := `{"firmware":"bios","cpu":{"model":"X","cores":4},"memory_mib":1024,"disks":[]}`
	_, err := s.Submit(context.Background(), "SN-1", token, []byte(noDisk))
	var rejected *RejectedError
	if !errors.As(err, &rejected) {
		t.Fatalf("想要 RejectedError，得到 %v", err)
	}
	if len(rejected.Problems) != 1 || rejected.Problems[0].Code != "inventory_no_disk" {
		t.Fatal(rejected.Problems)
	}
}

func Test有打开的任务时拒收上报(t *testing.T) {
	hosts := newFakeHosts(sampleHost())
	open := false
	s := New(Deps{Hosts: hosts, Tokens: &fakeTokens{}, Clock: func() int64 { return 1000 },
		Tasks: openVar{&open}})
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, _ := s.Issue("SN-1")
	open = true
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(body)); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("想要 ErrHostBusy，得到 %v", err)
	}
}

func Test发令牌可以重复取(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	first, ok := s.Issue("SN-1")
	if !ok {
		t.Fatal("没发到")
	}
	second, ok := s.Issue("SN-1")
	if !ok || second != first {
		t.Fatal("同一轮里应复用同一个令牌，否则机器重启一次就换一次")
	}
}

var _ = access.PermHostWrite
