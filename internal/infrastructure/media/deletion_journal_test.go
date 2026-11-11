package iso

import (
	"context"
	"encoding/json"
	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/model"
	"os"
	"path/filepath"
	"testing"
)

func Test旧删除日志和坏日志同时读取(t *testing.T) {
	staging := t.TempDir()
	j, err := NewDeletionJournal(staging)
	if err != nil {
		t.Fatal(err)
	}
	// 字段名称与搬迁前一致；正文由旧的公开模型序列化格式组成。
	item := model.ISO{ID: "old", Family: "linux", Series: "rocky", State: model.ISOAcquiring, Rev: 1}
	itemJSON, err := json.Marshal(item)
	if err != nil {
		t.Fatal(err)
	}
	legacy := append([]byte(`{"ISO":`), itemJSON...)
	legacy = append(legacy, []byte(`,"Sources":[]}`)...)
	if err := os.WriteFile(filepath.Join(j.dir, "old.json"), legacy, 0600); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"bad.json": "{", "nil.json": `{"ISO":{"id":"nil"},"Sources":null}`, "unknown.json": `{"Other":1}`, "mismatch.json": string(legacy), "extra.json": string(legacy) + "{}"} {
		if err := os.WriteFile(filepath.Join(j.dir, name), []byte(body), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(filepath.Join(j.dir, "old.json"), filepath.Join(j.dir, "link.json")); err != nil {
		t.Fatal(err)
	}
	snapshots, err := j.List(t.Context())
	if err == nil || len(snapshots) != 1 || snapshots[0].ISO.ID != "old" {
		t.Fatal(snapshots, err)
	}
}
func Test删除日志持久化权限与重复丢弃(t *testing.T) {
	j, err := NewDeletionJournal(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s := port.DeletionSnapshot{ISO: model.ISO{ID: "safe"}, Sources: []model.Source{}}
	if err := j.Persist(t.Context(), s); err != nil {
		t.Fatal(err)
	}
	for p, mode := range map[string]os.FileMode{j.dir: 0700, filepath.Join(j.dir, "safe.json"): 0600} {
		st, err := os.Stat(p)
		if err != nil || st.Mode().Perm() != mode {
			t.Fatal(p, st, err)
		}
	}
	if _, err := os.Stat(filepath.Join(j.dir, ".safe.tmp")); !os.IsNotExist(err) {
		t.Fatal("临时文件未清理", err)
	}
	for range 2 {
		if err := j.Discard(t.Context(), "safe"); err != nil {
			t.Fatal(err)
		}
	}
	if err := j.Persist(t.Context(), port.DeletionSnapshot{ISO: model.ISO{ID: "../outside"}, Sources: []model.Source{}}); err == nil {
		t.Fatal("越界编号通过")
	}
	if err := j.Discard(t.Context(), "../outside"); err == nil {
		t.Fatal("越界丢弃通过")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := j.Persist(ctx, s); err == nil {
		t.Fatal("取消后保存")
	}
	if err := j.Persist(t.Context(), port.DeletionSnapshot{ISO: model.ISO{ID: "safe"}}); err == nil {
		t.Fatal("nil来源通过")
	}
}
