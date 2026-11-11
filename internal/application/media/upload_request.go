package media

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"io"
	"math"
)

// exactChunkReader 在暂存正文时检查真实长度，不缓冲整片，失败仍返回普通非法输入。
type exactChunkReader struct {
	r         io.Reader
	remaining int64
}

func (r *exactChunkReader) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	if r.remaining == 0 {
		var probe [1]byte
		n, err := r.r.Read(probe[:])
		if n > 0 {
			return 0, repository.ErrInvalid
		}
		return 0, err
	}
	if int64(len(p)) > r.remaining {
		p = p[:int(r.remaining)]
	}
	n, err := r.r.Read(p)
	r.remaining -= int64(n)
	if errors.Is(err, io.EOF) && r.remaining > 0 {
		return n, repository.ErrInvalid
	}
	return n, err
}

// Upload 先来源键、存在性、种类，再预检范围与声明长度，最后流式读取。分片不读时钟、不写用户审计。
// 已确认字节和取消版本的竞争仍由原控制能力处理，相同完成分片可重传。
func (s *registry) Upload(ctx context.Context, a ports.Actor, r UploadRequest) error {
	if err := a.Authorize(access.PermISOWrite); err != nil {
		return err
	}
	if err := repository.CheckSourceKey(r.ID, r.Role); err != nil {
		return err
	}
	source, exists, err := s.deps.Sources.GetSource(ctx, r.ID, r.Role)
	if err != nil {
		return err
	}
	if !exists {
		return repository.ErrNotFound
	}
	if source.Kind != model.SourceUpload {
		return repository.ErrConflict
	}
	if r.Start < 0 || r.End < r.Start || r.End == math.MaxInt64 {
		return &RangeError{}
	}
	length := r.End - r.Start + 1
	if r.Total < -1 || r.End >= source.TotalSize || r.Total != -1 && r.Total != source.TotalSize || r.ContentLength >= 0 && r.ContentLength != length {
		return &RangeError{}
	}
	if r.Body == nil {
		return repository.ErrInvalid
	}
	return s.deps.Control.Upload(ctx, r.ID, r.Role, r.Start, &exactChunkReader{r: r.Body, remaining: length}, repository.Audit{})
}
