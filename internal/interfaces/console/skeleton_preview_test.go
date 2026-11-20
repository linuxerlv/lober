package console

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
)

func Test从骨架新建草稿并预览软件包(t *testing.T) {
	s, store, _ := newHTTPServer(t)
	token := adminHTTP(t, s, store)
	seedBuiltinContent(t, store)
	const templates = "/console/v1/templates"
	list := responseData[templatePage](t, httpCall(s, "GET", templates+"?prefix=lober:kickstart&approved=true", "", token))
	if len(list.Items) != 1 || !list.Items[0].Builtin {
		t.Fatal("缺少已批准的 Kickstart 骨架")
	}
	source := list.Items[0]
	content := templateRequest{
		Name: "Rocky-Base", Family: source.Family, Series: []catalog.Series{"rocky"},
		Body: source.Body, Params: source.Params, Bindings: source.Bindings,
	}
	encode := func(v any) string {
		b, err := json.Marshal(v)
		if err != nil {
			t.Fatal(err)
		}
		return string(b)
	}
	draft := responseData[templateResponse](t, httpCall(s, "POST", templates, encode(content), token))
	if len(draft.Assembly) != 0 {
		t.Fatal("新草稿应由用户选择 Hook")
	}
	version := fmt.Sprintf("%s/%s/versions/%d", templates, draft.ID, draft.Version)
	candidates := responseData[hookCandidatePage](t, httpCall(s, "GET", version+"/hook-candidates?slot=packages&index=1&prefix=lober:packages", "", token))
	if len(candidates.Items) != 1 || !candidates.Items[0].Usable {
		t.Fatal("无法添加内置软件包 Hook")
	}
	hook := candidates.Items[0].Hook
	content.Assembly = map[string][]instanceDTO{"packages": {{
		HookID: hook.ID, HookVersion: hook.Version,
		Bindings: map[string]bindingDTO{"packages": {Source: "fixed", Value: "tmux", Editable: true}},
	}}}
	responseData[templateResponse](t, httpCall(s, "PUT", version, encode(templateDraftRequest{content, draft.Rev}), token))
	preview := responseData[previewResponse](t, httpCall(s, "POST", version+"/preview", "{}", token))
	for _, file := range preview.Files {
		if file.Name == "ks.cfg" {
			_, packages, found := strings.Cut(file.Body, "%packages\n")
			packages, _, closed := strings.Cut(packages, "%end")
			if found && closed && strings.Contains("\n"+packages, "\ntmux\n") {
				return
			}
		}
	}
	t.Fatalf("生成的 ks.cfg 软件包段缺少 tmux，预览诊断为 %v", preview.Problems)
}
