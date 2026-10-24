package archtest

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
)

// 第六阶段起点逐字段冻结；仅供历史重建，不能用于当前放行。
var phaseSixOriginalEntries = map[string][]migrationException{
	"migrationExceptions": {
		// 保留 dev 已有流程；逐项登记，向导不增加放行。
		{pkgConsole, "internal/interfaces/console/pagination.go", "queryPagination", "github.com/lober/lober/internal/domain/repository", "CheckPageQuery", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "BatchError", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrConflict", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrDuplicate", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrImmutable", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrInvalid", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrLastAdmin", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrNotFound", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrReferenced", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ProblemsError", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ReferencedError", "6"},
		{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "Events", "6"},
		{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "Tasks", "6"},
		{pkgConsole, "internal/interfaces/console/taskcontrol.go", "Problems", pkgStore, "ErrRejected", "6"},
		{pkgConsole, "internal/interfaces/console/taskcontrol.go", "Problems", pkgStore, "ProblemsError", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).controlTask", pkgStore, "Tasks.GetTask", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).getTask", pkgStore, "Tasks.GetTask", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listEvents", pkgStore, "Event", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listEvents", pkgStore, "Events.ListEvents", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", pkgStore, "Page", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", pkgStore, "TaskQuery", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", pkgStore, "Tasks.ListTasks", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).previewTask", pkgStore, "Tasks.GetTask", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).taskStream", pkgStore, "Event", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).taskStream", pkgStore, "Events.ListEvents", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).taskStream", pkgStore, "Tasks.GetTask", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "toEventItem", pkgStore, "Event", "6"},
	},
	"mediaMigrationExceptions": {},
	"consoleRuleMigrationExceptions": {
		// 保留 dev 已有流程；逐项登记，向导不增加放行。
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", "github.com/lober/lober/internal/domain/repository", "ErrTemplateNotCurrent", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "StatusOfProblems", "github.com/lober/lober/internal/domain/model", "Code.Category", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllArchs", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllFirmwares", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllFormats", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllSeries", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "Families", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/model", "Roles", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "NormalizeSN", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "TaskState.Valid", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).previewTask", "github.com/lober/lober/internal/domain/catalog", "Firmware.Valid", "6"},
	},
	"notificationMigrationExceptions": {
		{pkgConsole, "internal/interfaces/console/notify.go", "NewHub", pkgNotification, "NewHub", "6"},
	},
}

// 只给历史检查提供副本；当前表非法时拒绝恢复，不能掩盖新增、改字段或调序。
func phaseFiveBaselineEntries(table string, current []migrationException) []migrationException {
	original, ok := phaseSixOriginalEntries[table]
	if !ok {
		return nil
	}
	next := 0
	for _, e := range current {
		for next < len(original) && original[next] != e {
			next++
		}
		if next == len(original) {
			return nil
		}
		next++
	}
	out := make([]migrationException, len(original))
	copy(out, original)
	return out
}

// 每个任务完成后只追加明确删除项；当前表仍必须是原表扣除这些项的原序结果。
var phaseSixCompletedRemovals = map[string][]migrationException{
	"migrationExceptions": {
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "BatchError", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrConflict", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrDuplicate", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrImmutable", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrInvalid", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrLastAdmin", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrNotFound", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrReferenced", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ProblemsError", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ReferencedError", "6"},
		{pkgConsole, "internal/interfaces/console/pagination.go", "queryPagination", "github.com/lober/lober/internal/domain/repository", "CheckPageQuery", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).getTask", pkgStore, "Tasks.GetTask", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", pkgStore, "Page", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", pkgStore, "TaskQuery", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", pkgStore, "Tasks.ListTasks", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).previewTask", pkgStore, "Tasks.GetTask", "6"},
		{pkgConsole, "internal/interfaces/console/taskcontrol.go", "Problems", pkgStore, "ErrRejected", "6"},
		{pkgConsole, "internal/interfaces/console/taskcontrol.go", "Problems", pkgStore, "ProblemsError", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).controlTask", pkgStore, "Tasks.GetTask", "6"},
	},
	"consoleRuleMigrationExceptions": {
		{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", pkgStore, "ErrTemplateNotCurrent", "6"},
		{pkgConsole, "internal/interfaces/console/errors.go", "StatusOfProblems", pkgModel, "Code.Category", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllArchs", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllFirmwares", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllFormats", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllSeries", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "Families", "6"},
		{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/model", "Roles", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "NormalizeSN", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "TaskState.Valid", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).previewTask", "github.com/lober/lober/internal/domain/catalog", "Firmware.Valid", "6"},
	},
}

