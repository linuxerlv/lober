package access

import "github.com/lober/lober/internal/domain/model"

// ValidPasswordText 只要求非空且可保存，不增加长度、复杂度或清理空白规则。
func ValidPasswordText(password string) bool { return password != "" && model.StorableText(password) }
