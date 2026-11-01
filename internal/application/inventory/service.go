// Package inventory 是采集上报的应用层：发放一次性令牌、校验上报、把硬件属性合并进主机登记。
//
// 采集与装机是两件事：采集时机器上还没有任务，所以走的是「登记过的主机 + 一次性令牌」，
// 不是任务令牌。令牌只在本进程内存里，一小时作废，进程重启就丢——这在设计上是接受的：
// 运维重新点一次「更新配置」即可，比留一个长效凭据安全。
package inventory

import (
	"context"
	"errors"
	"strconv"
	"sync"

	"github.com/lober/lober/internal/application/inventory/port"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	domaininv "github.com/lober/lober/internal/domain/inventory"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
	"github.com/lober/lober/internal/domain/repository"
)

// TokenLife 是一次采集令牌的有效期，一小时。采集镜像拿到令牌后最多几分钟就能跑完；
// 一小时是留给网络不稳与重启重试的余量，超时后要人工重新点「更新配置」。
const TokenLife int64 = 3600

var (
	// ErrUnknownHost 表示这个 SN 没在主机表里登记过。采集只对已登记的主机开放：
	// 没有登记就不知道该把属性写到哪台机器上。
	ErrUnknownHost = errors.New("host not registered")
	// ErrNoToken 表示这台主机没有待用的采集令牌，或者令牌不对。两者不区分，
	// 避免有人拿 SN 试探「这台机器是不是正好在等采集」。
	ErrNoToken = errors.New("inventory token unavailable")
	// ErrTokenExpired 与 ErrNoToken 分开，只用于日志：对调用方两者都是 401。
	ErrTokenExpired = errors.New("inventory token expired")
	// ErrHostBusy 表示这台主机有打开的任务，此刻不接受采集上报。任务快照已经按旧属性
	// 冻结好了，这时改属性会让快照与登记对不上。
	ErrHostBusy = errors.New("host has an open task")
)

// 采集状态属性的键。写在主机属性里，让「要不要给采集菜单」在进程重启后仍然可判断——
// 令牌本身在内存里，会丢；判断依据必须在库里。
const (
	// AttrCollectedAt 是上次采集成功的时间戳（Unix 秒）。有这个键就说明采过了。
	AttrCollectedAt = "inv_collected_at"
	// AttrRequested 是「重新采集」请求的时间戳。比 AttrCollectedAt 新才需要再采。
	AttrRequested = "inv_requested_at"
	// AttrInFlight 是采集进行中的标记时间戳。超过 TokenLife 自动视为作废，避免机器无限重启。
	AttrInFlight = "inv_in_flight_at"
)

// Deps 是本模块的构造依赖。
type Deps struct {
	Hosts  port.Hosts
	Tokens port.Tokens
	Clock  ports.Clock
	// Tasks 只用来判断这台主机有没有打开的任务。nil 时按「没有任务」处理，
	// 只用于不需要这道检查的测试。
	Tasks port.OpenTasks
	// Menu 是渲染采集菜单要的部署参数：网关基地址、制品基地址、内核与 initrd 的相对路径。
	Menu MenuEndpoints
}

// MenuEndpoints 是采集菜单要的部署参数。
type MenuEndpoints struct {
	Gateway, Artifacts string
	Kernel, Initrd     string
}

// Configured 报告四项部署参数是不是都配齐了。
func (m MenuEndpoints) Configured() bool {
	return m.Gateway != "" && m.Artifacts != "" && m.Kernel != "" && m.Initrd != ""
}

// Service 发放令牌、接收上报。
type Service struct {
	hosts    port.Hosts
	keys     port.Tokens
	now      ports.Clock
	openTask func(context.Context, string) (bool, error)
	menu     MenuEndpoints

	mu     sync.Mutex
	grants map[string]grant // 键是规范化后的 SN
}

// grant 是一台主机当前的采集授权。
type grant struct {
	token   string
	expires int64
}

// New 构造采集应用。缺依赖直接 panic：这是组装点的编程错误，不该在运行期兜底。
func New(d Deps) *Service {
	if d.Hosts == nil || d.Tokens == nil || d.Clock == nil {
		panic("采集应用缺少必要能力")
	}
	openTask := func(context.Context, string) (bool, error) { return false, nil }
	if d.Tasks != nil {
		openTask = func(ctx context.Context, sn string) (bool, error) {
			_, open, err := d.Tasks.FindOpenTask(ctx, sn)
			return open, err
		}
	}
	return &Service{hosts: d.Hosts, keys: d.Tokens, now: d.Clock, openTask: openTask, menu: d.Menu, grants: map[string]grant{}}
}

