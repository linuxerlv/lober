package model

import "slices"

const (
	InventoryRequestID   = "inv_request_id"
	InventoryRequestedAt = "inv_requested_at"
	InventoryInFlightAt  = "inv_in_flight_at"
	InventoryCollectedAt = "inv_collected_at"
)

// InventoryControlAttrs 是采集服务管理的属性。硬件报告与人工填写的业务属性另算。
func InventoryControlAttrs() []string {
	return []string{InventoryRequestID, InventoryRequestedAt, InventoryInFlightAt, InventoryCollectedAt}
}

func IsInventoryControlAttr(key string) bool { return slices.Contains(InventoryControlAttrs(), key) }
