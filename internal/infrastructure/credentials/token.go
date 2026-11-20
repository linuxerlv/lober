package credentials

import (
	"crypto/rand"
	"encoding/hex"
	"github.com/lober/lober/internal/application/identity/port"
)

type tokens struct{}

func NewTokens() port.Tokens { return tokens{} }
func (tokens) NewToken() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
