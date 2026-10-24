package media

import (
	"context"
	"sort"

	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

type ResumeKind string

const (
	ResumeFetch  ResumeKind = "fetch"
	ResumeImport ResumeKind = "import"
)

// 上传来源的 ResumeFetch 表示等客户端送片，后台须检查种类，不能调用 FetchOne。
type ResumeWork struct {
	ISOID string
	Role  string
	Kind  ResumeKind
}

// Recover 只按编号、角色和种类列出持久化待办，不开始下载、导入或清理。
func (s *Sources) Recover(ctx context.Context) ([]ResumeWork, error) {
	unfinished, err := s.sources.ListUnfinishedSources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ResumeWork, 0, len(unfinished))
	for _, src := range unfinished {
		if src.State != model.SourcePending && src.State != model.SourceFetching {
			continue
		}
		item, err := s.isos.GetISO(ctx, src.ISOID)
		if err != nil {
			return nil, err
		}
		if item.State == model.ISOAcquiring {
			out = append(out, ResumeWork{ISOID: src.ISOID, Role: src.Role, Kind: ResumeFetch})
		}
	}
	q := repository.ISOQuery{States: []model.ISOState{model.ISOAcquiring, model.ISOImporting}, Limit: 500}
	for {
		page, err := s.isos.ListISOs(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			sources, err := s.sources.SourcesOf(ctx, item.ID)
			if err != nil {
				return nil, err
			}
			if mediarules.AllFetched(item, sources) {
				out = append(out, ResumeWork{ISOID: item.ID, Kind: ResumeImport})
			}
		}
		if page.Next == "" {
			break
		}
		q.Cursor = page.Next
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].ISOID != out[j].ISOID {
			return out[i].ISOID < out[j].ISOID
		}
		if out[i].Role != out[j].Role {
			return out[i].Role < out[j].Role
		}
		return out[i].Kind < out[j].Kind
	})
	return out, nil
}
