package execution

import (
	"context"

	"github.com/lober/lober/internal/application/execution/port"
	"github.com/lober/lober/internal/domain/catalog"
	executionrules "github.com/lober/lober/internal/domain/execution"
	"github.com/lober/lober/internal/domain/model"
)

type BootInput struct {
	SN       string
	Arch     catalog.Arch
	Firmware catalog.Firmware
}
type InstallerAnswerInput struct{ SN, RemoteAddr string }
type FileInput struct{ TaskID, Token, Name, RemoteAddr string }
type SignalInput struct {
	TaskID, Token, RemoteAddr string
	Signal                    executionrules.Signal
}
type BootResult struct {
	Body  []byte
	Found bool
}

type Gateway interface {
	// BootMenu 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	BootMenu(context.Context, BootInput) (BootResult, error)
	// InstallerAnswer 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	InstallerAnswer(context.Context, InstallerAnswerInput) (BootResult, error)
	// File 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	File(context.Context, FileInput) ([]byte, error)
	// Signal 的八项应用约定唯一连接 docs/contracts/machine-execution.md「机器交付入口」本方法行及共同约定。
	Signal(context.Context, SignalInput) error
}
type Endpoints struct{ Gateway, Artifacts, SMB, SyslogAddr string }
type GatewayDeps struct {
	Service   *Service
	Renderer  port.FrozenRenderer
	Endpoints Endpoints
	Observe   func(model.Task, string)
}
type GatewayFailureKind string

const (
	LookupUnavailable GatewayFailureKind = "lookup-unavailable"
	RenderRejected    GatewayFailureKind = "render-rejected"
	Unauthorized      GatewayFailureKind = "unauthorized"
	FileNotFound      GatewayFailureKind = "file-not-found"
	AnswerConflict    GatewayFailureKind = "answer-conflict"
	SignalConflict    GatewayFailureKind = "signal-conflict"
)

type GatewayError struct {
	Kind  GatewayFailureKind
	Cause error
}

// Error 的八项纯错误约定唯一连接 docs/contracts/machine-execution.md「机器交付错误」。
func (e *GatewayError) Error() string { return string(e.Kind) }

// Unwrap 的八项纯错误约定唯一连接 docs/contracts/machine-execution.md「机器交付错误」。
func (e *GatewayError) Unwrap() error { return e.Cause }