func phaseSixCurrentTables() map[string][]migrationException {
	return map[string][]migrationException{
		"migrationExceptions":             migrationExceptions,
		"mediaMigrationExceptions":        mediaMigrationExceptions,
		"consoleRuleMigrationExceptions":  consoleRuleMigrationExceptions,
		"notificationMigrationExceptions": notificationMigrationExceptions,
	}
}

func Test第六阶段历史保持39项原始摘要(t *testing.T) {
	total := 0
	for _, tc := range []struct {
		name  string
		count int
		hash  string
	}{
		{"migrationExceptions", 27, "a28dab617ab35d3974dd809813bdf827338a98185361a92f3e40e210f0f6de8b"},
		{"mediaMigrationExceptions", 0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
		{"consoleRuleMigrationExceptions", 11, "04264bbe752a287b4c36354564a36611a1822969bf9a3cf6ca6be6260742d659"},
		{"notificationMigrationExceptions", 1, "ba63e2a81c31ee5bf62e0288cdf68e5845af7009edfd247e6b18a119189aeaf6"},
	} {
		got := phaseFiveBaselineEntries(tc.name, phaseSixCurrentTables()[tc.name])
		if len(got) != tc.count {
			t.Fatalf("%s=%d want %d", tc.name, len(got), tc.count)
		}
		total += len(got)
		var lines []string
		for _, e := range got {
			lines = append(lines, strings.Join([]string{e.Caller, e.File, e.Function, e.Target, e.Symbol, e.RemoveIn}, "\t")+"\n")
		}
		if hash := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(lines, "")))); hash != tc.hash {
			t.Errorf("%s 原始摘要改变: %s", tc.name, hash)
		}
	}
	if total != 39 {
		t.Fatal(total)
	}
}

func Test第六阶段历史只接受原表有序子集并复制(t *testing.T) {
	name := "migrationExceptions"
	original := phaseSixOriginalEntries[name]
	input := []migrationException{original[0], original[3], original[len(original)-1]}
	before := slices.Clone(input)
	got := phaseFiveBaselineEntries(name, input)
	if !reflect.DeepEqual(got, original) || !reflect.DeepEqual(input, before) {
		t.Fatal("未恢复原表或修改了输入")
	}
	got[0].Symbol = "Changed"
	if original[0].Symbol != "CheckPageQuery" || !reflect.DeepEqual(input, before) {
		t.Fatal("结果污染了冻结表或输入")
	}
	if !reflect.DeepEqual(phaseFiveBaselineEntries(name, nil), original) {
		t.Fatal("空当前表仍需恢复完整历史")
	}
	for _, tc := range []struct {
		name    string
		entries []migrationException
	}{
		{"调序", []migrationException{original[1], original[0]}},
		{"重复", []migrationException{original[0], original[0]}},
		{"增加", append(slices.Clone(input), migrationException{pkgConsole, "internal/interfaces/console/new.go", "f", pkgStore, "ErrConflict", "6"})},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := phaseFiveBaselineEntries(name, tc.entries); got != nil {
				t.Fatalf("非法历史输入被接受: %v", got)
			}
			if got := phaseFourBaselineEntries(name, tc.entries); got != nil {
				t.Fatal("第四阶段接收了非法历史输入")
			}
		})
	}
	for _, field := range []string{"Caller", "File", "Function", "Target", "Symbol", "RemoveIn"} {
		t.Run(field, func(t *testing.T) {
			changed := original[0]
			value := reflect.ValueOf(&changed).Elem().FieldByName(field)
			value.SetString(value.String() + "changed")
			if got := phaseFiveBaselineEntries(name, []migrationException{changed}); got != nil {
				t.Fatal("改字段的历史输入被接受")
			}
		})
	}
	if got := phaseFiveBaselineEntries("unknown", nil); got != nil {
		t.Fatal("未知表名被接受")
	}
	if got := phaseFiveBaselineEntries("consoleRuleMigrationExceptions", input); got != nil {
		t.Fatal("条目可以借用别的表")
	}
}

