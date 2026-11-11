package port

import "context"

// SourceLocks 让同一实例内的获取、导入与删除共用来源锁和取消登记。
type SourceLocks interface {
	// Lock 的八项能力约定唯一连接 docs/contracts/media.md 第10节「SourceLocks.Lock」行及该节共同边界。
	Lock(id, role string) func()
	// Track 的八项能力约定唯一连接 docs/contracts/media.md 第10节「SourceLocks.Track」行及该节共同边界。
	Track(id, role string, cancel context.CancelFunc) func()
	// Cancel 的八项能力约定唯一连接 docs/contracts/media.md 第10节「SourceLocks.Cancel」行及该节共同边界。
	Cancel(id, role string)
}

// RegistrationGate 让删除快照覆盖已完成登记的来源。
type RegistrationGate interface {
	// Lock 的八项能力约定唯一连接 docs/contracts/media.md 第10节「RegistrationGate.Lock」行及该节共同边界。
	Lock()
	// Unlock 的八项能力约定唯一连接 docs/contracts/media.md 第10节「RegistrationGate.Unlock」行及该节共同边界。
	Unlock()
	// RLock 的八项能力约定唯一连接 docs/contracts/media.md 第10节「RegistrationGate.RLock」行及该节共同边界。
	RLock()
	// RUnlock 的八项能力约定唯一连接 docs/contracts/media.md 第10节「RegistrationGate.RUnlock」行及该节共同边界。
	RUnlock()
}
