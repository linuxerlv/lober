package identity

import (
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/access"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func Test身份普通用户本人更新仍报禁止(t *testing.T) {
	s, u, _, book := accessFixture()
	u.user.Role = model.RoleViewer
	u.user.MustChangePassword = true
	p, err := s.Authenticate(t.Context(), book.Put("alice"))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage, Operation: LegacySelfUpdate, TargetUsername: "alice"}); !errors.Is(err, ports.ErrForbidden) {
		t.Fatal("本人更新错误分类变化", err)
	}
	if err := s.Authorize(p, AccessRequest{Permission: access.PermUserManage, Operation: LegacySelfUpdate, TargetUsername: "bob"}); !errors.Is(err, ports.ErrPasswordChangeRequired) {
		t.Fatal("他人更新错误分类变化", err)
	}
}
