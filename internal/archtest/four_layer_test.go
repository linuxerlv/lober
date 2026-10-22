package archtest

import (
	"strings"
	"testing"
	"testing/fstest"
)

func Test基础设施拒绝接口层和应用具体服务(t *testing.T) {
	for _, suffix := range []string{"persistence/pgstore", "persistence/pgstore/pgtest", "builtin", "media", "notification", "identity", "events"} {
		pkg := modulePath + "/internal/infrastructure/" + suffix
		for _, imp := range []string{modulePath + "/internal/interfaces/console", modulePath + "/internal/application/tasks", modulePath + "/internal/application/media", modulePath + "/internal/application/ports/hidden"} {
			t.Run(suffix+"/"+imp, func(t *testing.T) {
				dir := strings.TrimPrefix(pkg, modulePath+"/")
				got, counts := checkModule(fstest.MapFS{dir + "/x.go": {Data: []byte(importing(imp))}}, nil, []goPackage{{ImportPath: pkg, GoFiles: []string{"x.go"}}})
				if len(counts) == 0 {
					t.Fatalf("新基础设施包没有接入检查：%v", got)
				}
				if len(got) == 0 {
					t.Fatal("基础设施放过接口层或应用具体服务", pkg, imp)
				}
			})
		}
	}
	// 实现共同能力可以导入这个精确的接口包，不能用禁止全部应用导入来冒充正确边界。
	pkg := modulePath + "/internal/infrastructure/notification"
	dir := strings.TrimPrefix(pkg, modulePath+"/")
	got, counts := checkModule(fstest.MapFS{dir + "/x.go": {Data: []byte(importing("sync", modulePath+"/internal/application/ports"))}}, nil, []goPackage{{ImportPath: pkg, GoFiles: []string{"x.go"}}})
	if len(got) != 0 || len(counts) == 0 {
		t.Fatal("通知实现不能使用自己的能力接口", got, counts)
	}
}

func Test未知包不能绕过四层检查(t *testing.T) {
	for _, suffix := range []string{"infrastructure", "infrastructure/unknown", "infrastructure/identity/hidden", "infrastructure/persistence/unknown", "application/ports/hidden", "interfaces/unknown", "interfaces", "domainutils", "store/pgstore", "iso", "builtin", "console"} {
		pkg := modulePath + "/internal/" + suffix
		got, counts := checkModule(fstest.MapFS{}, nil, []goPackage{{ImportPath: pkg}})
		if len(got) != 1 || got[0].rule != ruleLayer || len(counts) != 0 {
			t.Errorf("未知包没有直接失败：%s，%v，%v", pkg, got, counts)
		}
	}
	for _, suffix := range []string{"domain/model", "domain/repository/nested", "domain/render", "application/ports", "application/tasks", "application/tasks/port", "application/execution", "infrastructure/persistence/pgstore", "infrastructure/media", "infrastructure/notification", "infrastructure/events", "interfaces/console", "interfaces/machine"} {
		pkg := modulePath + "/internal/" + suffix
		got, counts := checkModule(fstest.MapFS{}, nil, []goPackage{{ImportPath: pkg}})
		if len(got) != 0 || len(counts) != 1 {
			t.Errorf("已登记包漏出检查：%s，%v，%v", pkg, got, counts)
		}
	}
}
