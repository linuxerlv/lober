package admission

import (
	"github.com/lober/lober/internal/domain/catalog"
	"testing"
)

func TestHistoricalMixedSeriesTemplateCannotCreateSupportedTask(t *testing.T) {
	for _, series := range []catalog.Series{"sles15", "leap15", "sles16", "leap16", "openeuler", "opencloudos", "removed"} {
		t.Run(string(series), func(t *testing.T) {
			tpl := baselineTemplate()
			tpl.Series = append(tpl.Series, series)
			if ps := TemplateUsable(tpl, nil, rockyISO()); len(ps) == 0 {
				t.Fatal("旧模板包含不支持系统时仍可用于新任务")
			}
		})
	}
}
