package console

import (
	"fmt"
	"testing"

	"github.com/lober/lober/internal/core/catalog"

	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

func Test插槽扫描位置顺序与不完整正文(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "slots", Author: "admin", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "中文{{slot:分区}}\n{{slot:执行@post-target}}\n{{slot:坏名字 with space}}\n{{slot:分区}}\n", Assembly: map[string][]model.Instance{"unused": {}}})
	if err != nil {
		t.Fatal(err)
	}
	before, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	p := responseData[slotList](t, httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/slots", tpl.ID), "", token))
	if len(p.Slots) != 2 || p.Slots[0].Name != "分区" || p.Slots[0].Exec || p.Slots[0].Line != 1 || p.Slots[0].Col != 3 || p.Slots[1].Name != "执行" || !p.Slots[1].Exec || p.Slots[1].Timing != "post-target" || p.Slots[1].Line != 2 {
		t.Fatal(p)
	}
	duplicate, malformed := false, false
	for _, problem := range p.Problems {
		if problem.Subject.ID != tpl.ID+"@1" || problem.Subject.Line == 0 {
			t.Fatal(problem)
		}
		duplicate = duplicate || problem.Code == "slot_duplicate"
		malformed = malformed || problem.Code == "placeholder_malformed"
	}
	if !duplicate || !malformed {
		t.Fatal(p.Problems)
	}
	after, _ := st.ListAudit(t.Context(), store.AuditQuery{Limit: 500})
	if len(before.Items) != len(after.Items) {
		t.Fatal("slots audited")
	}
}
