package iso

import (
	"context"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
)

type Transfer struct {
	dirs        Dirs
	resolver    func(context.Context, string) ([]netip.Addr, error)
	dialContext func(context.Context, string, string) (net.Conn, error)
}

var _ port.Transfer = (*Transfer)(nil)

func NewTransfer(d Dirs) *Transfer {
	d.LocalRoots = append([]string(nil), d.LocalRoots...)
	return &Transfer{dirs: d, resolver: func(ctx context.Context, host string) ([]netip.Addr, error) {
		return net.DefaultResolver.LookupNetIP(ctx, "ip", host)
	}, dialContext: (&net.Dialer{}).DialContext}
}
func (t *Transfer) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(t.dirs.Staging, id)
}
func (t *Transfer) stagingPath(id, role string) string { return t.finalPath(id, role) + ".part" }
func (t *Transfer) sourcePath(src model.Source) string {
	if src.Kind == model.SourceLocal {
		return src.Path
	}
	return t.finalPath(src.ISOID, src.Role)
}
func fault(kind port.FaultKind) error { return &port.Fault{Kind: kind} }
func (t *Transfer) ValidateSource(ctx context.Context, src model.Source) (model.Source, error) {
	switch src.Kind {
	case model.SourceRemote:
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
		if err != nil {
			return src, fault(port.URLRejected)
		}
		ips, err := t.lookup(ctx, req.URL.Hostname())
		if err != nil {
			return src, err
		}
		if err = CheckURL(req.URL, ips); err != nil {
			return src, err
		}
	case model.SourceLocal:
		if err := CheckLocalPath(t.dirs.LocalRoots, src.Path); err != nil {
			return src, err
		}
		p, err := resolvedPath(src.Path)
		if err != nil {
			return src, err
		}
		src.Path = p
	}
	return src, nil
}
func (t *Transfer) FreeSpace(ctx context.Context) (int64, int64, error) {
	st, err := freeSpace(ctx, t.dirs.Staging)
	if err != nil {
		return 0, 0, err
	}
	art, err := freeSpace(ctx, t.dirs.Artifacts)
	return st, art, err
}
func (t *Transfer) BeginFetch(ctx context.Context, src model.Source) (port.FetchSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &fetchSession{transfer: t, src: src, offset: src.Fetched, total: src.TotalSize}, nil
}
func (t *Transfer) BeginPublish(ctx context.Context, src model.Source) (port.FetchSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &fetchSession{transfer: t, src: src, offset: src.Fetched, total: src.TotalSize}, nil
}
func (t *Transfer) restorePublication(src model.Source) error {
	if src.Kind == model.SourceLocal || src.State != model.SourceFetching {
		return nil
	}
	part := t.stagingPath(src.ISOID, src.Role)
	if _, err := os.Stat(part); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	final := t.sourcePath(src)
	st, err := os.Stat(final)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return fault(port.InvalidFile)
	}
	return os.Rename(final, part)
}

// 会话持有全部外部句柄；关闭不会删除有效断点。
type fetchSession struct {
	buffer        []byte
	transfer      *Transfer
	src           model.Source
	offset, total int64
	output        *os.File
	body          io.ReadCloser
	closed        bool
}
