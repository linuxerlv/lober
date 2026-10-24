package console

import (
	"encoding/json"
	"fmt"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/application/identity"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"

	"reflect"
	"regexp"
	"slices"
	"strings"
)

func openAPIPath(path string) string {
	parts := strings.Split(path, "/")
	for i, p := range parts {
		if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
			parts[i] = "{" + p[1:] + "}"
		}
	}
	return strings.Join(parts, "/")
}

// OpenAPI derives the wire contract, including actual fields and response
// envelopes, from registered descriptors. Every call owns its entire document.
func (s *Server) OpenAPI() map[string]any { return openAPIDocument(s.server.routes) }

func openAPIDocument(routes []route) map[string]any {
	b := schemaBuilder{defs: make(map[string]any), names: make(map[reflect.Type]string), owners: make(map[string]reflect.Type)}
	paths := make(map[string]any)
	rs := slices.Clone(routes)
	slices.SortFunc(rs, func(a, c route) int { return strings.Compare(a.path+" "+a.method, c.path+" "+c.method) })
	problem := b.value(reflect.TypeOf(problemList{}))
	batch := b.value(reflect.TypeOf(batchDetail{}))
	for _, r := range rs {
		responses := make(map[string]any)
		media := r.responseMedia
		if media == "" {
			media = "application/json"
		}
		if r.stream {
			media = "text/event-stream"
		}
		var success any = map[string]any{"type": "object", "nullable": true}
		if r.resp != nil {
			success = b.value(reflect.TypeOf(r.resp))
		}
		if len(r.respVariants) > 0 {
			variants := []any{success}
			for _, variant := range r.respVariants {
				variants = append(variants, b.value(reflect.TypeOf(variant)))
			}
			success = map[string]any{"oneOf": variants}
		}
		if r.stream {
			success = map[string]any{"type": "string"}
		} else if !r.raw {
			success = responseEnvelope(success, 0)
		}
		responses["200"] = map[string]any{"description": "Success", "content": map[string]any{media: map[string]any{"schema": success}}}
		for _, status := range []int{400, 401, 403, 404, 409, 422, 500} {
			data := any(map[string]any{"type": "object", "nullable": true, "enum": []any{nil}})
			if status == 409 || status == 422 {
				data = map[string]any{"oneOf": []any{problem, batch, map[string]any{"type": "object", "nullable": true, "enum": []any{nil}}}}
			}
			if status == 409 {
				data.(map[string]any)["oneOf"] = append(data.(map[string]any)["oneOf"].([]any), b.value(reflect.TypeOf(referenceDetail{})))
			}
			if status == 422 && r.errorData != nil {
				data.(map[string]any)["oneOf"] = append(data.(map[string]any)["oneOf"].([]any), b.value(reflect.TypeOf(r.errorData)))
			}
			responses[fmt.Sprint(status)] = map[string]any{"description": "Request failed", "content": map[string]any{"application/json": map[string]any{"schema": responseEnvelope(data, status)}}}
		}
		op := map[string]any{"summary": r.summary, "tags": []string{r.tag}, "responses": responses}
		if r.perm == "" {
			op["security"] = []any{}
		} else {
			op["security"] = []any{map[string]any{"BearerAuth": []string{}}}
		}
		if r.stream {
			op["security"] = []any{map[string]any{"BearerAuth": []string{}}, map[string]any{"StreamToken": []string{}}}
		}
		parameters := make([]any, 0)
		for _, p := range strings.Split(r.path, "/") {
			if strings.HasPrefix(p, ":") || strings.HasPrefix(p, "*") {
				parameters = append(parameters, map[string]any{"name": p[1:], "in": "path", "required": true, "schema": map[string]any{"type": "string"}})
			}
		}
		for _, input := range []struct {
			value    any
			location string
		}{{r.query, "query"}, {r.headers, "header"}} {
			if input.value == nil {
				continue
			}
			t := reflect.TypeOf(input.value)
			for t.Kind() == reflect.Pointer {
				t = t.Elem()
			}
			if t.Kind() == reflect.Struct {
				for i := 0; i < t.NumField(); i++ {
					f := t.Field(i)
					if f.PkgPath != "" {
						continue
					}
					name := strings.Split(f.Tag.Get("form"), ",")[0]
					if name == "-" {
						continue
					}
					if name == "" {
						name = f.Name
					}
					parameters = append(parameters, map[string]any{"name": name, "in": input.location, "required": hasTag(f.Tag.Get("binding"), "required"), "schema": b.value(f.Type)})
				}
			}
		}
		if len(parameters) > 0 {
			op["parameters"] = parameters
		}
		if r.req != nil {
			requestMedia := r.requestMedia
			if requestMedia == "" {
				requestMedia = "application/json"
			}
			requestSchema := b.value(reflect.TypeOf(r.req))
			if requestMedia == "application/octet-stream" {
				requestSchema = map[string]any{"type": "string", "format": "binary"}
			}
			op["requestBody"] = map[string]any{"required": true, "content": map[string]any{requestMedia: map[string]any{"schema": requestSchema}}}
		}
		path := openAPIPath(r.path)
		item, ok := paths[path].(map[string]any)
		if !ok {
			item = make(map[string]any)
			paths[path] = item
		}
		item[strings.ToLower(r.method)] = op
	}
	return map[string]any{"openapi": "3.0.3", "info": map[string]any{"title": "Lober console", "version": "1.0.0"}, "paths": paths, "components": map[string]any{"schemas": b.defs, "securitySchemes": map[string]any{"BearerAuth": map[string]any{"type": "http", "scheme": "bearer"}, "StreamToken": map[string]any{"type": "apiKey", "in": "query", "name": "access_token"}}}}
}
func responseEnvelope(data any, code int) map[string]any {
	return map[string]any{"type": "object", "required": []string{"code", "data", "message"}, "properties": map[string]any{"code": map[string]any{"type": "integer", "enum": []int{code}}, "data": data, "message": map[string]any{"type": "string"}}}
}
func hasTag(tags, value string) bool { return slices.Contains(strings.Split(tags, ","), value) }

