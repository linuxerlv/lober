package console

import (
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
)

// 旧控制台入口在身份阶段清除，权限规则统一由 access 提供。
const (
	PermAnyUser         = access.PermAnyUser
	PermHostRead        = access.PermHostRead
	PermISORead         = access.PermISORead
	PermHookRead        = access.PermHookRead
	PermTemplateRead    = access.PermTemplateRead
	PermTaskRead        = access.PermTaskRead
	PermHostWrite       = access.PermHostWrite
	PermTaskCreate      = access.PermTaskCreate
	PermTaskCancel      = access.PermTaskCancel
	PermTaskRetry       = access.PermTaskRetry
	PermHookWrite       = access.PermHookWrite
	PermTemplateWrite   = access.PermTemplateWrite
	PermHookApprove     = access.PermHookApprove
	PermTemplateApprove = access.PermTemplateApprove
	PermISOWrite        = access.PermISOWrite
	PermUserManage      = access.PermUserManage
	PermAuditRead       = access.PermAuditRead
)

func AllPerms() []string            { return access.AllPerms() }
func PermsOf(r model.Role) []string { return access.PermsOf(r) }
