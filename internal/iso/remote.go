package iso

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/store"
)

// parseContentRange accepts a complete trailing range, not a loosely parsed
// prefix. All lengths and offsets are bounded signed integers.
func parseContentRange(value string) (start, end, total int64, err error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, store.ErrInvalid
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 {
		return 0, 0, 0, store.ErrInvalid
	}
	span := strings.Split(parts[0], "-")
	if len(span) != 2 {
		return 0, 0, 0, store.ErrInvalid
	}
	start, err = rangeNumber(span[0])
	if err != nil {
		return
	}
	end, err = rangeNumber(span[1])
	if err != nil {
		return
	}
	total, err = rangeNumber(parts[1])
	if err != nil {
		return
	}
	if start < 0 || end < start || total <= end {
		err = store.ErrInvalid
	}
	return
}

func rangeNumber(value string) (int64, error) {
	if value == "" {
		return 0, store.ErrInvalid
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, store.ErrInvalid
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

func (s *Sources) fetchRemote(ctx context.Context, src store.Source) error {
	src, offset, err := s.resumeOffset(ctx, src)
	if err != nil {
		return err
	}
	// A byte range alone does not identify the remote entity. Without a supplied
	// content digest, restart rather than combine a stale prefix with a new tail.
	// Digest-bound resumes are verified again before publication.
	if offset > 0 && src.ExpectedSHA256 == "" {
		offset, src.Fetched = 0, 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, src.URL, nil)
	if err != nil {
		return err
	}
	// Disable transparent decompression: byte offsets refer to the media itself.
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		var rejected *store.ProblemsError
		if errors.As(err, &rejected) {
			removeErr := os.Remove(s.stagingPath(src.ISOID, src.Role))
			if removeErr != nil && !os.IsNotExist(removeErr) {
				return errors.Join(err, removeErr)
			}
			src.Fetched = 0
			_, saveErr := s.store.SaveSource(ctx, src, store.Audit{})
			return errors.Join(sourceRejected(src.ISOID, src.Role, isoSourceReasonURL), saveErr)
		}
		return err
	}
	defer resp.Body.Close()
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
	}
	switch resp.StatusCode {
	case http.StatusRequestedRangeNotSatisfiable:
		value := resp.Header.Get("Content-Range")
		if !strings.HasPrefix(value, "bytes */") {
			return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
		}
		total, e := rangeNumber(strings.TrimPrefix(value, "bytes */"))
		if e != nil || total <= 0 || total != offset || src.TotalSize > 0 && total != src.TotalSize {
			return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
		}
		src.TotalSize = total
		src, err = s.store.SaveSource(ctx, src, store.Audit{})
		if err != nil {
			return err
		}
		return s.finishFetch(ctx, src)
	case http.StatusPartialContent:
		start, end, total, e := parseContentRange(resp.Header.Get("Content-Range"))
		if e != nil || start != offset || end != total-1 || resp.ContentLength >= 0 && resp.ContentLength != end-start+1 || src.TotalSize > 0 && src.TotalSize != total {
			return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
		}
		src.TotalSize = total
	case http.StatusOK:
		offset = 0
		src.Fetched = 0
		src.TotalSize = resp.ContentLength
		if src.TotalSize < 0 {
			src.TotalSize = 0
		}
	default:
		return fmt.Errorf("fetch source: HTTP %d", resp.StatusCode)
	}
	if err = os.MkdirAll(s.dirs.Staging, 0700); err != nil {
		return err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	f, err := os.OpenFile(s.stagingPath(src.ISOID, src.Role), flags, 0600)
	if err != nil {
		return err
	}
	defer f.Close()
	if _, err = f.Seek(offset, 0); err != nil {
		return err
	}
	if err = f.Sync(); err != nil {
		return err
	}
	src, err = s.store.SaveSource(ctx, src, store.Audit{})
	if err != nil {
		return err
	}
	src, err = s.writeFetch(ctx, src, f, resp.Body, offset)
	if err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if src.TotalSize > 0 && src.Fetched != src.TotalSize {
		return sourceRejected(src.ISOID, src.Role, isoSourceReasonChunk)
	}
	return s.finishFetch(ctx, src)
}
