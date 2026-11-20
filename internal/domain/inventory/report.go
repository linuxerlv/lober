package inventory

import (
	"encoding/json"
	"errors"

	"github.com/lober/lober/internal/domain/model"
)

// ErrMalformed 表示上报的 JSON 本身就不成立：不是对象、有白名单以外的键、字段类型不对、
// 或者大小超过上限。它与 Validate 报的问题不同：Validate 说的是「值不对」，这里说的是
// 「根本没有一份像样的上报」。
var ErrMalformed = errors.New("inventory report malformed")

// maxReportBytes 是一份上报的大小上限。真实上报不到 4 KiB；留到 64 KiB 够容纳
// 一台装满盘的服务器，再大就只可能是有人在灌垃圾。
const maxReportBytes = 64 << 10

// reportJSON 是上报的白名单结构。只有这里列出的键被接受：多一个键就整体拒收，
// 不做「认识几个收几个」。这条是采集不采网络地址的第二道防线——采集脚本被人改过、
// 往上报里塞了 ip 或 mac，服务端也不会把它写进主机属性。
type reportJSON struct {
	CollectedAt string     `json:"collected_at"`
	Firmware    string     `json:"firmware"`
	CPU         cpuJSON    `json:"cpu"`
	MemoryMib   int        `json:"memory_mib"`
	System      systemJSON `json:"system"`
	Disks       []diskJSON `json:"disks"`
}

type cpuJSON struct {
	Model string `json:"model"`
	Cores int    `json:"cores"`
}

type systemJSON struct {
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
}

type diskJSON struct {
	Name       string `json:"name"`
	Bytes      int64  `json:"bytes"`
	Model      string `json:"model"`
	Rotational bool   `json:"rotational"`
}

// allowedKeys 是允许出现的全部键，按层级列出。任何一层多出键都拒收。
var allowedKeys = map[string][]string{
	"":        {"collected_at", "firmware", "cpu", "memory_mib", "system", "disks"},
	"cpu":     {"model", "cores"},
	"system":  {"manufacturer", "product"},
	"disks[]": {"name", "bytes", "model", "rotational"},
}

// forbiddenKeys 是在上报里出现就明确拒收、并给专门说明的键。它们全是网络字段：
// 采到的是装机网络的地址，对业务没有意义，写进主机属性只会让人误以为那是主机地址。
var forbiddenKeys = map[string]bool{
	"ip": true, "ips": true, "addr": true, "address": true, "addresses": true,
	"mac": true, "macs": true, "hwaddr": true, "interfaces": true, "nics": true,
	"netmask": true, "gateway": true, "dns": true, "network": true, "networks": true,
	"routes": true, "cidr": true,
}

// Parse 把一份 JSON 上报解成 Report，再交给 Validate 校验。
//
// 解析与校验分两步，是因为两者的失败含义不同：ErrMalformed 说明请求本身不成立
// （调用方回 400），Validate 的问题说明这台机器的硬件画像不可信（调用方也要拒收，
// 但记一条能看懂的原因）。两者都通过才返回结果。
func Parse(body []byte) (Result, []model.Problem, error) {
	if len(body) == 0 || len(body) > maxReportBytes {
		return Result{}, nil, ErrMalformed
	}
	if err := checkKeys(body); err != nil {
		return Result{}, nil, err
	}
	var raw reportJSON
	if err := json.Unmarshal(body, &raw); err != nil {
		return Result{}, nil, ErrMalformed
	}
	report := Report{
		Firmware:    raw.Firmware,
		CPUModel:    raw.CPU.Model,
		CPUCores:    raw.CPU.Cores,
		MemoryMib:   raw.MemoryMib,
		SystemModel: joinModel(raw.System.Manufacturer, raw.System.Product),
		Disks:       make([]Disk, 0, len(raw.Disks)),
	}
	for _, d := range raw.Disks {
		report.Disks = append(report.Disks, Disk{Name: d.Name, Bytes: d.Bytes, Model: d.Model, Rotational: d.Rotational})
	}
	result, problems := Validate(report)
	if len(problems) != 0 {
		return Result{}, problems, nil
	}
	return result, nil, nil
}

// checkKeys 逐层查键名。encoding/json 默认忽略未知键，这里不能忽略：白名单是采不到
// 网络地址的第一道防线，多出来的键必须让整个请求失败。
func checkKeys(body []byte) error {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		return ErrMalformed
	}
	for _, key := range allKeys(top) {
		if forbiddenKeys[key] {
			return ErrForbiddenKey
		}
	}
	if err := expectKeys(top, allowedKeys[""]); err != nil {
		return err
	}
	for section, allowed := range map[string][]string{"cpu": allowedKeys["cpu"], "system": allowedKeys["system"]} {
		nested, ok := top[section]
		if !ok {
			continue
		}
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(nested, &obj); err != nil {
			return ErrMalformed
		}
		for _, key := range allKeys(obj) {
			if forbiddenKeys[key] {
				return ErrForbiddenKey
			}
		}
		if err := expectKeys(obj, allowed); err != nil {
			return err
		}
	}
	if disks, ok := top["disks"]; ok {
		var items []map[string]json.RawMessage
		if err := json.Unmarshal(disks, &items); err != nil {
			return ErrMalformed
		}
		for _, item := range items {
			for _, key := range allKeys(item) {
				if forbiddenKeys[key] {
					return ErrForbiddenKey
				}
			}
			if err := expectKeys(item, allowedKeys["disks[]"]); err != nil {
				return err
			}
		}
	}
	return nil
}

// ErrForbiddenKey 表示上报里出现了网络字段。单独一个错误，好让调用方在日志里
// 说清楚「不是格式错，是有人往上报里塞了地址」。
var ErrForbiddenKey = errors.New("inventory report contains network fields")

func allKeys(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}

func expectKeys(m map[string]json.RawMessage, allowed []string) error {
	set := make(map[string]bool, len(allowed))
	for _, k := range allowed {
		set[k] = true
	}
	for k := range m {
		if !set[k] {
			return ErrMalformed
		}
	}
	return nil
}

// joinModel 把厂商与产品名拼成一行。两者都可能为空，也可能相同（有的机器 product
// 里已经带了厂商名）。
func joinModel(manufacturer, product string) string {
	switch {
	case manufacturer == "":
		return product
	case product == "":
		return manufacturer
	case manufacturer == product:
		return manufacturer
	}
	return manufacturer + " " + product
}
