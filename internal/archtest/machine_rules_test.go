package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
)

const ruleMachine rule = "执行状态机与机器网关只依赖标准库、core、store 契约与各自包；跨模块通过声明接口和装配点连接"

func checkMachineFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	for _, imp := range f.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		own := pkgExecution
		if under(pkg, pkgMachine) {
			own = pkgMachine
		}
		if under(pkg, pkgEvents) {
			own = pkgEvents
		}
		allowed := (isStd(name) && name != "C" && name != "unsafe") || inLayer1(name) || name == pkgStore || under(name, own) || pkg == pkgExecution && name == pkgPorts
		if !allowed {
			found = append(found, violation{rule: ruleMachine, pkg: pkg, subject: name, pos: position(fset, imp.Pos())})
		}
	}
	return found
}
