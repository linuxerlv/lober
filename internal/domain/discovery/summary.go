package discovery

import (
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/inventory"
)

// InventorySummary 只把符合旧版校验规则的概况交给装机流程。读不全时仍保存原始部件清单。
func (r Report) InventorySummary() inventory.Result {
	var facts inventory.Report
	for _, section := range r.Sections {
		if section.Key == "storage" && strings.Contains(section.Reason, "截断") {
			continue
		}
		for _, part := range section.Items {
			f := part.Fields
			switch section.Key {
			case "system":
				if part.Name == "整机" {
					facts.SystemModel = strings.TrimSpace(f["厂商"] + " " + f["型号"])
				}
			case "cpu":
				if part.Name == "处理器概况" {
					facts.CPUModel = f["型号"]
					facts.CPUCores, _ = strconv.Atoi(f["线程数"])
				}
			case "memory":
				if kb, ok := strings.CutSuffix(f["可用总量"], " kB"); ok {
					n, _ := strconv.Atoi(kb)
					facts.MemoryMib = n / 1024
				}
			case "storage":
				if dev := installDevice(part.Name); dev != "" && (f["类型"] == "磁盘" || f["类型"] == "系统可见磁盘") {
					size, _ := strconv.ParseInt(f["容量（字节）"], 10, 64)
					facts.Disks = append(facts.Disks, inventory.Disk{Name: dev, Bytes: size, Model: f["型号"], Rotational: f["介质"] == "机械盘"})
				}
			}
		}
	}
	return inventory.PartialSummary(facts)
}
