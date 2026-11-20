package media

import (
	"reflect"
	"testing"
)

func TestESXiBootFilesPreservesOrderAndNestedPaths(t *testing.T) {
	got, ok := ESXiBootFiles("# installer\r\nprefix=/\r\nkernel=/b.b00\r\nmodules=/jumpstrt.gz --- nested/driver.v00 arg=1\r\n")
	if !ok || !reflect.DeepEqual(got, []string{"b.b00", "jumpstrt.gz", "nested/driver.v00"}) {
		t.Fatalf("启动文件应保留顺序和子目录：%v，%v", got, ok)
	}
}

func TestESXiBootFilesRejectsInvalidConfiguration(t *testing.T) {
	for _, raw := range []string{
		"kernel=/b.b00\n", "modules=/a.v00\n",
		"kernel=/b.b00\nmodules=/a.v00 ---\n",
		"kernel=/b.b00\nkernel=/b.b00\nmodules=/a.v00\n",
		"prefix=https://example.invalid/\nkernel=/b.b00\nmodules=/a.v00\n",
		"kernel=/b.b00\nmodules=https://example.invalid/a.v00\n",
		"kernel=/../b.b00\nmodules=/a.v00\n",
		"kernel=//b.b00\nmodules=/a.v00\n",
		"kernel=/b.b00\nmodules=/nested/./a.v00\n",
		"kernel=/b.b00\nmodules=/nested\\a.v00\n",
		"kernel=/b.b00\nmodules=/a.v00?extra=1\n",
		"kernel=/b.b00\nmodules=/a.v00\x00\n",
		"kernel=/b.b00\nmodules=/a.v00\nnot-a-setting\n",
	} {
		if files, ok := ESXiBootFiles(raw); ok {
			t.Fatalf("接受了无效配置：%q，%v", raw, files)
		}
	}
}