func phaseSixCompletedOwner(e migrationException) bool {
	if e.File == "internal/interfaces/console/errors.go" {
		return true
	}
	if e.File == "internal/interfaces/console/taskcontrol.go" {
		return e.Function == "Problems" && e.Target == pkgStore && slices.Contains([]string{"ErrRejected", "ProblemsError"}, e.Symbol)
	}
	if e.File == "internal/interfaces/console/pagination.go" {
		return e.Function == "queryPagination" && e.Target == pkgStore && e.Symbol == "CheckPageQuery"
	}
	if e.File == "internal/interfaces/console/tasks.go" {
		switch e.Function {
		case "(*server).controlTask", "(*server).getTask", "(*server).previewTask":
			return e.Target == pkgStore && e.Symbol == "Tasks.GetTask" || e.Function == "(*server).previewTask" && e.Target == modulePath+"/internal/domain/catalog" && e.Symbol == "Firmware.Valid"
		case "(*server).listTasks":
			return e.Target == pkgStore && slices.Contains([]string{"Page", "TaskQuery", "Tasks.ListTasks"}, e.Symbol) || e.Target == pkgModel && slices.Contains([]string{"NormalizeSN", "TaskState.Valid"}, e.Symbol)
		}
		return false
	}

	if e.File != "internal/interfaces/console/openapi.go" || e.Function != "(*schemaBuilder).definition" {
		return false
	}
	if e.Target == pkgModel {
		return e.Symbol == "Roles"
	}
	return e.Target == modulePath+"/internal/domain/catalog" && slices.Contains([]string{"AllArchs", "AllFirmwares", "AllFormats", "AllSeries", "Families"}, e.Symbol)
}

func Test错误枚举任务读取与控制迁移精确删除30项保留9项(t *testing.T) {
	total, removedTotal := 0, 0
	for name, current := range phaseSixCurrentTables() {
		var want []migrationException
		for _, e := range phaseSixOriginalEntries[name] {
			removed := slices.Contains(phaseSixCompletedRemovals[name], e)
			if removed {
				removedTotal++
				if transitionAllows(e) {
					t.Errorf("历史条目重新放行: %+v", e)
				}
			} else {
				want = append(want, e)
			}
		}
		for _, e := range phaseSixCompletedRemovals[name] {
			if !slices.Contains(phaseSixOriginalEntries[name], e) || !phaseSixCompletedOwner(e) {
				t.Errorf("已完成迁移的删除项不精确: %+v", e)
			}
		}
		if !slices.Equal(current, want) {
			t.Errorf("%s 当前剩余集不精确: got=%v want=%v", name, current, want)
		}
		total += len(current)
	}
	if total != 9 || removedTotal != 30 {
		t.Fatal(total, removedTotal)
	}
}

func Test错误枚举任务读取与控制历史不能放行已删除的真实类型调用(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, entries := range phaseSixCompletedRemovals {
		for _, e := range entries {
			t.Run(e.Symbol, func(t *testing.T) {
				body := `import r "` + pkgStore + `";func RespondError(){_=r.` + e.Symbol + `}`
				switch e.Symbol {
				case "CheckPageQuery":
					body = `import r "` + pkgStore + `";func queryPagination(){_,_=r.CheckPageQuery("",0,0)}`
				case "Tasks.GetTask":
					body = `import r "` + pkgStore + `";type server struct { tasks r.Tasks };func (s *server)` + strings.TrimPrefix(e.Function, "(*server).") + `(){_=s.tasks.GetTask}`
				case "Page":
					body = `import r "` + pkgStore + `";type server struct{};func (*server)listTasks(){var _ r.Page[int]}`
				case "TaskQuery":
					body = `import r "` + pkgStore + `";type server struct{};func (*server)listTasks(){_=r.TaskQuery{}}`
				case "Tasks.ListTasks":
					body = `import r "` + pkgStore + `";type server struct { tasks r.Tasks };func (s *server)listTasks(){_=s.tasks.ListTasks}`
				case "NormalizeSN":
					body = `import m "` + pkgModel + `";type server struct{};func (*server)listTasks(){_,_=m.NormalizeSN("")}`
				case "TaskState.Valid":
					body = `import m "` + pkgModel + `";type server struct{};func (*server)listTasks(){_=m.TaskState("").Valid()}`
				case "Firmware.Valid":
					body = `import c "` + e.Target + `";type server struct{};func (*server)previewTask(){_=c.Firmware("").Valid()}`
				case "AllArchs", "AllFirmwares", "AllFormats", "AllSeries", "Families", "Roles":
					body = `import d "` + e.Target + `";type schemaBuilder struct{};func (*schemaBuilder)definition(){_=d.` + e.Symbol + `()}`
				case "BatchError", "ProblemsError", "ReferencedError":
					body = `import r "` + pkgStore + `";func RespondError(){var _ *r.` + e.Symbol + `}`
				case "Code.Category":
					body = `import m "` + pkgModel + `";func StatusOfProblems(c m.Code){_=c.Category()}`
				}
				if e.Function == "Problems" {
					if e.Symbol == "ProblemsError" {
						body = `import r "` + pkgStore + `";func Problems(){var _ *r.ProblemsError}`
					} else {
						body = `import r "` + pkgStore + `";func Problems(){_=r.ErrRejected}`
					}
				}
				got := identityTypedFixture(t, imports, pkgConsole, strings.TrimPrefix(e.File, "internal/interfaces/console/"), body)
				for _, v := range got {
					if v.subject == e.Target+"."+e.Symbol {
						return
					}
				}
				t.Fatalf("已删除的旧位置调用仍被放行: %+v findings=%v", e, got)
			})
		}
	}
}

