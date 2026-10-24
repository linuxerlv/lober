package media

import (
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

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