// Request 为一次采集写请求标记。调用方是控制台的「更新配置」按钮。
//
// 只写属性，不发令牌：令牌在机器来取采集菜单时才发（见 Issue），这样「点了更新配置」
// 和「机器还没重启」之间隔多久都不会丢。连点两次只是刷新时间戳。
func (s *Service) Request(ctx context.Context, a ports.Actor, rawSN string) (model.Host, error) {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return model.Host{}, err
	}
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return model.Host{}, err
	}
	host, err := s.hosts.GetHost(ctx, sn)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return model.Host{}, ErrUnknownHost
		}
		return model.Host{}, err
	}
	open, err := s.openTask(ctx, sn)
	if err != nil {
		return model.Host{}, err
	}
	if open {
		return model.Host{}, ErrHostBusy
	}
	at := s.now()
	host.Attrs = cloneAttrs(host.Attrs)
	// 请求时刻必须严格晚于上次采集时刻，否则同一秒内「采完再点更新配置」会被判成没有新请求。
	// 撞上同一秒时往后挪一秒：这是个先后标记，不是精确时间戳。
	requested := at
	if collected, ok := atoi(host.Attrs[AttrCollectedAt]); ok && requested <= collected {
		requested = collected + 1
	}
	host.Attrs[AttrRequested] = strconv.FormatInt(requested, 10)
	delete(host.Attrs, AttrInFlight)
	updated, err := s.hosts.UpdateHost(ctx, host, repository.Audit{Actor: a.Username, Action: "host.inventory_request", At: at})
	if err != nil {
		return model.Host{}, err
	}
	s.revoke(sn)
	return updated, nil
}

// Issue 供网关在给出采集菜单时取令牌：已有未过期的就复用，否则现发一个。
//
// 不写库：它只在「已经该采了」的前提下补发令牌，该不该采由 Pending 判断。
func (s *Service) Issue(sn string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	at := s.now()
	if g, ok := s.grants[sn]; ok && at < g.expires {
		return g.token, true
	}
	token, err := s.keys.NewToken()
	if err != nil {
		return "", false
	}
	s.grants[sn] = grant{token: token, expires: at + TokenLife}
	return token, true
}

// Pending 判断这台主机现在该不该进采集镜像。
//
// 条件全部满足才进：登记过、没有打开的任务、有采集请求且尚未完成、进行中标记没超时。
// 超时的处理是「作废这次请求」，不是「继续等」——网络长期不通时机器会一直重启，作废才能停下来。
func (s *Service) Pending(ctx context.Context, rawSN string) (bool, error) {
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return false, nil
	}
	host, err := s.hosts.GetHost(ctx, sn)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}
	requested, hasRequest := atoi(host.Attrs[AttrRequested])
	if !hasRequest {
		return false, nil
	}
	collected, hasCollected := atoi(host.Attrs[AttrCollectedAt])
	// 采过了，且请求不晚于上次采集：没有新请求，不进。
	if hasCollected && requested <= collected {
		return false, nil
	}
	open, err := s.openTask(ctx, sn)
	if err != nil || open {
		return false, err
	}
	inFlight, hasInFlight := atoi(host.Attrs[AttrInFlight])
	if !hasInFlight {
		// 还没开始：这是第一次进采集镜像，打标记。
		return s.claim(ctx, sn, requested)
	}
	if s.now()-inFlight < TokenLife {
		return true, nil
	}
	// 进行中标记超时：作废这次请求，别再让机器无限重启。
	if err := s.abandon(ctx, sn, collected); err != nil {
		return false, err
	}
	return false, nil
}

// claim 在第一次给采集菜单时打「进行中」标记。打标记失败就不给菜单：没有标记的话
// 一小时后就无法判断这次采集是不是卡住了。
func (s *Service) claim(ctx context.Context, sn string, requested int64) (bool, error) {
	host, err := s.hosts.GetHost(ctx, sn)
	if err != nil {
		return false, err
	}
	host.Attrs = cloneAttrs(host.Attrs)
	host.Attrs[AttrInFlight] = strconv.FormatInt(s.now(), 10)
	if _, err := s.hosts.UpdateHost(ctx, host, repository.Audit{Actor: "inventory", Action: "host.inventory_claim", At: s.now()}); err != nil {
		return false, err
	}
	return true, nil
}

// abandon 把超时的请求作废：请求时刻退回上次采集的时刻，没有采集记录就整体删掉。
func (s *Service) abandon(ctx context.Context, sn string, collected int64) error {
	host, err := s.hosts.GetHost(ctx, sn)
	if err != nil {
		return err
	}
	host.Attrs = cloneAttrs(host.Attrs)
	delete(host.Attrs, AttrInFlight)
	if collected > 0 {
		host.Attrs[AttrRequested] = strconv.FormatInt(collected, 10)
	} else {
		delete(host.Attrs, AttrRequested)
	}
	if _, err := s.hosts.UpdateHost(ctx, host, repository.Audit{Actor: "inventory", Action: "host.inventory_abandon", At: s.now()}); err != nil {
		return err
	}
	s.revoke(sn)
	return nil
}

