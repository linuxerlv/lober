package model

import "regexp"

var machineCredentialPath = regexp.MustCompile(`(/v1/(?:answer|signal)/[^/\s]+/)[^/\s]+`)

// SafeLogTail removes machine credentials before bounded binary logs persist.
// Redact before truncating so a tail cannot cut away the credential's prefix.
func SafeLogTail(body []byte) []byte {
	safe := machineCredentialPath.ReplaceAll(body, []byte("${1}[REDACTED]"))
	if len(safe) > 4096 {
		safe = safe[len(safe)-4096:]
	}
	return safe
}
