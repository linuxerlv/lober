package model

import (
	"slices"
	"testing"
)

func TestSubject的Kind常量与注释一致(t *testing.T) {
	want := []string{"host", "iso", "hook", "impl", "template", "slot", "param", "body"}
	if !slices.Equal(Kinds(), want) {
		t.Fatal(Kinds())
	}
	values := []string{KindHost, KindISO, KindHook, KindImpl, KindTemplate, KindSlot, KindParam, KindBody}
	if !slices.Equal(values, want) {
		t.Fatal(values)
	}
	changed := Kinds()
	changed[0] = "changed"
	if !slices.Equal(Kinds(), want) {
		t.Fatal("caller changed kinds")
	}
}
func Test版本键的字符串形式(t *testing.T) {
	if (HookKey{ID: "lvm", Version: 3}).String() != "lvm@3" {
		t.Fatal("hook version")
	}
	if (TemplateKey{ID: "rhel-base", Version: 12}).String() != "rhel-base@12" {
		t.Fatal("template version")
	}
}
