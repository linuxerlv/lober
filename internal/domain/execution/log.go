package execution

import "github.com/lober/lober/internal/domain/model"

// CanReceiveInstallerLog 的八项纯值约定唯一连接 docs/contracts/machine-execution.md「领域纯规则」本方法行及共同约定。
// 地址归属、有效期与正文裁剪仍由日志收集器处理。
func CanReceiveInstallerLog(t model.Task, attempt int) bool {
	return !t.State.Finished() && t.Execution != nil && t.Execution.Attempt == attempt
}
