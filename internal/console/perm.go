package console

import (
	"github.com/lober/lober/internal/store"
	"slices"
)

const (
	PermAnyUser         = "@any"
	PermHostRead        = "host:read"
	PermISORead         = "iso:read"
	PermHookRead        = "hook:read"
	PermTemplateRead    = "template:read"
	PermTaskRead        = "task:read"
	PermHostWrite       = "host:write"
	PermTaskCreate      = "task:create"
	PermTaskCancel      = "task:cancel"
	PermTaskRetry       = "task:retry"
	PermHookWrite       = "hook:write"
	PermTemplateWrite   = "template:write"
	PermHookApprove     = "hook:approve"
	PermTemplateApprove = "template:approve"
	PermISOWrite        = "iso:write"
	PermUserManage      = "user:manage"
	PermAuditRead       = "audit:read"
)

var allPerms = []string{PermHostRead, PermISORead, PermHookRead, PermTemplateRead, PermTaskRead, PermHostWrite, PermTaskCreate, PermTaskCancel, PermTaskRetry, PermHookWrite, PermTemplateWrite, PermHookApprove, PermTemplateApprove, PermISOWrite, PermUserManage, PermAuditRead}

func AllPerms() []string { return slices.Clone(allPerms) }
func PermsOf(r store.Role) []string {
	if !r.Valid() {
		return []string{}
	}
	if r == store.RoleAdmin {
		return AllPerms()
	}
	out := slices.Clone(allPerms[:5])
	switch r {
	case store.RoleOperator:
		out = append(out, PermHostWrite, PermTaskCreate, PermTaskCancel, PermTaskRetry)
	case store.RoleHookAuthor:
		out = append(out, PermHookWrite, PermTemplateWrite)
	case store.RoleHookApprover:
		out = append(out, PermHookApprove, PermTemplateApprove)
	}
	return out
}
