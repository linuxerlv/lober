package archtest

import (
	"testing"
	"testing/fstest"
)

func TestOpenAPIExporterNarrowDependencies(t *testing.T) {
	pkg := modulePath + "/cmd/console-openapi"
	wantRule := rule("OpenAPI 导出工具只依赖 encoding/json、os、gin 与控制台包")
	for _, imp := range []string{"encoding/json", "os", gin, pkgConsole, "time", pkgStore, pkgPgstore, pkgAssembly, pkgModel, pkgConsole + "/hidden", gin + "/binding", "C", "unsafe"} {
		t.Run(imp, func(t *testing.T) {
			fsys := fstest.MapFS{"cmd/console-openapi/main.go": {Data: []byte(importing(imp))}}
			got, checked := checkModule(fsys, []string{"cmd/console-openapi"}, []goPackage{{ImportPath: pkg, GoFiles: []string{"main.go"}}})
			allowed := imp == "encoding/json" || imp == "os" || imp == gin || imp == pkgConsole
			var want []finding
			if !allowed {
				want = []finding{{wantRule, imp}}
			}
			assertFindings(t, got, "", want)
			if checked[layer("契约导出")] != 1 || layerOf(pkg+"/hidden") != "" {
				t.Fatalf("exporter must be registered as its own exact package: %v", checked)
			}
		})
	}
}

func TestOpenAPIExporterChecksIgnoredSources(t *testing.T) {
	pkg := modulePath + "/cmd/console-openapi"
	fsys := fstest.MapFS{"cmd/console-openapi/hidden_linux.go": {Data: []byte(importing("time"))}}
	got, _ := checkModule(fsys, []string{"cmd/console-openapi"}, []goPackage{{ImportPath: pkg, IgnoredGoFiles: []string{"hidden_linux.go"}}})
	assertFindings(t, got, "", []finding{{ruleIgnored, "hidden_linux.go"}, {rule("OpenAPI 导出工具只依赖 encoding/json、os、gin 与控制台包"), "time"}})
}
