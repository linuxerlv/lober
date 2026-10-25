package hosts

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type registry struct{ deps Deps }

func NewRegistry(d Deps) Registry {
	if d.Hosts == nil || d.Clock == nil {
		panic("主机应用缺少必要能力")
	}
	return &registry{deps: d}
}
func (s *registry) List(ctx context.Context, a ports.Actor, r ListRequest) (HostPage, error) {
	if err := a.Authorize(access.PermHostRead); err != nil {
		return HostPage{}, err
	}
	q := repository.HostQuery{SNPrefix: r.Prefix, AttrKey: r.AttrKey, AttrValue: r.AttrValue, Cursor: r.Cursor, Limit: r.Limit, Page: r.Page}
	if _, err := repository.CheckHostQuery(q); err != nil {
		return HostPage{}, err
	}
	p, err := s.deps.Hosts.ListHosts(ctx, q)
	if err != nil {
		return HostPage{}, err
	}
	return HostPage{Items: p.Items, Next: p.Next, Total: p.Total}, nil
}
func (s *registry) Get(ctx context.Context, a ports.Actor, raw string) (model.Host, error) {
	if err := a.Authorize(access.PermHostRead); err != nil {
		return model.Host{}, err
	}
	sn, err := model.NormalizeSN(raw)
	if err != nil {
		return model.Host{}, err
	}
	return s.deps.Hosts.GetHost(ctx, sn)
}
func (s *registry) Create(ctx context.Context, a ports.Actor, h model.Host) (model.Host, error) {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return model.Host{}, err
	}
	sn, err := model.NormalizeSN(h.SN)
	if err != nil {
		return model.Host{}, err
	}
	h.SN = sn
	return s.deps.Hosts.CreateHost(ctx, h, s.audit(a, "host.create"))
}
func (s *registry) Update(ctx context.Context, a ports.Actor, r UpdateRequest) (model.Host, error) {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return model.Host{}, err
	}
	if r.Host.Rev < 1 {
		return model.Host{}, repository.ErrInvalid
	}
	sn, err := model.NormalizeSN(r.PathSN)
	if err != nil {
		return model.Host{}, err
	}
	if r.Host.SN != "" {
		bodySN, err := model.NormalizeSN(r.Host.SN)
		if err != nil {
			return model.Host{}, err
		}
		if bodySN != sn {
			return model.Host{}, repository.ErrInvalid
		}
	}
	r.Host.SN = sn
	return s.deps.Hosts.UpdateHost(ctx, r.Host, s.audit(a, "host.update"))
}
func (s *registry) Delete(ctx context.Context, a ports.Actor, r DeleteRequest) error {
	if err := a.Authorize(access.PermHostWrite); err != nil {
		return err
	}
	if r.Rev < 1 {
		return repository.ErrInvalid
	}
	sn, err := model.NormalizeSN(r.SN)
	if err != nil {
		return err
	}
	return s.deps.Hosts.DeleteHost(ctx, sn, r.Rev, s.audit(a, "host.delete"))
}
func (s *registry) audit(a ports.Actor, action string) repository.Audit {
	return repository.Audit{Actor: a.Username, Action: action, At: s.deps.Clock()}
}
