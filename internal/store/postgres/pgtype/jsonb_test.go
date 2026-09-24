// 本文件测试 StringMap/StringSlice 的 Scanner/Valuer 往返行为。对应实现
// 计划任务 2。
//
// 四类输入必须能区分：非空、空但非 nil（map[string]string{}）、nil、从 SQL
// NULL 读回。混淆 nil 与空会让 form_data_json 这类列的「没填」和「填了空
// 表单」变成同一个值——这正是本测试要锁住的行为。
package pgtype

import (
	"database/sql/driver"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestStringMap_Valuer_NonEmpty 验证非空 map 序列化成 JSON 对象。
func TestStringMap_Valuer_NonEmpty(t *testing.T) {
	m := StringMap{"biz_ip": "10.0.0.1", "hostname": "host-01"}

	got, err := m.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}

	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("Value() 返回类型 %T, want []byte", got)
	}

	// 反解回 map 比较，避免依赖 JSON 键序（Go map 遍历顺序不定）。
	var roundtrip StringMap
	if err := roundtrip.Scan(gotBytes); err != nil {
		t.Fatalf("Scan(Value() 的结果) 返回错误: %v", err)
	}
	if diff := cmp.Diff(m, roundtrip); diff != "" {
		t.Errorf("非空 map 往返后不一致 (-want +got):\n%s", diff)
	}
}

// TestStringMap_Valuer_EmptyNotNil 验证空但非 nil 的 map（map[string]string{}）
// 序列化成 JSON 空对象 "{}"，不是 JSON null——这是「填了空表单」的正确表达。
func TestStringMap_Valuer_EmptyNotNil(t *testing.T) {
	m := StringMap{}

	got, err := m.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}

	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("Value() 返回类型 %T, want []byte", got)
	}
	if string(gotBytes) != "{}" {
		t.Errorf("Value() = %q, want %q —— "+
			"空 map 必须序列化成 JSON 空对象，不能与 nil 混淆成同一个值",
			string(gotBytes), "{}")
	}
}

// TestStringMap_Valuer_Nil 验证 nil map 序列化成 driver.Value 的 nil
// （最终落地为 SQL NULL），不是 JSON null 字符串本身。
func TestStringMap_Valuer_Nil(t *testing.T) {
	var m StringMap // nil

	got, err := m.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}
	if got != nil {
		t.Errorf("Value() = %v, want nil —— nil map 必须映射成 SQL NULL", got)
	}
}

// TestStringMap_Scanner_FromSQLNull 验证从 SQL NULL 读回得到 nil map，
// 不是空 map——调用方用 `m == nil` 判断「这一行的该列原本是 NULL」。
func TestStringMap_Scanner_FromSQLNull(t *testing.T) {
	var m StringMap
	if err := m.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) 返回错误: %v", err)
	}
	if m != nil {
		t.Errorf("Scan(nil) 后 m = %#v, want nil —— "+
			"从 SQL NULL 读回必须得到 nil map，不是空 map，否则「没填」和"+
			"「填了空表单」在读回后就分不清了", m)
	}
}

// TestStringMap_Scanner_FromEmptyObject 验证从 JSON "{}" 读回得到非 nil 的
// 空 map，与「从 NULL 读回」严格区分。
func TestStringMap_Scanner_FromEmptyObject(t *testing.T) {
	var m StringMap
	if err := m.Scan([]byte("{}")); err != nil {
		t.Fatalf("Scan([]byte(\"{}\")) 返回错误: %v", err)
	}
	if m == nil {
		t.Error("Scan(\"{}\") 后 m 为 nil, want 非 nil 的空 map —— " +
			"JSON 空对象与 SQL NULL 是两种不同的输入，读回结果不能相同")
	}
	if len(m) != 0 {
		t.Errorf("Scan(\"{}\") 后 len(m) = %d, want 0", len(m))
	}
}

// TestStringMap_Scanner_AcceptsStringSrc 验证 Scan 同时接受 string 类型的
// src——不同数据库驱动对 jsonb 列的扫描目标类型不完全一致（有的给
// []byte，有的给 string），Scanner 必须都能处理。
func TestStringMap_Scanner_AcceptsStringSrc(t *testing.T) {
	var m StringMap
	if err := m.Scan(`{"k":"v"}`); err != nil {
		t.Fatalf("Scan(string) 返回错误: %v", err)
	}
	if m["k"] != "v" {
		t.Errorf("Scan(string) 后 m[\"k\"] = %q, want %q", m["k"], "v")
	}
}

// ---- StringSlice：同样四类输入，理由同上（HookDigests 列用得到）----

func TestStringSlice_Valuer_NonEmpty(t *testing.T) {
	s := StringSlice{"sha256:aaa", "sha256:bbb"}

	got, err := s.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}
	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("Value() 返回类型 %T, want []byte", got)
	}

	var roundtrip StringSlice
	if err := roundtrip.Scan(gotBytes); err != nil {
		t.Fatalf("Scan(Value() 的结果) 返回错误: %v", err)
	}
	if diff := cmp.Diff(s, roundtrip); diff != "" {
		t.Errorf("非空 slice 往返后不一致 (-want +got):\n%s", diff)
	}
}

func TestStringSlice_Valuer_EmptyNotNil(t *testing.T) {
	s := StringSlice{}

	got, err := s.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}
	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("Value() 返回类型 %T, want []byte", got)
	}
	if string(gotBytes) != "[]" {
		t.Errorf("Value() = %q, want %q", string(gotBytes), "[]")
	}
}

func TestStringSlice_Valuer_Nil(t *testing.T) {
	var s StringSlice // nil

	got, err := s.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}
	if got != nil {
		t.Errorf("Value() = %v, want nil", got)
	}
}

func TestStringSlice_Scanner_FromSQLNull(t *testing.T) {
	var s StringSlice
	if err := s.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) 返回错误: %v", err)
	}
	if s != nil {
		t.Errorf("Scan(nil) 后 s = %#v, want nil", s)
	}
}

func TestStringSlice_Scanner_FromEmptyArray(t *testing.T) {
	var s StringSlice
	if err := s.Scan([]byte("[]")); err != nil {
		t.Fatalf("Scan([]byte(\"[]\")) 返回错误: %v", err)
	}
	if s == nil {
		t.Error("Scan(\"[]\") 后 s 为 nil, want 非 nil 的空 slice")
	}
	if len(s) != 0 {
		t.Errorf("Scan(\"[]\") 后 len(s) = %d, want 0", len(s))
	}
}

// TestStringMap_Scan_InvalidType 验证 Scan 遇到不认识的 src 类型时返回
// 错误而不是静默吞掉——静默失败会让调用方以为读到了合法数据。
func TestStringMap_Scan_InvalidType(t *testing.T) {
	var m StringMap
	err := m.Scan(12345) // int 既不是 []byte 也不是 string
	if err == nil {
		t.Error("Scan(int) 未返回错误, want 非 nil 错误 —— " +
			"不认识的 src 类型必须报错，静默吞掉会让调用方以为读到了合法数据")
	}
}

// 编译期断言两个类型确实实现了 sql.Scanner 与 driver.Valuer——如果签名
// 写错，这里会在编译期直接报错，比运行期测试失败更早发现问题。
var (
	_ driver.Valuer = StringMap(nil)
	_ driver.Valuer = StringSlice(nil)
)
