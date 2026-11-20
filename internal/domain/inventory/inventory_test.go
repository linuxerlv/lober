package inventory

import (
	"strings"
	"testing"
)

// 一台典型虚拟机：三块盘，最小的是 vda 8 GiB。
func vmReport() Report {
	return Report{
		Firmware:    "bios",
		CPUModel:    "Intel(R) Xeon(R) Gold 6248R",
		CPUCores:    4,
		MemoryMib:   8192,
		SystemModel: "QEMU Standard PC (Q35 + ICH9, 2009)",
		Disks: []Disk{
			{Name: "vda", Bytes: 8589934592, Model: "QEMU HARDDISK"},
			{Name: "vdb", Bytes: 9663676416, Model: "QEMU HARDDISK"},
			{Name: "vdc", Bytes: 10737418240},
		},
	}
}

func Test采集上报选出容量最小的磁盘(t *testing.T) {
	got, problems := Validate(vmReport())
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got.SystemDisk != "vda" {
		t.Fatalf("系统盘 = %q，想要 vda", got.SystemDisk)
	}
	if got.Attrs[InvSystemDisk] != "vda" {
		t.Fatalf("属性里的系统盘 = %q", got.Attrs[InvSystemDisk])
	}
	if got.Attrs[InvCPUCores] != "4" || got.Attrs[InvMemMib] != "8192" {
		t.Fatal(got.Attrs)
	}
	if !strings.HasPrefix(got.Attrs[InvDisks], "vda:8589934592:") {
		t.Fatalf("磁盘清单 = %q", got.Attrs[InvDisks])
	}
}

// 容量相同时取名字靠前的那个，保证同一台机器报两次得到同一个结果。
func Test容量相同时按设备名排序(t *testing.T) {
	r := vmReport()
	r.Disks = []Disk{{Name: "sdb", Bytes: 100}, {Name: "sda", Bytes: 100}}
	got, problems := Validate(r)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if got.SystemDisk != "sda" {
		t.Fatalf("系统盘 = %q，平局时应取名字靠前的 sda", got.SystemDisk)
	}
	// 顺序换一下，结果不变。
	r.Disks = []Disk{{Name: "sda", Bytes: 100}, {Name: "sdb", Bytes: 100}}
	again, _ := Validate(r)
	if again.SystemDisk != got.SystemDisk {
		t.Fatalf("输入顺序改变了选中结果：%q 与 %q", again.SystemDisk, got.SystemDisk)
	}
}

func Test没有磁盘整体拒收(t *testing.T) {
	r := vmReport()
	r.Disks = nil
	got, problems := Validate(r)
	if len(problems) != 1 || problems[0].Code != "inventory_no_disk" {
		t.Fatalf("想要 inventory_no_disk，得到 %v", problems)
	}
	if got.Attrs != nil || got.SystemDisk != "" {
		t.Fatal("拒收时不能给出部分结果")
	}
}

func Test一块磁盘不合法就整体拒收(t *testing.T) {
	for _, tc := range []struct {
		name   string
		disk   Disk
		expect string
	}{
		{"容量为零", Disk{Name: "sda", Bytes: 0}, "inventory_disk_invalid"},
		{"容量超上限", Disk{Name: "sda", Bytes: maxBytes + 1}, "inventory_disk_invalid"},
		{"带路径", Disk{Name: "/dev/sda", Bytes: 100}, "inventory_disk_invalid"},
		{"大写", Disk{Name: "SDA", Bytes: 100}, "inventory_disk_invalid"},
		{"含斜杠", Disk{Name: "a/b", Bytes: 100}, "inventory_disk_invalid"},
		{"空名字", Disk{Name: "", Bytes: 100}, "inventory_disk_invalid"},
		{"型号带换行", Disk{Name: "sda", Bytes: 100, Model: "a\nb"}, "inventory_text_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := vmReport()
			r.Disks = append(r.Disks, tc.disk)
			_, problems := Validate(r)
			found := false
			for _, p := range problems {
				found = found || string(p.Code) == tc.expect
			}
			if !found {
				t.Fatalf("想要 %s，得到 %v", tc.expect, problems)
			}
		})
	}
}

