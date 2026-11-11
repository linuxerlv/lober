package access

import (
	"github.com/lober/lober/internal/domain/model"
	"slices"
)

type Menu struct {
	ID, Title, ParentID, Permission string
	Required                        bool
}

var menuCatalog = []Menu{
	{ID: "tasks-group", Title: "任务"},
	{ID: "tasks", Title: "任务列表", ParentID: "tasks-group", Permission: PermTaskRead},
	{ID: "hosts-group", Title: "主机清单"},
	{ID: "hosts", Title: "主机清单", ParentID: "hosts-group", Permission: PermHostRead},
	{ID: "templates-group", Title: "模板"},
	{ID: "templates", Title: "模板", ParentID: "templates-group", Permission: PermTemplateRead},
	{ID: "hooks-group", Title: "hook"},
	{ID: "hooks", Title: "hook 管理与审批", ParentID: "hooks-group", Permission: PermHookRead},
	{ID: "isos-group", Title: "制品集"},
	{ID: "isos", Title: "制品集导入", ParentID: "isos-group", Permission: PermISORead},
	{ID: "tools-group", Title: "工具"},
	{ID: "tools", Title: "磁盘策略 dry-run", ParentID: "tools-group", Permission: PermTemplateRead},
	{ID: "reports-group", Title: "报表与审计"},
	{ID: "reports", Title: "报表与审计", ParentID: "reports-group", Permission: PermAuditRead},
	{ID: "access", Title: "权限管理"},
	{ID: "users", Title: "账号管理", ParentID: "access", Permission: PermUserManage},
	{ID: "role-menus", Title: "角色与菜单", ParentID: "access", Permission: PermUserManage, Required: true},
}

// MenuCatalog 返回独立目录副本，调用方修改不会影响下次结果。
func MenuCatalog() []Menu { return slices.Clone(menuCatalog) }

// AvailableMenuIDs 按权限和目录顺序返回二级页面；结果切片由调用方独占。
func AvailableMenuIDs(role model.Role) []string {
	out := []string{}
	permissions := PermsOf(role)
	for _, m := range menuCatalog {
		if m.ParentID != "" && slices.Contains(permissions, m.Permission) {
			out = append(out, m.ID)
		}
	}
	return out
}

// EffectiveMenuIDs 在未配置时返回默认页面，显式空配置不恢复默认；必需页面仍保留。
// 读取重新按当前权限过滤，输入和结果不共享切片，也不会授予接口权限。
func EffectiveMenuIDs(role model.Role, storedIDs []string, found bool) []string {
	allowed := AvailableMenuIDs(role)
	if !found {
		return allowed
	}
	out := []string{}
	for _, m := range menuCatalog {
		if slices.Contains(allowed, m.ID) && (slices.Contains(storedIDs, m.ID) || m.Required) {
			out = append(out, m.ID)
		}
	}
	return out
}

// 格式、重复和版本仍由保存契约校验；这里只检查可授予页面与目录顺序。
func NormalizeMenuIDs(role model.Role, ids []string) ([]string, bool) {
	allowed := AvailableMenuIDs(role)
	for _, id := range ids {
		if !slices.Contains(allowed, id) {
			return nil, false
		}
	}
	out := []string{}
	for _, m := range menuCatalog {
		if m.Required && slices.Contains(allowed, m.ID) && !slices.Contains(ids, m.ID) {
			return nil, false
		}
		if slices.Contains(ids, m.ID) {
			out = append(out, m.ID)
		}
	}
	return out, true
}