type schemaBuilder struct {
	defs   map[string]any
	names  map[reflect.Type]string
	owners map[string]reflect.Type
}

var schemaChars = regexp.MustCompile(`[^a-zA-Z0-9_]`)

func upperFirst(s string) string {
	if s == "" {
		return "Anonymous"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}
func (b *schemaBuilder) value(t reflect.Type) map[string]any {
	if t == nil {
		return map[string]any{}
	}
	if t == reflect.TypeOf(json.RawMessage{}) {
		return map[string]any{}
	}
	if t.Kind() == reflect.Pointer {
		return map[string]any{"anyOf": []any{b.value(t.Elem()), map[string]any{"type": "object", "nullable": true, "enum": []any{nil}}}}
	}
	if t.Name() != "" && t.PkgPath() != "" {
		if name, exists := b.names[t]; exists {
			return map[string]any{"$ref": "#/components/schemas/" + name}
		}
		name := upperFirst(schemaChars.ReplaceAllString(t.Name(), "_"))
		if !strings.HasSuffix(t.PkgPath(), "/console") {
			parts := strings.Split(t.PkgPath(), "/")
			name = upperFirst(parts[len(parts)-1]) + name
		}
		// 已发布的名称保持稳定，内部类型搬目录不改变网页引用。
		switch t {
		case reflect.TypeOf(model.Role("")):
			name = "StoreRole"
		case reflect.TypeOf(model.SourceState("")):
			name = "StoreSourceState"
		}
		if owner, exists := b.owners[name]; exists && owner != t {
			name += fmt.Sprint(len(b.names))
		}
		b.names[t] = name
		b.owners[name] = t
		b.defs[name] = map[string]any{}
		b.defs[name] = b.definition(t)
		return map[string]any{"$ref": "#/components/schemas/" + name}
	}
	return b.definition(t)
}
func (b *schemaBuilder) definition(t reflect.Type) map[string]any {
	switch t.Kind() {
	case reflect.Struct:
		props := make(map[string]any)
		required := make([]string, 0)
		for i := 0; i < t.NumField(); i++ {
			f := t.Field(i)
			fieldType := f.Type
			for fieldType.Kind() == reflect.Pointer {
				fieldType = fieldType.Elem()
			}
			// encoding/json exposes exported fields of an anonymous private
			// struct; the struct's own unexported name does not hide its content.
			if f.PkgPath != "" && !(f.Anonymous && fieldType.Kind() == reflect.Struct) {
				continue
			}
			tag := f.Tag.Get("json")
			name := strings.Split(tag, ",")[0]
			if name == "-" {
				continue
			}
			if f.Anonymous && name == "" {
				inner := f.Type
				for inner.Kind() == reflect.Pointer {
					inner = inner.Elem()
				}
				if inner.Kind() == reflect.Struct {
					def := b.definition(inner)
					for k, v := range def["properties"].(map[string]any) {
						props[k] = v
					}
					fields, _ := def["required"].([]string)
					required = append(required, fields...)
					continue
				}
			}
			if name == "" {
				name = f.Name
			}
			property := b.value(f.Type)
			// Optional inventory facts serialize as an empty string when not
			// registered. Describe that wire value without weakening the catalog
			// enum at required uses such as ready ISO architecture.
			if f.Tag.Get("openapi") == "allowEmpty" {
				property = map[string]any{"anyOf": []any{property, map[string]any{"type": "string", "enum": []string{""}}}}
			}
			if hasTag(f.Tag.Get("binding"), "required") {
				if f.Type.Kind() == reflect.Pointer {
					property = b.value(f.Type.Elem())
				}
				delete(property, "nullable")
			}
			props[name] = property
			if !hasTag(tag, "omitempty") || hasTag(f.Tag.Get("binding"), "required") {
				required = append(required, name)
			}
		}
		out := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			out["required"] = required
		}
		return out
	case reflect.Map:
		return map[string]any{"type": "object", "additionalProperties": b.value(t.Elem()), "nullable": true}
	case reflect.Slice, reflect.Array:
		if t.Elem().Kind() == reflect.Uint8 {
			return map[string]any{"type": "string", "format": "byte", "nullable": true}
		}
		return map[string]any{"type": "array", "items": b.value(t.Elem()), "nullable": t.Kind() == reflect.Slice}
	case reflect.Bool:
		return map[string]any{"type": "boolean"}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64, reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return map[string]any{"type": "integer", "format": "int64"}
	case reflect.Float32, reflect.Float64:
		return map[string]any{"type": "number", "format": "double"}
	case reflect.String:
		out := map[string]any{"type": "string"}
		values := make([]string, 0)
		switch t {
		case reflect.TypeOf(model.Role("")):
			for _, v := range identity.PublicRoles() {
				values = append(values, string(v))
			}
		case reflect.TypeOf(SourceKind("")):
			values = []string{"url", "upload", "path"}
		case reflect.TypeOf(catalog.Family("")):
			for _, v := range authoring.PublicCatalog().Families {
				values = append(values, string(v))
			}
		case reflect.TypeOf(catalog.Format("")):
			for _, v := range authoring.PublicCatalog().Formats {
				values = append(values, string(v))
			}
		case reflect.TypeOf(catalog.Series("")):
			for _, v := range authoring.PublicCatalog().Series {
				values = append(values, string(v))
			}
		case reflect.TypeOf(catalog.Arch("")):
			for _, v := range authoring.PublicCatalog().Archs {
				values = append(values, string(v))
			}
		case reflect.TypeOf(catalog.Firmware("")):
			for _, v := range authoring.PublicCatalog().Firmwares {
				values = append(values, string(v))
			}
		}
		if len(values) > 0 {
			out["enum"] = values
		}
		return out
	default:
		return map[string]any{}
	}
}

// OpenAPIDocument 只导出真实路由描述，不启动服务或访问依赖。
func OpenAPIDocument() map[string]any { return openAPIDocument((&server{}).allRoutes()) }
