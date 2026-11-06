package inventory

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	domaininv "github.com/lober/lober/internal/domain/inventory"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// fakeHosts 模拟采集仓储在持锁时读取和写入同一份主机记录。
type fakeHosts struct {
	bySN       map[string]model.Host
	updates    int
	audits     []repository.Audit
	beforeEdit func()
	busy       bool
}

func newFakeHosts(hosts ...model.Host) *fakeHosts {
	f := &fakeHosts{bySN: map[string]model.Host{}}
	for _, h := range hosts {
		f.bySN[h.SN] = h
	}
	return f
}

func (f *fakeHosts) UpdateInventory(_ context.Context, sn string, decide func(model.Host, bool) (repository.InventoryChange, error)) (model.Host, error) {
	if before := f.beforeEdit; before != nil {
		f.beforeEdit = nil
		before()
	}
	h, ok := f.bySN[sn]
	if !ok {
		return model.Host{}, repository.ErrNotFound
	}
	change, err := decide(h, f.busy)
	if err != nil {
		return model.Host{}, err
	}
	if change.Attrs == nil {
		return h, nil
	}
	f.updates++
	f.audits = append(f.audits, change.Audit)
	h.Attrs = change.Attrs
	h.Rev++
	f.bySN[sn] = h
	return h, nil
}

func TestOldInventoryTimeoutKeepsNewRequest(t *testing.T) {
	h := sampleHost()
	h.Attrs[AttrRequested], h.Attrs[AttrInFlight], h.Attrs[AttrCollectedAt] = "10", "10", "5"
	s, hosts, now := seededService(h)
	*now = 5000
	hosts.beforeEdit = func() {
		if _, err := s.Request(t.Context(), adminActor(), h.SN); err != nil {
			t.Fatal(err)
		}
	}
	token, err := s.ClaimToken(t.Context(), h.SN)
	if err != nil || token == "" {
		t.Fatalf("新请求应领取到令牌：%v", err)
	}
	if got := hosts.bySN[h.SN].Attrs[AttrRequested]; got != "5000" {
		t.Fatalf("旧批次超时覆盖新请求，当前请求时间=%q", got)
	}
}

func TestInventoryReportCannotOverwriteNewBatch(t *testing.T) {
	h := sampleHost()
	s, hosts, _ := seededService(h)
	first, err := s.Request(t.Context(), adminActor(), h.SN)
	if err != nil {
		t.Fatal(err)
	}
	oldToken, err := s.ClaimToken(t.Context(), h.SN)
	if err != nil || oldToken == "" {
		t.Fatal("旧批次没有令牌", err)
	}
	var next model.Host
	hosts.beforeEdit = func() {
		var err error
		next, err = s.Request(t.Context(), adminActor(), h.SN)
		if err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Submit(t.Context(), h.SN, oldToken, []byte(body)); !errors.Is(err, ErrNoToken) {
		t.Fatalf("已换批的上报应被拒绝：%v", err)
	}
	if first.Attrs[AttrRequested] != next.Attrs[AttrRequested] || first.Attrs[AttrRequestID] == next.Attrs[AttrRequestID] ||
		hosts.bySN[h.SN].Attrs[AttrRequestID] != next.Attrs[AttrRequestID] {
		t.Fatal("同一秒发起的新批次应独立存在")
	}
	newToken, err := s.ClaimToken(t.Context(), h.SN)
	if err != nil || newToken == "" || newToken == oldToken {
		t.Fatal("新批次未领取到独立令牌", err)
	}
	hosts.beforeEdit = func() { hosts.bySN[h.SN].Attrs["rack"] = "changed" }
	got, err := s.Submit(t.Context(), h.SN, newToken, []byte(body))
	if err != nil || got.Attrs["rack"] != "changed" {
		t.Fatal("采集上报丢失并发主机编辑", err)
	}
}

func pendingForTest(s *Service, ctx context.Context, sn string) (bool, error) {
	token, err := s.ClaimToken(ctx, sn)
	return token != "", err
}

func issueForTest(s *Service, sn string) (string, bool) {
	token, err := s.ClaimToken(context.Background(), sn)
	return token, err == nil && token != ""
}

type fakeTokens struct{ n int }

var configuredMenu = MenuEndpoints{Gateway: "http://gateway.invalid", Artifacts: "http://artifacts.invalid", Kernel: "kernel", Initrd: "initrd"}

func (f *fakeTokens) NewToken() (string, error) {
	f.n++
	return strings.Repeat(string(rune('a'+f.n%6)), 64), nil
}

func seededService(host model.Host) (*Service, *fakeHosts, *int64) {
	hosts := newFakeHosts(host)
	now := int64(1000)
	s := New(Deps{Hosts: hosts, Tokens: &fakeTokens{}, Clock: func() int64 { return now }, Menu: configuredMenu})
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
	hosts.busy = true
	s := New(Deps{Hosts: hosts, Tokens: &fakeTokens{}, Clock: func() int64 { return 1000 }, Menu: configuredMenu})
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("想要 ErrHostBusy，得到 %v", err)
	}
}

