// Package inventory 是采集上报的纯规则：校验一份上报的结构，算出要写进主机属性的那几项，
// 并挑出容量最小的可安装磁盘。这里不读文件、不碰网络、不看时钟，同样的输入得到同样的结果。
package inventory

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/lober/lober/internal/domain/model"
)

// 上报里数值字段的上限。超出的一律拒收，不去截断：采集程序报出来的字节数或核数不对，
// 说明它读错了地方，写进主机属性只会让人按错的容量规划。
const (
	// maxDisks 是一台上报里允许出现的磁盘块数。真实服务器常见 8–24 块，留到 128 够用，
	// 再多说明采集程序把分区或逻辑卷也当成了磁盘。
	maxDisks = 128
	// maxBytes 是单块磁盘容量的上限，16 PiB。超过它只可能是读错了单位或溢出了。
	maxBytes = 1 << 54
	// maxCores 是 CPU 核数上限。
	maxCores = 8192
	// maxMemoryMib 是内存上限，4 TiB。
	maxMemoryMib = 4 << 20
	// maxTextLen 是 CPU 型号、整机型号这类单行文本的长度上限。
	maxTextLen = 128
)

// Report 是采集程序上报的硬件事实。字段与 /v1/inventory 的 JSON 一一对应。
//
// 这里刻意没有任何网络字段：主机在装机网络的地址对业务没有意义，采了只会误导。
// 上报里出现 ip、mac 这类键由解析层拒收，不进到这个结构里。
type Report struct {
	// Firmware 是采集时看到的固件类型，取值 bios 或 uefi。
	Firmware string
	// CPUModel 是处理器型号，例如 "Intel(R) Xeon(R) Gold 6248R"。
	CPUModel string
	// CPUCores 是逻辑核数。
	CPUCores int
	// MemoryMib 是 /proc/meminfo 的 MemTotal，单位 MiB。
	MemoryMib int
	// SystemModel 是整机厂商与型号，例如 "Dell Inc. PowerEdge R750"。
	SystemModel string
	// Disks 是真实块设备，不含 loop、ram、光盘与设备映射。
	Disks []Disk
}

// Disk 是一块块设备。
type Disk struct {
	// Name 是内核里的设备名，不带 /dev/ 前缀，例如 vda、sda、nvme0n1。
	Name string
	// Bytes 是容量，字节。
	Bytes int64
	// Model 是硬件型号，可能为空（虚拟机的 virtio 盘就没有）。
	Model string
	// Rotational 报告是不是机械盘。缩容、选盘的策略以后可能用到，现在只记录。
	Rotational bool
}

// Result 是一份通过校验的上报算出来的主机属性。
type Result struct {
	// Attrs 是要合并进主机属性的键值，键都以 inv_ 开头。
	Attrs map[string]string
	// SystemDisk 是容量最小的可安装磁盘的设备名。
	SystemDisk string
}

// Validate 校验一份上报，通过时返回要写进主机属性的值与选中的系统盘。
//
// 拒收的条件都是「采集程序或硬件本身有问题」，不是「值不好看」：没有磁盘、磁盘容量为零、
// 设备名不是内核的写法、文本超长或含控制字符。这些都让上报整体失败，不做部分采纳——
// 部分采纳会让主机记下一份半对半错的画像，比没有画像更危险。
func Validate(r Report) (Result, []model.Problem) {
	var out []model.Problem
	subject := model.Subject{Kind: model.KindHost}

	if r.Firmware != "bios" && r.Firmware != "uefi" {
		out = append(out, problem("inventory_firmware_invalid", subject, r.Firmware))
	}
	if p := checkText(r.CPUModel, "cpu_model"); p != nil {
		out = append(out, *p)
	}
	if r.CPUCores < 1 || r.CPUCores > maxCores {
		out = append(out, problem("inventory_number_invalid", subject, "cpu_cores", strconv.Itoa(r.CPUCores)))
	}
	if r.MemoryMib < 1 || r.MemoryMib > maxMemoryMib {
		out = append(out, problem("inventory_number_invalid", subject, "mem_mib", strconv.Itoa(r.MemoryMib)))
	}
	if p := checkText(r.SystemModel, "system_model"); p != nil {
		out = append(out, *p)
	}

	disks, ps := checkDisks(r.Disks, subject)
	out = append(out, ps...)
	if len(out) != 0 {
		return Result{}, out
	}

	smallest := disks[0]
	for _, d := range disks[1:] {
		// 容量相同时取名字字典序靠前的，保证同一台机器报两次得到同一个结果。
		if d.Bytes < smallest.Bytes || d.Bytes == smallest.Bytes && d.Name < smallest.Name {
			smallest = d
		}
	}

	return Result{
		Attrs: map[string]string{
			InvCPUMode:    r.CPUModel,
			InvCPUCores:   strconv.Itoa(r.CPUCores),
			InvMemMib:     strconv.Itoa(r.MemoryMib),
			InvSystemMode: r.SystemModel,
			InvDisks:      joinDisks(disks),
			InvSystemDisk: smallest.Name,
		},
		SystemDisk: smallest.Name,
	}, nil
}

// 写进主机属性的键。集中在这里，改动一处就够。
const (
	InvCPUMode    = "inv_cpu_model"
	InvCPUCores   = "inv_cpu_cores"
	InvMemMib     = "inv_mem_mib"
	InvSystemMode = "inv_system_model"
	InvDisks      = "inv_disks"
	InvSystemDisk = "inv_system_disk"
)

