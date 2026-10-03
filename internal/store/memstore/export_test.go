package memstore

// NewFaulty 是打开了失败注入的 New，只给契约测试用：审计的 Actor 等于 actor 时，这条审计的写入在动作写完之后失败，
// 整次写入随之丢掉（storetest.Harness 的要求）。它放在 _test.go 里，生产代码构造不出会注入失败的仓储。
func NewFaulty(actor string) *Store {
	s := New()
	s.fault = actor
	return s
}
