package media

import (
	"errors"
	"io"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/repository"
)

const (
	sourceFailed         = "镜像来源处理失败"
	sourceInvalid        = "镜像来源不合法"
	sourceDigestMismatch = "镜像内容与登记的校验值不符"
	sourceInterrupted    = "镜像下载中断"
	sourceUnavailable    = "无法下载镜像"
	sourceHTTPStatus     = "镜像服务器返回 HTTP "
)

type remoteFetchError struct{ status int }

func (e remoteFetchError) Error() string {
	if e.status >= 100 && e.status <= 599 {
		return sourceHTTPStatus + strconv.Itoa(e.status)
	}
	return sourceUnavailable
}

func sourceFailureMessage(cause error) string {
	var remote remoteFetchError
	if errors.As(cause, &remote) {
		return remote.Error()
	}
	var failure *repository.ProblemsError
	if errors.As(cause, &failure) && len(failure.Problems) == 1 {
		switch failure.Problems[0].Code {
		case "iso_source_rejected":
			return sourceInvalid
		case "iso_content_mismatch":
			return sourceDigestMismatch
		}
	}
	if errors.Is(cause, io.ErrUnexpectedEOF) {
		return sourceInterrupted
	}
	return sourceFailed
}

// 旧记录可能包含原始链接。进度只展示固定文案和合法的数字状态码。
func publicSourceError(stored string) string {
	switch stored {
	case "", sourceFailed, sourceInvalid, sourceDigestMismatch, sourceInterrupted, sourceUnavailable:
		return stored
	}
	if suffix, ok := strings.CutPrefix(stored, sourceHTTPStatus); ok && len(suffix) == 3 {
		if status, err := strconv.Atoi(suffix); err == nil && status >= 100 && status <= 599 {
			return remoteFetchError{status: status}.Error()
		}
	}
	return sourceFailed
}
