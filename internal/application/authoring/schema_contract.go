package authoring

import "github.com/lober/lober/internal/domain/catalog"

// CatalogEnums 是随程序发布的五组公开目录。
type CatalogEnums struct {
	Families  []catalog.Family
	Formats   []catalog.Format
	Series    []catalog.Series
	Archs     []catalog.Arch
	Firmwares []catalog.Firmware
}

// PublicCatalog 不要求身份，不读仓储、配置或会话；每次返回原顺序的独立切片。
func PublicCatalog() CatalogEnums {
	return CatalogEnums{
		Families:  catalog.Families(),
		Formats:   catalog.AllFormats(),
		Series:    catalog.AllSeries(),
		Archs:     catalog.AllArchs(),
		Firmwares: catalog.AllFirmwares(),
	}
}
