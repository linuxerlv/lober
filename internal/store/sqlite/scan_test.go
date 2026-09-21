// 本文件测试行扫描辅助：零值时间经真实数据库往返仍是零值、JSON 列的
// nil/空/有内容三种情况往返一致。对应实现计划任务 2，依据 SPEC §3.5。
//
// 时间列不做 NULL 包裹（见 scan.go 顶部长注释：M1 schema 把全部时间列
// 声明为 NOT NULL DEFAULT 0，没有可为 NULL 的列）——本文件验证的是
// 「裸 int64 写入 NOT NULL DEFAULT 0 列后原样读回，不被驱动或 SQLite
// 本身的整数存储悄悄改变」，而不是 NULL 互转。
package sqlite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/google/go-cmp/cmp"
)

// TestScan_ZeroTimeRoundTripsToZeroNotEpoch 验证零值时间（表示「从未
// 发生」）写入 tasks.last_signal_at 后原样读回仍是 0，不会被 SQLite 的
// 整数存储或驱动的类型转换悄悄变成非零值——last_signal_at 是 NOT NULL
// DEFAULT 0 列，0 本身就是「未设置」的语义零值（model.Task.LastSignalAt
// 字段注释：「距今多久收到过信号」，从未收到时天然是 0），这条测试锁住
// 的是「0 写进去、0 读出来」这个看似平凡但容易被无谓的 NULL 包裹逻辑
// 破坏的往返。
func TestScan_ZeroTimeRoundTripsToZeroNotEpoch(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("file:" + filepath.Join(dir, "scan.db"))
	if err != nil {
		t.Fatalf("Open() 失败: %v", err)
	}
	defer db.Close()

	if _, err := db.ExecContext(context.Background(), `INSERT INTO tasks (
		id, created_at, host_sn, template_id, template_version, artifact_set_id,
		state, state_updated_at, current_phase, phase_entered_at, last_signal_at
	) VALUES ('task-zero-time', 0, '', 'tpl-1', 1, 'art-1', 'pending', 0, 'pending', 0, 0)`); err != nil {
		t.Fatalf("插入零值时间任务失败: %v", err)
	}

	var got int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT last_signal_at FROM tasks WHERE id = 'task-zero-time'`).Scan(&got); err != nil {
		t.Fatalf("查询 last_signal_at 失败: %v", err)
	}
	if got != 0 {
		t.Errorf("last_signal_at 写入 0 后读回 = %d, want 0 —— SPEC §3.5 要求「上次信号"+
			"时间」在没收到任何信号时读出来必须是 0，不能变成一个非零的历史时刻，"+
			"否则「距今多久」会算出异常长的时间差，诊断信息就废了", got)
	}
}

func TestScan_NonZeroTimeRoundTrips(t *testing.T) {
	dir := t.TempDir()
	db, err := Open("file:" + filepath.Join(dir, "scan_nonzero.db"))
	if err != nil {
		t.Fatalf("Open() 失败: %v", err)
	}
	defer db.Close()

	const ts = int64(1_700_000_000)
	if _, err := db.ExecContext(context.Background(), `INSERT INTO tasks (
		id, created_at, host_sn, template_id, template_version, artifact_set_id,
		state, state_updated_at, current_phase, phase_entered_at, last_signal_at
	) VALUES ('task-nonzero-time', 0, '', 'tpl-1', 1, 'art-1', 'pending', 0, 'pending', 0, ?)`, ts); err != nil {
		t.Fatalf("插入非零时间任务失败: %v", err)
	}

	var got int64
	if err := db.QueryRowContext(context.Background(),
		`SELECT last_signal_at FROM tasks WHERE id = 'task-nonzero-time'`).Scan(&got); err != nil {
		t.Fatalf("查询 last_signal_at 失败: %v", err)
	}
	if got != ts {
		t.Errorf("非零时间往返后 = %d, want %d", got, ts)
	}
}

// jsonRoundTripCase 覆盖 JSON 列的 nil、空、有内容三种情况。
type jsonPathsCase struct {
	name string
	in   []string
}

// TestScan_JSONSliceRoundTrip 验证切片字段（如 ArtifactPaths.Initrd）的
// nil、空切片、非空切片三种情况编解码后与写入前一致，且编码结果不是
// 空字符串（空字符串不是合法 JSON，解码会报错——计划任务 2 明确要求）。
func TestScan_JSONSliceRoundTrip(t *testing.T) {
	cases := []jsonPathsCase{
		{name: "nil切片", in: nil},
		{name: "空切片", in: []string{}},
		{name: "有内容", in: []string{"a.img", "b.img"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded, err := encodeJSON(c.in)
			if err != nil {
				t.Fatalf("encodeJSON(%#v) 失败: %v", c.in, err)
			}
			if encoded == "" {
				t.Fatalf("encodeJSON(%#v) 产出空字符串——空字符串不是合法 JSON，"+
					"decodeJSON 解码时会报错，而不是还原出一个空切片", c.in)
			}

			var out []string
			if err := decodeJSON(encoded, &out); err != nil {
				t.Fatalf("decodeJSON(%q) 失败: %v", encoded, err)
			}
			if diff := cmp.Diff(c.in, out); diff != "" {
				t.Errorf("JSON 切片往返不一致（-want +got）:\n%s", diff)
			}
		})
	}
}

// TestScan_JSONMapRoundTrip 验证 map 字段（如 ArtifactPaths.Extra、
// Task.FormData）的 nil、空 map、非空 map 三种情况编解码后与写入前一致。
func TestScan_JSONMapRoundTrip(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
	}{
		{name: "nil map", in: nil},
		{name: "空map", in: map[string]string{}},
		{name: "有内容", in: map[string]string{"boot_cfg": "x.cfg", "wimboot": "y.efi"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			encoded, err := encodeJSON(c.in)
			if err != nil {
				t.Fatalf("encodeJSON(%#v) 失败: %v", c.in, err)
			}
			if encoded == "" {
				t.Fatalf("encodeJSON(%#v) 产出空字符串", c.in)
			}

			var out map[string]string
			if err := decodeJSON(encoded, &out); err != nil {
				t.Fatalf("decodeJSON(%q) 失败: %v", encoded, err)
			}
			if diff := cmp.Diff(c.in, out); diff != "" {
				t.Errorf("JSON map 往返不一致（-want +got）:\n%s", diff)
			}
		})
	}
}
