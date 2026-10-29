package render

import (
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

func TestUbuntuMenuPreventsISOAsCloudConfig(t *testing.T) {
	for _, firmware := range []catalog.Firmware{"bios", "uefi"} {
		t.Run(string(firmware), func(t *testing.T) {
			menu, problems, err := Render(menuFixture("ubuntu", firmware))
			if err != nil || len(problems) != 0 {
				t.Fatalf("render: %v %v", problems, err)
			}

			arguments := map[string][]string{}
			for _, line := range strings.Split(string(menu), "\n") {
				if !strings.HasPrefix(line, "kernel ") {
					continue
				}
				fields := strings.Fields(line)
				if len(fields) < 3 {
					t.Fatalf("missing installer kernel arguments: %q", line)
				}
				for _, argument := range fields[2:] {
					key, value, ok := strings.Cut(argument, "=")
					if ok {
						arguments[key] = append(arguments[key], value)
					}
				}
			}

			// 首次配置程序也会读取 url，必须明确让它跳过安装镜像。
			// 镜像地址和自动安装应答地址仍分别供原有安装流程使用。
			for key, want := range map[string]string{
				"cloud-config-url": "/dev/null",
				"url":              "http://ng.internal/artifacts/as-ubuntu2404/original.iso",
				"ds":               "nocloud-net;s=http://gw.internal/v1/answer/42/install-tok-fixed-0000000000000000/",
			} {
				got := arguments[key]
				if len(got) != 1 || got[0] != want {
					t.Errorf("kernel argument %s: got %q, want exactly %q", key, got, want)
				}
			}
		})
	}
}
