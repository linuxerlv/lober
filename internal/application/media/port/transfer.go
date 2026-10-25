package port

import (
	"context"
	"fmt"
	"github.com/lober/lober/internal/domain/model"
	"io"
)

// Transfer 只操作字节，不读写业务记录。成功打开的会话由调用方关闭一次。
type Transfer interface {
	// ValidateSource 的八项能力约定唯一连接 docs/contracts/media.md 第10节「Transfer.ValidateSource」行及该节共同边界。
	ValidateSource(context.Context, model.Source) (model.Source, error)
	// BeginFetch 的八项能力约定唯一连接 docs/contracts/media.md 第10节「Transfer.BeginFetch」行及该节共同边界。
	BeginFetch(context.Context, model.Source) (FetchSession, error)
	// BeginPublish 的八项能力约定唯一连接 docs/contracts/media.md 第10节「Transfer.BeginPublish」行及该节共同边界。
	BeginPublish(context.Context, model.Source) (FetchSession, error)
	// BeginUpload 的八项能力约定唯一连接 docs/contracts/media.md 第10节「Transfer.BeginUpload」行及该节共同边界。
	BeginUpload(context.Context, model.Source, int64, io.Reader) (UploadSession, error)
	// ParkRetry 的八项能力约定唯一连接 docs/contracts/media.md 第10节「Transfer.ParkRetry」行及该节共同边界。
	ParkRetry(context.Context, model.Source) (RetryFiles, error)
	// FreeSpace 的八项能力约定唯一连接 docs/contracts/media.md 第10节「Transfer.FreeSpace」行及该节共同边界。
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
	// Resume 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Resume」行及该节共同边界。
	Resume(context.Context) (int64, error)
	// Open 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Open」行及该节共同边界。
	Open(context.Context) (FetchInfo, error)
	// Step 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Step」行及该节共同边界。
	Step(context.Context) (int64, bool, error)
	// Verify 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Verify」行及该节共同边界。
	Verify(context.Context) (VerifiedContent, error)
	// Publish 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Publish」行及该节共同边界。
	Publish(context.Context) error
	// Restore 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Restore」行及该节共同边界。
	Restore(context.Context) error
	// Close 的八项能力约定唯一连接 docs/contracts/media.md 第10节「FetchSession.Close」行及该节共同边界。
	Close() error
}

// Size 是完整暂存且验证通过的实际片长，成功打开后固定，不使用请求声明长度。
// CheckOverlap 不改已确认字节；Write 只在来源状态提交成功后调用。
type UploadSession interface {
	// Size 的八项能力约定唯一连接 docs/contracts/media.md 第10节「UploadSession.Size」行及该节共同边界。
	Size() int64
	// CheckOverlap 的八项能力约定唯一连接 docs/contracts/media.md 第10节「UploadSession.CheckOverlap」行及该节共同边界。
	CheckOverlap(context.Context, []model.Range) error
	// Write 的八项能力约定唯一连接 docs/contracts/media.md 第10节「UploadSession.Write」行及该节共同边界。
	Write(context.Context) error
	// VerifyReplay 的八项能力约定唯一连接 docs/contracts/media.md 第10节「UploadSession.VerifyReplay」行及该节共同边界。
	VerifyReplay(context.Context) error
	// Close 的八项能力约定唯一连接 docs/contracts/media.md 第10节「UploadSession.Close」行及该节共同边界。
	Close() error
}
type RetryFiles interface {
	// Restore 的八项能力约定唯一连接 docs/contracts/media.md 第10节「RetryFiles.Restore」行及该节共同边界。
	Restore() error
	// Commit 的八项能力约定唯一连接 docs/contracts/media.md 第10节「RetryFiles.Commit」行及该节共同边界。
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

// Unwrap 供标准错误检查读取原Cause；无输入校验或新错误，nil Cause原样返回。
// 只读错误值，重复同值相同；无事务、冲突、提交复查、审计、通知、上下文或超时。
func (f *Fault) Unwrap() error { return f.Cause }
