package iso

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/iso/iso9660"
	"github.com/lober/lober/internal/store"
	"io"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func importProblem(id string, code model.Code, p string, args ...any) error {
	return model.NewProblem(code, model.Subject{Kind: "iso", ID: id, Path: p}, args...)
}
func identifyForISO(ctx context.Context, id string, tree fs.FS) (Identified, error) {
	found, err := Identify(ctx, tree)
	var p model.Problem
	if errors.As(err, &p) {
		p.Subject.ID = id
		err = p
	}
	return found, err
}
func (s *Sources) artifactRoot(series catalog.Series, version string, arch catalog.Arch) string {
	return filepath.Join(s.dirs.Artifacts, string(series), version, string(arch))
}

// Import is synchronous. Immutable publication uses a temporary sibling on each
// destination filesystem; source, staging and SMB may be on different disks.
// Same-platform content belonging to another ISO is never overwritten.
func (s *Sources) Import(ctx context.Context, id string) (err error) {
	if err := store.CheckID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.lockSource(id, "@import")
	defer unlock()
	iso, err := s.store.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if iso.State == model.ISOReady {
		return store.ErrConflict
	}
	if iso.State == model.ISODeprecated {
		return store.ErrImmutable
	}
	srcs, err := s.store.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	var main store.Source
	extras := map[string]store.Source{}
	for _, src := range srcs {
		if src.Role == "" {
			main = src
		} else {
			extras[src.Role] = src
		}
	}
	if main.ISOID == "" || main.State != store.SourceFetched {
		return store.ErrConflict
	}
	for _, src := range srcs {
		if src.State != store.SourceFetched {
			return store.ErrConflict
		}
	}
	// Once bytes are complete, any content failure belongs to import, not fetch.
	publicationConflict := false
	defer func() {
		if err != nil && (!errors.Is(err, store.ErrConflict) || publicationConflict) && !errors.Is(err, store.ErrImmutable) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.failImport(ctx, id, err)
		}
	}()
	media, tree, before, err := s.openMedia(ctx, main)
	if err != nil {
		return err
	}
	defer media.Close()
	digest, err := importDigest(ctx, media)
	if err != nil {
		return err
	}
	if main.ExpectedSHA256 != "" && main.ExpectedSHA256 != digest {
		return importProblem(id, "iso_content_mismatch", "source", main.ExpectedSHA256, digest)
	}
	snapshots := []importMediaSnapshot{{file: media, before: before, digest: digest, role: main.Role}}
	found, err := identifyForISO(ctx, id, tree)
	if err != nil {
		return err
	}
	if found.Series != iso.Series {
		return importProblem(id, "iso_series_mismatch", "series", iso.Series, found.Series)
	}
	var extraTree fs.FS
	if need, ok := catalog.ExtraSourceOf(iso.Series); ok {
		extra, exists := extras[need.Role]
		if !exists {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		ef, et, eb, e := s.openMedia(ctx, extra)
		if e != nil {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		defer ef.Close()
		ed, e := importDigest(ctx, ef)
		if e != nil {
			return e
		}
		if extra.ExpectedSHA256 != "" && extra.ExpectedSHA256 != ed {
			return importProblem(id, "iso_content_mismatch", "source."+need.Role, extra.ExpectedSHA256, ed)
		}
		snapshots = append(snapshots, importMediaSnapshot{file: ef, before: eb, digest: ed, role: extra.Role})
		ei, e := identifyNetboot(ctx, et)
		if e != nil || ei.Series != found.Series || ei.Arch != found.Arch || need.SameVersion && ei.Version != found.Version {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		// Optical netinst initrds cannot be treated as network installer initrds.
		info, e := identifyText(ctx, et, ".disk/info")
		if e != nil || !strings.Contains(strings.ToUpper(info), "NETBOOT") {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		extraTree = et
	}
	iso.Version, iso.Arch, iso.SHA256, iso.State = found.Version, found.Arch, digest, model.ISOImporting
	if ps := iso.Validate(); len(ps) > 0 {
		return ps[0]
	}
	iso, err = s.store.UpdateISO(ctx, iso)
	if err != nil {
		return err
	} // claims SHA256 before any publication
	root := s.artifactRoot(iso.Series, iso.Version, iso.Arch)
	tmp := root + ".tmp-" + id
	if err = prepareImportTemp(tmp); err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	roles, err := s.extract(ctx, id, found, tree, extraTree, media, tmp)
	if err != nil {
		return err
	}
	for _, role := range catalog.RequiredRoles(iso.Series) {
		rel := strings.TrimSuffix(roles[role], "/")
		if rel == "" || !safeImportRel(rel) {
			return importProblem(id, "iso_artifact_missing", "artifacts."+role, role)
		}
		st, e := os.Stat(filepath.Join(tmp, filepath.FromSlash(rel)))
		if e != nil || !st.IsDir() && (!st.Mode().IsRegular() || st.Size() == 0) {
			return importProblem(id, "iso_artifact_missing", "artifacts."+role, role)
		}
	}
	owner := id + "\n" + digest + "\n"
	if err = os.WriteFile(filepath.Join(tmp, ".lober-owner"), []byte(owner), 0644); err != nil {
		return err
	}
	var smb, smbTmp string
	var madeSMB, made bool
	defer func() {
		if err != nil && (made || madeSMB) && s.rollbackImportAllowed(ctx, id, digest) {
			if made {
				os.RemoveAll(root)
			}
			if madeSMB {
				os.RemoveAll(smb)
			}
		}
	}()
	if iso.Family == "windows" {
		smb = filepath.Join(s.dirs.SMB, string(iso.Series), iso.Version, string(iso.Arch))
		smbTmp = smb + ".tmp-" + id
		if err = prepareImportTemp(smbTmp); err != nil {
			return err
		}
		defer os.RemoveAll(smbTmp)
		if err = copyImportTree(ctx, tree, smbTmp); err != nil {
			return err
		}
		if err = os.WriteFile(filepath.Join(smbTmp, ".lober-owner"), []byte(owner), 0644); err != nil {
			return err
		}
	}
	// Recheck every open source only after all consumers, including SMB copying,
	// have finished. Auxiliary and local media have the same integrity gate.
	for _, snapshot := range snapshots {
		if err = snapshot.verify(ctx, id); err != nil {
			return err
		}
	}
	if smbTmp != "" {
		madeSMB, err = publishImportTree(smbTmp, smb, owner)
		if err != nil {
			publicationConflict = errors.Is(err, store.ErrConflict)
			return err
		}
	}
	made, err = publishImportTree(tmp, root, owner)
	if err != nil {
		publicationConflict = errors.Is(err, store.ErrConflict)
		return err
	}
	relRoot, e := filepath.Rel(s.dirs.Artifacts, root)
	if e != nil {
		return e
	}
	relRoot = filepath.ToSlash(relRoot)
	iso.Artifacts = map[string]string{}
	for role, rel := range roles {
		iso.Artifacts[role] = relRoot + "/" + rel
	}
	if found.Editions != nil {
		iso.Offers = map[string][]model.Option{"editions": found.Editions}
	}
	// A successful reimport must not report the previous import error.
	main, err = s.getSource(ctx, id, "")
	if err != nil {
		return err
	}
	if main.Error != "" {
		main.Error = ""
		if _, err = s.store.SaveSource(ctx, main, store.Audit{}); err != nil {
			return err
		}
	}
	iso.State = model.ISOReady
	_, err = s.store.UpdateISO(ctx, iso)
	if err != nil {
		// A failed COMMIT acknowledgment does not prove rollback. Reconcile using
		// an uncancelled bounded read, and preserve files when the result is unknown.
		current, readErr := s.readImportOutcome(ctx, id)
		if readErr == nil && current.State == model.ISOReady && current.SHA256 == digest && maps.Equal(current.Artifacts, iso.Artifacts) {
			err = nil
		}
	}
	return err
}

type importMediaSnapshot struct {
	file         *os.File
	before       fs.FileInfo
	digest, role string
}

func (m importMediaSnapshot) verify(ctx context.Context, id string) error {
	again, err := importDigest(ctx, m.file)
	if err != nil {
		return err
	}
	after, err := m.file.Stat()
	if err != nil {
		return err
	}
	if after.Size() != m.before.Size() || !after.ModTime().Equal(m.before.ModTime()) || again != m.digest {
		p := "source"
		if m.role != "" {
			p += "." + m.role
		}
		return importProblem(id, "iso_content_mismatch", p, m.digest, again)
	}
	return nil
}
func (s *Sources) readImportOutcome(ctx context.Context, id string) (model.ISO, error) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.store.GetISO(readCtx, id)
}
func (s *Sources) rollbackImportAllowed(ctx context.Context, id, digest string) bool {
	current, err := s.readImportOutcome(ctx, id)
	return err == nil && current.SHA256 == digest && current.State != model.ISOReady && current.State != model.ISODeprecated
}

func (s *Sources) openMedia(ctx context.Context, src store.Source) (*os.File, fs.FS, fs.FileInfo, error) {
	var f *os.File
	var err error
	if src.Kind == store.SourceLocal {
		root, rel, ok := localRoot(s.dirs.LocalRoots, src.Path)
		if !ok {
			return nil, nil, nil, sourceRejected(src.ISOID, src.Role, isoSourceReasonLocal)
		}
		f, err = os.OpenInRoot(root, rel)
	} else {
		f, err = os.Open(s.sourcePath(src))
	}
	if err != nil {
		return nil, nil, nil, err
	}
	st, err := f.Stat()
	if err != nil || !st.Mode().IsRegular() {
		f.Close()
		if err == nil {
			err = store.ErrInvalid
		}
		return nil, nil, nil, err
	}
	if src.TotalSize > 0 && st.Size() != src.TotalSize {
		f.Close()
		p := "source"
		if src.Role != "" {
			p += "." + src.Role
		}
		return nil, nil, nil, importProblem(src.ISOID, "iso_content_mismatch", p, fmt.Sprintf("size %d", src.TotalSize), fmt.Sprintf("size %d", st.Size()))
	}
	tree, err := iso9660.Open(iso9660.FileReader{ReaderAt: f, Length: st.Size()})
	if err != nil {
		f.Close()
		return nil, nil, nil, errors.Join(importProblem(src.ISOID, "iso_media_unrecognized", "media"), err)
	}
	return f, tree, st, nil
}
func importDigest(ctx context.Context, f *os.File) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", err
	}
	h := sha256.New()
	if _, err := io.Copy(h, contextReader{ctx: ctx, r: f}); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func (s *Sources) failImport(ctx context.Context, id string, cause error) {
	current, e := s.store.GetISO(ctx, id)
	if e != nil || current.State == model.ISODeprecated || current.State == model.ISOReady {
		return
	}
	current.State = model.ISOFailed
	if _, e = s.store.UpdateISO(ctx, current); e != nil {
		return
	}
	src, ok, e := s.store.GetSource(ctx, id, "")
	if e != nil || !ok {
		return
	}
	text := cause.Error()
	if len(text) > 4096 || !model.StorableText(text) {
		text = "import failed"
	}
	src.Error = text
	s.store.SaveSource(ctx, src, store.Audit{})
}
func prepareImportTemp(p string) error {
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	if err := os.RemoveAll(p); err != nil {
		return err
	}
	return os.Mkdir(p, 0755)
}
func publishImportTree(tmp, root, owner string) (bool, error) {
	if st, err := os.Lstat(root); err == nil {
		if !st.IsDir() || st.Mode()&os.ModeSymlink != 0 {
			return false, store.ErrConflict
		}
		b, err := os.ReadFile(filepath.Join(root, ".lober-owner"))
		if err != nil || string(b) != owner {
			return false, fmt.Errorf("%w: artifact destination already belongs to other content", store.ErrConflict)
		}
		// A crash between rename and DB commit can reuse a complete immutable tree.
		same, err := equalImportTrees(tmp, root)
		if err != nil {
			return false, err
		}
		if !same {
			return false, fmt.Errorf("%w: existing artifact tree differs", store.ErrConflict)
		}
		return false, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return false, err
	}
	if err := os.Rename(tmp, root); err != nil {
		return false, err
	}
	return true, nil
}
func equalImportTrees(a, b string) (bool, error) {
	same := true
	countA, countB := 0, 0
	err := fs.WalkDir(os.DirFS(a), ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		countA++
		left, e := os.Lstat(filepath.Join(a, p))
		if e != nil {
			return e
		}
		right, e := os.Lstat(filepath.Join(b, p))
		if e != nil {
			if errors.Is(e, os.ErrNotExist) {
				same = false
				return nil
			}
			return e
		}
		if left.Mode().Type() != right.Mode().Type() || left.Size() != right.Size() && !left.IsDir() {
			same = false
			return nil
		}
		if left.Mode()&os.ModeSymlink != 0 {
			x, e := os.Readlink(filepath.Join(a, p))
			if e != nil {
				return e
			}
			y, e := os.Readlink(filepath.Join(b, p))
			if e != nil {
				return e
			}
			same = same && x == y
			return nil
		}
		if left.Mode().IsRegular() {
			x, e := os.Open(filepath.Join(a, p))
			if e != nil {
				return e
			}
			defer x.Close()
			y, e := os.Open(filepath.Join(b, p))
			if e != nil {
				return e
			}
			defer y.Close()
			hx, hy := sha256.New(), sha256.New()
			if _, e = io.Copy(hx, x); e != nil {
				return e
			}
			if _, e = io.Copy(hy, y); e != nil {
				return e
			}
			same = same && hex.EncodeToString(hx.Sum(nil)) == hex.EncodeToString(hy.Sum(nil))
		}
		return nil
	})
	if err != nil {
		return false, err
	}
	err = fs.WalkDir(os.DirFS(b), ".", func(_ string, _ fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		countB++
		return nil
	})
	return same && countA == countB, err
}
