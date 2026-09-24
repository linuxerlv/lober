// 本文件实现 Unix 秒与可空 timestamptz 之间的互转，对应实现计划任务 2。
//
// Task 里大量时间戳字段的零值语义是「尚未发生」（phase_entered_at 在
// pending 阶段前未设置、token_expires_at 在 token 尚未签发前未设置等），
// 这个零值必须映射成 SQL NULL 而不是 1970-01-01——否则查询「哪些任务的
// token 已过期」这类条件会把「从未签发」误判为「早已过期」。
package pgtype

import "time"

// NullTimeFromUnix 把 Unix 秒转换为可空的时刻指针。sec == 0 表示「尚未
// 发生」，映射为 nil（落地为 SQL NULL）；非零值转换为 UTC 时刻——统一
// 时区是因为 Unix 秒本身无时区信息，往返不丢信息的前提是双方都固定用
// 同一个时区解读。
func NullTimeFromUnix(sec int64) *time.Time {
	if sec == 0 {
		return nil
	}
	t := time.Unix(sec, 0).UTC()
	return &t
}

// UnixFromNullTime 是 NullTimeFromUnix 的逆操作：nil 转回 0，非 nil 取
// Unix 秒（time.Time.Unix() 本身与时区无关，无需先转 UTC）。
func UnixFromNullTime(t *time.Time) int64 {
	if t == nil {
		return 0
	}
	return t.Unix()
}
