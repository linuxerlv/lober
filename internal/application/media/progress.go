package media

import (
	"context"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// Progress 先镜像再来源；每个来源读确认区间后核对版本，最多三次，不承诺整镜像快照。
func (s *registry) Progress(ctx context.Context, a ports.Actor, id string) (Progress, error) {
	item, err := s.Get(ctx, a, id)
	if err != nil {
		return Progress{}, err
	}
	sources, err := s.deps.Sources.SourcesOf(ctx, item.ID)
	if err != nil {
		return Progress{}, err
	}
	out := Progress{ID: item.ID, State: item.State, Rev: item.Rev, Sources: make([]SourceProgress, 0, len(sources))}
	for _, source := range sources {
		p, err := s.sourceProgress(ctx, source)
		if err != nil {
			return Progress{}, err
		}
		out.Sources = append(out.Sources, p)
	}
	return out, nil
}
func (s *registry) sourceProgress(ctx context.Context, source model.Source) (SourceProgress, error) {
	for attempt := 0; attempt < 3; attempt++ {
		ranges, err := s.deps.Sources.RangesOf(ctx, source.ISOID, source.Role)
		if err != nil {
			return SourceProgress{}, err
		}
		current, exists, err := s.deps.Sources.GetSource(ctx, source.ISOID, source.Role)
		if err != nil {
			return SourceProgress{}, err
		}
		if !exists {
			return SourceProgress{}, repository.ErrNotFound
		}
		if current.Rev != source.Rev {
			source = current
			continue
		}
		if ranges == nil {
			ranges = []model.Range{}
		}
		return SourceProgress{source.Role, source.Kind, source.TotalSize, source.Fetched, source.State, source.Error, source.Rev, ranges}, nil
	}
	return SourceProgress{}, repository.ErrConflict
}
