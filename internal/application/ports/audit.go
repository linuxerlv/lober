package ports

// AuditIntent 是可信入口记录的操作者、动作、说明和时间，不包含保存能力。
// 它是普通值而非操作：不校验、不保存，无结果顺序、错误、重试、事务或并发提交。
// 校验和原子审计由消费操作的仓储约定负责；值本身无通知、上下文或超时。
type AuditIntent struct {
	Actor, Action, Detail string
	At                    int64
}
