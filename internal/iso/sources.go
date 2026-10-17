package iso

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

// Register records pending work. The assembly point starts remote/local work
// with FetchOne, or awaits Upload calls; this package owns no background workers.
func (s *Sources) Register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a store.Audit) error {
	return s.register(ctx, item, kind, addr, role, size, digest, a, false)
}

// RegisterQueued also persists acquisition scheduling for a failed ISO.
// Register preserves the existing synchronous caller's reimport workflow.
func (s *Sources) RegisterQueued(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a store.Audit) error {
	return s.register(ctx, item, kind, addr, role, size, digest, a, true)
}

func (s *Sources) register(ctx context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a store.Audit, queue bool) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	if err := checkSourceKey(item.ID, role); err != nil {
		return err
	}
	current, err := s.store.GetISO(ctx, item.ID)
	if err != nil {
		return err
	}
	if current.State != model.ISOAcquiring && !(current.State == model.ISOFailed && role != "") {
		return store.ErrConflict
	}
	if role != "" {
		extra, ok := catalog.ExtraSourceOf(current.Series)
		if !ok || role != extra.Role {
			return sourceRejected(item.ID, role, isoSourceReasonChunk)
		}
		if current.State == model.ISOFailed {
			main, err := s.getSource(ctx, item.ID, "")
			if err != nil {
				return err
			}
			if main.State != store.SourceFetched {
				return store.ErrConflict
			}
		}
	}
	src := store.Source{ISOID: item.ID, Role: role, State: store.SourcePending, TotalSize: size, ExpectedSHA256: digest}
	switch kind {
	case "url":
		src.Kind = store.SourceRemote
		src.URL = addr
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, addr, nil)
		if err != nil {
			return sourceRejected(item.ID, role, isoSourceReasonURL)
		}
		ips, err := s.lookup(ctx, req.URL.Hostname())
		if err != nil {
			return err
		}
		if err = CheckURL(req.URL, ips); err != nil {
			return sourceRejected(item.ID, role, isoSourceReasonURL)
		}
	case "path":
		src.Kind = store.SourceLocal
		if err := CheckLocalPath(s.dirs.LocalRoots, addr); err != nil {
			return sourceRejected(item.ID, role, isoSourceReasonLocal)
		}
		src.Path, err = resolvedPath(addr)
		if err != nil {
			return err
		}
	case "upload":
		src.Kind = store.SourceUpload
		if addr != "" || size <= 0 {
			return sourceRejected(item.ID, role, isoSourceReasonChunk)
		}
	default:
		return sourceRejected(item.ID, role, isoSourceReasonChunk)
	}
	if err = store.CheckSource(src); err != nil {
		return err
	}
	unlock := s.lockSource(item.ID, role)
	defer unlock()
	if _, exists, err := s.store.GetSource(ctx, item.ID, role); err != nil {
		return err
	} else if exists {
		return store.ErrConflict
	}
	if queue && current.State == model.ISOFailed {
		_, err = s.store.SaveSourceAndAcquire(ctx, src, current.Rev, a)
	} else {
		_, err = s.store.SaveSource(ctx, src, a)
	}
	return err
}

// Cancel persists immediately without waiting on a file lock held by a blocked
// HTTP request. It then cancels any current instance-owned acquisition context.
func (s *Sources) Cancel(ctx context.Context, id string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	item, err := s.store.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if item.State != model.ISOAcquiring && item.State != model.ISOFailed {
		return store.ErrConflict
	}
	sources, err := s.store.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	for _, src := range sources {
		for attempts := 0; attempts < 16; attempts++ {
			if src.State != store.SourcePending && src.State != store.SourceFetching {
				break
			}
			src.State = store.SourceCancelled
			_, err = s.store.SaveSource(ctx, src, a)
			if err == nil {
				if cancel, ok := s.active.Load(id + "/" + src.Role); ok {
					cancel.(context.CancelFunc)()
				}
				break
			}
			if !errors.Is(err, store.ErrConflict) {
				return err
			}
			src, err = s.getSource(ctx, id, src.Role)
			if err != nil {
				return err
			}
			if attempts == 15 {
				return store.ErrConflict
			}
		}
	}
	return nil
}

func (s *Sources) Retry(ctx context.Context, id string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	item, err := s.store.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if item.State != model.ISOAcquiring && item.State != model.ISOFailed {
		return store.ErrConflict
	}
	sources, err := s.store.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	for _, src := range sources {
		if src.State != store.SourceFailed && src.State != store.SourceCancelled {
			continue
		}
		unlock := s.lockSource(id, src.Role)
		err = func() error {
			current, err := s.getSource(ctx, id, src.Role)
			if err != nil {
				return err
			}
			if current.State != store.SourceFailed && current.State != store.SourceCancelled {
				return store.ErrConflict
			}
			return s.retrySource(ctx, current, a)
		}()
		unlock()
		if err != nil {
			return err
		}
	}
	return nil
}

