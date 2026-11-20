package console

import (
	"testing"

	"github.com/lober/lober/internal/domain/model"
)

func TestTaskSnapshotRedactsCredentials(t *testing.T) {
	in := map[string]model.SnapshotValue{
		"username": {Type: "word", Items: []string{"lober"}},
		"pwhash":   {Type: "pwhash", Items: []string{"$6$fixture$notARealPasswordHash"}},
		"password": {Type: "word", Items: []string{"Fixture_Pass9"}},
		"key":      {Type: "sshkey", List: false, Items: []string{"ssh-ed25519 AAAA fixture"}},
	}
	got := snapshotValues(in)
	for _, key := range []string{"pwhash", "password"} {
		if len(got[key].Items) != 1 || got[key].Items[0] != "[redacted]" {
			t.Fatalf("%s 没有脱敏：%+v", key, got[key])
		}
	}
	if got["username"].Items[0] != "lober" || got["key"].Items[0] == "[redacted]" {
		t.Fatalf("普通参数被误伤：%+v", got)
	}
	if got["pwhash"].Type != "pwhash" {
		t.Fatalf("脱敏后类型丢了：%+v", got["pwhash"])
	}
}