// InvKeys 返回全部采集属性的键，供控制台区分「采集来的」与「人工填的」。返回的是副本。
func InvKeys() []string {
	return []string{InvCPUMode, InvCPUCores, InvMemMib, InvSystemMode, InvDisks, InvSystemDisk}
}

// checkDisks 查磁盘清单。设备名重复、容量为零、名字不是内核写法、块数超限，都整体拒收。
func checkDisks(disks []Disk, subject model.Subject) ([]Disk, []model.Problem) {
	if len(disks) == 0 {
		// 这不是「机器没硬盘」的正常情况：装机目标机不可能一块盘都没有。多半是采集镜像
		// 缺了存储驱动，内核没枚举出块设备。要让人在机柜前就知道，而不是等建任务时盘名取不到值。
		return nil, []model.Problem{problem("inventory_no_disk", subject)}
	}
	if len(disks) > maxDisks {
		return nil, []model.Problem{problem("inventory_number_invalid", subject, "disks", strconv.Itoa(len(disks)))}
	}
	seen := make(map[string]bool, len(disks))
	out := make([]Disk, 0, len(disks))
	var problems []model.Problem
	for _, d := range disks {
		switch {
		case !validDiskName(d.Name):
			problems = append(problems, problem("inventory_disk_invalid", subject, d.Name))
			continue
		case seen[d.Name]:
			problems = append(problems, problem("inventory_disk_duplicate", subject, d.Name))
			continue
		case d.Bytes < 1 || d.Bytes > maxBytes:
			problems = append(problems, problem("inventory_disk_invalid", subject, d.Name))
			continue
		}
		if p := checkText(d.Model, "disk_model"); p != nil {
			p.Subject = subject
			problems = append(problems, *p)
			continue
		}
		seen[d.Name] = true
		out = append(out, d)
	}
	if len(problems) != 0 {
		return nil, problems
	}
	return out, nil
}

// validDiskName 认内核给块设备起的名字：小写字母开头，只含小写字母、数字和连字符。
// 不认路径（/dev/sda）、不认斜杠、不认点——采集程序报的是 /sys/block 下的目录名。
func validDiskName(s string) bool {
	if s == "" || len(s) > 32 || s[0] < 'a' || s[0] > 'z' {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if (c < 'a' || c > 'z') && (c < '0' || c > '9') && c != '-' {
			return false
		}
	}
	return true
}

// checkText 查一条可选的单行文本：空着可以，非空时要是可存文本、不超过上限、不含控制字符。
//
// 换行与制表符也算控制字符：CPU 型号、整机型号、磁盘型号都是单行字段，换行会把磁盘清单
// 的一项拆成两行，制表符进了 HTML 与终端也只会错位。采集来的值不能盲信。
func checkText(s, field string) *model.Problem {
	if s == "" {
		return nil
	}
	if !model.StorableText(s) || len(s) > maxTextLen || hasControl(s) {
		p := problem("inventory_text_invalid", model.Subject{Kind: model.KindHost}, field)
		return &p
	}
	return nil
}

func hasControl(s string) bool {
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return true
		}
	}
	return false
}

// joinDisks 把磁盘清单写成每行一项的列表，格式为「名字:字节数:型号」。型号为空时留空，
// 行尾不补冒号的多余部分：解析回来时按两个冒号切三段，第三段可以为空。
//
// 型号里的冒号与换行必须挡掉，否则同一行会被解析成四段、或一项变成两行。checkText 已经
// 挡掉换行与控制字符，冒号额外在这里换掉。
func joinDisks(disks []Disk) string {
	lines := make([]string, 0, len(disks))
	for _, d := range disks {
		lines = append(lines, fmt.Sprintf("%s:%d:%s", d.Name, d.Bytes, strings.ReplaceAll(d.Model, ":", "_")))
	}
	return strings.Join(lines, "\n")
}

func problem(code model.Code, subject model.Subject, args ...any) model.Problem {
	return model.NewProblem(code, subject, args...)
}

// ErrorKind 是采集上报被拒的类别。执行模块按它决定 HTTP 状态码，采集模块按它给自己的
// 错误分类；两个应用模块互不依赖，只共享这一份来自领域层的词表。
type ErrorKind string

const (
	// KindUnauthorized 是令牌不对、超时或没发过。
	KindUnauthorized ErrorKind = "unauthorized"
	// KindUnknownHost 是这个 SN 没登记过。
	KindUnknownHost ErrorKind = "unknown-host"
	// KindBusy 是主机有打开的任务，此刻不接受采集。
	KindBusy ErrorKind = "busy"
	// KindRejected 是上报格式对但值不可信。
	KindRejected ErrorKind = "rejected"
)

// Kinded 是带类别的错误。采集应用把它包在返回的错误上，执行模块用 KindOf 取类别。
type Kinded struct {
	Kind  ErrorKind
	Cause error
}

func (e *Kinded) Error() string { return string(e.Kind) }
func (e *Kinded) Unwrap() error { return e.Cause }

// KindOf 取错误的类别。错误链上没有类别时返回空与 false。
func KindOf(err error) (ErrorKind, bool) {
	var k *Kinded
	if errors.As(err, &k) {
		return k.Kind, true
	}
	return "", false
}
