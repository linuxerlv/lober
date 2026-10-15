package iso

import (
	"context"
	"sort"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

type ResumeKind string

const (
	ResumeFetch  ResumeKind = "fetch"
	ResumeImport ResumeKind = "import"
)

// ResumeFetch on an upload means await client chunks. The assembly point must
// inspect the persisted source kind; Upload, not FetchOne, receives those bytes.
type ToResume struct {
	ISOID string
	Role  string
	Kind  ResumeKind
}

func allFetched(item model.ISO, sources []store.Source) bool {
	main := false
	extraFound := false
	extra, needsExtra := catalog.ExtraSourceOf(item.Series)
	for _, src := range sources {
		if src.State != store.SourceFetched {
			return false
		}
		if src.Role == "" {
			main = true
		}
		if needsExtra && src.Role == extra.Role {
			extraFound = true
		}
	}
	return main && (!needsExtra || extraFound)
}

// Recover returns durable work only. It never downloads, imports or removes
// temporary directories; cleanup belongs to the subsequent Import invocation.
func (s *Sources) Recover(ctx context.Context) ([]ToResume, error) {
	unfinished, err := s.store.ListUnfinishedSources(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ToResume, 0, len(unfinished))
	for _, src := range unfinished {
		if src.State != store.SourcePending && src.State != store.SourceFetching {
			continue
		}
		item, err := s.store.GetISO(ctx, src.ISOID)
		if err != nil {
			return nil, err
		}
		if item.State == model.ISOAcquiring {
			out = append(out, ToResume{ISOID: src.ISOID, Role: src.Role, Kind: ResumeFetch})
		}
	}
	q := store.ISOQuery{States: []model.ISOState{model.ISOAcquiring, model.ISOImporting}, Limit: 500}
	for {
		page, err := s.store.ListISOs(ctx, q)
		if err != nil {
			return nil, err
		}
		for _, item := range page.Items {
			sources, err := s.store.SourcesOf(ctx, item.ID)
			if err != nil {
				return nil, err
			}
			if allFetched(item, sources) {
				out = append(out, ToResume{ISOID: item.ID, Kind: ResumeImport})
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
