package model

import "github.com/lober/lober/internal/domain/catalog"

// Execution is durable machine progress, separate from the immutable snapshot.
// Credentials are signed from Attempt/IssuedAt; plaintext credentials never persist here.
type Execution struct {
	Attempt       int
	BootCount     int
	Firmware      catalog.Firmware
	IssuedAt      int64
	PhaseAt       int64
	Deadline      int64
	Stage         Stage
	TimingRank    int
	ReachedReboot bool
	InitDone      bool
	// SlowWarned 记录本次尝试是否已经发过“装得比旧预算慢”的警告。它是持久状态：
	// 发过一次就不再发，避免每 15 秒扫一次就写一条事件。
	SlowWarned bool
	// Signals maps a canonical per-attempt signal key to its exact payload digest.
	// It makes retries idempotent and rejects contradictory duplicate reports.
	Signals map[string]string
}
