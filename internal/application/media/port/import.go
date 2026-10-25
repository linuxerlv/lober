package port

import (
	"context"
	"errors"
	mediarules "github.com/lober/lober/internal/domain/media"
	"github.com/lober/lober/internal/domain/model"
)

// Open 只建会话。调用方按主源、额外源的顺序懒加载；Close 只清理临时内容。
type ImportFiles interface {
	// Open 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportFiles.Open」行及该节共同边界。
	Open(context.Context, model.ISO) (ImportSession, error)
}
type ImportSession interface {
	// OpenSource 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.OpenSource」行及该节共同边界。
	OpenSource(context.Context, model.Source) (MediaView, error)
	// BeginPrepare 只创建制品临时根。应用随后检查 Debian，再调用 Prepare。
	// BeginPrepare 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.BeginPrepare」行及该节共同边界。
	BeginPrepare(context.Context, ExtractionPlan) error
	// Prepare 不重建临时根；消费所有来源，检查指定必需角色，最后复制 Windows 共享树。
	// Prepare 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.Prepare」行及该节共同边界。
	Prepare(context.Context, ExtractionPlan) (map[string]string, error)
	// Verify 必须在所有复制完成后调用；比对打开时大小、修改时间和摘要。
	// Verify 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.Verify」行及该节共同边界。
	Verify(context.Context) error
	// Publish 先共享树再制品树，复用完整同内容树，冲突不覆盖。
	// Publish 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.Publish」行及该节共同边界。
	Publish(context.Context) (Publication, error)
	// Rollback 只撤销本会话新建且拥有者仍匹配的树；数据库结果未知不得调用。
	// Rollback 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.Rollback」行及该节共同边界。
	Rollback(context.Context, Publication) error
	// Close 的八项能力约定唯一连接 docs/contracts/media.md 第10节「ImportSession.Close」行及该节共同边界。
	Close() error
}

// Entries 只列直接子项，按名称排序；不存在仍返回 fs.ErrNotExist。
type MediaView interface {
	// Stat 的八项能力约定唯一连接 docs/contracts/media.md 第10节「MediaView.Stat」行及该节共同边界。
	Stat(context.Context, string) (Entry, error)
	// Entries 的八项能力约定唯一连接 docs/contracts/media.md 第10节「MediaView.Entries」行及该节共同边界。
	Entries(context.Context, string) ([]Entry, error)
	// Text 的八项能力约定唯一连接 docs/contracts/media.md 第10节「MediaView.Text」行及该节共同边界。
	Text(context.Context, string, int64) (string, error)
	// WIM 的八项能力约定唯一连接 docs/contracts/media.md 第10节「MediaView.WIM」行及该节共同边界。
	WIM(context.Context, string) ([]mediarules.EditionFact, error)
	// Digest 的八项能力约定唯一连接 docs/contracts/media.md 第10节「MediaView.Digest」行及该节共同边界。
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
