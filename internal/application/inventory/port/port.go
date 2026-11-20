// Package port 声明采集模块需要的最小外部能力。
package port

import "github.com/lober/lober/internal/domain/repository"

// Hosts 只开放采集的原子更新入口。通用主机编辑不经过这个接口。
type Hosts = repository.HostInventory

// Tokens 生成随机令牌和采集批次编号。实现返回 64 位小写十六进制串。
type Tokens interface {
	NewToken() (string, error)
}
