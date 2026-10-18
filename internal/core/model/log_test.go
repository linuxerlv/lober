package model

import (
	"bytes"
	"strings"
	"testing"
)

func TestSafeLogTailRedactsMachineCredentialAndPreservesBinary(t *testing.T) {
	token := strings.Repeat("a", 64)
	raw := []byte("wget http://gateway/v1/answer/task-one/" + token + "/hooks/digest\nraw\x00tail")
	got := SafeLogTail(raw)
	if bytes.Contains(got, []byte(token)) || !bytes.Contains(got, []byte("[REDACTED]")) || !bytes.HasSuffix(got, []byte("raw\x00tail")) {
		t.Fatal("unsafe or corrupted log tail")
	}
	if len(SafeLogTail([]byte(strings.Repeat("x", 8000)))) != 4096 {
		t.Fatal("unbounded log tail")
	}
}
