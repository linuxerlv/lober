package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"
)

const (
	pkgPorts                 = modulePath + "/internal/application/ports"
	pkgTasks                 = modulePath + "/internal/application/tasks"
	pkgTasksPort             = pkgTasks + "/port"
	pkgSelectionGuard        = modulePath + "/internal/infrastructure/selection"
	pkgNotification          = modulePath + "/internal/infrastructure/notification"
	pkgCredentials           = modulePath + "/internal/infrastructure/credentials"
	pkgIdentityPort          = modulePath + "/internal/application/identity/port"
	ruleApplication     rule = "应用模块只依赖标准库、领域与自己的能力接口，不依赖网页、基础设施或其他应用服务"
	ruleApplicationPort rule = "应用能力接口只依赖标准库、领域与共同能力接口"
	ruleNotification    rule = "通知实现只依赖 sync 与共同通知契约"
)

func checkCredentialsFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	ruleCredentials := rule("凭据实现只依赖固定密码算法、令牌随机数与精确身份能力")
	found := checkImports(fset, pkg, f, func(_, imp string) rule {
		switch imp {
		case "crypto/pbkdf2", "crypto/rand", "crypto/sha256", "crypto/subtle", "encoding/base64", "encoding/hex", "strings", pkgIdentityPort, layer1 + "/access":
			return ""
		default:
			return ruleCredentials
		}
	})
	allowed := map[string]map[string]bool{}
	for _, imp := range f.Imports {
		path, _ := strconv.Unquote(imp.Path.Value)
		var names map[string]bool
		alias := ""
		switch path {
		case pkgIdentityPort:
			alias = "port"
			names = map[string]bool{"Passwords": true, "Tokens": true, "ErrInvalidPassword": true}
		case layer1 + "/access":
			alias = "access"
			names = map[string]bool{"ValidPasswordText": true}
		default:
			continue
		}
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." || alias == "_" {
			found = append(found, violation{rule: ruleCredentials, pkg: pkg, subject: path, pos: position(fset, imp.Pos())})
			continue
		}
		allowed[alias] = names
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok {
			return true
		}
		id, ok := sel.X.(*ast.Ident)
		if !ok || id.Obj != nil {
			return true
		}
		names, restricted := allowed[id.Name]
		if restricted && !names[sel.Sel.Name] {
			found = append(found, violation{rule: ruleCredentials, pkg: pkg, subject: id.Name + "." + sel.Sel.Name, pos: position(fset, sel.Pos())})
		}
		return true
	})
	return found
}

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
	if pkg == pkgTasks || pkg == pkgTasksPort {
		if under(imp, "os") || under(imp, "net") || under(imp, "database") || under(imp, "syscall") || imp == "io/fs" || imp == "io/ioutil" || under(imp, "crypto/rand") || under(imp, "math/rand") {
			return ruleApplication
		}
	}
	if under(pkg, pkgIdentityApp) || under(pkg, pkgAuditApp) {
		if imp == "os" || under(imp, "os") || under(imp, "net") || imp == "io/ioutil" || imp == "syscall" || imp == "path/filepath" || imp == "time" || under(imp, "crypto") || under(imp, "math/rand") {
			return ruleApplication
		}
	}
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
