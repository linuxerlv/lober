package authoring

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/authoring/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/render"
)

// MachineRenderRequest 只传已冻结任务和本次文件字段，不查询主机、模板或凭据。
// 调用方负责提供完整Values和Slot；沿用领域校验，不新增规范化或大小限制。
type MachineRenderRequest struct {
	Task   model.Task
	Kind   catalog.FileKind
	Slot   string
	Values map[catalog.SystemValue]string
}

// MachineRenderer 是编写模块的机器文件入口，仅供可信机器协议适配器调用。
// 输入为冻结任务、文件种类、脚本插槽和真实系统值；不注入预览示例。
// 成功只返回请求的一个文件，字节和空文件形式沿用领域结果，无文件列表顺序。
// ESXi辅助文件先读取原配置，失败为 immutable boot configuration unavailable；随后渲染错误或
// 领域问题统一为 machine rendering rejected；不新增原因代码，也不靠错误文字分支。
// 只读冻结输入和原配置，不写任务或仓储，无事务、版本冲突与提交复查。
// 相同冻结输入和原配置可重复渲染；并发调用方不得修改共享输入，原配置由实现保证不可变。
// 无审计和通知；上下文原样传给原配置读取，纯内存渲染不新增取消或超时检查。
type MachineRenderer interface {
	Render(context.Context, MachineRenderRequest) ([]byte, error)
}
type machineRenderer struct{ bootConfig port.BootConfig }

// NewMachineRenderer 只构造上述只读入口；ESXi辅助文件需要BootConfig，其他文件允许nil。
// 构造不读取数据、不校验输入、不写入、不失败；重建无副作用，无并发复查、审计、通知或取消。
func NewMachineRenderer(reader port.BootConfig) MachineRenderer { return &machineRenderer{reader} }

// Render 遵守MachineRenderer的输入、错误优先顺序、只读、重试和取消约定。
func (s *machineRenderer) Render(ctx context.Context, r MachineRenderRequest) ([]byte, error) {
	fw := catalog.Firmware("")
	if r.Task.Execution != nil {
		fw = r.Task.Execution.Firmware
	}
	original := ""
	if r.Task.Snapshot.Format == catalog.FormatESXiKS && r.Kind == catalog.FileAux {
		if s.bootConfig == nil {
			return nil, errors.New("immutable boot configuration unavailable")
		}
		var err error
		original, err = s.bootConfig.ReadESXiBootConfig(ctx, r.Task.Snapshot.ISO)
		if err != nil {
			return nil, errors.New("immutable boot configuration unavailable")
		}
	}
	body, problems, err := render.Render(render.Request{Snapshot: r.Task.Snapshot, Firmware: fw, ESXiBootConfig: original, Values: r.Values, Kind: r.Kind, Slot: r.Slot})
	if err != nil || len(problems) != 0 {
		return nil, errors.New("machine rendering rejected")
	}
	return body, nil
}
