package execution

import "errors"

// 采集上报的分类错误。执行模块只认这五个类别，不认采集模块的具体错误类型：接口层按
// GatewayError.Kind 回状态码，不知道采集模块存在。
//
// 组装点负责把采集模块的错误翻成这里的哨兵错误，两个应用模块互不依赖。
var (
	// ErrInventoryUnauthorized 是令牌不对、超时或没发过。调用方回 401。
	ErrInventoryUnauthorized = errors.New("inventory credential rejected")
	// ErrInventoryUnknownHost 是这个 SN 没登记过。调用方回 404。
	ErrInventoryUnknownHost = errors.New("inventory host unknown")
	// ErrInventoryBusy 是主机有打开的任务，此刻不接受采集。调用方回 409。
	ErrInventoryBusy = errors.New("inventory host busy")
	// ErrInventoryRejected 是上报格式对但值不可信。调用方回 422。
	ErrInventoryRejected = errors.New("inventory report rejected")
	// ErrInventoryUnsupported 是没接采集能力。调用方回 404，与「路径不存在」同一响应。
	ErrInventoryUnsupported = errors.New("inventory not configured")
)
