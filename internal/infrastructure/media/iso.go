package iso

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"net"
	"net/netip"
	"sync"

	"github.com/lober/lober/internal/domain/repository"
)

// Dirs are injected by the assembly point, never read from environment variables.
type Dirs struct {
	Staging    string
	Artifacts  string
	SMB        string
	Tools      string
	LocalRoots []string
}

// Sources implements the caller-owned acquisition interface. It owns no workers.
type Sources struct {
	store       repository.Store
	dirs        Dirs
	fileLocks   sync.Map
	active      sync.Map
	resolver    func(context.Context, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func New(st repository.Store, d Dirs) *Sources {
	d.LocalRoots = append([]string(nil), d.LocalRoots...)
	return &Sources{store: st, dirs: d, resolver: func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}, dialContext: (&net.Dialer{}).DialContext}
}

func (s *Sources) lockSource(id, role string) func() {
	value, _ := s.fileLocks.LoadOrStore(id+"/"+role, &sync.Mutex{})
	m := value.(*sync.Mutex)
	m.Lock()
	return m.Unlock
}

// FetchOne blocks until a registered remote/local source completes. Upload
// sources receive their bytes through Upload instead. Restarting a fetching
// source resumes its durable partial file; explicit Retry resets terminal work.
func (s *Sources) FetchOne(ctx context.Context, id, role string) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = checkSourceKey(id, role); err != nil {
		return err
	}
	unlock := s.lockSource(id, role)
	defer unlock()
	src, err := s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.State == model.SourceFetched {
		return nil
	}
	if src.Kind == model.SourceUpload || src.State != model.SourcePending && src.State != model.SourceFetching {
		return repository.ErrConflict
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	key := id + "/" + role
	s.active.Store(key, cancel)
	defer s.active.Delete(key)
	// Publish the cancel function before reading the state again. Cancel can then
	// either cancel this context or make the optimistic state transition conflict.
	src, err = s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.State == model.SourcePending {
		src.State = model.SourceFetching
		src, err = s.store.SaveSource(ctx, src, repository.Audit{})
		if err != nil {
			return err
		}
	}
	if src.State != model.SourceFetching {
		return repository.ErrConflict
	}
	defer func() {
		if err != nil {
			s.failFetch(ctx, src, err)
		}
	}()
	switch src.Kind {
	case model.SourceRemote:
		err = s.fetchRemote(ctx, src)
	case model.SourceLocal:
		err = s.fetchLocal(ctx, src)
	default:
		err = repository.ErrInvalid
	}
	return err
}
