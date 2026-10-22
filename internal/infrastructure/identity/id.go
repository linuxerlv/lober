package identity

import (
	"crypto/rand"
	"encoding/base32"
)

// idEncoding 是 RFC 4648 的 base32 换成小写字母，不带填充。
var idEncoding = base32.NewEncoding("abcdefghijklmnopqrstuvwxyz234567").WithPadding(base32.NoPadding)

// NewID 生成一个 ID：128 位随机数，编成 26 个字符的小写 base32。它满足 model.ValidID 与 model.IsSafeWord：
// 任务 ID 经 {{sys:task}} 原样代入正文（L-13b）。它不含 -，所以不会和 builtin-kickstart 这类内置内容的固定 ID
// 撞上。
func NewID() string {
	var b [16]byte
	// Go 1.24 起 crypto/rand.Read 不会返回错误：取不到随机数时进程直接崩溃，不会给出可预测的 ID。
	rand.Read(b[:])
	return idEncoding.EncodeToString(b[:])
}
