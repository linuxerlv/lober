// Package execution owns durable machine progress and attempt-bound credentials.
package execution

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"

	"github.com/lober/lober/internal/domain/model"
)

type Scope string

const (
	Install  Scope = "install"
	Finalize Scope = "finalize"
)

var ErrProtocol = errors.New("machine protocol rejected")
var ErrCredential = errors.New("machine credential rejected")

func (s *Service) Token(task model.Task, scope Scope) string {
	if task.Execution == nil || (scope != Install && scope != Finalize) {
		return ""
	}
	mac := hmac.New(sha256.New, s.key)
	fmt.Fprintf(mac, "lober-v1|%s|%d|%d|%s", task.ID, task.Execution.Attempt, task.Execution.IssuedAt, scope)
	return hex.EncodeToString(mac.Sum(nil))
}
func (s *Service) Authorize(task model.Task, token string, scope Scope) error {
	if err := s.authorizeCredential(task, token, scope); err != nil {
		return err
	}
	if scope == Install && task.Execution.Deadline > 0 && s.now() >= task.Execution.Deadline {
		return ErrCredential
	}
	if scope == Install && (task.State != model.TaskBooting && task.State != model.TaskInstalling) {
		return ErrCredential
	}
	return nil
}
func (s *Service) authorizeCredential(task model.Task, token string, scope Scope) error {
	if task.Execution == nil || len(token) != 64 || (scope != Install && scope != Finalize) {
		return ErrCredential
	}
	life := int64(5400)
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
