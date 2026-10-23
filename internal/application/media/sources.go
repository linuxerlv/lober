package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type SourceDeps struct {
	ISOs     port.ISOs
	Sources  port.Sources
	Transfer port.Transfer
	Locks    port.SourceLocks
	Gate     port.RegistrationGate
}
type Sources struct {
	isos     port.ISOs
	sources  port.Sources
	transfer port.Transfer
	locks    port.SourceLocks
	gate     port.RegistrationGate
}

func NewSources(d SourceDeps) *Sources {
	return &Sources{isos: d.ISOs, sources: d.Sources, transfer: d.Transfer, locks: d.Locks, gate: d.Gate}
}
func (s *Sources) FreeSpace(ctx context.Context) (int64, int64, error) {
	return s.transfer.FreeSpace(ctx)
}

// Register 供受信任的内部调用者登记待办。后台调用 FetchOne，上传由客户端送片。
func (s *Sources) Register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a repository.Audit) error {
	return s.register(ctx, item, kind, addr, role, size, digest, a, false)
}

// RegisterQueued 将失败镜像补来源与重新排队一同提交；普通 Register 只登记来源。
func (s *Sources) RegisterQueued(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a repository.Audit) error {
	return s.register(ctx, item, kind, addr, role, size, digest, a, true)
}

func (s *Sources) register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a repository.Audit, queue bool) error {
	s.gate.RLock()
	defer s.gate.RUnlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := repository.CheckAudit(a); err != nil {
		return err
	}
	if err := checkSourceKey(item.ID, role); err != nil {
		return err
	}
	current, err := s.isos.GetISO(ctx, item.ID)
	if err != nil {
		return err
	}
	if current.State != model.ISOAcquiring && !(current.State == model.ISOFailed && role != "") {
		return repository.ErrConflict
	}
	if role != "" {
		extra, ok := catalog.ExtraSourceOf(current.Series)
		if !ok || role != extra.Role {
			return sourceRejected(item.ID, role, isoSourceReasonChunk)
		}
		if current.State == model.ISOFailed {
			main, err := s.getSource(ctx, item.ID, "")
			if err != nil {
				return err
			}
			if main.State != model.SourceFetched {
				return repository.ErrConflict
			}
		}
	}
	src := model.Source{ISOID: item.ID, Role: role, State: model.SourcePending, TotalSize: size, ExpectedSHA256: digest}
	switch kind {
	case "url":
		src.Kind = model.SourceRemote
		src.URL = addr

	case "path":
		src.Kind = model.SourceLocal
		src.Path = addr
	case "upload":
		src.Kind = model.SourceUpload
		if addr != "" || size <= 0 {
			return sourceRejected(item.ID, role, isoSourceReasonChunk)
		}
	default:
		return sourceRejected(item.ID, role, isoSourceReasonChunk)
	}
	src, err = s.transfer.ValidateSource(ctx, src)
	if err != nil {
		return s.problem(src, err)
	}
	if err = repository.CheckSource(src); err != nil {
		return err
	}
	unlock := s.locks.Lock(item.ID, role)
	defer unlock()
	if _, exists, err := s.sources.GetSource(ctx, item.ID, role); err != nil {
		return err
	} else if exists {
		return repository.ErrConflict
	}
	if queue && current.State == model.ISOFailed {
		_, err = s.sources.SaveSourceAndAcquire(ctx, src, current.Rev, a)
	} else {
		_, err = s.sources.SaveSource(ctx, src, a)
	}
	return err
}

// Cancel 先保存取消，再中断本实例的读取，不等待被下载占用的文件锁。
func (s *Sources) Cancel(ctx context.Context, id string, a repository.Audit) error {
	if err := repository.CheckAudit(a); err != nil {
		return err
	}
	item, err := s.isos.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if item.State != model.ISOAcquiring && item.State != model.ISOFailed {
		return repository.ErrConflict
	}
	sources, err := s.sources.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	for _, src := range sources {
		for attempts := 0; attempts < 16; attempts++ {
			if src.State != model.SourcePending && src.State != model.SourceFetching {
				break
			}
			src.State = model.SourceCancelled
			_, err = s.sources.SaveSource(ctx, src, a)
			if err == nil {
				s.locks.Cancel(id, src.Role)
				break
			}
			if !errors.Is(err, repository.ErrConflict) {
				return err
			}
			src, err = s.getSource(ctx, id, src.Role)
			if err != nil {
				return err
			}
			if attempts == 15 {
				return repository.ErrConflict
			}
		}
	}
	return nil
}

type AcquisitionProgress struct {
	State   model.ISOState
	Sources []AcquisitionSourceProgress
}
type AcquisitionSourceProgress struct {
	Role      string
	Kind      model.SourceKind
	State     model.SourceState
	Fetched   int64
	TotalSize int64
	Missing   []model.Range
	Error     string
}

func (s *Sources) Progress(ctx context.Context, id string) (AcquisitionProgress, error) {
	item, err := s.isos.GetISO(ctx, id)
	if err != nil {
		return AcquisitionProgress{}, err
	}
	sources, err := s.sources.SourcesOf(ctx, id)
	if err != nil {
		return AcquisitionProgress{}, err
	}
	out := AcquisitionProgress{State: item.State, Sources: make([]AcquisitionSourceProgress, 0, len(sources))}
	for _, src := range sources {
		p := AcquisitionSourceProgress{Role: src.Role, Kind: src.Kind, State: src.State, Fetched: src.Fetched, TotalSize: src.TotalSize, Error: src.Error}
		if src.State != model.SourceFetched && src.TotalSize > 0 {
			if src.Kind == model.SourceUpload {
				ranges, err := s.sources.RangesOf(ctx, id, src.Role)
				if err != nil {
					return AcquisitionProgress{}, err
				}
				pos := int64(0)
				for _, span := range ranges {
					if span.Start > pos {
						p.Missing = append(p.Missing, model.Range{Start: pos, Length: span.Start - pos})
					}
					pos = max(pos, span.Start+span.Length)
				}
				if pos < src.TotalSize {
					p.Missing = append(p.Missing, model.Range{Start: pos, Length: src.TotalSize - pos})
				}
			} else if src.Fetched < src.TotalSize {
				p.Missing = []model.Range{{Start: src.Fetched, Length: src.TotalSize - src.Fetched}}
			}
		}
		out.Sources = append(out.Sources, p)
	}
	return out, nil
}
