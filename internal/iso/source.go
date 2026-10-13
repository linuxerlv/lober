package iso

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

const (
	isoSourceReasonURL   = model.ISOReasonURL
	isoSourceReasonLocal = model.ISOReasonPath
	isoSourceReasonChunk = model.ISOReasonChunk
)

func sourceRejected(id, role, reason string) error {
	return &store.ProblemsError{Kind: store.ErrInvalid, Problems: []model.Problem{model.NewProblem("iso_source_rejected", sourceSubject(id, role), reason)}}
}

func sourceSubject(id, role string) model.Subject {
	path := "source"
	if role != "" {
		path += "." + role
	}
	return model.Subject{Kind: "iso", ID: id, Path: path}
}

func checkSourceKey(id, role string) error {
	return store.CheckSourceKey(id, role)
}

func (s *Sources) getSource(ctx context.Context, id, role string) (store.Source, error) {
	src, ok, err := s.store.GetSource(ctx, id, role)
	if err != nil {
		return store.Source{}, err
	}
	if !ok {
		return store.Source{}, store.ErrNotFound
	}
	return src, nil
}

func (s *Sources) stagingPath(id, role string) string { return s.finalPath(id, role) + ".part" }
func (s *Sources) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(s.dirs.Staging, id)
}
func (s *Sources) sourcePath(src store.Source) string {
	if src.Kind == store.SourceLocal {
		return src.Path
	}
	return s.finalPath(src.ISOID, src.Role)
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.r.Read(p)
}

// resumeOffset treats the durable file as authoritative. A crash can leave the
// file either shorter or longer than the last committed progress checkpoint.
func (s *Sources) resumeOffset(ctx context.Context, src store.Source) (store.Source, int64, error) {
	if err := s.restorePublication(src); err != nil {
		return src, 0, err
	}
	st, err := os.Stat(s.stagingPath(src.ISOID, src.Role))
	n := int64(0)
	if err == nil {
		if !st.Mode().IsRegular() {
			return src, 0, store.ErrInvalid
		}
		n = st.Size()
	} else if !os.IsNotExist(err) {
		return src, 0, err
	}
	if src.Fetched != n {
		src.Fetched = n
		src, err = s.store.SaveSource(ctx, src, store.Audit{})
		if err != nil {
			return src, 0, err
		}
	}
	return src, n, nil
}

// A process can die after publishing the complete file but before committing
// fetched. Restore it to the partial name so the ordinary checksum and state
// transition remain the only path to completion. Called under the source lock.
func (s *Sources) restorePublication(src store.Source) error {
	if src.Kind == store.SourceLocal || src.State != store.SourceFetching {
		return nil
	}
	part := s.stagingPath(src.ISOID, src.Role)
	if _, err := os.Stat(part); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	final := s.sourcePath(src)
	st, err := os.Stat(final)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() {
		return store.ErrInvalid
	}
	return os.Rename(final, part)
}

func (s *Sources) finishFetch(ctx context.Context, src store.Source) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path := s.stagingPath(src.ISOID, src.Role)
	if src.Kind == store.SourceLocal {
		path = src.Path
	}
	var f *os.File
	var err error
	if src.Kind == store.SourceLocal {
		root, rel, ok := localRoot(s.dirs.LocalRoots, src.Path)
		if !ok {
			return sourceRejected(src.ISOID, src.Role, isoSourceReasonLocal)
		}
		f, err = os.OpenInRoot(root, rel)
	} else {
		f, err = os.Open(path)
	}
	if err != nil {
		return err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 || src.TotalSize > 0 && st.Size() != src.TotalSize {
		f.Close()
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, contextReader{ctx: ctx, r: f})
	after, statErr := f.Stat()
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if statErr != nil {
		return statErr
	}
	if closeErr != nil {
		return closeErr
	}
	if after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
	}
	got := fmt.Sprintf("%x", hash.Sum(nil))
	if src.ExpectedSHA256 != "" && got != src.ExpectedSHA256 {
		return &store.ProblemsError{Kind: store.ErrRejected, Problems: []model.Problem{model.NewProblem("iso_content_mismatch", sourceSubject(src.ISOID, src.Role), src.ExpectedSHA256, got)}}
	}
	// Refuse to publish after a concurrent cancellation/progress change.
	current, err := s.getSource(ctx, src.ISOID, src.Role)
	if err != nil {
		return err
	}
	if current.State != store.SourceFetching || current.Rev != src.Rev {
		return store.ErrConflict
	}
	final := s.sourcePath(src)
	if src.Kind != store.SourceLocal {
		if err = os.Rename(path, final); err != nil {
			return err
		}
	}
	src.State = store.SourceFetched
	src.Error = ""
	src.Fetched = st.Size()
	if src.TotalSize == 0 {
		src.TotalSize = st.Size()
	}
	if _, err = s.store.SaveSource(ctx, src, store.Audit{}); err != nil {
		// A cancellation may win after rename. Keep bytes as a resumable partial
		// rather than expose a final file with an unfetched database record.
		var restoreErr error
		if src.Kind != store.SourceLocal {
			restoreErr = os.Rename(final, path)
		}
		return errors.Join(err, restoreErr)
	}
	return nil
}

// failFetch reads current state instead of reusing the acquisition's stale Rev.
// Cancellation wins over progress and must never become a download failure.
func (s *Sources) failFetch(ctx context.Context, src store.Source, cause error) {
	cleanupCtx := context.WithoutCancel(ctx)
	current, err := s.getSource(cleanupCtx, src.ISOID, src.Role)
	if err != nil || current.State != store.SourceFetching {
		return
	}
	current.State = store.SourceFailed
	current.Error = cause.Error()
	if ctx.Err() != nil {
		current.State = store.SourceCancelled
	}
	_, _ = s.store.SaveSource(cleanupCtx, current, store.Audit{})
}

// writeFetch checkpoints only bytes that have been synced to the staging file.
// The source Rev prevents a late downloader from undoing Cancel.
func (s *Sources) writeFetch(ctx context.Context, src store.Source, f *os.File, r io.Reader, offset int64) (store.Source, error) {
	buf := make([]byte, 256<<10)
	for {
		if err := ctx.Err(); err != nil {
			return src, err
		}
		n, readErr := r.Read(buf)
		if n > 0 {
			if src.TotalSize > 0 && int64(n) > src.TotalSize-offset {
				return src, sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
			}
			count, err := f.Write(buf[:n])
			if err != nil {
				return src, err
			}
			if count != n {
				return src, io.ErrShortWrite
			}
			offset += int64(n)
			if err = f.Sync(); err != nil {
				return src, err
			}
			src.Fetched = offset
			src, err = s.store.SaveSource(ctx, src, store.Audit{})
			if err != nil {
				return src, err
			}
		}
		if readErr == io.EOF {
			return src, nil
		}
		if readErr != nil {
			return src, readErr
		}
		if n == 0 {
			return src, io.ErrNoProgress
		}
	}
}
