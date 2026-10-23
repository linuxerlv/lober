package console

import (
	"github.com/lober/lober/internal/application/hosts"
	"testing"
)

func TestCSV转换保留空数组(t *testing.T) {
	p := csvPlanOf(hosts.ImportPlan{Rows: []hosts.ImportRow{{Row: 2, SN: "SN-1", Action: "create"}}, Total: 1})
	if p.Total != 1 || p.Rows[0].Row != 2 || p.Rows[0].SN != "SN-1" || p.Rows[0].Action != "create" || p.Rows[0].Problems == nil || len(p.Rows[0].Problems) != 0 {
		t.Fatal(p)
	}
	if csvPlanOf(hosts.ImportPlan{}).Rows == nil {
		t.Fatal("空行必须输出数组")
	}
}
