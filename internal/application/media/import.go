package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"maps"
	"time"
)

func importProblem(id string, code model.Code, p string, args ...any) error {
	return model.NewProblem(code, model.Subject{Kind: "iso", ID: id, Path: p}, args...)
}
func identifyForISO(ctx context.Context, id string, tree port.MediaView) (Identified, error) {
	found, err := Identify(ctx, tree)
	var p model.Problem
	if errors.As(err, &p) {
		p.Subject.ID = id
		err = p
	}
	return found, err
}

// Import is synchronous. Immutable publication uses a temporary sibling on each
// destination filesystem; source, staging and SMB may be on different disks.
// Same-platform content belonging to another ISO is never overwritten.
func (s *Sources) Import(ctx context.Context, id string) (err error) {
	if err := repository.CheckID(id); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	unlock := s.locks.Lock(id, "@import")
	defer unlock()
	iso, err := s.isos.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if iso.State == model.ISOReady {
		return repository.ErrConflict
	}
	if iso.State == model.ISODeprecated {
		return repository.ErrImmutable
	}
	srcs, err := s.sources.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	var main model.Source
	extras := map[string]model.Source{}
	for _, src := range srcs {
		if src.Role == "" {
			main = src
		} else {
			extras[src.Role] = src
		}
	}
	if main.ISOID == "" || !mediarules.Fetched(srcs) {
		return repository.ErrConflict
	}
	// Once bytes are complete, any content failure belongs to import, not fetch.
	publicationConflict := false
	defer func() {
		if err != nil && (!errors.Is(err, repository.ErrConflict) || publicationConflict) && !errors.Is(err, repository.ErrImmutable) && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded) {
			s.failImport(ctx, id, err)
		}
	}()
	session, err := s.importFiles.Open(ctx, iso)
	if err != nil {
		return importFileError(id, err)
	}
	defer session.Close()
	tree, err := session.OpenSource(ctx, main)
	if err != nil {
		return importFileError(id, err)
	}
	digest, err := tree.Digest(ctx)
	if err != nil {
		return importFileError(id, err)
	}
	if main.ExpectedSHA256 != "" && main.ExpectedSHA256 != digest {
		return importProblem(id, "iso_content_mismatch", "source", main.ExpectedSHA256, digest)
	}
	found, err := identifyForISO(ctx, id, tree)
	if err != nil {
		return err
	}
	if err := mediarules.CheckImport(iso, found, srcs); err != nil {
		return err
	}
	if need, ok := catalog.ExtraSourceOf(iso.Series); ok {
		extra, exists := extras[need.Role]
		if !exists {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		et, e := session.OpenSource(ctx, extra)
		if e != nil {
			if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
				return e
			}
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		ed, e := et.Digest(ctx)
		if e != nil {
			return e
		}
		if extra.ExpectedSHA256 != "" && extra.ExpectedSHA256 != ed {
			return importProblem(id, "iso_content_mismatch", "source."+need.Role, extra.ExpectedSHA256, ed)
		}
		ei, e := identifyNetboot(ctx, et)
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		if e != nil || !mediarules.CheckExtra(found, ei, need.SameVersion, "NETBOOT") {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
		// Optical netinst initrds cannot be treated as network installer initrds.
		info, e := identifyText(ctx, et, ".disk/info")
		if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
			return e
		}
		if e != nil || !mediarules.CheckExtra(found, ei, need.SameVersion, info) {
			return importProblem(id, "iso_extra_source_invalid", "source."+need.Role, need.Role)
		}
	}
	iso.Version, iso.Arch, iso.SHA256, iso.State = found.Version, found.Arch, digest, model.ISOImporting
	if ps := iso.Validate(); len(ps) > 0 {
		return ps[0]
	}
	iso, err = s.isos.UpdateISO(ctx, iso)
	if err != nil {
		return err
	} // claims SHA256 before any publication
	plan := port.ExtractionPlan{ISO: iso, Identified: found, RepoPaths: catalog.RepoPaths(iso.Series), RequiredRoles: catalog.RequiredRoles(iso.Series)}
	if err = session.BeginPrepare(ctx, plan); err != nil {
		return importFileError(id, err)
	}
	if found.Format == "preseed" {
		if err = validateDebianRepository(ctx, found.Arch, tree); err != nil {
			return identifyErrorForISO(id, err)
		}
	}
	if _, err = session.Prepare(ctx, plan); err != nil {
		return importFileError(id, err)
	}
	// 所有消费者完成后，才复核每一份来源。
	if err = session.Verify(ctx); err != nil {
		return importFileError(id, err)
	}
	var published port.Publication
	defer func() {
		if err != nil && (published.MadeArtifacts || published.MadeSMB) && s.rollbackImportAllowed(ctx, id, digest) {
			_ = session.Rollback(context.WithoutCancel(ctx), published)
		}
	}()
	published, err = session.Publish(ctx)
	if err != nil {
		var fault *port.Fault
		publicationConflict = errors.As(err, &fault) && fault.Kind == port.PublicationConflict
		return importFileError(id, err)
	}
	iso.Artifacts = published.Artifacts
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
		if _, err = s.sources.SaveSource(ctx, main, repository.Audit{}); err != nil {
			return err
		}
	}
	iso.State = model.ISOReady
	_, err = s.isos.UpdateISO(ctx, iso)
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

func (s *Sources) readImportOutcome(ctx context.Context, id string) (model.ISO, error) {
	readCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	return s.isos.GetISO(readCtx, id)
}
func (s *Sources) rollbackImportAllowed(ctx context.Context, id, digest string) bool {
	current, err := s.readImportOutcome(ctx, id)
	return err == nil && current.SHA256 == digest && current.State != model.ISOReady && current.State != model.ISODeprecated
}

func (s *Sources) failImport(ctx context.Context, id string, cause error) {
	current, e := s.isos.GetISO(ctx, id)
	if e != nil || current.State == model.ISODeprecated || current.State == model.ISOReady {
		return
	}
	current.State = model.ISOFailed
	if _, e = s.isos.UpdateISO(ctx, current); e != nil {
		return
	}
	src, ok, e := s.sources.GetSource(ctx, id, "")
	if e != nil || !ok {
		return
	}
	text := cause.Error()
	if len(text) > 4096 || !model.StorableText(text) {
		text = "import failed"
	}
	src.Error = text
	s.sources.SaveSource(ctx, src, repository.Audit{})
}

func identifyErrorForISO(id string, err error) error {
	var p model.Problem
	if errors.As(err, &p) {
		p.Subject.ID = id
		return p
	}
	return err
}
