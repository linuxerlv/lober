package console

import (
	"github.com/lober/lober/internal/application/identity"
)

// 路由仅引用应用公布的权限名称，权限判断由身份应用完成。
const (
	PermAnyUser         = identity.PermAnyUser
	PermHostRead        = identity.PermHostRead
	PermISORead         = identity.PermISORead
	PermHookRead        = identity.PermHookRead
	PermTemplateRead    = identity.PermTemplateRead
	PermTaskRead        = identity.PermTaskRead
	PermHostWrite       = identity.PermHostWrite
	PermTaskCreate      = identity.PermTaskCreate
	PermTaskCancel      = identity.PermTaskCancel
	PermTaskRetry       = identity.PermTaskRetry
	PermHookWrite       = identity.PermHookWrite
	PermTemplateWrite   = identity.PermTemplateWrite
	PermHookApprove     = identity.PermHookApprove
	PermTemplateApprove = identity.PermTemplateApprove
	PermISOWrite        = identity.PermISOWrite
	PermUserManage      = identity.PermUserManage
	PermAuditRead       = identity.PermAuditRead
)
