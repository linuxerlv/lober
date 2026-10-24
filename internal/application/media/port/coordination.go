package port

import "context"

// SourceLocks 让同一实例内的获取、导入与删除共用来源锁和取消登记。
type SourceLocks interface {
	Lock(id, role string) func()
	Track(id, role string, cancel context.CancelFunc) func()
	Cancel(id, role string)
}

// RegistrationGate 让删除快照覆盖已完成登记的来源。
type RegistrationGate interface {
	Lock()
	Unlock()
	RLock()
	RUnlock()
}
