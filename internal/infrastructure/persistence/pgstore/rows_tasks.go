package pgstore

import (
	"fmt"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
)

// 任务与事件的行类型，约定同 rows_registry.go。

// taskRow 是 tasks 表的一行。HostSN、ISOID、TemplateID 与 TemplateVersion 和快照里的重复（见 model.Task），查任务与建外键
// 靠它们，不必解开快照。部分唯一索引 idx_tasks_open 守着 C31：每台主机最多一个未结束的任务，结束了的不占位。
type taskRow struct {
	ID              string `gorm:"primaryKey"`
	HostSN          string `gorm:"not null;index:idx_tasks_host_sn;uniqueIndex:idx_tasks_open,where:state NOT IN ('done'\\, 'failed'\\, 'cancelled')"`
	ISOID           string `gorm:"not null;index:idx_tasks_iso_id"`
	TemplateID      string `gorm:"not null;index:idx_tasks_template_version,priority:1"`
	TemplateVersion int    `gorm:"not null;index:idx_tasks_template_version,priority:2"`
	State           string `gorm:"not null;index:idx_tasks_state;check:state IN ('pending', 'booting', 'installing', 'rebooting', 'initializing', 'done', 'failed', 'cancelled')"`
	Snapshot        string `gorm:"type:jsonb;not null"` // model.Snapshot 的 JSON
	CreatedBy       string `gorm:"not null"`
	// CreatedAt 由调用方给，仓储不读时钟。GORM 见到这个字段名，会在新建时把零值填成当前时间，这里关掉。
	CreatedAt int64 `gorm:"not null;autoCreateTime:false;index:idx_tasks_created_at;check:created_at > 0"`
	Rev       int64 `gorm:"not null;check:rev >= 1"`
	// LastSeq 是这个任务分配过的最大事件 Seq，还没有事件时为 0。追加事件先锁住这一行，在同一个事务里往上加，所以同一个任务
	// 并发追加，Seq 照样连续、不重。它不是 model.Task 的字段：追加事件不算改任务，Rev 不跟着变。
	LastSeq   int64  `gorm:"not null"`
	Execution string `gorm:"type:jsonb;not null;default:'null'"`

	// 下面三个只为让 AutoMigrate 建外键 fk_tasks_host、fk_tasks_iso 与 fk_tasks_template，读写都不碰它们。
	Host     hostRow            `gorm:"foreignKey:HostSN;references:SN;constraint:OnDelete:RESTRICT"`
	ISO      isoRow             `gorm:"foreignKey:ISOID;references:ID;constraint:OnDelete:RESTRICT"`
	Template templateVersionRow `gorm:"foreignKey:TemplateID,TemplateVersion;references:TemplateID,Version;constraint:OnDelete:RESTRICT"`
}

func (taskRow) TableName() string { return "tasks" }

// taskEventRow 是 task_events 表的一行：任务的一条事件，只追加，不更新。Key 非空时由 (task_id, key) 的部分唯一索引兜底去重；
// 两个列都标了同名的 tag，漏标一个就会静默建成单列索引（见 schema.go）。
type taskEventRow struct {
	TaskID string `gorm:"primaryKey;uniqueIndex:idx_task_events_key,priority:1"`
	Seq    int64  `gorm:"primaryKey;check:seq >= 1"`
	At     int64  `gorm:"not null;check:at > 0"`
	Kind   string `gorm:"not null"`
	Key    string `gorm:"not null;uniqueIndex:idx_task_events_key,priority:2,where:key <> ''"`
	Fields string `gorm:"type:jsonb;not null"` // map[string]string 的 JSON

	// Task 只为建外键 fk_task_events_task。
	Task taskRow `gorm:"foreignKey:TaskID;references:ID;constraint:OnDelete:RESTRICT"`
}

func (taskEventRow) TableName() string { return "task_events" }

// toTaskRow 把任务 t 转成一行，还没有事件。
func toTaskRow(t model.Task) (taskRow, error) {
	snap, err := encodeJSON(t.Snapshot)
	if err != nil {
		return taskRow{}, err
	}
	return taskRow{
		ID: t.ID, HostSN: t.HostSN, ISOID: t.ISOID, TemplateID: t.TemplateID, TemplateVersion: t.TemplateVersion,
		State: string(t.State), Snapshot: snap, CreatedBy: t.CreatedBy, CreatedAt: t.CreatedAt, Rev: t.Rev,
	}, nil
}

// task 把一行转回 model.Task。快照每次都从 JSON 解出新的值，所以返回值就是深拷贝，nil 与空的 map、切片也照写进去的分开。
func (r taskRow) task() (model.Task, error) {
	t := model.Task{
		ID: r.ID, HostSN: r.HostSN, ISOID: r.ISOID, TemplateID: r.TemplateID, TemplateVersion: r.TemplateVersion,
		State: model.TaskState(r.State), CreatedBy: r.CreatedBy, CreatedAt: r.CreatedAt, Rev: r.Rev,
	}
	if err := decodeJSON(r.Snapshot, &t.Snapshot); err != nil {
		return model.Task{}, fmt.Errorf("任务 %s 的快照：%w", r.ID, err)
	}
	if r.Execution != "" {
		if err := decodeJSON(r.Execution, &t.Execution); err != nil {
			return model.Task{}, fmt.Errorf("任务执行状态无法读取")
		}
	}
	return t, nil
}

// toEventRow 把要追加给任务 taskID 的事件 e 转成一行，Seq 由调用方分配。
func toEventRow(taskID string, seq int64, e repository.NewEvent) (taskEventRow, error) {
	fields, err := encodeJSON(e.Fields)
	if err != nil {
		return taskEventRow{}, err
	}
	return taskEventRow{TaskID: taskID, Seq: seq, At: e.At, Kind: e.Kind, Key: e.Key, Fields: fields}, nil
}

// event 把一行转回 repository.Event，字段同样每次解出新的 map。
func (r taskEventRow) event() (repository.Event, error) {
	e := repository.Event{TaskID: r.TaskID, Seq: r.Seq, NewEvent: repository.NewEvent{At: r.At, Kind: r.Kind, Key: r.Key}}
	if err := decodeJSON(r.Fields, &e.Fields); err != nil {
		return repository.Event{}, fmt.Errorf("任务 %s 的事件 %d 的字段：%w", r.TaskID, r.Seq, err)
	}
	return e, nil
}
