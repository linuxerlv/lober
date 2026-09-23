// 本文件实现 contract.ArtifactSetStore：制品集的状态更新与导入写回。
// 对应实现计划任务 9，依据 SPEC §2.1、§2.7。
//
// # FailReason 的落盘位置（契约缺口的保守绕法，详见文件末尾说明）
//
// schema（internal/store/sqlite/schema/001_init.sql）的 artifact_sets 表
// 没有专门的失败原因列，且本次任务不允许 ALTER TABLE（schema 是数据
// 契约）。本实现把 FailReason 编码进既有的 paths_json 列——该列本来只装
// ArtifactPaths，现在改为装一个同时含 Paths 与 FailReason 的信封结构体
// artifactSetContentEnvelope。两个写方法因此都必须先读出信封、只改自己
// 负责的那一半字段、再整体写回（read-modify-write），而不能像改动前那样
// 各自一条不依赖对方的 UPDATE 语句。
package sqlite

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

const artifactSetColumns = `id, name, os_family, os_version, arch, firmware,
	source_iso_sha256, state, manifest_sha256, paths_json`

// artifactSetContentEnvelope 是 paths_json 列真正编码的 JSON 形态：除了
// 原本的 ArtifactPaths 四个字段，额外带上 FailReason——这是把
// model.ArtifactSet.FailReason 塞进一个没有专属列的既有 JSON 列的具体
// 手法（见文件头「FailReason 的落盘位置」）。
type artifactSetContentEnvelope struct {
	Kernel     string            `json:"kernel"`
	Initrd     []string          `json:"initrd"`
	Repo       string            `json:"repo"`
	Extra      map[string]string `json:"extra"`
	FailReason string            `json:"failReason"`
}

type artifactSetRowScanner interface {
	Scan(dest ...any) error
}

func scanArtifactSetRow(row artifactSetRowScanner) (model.ArtifactSet, error) {
	var (
		a         model.ArtifactSet
		pathsJSON string
	)
	err := row.Scan(
		&a.ID, &a.Name, &a.OSFamily, &a.OSVersion, &a.Arch, &a.Firmware,
		&a.SourceISOSHA256, &a.State, &a.ManifestSHA256, &pathsJSON,
	)
	if err != nil {
		return model.ArtifactSet{}, err
	}
	paths, reason, err := decodeArtifactContentEnvelope(pathsJSON)
	if err != nil {
		return model.ArtifactSet{}, fmt.Errorf("sqlite: 解码制品集 %q 的 paths_json 失败: %w", a.ID, err)
	}
	a.Paths = paths
	a.FailReason = reason
	return a, nil
}

// decodeArtifactContentEnvelope 解码 paths_json 到 (ArtifactPaths,
// FailReason)，Extra 字段解码结果为空 map 时归一为 nil。
//
// 理由与 scan.go 的 decodeJSONMapNilIfEmpty 相同：Extra 在 Go 层用 nil
// 表达「没有额外文件」，与「有一个空 map」在业务语义上没有区别，但
// go-cmp 会把两者视为不同值。conformance 套件里
// testArtifactSetStateUpdateDoesNotClobberPaths 构造的 paths 字面量没有
// 显式设置 Extra（零值 nil），若解码后变成非 nil 的空 map，
// UpdateArtifactSetState「不触及 Paths」这条断言会误报——这不是
// UpdateArtifactSetState 真的改了 Paths，是 SetArtifactSetContent 写入
// 时把 nil 归一编码成 "{}" 后，两次查询用不同的解码方式导致的假阳性。
// Initrd 不做同样的归一：conformance 套件里出现 Initrd 的用例都用非 nil
// 切片字面量，目前没有需要这层归一的断言。
func decodeArtifactContentEnvelope(pathsJSON string) (model.ArtifactPaths, string, error) {
	var env artifactSetContentEnvelope
	if err := decodeJSON(pathsJSON, &env); err != nil {
		return model.ArtifactPaths{}, "", err
	}
	extra := env.Extra
	if len(extra) == 0 {
		extra = nil
	}
	paths := model.ArtifactPaths{
		Kernel: env.Kernel,
		Initrd: env.Initrd,
		Repo:   env.Repo,
		Extra:  extra,
	}
	return paths, env.FailReason, nil
}

