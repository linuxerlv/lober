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
	Stage         string
	TimingRank    int
	ReachedReboot bool
	InitDone      bool
	// Signals maps a canonical per-attempt signal key to its exact payload digest.
	// It makes retries idempotent and rejects contradictory duplicate reports.
	Signals map[string]string
}
