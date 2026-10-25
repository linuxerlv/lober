package archtest

import (
	"slices"
	"testing"
)

// 只供历史指纹重建；不是当前许可，不得传给 transitionAllows。
// 第五阶段的68项计划包括第六任务才移除的审计入口，仍使用中的条目不重复补入。
var phaseFivePlannedRemovals = map[string][]migrationException{
	"migrationExceptions": {
		{pkgConsole, "internal/interfaces/console/menus.go", "(*server).roleMenus", "github.com/lober/lober/internal/domain/repository", "RoleMenus.GetRoleMenus", "5"},
		{pkgConsole, "internal/interfaces/console/menus.go", "(*server).saveRoleMenus", "github.com/lober/lober/internal/domain/repository", "CheckRoleMenus", "5"},
		{pkgConsole, "internal/interfaces/console/menus.go", "(*server).saveRoleMenus", "github.com/lober/lober/internal/domain/repository", "RoleMenuSettings", "5"},
		{pkgConsole, "internal/interfaces/console/menus.go", "(*server).saveRoleMenus", "github.com/lober/lober/internal/domain/repository", "RoleMenus.SaveRoleMenus", "5"},
		{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "5"},
		{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/repository", "Users.GetUser", "5"},
		{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/repository", "Users.UpdateUser", "5"},
		{pkgConsole, "internal/interfaces/console/profile.go", "(*server).updateProfile", "github.com/lober/lober/internal/domain/repository", "CheckProfile", "5"},
		{pkgConsole, "internal/interfaces/console/profile.go", "(*server).updateProfile", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "5"},
		{pkgConsole, "internal/interfaces/console/profile.go", "(*server).updateProfile", "github.com/lober/lober/internal/domain/repository", "Users.UpdateUser", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "package-init", "github.com/lober/lober/internal/domain/repository", "RoleMenus", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "(*server).exportAudit", pkgStore, "AuditEntry", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "(*server).exportAudit", pkgStore, "AuditLog.ListAudit", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "(*server).exportAudit", pkgStore, "Page", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "(*server).listAudit", pkgStore, "AuditEntry", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "(*server).listAudit", pkgStore, "AuditLog.ListAudit", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "(*server).listAudit", pkgStore, "Page", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "auditFilters", pkgStore, "AuditQuery", "5"},
		{pkgConsole, "internal/interfaces/console/audit.go", "toAuditItem", pkgStore, "AuditEntry", "5"},
		{pkgConsole, "internal/interfaces/console/auth.go", "(*server).login", pkgStore, "ErrNotFound", "5"},
		{pkgConsole, "internal/interfaces/console/auth.go", "(*server).login", pkgStore, "Users.GetUser", "5"},
		{pkgConsole, "internal/interfaces/console/auth.go", "HashPassword", pkgStore, "ErrInvalid", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "(*server).auditFrom", pkgStore, "Audit", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "(*server).authenticate", pkgStore, "ErrNotFound", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "(*server).authenticate", pkgStore, "Users.GetUser", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "AuditLog", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "package-init", pkgStore, "Users", "5"},
		{pkgConsole, "internal/interfaces/console/taskcontrol.go", "package-init", pkgStore, "Audit", "6"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).streamAuthorized", pkgStore, "Users.GetUser", "6"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", pkgStore, "Users.CreateUser", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).disableUser", pkgStore, "Users.GetUser", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).disableUser", pkgStore, "Users.UpdateUser", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).listUsers", pkgStore, "Page", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).listUsers", pkgStore, "UserQuery", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).listUsers", pkgStore, "Users.ListUsers", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).updateUser", pkgStore, "Users.GetUser", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).updateUser", pkgStore, "Users.UpdateUser", "5"},
	},
	"mediaMigrationExceptions": {},
	"consoleRuleMigrationExceptions": {
		{pkgConsole, "internal/interfaces/console/menus.go", "(*server).listRoleMenus", "github.com/lober/lober/internal/domain/model", "Roles", "5"},
		{pkgConsole, "internal/interfaces/console/menus.go", "(*server).saveRoleMenus", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
		{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
		{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/model", "StorableText", "5"},
		{pkgConsole, "internal/interfaces/console/auth.go", "HashPassword", "github.com/lober/lober/internal/domain/model", "StorableText", "5"},
		{pkgConsole, "internal/interfaces/console/auth.go", "(*server).login", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermAnyUser", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermAuditRead", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermHookApprove", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermHookRead", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermHookWrite", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermHostRead", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermHostWrite", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermISORead", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermISOWrite", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTaskCancel", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTaskCreate", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTaskRead", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTaskRetry", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTemplateApprove", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTemplateRead", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermTemplateWrite", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "package-init", "github.com/lober/lober/internal/domain/access", "PermUserManage", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "AllPerms", "github.com/lober/lober/internal/domain/access", "AllPerms", "5"},
		{pkgConsole, "internal/interfaces/console/perm.go", "PermsOf", "github.com/lober/lober/internal/domain/access", "PermsOf", "5"},
		{pkgConsole, "internal/interfaces/console/server.go", "(*server).authenticate", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
		{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).streamAuthorized", "github.com/lober/lober/internal/domain/model", "Role.Valid", "6"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", "github.com/lober/lober/internal/domain/model", "StorableText", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", "github.com/lober/lober/internal/domain/model", "ValidIdent", "5"},
		{pkgConsole, "internal/interfaces/console/users.go", "(*server).updateUser", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
	},
	"notificationMigrationExceptions": {},
}

func phaseFourBaselineEntries(table string, current []migrationException) []migrationException {
	out := phaseFiveBaselineEntries(table, current)
	if out == nil {
		return nil
	}
	seen := map[migrationException]bool{}
	for _, e := range out {
		seen[e] = true
	}
	for _, e := range phaseFivePlannedRemovals[table] {
		if !seen[e] {
			out = append(out, e)
			seen[e] = true
		}
	}
	return out
}
func Test历史重建不添加当前许可(t *testing.T) {
	for name, count := range map[string]int{"migrationExceptions": 64, "mediaMigrationExceptions": 0, "consoleRuleMigrationExceptions": 42, "notificationMigrationExceptions": 1} {
		current := slices.Clone(phaseSixCurrentTables()[name])
		original := slices.Clone(current)
		restored := phaseFourBaselineEntries(name, current)
		if len(restored) != count || !slices.Equal(restored, phaseFourBaselineEntries(name, nil)) {
			t.Fatal("缺失历史未补全或重复补入", name)
		}
		if !slices.Equal(current, original) {
			t.Fatal("重建修改了当前清单")
		}
		seen := map[migrationException]bool{}
		for _, e := range restored {
			if seen[e] {
				t.Fatal("仍在使用的许可被重复补入", name, e)
			}
			seen[e] = true
		}
	}
}

func Test历史重建清单不能放行已删除调用(t *testing.T) {
	current := map[migrationException]bool{}
	for _, entries := range [][]migrationException{migrationExceptions, mediaMigrationExceptions, consoleRuleMigrationExceptions, notificationMigrationExceptions} {
		for _, e := range entries {
			current[e] = true
		}
	}
	for _, entries := range phaseFivePlannedRemovals {
		for _, e := range entries {
			if transitionAllows(e) != current[e] {
				t.Fatal("历史清单改变了当前许可", e)
			}
		}
	}
}
