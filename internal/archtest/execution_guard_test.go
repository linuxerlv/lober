package archtest

import (
	"go/parser"
	"go/token"
	"testing"
)

func TestMachineAndExecutionStayBehindDeclaredInterfaces(t *testing.T) {
	for _, pkg := range []string{pkgExecution, pkgMachine, pkgEvents} {
		if layerOf(pkg) == "" {
			t.Errorf("unclassified execution package %s", pkg)
		}
		for _, dependency := range []string{pkgConsole, pkgRender, pkgPgstore, pkgMachine, pkgExecution, pkgEvents, "github.com/gin-gonic/gin"} {
			if dependency == pkg {
				continue
			}
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "probe.go", "package probe\nimport _ \""+dependency+"\"", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(checkMachineFile(fset, pkg, file)) == 0 && !(pkg == pkgMachine && dependency == pkgExecution) {
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

func Test共同审计意图只开放给执行包(t *testing.T) {
	for _, pkg := range []string{pkgExecution, pkgExecution + "/hidden", pkgMachine, pkgEvents} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "probe.go", "package probe;import _ \""+pkgPorts+"\"", 0)
		if err != nil {
			t.Fatal(err)
		}
		allowed := len(checkMachineFile(fset, pkg, f)) == 0
		if allowed != (pkg == pkgExecution) {
			t.Fatal("共同能力开放范围改变", pkg, allowed)
		}
	}
}

func Test机器只新增执行应用精确依赖(t *testing.T) {
	for _, dependency := range []string{pkgExecution, pkgExecution + "/hidden", pkgExecution + "/port", pkgConsole, pkgEvents} {
		fset := token.NewFileSet()
		f, err := parser.ParseFile(fset, "probe.go", "package probe;import _ \""+dependency+"\"", 0)
		if err != nil {
			t.Fatal(err)
		}
		if (len(checkMachineFile(fset, pkgMachine, f)) == 0) != (dependency == pkgExecution) {
			t.Fatal(dependency)
		}
	}
}
