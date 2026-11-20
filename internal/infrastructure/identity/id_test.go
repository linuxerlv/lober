package identity_test

import (
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/infrastructure/identity"
	"strings"
	"testing"
)

func TestNewID生成的ID互不相同且合规则(t *testing.T) {
	const lowerBase32 = "abcdefghijklmnopqrstuvwxyz234567"
	seen := make(map[string]bool, 1000)
	for range 1000 {
		id := identity.NewID()
		// Trim 掉字母表里的字符后为空，说明每个字符都在小写 base32 里，也就不含 -。
		if len(id) != 26 || strings.Trim(id, lowerBase32) != "" || !model.ValidID(id) || !model.IsSafeWord(id) {
			t.Fatalf("ID %q 不合规则：应为 26 个字符的小写 base32", id)
		}
		if seen[id] {
			t.Fatalf("ID %q 重复了", id)
		}
		seen[id] = true
	}
}
