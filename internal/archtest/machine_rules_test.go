package archtest

import (
	"go/ast"
	"go/token"
)

const ruleMachine rule = "机器接口只转换并委托网关，日志适配器只接收观察与日志能力，执行应用遵守应用边界"

func checkMachineFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	if pkg == pkgExecution || pkg == pkgExecutionPort {
		return checkApplicationFile(fset, pkg, f)
	}
	return checkImports(fset, pkg, f, func(_, imp string) rule {
		allowed := isStd(imp) && imp != "C" && imp != "unsafe" && !applicationExternalImport(imp) || imp == "net/http" || imp == "net/url" || imp == "net/netip"
		if under(pkg, pkgMachine) {
			allowed = allowed || under(imp, pkgMachine) || imp == pkgExecution || imp == pkgModel || imp == pkgCatalog || imp == pkgExecutionRules
		} else if under(pkg, pkgEvents) {
			// TCP 的有界换行、空闲期限和异常保护只增加这三个标准库。
			allowed = imp == "context" || imp == "net" || imp == "net/netip" || imp == "sync" || imp == "bufio" || imp == "time" || imp == "errors" || imp == pkgModel || imp == pkgExecutionPort || under(imp, pkgEvents)
		}
		if allowed {
			return ""
		}
		return ruleMachine
	})
}

func checkMachineKeysFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkImports(fset, pkg, f, func(_, imp string) rule {
		switch imp {
		case "crypto/rand", "errors", "io", "os", "path/filepath":
			return ""
		}
		return rule("机器密钥只依赖原私密文件操作需要的精确标准库")
	})
}
