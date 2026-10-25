package iso

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"io"
	"os"
)

func (s *fetchSession) Resume(ctx context.Context) (int64, error) {
	if err := ctx.Err(); err != nil {
		return s.offset, err
	}
	if s.src.Kind == model.SourceLocal {
		return s.offset, nil
	}
	if err := s.transfer.restorePublication(s.src); err != nil {
		return s.offset, err
	}
	st, err := os.Stat(s.transfer.stagingPath(s.src.ISOID, s.src.Role))
	s.offset = 0
	if err == nil {
		if !st.Mode().IsRegular() {
			return 0, fault(port.InvalidFile)
		}
		s.offset = st.Size()
	} else if !os.IsNotExist(err) {
		return 0, err
	}
	return s.offset, nil
}
func (s *fetchSession) Open(ctx context.Context) (port.FetchInfo, error) {
	if s.src.Kind == model.SourceRemote {
		return s.openRemote(ctx)
	}
	if s.src.Kind != model.SourceLocal {
		return port.FetchInfo{}, fault(port.InvalidFile)
	}
	root, rel, ok := localRoot(s.transfer.dirs.LocalRoots, s.src.Path)
	if !ok {
		return port.FetchInfo{}, fault(port.PathRejected)
	}
	f, err := os.OpenInRoot(root, rel)
	if err != nil {
		return port.FetchInfo{}, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return port.FetchInfo{}, err
	}
	if !st.Mode().IsRegular() || st.Size() <= 0 {
		return port.FetchInfo{}, fault(port.PathRejected)
	}
	s.total = st.Size()
	return port.FetchInfo{Offset: s.offset, TotalSize: s.total, Complete: true}, nil
}
func (s *fetchSession) Step(ctx context.Context) (int64, bool, error) {
	if err := ctx.Err(); err != nil {
		return s.offset, false, err
	}
	if s.buffer == nil {
		s.buffer = make([]byte, 256<<10)
	}
	buf := s.buffer
	n, readErr := s.body.Read(buf)
	if n > 0 {
		if s.total > 0 && int64(n) > s.total-s.offset {
			return s.offset, false, fault(port.ChunkRejected)
		}
		count, err := s.output.Write(buf[:n])
		if err != nil {
			return s.offset, false, err
		}
		if count != n {
			return s.offset, false, io.ErrShortWrite
		}
		if err = s.output.Sync(); err != nil {
			return s.offset, false, err
		}
		s.offset += int64(n)
	}
	if readErr == io.EOF {
		return s.offset, true, nil
	}
	if readErr != nil {
		return s.offset, false, readErr
	}
	if n == 0 {
		return s.offset, false, io.ErrNoProgress
	}
	return s.offset, false, nil
}
func (s *fetchSession) closeOutput() error {
	if s.output == nil {
		return nil
	}
	f := s.output
	s.output = nil
	return f.Close()
}
func (s *fetchSession) Verify(ctx context.Context) (port.VerifiedContent, error) {
	if err := s.closeOutput(); err != nil {
		return port.VerifiedContent{}, err
	}
	var f *os.File
	var err error
	if s.src.Kind == model.SourceLocal {
		root, rel, ok := localRoot(s.transfer.dirs.LocalRoots, s.src.Path)
		if !ok {
			return port.VerifiedContent{}, fault(port.PathRejected)
		}
		f, err = os.OpenInRoot(root, rel)
	} else {
		f, err = os.Open(s.transfer.stagingPath(s.src.ISOID, s.src.Role))
	}
	if err != nil {
		return port.VerifiedContent{}, err
	}
	st, err := f.Stat()
	if err != nil {
		f.Close()
		return port.VerifiedContent{}, err
	}
	if !st.Mode().IsRegular() {
		f.Close()
		return port.VerifiedContent{}, fault(port.ChunkRejected)
	}
	hash := sha256.New()
	_, err = io.Copy(hash, contextReader{ctx: ctx, r: f})
	after, statErr := f.Stat()
	closeErr := f.Close()
	if err != nil || statErr != nil || closeErr != nil {
		return port.VerifiedContent{}, errors.Join(err, statErr, closeErr)
	}
	if after.Size() != st.Size() || !after.ModTime().Equal(st.ModTime()) {
		return port.VerifiedContent{}, fault(port.ChunkRejected)
	}
	return port.VerifiedContent{Size: st.Size(), SHA256: fmt.Sprintf("%x", hash.Sum(nil))}, nil
}
func (s *fetchSession) Publish(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if s.src.Kind == model.SourceLocal {
		return nil
	}
	return os.Rename(s.transfer.stagingPath(s.src.ISOID, s.src.Role), s.transfer.sourcePath(s.src))
}
func (s *fetchSession) Restore(ctx context.Context) error {
	if s.src.Kind == model.SourceLocal {
		return nil
	}
	return os.Rename(s.transfer.sourcePath(s.src), s.transfer.stagingPath(s.src.ISOID, s.src.Role))
}
func (s *fetchSession) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	err := s.closeOutput()
	if s.body != nil {
		err = errors.Join(err, s.body.Close())
		s.body = nil
	}
	return err
}
