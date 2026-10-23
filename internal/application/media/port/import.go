package port

import (
	"context"
	"errors"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
)

// Open 只建会话。调用方按主源、额外源的顺序懒加载；Close 只清理临时内容。
type ImportFiles interface {
	Open(context.Context, model.ISO) (ImportSession, error)
}
type ImportSession interface {
	OpenSource(context.Context, model.Source) (MediaView, error)
	// BeginPrepare 只创建制品临时根。应用随后检查 Debian，再调用 Prepare。
	BeginPrepare(context.Context, ExtractionPlan) error
	// Prepare 不重建临时根；消费所有来源，检查指定必需角色，最后复制 Windows 共享树。
	Prepare(context.Context, ExtractionPlan) (map[string]string, error)
	// Verify 必须在所有复制完成后调用；比对打开时大小、修改时间和摘要。
	Verify(context.Context) error
	// Publish 先共享树再制品树，复用完整同内容树，冲突不覆盖。
	Publish(context.Context) (Publication, error)
	// Rollback 只撤销本会话新建且拥有者仍匹配的树；数据库结果未知不得调用。
	Rollback(context.Context, Publication) error
	Close() error
}

// Entries 只列直接子项，按名称排序；不存在仍返回 fs.ErrNotExist。
type MediaView interface {
	Stat(context.Context, string) (Entry, error)
	Entries(context.Context, string) ([]Entry, error)
	Text(context.Context, string, int64) (string, error)
	WIM(context.Context, string) ([]mediarules.EditionFact, error)
	Digest(context.Context) (string, error)
}
type Entry struct {
	Name             string
	Size             int64
	IsDir, IsRegular bool
}
type ExtractionPlan struct {
	ISO                      model.ISO
	Identified               mediarules.Identified
	RepoPaths, RequiredRoles []string
}

// 即使发布一半失败也返回已发布事实；应用确认数据库结果后才可撤销。
type Publication struct {
	Artifacts              map[string]string
	MadeArtifacts, MadeSMB bool
}

var ErrMediaMalformed = errors.New("invalid media format")
var ErrTextTooLarge = errors.New("media text exceeds limit")
var ErrWIMMalformed = errors.New("invalid WIM metadata")
var ErrWIMCompressed = errors.New("unsupported WIM metadata compression")

type ExtractionFault struct{ Kind, Name string }

func (f *ExtractionFault) Error() string { return f.Kind + ": " + f.Name }
