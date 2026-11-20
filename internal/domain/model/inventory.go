package model

import (
	"encoding/base64"
	"slices"
	"strings"
)

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

// InventoryMachineSN 把序列号变成一个可同时用于引导参数和上报路径的值。
// 常用序列号保持原样。带空格或符号的值用 ~ 开头的编码，采集镜像原样回传即可。
func InventoryMachineSN(raw string) (string, error) {
	sn, err := NormalizeSN(raw)
	if err != nil {
		return "", err
	}
	for i := range len(sn) {
		if !isASCIIAlnum(sn[i]) && strings.IndexByte("._-", sn[i]) < 0 {
			return "~" + base64.RawURLEncoding.EncodeToString([]byte(sn)), nil
		}
	}
	return sn, nil
}

func InventoryReportSN(raw string) (string, error) {
	if encoded, ok := strings.CutPrefix(raw, "~"); ok && len(encoded) <= base64.RawURLEncoding.EncodedLen(snMaxLen) {
		if decoded, err := base64.RawURLEncoding.DecodeString(encoded); err == nil {
			if canonical, err := InventoryMachineSN(string(decoded)); err == nil && canonical == raw {
				return NormalizeSN(string(decoded))
			}
		}
	}
	return NormalizeSN(raw)
}
