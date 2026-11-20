package repository

import (
	"context"
	"math"
	"strings"

	"github.com/lober/lober/internal/domain/model"
)

// RoleMenuSettings uses Rev=0 only when creating the first explicit setting.
// An explicit empty MenuIDs slice differs from an absent setting.
type RoleMenuSettings struct {
	Role    model.Role
	MenuIDs []string
	Rev     int64
}

type RoleMenus interface {
	// GetRoleMenus 先检查有效角色，无效报ErrInvalid；缺配置返回零值、false、nil，与显式空配置不同。
	// 存在时返回独立MenuIDs数组及true；只读，不过滤目录或授权，重复、无提交复查与取消遵守doc.go。
	GetRoleMenus(context.Context, model.Role) (RoleMenuSettings, bool, error)
	// SaveRoleMenus 先CheckAudit，再CheckRoleMenus；不在此校验页面权限，调用方先按目录校验排序。
	// Rev=0仅首次创建，唯一约束处理竞争；已有配置行锁内比原Rev，丢失或旧版均ErrConflict，不自动重试。
	// 返回输入顺序的非nilMenuIDs及Rev+1；配置与role审计同事务，详情由保存层生成before/after。
	// 首次before为nil，之后为原数组；失败全部回滚，取消与无通知遵守doc.go。
	SaveRoleMenus(context.Context, RoleMenuSettings, Audit) (RoleMenuSettings, error)
}

func CheckRoleMenus(s RoleMenuSettings) error {
	if !s.Role.Valid() || s.Rev < 0 || s.Rev == math.MaxInt64 {
		return ErrInvalid
	}
	seen := make(map[string]bool, len(s.MenuIDs))
	for _, id := range s.MenuIDs {
		if !model.ValidIdent(strings.ReplaceAll(id, "-", "_")) || seen[id] {
			return ErrInvalid
		}
		seen[id] = true
	}
	return nil
}
