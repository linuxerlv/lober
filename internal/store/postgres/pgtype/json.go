// 本文件实现承载任意结构体的 JSONB 列，对应实现计划任务 2 的补充要求：
// StringMap/StringSlice 只覆盖 map[string]string 与 []string，但
// tasks.failure_detail_json 这类列的载荷是 *model.FailureDetail 这样的
// 结构体指针（不是 map 或 slice），需要第三个类型承载。
//
// 用泛型而不是给每个聚合包各写一个具体类型：FailureDetail 是目前唯一已知
// 的结构体 JSONB 载荷，但下一个类似需求出现时，泛型让调用方直接
// JSON[*SomeStruct] 复用，不需要回到本包加代码。
package pgtype

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"reflect"
)

// JSON 包装任意可 JSON 序列化的类型 T，实现 sql.Scanner 与
// driver.Valuer。典型用法是 T 为指针类型（如 *model.FailureDetail），
// 让「该列为 SQL NULL」与「该列有值」分别对应 Val 为 nil 指针与非 nil
// 指针——这与 StringMap/StringSlice 的 nil-vs-空 区分是同一套约定。
type JSON[T any] struct {
	Val T
}

// Value 实现 driver.Valuer。Val 若是 nil 指针（或其他能与 nil 比较且确实
// 为 nil 的类型），映射为 SQL NULL；否则序列化成 JSON。
func (j JSON[T]) Value() (driver.Value, error) {
	if isNilValue(j.Val) {
		return nil, nil
	}
	b, err := json.Marshal(j.Val)
	if err != nil {
		return nil, fmt.Errorf("pgtype: JSON 序列化失败: %w", err)
	}
	return b, nil
}

// Scan 实现 sql.Scanner。src 为 nil（SQL NULL）时 Val 保持该类型的零值
// （T 为指针类型时即 nil）；否则按 JSON 解码进 Val。
func (j *JSON[T]) Scan(src any) error {
	if src == nil {
		var zero T
		j.Val = zero
		return nil
	}

	b, err := asBytes(src)
	if err != nil {
		return fmt.Errorf("pgtype: JSON.Scan: %w", err)
	}

	return unmarshalInto(b, &j.Val)
}

// unmarshalInto 把 JSON 解码进 *T——单独抽出是因为 T 为指针类型时
// json.Unmarshal 需要拿到指向指针的指针才能把结果写回 j.Val 本身，直接
// json.Unmarshal(b, j.Val) 在 j.Val 为 nil 指针时会报错（无法写入 nil）。
func unmarshalInto[T any](b []byte, dst *T) error {
	if err := json.Unmarshal(b, dst); err != nil {
		return fmt.Errorf("pgtype: JSON 反序列化失败: %w", err)
	}
	return nil
}

// isNilValue 判断 v 是否为 nil——v 的静态类型是泛型参数 T，只有 T 实例化
// 为指针、map、slice 等可为 nil 的类型时判断才有意义；T 为非指针结构体等
// 值类型时恒返回 false（值类型没有 nil 状态，调用方若想要「未设置」语义
// 应该用指针类型实例化 JSON[T]）。
func isNilValue(v any) bool {
	if v == nil {
		return true
	}
	// any(v) 对指针类型装箱后不等于裸 nil（Go 的经典陷阱：typed nil !=
	// untyped nil），必须用 reflect 才能正确识别「类型化的 nil 指针」。
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Ptr, reflect.Map, reflect.Slice, reflect.Interface:
		return rv.IsNil()
	default:
		return false
	}
}
