package console

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"reflect"
	"slices"
	"strings"
	"testing"
)

type schemaNode struct {
	ID       string        `json:"id" binding:"required"`
	Children []*schemaNode `json:"children,omitempty"`
}
type schemaRequest struct {
	Node    *schemaNode    `json:"node" binding:"required"`
	Hidden  string         `json:"-"`
	Options map[string]int `json:"options"`
}

func Test每条路由都有OpenAPI描述且没有多余路由(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	doc := s.OpenAPI()
	paths := doc["paths"].(map[string]any)
	for _, r := range s.Engine.Routes() {
		path := openAPIPath(r.Path)
		item, ok := paths[path].(map[string]any)
		if !ok {
			t.Fatal(path)
		}
		op, ok := item[strings.ToLower(r.Method)].(map[string]any)
		if !ok || op["summary"] == "" || len(op["tags"].([]string)) == 0 {
			t.Fatal(op)
		}
	}
	count := 0
	for _, item := range paths {
		count += len(item.(map[string]any))
	}
	if count != len(s.Engine.Routes()) {
		t.Fatal("route/document mismatch")
	}
}
func TestOpenAPI引用的schema真实存在且包含字段(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	doc := s.OpenAPI()
	defs := doc["components"].(map[string]any)["schemas"].(map[string]any)
	var visit func(any)
	visit = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if ref, ok := x["$ref"].(string); ok {
				n := strings.TrimPrefix(ref, "#/components/schemas/")
				def, exists := defs[n]
				if !exists || n == ref || def == nil {
					t.Fatal("dangling ref", ref)
				}
			}
			for _, child := range x {
				visit(child)
			}
		case []any:
			for _, child := range x {
				visit(child)
			}
		}
	}
	visit(doc)
	u := defs["UserResponse"].(map[string]any)["properties"].(map[string]any)
	if u["username"] == nil || u["role"] == nil || u["password"] != nil || u["rev"] == nil {
		t.Fatal(u)
	}
}
func TestOpenAPI递归请求与路径参数生成(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	s.server.installRoutes(s, []route{{method: "PUT", path: "/console/v1/probe/:id", perm: PermAnyUser, tag: "probe", summary: "Schema probe", req: schemaRequest{}, resp: schemaNode{}, handle: func(c *gin.Context) {}}})
	doc := s.OpenAPI()
	defs := doc["components"].(map[string]any)["schemas"].(map[string]any)
	p := defs["SchemaRequest"].(map[string]any)["properties"].(map[string]any)
	if p["node"] == nil || p["options"] == nil || p["Hidden"] != nil {
		t.Fatal(p)
	}
	op := doc["paths"].(map[string]any)["/console/v1/probe/{id}"].(map[string]any)["put"].(map[string]any)
	params := op["parameters"].([]any)
	if len(params) != 1 || params[0].(map[string]any)["required"] != true {
		t.Fatal(params)
	}
	if op["requestBody"] == nil || defs["SchemaNode"] == nil {
		t.Fatal("missing request schema")
	}
}
func TestOpenAPI编码两次相同且返回独立文档(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	a, b := s.OpenAPI(), s.OpenAPI()
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(b)
	if string(aa) != string(bb) {
		t.Fatal("nondeterministic")
	}
	a["paths"].(map[string]any)["changed"] = true
	if reflect.DeepEqual(s.OpenAPI(), a) {
		t.Fatal("shared doc")
	}
}

func TestOpenAPI草稿嵌入内容与Rev都在请求schema(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	defs := s.OpenAPI()["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"HookDraftRequest", "TemplateDraftRequest"} {
		def := defs[name].(map[string]any)
		fields := def["properties"].(map[string]any)
		if fields["rev"] == nil || fields["params"] == nil || fields["name"] == nil {
			t.Fatal(name, fields)
		}
		required := def["required"].([]string)
		if !slices.Contains(required, "rev") {
			t.Fatal(required)
		}
	}
}

// 包目录可以调整，已经发布给网页的类型名称和引用仍须稳定。
func TestOpenAPI公开角色与来源状态名称保持兼容(t *testing.T) {
	doc := New(Deps{Clock: func() int64 { return fixedAt }}).OpenAPI()
	defs := doc["components"].(map[string]any)["schemas"].(map[string]any)
	for _, name := range []string{"StoreRole", "StoreSourceState"} {
		if _, exists := defs[name]; !exists {
			t.Errorf("公开类型丢失：%s", name)
		}
	}
	for _, name := range []string{"ModelRole", "ModelSourceState"} {
		if _, exists := defs[name]; exists {
			t.Errorf("内部模型目录改变了公开类型名称：%s", name)
		}
	}
	for _, tc := range []struct{ schema, field, target string }{
		{"RoleMenuResponse", "role", "StoreRole"},
		{"UserCreateRequest", "role", "StoreRole"},
		{"UserResponse", "role", "StoreRole"},
		{"UserUpdateRequest", "role", "StoreRole"},
		{"SourceProgress", "state", "StoreSourceState"},
	} {
		definition, exists := defs[tc.schema].(map[string]any)
		if !exists {
			t.Errorf("公开响应或请求丢失：%s", tc.schema)
			continue
		}
		properties := definition["properties"].(map[string]any)
		field, exists := properties[tc.field].(map[string]any)
		if !exists || field["$ref"] != "#/components/schemas/"+tc.target {
			t.Errorf("%s.%s引用应保持%s，实际为%v", tc.schema, tc.field, tc.target, properties[tc.field])
		}
	}
	if role, exists := defs["StoreRole"].(map[string]any); exists {
		want := map[string]any{"type": "string", "enum": []string{"viewer", "operator", "hook-author", "hook-approver", "admin"}}
		if !reflect.DeepEqual(role, want) {
			t.Errorf("公开角色取值改变：%v", role)
		}
	}
	if state, exists := defs["StoreSourceState"].(map[string]any); exists {
		if !reflect.DeepEqual(state, map[string]any{"type": "string"}) {
			t.Errorf("公开来源状态格式改变：%v", state)
		}
	}
}
