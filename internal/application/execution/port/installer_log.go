package port

import "context"

// InstallerLog 是可信日志适配器已按租约归属、截断并脱敏的单包正文。
// 调用期间不得并发修改Body；接收器同步编码，不保留此可变切片。
type InstallerLog struct {
	TaskID   string
	Attempt  int
	SourceIP string
	At       int64
	Body     []byte
}
type InstallerLogReceiver interface {
	// ReceiveInstallerLog 的八项约定唯一连接 docs/contracts/machine-execution.md「日志收录入口」。
	ReceiveInstallerLog(context.Context, InstallerLog) error
}
