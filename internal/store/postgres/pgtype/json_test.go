// 本文件测试 JSON[T] 泛型类型——StringMap/StringSlice 只覆盖
// map[string]string 与 []string 两种形态，但 tasks.failure_detail_json
// 这类列承载的是 *model.FailureDetail 这样的结构体指针（计划任务 2 的
// 交付要求：「这两个类型还要能承载 *FailureDetail 这类结构体的 JSONB」）。
//
// 偏差说明（在 StructuredOutput 的 deviations 里也会重复一遍）：计划给出
// 的接口契约只列了 StringMap/StringSlice 两个类型，未列出承载任意结构体
// 的第三个类型。本文件新增 JSON[T]，用泛型而不是给每个聚合包各写一个具体
// 类型——FailureDetail 目前是唯一已知的结构体 JSONB 载荷，但泛型让下一个
// 类似需求（若有）不需要再回到本包加代码。
package pgtype

import (
	"testing"

	"github.com/google/go-cmp/cmp"
)

// sampleDetail 是本文件测试用的最小结构体，字段形态与 model.FailureDetail
// 类似（有字符串、有 int），不直接依赖 model 包——pgtype 是共享基础包，不
// 应该反向依赖任何聚合包的语义（只依赖 model 的类型定义本身，不在测试里
// 引入循环风险）。
type sampleDetail struct {
	Phase    string
	ExitCode int
}

// TestJSON_Valuer_NonNilPointer 验证非 nil 指针序列化成 JSON 对象。
func TestJSON_Valuer_NonNilPointer(t *testing.T) {
	v := JSON[*sampleDetail]{Val: &sampleDetail{Phase: "packages", ExitCode: 1}}

	got, err := v.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}
	gotBytes, ok := got.([]byte)
	if !ok {
		t.Fatalf("Value() 返回类型 %T, want []byte", got)
	}

	var roundtrip JSON[*sampleDetail]
	if err := roundtrip.Scan(gotBytes); err != nil {
		t.Fatalf("Scan(Value() 的结果) 返回错误: %v", err)
	}
	if diff := cmp.Diff(v.Val, roundtrip.Val); diff != "" {
		t.Errorf("非 nil 指针往返后不一致 (-want +got):\n%s", diff)
	}
}

// TestJSON_Valuer_NilPointer 验证 nil 指针映射为 SQL NULL——
// FailureDetail 为 nil 表示「该任务尚未失败或失败详情未记录」。
func TestJSON_Valuer_NilPointer(t *testing.T) {
	var v JSON[*sampleDetail] // Val 为 nil

	got, err := v.Value()
	if err != nil {
		t.Fatalf("Value() 返回错误: %v", err)
	}
	if got != nil {
		t.Errorf("Value() = %v, want nil —— nil 指针必须映射成 SQL NULL", got)
	}
}

// TestJSON_Scanner_FromSQLNull 验证从 SQL NULL 读回得到 nil 指针。
func TestJSON_Scanner_FromSQLNull(t *testing.T) {
	var v JSON[*sampleDetail]
	if err := v.Scan(nil); err != nil {
		t.Fatalf("Scan(nil) 返回错误: %v", err)
	}
	if v.Val != nil {
		t.Errorf("Scan(nil) 后 Val = %#v, want nil", v.Val)
	}
}

// TestJSON_Scanner_FromObject 验证从 JSON 对象读回得到正确填充的指针。
func TestJSON_Scanner_FromObject(t *testing.T) {
	var v JSON[*sampleDetail]
	if err := v.Scan([]byte(`{"Phase":"post","ExitCode":2}`)); err != nil {
		t.Fatalf("Scan() 返回错误: %v", err)
	}
	if v.Val == nil {
		t.Fatal("Scan() 后 Val 为 nil, want 非 nil")
	}
	want := sampleDetail{Phase: "post", ExitCode: 2}
	if diff := cmp.Diff(want, *v.Val); diff != "" {
		t.Errorf("Scan() 后 *Val 不一致 (-want +got):\n%s", diff)
	}
}

// TestJSON_Scanner_AcceptsStringSrc 与 StringMap 同理：不同驱动对 jsonb
// 扫描目标类型给的可能是 string 而不是 []byte。
func TestJSON_Scanner_AcceptsStringSrc(t *testing.T) {
	var v JSON[*sampleDetail]
	if err := v.Scan(`{"Phase":"done","ExitCode":0}`); err != nil {
		t.Fatalf("Scan(string) 返回错误: %v", err)
	}
	if v.Val == nil || v.Val.Phase != "done" {
		t.Errorf("Scan(string) 后 Val = %#v, want Phase=done", v.Val)
	}
}
