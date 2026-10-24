package access

import (
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func Test强制改密的旧本人更新保持原错误优先级(t *testing.T) {
	for _, role := range model.Roles() {
		s := Subject{Username: "alice", Role: role, MustChangePassword: true}
		own := Request{Permission: PermUserManage, Operation: LegacySelfUpdate, TargetUsername: "alice"}
		want := Forbidden
		if role == model.RoleAdmin {
			want = Allow
		}
		if got := CheckRequest(s, own); got != want {
			t.Errorf("本人更新 %s: got=%v want=%v", role, got, want)
		}
		own.TargetUsername = "bob"
		if got := CheckRequest(s, own); got != PasswordChangeRequired {
			t.Errorf("他人更新 %s: %v", role, got)
		}
	}
}
