package archtest

import (
	"go/ast"
	"go/parser"
	"go/token"
	"go/types"
	"strings"
	"testing"
	"testing/fstest"
)

func Test镜像删除领域只调用纯仓储校验(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"纯校验", "func check(){repo.CheckID();repo.CheckSource();_ = repo.ErrInvalid}", true},
		{"保存仓储", "type bad struct { store repo.Store }", false},
		{"方法值", "func bad(s repo.Store){_ = s.GetISO}", false},
		{"接口别名", "type bad = repo.ISOs", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, "internal/domain/media/check.go", "package media;import repo \""+pkgStore+"\";"+tc.body, 0)
			if err != nil {
				t.Fatal(err)
			}
			info := &types.Info{Uses: map[*ast.Ident]types.Object{}}
			imports := transitionFixtureImporter{pkgStore: "package repository;func CheckID(){};func CheckSource(){};var ErrInvalid error;type ISOs interface {GetISO()};type Store interface{ISOs}"}
			caller := layer1 + "/media"
			if _, err := (&types.Config{Importer: imports}).Check(caller, fset, []*ast.File{f}, info); err != nil {
				t.Fatal(err)
			}
			got := typedTransitionFindings("", caller, fset, []*ast.File{f}, info)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v got=%v", tc.allowed, got)
			}
		})
	}
}

func Test镜像删除领域导入只开放精确包(t *testing.T) {
	for _, tc := range []struct {
		name, caller, imp string
		allowed           bool
	}{
		{"纯校验所在包", layer1 + "/media", pkgStore, true},
		{"相邻领域包", layer1 + "/mediaother", pkgStore, false},
		{"镜像领域子包", layer1 + "/media/nested", pkgStore, false},
		{"仓储子包", layer1 + "/media", pkgStore + "/nested", false},
		{"数据库适配器", layer1 + "/media", pkgPgstore, false},
		{"选择领域限制保留", layer1 + "/selection", pkgStore, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := strings.TrimPrefix(tc.caller, modulePath+"/")
			got, counts := checkModule(fstest.MapFS{dir + "/x.go": {Data: []byte(importing(tc.imp))}}, nil, []goPackage{{ImportPath: tc.caller, GoFiles: []string{"x.go"}}})
			if counts[layerCore] != 1 {
				t.Fatalf("未检查领域包: %v", counts)
			}
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v got=%v", tc.allowed, got)
			}
		})
	}
}
