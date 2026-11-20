package iso

import (
	"bytes"
	"context"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"io"
	"os"
)

// compareRange compares persisted bytes, not a caller-provided chunk digest.
// This allows arbitrary chunk sizes and detects conflicts in partial overlaps.
func compareRange(ctx context.Context, left, right io.Reader) error {
	a, b := make([]byte, 64<<10), make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, e := io.ReadFull(left, a)
		m, f := io.ReadFull(right, b)
		if m != n || !bytes.Equal(a[:n], b[:m]) {
			return fault(port.ChunkRejected)
		}
		if f != nil && f != io.EOF && f != io.ErrUnexpectedEOF {
			return f
		}
		if e == io.EOF || e == io.ErrUnexpectedEOF {
			return nil
		}
		if e != nil {
			return e
		}
	}
}

type uploadSession struct {
	transfer         *Transfer
	src              model.Source
	offset, size     int64
	chunk, confirmed *os.File
	closed           bool
}

func (t *Transfer) BeginUpload(ctx context.Context, src model.Source, offset int64, r io.Reader) (port.UploadSession, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if offset < 0 || offset >= src.TotalSize || r == nil {
		return nil, fault(port.ChunkRejected)
	}
	if err := os.MkdirAll(t.dirs.Staging, 0700); err != nil {
		return nil, err
	}
	chunk, err := os.CreateTemp(t.dirs.Staging, ".chunk-"+src.ISOID+"-")
	if err != nil {
		return nil, err
	}
	session := &uploadSession{transfer: t, src: src, offset: offset, chunk: chunk}
	remaining := src.TotalSize - offset
	limit := remaining
	if limit < 1<<63-1 {
		limit++
	}
	n, err := io.Copy(chunk, io.LimitReader(contextReader{ctx: ctx, r: r}, limit))
	if err != nil {
		session.Close()
		return nil, err
	}
	if n <= 0 || n > remaining {
		session.Close()
		return nil, fault(port.ChunkRejected)
	}
	session.size = n
	return session, nil
}
func (s *uploadSession) Size() int64 { return s.size }
func (s *uploadSession) CheckOverlap(ctx context.Context, ranges []model.Range) error {
	if err := s.transfer.restorePublication(s.src); err != nil {
		return err
	}
	path := s.transfer.stagingPath(s.src.ISOID, s.src.Role)
	flags := os.O_CREATE | os.O_RDWR
	if s.src.State == model.SourceFetched {
		path = s.transfer.sourcePath(s.src)
		flags = os.O_RDONLY
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	s.confirmed = f
	for _, span := range ranges {
		start := max(s.offset, span.Start)
		end := min(s.offset+s.size, span.Start+span.Length)
		if start >= end {
			continue
		}
		if err = compareRange(ctx, io.NewSectionReader(f, start, end-start), io.NewSectionReader(s.chunk, start-s.offset, end-start)); err != nil {
			return fault(port.ChunkRejected)
		}
	}
	return nil
}
func (s *uploadSession) VerifyReplay(ctx context.Context) error {
	if err := compareRange(ctx, io.NewSectionReader(s.confirmed, s.offset, s.size), io.NewSectionReader(s.chunk, 0, s.size)); err != nil {
		return fault(port.ChunkRejected)
	}
	return nil
}
func (s *uploadSession) Write(ctx context.Context) error {
	if _, err := s.confirmed.Seek(s.offset, io.SeekStart); err != nil {
		return err
	}
	if _, err := io.Copy(s.confirmed, contextReader{ctx: ctx, r: io.NewSectionReader(s.chunk, 0, s.size)}); err != nil {
		return err
	}
	if err := s.confirmed.Sync(); err != nil {
		return err
	}
	return nil
}
func (s *uploadSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	var err error
	if s.confirmed != nil {
		err = s.confirmed.Close()
		s.confirmed = nil
	}
	// 临时片是本次调用的清理工作，沿用原行为，不把清理错误当成发布失败。
	if s.chunk != nil {
		s.chunk.Close()
		os.Remove(s.chunk.Name())
		s.chunk = nil
	}
	return err
}
