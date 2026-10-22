package repository

// Store 是仓储的全部能力，由装配点构造。调用方只声明自己用得到的那几个接口（spec §6.1）。
type Store interface {
	Hosts
	ISOs
	ISOSources
	Hooks
	Templates
	Tasks
	CurrentTaskCreator
	Events
	AuditLog
	Users
	RoleMenus
	// Close 释放连接这类资源，之后不能再调用别的方法。
	Close() error
}
