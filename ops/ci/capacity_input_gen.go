//go:build ignore

// 容量测试专用。生成体积小、版本不同的导入样本。
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/infrastructure/media/isotest"
)

func main() {
	if len(os.Args) != 2 {
		panic("需要已存在的私有输出目录")
	}
	for index := 0; index < 50; index++ {
		version := fmt.Sprintf("9.%d", 60+index)
		files := map[string][]byte{
			".treeinfo":                           []byte("[general]\nname=Rocky Linux\nversion=" + version + "\narch=x86_64\npackagedir=media/Packages\n"),
			"images/pxeboot/vmlinuz":              []byte("TEST-ONLY kernel"),
			"images/pxeboot/initrd.img":           []byte("TEST-ONLY initrd"),
			"images/install.img":                  []byte("TEST-ONLY stage2"),
			"media/BaseOS/repodata/repomd.xml":    []byte("TEST-ONLY base"),
			"media/AppStream/repodata/repomd.xml": []byte("TEST-ONLY app"),
			"media/Packages/sample.rpm":           []byte("TEST-ONLY package"),
		}
		path := filepath.Join(os.Args[1], fmt.Sprintf("sample-%02d.iso", index))
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			panic(err)
		}
		_, err = file.Write(isotest.Image(files))
		closeErr := file.Close()
		if err != nil {
			panic(err)
		}
		if closeErr != nil {
			panic(closeErr)
		}
	}
}
