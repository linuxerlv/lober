package archtest

import (
	"testing"
	"testing/fstest"
)

func Test内置内容不扩大上下文许可(t *testing.T) {
	fsys := fstest.MapFS{
		"internal/infrastructure/builtin/x.go":        {Data: []byte(importing("context"))},
		"internal/infrastructure/builtin/nested/x.go": {Data: []byte(importing("context"))},
		"internal/domain/render/x.go":                 {Data: []byte(withImport(`"context"`, "type Reader interface { Read(context.Context) error }"))},
	}
	got, counts := checkModule(fsys, nil, []goPackage{
		{ImportPath: pkgBuiltin, GoFiles: []string{"x.go"}},
		{ImportPath: pkgBuiltin + "/nested", GoFiles: []string{"x.go"}},
		{ImportPath: pkgRender, GoFiles: []string{"x.go"}},
	})
	assertFindings(t, got, "", []finding{{ruleStd, "context"}, {ruleStd, "context"}})
	if counts[layerBuiltin] != 2 || counts[layerRender] != 1 {
		t.Fatal("没有检查所有入口", counts)
	}
	for _, pkg := range []string{pkgBuiltin, pkgBuiltin + "/nested"} {
		if got := checkLayer3Source(t, pkg, importing("embed")); len(got) != 0 {
			t.Fatalf("内置正文原有embed许可被改变：%v", got)
		}
	}
}
