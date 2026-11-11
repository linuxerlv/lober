package console

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
)

func TestPreviewParamsEvaluatesDraftWithRealHostISOAndLockedLists(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, h := previewFixture(t, st)
	tpl.Params = []model.ParamDecl{{Name: "disks", Label: "目标盘", Type: "diskpath", List: true, Required: true}, {Name: "edition", Type: "enum", Offer: "editions", Required: true}, {Name: "missing", Type: "word", Required: true}}
	tpl.Bindings = map[string]model.Binding{"disks": {Source: model.BindingAttr, Attr: "disks", Editable: false}}
	var err error
	tpl, err = st.SaveTemplateDraft(t.Context(), tpl)
	if err != nil {
		t.Fatal(err)
	}
	item := readyISO(t, st, "ParamISO", "rocky")
	item.Offers = map[string][]model.Option{"editions": {{Value: "server", Label: "服务器"}, {Value: "minimal", Label: "最小"}}}
	item, err = st.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	host, err := st.CreateHost(t.Context(), model.Host{SN: "PARAM-SN", Arch: "x86_64", Firmware: "uefi", Attrs: map[string]string{"disks": "/dev/sda\n/dev/sdb"}}, repository.Audit{Actor: "seed", Action: "host.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	request := fmt.Sprintf(`{"hostSN":%q,"isoID":%q,"firmware":"uefi","entered":{"tpl/edition":"server"}}`, host.SN, item.ID)
	path := fmt.Sprintf("/console/v1/templates/%s/versions/1/preview-params", tpl.ID)
	result := responseData[struct {
		Fields   []paramFieldDTO `json:"fields"`
		Problems []problemJSON   `json:"problems"`
		Sample   []string        `json:"sample"`
	}](t, httpCall(s, "POST", path, request, token))
	expected := params.Evaluate(params.Input{Template: tpl, Hooks: map[model.HookKey]model.Hook{h.Key(): h}, ISO: item, Host: host, Entered: map[params.FieldKey]string{"tpl/edition": "server"}})
	if !reflect.DeepEqual(result.Fields, paramFields(expected.Fields)) || !reflect.DeepEqual(result.Problems, jsonProblems(expected.Problems)) {
		t.Fatal(result, expected)
	}
	if len(result.Fields) != 4 || !result.Fields[0].Locked || !result.Fields[0].List || result.Fields[0].Prefill != "/dev/sda\n/dev/sdb" || len(result.Fields[1].Options) != 2 || len(result.Problems) != 1 || result.Problems[0].Subject.Path != "tpl/missing" {
		t.Fatal(result)
	}
	if len(result.Sample) != 0 {
		t.Fatal(result.Sample)
	}
}

func TestPreviewParamsSharesSampleAndRealFactValidation(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	tpl, _ := previewFixture(t, st)
	path := fmt.Sprintf("/console/v1/templates/%s/versions/1/preview-params", tpl.ID)
	result := responseData[struct {
		Fields   []paramFieldDTO `json:"fields"`
		Problems []problemJSON   `json:"problems"`
		Sample   []string        `json:"sample"`
	}](t, httpCall(s, "POST", path, `{}`, token))
	if len(result.Fields) != 2 || len(result.Problems) != 0 || !slices.Contains(result.Sample, "ISO") || !slices.Contains(result.Sample, "主机") || !slices.Contains(result.Sample, "firmware") {
		t.Fatal(result)
	}
	for _, test := range []struct {
		body   string
		status int
	}{{`{"hostSN":"MISSING"}`, 404}, {`{"isoID":"missing"}`, 404}, {`{"firmware":"wrong"}`, 400}, {`{"series":"wrong"}`, 400}} {
		w := httpCall(s, "POST", path, test.body, token)
		if w.Code != test.status {
			t.Fatal(test, w.Code, w.Body.String())
		}
	}
	// A chosen real ISO's series remains authoritative, even for an incomplete draft.
	item := readyISO(t, st, "OtherISO", catalog.SeriesUbuntu)
	other := responseData[struct {
		Fields   []paramFieldDTO `json:"fields"`
		Problems []problemJSON   `json:"problems"`
	}](t, httpCall(s, "POST", path, fmt.Sprintf(`{"isoID":%q}`, item.ID), token))
	if len(other.Fields) != 0 || len(other.Problems) != 1 || other.Problems[0].Code != "series_not_ticked" {
		t.Fatal(other)
	}
}