// Temporarily park terminal-source bytes before resetting the ledger. Audit
// failure restores them; a committed reset deletes the parked copies. Local
// originals remain untouched. This avoids claiming stale progress after an
// audit rollback and prevents old bytes being resumed after a successful retry.
func (s *Sources) retrySource(ctx context.Context, src store.Source, a store.Audit) error {
	type parkedFile struct{ original, backup string }
	var parked []parkedFile
	restore := func() error {
		var errs []error
		for i := len(parked) - 1; i >= 0; i-- {
			if err := os.Rename(parked[i].backup, parked[i].original); err != nil {
				errs = append(errs, err)
			}
		}
		return errors.Join(errs...)
	}
	if src.Kind != store.SourceLocal {
		for _, path := range []string{s.stagingPath(src.ISOID, src.Role), s.sourcePath(src)} {
			st, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil {
				return errors.Join(err, restore())
			}
			if !st.Mode().IsRegular() {
				return errors.Join(store.ErrInvalid, restore())
			}
			temp, err := os.CreateTemp(s.dirs.Staging, ".retry-"+src.ISOID+"-")
			if err != nil {
				return errors.Join(err, restore())
			}
			backup := temp.Name()
			if err = temp.Close(); err != nil {
				os.Remove(backup)
				return errors.Join(err, restore())
			}
			if err = os.Remove(backup); err != nil {
				return errors.Join(err, restore())
			}
			if err = os.Rename(path, backup); err != nil {
				return errors.Join(err, restore())
			}
			parked = append(parked, parkedFile{original: path, backup: backup})
		}
	}
	src.State = store.SourcePending
	src.Fetched = 0
	src.Error = ""
	if _, err := s.store.SaveSource(ctx, src, a); err != nil {
		return errors.Join(err, restore())
	}
	var errs []error
	for _, file := range parked {
		if err := os.Remove(file.backup); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

func (s *Sources) Reimport(ctx context.Context, id string, a store.Audit) error {
	if err := s.RequestReimport(ctx, id, a); err != nil {
		return err
	}
	return s.Import(ctx, id)
}

// RequestReimport persists accepted work before returning. Recover can resume
// it after a restart; the assembly point invokes Import in its own worker.
// Reimport retains the synchronous API for existing callers.
func (s *Sources) RequestReimport(ctx context.Context, id string, a store.Audit) error {
	if err := store.CheckAudit(a); err != nil {
		return err
	}
	item, err := s.store.GetISO(ctx, id)
	if err != nil {
		return err
	}
	if item.State != model.ISOFailed {
		return store.ErrConflict
	}
	sources, err := s.store.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	if !allFetched(item, sources) {
		return store.ErrConflict
	}
	// Main media is the stable request carrier; queue and audit commit together.
	for _, src := range sources {
		if src.Role == "" {
			_, err = s.store.SaveSourceAndAcquire(ctx, src, item.Rev, a)
			return err
		}
	}
	err = store.ErrConflict
	return err
}

type Progress struct {
	State   model.ISOState
	Sources []SourceProgress
}
type SourceProgress struct {
	Role      string
	Kind      store.SourceKind
	State     store.SourceState
	Fetched   int64
	TotalSize int64
	Missing   []store.Range
	Error     string
}

func (s *Sources) Progress(ctx context.Context, id string) (Progress, error) {
	item, err := s.store.GetISO(ctx, id)
	if err != nil {
		return Progress{}, err
	}
	sources, err := s.store.SourcesOf(ctx, id)
	if err != nil {
		return Progress{}, err
	}
	out := Progress{State: item.State, Sources: make([]SourceProgress, 0, len(sources))}
	for _, src := range sources {
		p := SourceProgress{Role: src.Role, Kind: src.Kind, State: src.State, Fetched: src.Fetched, TotalSize: src.TotalSize, Error: src.Error}
		if src.State != store.SourceFetched && src.TotalSize > 0 {
			if src.Kind == store.SourceUpload {
				ranges, err := s.store.RangesOf(ctx, id, src.Role)
				if err != nil {
					return Progress{}, err
				}
				pos := int64(0)
				for _, span := range ranges {
					if span.Start > pos {
						p.Missing = append(p.Missing, store.Range{Start: pos, Length: span.Start - pos})
					}
					pos = max(pos, span.Start+span.Length)
				}
				if pos < src.TotalSize {
					p.Missing = append(p.Missing, store.Range{Start: pos, Length: src.TotalSize - pos})
				}
			} else if src.Fetched < src.TotalSize {
				p.Missing = []store.Range{{Start: src.Fetched, Length: src.TotalSize - src.Fetched}}
			}
		}
		out.Sources = append(out.Sources, p)
	}
	return out, nil
}

func (s *Sources) FreeSpace(ctx context.Context) (staging, artifacts int64, err error) {
	staging, err = freeSpace(ctx, s.dirs.Staging)
	if err != nil {
		return 0, 0, err
	}
	artifacts, err = freeSpace(ctx, s.dirs.Artifacts)
	return
}

// Free space uses platform utilities instead of ignored build-tag files, so the
// architecture guard checks every source file on every supported platform.
func freeSpace(ctx context.Context, p string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return 0, fmt.Errorf("free space %s: %w", p, err)
	}
	if !st.IsDir() {
		return 0, fmt.Errorf("free space %s: not a directory", p)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.CommandContext(ctx, "df", "--output=avail", "-B1", "--", p)
	case "windows":
		absolute, err := filepath.Abs(p)
		if err != nil {
			return 0, err
		}
		literal := "'" + strings.ReplaceAll(absolute, "'", "''") + "'"
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; [System.IO.DriveInfo]::new([System.IO.Path]::GetPathRoot("+literal+")).AvailableFreeSpace")
	default:
		return 0, fmt.Errorf("free space %s: unsupported platform %s", p, runtime.GOOS)
	}
	data, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("free space %s: %w", p, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("free space %s: empty utility output", p)
	}
	n, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("free space %s: invalid utility output", p)
	}
	return n, nil
}
