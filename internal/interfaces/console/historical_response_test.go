package console

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func Test历史响应保留旧值且文档允许读取(t *testing.T) {
	defs := OpenAPIDocument()["components"].(map[string]any)["schemas"].(map[string]any)
	for _, series := range []catalog.Series{"leap15", "leap16", "sles16"} {
		t.Run(string(series), func(t *testing.T) {
			iso := toISOResponse(model.ISO{Family: "linux", Series: series})
			template := toTemplateResponseWithProblems(model.Template{Family: "linux", Series: []catalog.Series{series}}, nil)
			hook := toHookResponse(authoring.HookView{Hook: model.Hook{Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{series}}}}})
			task := toTaskItem(model.Task{Snapshot: model.Snapshot{Family: "linux", Series: series, Format: "agama"}})
			for _, tc := range []struct {
				name   string
				value  any
				fields []string
				want   string
			}{
				{"IsoResponse", iso, []string{"series"}, string(series)},
				{"TemplateResponse", template, []string{"series", "[]"}, string(series)},
				{"HookResponse", hook, []string{"impls", "[]", "series", "[]"}, string(series)},
				{"TaskItem", task, []string{"snapshot", "series"}, string(series)},
				{"TaskItem", task, []string{"snapshot", "format"}, "agama"},
			} {
				t.Run(tc.name+"/"+strings.Join(tc.fields, "/"), func(t *testing.T) {
					encoded, err := json.Marshal(tc.value)
					if err != nil {
						t.Fatal(err)
					}
					var wire any
					if err := json.Unmarshal(encoded, &wire); err != nil {
						t.Fatal(err)
					}
					field := defs[tc.name].(map[string]any)
					for _, step := range tc.fields {
						if step == "[]" {
							field = field["items"].(map[string]any)
							wire = wire.([]any)[0]
						} else {
							field = field["properties"].(map[string]any)[step].(map[string]any)
							wire = wire.(map[string]any)[step]
						}
						if ref, ok := field["$ref"].(string); ok {
							field = defs[strings.TrimPrefix(ref, "#/components/schemas/")].(map[string]any)
						}
					}
					if wire != tc.want {
						t.Errorf("历史记录被改写：got=%v want=%s", wire, tc.want)
					}
					if !reflect.DeepEqual(field, map[string]any{"type": "string"}) {
						t.Errorf("历史字段应允许任意字符串，实际为%v", field)
					}
				})
			}
		})
	}
}

func Test新建请求仍只接受当前系统枚举(t *testing.T) {
	defs := OpenAPIDocument()["components"].(map[string]any)["schemas"].(map[string]any)
	wantSeries := []string{"rhel", "rocky", "almalinux", "debian", "ubuntu", "windows-server", "windows-desktop", "esxi"}
	if got := defs["CatalogSeries"].(map[string]any)["enum"]; !reflect.DeepEqual(got, wantSeries) {
		t.Fatalf("新建请求的系统名单变化：got=%v want=%v", got, wantSeries)
	}
	for _, tc := range []struct {
		name  string
		array bool
	}{
		{"IsoRequest", false},
		{"TemplateRequest", true},
		{"ImplDTO", true},
	} {
		field := defs[tc.name].(map[string]any)["properties"].(map[string]any)["series"].(map[string]any)
		if tc.array {
			field = field["items"].(map[string]any)
		}
		if field["$ref"] != "#/components/schemas/CatalogSeries" {
			t.Errorf("%s新建请求不再使用当前系统名单：%v", tc.name, field)
		}
	}
	b := schemaBuilder{defs: map[string]any{}, names: map[reflect.Type]string{}, owners: map[string]reflect.Type{}}
	format := b.definition(reflect.TypeOf(catalog.Format("")))
	wantFormat := []string{"kickstart", "preseed", "autoinstall", "unattend", "esxi-ks"}
	if !reflect.DeepEqual(format["enum"], wantFormat) {
		t.Errorf("当前格式名单变化：%v", format)
	}
}

func TestHook响应完整保留实现内容且空名单仍是数组(t *testing.T) {
	in := []model.Impl{{Family: "linux", Series: []catalog.Series{"leap16"}, Kind: model.ImplScript, Body: "echo fixture", Interpreter: "sh", Timings: []catalog.Timing{"post-target"}}}
	want, err := json.Marshal(implsToDTO(in))
	if err != nil {
		t.Fatal(err)
	}
	out := implsToResponseDTO(in)
	got, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(want) {
		t.Fatalf("响应丢失实现内容：got=%s want=%s", got, want)
	}
	out[0].Series[0] = "ubuntu"
	out[0].Timings[0] = "firstboot"
	if in[0].Series[0] != "leap16" || in[0].Timings[0] != "post-target" {
		t.Fatal("响应修改了原始记录")
	}
	if seriesToResponse(nil) == nil || implsToResponseDTO(nil) == nil {
		t.Fatal("空列表必须输出数组")
	}
}
