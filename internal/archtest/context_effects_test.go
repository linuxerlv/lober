package archtest

import (
	"go/ast"
	"go/token"
	"strconv"
)

const ruleContextEffects rule = "领域只使用上下文契约，不用 context 的计时器、异步回调或点导入"

// context.Context 可以出现在接口中；下面这些函数会借用时钟或启动异步工作。
var contextEffects = map[string]bool{
	"WithTimeout": true, "WithTimeoutCause": true,
	"WithDeadline": true, "WithDeadlineCause": true, "AfterFunc": true,
}

func checkContextEffects(fset *token.FileSet, pkg string, f *ast.File) []violation {
	if !under(pkg, layer1) {
		return nil
	}
	var found []violation
	report := func(subject string, pos token.Pos) {
		found = append(found, violation{rule: ruleContextEffects, pkg: pkg, subject: subject, pos: position(fset, pos)})
	}
	aliases := map[string]bool{}
	for _, imp := range f.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		if name != "context" {
			continue
		}
		alias := "context"
		if imp.Name != nil {
			alias = imp.Name.Name
		}
		if alias == "." {
			// 点导入会丢失调用来源，不能让它绕过对计时函数的检查。
			report("context", imp.Pos())
			continue
		}
		aliases[alias] = true
	}
	if len(aliases) == 0 {
		return found
	}
	ast.Inspect(f, func(n ast.Node) bool {
		sel, ok := n.(*ast.SelectorExpr)
		if !ok || !contextEffects[sel.Sel.Name] {
			return true
		}
		if name, ok := sel.X.(*ast.Ident); ok && aliases[name.Name] && name.Obj == nil {
			// 解析器会绑定局部声明；只有未绑定的导入名才代表 context 包。
			// 直接调用与保存成函数值都会访问同一个选择器。
			report("context."+sel.Sel.Name, sel.Pos())
		}
		return true
	})
	return found
}
