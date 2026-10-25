package iso

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/application/media/port"
	"net/http"
	"os"
	"strconv"
	"strings"
)

// parseContentRange accepts a complete trailing range, not a loosely parsed
// prefix. All lengths and offsets are bounded signed integers.
func parseContentRange(value string) (start, end, total int64, err error) {
	if !strings.HasPrefix(value, "bytes ") {
		return 0, 0, 0, fault(port.InvalidFile)
	}
	parts := strings.Split(strings.TrimPrefix(value, "bytes "), "/")
	if len(parts) != 2 {
		return 0, 0, 0, fault(port.InvalidFile)
	}
	span := strings.Split(parts[0], "-")
	if len(span) != 2 {
		return 0, 0, 0, fault(port.InvalidFile)
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
		err = fault(port.InvalidFile)
	}
	return
}

func rangeNumber(value string) (int64, error) {
	if value == "" {
		return 0, fault(port.InvalidFile)
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0, fault(port.InvalidFile)
		}
	}
	return strconv.ParseInt(value, 10, 64)
}

func (s *fetchSession) openRemote(ctx context.Context) (port.FetchInfo, error) {
	offset := s.offset
	if offset > 0 && s.src.ExpectedSHA256 == "" {
		offset = 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, s.src.URL, nil)
	if err != nil {
		return port.FetchInfo{}, err
	}
	req.Header.Set("Accept-Encoding", "identity")
	if offset > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", offset))
	}
	resp, err := s.transfer.httpClient().Do(req)
	if err != nil {
		var f *port.Fault
		if errors.As(err, &f) && f.Kind == port.URLRejected {
			removeErr := os.Remove(s.transfer.stagingPath(s.src.ISOID, s.src.Role))
			if removeErr != nil && !os.IsNotExist(removeErr) {
				return port.FetchInfo{}, errors.Join(err, removeErr)
			}
			s.offset = 0
			return port.FetchInfo{Offset: 0, TotalSize: s.total, Reset: true}, fault(port.URLRejected)
		}
		return port.FetchInfo{}, err
	}
	s.body = resp.Body
	if encoding := resp.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return port.FetchInfo{}, fault(port.ChunkRejected)
	}
	switch resp.StatusCode {
	case http.StatusRequestedRangeNotSatisfiable:
		value := resp.Header.Get("Content-Range")
		if !strings.HasPrefix(value, "bytes */") {
			return port.FetchInfo{}, fault(port.ChunkRejected)
		}
		total, e := rangeNumber(strings.TrimPrefix(value, "bytes */"))
		if e != nil || total <= 0 || total != offset || s.total > 0 && total != s.total {
			return port.FetchInfo{}, fault(port.ChunkRejected)
		}
		s.offset = offset
		s.total = total
		return port.FetchInfo{Offset: offset, TotalSize: total, Complete: true}, nil
	case http.StatusPartialContent:
		start, end, total, e := parseContentRange(resp.Header.Get("Content-Range"))
		if e != nil || start != offset || end != total-1 || resp.ContentLength >= 0 && resp.ContentLength != end-start+1 || s.total > 0 && s.total != total {
			return port.FetchInfo{}, fault(port.ChunkRejected)
		}
		s.total = total
	case http.StatusOK:
		offset = 0
		s.total = max(resp.ContentLength, 0)
	default:
		return port.FetchInfo{}, fmt.Errorf("fetch source: HTTP %d", resp.StatusCode)
	}
	if err = os.MkdirAll(s.transfer.dirs.Staging, 0700); err != nil {
		return port.FetchInfo{}, err
	}
	flags := os.O_CREATE | os.O_WRONLY
	if offset == 0 {
		flags |= os.O_TRUNC
	}
	s.output, err = os.OpenFile(s.transfer.stagingPath(s.src.ISOID, s.src.Role), flags, 0600)
	if err != nil {
		return port.FetchInfo{}, err
	}
	if _, err = s.output.Seek(offset, 0); err != nil {
		return port.FetchInfo{}, err
	}
	if err = s.output.Sync(); err != nil {
		return port.FetchInfo{}, err
	}
	s.offset = offset
	return port.FetchInfo{Offset: offset, TotalSize: s.total, Reset: offset == 0}, nil
}
