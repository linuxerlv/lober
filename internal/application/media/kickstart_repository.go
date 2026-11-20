package media

import (
	"context"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
	mediarules "github.com/lober/lober/internal/domain/media"
)

func planKickstartRepository(ctx context.Context, tree port.MediaView, plan port.ExtractionPlan) (port.ExtractionPlan, error) {
	if plan.Identified.Format != "kickstart" || (plan.Identified.Series != catalog.SeriesAlmalinux && plan.Identified.Series != catalog.SeriesRocky) {
		return plan, nil
	}
	text, err := identifyText(ctx, tree, ".treeinfo")
	if err != nil {
		return plan, identifyErrorForISO(plan.ISO.ID, err)
	}
	layout, matched, err := mediarules.MinimalKickstartRepository(plan.Identified.Series, text)
	if err != nil {
		return plan, identifyErrorForISO(plan.ISO.ID, err)
	}
	if matched {
		plan.Identified.RepoRoot = layout.Root
		plan.RepoPaths = layout.RepoPaths
	}
	return plan, nil
}
