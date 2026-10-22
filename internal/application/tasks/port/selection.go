package port

// SelectionGuard 协调同一进程内的选择与模板状态变更。
// 返回值释放锁；它不能替代 CreateCurrentTasks 的数据库事务保护。
type SelectionGuard interface {
	ReadLock() func()
	WriteLock() func()
}
