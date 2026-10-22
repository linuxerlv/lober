package console

import (
	"fmt"
	"testing"

	"github.com/lober/lober/internal/core/catalog"
	"github.com/lober/lober/internal/core/model"
	"github.com/lober/lober/internal/store"
)

func TestNumberedWizardListsAndEligibleTemplateTotals(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	hosts := responseData[struct {
		Items []wizardHostItem `json:"items"`
		Total int64            `json:"total"`
	}](t, httpCall(s, "GET", "/console/v1/wizard/hosts?page=2&page_size=2", "", token))
	if hosts.Total != 3 || len(hosts.Items) != 1 || hosts.Items[0].SN != "SN-3" {
		t.Fatalf("host page: %+v", hosts)
	}
	readyISO(t, st, "Z-last", "rocky")
	isos := responseData[struct {
		Items []wizardISOItem `json:"items"`
		Total int64           `json:"total"`
	}](t, httpCall(s, "POST", "/console/v1/wizard/isos", `{"hostSNs":["SN-1"],"page":2,"page_size":1}`, token))
	if isos.Total != 2 || len(isos.Items) != 1 || isos.Items[0].Name != "Z-last" {
		t.Fatalf("iso page: %+v", isos)
	}
	offered, err := st.CreateTemplate(t.Context(), model.Template{Name: "a-unusable", Author: "author", Family: "linux", Series: []catalog.Series{"rocky"}, Body: tpl.Body, Params: append(tpl.Params, model.ParamDecl{Name: "edition", Type: "enum", Offer: "editions"}), Bindings: tpl.Bindings})
	if err != nil {
		t.Fatal(err)
	}
	key := model.TemplateKey{ID: offered.ID, Version: 1}
	if err := st.SubmitTemplate(t.Context(), key, 1, store.Audit{Actor: "author", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err := st.ApproveTemplate(t.Context(), key, 2, "reviewer", store.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	for page := 1; page <= 2; page++ {
		result := responseData[struct {
			Items []wizardTemplateItem `json:"items"`
			Total int64                `json:"total"`
		}](t, httpCall(s, "POST", "/console/v1/wizard/templates", fmt.Sprintf(`{"isoID":%q,"page":%d,"page_size":1}`, iso.ID, page), token))
		if result.Total != 1 {
			t.Fatalf("eligible total: %+v", result)
		}
		if page == 1 && (len(result.Items) != 1 || result.Items[0].ID != tpl.ID) {
			t.Fatalf("eligible first: %+v", result)
		}
		if page == 2 && len(result.Items) != 0 {
			t.Fatalf("eligible empty tail: %+v", result)
		}
	}
	for _, body := range []string{`{"hostSNs":["SN-1"],"page":0}`, `{"hostSNs":["SN-1"],"page_size":0}`, `{"hostSNs":["SN-1"],"page":1,"cursor":""}`, `{"hostSNs":["SN-1"],"page":1,"limit":0}`} {
		if w := httpCall(s, "POST", "/console/v1/wizard/isos", body, token); w.Code != 400 {
			t.Fatalf("%s: %d", body, w.Code)
		}
	}
}

func TestNumberedHookCandidatesPageVersionsIndividually(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	hook := approvedTestHook(t, st, "paging-candidate")
	if _, err := st.NewHookVersion(t.Context(), hook.ID, 1, "alice"); err != nil {
		t.Fatal(err)
	}
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "candidate-target", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{slot:disk}}\n"})
	if err != nil {
		t.Fatal(err)
	}
	for page := 1; page <= 2; page++ {
		url := fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=disk&prefix=paging-candidate&page=%d&page_size=1", tpl.ID, page)
		result := responseData[struct {
			Items []hookCandidateResponse `json:"items"`
			Total int64                   `json:"total"`
		}](t, httpCall(s, "GET", url, "", token))
		if result.Total != 2 || len(result.Items) != 1 || result.Items[0].Hook.ID != hook.ID || result.Items[0].Hook.Version != page {
			t.Fatalf("version page: %+v", result)
		}
	}
}
