package repository

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"math"
)

// ISOSources 存获取状态；所有带 Audit 的写入与审计一起提交，零值不审计。
type ISOSources interface {
	SaveSource(context.Context, model.Source, Audit) (model.Source, error)
	// SaveSourceAndAcquire atomically records a failed ISO's source request,
	// acquiring state and audit. isoRev is the caller's expected ISO revision.
	SaveSourceAndAcquire(context.Context, model.Source, int64, Audit) (model.Source, error)
	GetSource(context.Context, string, string) (model.Source, bool, error)
	SourcesOf(context.Context, string) ([]model.Source, error)
	ListUnfinishedSources(context.Context) ([]model.Source, error)
	DeleteSource(context.Context, string, string, Audit) error
	// SaveRange 在锁住来源的事务里合并区间、更新 Fetched 并递增 Rev。
	SaveRange(context.Context, string, string, int64, int64) error
	RangesOf(context.Context, string, string) ([]model.Range, error)
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
func CheckSource(s model.Source) error {
	if err := CheckSourceKey(s.ISOID, s.Role); err != nil {
		return err
	}
	if !model.StorableText(s.URL) || !model.StorableText(s.Path) || !model.StorableText(s.Error) || len(s.Error) > 4096 {
		return invalidf("source text invalid")
	}
	switch s.Kind {
	case model.SourceRemote:
		if s.URL == "" || s.Path != "" {
			return invalidf("remote needs only URL")
		}
	case model.SourceLocal:
		if s.Path == "" || s.URL != "" {
			return invalidf("local needs only Path")
		}
	case model.SourceUpload:
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
	case model.SourcePending, model.SourceFetching, model.SourceFetched, model.SourceFailed, model.SourceCancelled:
	default:
		return invalidf("invalid source state")
	}
	return nil
}
func CheckSourceTransition(from, to model.SourceState) error {
	valid := func(s model.SourceState) bool {
		switch s {
		case model.SourcePending, model.SourceFetching, model.SourceFetched, model.SourceFailed, model.SourceCancelled:
			return true
		}
		return false
	}
	allowed := valid(from) && valid(to) && (from == to || from == model.SourcePending && (to == model.SourceFetching || to == model.SourceFailed || to == model.SourceCancelled) || from == model.SourceFetching && (to == model.SourceFetched || to == model.SourceFailed || to == model.SourceCancelled) || from != model.SourcePending && from != model.SourceFetching && to == model.SourcePending)
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