func Test磁盘重复整体拒收(t *testing.T) {
	r := vmReport()
	r.Disks = append(r.Disks, r.Disks[0])
	// 同名的第二块容量改掉，免得先被重复判定之外的规则拦下。
	r.Disks[len(r.Disks)-1].Bytes++
	_, problems := Validate(r)
	if len(problems) != 1 || problems[0].Code != "inventory_disk_duplicate" {
		t.Fatalf("想要 inventory_disk_duplicate，得到 %v", problems)
	}
}

func Test磁盘块数超限(t *testing.T) {
	r := vmReport()
	for range maxDisks {
		r.Disks = append(r.Disks, Disk{Name: "x" + strings.Repeat("a", 3) + "z", Bytes: 1})
	}
	// 名字重复会先报重复；这里只关心块数超限会不会被拦。
	r.Disks = r.Disks[:0]
	for i := range maxDisks + 1 {
		r.Disks = append(r.Disks, Disk{Name: "d" + itoa(i), Bytes: int64(100 + i)})
	}
	_, problems := Validate(r)
	if len(problems) != 1 || problems[0].Code != "inventory_number_invalid" {
		t.Fatalf("想要 inventory_number_invalid，得到 %v", problems)
	}
}

func Test固件只认两种写法(t *testing.T) {
	for _, tc := range []struct {
		value string
		ok    bool
	}{{"bios", true}, {"uefi", true}, {"BIOS", false}, {"efi", false}, {"", false}} {
		r := vmReport()
		r.Firmware = tc.value
		_, problems := Validate(r)
		if (len(problems) == 0) != tc.ok {
			t.Fatalf("固件 %q：想要通过=%v，得到 %v", tc.value, tc.ok, problems)
		}
	}
}

func Test数值与文本的范围(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*Report)
		code  string
	}{
		{"核数为零", func(r *Report) { r.CPUCores = 0 }, "inventory_number_invalid"},
		{"核数超限", func(r *Report) { r.CPUCores = maxCores + 1 }, "inventory_number_invalid"},
		{"内存为零", func(r *Report) { r.MemoryMib = 0 }, "inventory_number_invalid"},
		{"内存超限", func(r *Report) { r.MemoryMib = maxMemoryMib + 1 }, "inventory_number_invalid"},
		{"型号超长", func(r *Report) { r.CPUModel = strings.Repeat("x", maxTextLen+1) }, "inventory_text_invalid"},
		{"整机型号带控制字符", func(r *Report) { r.SystemModel = "a\x00b" }, "inventory_text_invalid"},
		{"整机型号带换行", func(r *Report) { r.SystemModel = "a\nb" }, "inventory_text_invalid"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := vmReport()
			tc.apply(&r)
			_, problems := Validate(r)
			found := false
			for _, p := range problems {
				found = found || string(p.Code) == tc.code
			}
			if !found {
				t.Fatalf("想要 %s，得到 %v", tc.code, problems)
			}
		})
	}
	// 空的可选文本不算错：虚拟机常常取不到整机型号。
	r := vmReport()
	r.SystemModel = ""
	if _, problems := Validate(r); len(problems) != 0 {
		t.Fatal(problems)
	}
}

// 型号里的冒号换成下划线，否则行会被切成四段。
func Test型号里的冒号不会破坏列表格式(t *testing.T) {
	r := vmReport()
	r.Disks = []Disk{{Name: "sda", Bytes: 100, Model: "RAID: Controller"}}
	got, problems := Validate(r)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	if want := "sda:100:RAID_ Controller"; got.Attrs[InvDisks] != want {
		t.Fatalf("磁盘清单 = %q，想要 %q", got.Attrs[InvDisks], want)
	}
}

func Test采集属性的键都有前缀(t *testing.T) {
	for _, k := range InvKeys() {
		if !strings.HasPrefix(k, "inv_") {
			t.Fatalf("键 %q 没有 inv_ 前缀", k)
		}
	}
	// InvKeys 返回副本，改它不影响包里的常量。
	keys := InvKeys()
	keys[0] = "changed"
	if InvKeys()[0] == "changed" {
		t.Fatal("InvKeys 返回的不是副本")
	}
}

// itoa 用来说明磁盘名，避免为了拼名字引入 strconv。
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
