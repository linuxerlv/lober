package archtest

import (
	"go/parser"
	"go/token"
	"testing"
	"testing/fstest"
)

func Test领域拒绝随机数和外部操作(t *testing.T) {
	for _, pkg := range []string{layer1 + "/media", pkgModel, pkgStore, pkgStore + "/nested", layer1 + "/selection", layer1 + "/access", pkgRender} {
		for _, imp := range []string{"crypto/rand", "math/rand", "io/fs", "os", "io/ioutil", "net", "net/http", "time", gin, "gorm.io/gorm", pgxV5, pkgPgstore, modulePath + "/internal/infrastructure/identity", pkgConsole} {
			t.Run(pkg+"/"+imp, func(t *testing.T) {
				check := checkSource
				if under(pkg, pkgStore) {
					check = checkStoreSource
				}
				if under(pkg, pkgRender) {
					check = checkLayer3Source
				}
				if got := check(t, pkg, importing(imp)); len(got) == 0 {
					t.Fatalf("领域包 %s 放过了外部操作 %s", pkg, imp)
				}
			})
		}
	}
	for _, imp := range []string{"context", "math", "reflect", "encoding/base64", "encoding/json"} {
		if got := checkStoreSource(t, pkgStore, importing(imp)); len(got) != 0 {
			t.Errorf("纯计算依赖 %s 被拒绝：%v", imp, got)
		}
	}
}

func Test领域总检查包含仓储子目录和编号调用(t *testing.T) {
	fsys := fstest.MapFS{
		"internal/domain/repository/nested/x.go":               {Data: []byte(importing("crypto/rand", "os", "net/http"))},
		"internal/domain/selection/x.go":                       {Data: []byte(importing(pkgStore))},
		"internal/domain/render/x.go":                          {Data: []byte(importing(pkgStore))},
		"internal/infrastructure/persistence/pgstore/extra.go": {Data: []byte(withImport(`id "`+pkgIdentity+`"`, "func (s *Store) createTasks() { _ = id.NewID() }"))},
	}
	got, counts := checkModule(fsys, nil, []goPackage{
		{ImportPath: pkgStore + "/nested", GoFiles: []string{"x.go"}},
		{ImportPath: layer1 + "/selection", GoFiles: []string{"x.go"}},
		{ImportPath: pkgRender, GoFiles: []string{"x.go"}},
		{ImportPath: pkgPgstore, GoFiles: []string{"extra.go"}},
	})
	assertFindings(t, got, "", []finding{{ruleContract, "crypto/rand"}, {ruleContract, "os"}, {ruleContract, "net/http"}, {ruleOutside, pkgStore}, {ruleLayer3, pkgStore}, {ruleCapability, pkgIdentity + ".NewID"}})
	if counts[layerStore] != 2 || counts[layerCore] != 1 || counts[layerRender] != 1 {
		t.Fatal("领域分类遗漏", counts)
	}
}

func Test编号正式能力必须精确到实际调用(t *testing.T) {
	const target = "github.com/lober/lober/internal/infrastructure/identity"
	for _, tc := range []struct {
		name, file, body string
		allowed          bool
	}{
		{"指定方法", "internal/infrastructure/persistence/pgstore/tasks.go", "func (s *Store) createTasks() { _ = id.NewID() }", true},
		{"方法中的函数值", "internal/infrastructure/persistence/pgstore/tasks.go", "func (s *Store) createTasks() { next := id.NewID; _ = next }", true},
		{"同文件其他方法", "internal/infrastructure/persistence/pgstore/tasks.go", "func (s *Store) extra() { _ = id.NewID() }", false},
		{"同方法其他文件", "internal/infrastructure/persistence/pgstore/extra.go", "func (s *Store) createTasks() { _ = id.NewID() }", false},
		{"同方法其他符号", "internal/infrastructure/persistence/pgstore/tasks.go", "func (s *Store) createTasks() { _ = id.Other() }", false},
		{"包级函数值", "internal/infrastructure/persistence/pgstore/tasks.go", "var next = id.NewID", false},
		{"点导入", "internal/infrastructure/persistence/pgstore/tasks.go", "func (s *Store) createTasks() { _ = NewID() }", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			alias := "id"
			if tc.name == "点导入" {
				alias = "."
			}
			fset := token.NewFileSet()
			f, err := parser.ParseFile(fset, tc.file, withImport(alias+" \""+target+"\"", tc.body), 0)
			if err != nil {
				t.Fatal(err)
			}
			got := checkStoreFile(fset, pkgPgstore, f)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("准许=%v，违规=%v", tc.allowed, got)
			}
		})
	}
}
