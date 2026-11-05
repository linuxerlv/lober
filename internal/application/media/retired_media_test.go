package media_test

import (
	"context"
	"testing"
)

func TestIdentifyRejectsRetiredLinuxMedia(t *testing.T) {
	for _, product := range []string{"openEuler", "OpenCloudOS", "SUSE Linux Enterprise Server", "openSUSE Leap"} {
		t.Run(product, func(t *testing.T) {
			_, err := Identify(context.Background(), identifyTree(map[string]string{".treeinfo": "[general]\nname=" + product + "\nversion=15.6\narch=x86_64\n"}))
			if err == nil {
				t.Fatal("已停用镜像仍被识别为可安装系统")
			}
		})
	}
	for _, product := range []string{"SLES", "openSUSE"} {
		for _, live := range []bool{false, true} {
			files := map[string]string{"media.1/products": product + "|15.6|Server", "repodata/repomd.xml": "repo", "boot/x86_64/loader/linux": "kernel"}
			if live {
				files["LiveOS/squashfs.img"] = "live"
			}
			if _, err := Identify(context.Background(), identifyTree(files)); err == nil {
				t.Errorf("已停用镜像 %s live=%v 仍被接受", product, live)
			}
		}
	}
}
