package model

import "testing"

func TestLegacyReadyDebianRequiresRepository(t *testing.T) {
	iso := ISO{ID: "legacy-debian", Name: "Debian", Family: "linux", Series: "debian", Version: "13.7.0", Arch: "x86_64", State: ISOReady, Artifacts: map[string]string{"kernel": "debian/linux", "initrd": "debian/initrd.gz"}}
	if ps := iso.Unusable(); len(ps) != 1 || ps[0].Code != "iso_artifact_missing" || ps[0].Subject.Path != "artifacts.repo" {
		t.Fatalf("legacy ready ISO without repository admitted: %v", ps)
	}
	iso.Artifacts["repo"] = "debian/13.7.0/x86_64/install/"
	if ps := iso.Unusable(); len(ps) != 0 {
		t.Fatal(ps)
	}
}
