package render

import (
	"strings"
	"testing"
)

func inventoryRequest() InventoryMenuRequest {
	return InventoryMenuRequest{
		Gateway:   "http://192.168.201.10:18090",
		Artifacts: "http://192.168.201.10:18082/ecc4103-e2e-r02",
		SN:        "E2E32ALMA01",
		Token:     strings.Repeat("a", 64),
		Kernel:    "inventory/vmlinuz-lts",
		Initrd:    "inventory/inventory.cpio.gz",
	}
}

func Test采集菜单的引导行(t *testing.T) {
	body, problems, err := RenderInventoryMenu(inventoryRequest())
	if err != nil || len(problems) != 0 {
		t.Fatal(err, problems)
	}
	text := string(body)
	for _, want := range []string{
		"#!ipxe\n",
		"http://192.168.201.10:18082/ecc4103-e2e-r02/inventory/vmlinuz-lts",
		"http://192.168.201.10:18082/ecc4103-e2e-r02/inventory/inventory.cpio.gz",
		"lober.gateway=http://192.168.201.10:18090",
		"lober.sn=E2E32ALMA01",
		"lober.inventory_token=" + strings.Repeat("a", 64),
		"console=ttyS0,115200n8",
		"initrd ",
		"boot\n",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("引导行缺 %q：\n%s", want, text)
		}
	}
	// 令牌不在 URL 里：查询串会被安装器当成自己的参数。
	if strings.Contains(text, "?") {
		t.Fatalf("引导行里不该有查询串：\n%s", text)
	}
}

func Test采集菜单缺配置直接报错(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*InventoryMenuRequest)
		field string
	}{
		{"缺网关", func(r *InventoryMenuRequest) { r.Gateway = "" }, "gateway"},
		{"缺制品基地址", func(r *InventoryMenuRequest) { r.Artifacts = "" }, "artifacts"},
		{"缺内核", func(r *InventoryMenuRequest) { r.Kernel = "" }, "kernel"},
		{"缺 initrd", func(r *InventoryMenuRequest) { r.Initrd = "" }, "initrd"},
		{"只有空白", func(r *InventoryMenuRequest) { r.Kernel = "   " }, "kernel"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := inventoryRequest()
			tc.apply(&r)
			body, problems, err := RenderInventoryMenu(r)
			if err != nil {
				t.Fatal(err)
			}
			if len(problems) != 1 || problems[0].Code != "render_inventory_missing" {
				t.Fatalf("得到 %v", problems)
			}
			if len(body) != 0 {
				t.Fatal("有问题时不该给出半截引导行")
			}
		})
	}
}
