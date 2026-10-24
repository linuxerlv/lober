package port

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"io"
)

// Transfer 只操作字节，不读写业务记录。成功打开的会话由调用方关闭一次。
type Transfer interface {
	ValidateSource(context.Context, model.Source) (model.Source, error)
	BeginFetch(context.Context, model.Source) (FetchSession, error)
	BeginPublish(context.Context, model.Source) (FetchSession, error)
	BeginUpload(context.Context, model.Source, int64, io.Reader) (UploadSession, error)
	ParkRetry(context.Context, model.Source) (RetryFiles, error)
	FreeSpace(context.Context) (int64, int64, error)
}
type FetchInfo struct {
	Offset, TotalSize int64
	Complete, Reset   bool
}
type VerifiedContent struct {
	Size   int64
	SHA256 string
}

// Resume 和 Step 只报告已落盘长度；Open 返回 Reset 时，错误也携带需要保存的归零事实。
// Verify 后应用须重读版本才能 Publish；保存失败时 Restore 恢复断点。
type FetchSession interface {
	Resume(context.Context) (int64, error)
	Open(context.Context) (FetchInfo, error)
	Step(context.Context) (int64, bool, error)
	Verify(context.Context) (VerifiedContent, error)
	Publish(context.Context) error
	Restore(context.Context) error
	Close() error
}

// Size 是完整暂存且验证通过的实际片长，成功打开后固定，不使用请求声明长度。
// CheckOverlap 不改已确认字节；Write 只在来源状态提交成功后调用。
type UploadSession interface {
	Size() int64
	CheckOverlap(context.Context, []model.Range) error
	Write(context.Context) error
	VerifyReplay(context.Context) error
	Close() error
}
type RetryFiles interface {
	Restore() error
	Commit() error
}
type FaultKind string

const (
	URLRejected         FaultKind = "url_rejected"
	PathRejected        FaultKind = "path_rejected"
	ChunkRejected       FaultKind = "chunk_rejected"
	ContentMismatch     FaultKind = "content_mismatch"
	PublicationConflict FaultKind = "publication_conflict"
	InvalidFile         FaultKind = "invalid_file"
)

// Fault 是外部操作失败的事实。业务问题及状态由应用决定。
type Fault struct {
	Kind                   FaultKind
	Role, Expected, Actual string
	Cause                  error
}

func (f *Fault) Error() string {
	if f.Cause != nil {
		return fmt.Sprintf("%s: %v", f.Kind, f.Cause)
	}
	return string(f.Kind)
}
func (f *Fault) Unwrap() error { return f.Cause }
