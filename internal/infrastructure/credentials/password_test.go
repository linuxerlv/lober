package credentials

import (
	"encoding/base64"
	"errors"
	"github.com/lober/lober/internal/application/identity/port"
	"strings"
	"testing"
)

const knownPasswordHash = "pbkdf2-sha256$600000$AAECAwQFBgcICQoLDA0ODw$lqWQTC4IyNpCMF28xdfPGOrSY21J9ZUmtgbyZpYoFHM"

func Test口令散列格式随机盐与独立向量(t *testing.T) {
	a, err := NewPasswords().Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	b, err := NewPasswords().Hash("correct horse")
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(a, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" || a == b {
		t.Fatal("invalid password encoding")
	}
	salt, e1 := base64.RawURLEncoding.DecodeString(parts[2])
	key, e2 := base64.RawURLEncoding.DecodeString(parts[3])
	if e1 != nil || e2 != nil || len(salt) != 16 || len(key) != 32 {
		t.Fatal("salt/key lengths")
	}
	if !NewPasswords().Check("correct horse", knownPasswordHash) || !NewPasswords().Check("correct horse", a) {
		t.Fatal("independent PBKDF2 vector not accepted")
	}
	for _, bad := range []string{"", "correct hors", "CORRECT HORSE", "correct horse "} {
		if NewPasswords().Check(bad, a) {
			t.Fatal("wrong password accepted")
		}
	}
}
func Test口令散列拒绝坏编码和无界工作量(t *testing.T) {
	for _, bad := range []string{"", "md5$600000$abc$def", "pbkdf2-sha256$0$a$b", "pbkdf2-sha256$-1$a$b", "pbkdf2-sha256$999999999$AAECAwQFBgcICQoLDA0ODw$x", strings.Replace(knownPasswordHash, "600000", "1", 1), strings.Replace(knownPasswordHash, "AAECAwQFBgcICQoLDA0ODw", "!!!", 1), knownPasswordHash + "$extra"} {
		if NewPasswords().Check("correct horse", bad) {
			t.Fatal("malformed hash accepted")
		}
	}
	for _, bad := range []string{"", "x\x00", "x\xff"} {
		if _, err := NewPasswords().Hash(bad); err == nil {
			t.Fatal("invalid password accepted")
		}
	}
}

func Test非法口令返回能力错误(t *testing.T) {
	for _, pw := range []string{"", "x\x00", "x\xff"} {
		if _, err := NewPasswords().Hash(pw); !errors.Is(err, port.ErrInvalidPassword) {
			t.Fatal(err)
		}
	}
}
func Test随机令牌为独立小写十六进制(t *testing.T) {
	tokens := NewTokens()
	seen := map[string]bool{}
	for i := 0; i < 20; i++ {
		s, err := tokens.NewToken()
		if err != nil || len(s) != 64 || seen[s] {
			t.Fatal("令牌格式或随机性错误", err)
		}
		for _, c := range s {
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'f') {
				t.Fatal("令牌字符错误")
			}
		}
		seen[s] = true
	}
}
func Test假散列保持原固定值(t *testing.T) {
	// 已知原假散列用固定盐执行真实600000轮计算，调用必须可完成。
	NewPasswords().CheckDummy("missing account password")
	if dummyPasswordHash != "pbkdf2-sha256$600000$AAECAwQFBgcICQoLDA0ODw$NyjOjly17HG8sQZN6G_bCa7m5kd0W5PAaUE7jUNwNOw" {
		t.Fatal("假散列变化")
	}
}
