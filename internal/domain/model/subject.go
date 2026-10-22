package model

const (
	KindHost     = "host"
	KindISO      = "iso"
	KindHook     = "hook"
	KindImpl     = "impl"
	KindTemplate = "template"
	KindSlot     = "slot"
	KindParam    = "param"
	KindBody     = "body"
)

// Kinds returns the subject vocabulary with no shared writable storage.
func Kinds() []string {
	return []string{KindHost, KindISO, KindHook, KindImpl, KindTemplate, KindSlot, KindParam, KindBody}
}
