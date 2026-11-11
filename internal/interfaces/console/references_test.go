package console

import (
	"encoding/json"
	"fmt"
	"testing"
)

func Test引用删除返回结构化引用方(t *testing.T) {
	s, st, token, iso, tpl := wizardFixture(t)
	created := responseData[wizardSubmitResult](t, httpCall(s, "POST", "/console/v1/wizard/tasks", wizardBody(iso, tpl, `[{"sn":"SN-1"}]`), token))
	setMedia(s, st, st, &mockISOSource{calls: map[string][]sourceCall{}}, &mockISODeletion{st: st})
	check := func(path, kind, id string) {
		t.Helper()
		w := httpCall(s, "DELETE", path, "", token)
		if w.Code != 409 {
			t.Fatal(path, w.Code, w.Body.String())
		}
		var result struct {
			Data struct {
				References []struct {
					Kind string `json:"kind"`
					ID   string `json:"id"`
				} `json:"references"`
			} `json:"data"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil || len(result.Data.References) == 0 {
			t.Fatal("missing reference details", w.Body.String(), err)
		}
		if result.Data.References[0].Kind != kind || result.Data.References[0].ID != id {
			t.Fatal(result)
		}
	}
	check("/console/v1/hosts/SN-1?rev=1", "task", created.Tasks[0].ID)
	check(fmt.Sprintf("/console/v1/isos/%s?rev=%d", iso.ID, iso.Rev), "task", created.Tasks[0].ID)
	hook := responseData[hookResponse](t, httpCall(s, "POST", "/console/v1/hooks", `{"name":"referenced-draft"}`, token))
	dependent := responseData[templateResponse](t, httpCall(s, "POST", "/console/v1/templates", fmt.Sprintf(`{"name":"dependent-draft","assembly":{"setup":[{"hookID":%q,"hookVersion":1}]}}`, hook.ID), token))
	check(fmt.Sprintf("/console/v1/hooks/%s/versions/1?rev=1", hook.ID), "template", dependent.ID+"@1")
}
