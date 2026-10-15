package iso

import (
	"context"
	"net"
	"net/netip"
	"sync"

	"github.com/lober/lober/internal/store"
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
	store       store.Store
	dirs        Dirs
	fileLocks   sync.Map
	active      sync.Map
	resolver    func(context.Context, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

func New(st store.Store, d Dirs) *Sources {
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
	if src.State == store.SourceFetched {
		return nil
	}
	if src.Kind == store.SourceUpload || src.State != store.SourcePending && src.State != store.SourceFetching {
		return store.ErrConflict
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
	if src.State == store.SourcePending {
		src.State = store.SourceFetching
		src, err = s.store.SaveSource(ctx, src, store.Audit{})
		if err != nil {
			return err
		}
	}
	if src.State != store.SourceFetching {
		return store.ErrConflict
	}
	defer func() {
		if err != nil {
			s.failFetch(ctx, src, err)
		}
	}()
	switch src.Kind {
	case store.SourceRemote:
		err = s.fetchRemote(ctx, src)
	case store.SourceLocal:
		err = s.fetchLocal(ctx, src)
	default:
		err = store.ErrInvalid
	}
	return err
}
