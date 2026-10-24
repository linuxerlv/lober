package archtest

import (
	"go/token"
	"path"
	"strings"
	"testing"
)

// migrationException 只放行一个文件中一个函数对一个旧符号的访问。
type migrationException struct {
	Caller, File, Function, Target, Symbol, RemoveIn string
}

const ruleMigration rule = "迁移豁免必须精确到调用包、文件、函数、目标包和符号，并写明总路线的清除阶段"

func checkMigrationExceptions(exceptions []migrationException) []violation {
	var found []violation
	seen := map[migrationException]bool{}
	for _, e := range exceptions {
		var reasons []string
		for _, p := range []string{e.Caller, e.Target} {
			if !strings.HasPrefix(p, modulePath+"/") || path.Clean(p) != p || strings.ContainsAny(p, "*?[]\\ \t\n\r") || strings.Contains(p, "...") {
				reasons = append(reasons, "调用包和目标包必须是本模块的完整包路径，不能通配")
			}
		}
		if path.Dir(e.File) != strings.TrimPrefix(e.Caller, modulePath+"/") || path.Clean(e.File) != e.File || !strings.HasSuffix(e.File, ".go") || strings.HasSuffix(e.File, "_test.go") || strings.ContainsAny(e.File, "*?[]\\ \t\n\r") {
			reasons = append(reasons, "文件必须是调用包中的模块相对源文件路径")
		}
		function := e.Function
		if strings.HasPrefix(function, "(") {
			receiver, name, ok := strings.Cut(function, ").")
			if !ok || !token.IsIdentifier(strings.TrimPrefix(strings.TrimPrefix(receiver, "("), "*")) {
				function = ""
			} else {
				function = name
			}
		}
		if e.Function != "package-init" && !token.IsIdentifier(function) {
			reasons = append(reasons, "函数必须是具体名称；方法使用 (类型).方法 或 (*类型).方法")
		}
		for _, symbol := range strings.Split(e.Symbol, ".") {
			if !token.IsIdentifier(symbol) {
				reasons = append(reasons, "符号必须是具体名称或 接口.方法，不能通配")
				break
			}
		}
		if len(e.RemoveIn) != 1 || e.RemoveIn < "1" || e.RemoveIn > "6" {
			reasons = append(reasons, "清除阶段只能是总路线中的 1 至 6")
		}
		if seen[e] {
			reasons = append(reasons, "重复登记了同一条豁免")
		}
		seen[e] = true
		if len(reasons) != 0 {
			found = append(found, violation{rule: ruleMigration, pkg: e.Caller, subject: e.File + ":" + e.Function + " -> " + e.Target + "." + e.Symbol, detail: strings.Join(reasons, "；")})
		}
	}
	return found
}

// migrationAllows 不按包或文件整批放行；清除阶段是登记信息，不是访问身份。
func migrationAllows(exceptions []migrationException, use migrationException) bool {
	if len(checkMigrationExceptions(exceptions)) != 0 {
		return false
	}
	for _, e := range exceptions {
		if e.Caller == use.Caller && e.File == use.File && e.Function == use.Function && e.Target == use.Target && e.Symbol == use.Symbol {
			return true
		}
	}
	return false
}

func Test迁移豁免必须精确且有清除阶段(t *testing.T) {
	valid := migrationException{pkgConsole, "internal/interfaces/console/hosts.go", "(*server).listHosts", pkgStore, "Hosts.ListHosts", "3"}
	for _, tc := range []struct {
		name   string
		change func(*migrationException)
	}{
		{"空调用方", func(e *migrationException) { e.Caller = "" }},
		{"调用方非完整路径", func(e *migrationException) { e.Caller = "console" }},
		{"空文件", func(e *migrationException) { e.File = "" }},
		{"文件越出模块", func(e *migrationException) { e.File = "../hosts.go" }},
		{"文件不属于调用方", func(e *migrationException) { e.File = "internal/other/hosts.go" }},
		{"文件通配", func(e *migrationException) { e.File = "internal/interfaces/console/*.go" }},
		{"空函数", func(e *migrationException) { e.Function = "" }},
		{"函数通配", func(e *migrationException) { e.Function = "(*server).*" }},
		{"空目标", func(e *migrationException) { e.Target = "" }},
		{"目标非完整路径", func(e *migrationException) { e.Target = "store" }},
		{"通配目标", func(e *migrationException) { e.Target += "/..." }},
		{"星号目标", func(e *migrationException) { e.Target += "/*" }},
		{"空符号", func(e *migrationException) { e.Symbol = "" }},
		{"符号通配", func(e *migrationException) { e.Symbol = "Hosts.*" }},
		{"空阶段", func(e *migrationException) { e.RemoveIn = "" }},
		{"无效阶段", func(e *migrationException) { e.RemoveIn = "7" }},
		{"非阶段编号", func(e *migrationException) { e.RemoveIn = "阶段3" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := valid
			tc.change(&e)
			if got := checkMigrationExceptions([]migrationException{e}); len(got) == 0 {
				t.Fatal("不精确或没有清除阶段的豁免被接受")
			}
		})
	}
	if got := checkMigrationExceptions([]migrationException{valid, valid}); len(got) == 0 {
		t.Error("重复豁免被接受")
	}
	for _, phase := range []string{"1", "2", "3", "4", "5", "6"} {
		e := valid
		e.RemoveIn = phase
		if got := checkMigrationExceptions([]migrationException{e}); len(got) != 0 {
			t.Errorf("总路线阶段 %s 被拒绝：%v", phase, got)
		}
	}
	decl := valid
	decl.Function = "package-init"
	decl.Symbol = "Hosts"
	if got := checkMigrationExceptions([]migrationException{valid, decl}); len(got) != 0 {
		t.Errorf("精确条目被拒绝：%v", got)
	}
	if !migrationAllows([]migrationException{valid}, valid) {
		t.Error("完全匹配的访问没有放行")
	}
	for _, tc := range []struct {
		name   string
		change func(*migrationException)
	}{
		{"同包另一处理函数", func(e *migrationException) { e.Function = "(*server).createHost" }},
		{"新业务文件", func(e *migrationException) { e.File = "internal/interfaces/console/new.go" }},
		{"另一调用方", func(e *migrationException) { e.Caller = pkgConsole + "/other" }},
		{"另一目标", func(e *migrationException) { e.Target = pkgStore + "/other" }},
		{"另一符号", func(e *migrationException) { e.Symbol = "Hosts.CreateHost" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			use := valid
			tc.change(&use)
			if migrationAllows([]migrationException{valid}, use) {
				t.Fatal("其他访问借用了豁免")
			}
		})
	}
}

// migrationExceptions 是当前非向导代码的固定清单；新增文件和函数不会自动获得豁免。
// Symbol 的 接口.方法 表示经仓储接口调用；其余名称表示旧包的类型、函数或常量。
var migrationExceptions = []migrationException{
	// 保留 dev 已有流程；逐项登记，向导不增加放行。
	{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "Events", "6"},
	{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "Tasks", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listEvents", pkgStore, "Event", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listEvents", pkgStore, "Events.ListEvents", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).taskStream", pkgStore, "Event", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).taskStream", pkgStore, "Events.ListEvents", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).taskStream", pkgStore, "Tasks.GetTask", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "toEventItem", pkgStore, "Event", "6"},
}
