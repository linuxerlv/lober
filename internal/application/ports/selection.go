package ports

// SelectionGuard 协调同一进程内的选择与模板状态变更。
// 等待锁不能立即取消；取得锁后继续传递取消状态。数据库事务仍负责跨实例保护。
type SelectionGuard interface {
	ReadLock() func()
	WriteLock() func()
}
