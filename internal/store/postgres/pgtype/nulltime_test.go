// 本文件测试 Unix 秒与 *time.Time 之间的互转，覆盖 0、正数、nil 三类输入
// （对应实现计划任务 2）。
package pgtype

import (
	"testing"
	"time"
)

// TestNullTimeFromUnix_Zero 验证 sec == 0 映射为 nil——Task 里大量时间戳
// 字段的零值语义是「尚未发生」（如 phase_entered_at、token_expires_at 在
// pending 阶段前未设置），零值必须映射成 SQL NULL 而不是 1970-01-01。
func TestNullTimeFromUnix_Zero(t *testing.T) {
	got := NullTimeFromUnix(0)
	if got != nil {
		t.Errorf("NullTimeFromUnix(0) = %v, want nil", got)
	}
}

// TestNullTimeFromUnix_Positive 验证正数正确转换且时区为 UTC——时区不统一
// 会让「同一个 Unix 秒」在不同会话里显示成不同的本地时间，误导排查判断。
func TestNullTimeFromUnix_Positive(t *testing.T) {
	const sec = int64(1758700800) // 2025-09-24T08:00:00Z 附近的任意正数

	got := NullTimeFromUnix(sec)
	if got == nil {
		t.Fatal("NullTimeFromUnix(正数) = nil, want 非 nil")
	}
	if got.Unix() != sec {
		t.Errorf("got.Unix() = %d, want %d", got.Unix(), sec)
	}
	if got.Location() != time.UTC {
		t.Errorf("got.Location() = %v, want time.UTC —— "+
			"时区不统一会让同一个 Unix 秒在不同地方显示成不同的本地时间", got.Location())
	}
}

// TestUnixFromNullTime_Nil 验证 nil 映射为 0，与 NullTimeFromUnix(0) 互为
// 逆操作。
func TestUnixFromNullTime_Nil(t *testing.T) {
	got := UnixFromNullTime(nil)
	if got != 0 {
		t.Errorf("UnixFromNullTime(nil) = %d, want 0", got)
	}
}

// TestUnixFromNullTime_Positive 验证非 nil 时刻正确转回 Unix 秒，且往返
// 无损——不依赖输入时刻本身的时区（time.Time.Unix() 本就与时区无关）。
func TestUnixFromNullTime_Positive(t *testing.T) {
	const sec = int64(1758700800)
	tm := time.Unix(sec, 0)

	got := UnixFromNullTime(&tm)
	if got != sec {
		t.Errorf("UnixFromNullTime(&tm) = %d, want %d", got, sec)
	}
}

// TestNullTimeUnix_Roundtrip 组合往返：sec -> *time.Time -> sec 必须还原
// 原值，覆盖 0 与正数两种情况。
func TestNullTimeUnix_Roundtrip(t *testing.T) {
	for _, sec := range []int64{0, 1, 1758700800} {
		got := UnixFromNullTime(NullTimeFromUnix(sec))
		if got != sec {
			t.Errorf("往返(%d) = %d, want %d", sec, got, sec)
		}
	}
}
