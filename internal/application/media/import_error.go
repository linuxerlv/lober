package media

import (
	"errors"
	"strings"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

const importFailed = "镜像导入失败"

var importFailureMessages = map[model.Code]string{
	"iso_artifact_missing":     "安装镜像缺少必需文件",
	"iso_content_mismatch":     sourceDigestMismatch,
	"iso_extra_source_invalid": "附加镜像缺失、不完整或与主镜像不匹配",
	"iso_incomplete":           "安装镜像的信息不完整",
	"iso_media_unrecognized":   "无法识别安装镜像的格式",
	"iso_repo_incomplete":      "安装镜像的仓库不完整",
	"iso_series_mismatch":      "安装镜像的系统类型与登记不一致",
	"iso_series_unrecognized":  "无法识别安装镜像的系统类型",
	"iso_source_rejected":      sourceInvalid,
	"iso_tool_missing":         "服务端缺少导入 Windows 镜像的工具",
	"iso_version_missing":      "无法识别安装镜像的版本或架构",
	"iso_wim_compressed":       "Windows 安装镜像使用了不支持的压缩方式",
}

func importFailureMessage(cause error) string {
	var problem model.Problem
	if errors.As(cause, &problem) {
		if message := importFailureMessages[problem.Code]; message != "" {
			return message
		}
	}
	var failure *repository.ProblemsError
	if errors.As(cause, &failure) {
		for _, problem := range failure.Problems {
			if message := importFailureMessages[problem.Code]; message != "" {
				return message
			}
		}
	}
	return importFailed
}

func publicImportError(stored string) string {
	for _, message := range importFailureMessages {
		if stored == message {
			return message
		}
	}
	// 旧版从问题代码开头保存完整错误。只认代码，后面的路径和参数不公开。
	if end := strings.IndexAny(stored, "（："); end >= 0 {
		if message := importFailureMessages[model.Code(stored[:end])]; message != "" {
			return message
		}
	}
	return importFailed
}

func importFileError(id string, err error) error {
	mapped, _ := importFileErrorTree(id, err)
	return mapped
}
func importFileErrorTree(id string, err error) (error, bool) {
	if err == nil {
		return nil, false
	}
	switch wrapped := err.(type) {
	case interface{ Unwrap() []error }:
		causes := wrapped.Unwrap()
		mapped := make([]error, len(causes))
		changed := false
		for i, cause := range causes {
			var replaced bool
			mapped[i], replaced = importFileErrorTree(id, cause)
			changed = changed || replaced
		}
		if changed {
			return errors.Join(mapped...), true
		}
		return err, false
	case interface{ Unwrap() error }:
		// 已知外部故障隐藏内部原因；未知包装保留。独立错误分支仍由上面逐个处理。
		if _, direct := err.(*port.Fault); !direct {
			if mapped, changed := importFileErrorTree(id, wrapped.Unwrap()); changed {
				return mapped, true
			}
			return err, false
		}
	}
	switch err {
	case port.ErrMediaMalformed:
		return importProblem(id, "iso_media_unrecognized", "media"), true
	case port.ErrTextTooLarge, port.ErrWIMMalformed:
		return importProblem(id, "iso_version_missing", "version", model.ISOVersionLabel), true
	case port.ErrWIMCompressed:
		return importProblem(id, "iso_wim_compressed", "media"), true
	}
	if f, ok := err.(*port.Fault); ok {
		switch f.Kind {
		case port.PathRejected:
			return sourceRejected(id, f.Role, isoSourceReasonLocal), true
		case port.InvalidFile:
			return repository.ErrInvalid, true
		case port.ContentMismatch:
			p := "source"
			if f.Role != "" {
				p += "." + f.Role
			}
			return importProblem(id, "iso_content_mismatch", p, f.Expected, f.Actual), true
		case port.PublicationConflict:
			return repository.ErrConflict, true
		}
	}
	if f, ok := err.(*port.ExtractionFault); ok {
		switch f.Kind {
		case "artifact":
			return importProblem(id, "iso_artifact_missing", "artifacts."+f.Name, f.Name), true
		case "repository":
			return importProblem(id, "iso_repo_incomplete", "artifacts.repo", f.Name), true
		case "extra":
			return importProblem(id, "iso_extra_source_invalid", "source."+f.Name, f.Name), true
		case "tool":
			return importProblem(id, "iso_tool_missing", "tools."+f.Name, f.Name), true
		case "format":
			return importProblem(id, "iso_version_missing", "version", f.Name), true
		}
	}
	return err, false
}
