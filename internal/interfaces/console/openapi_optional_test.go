package console

import (
	"reflect"
	"slices"
	"testing"
)

func TestOpenAPIOptionalHostArchitectureAndFirmware(t *testing.T) {
	b := schemaBuilder{defs: map[string]any{}, names: map[reflect.Type]string{}, owners: map[string]reflect.Type{}}
	req := b.definition(reflect.TypeOf(hostRequest{}))
	required := req["required"].([]string)
	if slices.Contains(required, "arch") || slices.Contains(required, "firmware") {
		t.Fatal("optional host fields marked required")
	}
	resp := b.definition(reflect.TypeOf(hostResponse{}))["properties"].(map[string]any)
	for _, name := range []string{"arch", "firmware"} {
		variants, ok := resp[name].(map[string]any)["anyOf"].([]any)
		if !ok || len(variants) != 2 {
			t.Fatalf("%s cannot describe unregistered value", name)
		}
	}
}
