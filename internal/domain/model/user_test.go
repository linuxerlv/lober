package model

import (
	"slices"
	"testing"
)

func TestRole的取值与Valid(t *testing.T) {
	want := []Role{"viewer", "operator", "hook-author", "hook-approver", "admin"}
	if !slices.Equal(Roles(), want) {
		t.Fatal(Roles())
	}
	for _, r := range want {
		if !r.Valid() {
			t.Fatal(r)
		}
	}
	for _, r := range []Role{"", "root", "Admin", "viewer "} {
		if r.Valid() {
			t.Fatal(r)
		}
	}
}
func TestRoles返回副本(t *testing.T) {
	got := Roles()
	got[0] = "changed"
	if Roles()[0] != "viewer" {
		t.Fatal("shared roles")
	}
}
