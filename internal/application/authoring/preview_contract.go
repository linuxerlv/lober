package authoring

import (
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
)

type PreviewRequest struct {
	Key           model.TemplateKey
	HostSN, ISOID string
	Series        catalog.Series
	Firmware      catalog.Firmware
	Entered       map[params.FieldKey]string
}
type PreviewParamsResult struct {
	Fields   []params.Field
	Problems []model.Problem
	Sample   []string
}
type PreviewResult struct {
	Files    []ports.PreviewFile
	Problems []model.Problem
	Sample   []string
}

// ProblemsError 保留原问题次序，由入口按第一项决定响应。
type ProblemsError struct{ Problems []model.Problem }

func (e *ProblemsError) Error() string { return e.Problems[0].Message }
