package model

type SourceKind string

const (
	SourceRemote SourceKind = "remote"
	SourceUpload SourceKind = "upload"
	SourceLocal  SourceKind = "local"
)

type SourceState string

const (
	SourcePending   SourceState = "pending"
	SourceFetching  SourceState = "fetching"
	SourceFetched   SourceState = "fetched"
	SourceFailed    SourceState = "failed"
	SourceCancelled SourceState = "cancelled"
)

// Source 的键是 (ISOID,Role)，空 Role 为主介质。Rev 是仓储的乐观锁。
type Source struct {
	ISOID, Role    string
	Kind           SourceKind
	URL, Path      string
	TotalSize      int64
	ExpectedSHA256 string
	Fetched        int64
	State          SourceState
	Error          string
	Rev            int64
}

// Range 是已持久化的字节区间 [Start, Start+Length)。
type Range struct{ Start, Length int64 }
