// 本文件实现 contract.FactStore：facts 的合并写入与读取。对应实现计划
// 任务 7，依据 SPEC §5.5。
package sqlite

import "context"

// MergeFacts 把给定键值对合并写入该任务的 fact 集合：已存在的键被覆盖，
// 不存在的键被新增，未出现在本次调用中的既有键保持不变——写入是合并而非
// 替换，不这样做会导致后一个 hook 的写入抹掉前一个 hook 的结果（SPEC
// §5.5，计划任务 7）。
//
// 用 UPSERT（ON CONFLICT DO UPDATE）逐键写，不先删后插——先删后插会有
// 一个「该任务的 facts 暂时不存在」的窗口，且没有必要：task_facts 的
// 主键是 (task_id, key)，UPSERT 天然表达「已存在则覆盖，不存在则新增」。
func (s *Store) MergeFacts(ctx context.Context, taskID string, facts map[string]string) error {
	for k, v := range facts {
		_, err := s.db.ExecContext(ctx, `INSERT INTO task_facts (task_id, key, value, updated_at)
			VALUES (?, ?, ?, ?)
			ON CONFLICT (task_id, key) DO UPDATE SET value = excluded.value, updated_at = excluded.updated_at`,
			taskID, k, v, currentUnixTime())
		if err != nil {
			return translateConstraintErr(err, "MergeFacts")
		}
	}
	return nil
}

// GetFacts 返回该任务当前的全部 facts（早先写入的合并结果）。任务不存在
// 或尚无任何 fact 时返回空 map 而不是错误——facts 是纯粹的 KV 存储，没有
// 「facts 集合不存在」这个错误态，只有「facts 集合是空的」。
func (s *Store) GetFacts(ctx context.Context, taskID string) (map[string]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT key, value FROM task_facts WHERE task_id = ?`, taskID)
	if err != nil {
		return nil, translateConstraintErr(err, "GetFacts")
	}
	defer rows.Close()

	out := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, translateConstraintErr(err, "GetFacts")
		}
		out[k] = v
	}
	if err := rows.Err(); err != nil {
		return nil, translateConstraintErr(err, "GetFacts")
	}
	return out, nil
}
