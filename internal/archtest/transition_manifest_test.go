package archtest

// 镜像业务已迁入应用，基础设施不再持有旧流程许可。
var mediaMigrationExceptions = []migrationException{}

// 非向导的旧领域判定、渲染及描述调用，按所属模块阶段逐条删除。
var consoleRuleMigrationExceptions = []migrationException{
	// 保留 dev 已有流程；逐项登记，向导不增加放行。
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "NormalizeSN", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).listTasks", "github.com/lober/lober/internal/domain/model", "TaskState.Valid", "6"},
	{pkgConsole, "internal/interfaces/console/tasks.go", "(*server).previewTask", "github.com/lober/lober/internal/domain/catalog", "Firmware.Valid", "6"},
}

// 唯一通知转发维持旧构造默认值；生产组装直接创建实现。
var notificationMigrationExceptions = []migrationException{
	{pkgConsole, "internal/interfaces/console/notify.go", "NewHub", pkgNotification, "NewHub", "6"},
}
