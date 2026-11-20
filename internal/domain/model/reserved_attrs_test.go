package model

import (
	"reflect"
	"testing"
)

func TestHostReservedAttrs导出独立副本(t *testing.T) {
	got := HostReservedAttrs()
	if !reflect.DeepEqual(got, []string{"sn", "arch", "firmware"}) {
		t.Fatal(got)
	}
	got[0] = "changed"
	if HostReservedAttrs()[0] != "sn" || ValidAttrKey("sn") {
		t.Fatal("export mutated reserved table")
	}
}
