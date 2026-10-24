package iso

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/media/iso9660"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

type ImportFiles struct{ dirs Dirs }

func NewImportFiles(d Dirs) *ImportFiles {
	d.LocalRoots = append([]string(nil), d.LocalRoots...)
	return &ImportFiles{dirs: d}
}

var _ port.ImportFiles = (*ImportFiles)(nil)

func (f *ImportFiles) Open(ctx context.Context, item model.ISO) (port.ImportSession, error) {
	return &importSession{files: f, item: item, views: map[string]*mediaView{}}, nil
}

type importSession struct {
	files                         *ImportFiles
	item                          model.ISO
	views                         map[string]*mediaView
	opened                        []*mediaView
	root, tmp, smb, smbTmp, owner string
	roles                         map[string]string
	published                     port.Publication
}
type mediaView struct {
	file         *os.File
	tree         fs.FS
	before       fs.FileInfo
	digest, role string
}

// 打开和结构解析沿用旧顺序；取消在摘要读取时检查，不把有效额外源误报成坏介质。
func (s *importSession) OpenSource(ctx context.Context, src model.Source) (port.MediaView, error) {
	if v := s.views[src.Role]; v != nil {
		return v, nil
	}
	var f *os.File
	var err error
	if src.Kind == model.SourceLocal {
		root, rel, ok := localRoot(s.files.dirs.LocalRoots, src.Path)
		if !ok {
			return nil, &port.Fault{Kind: port.PathRejected, Role: src.Role}
		}
		f, err = os.OpenInRoot(root, rel)
	} else {
		name := src.ISOID
		if src.Role != "" {
			name += "." + src.Role
		}
		f, err = os.Open(filepath.Join(s.files.dirs.Staging, name))
	}
	if err != nil {
		return nil, err
	}
	before, err := f.Stat()
	if err != nil || !before.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = fault(port.InvalidFile)
		}
		return nil, err
	}
	if src.TotalSize > 0 && before.Size() != src.TotalSize {
		f.Close()
		return nil, &port.Fault{Kind: port.ContentMismatch, Role: src.Role, Expected: fmt.Sprintf("size %d", src.TotalSize), Actual: fmt.Sprintf("size %d", before.Size())}
	}
	tree, err := iso9660.Open(iso9660.FileReader{ReaderAt: f, Length: before.Size()})
	if err != nil {
		f.Close()
		return nil, errors.Join(port.ErrMediaMalformed, err)
	}
	v := &mediaView{file: f, tree: tree, before: before, role: src.Role}
	s.views[src.Role] = v
	s.opened = append(s.opened, v)
	return v, nil
}
func entry(st fs.FileInfo) port.Entry {
	return port.Entry{Name: st.Name(), Size: st.Size(), IsDir: st.IsDir(), IsRegular: st.Mode().IsRegular()}
}
func (v *mediaView) Stat(ctx context.Context, name string) (port.Entry, error) {
	st, err := fs.Stat(v.tree, name)
	if err != nil {
		return port.Entry{}, err
	}
	return entry(st), nil
}
func (v *mediaView) Entries(ctx context.Context, name string) ([]port.Entry, error) {
	entries, err := fs.ReadDir(v.tree, name)
	if err != nil {
		return nil, err
	}
	out := make([]port.Entry, 0, len(entries))
	for _, e := range entries {
		st, err := e.Info()
		if err != nil {
			return nil, err
		}
		out = append(out, entry(st))
	}
	return out, nil
}
func (v *mediaView) Text(ctx context.Context, name string, limit int64) (string, error) {
	return mediaText(ctx, v.tree, name, limit)
}
func mediaText(ctx context.Context, tree fs.FS, name string, limit int64) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	f, err := tree.Open(name)
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
func (v *mediaView) WIM(ctx context.Context, name string) ([]mediarules.EditionFact, error) {
	images, err := identifyWIM(ctx, v.tree, name)
	if err != nil {
		return nil, err
	}
	out := make([]mediarules.EditionFact, len(images))
	for i, x := range images {
		out[i] = mediarules.EditionFact{Name: x.Name, Flags: x.Flags, Version: x.Version, Arch: x.Arch}
	}
	return out, nil
}
func (v *mediaView) Digest(ctx context.Context) (string, error) {
	digest, err := importDigest(ctx, v.file)
	if err == nil {
		v.digest = digest
	}
	return digest, err
}
func (s *importSession) BeginPrepare(ctx context.Context, plan port.ExtractionPlan) error {
	s.item = plan.ISO
	s.root = filepath.Join(s.files.dirs.Artifacts, string(plan.ISO.Series), plan.ISO.Version, string(plan.ISO.Arch))
	tmp := s.root + ".tmp-" + plan.ISO.ID
	if err := prepareImportTemp(tmp); err != nil {
		return err
	}
	s.tmp = tmp
	return nil
}
func (s *importSession) Prepare(ctx context.Context, plan port.ExtractionPlan) (map[string]string, error) {
	main := s.views[""]
	var extra fs.FS
	for _, v := range s.opened {
		if v.role != "" {
			extra = v.tree
			break
		}
	}
	roles, err := s.files.extract(ctx, plan, main.tree, extra, main.file, s.tmp)
	if err != nil {
		return nil, err
	}
	for _, role := range plan.RequiredRoles {
		rel := strings.TrimSuffix(roles[role], "/")
		if rel == "" || !safeImportRel(rel) {
			return nil, &port.ExtractionFault{Kind: "artifact", Name: role}
		}
		st, e := os.Stat(filepath.Join(s.tmp, filepath.FromSlash(rel)))
		if e != nil || !st.IsDir() && (!st.Mode().IsRegular() || st.Size() == 0) {
			return nil, &port.ExtractionFault{Kind: "artifact", Name: role}
		}
	}
	s.roles = roles
	s.owner = plan.ISO.ID + "\n" + plan.ISO.SHA256 + "\n"
	if err = os.WriteFile(filepath.Join(s.tmp, ".lober-owner"), []byte(s.owner), 0644); err != nil {
		return nil, err
	}
	if plan.ISO.Family == "windows" {
		s.smb = filepath.Join(s.files.dirs.SMB, string(plan.ISO.Series), plan.ISO.Version, string(plan.ISO.Arch))
		smbTmp := s.smb + ".tmp-" + plan.ISO.ID
		if err = prepareImportTemp(smbTmp); err != nil {
			return nil, err
		}
		s.smbTmp = smbTmp
		if err = copyImportTree(ctx, main.tree, s.smbTmp); err != nil {
			return nil, err
		}
		if err = os.WriteFile(filepath.Join(s.smbTmp, ".lober-owner"), []byte(s.owner), 0644); err != nil {
			return nil, err
		}
	}
	return roles, nil
}
func (s *importSession) Verify(ctx context.Context) error {
	for _, v := range s.opened {
		again, err := importDigest(ctx, v.file)
		if err != nil {
			return err
		}
		after, err := v.file.Stat()
		if err != nil {
			return err
		}
		if after.Size() != v.before.Size() || !after.ModTime().Equal(v.before.ModTime()) || again != v.digest {
			return &port.Fault{Kind: port.ContentMismatch, Role: v.role, Expected: v.digest, Actual: again}
		}
	}
	return nil
}
func (s *importSession) Publish(ctx context.Context) (port.Publication, error) {
	var err error
	if s.smbTmp != "" {
		s.published.MadeSMB, err = publishImportTree(s.smbTmp, s.smb, s.owner)
		if err != nil {
			return s.published, err
		}
	}
	s.published.MadeArtifacts, err = publishImportTree(s.tmp, s.root, s.owner)
	if err != nil {
		return s.published, err
	}
	rel, err := filepath.Rel(s.files.dirs.Artifacts, s.root)
	if err != nil {
		return s.published, err
	}
	s.published.Artifacts = map[string]string{}
	for role, name := range s.roles {
		s.published.Artifacts[role] = filepath.ToSlash(rel) + "/" + name
	}
	return s.published, nil
}
func (s *importSession) Rollback(ctx context.Context, p port.Publication) error {
	var errs []error
	// 只有本会话创建且仍归本会话所有的树才允许删除。
	for _, x := range []struct {
		made bool
		path string
	}{{p.MadeArtifacts && s.published.MadeArtifacts, s.root}, {p.MadeSMB && s.published.MadeSMB, s.smb}} {
		if !x.made {
			continue
		}
		b, err := os.ReadFile(filepath.Join(x.path, ".lober-owner"))
		if err != nil {
			errs = append(errs, err)
			continue
		}
		if string(b) != s.owner {
			errs = append(errs, fault(port.PublicationConflict))
			continue
		}
		errs = append(errs, os.RemoveAll(x.path))
	}
	return errors.Join(errs...)
}
func (s *importSession) Close() error {
	var errs []error
	for _, v := range s.opened {
		errs = append(errs, v.file.Close())
	}
	for _, p := range []string{s.tmp, s.smbTmp} {
		if p != "" {
			errs = append(errs, os.RemoveAll(p))
		}
	}
	return errors.Join(errs...)
}
