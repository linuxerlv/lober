// 本文件实现 contract.ArtifactSetStore：制品集的状态更新与导入写回。
// 对应实现计划任务 9，依据 SPEC §2.1、§2.7。
package sqlite

import (
	"context"
	"fmt"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

const artifactSetColumns = `id, name, os_family, os_version, arch, firmware,
	source_iso_sha256, state, manifest_sha256, paths_json`

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
	paths, err := decodeArtifactPaths(pathsJSON)
	if err != nil {
		return model.ArtifactSet{}, fmt.Errorf("sqlite: 解码制品集 %q 的 paths_json 失败: %w", a.ID, err)
	}
	a.Paths = paths
	return a, nil
}

// decodeArtifactPaths 解码 paths_json 到 model.ArtifactPaths，Extra 字段
// 解码结果为空 map 时归一为 nil。
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
func decodeArtifactPaths(pathsJSON string) (model.ArtifactPaths, error) {
	var paths model.ArtifactPaths
	if err := decodeJSON(pathsJSON, &paths); err != nil {
		return model.ArtifactPaths{}, err
	}
	if len(paths.Extra) == 0 {
		paths.Extra = nil
	}
	return paths, nil
}

// emptyArtifactPathsJSON 是「尚无内容」的 ArtifactPaths 的 JSON 编码——
// CreateArtifactSet 登记时通常只知道名字与本地路径，Paths 要等识别与
// 抽取完成后才知道（计划任务 9「为什么需要这一步」），此刻写入的是
// ArtifactPaths 的零值，其 Initrd/Extra 字段是 nil，编码前须归一
// （scan.go：JSON 列不写空串，nil 切片/map 归一编码成合法的空复合字面量）。
func emptyArtifactPathsJSON() (string, error) {
	empty := model.ArtifactPaths{Initrd: []string{}, Extra: map[string]string{}}
	return encodeJSON(empty)
}

// CreateArtifactSet 登记一个新制品集。Paths 字段此刻通常为零值（尚未
// 导入完成），调用方若已经带着 Paths 字面量传入（比如测试夹具），原样
// 编码写入；ArtifactSetContent 的实际写回路径是 SetArtifactSetContent。
func (s *Store) CreateArtifactSet(ctx context.Context, set model.ArtifactSet) (model.ArtifactSet, error) {
	pathsJSON, err := encodePathsForWrite(set.Paths)
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

// encodePathsForWrite 把 ArtifactPaths 编码成 JSON，Initrd/Extra 为 nil
// 时归一成空复合字面量（"[]"/"{}"），不写裸空串。
func encodePathsForWrite(paths model.ArtifactPaths) (string, error) {
	normalized := paths
	normalized.Initrd = nonNilStringSlice(paths.Initrd)
	normalized.Extra = nonNilStringMap(paths.Extra)
	encoded, err := encodeJSON(normalized)
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

// UpdateArtifactSetState 只改 State 与 ManifestSHA256，不触及 Paths 与
// OSFamily——导入失败路径调用本方法，失败时 Paths 可能尚未写入或只写了
// 一半，绝不能被本方法覆盖成半成品（计划任务 9「两个写方法，各改各的列，
// 互不覆盖」，这条约束由 conformance 套件的
// ArtifactSetStateUpdateDoesNotClobberPaths 断言）。
func (s *Store) UpdateArtifactSetState(ctx context.Context, id string, state model.ArtifactSetState, manifestSHA256 string) (model.ArtifactSet, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE artifact_sets SET state = ?, manifest_sha256 = ? WHERE id = ?`,
		state, manifestSHA256, id)
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
func (s *Store) SetArtifactSetContent(ctx context.Context, id string, osFamily model.OSFamily, paths model.ArtifactPaths) (model.ArtifactSet, error) {
	pathsJSON, err := encodePathsForWrite(paths)
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