// Submit 校验一份上报并写进主机登记。
//
// 顺序：查令牌 → 解析与校验 → 读主机 → 查有没有打开的任务 → 合并属性 → 写回。
// 令牌先查，不成立的请求不碰库。
func (s *Service) Submit(ctx context.Context, rawSN, token string, body []byte) (model.Host, error) {
	sn, err := model.NormalizeSN(rawSN)
	if err != nil {
		return model.Host{}, kinded(domaininv.KindUnauthorized, ErrNoToken)
	}
	at := s.now()
	s.mu.Lock()
	g, ok := s.grants[sn]
	if ok && at >= g.expires {
		delete(s.grants, sn)
		s.mu.Unlock()
		return model.Host{}, kinded(domaininv.KindUnauthorized, ErrTokenExpired)
	}
	s.mu.Unlock()
	if !ok || g.token != token {
		return model.Host{}, kinded(domaininv.KindUnauthorized, ErrNoToken)
	}

	result, problems, err := domaininv.Parse(body)
	if err != nil {
		return model.Host{}, err
	}
	if len(problems) != 0 {
		return model.Host{}, kinded(domaininv.KindRejected, &RejectedError{Problems: problems})
	}

	open, err := s.openTask(ctx, sn)
	if err != nil {
		return model.Host{}, err
	}
	if open {
		return model.Host{}, kinded(domaininv.KindBusy, ErrHostBusy)
	}
	host, err := s.hosts.GetHost(ctx, sn)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return model.Host{}, kinded(domaininv.KindUnknownHost, ErrUnknownHost)
		}
		return model.Host{}, err
	}
	host.Attrs = cloneAttrs(host.Attrs)
	for k, v := range result.Attrs {
		host.Attrs[k] = v
	}
	host.Attrs[AttrCollectedAt] = strconv.FormatInt(at, 10)
	delete(host.Attrs, AttrInFlight)
	delete(host.Attrs, AttrRequested)
	updated, err := s.hosts.UpdateHost(ctx, host, repository.Audit{
		Actor:  "inventory",
		Action: "host.inventory",
		Detail: "system_disk=" + result.SystemDisk,
		At:     at,
	})
	if err != nil {
		return model.Host{}, err
	}
	s.revoke(sn)
	return updated, nil
}

// kinded 给错误标上类别，执行模块按类别决定 HTTP 状态码。
func kinded(kind domaininv.ErrorKind, cause error) error {
	return &domaininv.Kinded{Kind: kind, Cause: cause}
}

// RejectedError 带一份上报的问题清单：格式对但值不可信。
type RejectedError struct{ Problems []model.Problem }

func (e *RejectedError) Error() string { return "inventory report rejected" }

func (s *Service) revoke(sn string) {
	s.mu.Lock()
	delete(s.grants, sn)
	s.mu.Unlock()
}

func cloneAttrs(m map[string]string) map[string]string {
	out := make(map[string]string, len(m)+len(domaininv.InvKeys())+3)
	for k, v := range m {
		out[k] = v
	}
	return out
}

func atoi(s string) (int64, bool) {
	if s == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// Menu 产出采集镜像的 iPXE 引导行。渲染在领域层，这里只把部署参数与令牌填进去。
//
// 四项部署参数缺任何一项都返回 ErrMenuUnconfigured：给了菜单却没有可用的地址，
// 机器会卡在没有串口输出的地方，不如照旧从本地盘启动。
func (s *Service) Menu(sn, token string) ([]byte, error) {
	if !s.menu.Configured() {
		return nil, ErrMenuUnconfigured
	}
	body, problems, err := render.RenderInventoryMenu(render.InventoryMenuRequest{
		Gateway:   s.menu.Gateway,
		Artifacts: s.menu.Artifacts,
		SN:        sn,
		Token:     token,
		Kernel:    s.menu.Kernel,
		Initrd:    s.menu.Initrd,
	})
	if err != nil {
		return nil, err
	}
	if len(problems) != 0 {
		return nil, ErrMenuUnconfigured
	}
	return body, nil
}

// ErrMenuUnconfigured 表示采集菜单的部署参数没配齐。
var ErrMenuUnconfigured = errors.New("inventory menu unconfigured")

// Configured 报告采集菜单的部署参数是不是配齐了。没配齐时组装点不接采集能力。
func (s *Service) Configured() bool { return s.menu.Configured() }