// emptyArtifactContentJSON 是「尚无内容、尚无失败原因」的信封 JSON 编码
// ——CreateArtifactSet 登记时通常只知道名字与本地路径，Paths 要等识别与
// 抽取完成后才知道（计划任务 9「为什么需要这一步」），此刻写入的是全部
// 字段的零值，Initrd/Extra 须归一（scan.go：JSON 列不写空串，nil 切片/
// map 归一编码成合法的空复合字面量）。
func emptyArtifactContentJSON() (string, error) {
	return encodeArtifactContentEnvelope(model.ArtifactPaths{}, "")
}

// CreateArtifactSet 登记一个新制品集。Paths 字段此刻通常为零值（尚未
// 导入完成），调用方若已经带着 Paths 字面量传入（比如测试夹具），原样
// 编码写入；ArtifactSetContent 的实际写回路径是 SetArtifactSetContent。
// FailReason 同理，若调用方传入非空值（如测试夹具）原样写入。
func (s *Store) CreateArtifactSet(ctx context.Context, set model.ArtifactSet) (model.ArtifactSet, error) {
	pathsJSON, err := encodeArtifactContentEnvelope(set.Paths, set.FailReason)
	if err != nil {
		return model.ArtifactSet{}, err
	}

	query := fmt.Sprintf(`INSERT INTO artifact_sets (%s) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, artifactSetColumns)
	if _, err := s.db.ExecContext(ctx, query,
		set.ID, set.Name, set.OSFamily, set.OSVersion, set.Arch, set.Firmware,
		set.SourceISOSHA256, set.State, set.ManifestSHA256, pathsJSON,
	); err != nil {
		return model.ArtifactSet{}, translateConstraintErr(err, "CreateArtifactSet")
	}
	return set, nil
}

// encodeArtifactContentEnvelope 把 (ArtifactPaths, FailReason) 编码成
// paths_json 列的信封 JSON，Initrd/Extra 为 nil 时归一成空复合字面量
// （"[]"/"{}"），不写裸空串。
func encodeArtifactContentEnvelope(paths model.ArtifactPaths, failReason string) (string, error) {
	env := artifactSetContentEnvelope{
		Kernel:     paths.Kernel,
		Initrd:     nonNilStringSlice(paths.Initrd),
		Repo:       paths.Repo,
		Extra:      nonNilStringMap(paths.Extra),
		FailReason: failReason,
	}
	encoded, err := encodeJSON(env)
	if err != nil {
		return "", fmt.Errorf("sqlite: 编码 paths_json 失败: %w", err)
	}
	return encoded, nil
}

// GetArtifactSet 按 id 查制品集。查不到返回 contract.ErrNotFound。
func (s *Store) GetArtifactSet(ctx context.Context, id string) (model.ArtifactSet, error) {
	query := fmt.Sprintf(`SELECT %s FROM artifact_sets WHERE id = ?`, artifactSetColumns)
	row := s.db.QueryRowContext(ctx, query, id)
	a, err := scanArtifactSetRow(row)
	if err != nil {
		return model.ArtifactSet{}, translateConstraintErr(err, "GetArtifactSet")
	}
	return a, nil
}

// ListArtifactSets 列出全部制品集。
func (s *Store) ListArtifactSets(ctx context.Context) ([]model.ArtifactSet, error) {
	query := fmt.Sprintf(`SELECT %s FROM artifact_sets`, artifactSetColumns)
	rows, err := s.db.QueryContext(ctx, query)
	if err != nil {
		return nil, translateConstraintErr(err, "ListArtifactSets")
	}
	defer rows.Close()

	out := make([]model.ArtifactSet, 0)
	for rows.Next() {
		a, err := scanArtifactSetRow(rows)
		if err != nil {
			return nil, translateConstraintErr(err, "ListArtifactSets")
		}
		out = append(out, a)
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "ListArtifactSets")
	}
	return out, nil
}

// UpdateArtifactSetState 只改 State、ManifestSHA256 与 FailReason，不触及
// Paths 与 OSFamily——导入失败路径调用本方法，失败时 Paths 可能尚未写入或
// 只写了一半，绝不能被本方法覆盖成半成品（计划任务 9「两个写方法，各改各
// 的列，互不覆盖」，这条约束由 conformance 套件的
// ArtifactSetStateUpdateDoesNotClobberPaths 断言）。
//
// FailReason 与 Paths 同挤在 paths_json 一列（见文件头「FailReason 的
// 落盘位置」），本方法必须先读出该列已有的 Paths、原样保留，只把
// FailReason 换成本次传入的 reason，再整体写回——这是 read-modify-write，
// 不是像改动前那样一条不依赖任何已有状态的 UPDATE 语句。
func (s *Store) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256, reason string) (model.ArtifactSet, error) {
	var currentPathsJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT paths_json FROM artifact_sets WHERE id = ?`, id).Scan(&currentPathsJSON); err != nil {
		return model.ArtifactSet{}, translateConstraintErr(err, "UpdateArtifactSetState")
	}
	currentPaths, _, err := decodeArtifactContentEnvelope(currentPathsJSON)
	if err != nil {
		return model.ArtifactSet{}, fmt.Errorf("sqlite: UpdateArtifactSetState 解码既有 paths_json 失败: %w", err)
	}
	newPathsJSON, err := encodeArtifactContentEnvelope(currentPaths, reason)
	if err != nil {
		return model.ArtifactSet{}, err
	}

	res, err := s.db.ExecContext(ctx, `UPDATE artifact_sets SET state = ?, manifest_sha256 = ?, paths_json = ? WHERE id = ?`,
		state, manifestSHA256, newPathsJSON, id)
	if err != nil {
		return model.ArtifactSet{}, translateConstraintErr(err, "UpdateArtifactSetState")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.ArtifactSet{}, fmt.Errorf("sqlite: UpdateArtifactSetState 读取受影响行数失败: %w", err)
	}
	if affected == 0 {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return s.GetArtifactSet(ctx, id)
}

// SetArtifactSetContent 写回导入完成后才知道的内容：OSFamily 识别结果与
// Paths。只改这两项，不触及 State/ManifestSHA256——导入 job 成功路径调用
// 本方法（计划任务 9）。
//
// FailReason 与 Paths 同挤在 paths_json 一列，本方法必须先读出该列已有的
// FailReason、原样保留，只把 Paths 换成本次传入的 paths，再整体写回——
// 与 UpdateArtifactSetState 对称的 read-modify-write（见文件头「FailReason
// 的落盘位置」）。实践中这一步通常是多余的：导入成功路径会紧接着调用
// UpdateArtifactSetState(ready, manifestSHA, "") 把 FailReason 清空
// （imports/job.go 的 Run 方法），但本方法不能依赖调用方一定会这么做，
// 独立保证「不主动清空已有 FailReason」才是本方法自己的职责。
func (s *Store) SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	var currentPathsJSON string
	if err := s.db.QueryRowContext(ctx, `SELECT paths_json FROM artifact_sets WHERE id = ?`, id).Scan(&currentPathsJSON); err != nil {
		return model.ArtifactSet{}, translateConstraintErr(err, "SetArtifactSetContent")
	}
	_, currentReason, err := decodeArtifactContentEnvelope(currentPathsJSON)
	if err != nil {
		return model.ArtifactSet{}, fmt.Errorf("sqlite: SetArtifactSetContent 解码既有 paths_json 失败: %w", err)
	}
	pathsJSON, err := encodeArtifactContentEnvelope(paths, currentReason)
	if err != nil {
		return model.ArtifactSet{}, err
	}

	res, err := s.db.ExecContext(ctx, `UPDATE artifact_sets SET os_family = ?, paths_json = ? WHERE id = ?`,
		osFamily, pathsJSON, id)
	if err != nil {
		return model.ArtifactSet{}, translateConstraintErr(err, "SetArtifactSetContent")
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return model.ArtifactSet{}, fmt.Errorf("sqlite: SetArtifactSetContent 读取受影响行数失败: %w", err)
	}
	if affected == 0 {
		return model.ArtifactSet{}, contract.ErrNotFound
	}
	return s.GetArtifactSet(ctx, id)
}

// DeleteArtifactSet 删除制品集。若仍被某个 active 状态的 template 引用，
// 返回 contract.ErrStillReferenced（§2.7：只能 deprecate，不能删）。
// deprecated 的模板引用它不阻止删除（计划任务 9「删除的关键点」）。
func (s *Store) DeleteArtifactSet(ctx context.Context, id string) error {
	var exists int
	if err := s.db.QueryRowContext(ctx, `SELECT 1 FROM artifact_sets WHERE id = ?`, id).Scan(&exists); err != nil {
		return translateConstraintErr(err, "DeleteArtifactSet")
	}

	var refCount int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM templates WHERE artifact_set_id = ? AND state = ?`,
		id, model.TemplateStateActive).Scan(&refCount)
	if err != nil {
		return translateConstraintErr(err, "DeleteArtifactSet(check_refs)")
	}
	if refCount > 0 {
		return contract.ErrStillReferenced
	}

	if _, err := s.db.ExecContext(ctx, `DELETE FROM artifact_sets WHERE id = ?`, id); err != nil {
		return translateConstraintErr(err, "DeleteArtifactSet")
	}
	return nil
}
