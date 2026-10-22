package pgstore_test

import (
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore/pgtest"
	"strings"
	"testing"
)

func Test任务引用查询具有全表索引(t *testing.T) {
	st := pgtest.OpenStore(t)
	var indexes []struct {
		Name       string
		Definition string
	}
	if err := st.DBForTest().WithContext(t.Context()).Raw(`SELECT indexname AS name,indexdef AS definition FROM pg_indexes WHERE schemaname=current_schema() AND tablename='tasks'`).Scan(&indexes).Error; err != nil {
		t.Fatal(err)
	}
	expected := map[string]string{"idx_tasks_host_sn": "(host_sn)", "idx_tasks_iso_id": "(iso_id)", "idx_tasks_template_version": "(template_id, template_version)"}
	for name, columns := range expected {
		found := false
		for _, idx := range indexes {
			if idx.Name == name && strings.Contains(idx.Definition, columns) && !strings.Contains(idx.Definition, " WHERE ") {
				found = true
			}
		}
		if !found {
			t.Error("missing full index", name, columns)
		}
	}
}
