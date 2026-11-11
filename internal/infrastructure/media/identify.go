package iso

import (
	"context"
	"fmt"
	"io"
	"io/fs"
)

func identifyWIM(ctx context.Context, fsys fs.FS, name string) ([]WIMImage, error) {
	f, err := fsys.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	var r io.ReaderAt
	if at, ok := f.(io.ReaderAt); ok {
		r = at
	} else {
		r = &identifyForwardReader{ctx: ctx, r: f}
	}
	return WIMImages(r, info.Size())
}

// WIM metadata requires only ascending reads: header followed by XML resource.
// This adapter keeps Identify compatible with fs.File implementations without
// Seek or ReaderAt, without buffering the many-gigabyte install.wim.
type identifyForwardReader struct {
	ctx    context.Context
	r      io.Reader
	offset int64
}

func (r *identifyForwardReader) ReadAt(p []byte, off int64) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if off < r.offset {
		return 0, fmt.Errorf("WIM read offset moved backwards")
	}
	if off > r.offset {
		n, err := io.CopyN(io.Discard, identifyContextReader{ctx: r.ctx, r: r.r}, off-r.offset)
		r.offset += n
		if err != nil {
			return 0, err
		}
	}
	n, err := io.ReadFull(identifyContextReader{ctx: r.ctx, r: r.r}, p)
	r.offset += int64(n)
	return n, err
}

type identifyContextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r identifyContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}
