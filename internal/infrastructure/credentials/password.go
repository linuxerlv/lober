package credentials

import (
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"github.com/lober/lober/internal/application/identity/port"
	"github.com/lober/lober/internal/domain/access"
	"strings"
)

type passwords struct{}

func NewPasswords() port.Passwords             { return passwords{} }
func (p passwords) CheckDummy(password string) { p.Check(password, dummyPasswordHash) }

const passwordIterations = 600000
const dummyPasswordHash = "pbkdf2-sha256$600000$AAECAwQFBgcICQoLDA0ODw$NyjOjly17HG8sQZN6G_bCa7m5kd0W5PAaUE7jUNwNOw"

func (passwords) Hash(pw string) (string, error) {
	if !access.ValidPasswordText(pw) {
		return "", port.ErrInvalidPassword
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key, err := pbkdf2.Key(sha256.New, pw, salt, passwordIterations, 32)
	if err != nil {
		return "", err
	}
	return "pbkdf2-sha256$600000$" + base64.RawURLEncoding.EncodeToString(salt) + "$" + base64.RawURLEncoding.EncodeToString(key), nil
}

// 只接受固定格式，坏散列不能指定无限计算次数或任意结果长度。
func (passwords) Check(pw, encoded string) bool {
	parts := strings.Split(encoded, "$")
	if len(parts) != 4 || parts[0] != "pbkdf2-sha256" || parts[1] != "600000" {
		return false
	}
	salt, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || len(salt) != 16 {
		return false
	}
	want, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil || len(want) != 32 {
		return false
	}
	got, err := pbkdf2.Key(sha256.New, pw, salt, passwordIterations, 32)
	return err == nil && subtle.ConstantTimeCompare(got, want) == 1
}
