package media

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"math"
)

type registry struct{ deps Deps }

func NewRegistry(d Deps) Registry {
	if d.ISOs == nil || d.Sources == nil || d.Control == nil || d.Deletion == nil || d.Clock == nil {
		panic("镜像应用缺少必要能力")
	}
	return &registry{d}
}

// List 先检查分页，再检查筛选字段；重复读取不写入。
func (s *registry) List(ctx context.Context, a ports.Actor, r ListRequest) (ISOPage, error) {
	if err := a.Authorize(access.PermISORead); err != nil {
		return ISOPage{}, err
	}
	if _, err := repository.CheckPageQuery(r.Cursor, r.Limit, r.Page); err != nil {
		return ISOPage{}, err
	}
	if r.Family != "" && !r.Family.Valid() || r.Series != "" && !r.Series.Valid() || r.State != "" && !r.State.Valid() {
		return ISOPage{}, repository.ErrInvalid
	}
	q := repository.ISOQuery{Family: r.Family, Series: r.Series, Cursor: r.Cursor, Limit: r.Limit, Page: r.Page}
	if r.State != "" {
		q.States = []model.ISOState{r.State}
	}
	p, err := s.deps.ISOs.ListISOs(ctx, q)
	return ISOPage{p.Items, p.Next, p.Total}, err
}

// Get 编号检查先于存在性，权限拒绝时不读仓储。
func (s *registry) Get(ctx context.Context, a ports.Actor, id string) (model.ISO, error) {
	if err := a.Authorize(access.PermISORead); err != nil {
		return model.ISO{}, err
	}
	if err := repository.CheckID(id); err != nil {
		return model.ISO{}, err
	}
	return s.deps.ISOs.GetISO(ctx, id)
}
func checkISOSource(series catalog.Series, req *SourceSpec) error {
	if req.Kind != "url" && req.Kind != "path" && req.Kind != "upload" || req.Size < 0 {
		return repository.ErrInvalid
	}
	if req.Role != "" {
		extra, ok := catalog.ExtraSourceOf(series)
		if !ok || req.Role != extra.Role {
			return repository.ErrInvalid
		}
	}
	kind := model.SourceRemote
	source := model.Source{ISOID: "preflight", Role: req.Role, State: model.SourcePending, TotalSize: req.Size, ExpectedSHA256: req.SHA256}
	switch req.Kind {
	case "url":
		source.URL = req.Addr
	case "path":
		kind = model.SourceLocal
		source.Path = req.Addr
	case "upload":
		kind = model.SourceUpload
		req.Addr = ""
		if req.Size <= 0 {
			return repository.ErrInvalid
		}
	}
	source.Kind = kind
	return repository.CheckSource(source)
}

func (s *registry) checkSpace(ctx context.Context, sources []SourceSpec) error {
	var total int64
	for _, src := range sources {
		if src.Kind == "path" {
			continue
		}
		if src.Size > math.MaxInt64-total {
			return repository.ErrInvalid
		}
		total += src.Size
	}
	if total == 0 {
		return nil
	}
	free, _, err := s.deps.Control.FreeSpace(ctx)
	if err != nil {
		return err
	}
	if free < total {
		return &repository.ProblemsError{Problems: []model.Problem{model.NewProblem("iso_space_insufficient", model.Subject{Kind: model.KindISO, Path: "sources"}, free, total-free)}}
	}
	return nil
}

