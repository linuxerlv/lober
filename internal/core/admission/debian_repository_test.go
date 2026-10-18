package admission

import (
	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"testing"
)

func TestLegacyDebianWithoutRepoCannotCreateBatch(t *testing.T) {
	iso := model.ISO{ID: "old-debian", Name: "Debian", Family: "linux", Series: "debian", Version: "13.7.0", Arch: "x86_64", State: model.ISOReady, Artifacts: map[string]string{"kernel": "linux", "initrd": "initrd.gz"}}
	result := Batch(BatchInput{ISO: iso, Hosts: []model.Host{{SN: "QUALIFICATION"}}, Template: model.Template{ID: "debian", State: model.VersionApproved, Series: []catalog.Series{"debian"}}})
	if len(result.Common) != 1 || result.Common[0].Code != "iso_artifact_missing" {
		t.Fatalf("legacy Debian can create tasks: %v", result.Common)
	}
}
