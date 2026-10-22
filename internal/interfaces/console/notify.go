package console

import (
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/infrastructure/notification"
)

// Notifier 保留旧调用名；共同契约只有 ports.Notifier 一份。
type Notifier = ports.Notifier

// NewHub 是现有入口的兼容转发，第六阶段删除。生产组装直接创建通知实现。
func NewHub() ports.Notifier { return notification.NewHub() }
