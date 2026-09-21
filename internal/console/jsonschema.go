// 本文件实现 SPEC §12.7 的 JSON Schema 子集校验：params_schema 与
// form-schema 聚合结果都必须落在这个子集内，超出子集在注册期就拒绝，
// 不等渲染时静默丢字段。对应实现计划任务 4、任务 5。
//
// 子集范围（§12.7）：
//
//	type     string | integer | number | boolean | array（array 仅用于多选）
//	校验      enum / pattern / minimum / maximum / minLength / maxLength / required
//	展示      title / description / default
//	扩展      x-widget（input|textarea|password|number|switch|select|ip|host-select）
//	         x-order
//	         x-depends-on { field, value }
//
// 本文件只做「结构落在子集内」的校验，不是完整的 JSON Schema 实现——
// MVP 的表单渲染只需要这个子集，做一个通用 JSON Schema 校验器是给自己
// 增加不必要的维护面。
package console

import (
	"encoding/json"
	"fmt"
)

// jsonSchemaTypes 是 type 字段的合法取值白名单。
var jsonSchemaTypes = map[string]bool{
	"string": true, "integer": true, "number": true, "boolean": true, "array": true,
}

// jsonSchemaWidgets 是 x-widget 扩展字段的合法取值白名单。
var jsonSchemaWidgets = map[string]bool{
	"input": true, "textarea": true, "password": true, "number": true,
	"switch": true, "select": true, "ip": true, "host-select": true,
}

// jsonSchemaAllowedKeys 是单个字段 schema 对象里允许出现的键的完整集合
// （§12.7 子集）。出现子集之外的键即拒绝——不能因为「反正前端会忽略」
// 就放过，那会让「以为设置了某个校验规则但其实被静默丢弃」的错误配置
// 通过注册。
var jsonSchemaAllowedKeys = map[string]bool{
	"type": true, "enum": true, "pattern": true, "minimum": true, "maximum": true,
	"minLength": true, "maxLength": true, "required": true,
	"title": true, "description": true, "default": true,
	"x-widget": true, "x-order": true, "x-depends-on": true,
}

// validateParamsSchemaSubset 解析 schemaText（JSON Schema 文本）并校验它
// 落在 §12.7 的子集内。预期的顶层形态是一个 JSON object，键是字段名，
// 值是符合子集的字段 schema——这与 form-schema 端点要返回的形态一致
// （properties 风格的扁平映射，本模块用「顶层就是字段映射」而不是标准
// JSON Schema 的 {type:object, properties:{...}} 包一层，是刻意简化：
// hook 的 params_schema 只声明「需要哪些参数」，不需要标准 JSON Schema
// 的完整信封）。
//
// 空字符串视为「无参数声明」，直接通过（很多 hook 不需要任何参数）。
func validateParamsSchemaSubset(schemaText string) error {
	if schemaText == "" {
		return nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schemaText), &fields); err != nil {
		return fmt.Errorf("params_schema 不是合法的 JSON 对象: %w", err)
	}
	for name, raw := range fields {
		if err := validateFieldSchema(raw); err != nil {
			return fmt.Errorf("字段 %q 的 schema 不合法: %w", name, err)
		}
	}
	return nil
}

// validateFieldSchema 校验单个字段的 schema 对象落在 §12.7 子集内。
func validateFieldSchema(raw json.RawMessage) error {
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fmt.Errorf("不是合法的 JSON 对象: %w", err)
	}

	for key := range obj {
		if !jsonSchemaAllowedKeys[key] {
			return fmt.Errorf("键 %q 不在 §12.7 定义的子集内", key)
		}
	}

	if typeRaw, ok := obj["type"]; ok {
		var typ string
		if err := json.Unmarshal(typeRaw, &typ); err != nil {
			return fmt.Errorf("type 必须是字符串")
		}
		if !jsonSchemaTypes[typ] {
			return fmt.Errorf("type 取值 %q 不在子集内（必须是 string/integer/number/boolean/array 之一）", typ)
		}
	}

	if widgetRaw, ok := obj["x-widget"]; ok {
		var widget string
		if err := json.Unmarshal(widgetRaw, &widget); err != nil {
			return fmt.Errorf("x-widget 必须是字符串")
		}
		if !jsonSchemaWidgets[widget] {
			return fmt.Errorf("x-widget 取值 %q 不在子集内", widget)
		}
	}

	if dependsRaw, ok := obj["x-depends-on"]; ok {
		var dep struct {
			Field string          `json:"field"`
			Value json.RawMessage `json:"value"`
		}
		if err := json.Unmarshal(dependsRaw, &dep); err != nil {
			return fmt.Errorf("x-depends-on 必须是 { field, value } 形态")
		}
		if dep.Field == "" {
			return fmt.Errorf("x-depends-on.field 不能为空")
		}
	}

	return nil
}

// mergeParamsSchemas 把多个字段映射（每个来自一个 hook 的 params_schema
// 或固定字段声明）合并去重成一个总的字段映射，供 form-schema 端点聚合
// 使用（§8.3：所有 hook_refs 的 params_schema 合并去重）。
//
// 后出现的同名字段覆盖先出现的——固定字段与 template.required_form_fields
// 在调用顺序上先于 hook 聚合传入，因此 hook 声明的同名字段会覆盖固定
// 字段的 schema；这是刻意的方向：固定字段通常只声明「必填」这类弱约束，
// hook 若对同名字段有更具体的校验规则（如某个 IP 类型的字段限定
// pattern），理应生效。
func mergeParamsSchemas(schemas ...map[string]json.RawMessage) map[string]json.RawMessage {
	out := make(map[string]json.RawMessage)
	for _, s := range schemas {
		for k, v := range s {
			out[k] = v
		}
	}
	return out
}

// parseParamsSchemaFields 把 JSON Schema 文本解析成字段映射，供
// mergeParamsSchemas 使用。schemaText 为空返回空映射（不是 nil，调用方
// 遍历时不需要判空）。
func parseParamsSchemaFields(schemaText string) (map[string]json.RawMessage, error) {
	if schemaText == "" {
		return map[string]json.RawMessage{}, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal([]byte(schemaText), &fields); err != nil {
		return nil, err
	}
	return fields, nil
}
