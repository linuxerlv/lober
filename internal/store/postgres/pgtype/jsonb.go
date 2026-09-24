// 本文件实现 JSONB 列的 Scanner/Valuer，对应实现计划任务 2。
//
// nil 与「空但非 nil」必须能通过 Value/Scan 往返区分：nil 落地成 SQL
// NULL，空 map/slice 落地成 JSON "{}"/"[]"。这不是可省略的细节——
// form_data_json 这类列的「没填」（nil）与「填了空表单」（map[string]string{}）
// 是两种不同的业务事实，混淆会让调用方无法区分这两种情况。
package pgtype

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// StringMap 对应 jsonb 列，承载 map[string]string 形态的数据（如
// tasks.form_data_json、tasks.resolved_params_json）。
type StringMap map[string]string

// Value 实现 driver.Valuer。nil map 映射为 SQL NULL；非 nil（含空）map
// 序列化成 JSON。
func (m StringMap) Value() (driver.Value, error) {
	if m == nil {
		return nil, nil
	}
	b, err := json.Marshal(map[string]string(m))
	if err != nil {
		return nil, fmt.Errorf("pgtype: StringMap 序列化失败: %w", err)
	}
	return b, nil
}

// Scan 实现 sql.Scanner。src 为 nil（SQL NULL）时得到 nil map；否则按
// JSON 解码——不认识的 src 类型报错，不静默吞掉，避免调用方误以为读到了
// 合法数据。
func (m *StringMap) Scan(src any) error {
	if src == nil {
		*m = nil
		return nil
	}

	b, err := asBytes(src)
	if err != nil {
		return fmt.Errorf("pgtype: StringMap.Scan: %w", err)
	}

	var out map[string]string
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Errorf("pgtype: StringMap 反序列化失败: %w", err)
	}
	*m = StringMap(out)
	return nil
}

// StringSlice 对应 jsonb 列，承载 []string 形态的数据（如
// tasks.hook_digests_json）。
type StringSlice []string

// Value 实现 driver.Valuer，语义与 StringMap.Value 一致。
func (s StringSlice) Value() (driver.Value, error) {
	if s == nil {
		return nil, nil
	}
	b, err := json.Marshal([]string(s))
	if err != nil {
		return nil, fmt.Errorf("pgtype: StringSlice 序列化失败: %w", err)
	}
	return b, nil
}

// Scan 实现 sql.Scanner，语义与 StringMap.Scan 一致。
func (s *StringSlice) Scan(src any) error {
	if src == nil {
		*s = nil
		return nil
	}

	b, err := asBytes(src)
	if err != nil {
		return fmt.Errorf("pgtype: StringSlice.Scan: %w", err)
	}

	var out []string
	if err := json.Unmarshal(b, &out); err != nil {
		return fmt.Errorf("pgtype: StringSlice 反序列化失败: %w", err)
	}
	*s = StringSlice(out)
	return nil
}

// asBytes 把 Scan 收到的 src 统一转成 []byte——不同数据库驱动对 jsonb 列
// 的扫描目标类型不完全一致（有的给 []byte，有的给 string），Scanner 必须
// 都能处理，不能只认一种。
func asBytes(src any) ([]byte, error) {
	switch v := src.(type) {
	case []byte:
		return v, nil
	case string:
		return []byte(v), nil
	default:
		return nil, fmt.Errorf("不支持的 src 类型 %T，want []byte 或 string", src)
	}
}
