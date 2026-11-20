package console

type SourceKind string

const (
	SourceURL    SourceKind = "url"
	SourceUpload SourceKind = "upload"
	SourcePath   SourceKind = "path"
)

func (k SourceKind) Valid() bool { return k == SourceURL || k == SourceUpload || k == SourcePath }