// Create 按字段、重复角色、来源、主来源、余量顺序检查。镜像和各来源分别提交，重复调用会新建镜像。
// 审计的 Detail 为空，目标由仓储绑定；每次写入单独读取时钟，登记失败不丢已提交编号。
func (s *registry) Create(ctx context.Context, a ports.Actor, r CreateRequest) (CreateResult, error) {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return CreateResult{}, err
	}
	item := model.ISO{Name: r.Name, Family: r.Family, Series: r.Series}
	if err := repository.CheckNewISO(item); err != nil {
		return CreateResult{}, err
	}
	sources := append([]SourceSpec(nil), r.Sources...)
	main := 0
	roles := map[string]bool{}
	for i := range sources {
		src := &sources[i]
		if roles[src.Role] {
			return CreateResult{}, repository.ErrInvalid
		}
		roles[src.Role] = true
		if src.Role == "" {
			main++
		}
		if err := checkISOSource(r.Series, src); err != nil {
			return CreateResult{}, err
		}
	}
	if main != 1 {
		return CreateResult{}, repository.ErrInvalid
	}
	if err := s.checkSpace(ctx, sources); err != nil {
		return CreateResult{}, err
	}
	created, err := s.deps.ISOs.CreateISO(ctx, item, s.audit(a, "iso.create"))
	if err != nil {
		return CreateResult{}, err
	}
	result := CreateResult{created, true}
	for _, src := range sources {
		if err := s.deps.Control.Register(ctx, created, src.Kind, src.Addr, src.Role, src.Size, src.SHA256, s.audit(a, "iso.create")); err != nil {
			return result, err
		}
	}
	return result, nil
}

// RegisterSource 先读镜像，再预检来源及余量，最后读取时钟并登记带空 Detail 的审计。
// 版本、重复来源与失败镜像排队由控制能力在事务里复查，失败不撤销之前的提交。
func (s *registry) RegisterSource(ctx context.Context, a ports.Actor, r RegisterRequest) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	if err := repository.CheckID(r.ID); err != nil {
		return err
	}
	item, err := s.deps.ISOs.GetISO(ctx, r.ID)
	if err != nil {
		return err
	}
	src := r.Source
	if err := checkISOSource(item.Series, &src); err != nil {
		return err
	}
	if err := s.checkSpace(ctx, []SourceSpec{src}); err != nil {
		return err
	}
	return s.deps.Control.Register(ctx, item, src.Kind, src.Addr, src.Role, src.Size, src.SHA256, s.audit(a, "iso.source.register"))
}
func (s *registry) audit(a ports.Actor, action string) repository.Audit {
	return repository.Audit{Actor: a.Username, Action: action, At: s.deps.Clock()}
}

// Cancel 权限通过后读取时钟；控制能力按审计、编号、镜像状态、逐来源顺序处理。
// 每来源最多重读十六轮，先提交取消再中断读取；已终止来源重试不写入，也不保证全来源一起成功。
func (s *registry) Cancel(ctx context.Context, a ports.Actor, id string) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	return s.deps.Control.Cancel(ctx, id, s.audit(a, "iso.cancel"))
}

// Retry 权限通过后读取时钟；控制能力先查审计、编号和镜像状态，再重读来源。
// 只逐条重置 failed/cancelled，fetched 不动；并发改变报冲突，无待重试来源成功且不记新审计。
func (s *registry) Retry(ctx context.Context, a ports.Actor, id string) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	return s.deps.Control.Retry(ctx, id, s.audit(a, "iso.retry"))
}

// Reimport 权限通过后读取时钟；控制能力先查审计、编号，再查 failed 和必需来源已完成。
// 排队与审计一起保存，提交后才唤醒后台；请求内不提取，旧版本重复排队仍冲突。
func (s *registry) Reimport(ctx context.Context, a ports.Actor, id string) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	return s.deps.Control.Reimport(ctx, id, s.audit(a, "iso.reimport"))
}

// Deprecate 正版本先于时钟、审计和编号检查；仓储锁内先查版本再查 ready 状态。
// 废弃与审计一起提交，重复旧版本报冲突。
func (s *registry) Deprecate(ctx context.Context, a ports.Actor, r RevisionRequest) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	if r.Rev < 1 {
		return repository.ErrInvalid
	}
	return s.deps.ISOs.DeprecateISO(ctx, r.ID, r.Rev, s.audit(a, "iso.deprecate"))
}

// Delete 正版本先于时钟和审计；删除能力先查镜像及版本，再持久化完整来源快照。
// 快照失败不删除，记录与审计一起提交；重复已删编号报缺失，提交后清理失败仍成功并由后台重试。
func (s *registry) Delete(ctx context.Context, a ports.Actor, r RevisionRequest) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	if r.Rev < 1 {
		return repository.ErrInvalid
	}
	return s.deps.Deletion.Delete(ctx, r.ID, r.Rev, s.audit(a, "iso.delete"))
}
