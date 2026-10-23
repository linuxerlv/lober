package archtest

// 旧镜像实现仍组织获取、状态、事务和恢复流程，总路线镜像阶段逐条迁出；不是终态基础设施。
var mediaMigrationExceptions = []migrationException{
	// 保留 dev 已有流程；逐项登记，向导不增加放行。
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/model", "ISO.Validate", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "CheckID", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "CheckSource", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).CleanupDeleted", "github.com/lober/lober/internal/domain/repository", "ErrInvalid", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ErrNotFound", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "(*Sources).confirmDeleted", "github.com/lober/lober/internal/domain/repository", "ISOs.GetISO", "4"},
	{pkgISO, "internal/infrastructure/media/delete.go", "cleanupOwnedTree", "github.com/lober/lober/internal/domain/repository", "ErrConflict", "4"},
	{pkgISO, "internal/infrastructure/media/iso.go", "package-init", "github.com/lober/lober/internal/domain/repository", "Store", "4"},
	{pkgISO, "internal/infrastructure/media/iso.go", "New", "github.com/lober/lober/internal/domain/repository", "Store", "4"},
}

// 非向导的旧领域判定、渲染及描述调用，按所属模块阶段逐条删除。
var consoleRuleMigrationExceptions = []migrationException{
	// 保留 dev 已有流程；逐项登记，向导不增加放行。
	{pkgConsole, "internal/interfaces/console/menus.go", "(*server).listRoleMenus", "github.com/lober/lober/internal/domain/model", "Roles", "5"},
	{pkgConsole, "internal/interfaces/console/menus.go", "(*server).saveRoleMenus", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
	{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
	{pkgConsole, "internal/interfaces/console/password.go", "(*server).changePassword", "github.com/lober/lober/internal/domain/model", "StorableText", "5"},
	{pkgConsole, "internal/interfaces/console/auth.go", "HashPassword", "github.com/lober/lober/internal/domain/model", "StorableText", "5"},
	{pkgConsole, "internal/interfaces/console/auth.go", "(*server).login", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
	{pkgConsole, "internal/interfaces/console/errors.go", "RespondError", "github.com/lober/lober/internal/domain/repository", "ErrTemplateNotCurrent", "6"},
	{pkgConsole, "internal/interfaces/console/errors.go", "StatusOfProblems", "github.com/lober/lober/internal/domain/model", "Code.Category", "6"},
	{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllArchs", "6"},
	{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllFirmwares", "6"},
	{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllFormats", "6"},
	{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "AllSeries", "6"},
	{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/catalog", "Families", "6"},
	{pkgConsole, "internal/interfaces/console/openapi.go", "(*schemaBuilder).definition", "github.com/lober/lober/internal/domain/model", "Roles", "6"},
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
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "NormalizeSN", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "TaskState.Valid", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).streamAuthorized", "github.com/lober/lober/internal/domain/model", "Role.Valid", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).previewTask", "github.com/lober/lober/internal/domain/catalog", "Firmware.Valid", "6"},
	{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
	{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", "github.com/lober/lober/internal/domain/model", "StorableText", "5"},
	{pkgConsole, "internal/interfaces/console/users.go", "(*server).createUser", "github.com/lober/lober/internal/domain/model", "ValidIdent", "5"},
	{pkgConsole, "internal/interfaces/console/users.go", "(*server).updateUser", "github.com/lober/lober/internal/domain/model", "Role.Valid", "5"},
}

// 唯一通知转发维持旧构造默认值；生产组装直接创建实现。
var notificationMigrationExceptions = []migrationException{
	{pkgConsole, "internal/interfaces/console/notify.go", "NewHub", pkgNotification, "NewHub", "6"},
}