func Test第一次请求采集后进入待采集(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if pending, _ := pendingForTest(s, context.Background(), "SN-1"); pending {
		t.Fatal("没请求就待采集")
	}
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	pending, err := pendingForTest(s, context.Background(), "SN-1")
	if err != nil || !pending {
		t.Fatalf("请求后应待采集：%v %v", pending, err)
	}
	// 第二次问仍是待采集，且不该再写库。
	hosts := s.hosts.(*fakeHosts)
	before := hosts.updates
	if pending, _ := pendingForTest(s, context.Background(), "SN-1"); !pending {
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
	token, _ := issueForTest(s, "SN-1")
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(body)); err != nil {
		t.Fatal(err)
	}
	if pending, _ := pendingForTest(s, context.Background(), "SN-1"); pending {
		t.Fatal("采过且无新请求，仍在待采集")
	}
	// 再点一次更新配置，又要采。
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := pendingForTest(s, context.Background(), "SN-1"); !pending {
		t.Fatal("新请求后应待采集")
	}
	_ = hosts
}

func Test进行中标记超时后作废(t *testing.T) {
	s, hosts, now := seededService(sampleHost())
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	if pending, _ := pendingForTest(s, context.Background(), "SN-1"); !pending {
		t.Fatal("应待采集")
	}
	// 把时间推过一小时。
	*now += TokenLife + 1
	pending, err := pendingForTest(s, context.Background(), "SN-1")
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
	token, ok := issueForTest(s, "SN-1")
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
	token, _ := issueForTest(s, "SN-1")
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
	token, _ := issueForTest(s, "SN-1")
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
	token, _ := issueForTest(s, "SN-1")
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
	s := New(Deps{Hosts: hosts, Tokens: &fakeTokens{}, Clock: func() int64 { return 1000 }, Menu: configuredMenu})
	if _, err := s.Request(context.Background(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	token, _ := issueForTest(s, "SN-1")
	hosts.busy = true
	if _, err := s.Submit(context.Background(), "SN-1", token, []byte(body)); !errors.Is(err, ErrHostBusy) {
		t.Fatalf("想要 ErrHostBusy，得到 %v", err)
	}
}

func Test发令牌可以重复取(t *testing.T) {
	s, _, _ := seededService(sampleHost())
	if _, err := s.Request(t.Context(), adminActor(), "SN-1"); err != nil {
		t.Fatal(err)
	}
	first, ok := issueForTest(s, "SN-1")
	if !ok {
		t.Fatal("没发到")
	}
	second, ok := issueForTest(s, "SN-1")
	if !ok || second != first {
		t.Fatal("同一轮里应复用同一个令牌，否则机器重启一次就换一次")
	}
}
