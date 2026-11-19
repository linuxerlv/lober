package discovery

import (
	"cmp"
	"regexp"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/inventory"
	"github.com/lober/lober/internal/domain/model"
)

// HostDisk 以十进制文本传容量，以免浏览器把大容量磁盘的字节数取整。
type HostDisk struct {
	Device       string `json:"device"`
	Bytes        string `json:"bytes"`
	Model        string `json:"model"`
	Serial       string `json:"serial"`
	CapacityText string `json:"capacityText,omitempty"`
}

type HostDiskReport struct {
	Disks  []HostDisk `json:"disks"`
	Status string     `json:"status"`
	Notice string     `json:"notice,omitempty"`
}

var (
	deviceName     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	excludedDevice = regexp.MustCompile(`^(loop|ram|sr|dm-)|^(zram|scd|cdrom|fd|md|nbd|zd)[0-9]*$|^(nvme[0-9]+n[0-9]+|mmcblk[0-9]+|nbd[0-9]+|md[0-9]+|zd[0-9]+)p[0-9]+$|^(sd|vd|hd|xvd)[a-z]+[0-9]+$`)
	diskBytes      = regexp.MustCompile(`^[1-9][0-9]{0,18}$`)
)

func installDevice(raw string) string {
	name := strings.TrimPrefix(strings.TrimSpace(raw), "/dev/")
	if !deviceName.MatchString(name) || excludedDevice.MatchString(name) {
		return ""
	}
	return name
}

func inventoryDisks(raw string) []HostDisk {
	var disks []HostDisk
	for line := range strings.SplitSeq(raw, "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), ":", 3)
		if len(parts) != 3 || !diskBytes.MatchString(parts[1]) {
			continue
		}
		if device := installDevice(parts[0]); device != "" {
			disks = append(disks, HostDisk{Device: device, Bytes: parts[1], Model: strings.TrimSpace(parts[2])})
		}
	}
	return disks
}

func reportedDisks(report *Report) (disks []HostDisk, section *Section) {
	if report == nil {
		return nil, nil
	}
	for i := range report.Sections {
		s := &report.Sections[i]
		if s.Key != "storage" {
			continue
		}
		for _, item := range s.Items {
			f := item.Fields
			kind := strings.TrimSpace(f["类型"])
			device := installDevice(item.Name)
			if device == "" || (kind != "" && kind != "磁盘" && kind != "系统可见磁盘") {
				continue
			}
			d := HostDisk{Device: device, Model: strings.TrimSpace(f["型号"]), Serial: strings.TrimSpace(f["序列号"])}
			if n := strings.TrimSpace(f["容量（字节）"]); diskBytes.MatchString(n) {
				d.Bytes = n
			} else {
				d.CapacityText = strings.TrimSpace(f["容量"])
			}
			if d.Bytes != "" || d.CapacityText != "" {
				disks = append(disks, d)
			}
		}
		return disks, s
	}
	return nil, nil
}

// HostDisks 只在同一采集时间合并信息。新报告已换盘时，旧序列号不能带到新盘上。
func HostDisks(attrs map[string]string, report *Report, reportAt int64) HostDiskReport {
	inv := inventoryDisks(attrs[inventory.InvDisks])
	invAt, _ := strconv.ParseInt(attrs[model.InventoryCollectedAt], 10, 64)
	found, storage := reportedDisks(report)
	notice := ""
	if storage != nil {
		if storage.Status == "partial" || storage.Status == "unread" {
			notice = cmp.Or(strings.TrimSpace(storage.Reason), "磁盘信息未完整读取。")
		}
		switch {
		case len(inv) > 0 && invAt > reportAt:
			found = nil
			notice = ""
		case reportAt > invAt && (len(found) > 0 || storage.Status == "absent" || storage.Status == "read"):
			inv = nil
		case storage.Status == "absent" && reportAt == invAt:
			inv = nil
		}
	}
	out := HostDiskReport{Disks: []HostDisk{}, Status: "none", Notice: notice}
	index := map[string]int{}
	for _, source := range [][]HostDisk{inv, found} {
		for _, d := range source {
			if i, ok := index[d.Device]; ok {
				old := out.Disks[i]
				d.Bytes = cmp.Or(d.Bytes, old.Bytes)
				d.Model = cmp.Or(d.Model, old.Model)
				d.Serial = cmp.Or(d.Serial, old.Serial)
				out.Disks[i] = d
				continue
			}
			index[d.Device] = len(out.Disks)
			out.Disks = append(out.Disks, d)
		}
	}
	if len(out.Disks) == 0 {
		return out
	}
	out.Status = "ready"
	for _, d := range out.Disks {
		if d.Serial == "" {
			out.Notice = cmp.Or(out.Notice, "部分磁盘缺少序列号，请按设备名、容量或型号核对。")
			break
		}
	}
	if out.Notice != "" {
		out.Status = "partial"
	}
	return out
}
