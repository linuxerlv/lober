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

// Credentials 只负责按任务、尝试次数和用途签发与校验凭据。
type Credentials struct {
	key   []byte
	clock *clock
}

func newCredentials(key []byte, shared *clock) (*Credentials, error) {
	if len(key) < 32 || shared == nil || shared.now == nil {
		return nil, errors.New("invalid execution configuration")
	}
	return &Credentials{key: append([]byte(nil), key...), clock: shared}, nil
}

// Token 的八项只读应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」本方法行。
func (c *Credentials) Token(task model.Task, scope Scope) string {
	if task.Execution == nil || (scope != Install && scope != Finalize) {
		return ""
	}
	mac := hmac.New(sha256.New, c.key)
	fmt.Fprintf(mac, "lober-v1|%s|%d|%d|%s", task.ID, task.Execution.Attempt, task.Execution.IssuedAt, scope)
	return hex.EncodeToString(mac.Sum(nil))
}

// Authorize 的八项只读应用约定唯一连接 docs/contracts/machine-execution.md「构造、凭据与授权」本方法行。
func (c *Credentials) Authorize(task model.Task, token string, scope Scope) error {
	if err := c.authorizeCredential(task, token, scope); err != nil {
		return err
	}
	if scope == Install {
		at := int64(0)
		if executionrules.InstallAccessNeedsTime(task) {
			at = c.clock.Now()
		}
		if !executionrules.InstallAccessAllowed(task, at) {
			return ErrCredential
		}
	}
	return nil
}

func (c *Credentials) authorizeCredential(task model.Task, token string, scope Scope) error {
	if task.Execution == nil || len(token) != 64 || (scope != Install && scope != Finalize) {
		return ErrCredential
	}
	life := int64(executionrules.InstallLife)
	if scope == Finalize {
		life = 86400
	}
	now := c.clock.Now()
	if now < task.Execution.IssuedAt || now-task.Execution.IssuedAt >= life {
		return ErrCredential
	}
	if !hmac.Equal([]byte(token), []byte(c.Token(task, scope))) {
		return ErrCredential
	}
	return nil
}

// Credentials 属于执行模块，供可信机器协议入口取得当前尝试的两项凭据。
// 输入为原任务，不规范化、不另加大小限制；无Execution时两项均为空字符串。
// 成功始终先安装、后收尾，逐项委托Token；不重新校验状态、有效期或调用者凭据。
// 无错误或原因代码；不读仓储、不写任务，无事务、版本冲突与提交复查。
// 相同任务和服务密钥可重复调用，并发只读；调用期间不得修改任务，无新重试规则。
// 无审计、通知、上下文或超时，不新增取消检查。
func (c *Credentials) Credentials(task model.Task) (string, string) {
	return c.Token(task, Install), c.Token(task, Finalize)
}
