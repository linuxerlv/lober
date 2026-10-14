package console

import (
	"encoding/json"
	"github.com/gin-gonic/gin"
	"reflect"
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
