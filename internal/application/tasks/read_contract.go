package tasks

import (
	"context"
	"errors"
	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/application/tasks/port"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

var ErrBadRequest = errors.New("任务查询参数无效")

// ListRequest 的筛选文本可空；时间用十进制非负整数，0表示没有这一端限制。
// 应用负责主机编号规范化和状态检查；分页沿用仓储默认值及最大500条。
type ListRequest struct {
	PageRequest
	HostSN, State, CreatedFrom, CreatedTo string
}
type TaskPage struct {
	Items []TaskChoice
	Next  string
	Total *int64
}
type PreviewRequest struct{ Firmware catalog.Firmware }
type PreviewResult struct {
	Files    []ports.PreviewFile
	Sample   []string
	Problems []model.Problem
}

// Reader 只读取任务和冻结预览，不写业务、审计或通知；不启动后台工作。
// 操作者来自可信身份入口，所有方法先检查 task:read，再检查参数和调用依赖。
// 查询失败直接返回原错误；普通预览问题随结果按原顺序返回，由网页判定展示状态。
// 身份错误沿用ports分类，参数错误为ErrBadRequest或仓储ErrInvalid，编号问题保留原原因代码。
// 重复调用允许读到新状态及诊断；每次只取仓储事实，不保证多页或多次读取使用同一份数据。
// 冻结快照不重算；没有写入，所以不涉及原子写、提交时复查或版本冲突重试。
// context原样传给仓储和渲染器，取消与超时由原能力处理，不新增超时或后台工作。
type Reader interface {
	// List 按分页、起始时间、结束时间、范围、主机编号、状态的顺序检查。
	// 保留仓储顺序、游标及总数，空任务是非空数组；每项先取消诊断再重试诊断。
	List(context.Context, ports.Actor, ListRequest) (TaskPage, error)
	// Get 的编号原样交仓储校验，不增加应用格式和必填检查；成功后取得两项诊断。
	// 快照只取已保存任务，不读取当前主机、镜像、模板或组件。
	Get(context.Context, ports.Actor, string) (TaskChoice, error)
	// Preview 先检查非空请求固件，再查任务；空值回退快照，再回退UEFI并追加firmware样例标签。
	// 只向渲染器传原快照及查回的任务编号；先返回渲染错误，再交问题，成功才追加标签并标记文件。
	Preview(context.Context, ports.Actor, string, PreviewRequest) (PreviewResult, error)
}
type ReaderDeps struct {
	Tasks       port.TaskReader
	Diagnostics port.TaskDiagnostics
	Renderer    ports.FrozenPreview
}
