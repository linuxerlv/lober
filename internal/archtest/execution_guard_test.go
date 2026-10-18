package archtest

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestMachineAndExecutionStayBehindDeclaredInterfaces(t *testing.T) {
	for _, pkg := range []string{modulePath + "/internal/execution", modulePath + "/internal/machine", modulePath + "/internal/events"} {
		if layerOf(pkg) == "" {
			t.Errorf("unclassified execution package %s", pkg)
		}
		for _, dependency := range []string{pkgConsole, pkgRender, pkgPgstore, modulePath + "/internal/machine", modulePath + "/internal/execution", modulePath + "/internal/events", "github.com/gin-gonic/gin"} {
			if dependency == pkg {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "probe.go", "package probe\nimport _ \""+dependency+"\"", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(checkMachineFile(fset, pkg, file)) == 0 {
				t.Errorf("%s may import %s", pkg, dependency)
			}
		}
		for _, dependency := range []string{"context", "net/http", pkgStore, pkgModel, pkgCatalog} {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "probe.go", "package probe\nimport _ \""+dependency+"\"", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(checkMachineFile(fset, pkg, file)) != 0 {
				t.Errorf("declared contract dependency rejected: %s", dependency)
			}
		}
	}
}
