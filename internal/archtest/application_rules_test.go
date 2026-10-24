package archtest

import (
	"go/ast"
	"go/token"
	"strings"
)

const (
	pkgPorts                 = modulePath + "/internal/application/ports"
	pkgTasks                 = modulePath + "/internal/application/tasks"
	pkgTasksPort             = pkgTasks + "/port"
	pkgSelectionGuard        = modulePath + "/internal/infrastructure/selection"
	pkgNotification          = modulePath + "/internal/infrastructure/notification"
	ruleApplication     rule = "应用模块只依赖标准库、领域与自己的能力接口，不依赖网页、基础设施或其他应用服务"
	ruleApplicationPort rule = "应用能力接口只依赖标准库、领域与共同能力接口"
	ruleNotification    rule = "通知实现只依赖 sync 与共同通知契约"
)

// 只认已设计的模块入口和精确接口子包；新模块必须先登记，不能借四层根目录绕过。
func applicationModule(pkg string) string {
	if pkg == pkgPorts {
		return "ports"
	}
	for _, name := range []string{"tasks", "hosts", "authoring", "media", "identity", "audit"} {
		base := modulePath + "/internal/application/" + name
		if pkg == base || pkg == base+"/port" {
			return name
		}
	}
	return ""
}

func checkApplicationFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkImports(fset, pkg, f, applicationRule)
}

func applicationRule(pkg, imp string) rule {
	port := pkg == pkgPorts || strings.HasSuffix(pkg, "/port")
	if pkg == modulePath+"/internal/application/media" || pkg == modulePath+"/internal/application/media/port" {
		if imp == "os" || imp == "io/ioutil" || under(imp, "net") || under(imp, "os") || imp == "syscall" || imp == "path/filepath" {
			return ruleApplication
		}
	}
	if isStd(imp) && imp != "C" && imp != "unsafe" || under(imp, layer1) || imp == pkgPorts {
		return ""
	}
	if !port && imp == pkg+"/port" {
		return ""
	}
	if port {
		return ruleApplicationPort
	}
	return ruleApplication
}

func checkNotificationFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkImports(fset, pkg, f, func(_, imp string) rule {
		if imp == "sync" || imp == pkgPorts {
			return ""
		}
		return ruleNotification
	})
}

func checkSelectionFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	return checkImports(fset, pkg, f, func(_, imp string) rule {
		if imp == "sync" || imp == pkgPorts {
			return ""
		}
		return rule("选择锁只依赖 sync 与共同能力契约")
	})
}
