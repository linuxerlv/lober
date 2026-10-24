package ports

// AuditIntent 是可信入口记录的操作者、动作、说明和时间，不包含保存能力。
type AuditIntent struct {
	Actor, Action, Detail string
	At                    int64
}
