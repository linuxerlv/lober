package store

import (
	"context"
	"github.com/lober/lober/internal/core/model"
	"math"
)

type SourceKind string

const (
	SourceRemote SourceKind = "remote"
	SourceUpload SourceKind = "upload"
	SourceLocal  SourceKind = "local"
)

type SourceState string

const (
	SourcePending   SourceState = "pending"
	SourceFetching  SourceState = "fetching"
	SourceFetched   SourceState = "fetched"
	SourceFailed    SourceState = "failed"
	SourceCancelled SourceState = "cancelled"
)

// Source 的键是 (ISOID,Role)，空 Role 为主介质。Rev 是仓储的乐观锁。
type Source struct {
	ISOID, Role    string
	Kind           SourceKind
	URL, Path      string
	TotalSize      int64
	ExpectedSHA256 string
	Fetched        int64
	State          SourceState
	Error          string
	Rev            int64
}

// Range 是已持久化的字节区间 [Start, Start+Length)。
type Range struct{ Start, Length int64 }

// ISOSources 存获取状态；所有带 Audit 的写入与审计一起提交，零值不审计。
type ISOSources interface {
	SaveSource(context.Context, Source, Audit) (Source, error)
	GetSource(context.Context, string, string) (Source, bool, error)
	SourcesOf(context.Context, string) ([]Source, error)
	ListUnfinishedSources(context.Context) ([]Source, error)
	DeleteSource(context.Context, string, string, Audit) error
	// SaveRange 在锁住来源的事务里合并区间、更新 Fetched 并递增 Rev。
	SaveRange(context.Context, string, string, int64, int64) error
	RangesOf(context.Context, string, string) ([]Range, error)
}

func CheckSourceKey(id, role string) error {
	if err := CheckID(id); err != nil {
		return err
	}
	// 安全词允许斜杠；角色将用作文件名段，因此用更严格的标识符。
	if role != "" && !model.ValidIdent(role) {
		return invalidf("invalid source role %q", role)
	}
	return nil
}
func CheckSource(s Source) error {
	if err := CheckSourceKey(s.ISOID, s.Role); err != nil {
		return err
	}
	if !model.StorableText(s.URL) || !model.StorableText(s.Path) || !model.StorableText(s.Error) || len(s.Error) > 4096 {
		return invalidf("source text invalid")
	}
	switch s.Kind {
	case SourceRemote:
		if s.URL == "" || s.Path != "" {
			return invalidf("remote needs only URL")
		}
	case SourceLocal:
		if s.Path == "" || s.URL != "" {
			return invalidf("local needs only Path")
		}
	case SourceUpload:
		if s.URL != "" || s.Path != "" {
			return invalidf("upload has no address")
		}
	default:
		return invalidf("invalid source kind %q", s.Kind)
	}
	if s.ExpectedSHA256 != "" && !lowerHexSHA256(s.ExpectedSHA256) {
		return invalidf("invalid source digest")
	}
	if s.TotalSize < 0 || s.Fetched < 0 || s.TotalSize > 0 && s.Fetched > s.TotalSize || s.Rev < 0 {
		return invalidf("invalid source counters")
	}
	switch s.State {
	case SourcePending, SourceFetching, SourceFetched, SourceFailed, SourceCancelled:
	default:
		return invalidf("invalid source state")
	}
	return nil
}
func CheckSourceTransition(from, to SourceState) error {
	valid := func(s SourceState) bool {
		switch s {
		case SourcePending, SourceFetching, SourceFetched, SourceFailed, SourceCancelled:
			return true
		}
		return false
	}
	allowed := valid(from) && valid(to) && (from == to || from == SourcePending && (to == SourceFetching || to == SourceFailed || to == SourceCancelled) || from == SourceFetching && (to == SourceFetched || to == SourceFailed || to == SourceCancelled) || from != SourcePending && from != SourceFetching && to == SourcePending)
	if !allowed {
		return ErrConflict
	}
	return nil
}
func CheckRange(start, length int64) error {
	if start < 0 || length <= 0 || start > math.MaxInt64-length {
		return invalidf("invalid byte range")
	}
	return nil
}