func Test迁移入口只接受明确交付的共同错误能力(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"错误说明", `import p "` + pkgPorts + `";func f(){_,_=p.DescribeFailure(nil)}`, true},
		{"首条问题分类", `import p "` + pkgPorts + `";func f(){_=p.ProblemClass(nil)}`, true},
		{"其他共同业务能力仍被拒绝", `import p "` + pkgPorts + `";func f(a p.Actor){_=a.Authorize("")}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, "wizard.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

func Test迁移入口只接受明确交付的公开枚举(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"公开目录", `import a "` + modulePath + `/internal/application/authoring";func f(){_=a.PublicCatalog()}`, true},
		{"公开角色", `import i "` + pkgIdentityApp + `";func f(){_=i.PublicRoles()}`, true},
		{"编写服务构造器仍被拒绝", `import a "` + modulePath + `/internal/application/authoring";func f(){_=a.NewHooks}`, false},
		{"身份服务构造器仍被拒绝", `import i "` + pkgIdentityApp + `";func f(){_=i.NewService}`, false},
		{"其他身份操作仍被拒绝", `import i "` + pkgIdentityApp + `";func f(){_=i.NewSessionBook}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, "wizard.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatalf("allowed=%v findings=%v", tc.allowed, got)
			}
		})
	}
}

func Test任务读取入口只允许明确三个应用方法(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"列表", `import a "` + pkgTasks + `";func f(r a.Reader){_=r.List}`, true},
		{"详情", `import a "` + pkgTasks + `";func f(r a.Reader){_=r.Get}`, true},
		{"预览", `import a "` + pkgTasks + `";func f(r a.Reader){_=r.Preview}`, true},
		{"构造器仍拒绝", `import a "` + pkgTasks + `";func f(){_=a.NewReader}`, false},
		{"直接小仓储仍拒绝", `import p "` + pkgTasks + `/port";func f(r p.TaskReader){_=r.ListTasks}`, false},
		{"直接诊断仍拒绝", `import p "` + pkgTasks + `/port";func f(r p.TaskDiagnostics){_=r.CanCancel}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, "wizard.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatal(tc.allowed, got)
			}
		})
	}
}

func Test任务控制入口只允许明确两个应用方法(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, tc := range []struct {
		name, body string
		allowed    bool
	}{
		{"取消", `import a "` + pkgTasks + `";func f(c a.Controller){_=c.Cancel}`, true},
		{"重试", `import a "` + pkgTasks + `";func f(c a.Controller){_=c.Retry}`, true},
		{"构造器仍拒绝", `import a "` + pkgTasks + `";func f(){_=a.NewController}`, false},
		{"不可用构造器仍拒绝", `import a "` + pkgTasks + `";func f(){_=a.NewUnavailableControl}`, false},
		{"直接控制后端仍拒绝", `import p "` + pkgTasks + `/port";func f(c p.Control){_=c.Cancel}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := identityTypedFixture(t, imports, pkgConsole, "wizard.go", tc.body)
			if (len(got) == 0) != tc.allowed {
				t.Fatal(tc.allowed, got)
			}
		})
	}
}

func Test任务控制旧调用不再获准(t *testing.T) {
	imports := identityFixtureImports(t)
	for _, tc := range []struct{ file, body, symbol string }{
		{"tasks.go", `import r "` + pkgStore + `";type server struct{tasks r.Tasks};func(s *server)controlTask(){_=s.tasks.GetTask}`, "Tasks.GetTask"},
		{"taskcontrol.go", `import r "` + pkgStore + `";func Problems(){_=r.ErrRejected}`, "ErrRejected"},
		{"taskcontrol.go", `import r "` + pkgStore + `";func Problems(){var _ *r.ProblemsError}`, "ProblemsError"},
	} {
		got := identityTypedFixture(t, imports, pkgConsole, tc.file, tc.body)
		found := false
		for _, v := range got {
			if v.subject == pkgStore+"."+tc.symbol {
				found = true
			}
		}
		if !found {
			t.Fatal(tc.symbol, got)
		}
	}
}

func Test任务控制迁移当前精确保留9项(t *testing.T) {
	total := 0
	for _, entries := range phaseSixCurrentTables() {
		total += len(entries)
	}
	if total != 9 {
		t.Fatal(total)
	}
}
