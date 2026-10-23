package pgstore

import (
	"encoding/json"
	"fmt"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

// 行类型只在本包：第 1 层的 model 类型不带 tag（第 1 层修补时 T7-4 定的），字段改名由 model 的 golden 测试守住。
// 索引与约束的 tag 要与 schema.go 的期望值一致，由每次 Open 的表结构断言比对。复合索引的每个列都要标同名的 tag，
// 漏标一个就会静默建成单列索引。

// hostRow 是 hosts 表的一行。
type hostRow struct {
	SN       string `gorm:"primaryKey"`
	Arch     string `gorm:"not null"`
	Firmware string `gorm:"not null"`
	Attrs    string `gorm:"type:jsonb;not null"` // map[string]string 的 JSON
	Rev      int64  `gorm:"not null;check:rev >= 1"`
}

func (hostRow) TableName() string { return "hosts" }

// isoRow 是 isos 表的一行。SHA256 的部分唯一索引守着 C4：空的与导入失败的不占位，导入失败的 ISO 因此能记下与别的
// ISO 相同的内容，指向那一个（spec §5.1 第 3 步）。
type isoRow struct {
	ID        string `gorm:"primaryKey"`
	Name      string `gorm:"not null"`
	Family    string `gorm:"not null"`
	Series    string `gorm:"not null"`
	Version   string `gorm:"not null"`
	Arch      string `gorm:"not null"`
	State     string `gorm:"not null;check:state IN ('acquiring', 'importing', 'ready', 'failed', 'deprecated')"`
	SHA256    string `gorm:"not null;uniqueIndex:idx_isos_sha256,where:sha256 <> '' AND state <> 'failed'"`
	Artifacts string `gorm:"type:jsonb;not null"` // map[string]string 的 JSON
	Offers    string `gorm:"type:jsonb;not null"` // map[string][]model.Option 的 JSON
	Rev       int64  `gorm:"not null;check:rev >= 1"`
}

func (isoRow) TableName() string { return "isos" }

func toHostRow(h model.Host) (hostRow, error) {
	attrs, err := encodeJSON(h.Attrs)
	if err != nil {
		return hostRow{}, err
	}
	return hostRow{SN: h.SN, Arch: string(h.Arch), Firmware: string(h.Firmware), Attrs: attrs, Rev: h.Rev}, nil
}

// host 把一行转回 model.Host。属性每次都从 JSON 解出新的 map，所以返回值就是深拷贝。
func (r hostRow) host() (model.Host, error) {
	h := model.Host{SN: r.SN, Arch: catalog.Arch(r.Arch), Firmware: catalog.Firmware(r.Firmware), Rev: r.Rev}
	if err := decodeJSON(r.Attrs, &h.Attrs); err != nil {
		return model.Host{}, fmt.Errorf("主机 %s 的属性：%w", r.SN, err)
	}
	return h, nil
}

func toISORow(iso model.ISO) (isoRow, error) {
	artifacts, err := encodeJSON(iso.Artifacts)
	if err != nil {
		return isoRow{}, err
	}
	offers, err := encodeJSON(iso.Offers)
	if err != nil {
		return isoRow{}, err
	}
	return isoRow{
		ID: iso.ID, Name: iso.Name, Family: string(iso.Family), Series: string(iso.Series), Version: iso.Version,
		Arch: string(iso.Arch), State: string(iso.State), SHA256: iso.SHA256, Artifacts: artifacts, Offers: offers,
		Rev: iso.Rev,
	}, nil
}

// iso 把一行转回 model.ISO，制品与提供的集合同样每次解出新的值。
func (r isoRow) iso() (model.ISO, error) {
	iso := model.ISO{
		ID: r.ID, Name: r.Name, Family: catalog.Family(r.Family), Series: catalog.Series(r.Series), Version: r.Version,
		Arch: catalog.Arch(r.Arch), State: model.ISOState(r.State), SHA256: r.SHA256, Rev: r.Rev,
	}
	if err := decodeJSON(r.Artifacts, &iso.Artifacts); err != nil {
		return model.ISO{}, fmt.Errorf("ISO %s 的制品：%w", r.ID, err)
	}
	if err := decodeJSON(r.Offers, &iso.Offers); err != nil {
		return model.ISO{}, fmt.Errorf("ISO %s 提供的集合：%w", r.ID, err)
	}
	return iso, nil
}

// encodeJSON 把嵌套的 model 值编成 jsonb 的文本。nil 的 map 与切片编成 null，空的编成 {} 与 []：写进去什么就读出
// 什么，两者读回来才分得开。文本存不存得下由调用方先查，这里编出来的不会被换成 U+FFFD。
func encodeJSON(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", fmt.Errorf("pgstore：编 JSON：%w", err)
	}
	return string(b), nil
}

// decodeJSON 把 jsonb 的文本解进 v 指向的值：null 解成 nil，空的对象与数组解成空的 map 与切片。
func decodeJSON(s string, v any) error {
	if err := json.Unmarshal([]byte(s), v); err != nil {
		return fmt.Errorf("pgstore：解 JSON：%w", err)
	}
	return nil
}
