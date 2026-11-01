// Package execution owns durable machine progress and attempt-bound credentials.
package execution

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
)

type Scope string

const (
	Install  Scope = "install"
	Finalize Scope = "finalize"
)

var ErrProtocol = executionrules.ErrProtocol
var ErrCredential = errors.New("machine credential rejected")

// Token 的八项只读应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」本方法行。
func (s *Service) Token(task model.Task, scope Scope) string {
	if task.Execution == nil || (scope != Install && scope != Finalize) {
		return ""
	}
	mac := hmac.New(sha256.New, s.key)
	fmt.Fprintf(mac, "lober-v1|%s|%d|%d|%s", task.ID, task.Execution.Attempt, task.Execution.IssuedAt, scope)
	return hex.EncodeToString(mac.Sum(nil))
}

// Authorize 的八项只读应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」本方法行。
func (s *Service) Authorize(task model.Task, token string, scope Scope) error {
	if err := s.authorizeCredential(task, token, scope); err != nil {
		return err
	}
	if scope == Install {
		at := int64(0)
		if executionrules.InstallAccessNeedsTime(task) {
			at = s.now()
		}
		if !executionrules.InstallAccessAllowed(task, at) {
			return ErrCredential
		}
	}
	return nil
}
func (s *Service) authorizeCredential(task model.Task, token string, scope Scope) error {
	if task.Execution == nil || len(token) != 64 || (scope != Install && scope != Finalize) {
		return ErrCredential
	}
	life := int64(executionrules.InstallLife)
	if scope == Finalize {
		life = 86400
	}
	now := s.now()
	if now < task.Execution.IssuedAt || now-task.Execution.IssuedAt >= life {
		return ErrCredential
	}
	if !hmac.Equal([]byte(token), []byte(s.Token(task, scope))) {
		return ErrCredential
	}
	return nil
}
