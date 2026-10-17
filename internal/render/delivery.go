package render

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
)

// RuntimeFileName and HookFileName expose the same local staging names used in
// generated answers and runners; preview and future gateway code must share them.
func RuntimeFileName(format catalog.Format, slot model.SnapshotSlot) string {
	return slotFile(format, slot)
}
func HookFileName(instance model.SnapshotInstance) string { return hookFile(instance) }
