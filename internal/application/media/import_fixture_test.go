package media_test

import (
	"bytes"
	"context"
	"github.com/lober/lober/internal/application/media"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

const winpeshlContent = "[LaunchApps]\r\n%SYSTEMDRIVE%\\lober\\startnet.cmd\r\n"

type acquisitionFixture struct {
	store repository.Store
	dirs  iso.Dirs
	locks port.SourceLocks
	gate  port.RegistrationGate
	files port.ImportFiles
}

func newAcquisitionFixture(st repository.Store, d iso.Dirs) *acquisitionFixture {
	return &acquisitionFixture{store: st, dirs: d, locks: iso.NewSourceLocks(), gate: iso.NewRegistrationGate(), files: iso.NewImportFiles(d)}
}
func (s *acquisitionFixture) service() *media.Sources {
	return media.NewSources(media.SourceDeps{ISOs: s.store, Sources: s.store, Transfer: iso.NewTransfer(s.dirs), Import: s.files, Locks: s.locks, Gate: s.gate})
}
func (s *acquisitionFixture) Register(ctx context.Context, i model.ISO, k, a, r string, n int64, d string, audit repository.Audit) error {
	return s.service().Register(ctx, i, k, a, r, n, d, audit)
}
func (s *acquisitionFixture) Upload(ctx context.Context, id, r string, n int64, b io.Reader, a repository.Audit) error {
	return s.service().Upload(ctx, id, r, n, b, a)
}
func (s *acquisitionFixture) Import(ctx context.Context, id string) error {
	return s.service().Import(ctx, id)
}
func (s *acquisitionFixture) Reimport(ctx context.Context, id string, a repository.Audit) error {
	if err := s.service().RequestReimport(ctx, id, a); err != nil {
		return err
	}
	return s.Import(ctx, id)
}
func (s *acquisitionFixture) finalPath(id, role string) string {
	if role != "" {
		id += "." + role
	}
	return filepath.Join(s.dirs.Staging, id)
}
func (s *acquisitionFixture) sourcePath(src model.Source) string {
	if src.Kind == model.SourceLocal {
		return src.Path
	}
	return s.finalPath(src.ISOID, src.Role)
}
func (s *acquisitionFixture) artifactRoot(series catalog.Series, version string, arch catalog.Arch) string {
	return filepath.Join(s.dirs.Artifacts, string(series), version, string(arch))
}
func mustSource(t *testing.T, s *acquisitionFixture, id, role string) model.Source {
	t.Helper()
	src, ok, err := s.store.GetSource(t.Context(), id, role)
	if err != nil || !ok {
		t.Fatal(src, err)
	}
	return src
}

// 内存文件只负责给应用识别测试提供事实。真实文件会话另有集成测试。
type testMediaView struct{ tree fs.FS }

func (v testMediaView) Stat(ctx context.Context, name string) (port.Entry, error) {
	st, err := fs.Stat(v.tree, name)
	if err != nil {
		return port.Entry{}, err
	}
	return port.Entry{Name: st.Name(), Size: st.Size(), IsDir: st.IsDir(), IsRegular: st.Mode().IsRegular()}, nil
}
func (v testMediaView) Entries(ctx context.Context, name string) ([]port.Entry, error) {
	entries, err := fs.ReadDir(v.tree, name)
	if err != nil {
		return nil, err
	}
	var out []port.Entry
	for _, e := range entries {
		st, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, port.Entry{Name: st.Name(), Size: st.Size(), IsDir: st.IsDir(), IsRegular: st.Mode().IsRegular()})
	}
	return out, nil
}
func (v testMediaView) Text(ctx context.Context, name string, limit int64) (string, error) {
	f, err := v.tree.Open(name)
	if err != nil {
		return "", err
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit+1))
	if err != nil {
		return "", err
	}
	if int64(len(b)) > limit {
		return "", port.ErrTextTooLarge
	}
	return strings.TrimPrefix(string(b), "\ufeff"), ctx.Err()
}
func (v testMediaView) WIM(ctx context.Context, name string) ([]mediarules.EditionFact, error) {
	f, err := v.tree.Open(name)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	r, ok := f.(io.ReaderAt)
	if !ok {
		b, e := io.ReadAll(f)
		if e != nil {
			return nil, e
		}
		r = bytes.NewReader(b)
	}
	images, err := iso.WIMImages(r, st.Size())
	if err != nil {
		return nil, err
	}
	var out []mediarules.EditionFact
	for _, x := range images {
		out = append(out, mediarules.EditionFact{Name: x.Name, Flags: x.Flags, Version: x.Version, Arch: x.Arch})
	}
	return out, nil
}
func (v testMediaView) Digest(context.Context) (string, error) { return "", nil }
func Identify(ctx context.Context, tree fs.FS) (mediarules.Identified, error) {
	return media.Identify(ctx, testMediaView{tree: tree})
}
