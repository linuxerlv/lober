package iso

import (
	"bytes"
	"context"
	"github.com/lober/lober/internal/domain/model"
	"io"
	"os"

	"github.com/lober/lober/internal/domain/repository"
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
			return repository.ErrConflict
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

func (s *Sources) Upload(ctx context.Context, id, role string, offset int64, r io.Reader, a repository.Audit) (err error) {
	if err = ctx.Err(); err != nil {
		return err
	}
	if err = checkSourceKey(id, role); err != nil {
		return err
	}
	if a != (repository.Audit{}) {
		if err = repository.CheckAudit(a); err != nil {
			return err
		}
	}
	unlock := s.lockSource(id, role)
	defer unlock()
	src, err := s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.Kind != model.SourceUpload || src.State != model.SourcePending && src.State != model.SourceFetching && src.State != model.SourceFetched {
		return repository.ErrConflict
	}
	if offset < 0 || offset >= src.TotalSize || src.TotalSize <= 0 || r == nil {
		return sourceRejected(id, role, isoSourceReasonChunk)
	}
	if err = os.MkdirAll(s.dirs.Staging, 0700); err != nil {
		return err
	}
	// Spool at most the remaining space plus one sentinel byte before touching
	// confirmed content; an overlong body can never overwrite existing bytes.
	chunk, err := os.CreateTemp(s.dirs.Staging, ".chunk-"+id+"-")
	if err != nil {
		return err
	}
	defer func() { chunk.Close(); os.Remove(chunk.Name()) }()
	remaining := src.TotalSize - offset
	limit := remaining
	if limit < 1<<63-1 {
		limit++
	}
	n, err := io.Copy(chunk, io.LimitReader(contextReader{ctx: ctx, r: r}, limit))
	if err != nil {
		return err
	}
	if n <= 0 || n > remaining {
		return sourceRejected(id, role, isoSourceReasonChunk)
	}
	ranges, err := s.store.RangesOf(ctx, id, role)
	if err != nil {
		return err
	}
	path := s.stagingPath(id, role)
	if err = s.restorePublication(src); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_RDWR
	if src.State == model.SourceFetched {
		path = s.sourcePath(src)
		flags = os.O_RDONLY
	}
	f, err := os.OpenFile(path, flags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	for _, span := range ranges {
		start := max(offset, span.Start)
		end := min(offset+n, span.Start+span.Length)
		if start >= end {
			continue
		}
		err = compareRange(ctx, io.NewSectionReader(f, start, end-start), io.NewSectionReader(chunk, start-offset, end-start))
		if err != nil {
			return sourceRejected(id, role, isoSourceReasonChunk)
		}
	}
	if src.State == model.SourceFetched {
		if err = compareRange(ctx, io.NewSectionReader(f, offset, n), io.NewSectionReader(chunk, 0, n)); err != nil {
			return sourceRejected(id, role, isoSourceReasonChunk)
		}
		if a != (repository.Audit{}) {
			_, err = s.store.SaveSource(ctx, src, a)
		}
		return err
	}
	if src.State == model.SourcePending {
		src.State = model.SourceFetching
	}
	// Persist the transition/audit only after the chunk bounds and all overlap
	// bytes pass. SaveRange below atomically updates union progress and Rev.
	src, err = s.store.SaveSource(ctx, src, a)
	if err != nil {
		return err
	}
	if _, err = f.Seek(offset, io.SeekStart); err != nil {
		return err
	}
	if _, err = io.Copy(f, contextReader{ctx: ctx, r: io.NewSectionReader(chunk, 0, n)}); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	if err = s.store.SaveRange(ctx, id, role, offset, n); err != nil {
		return err
	}
	src, err = s.getSource(ctx, id, role)
	if err != nil {
		return err
	}
	if src.State != model.SourceFetching {
		return repository.ErrConflict
	}
	ranges, err = s.store.RangesOf(ctx, id, role)
	if err != nil {
		return err
	}
	if len(ranges) == 1 && ranges[0].Start == 0 && ranges[0].Length == src.TotalSize {
		if err = f.Close(); err != nil {
			return err
		}
		err = s.finishFetch(ctx, src)
		if err != nil {
			s.failFetch(ctx, src, err)
		}
	}
	return err
}
