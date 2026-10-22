package console

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/lober/lober/internal/application/authoring"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"github.com/lober/lober/internal/infrastructure/identity"
	"testing"
)

func TestAuthoringCandidatesUseCoreAndIgnoreUnrelatedDraftErrors(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	good := approvedTestHook(t, st, "candidate-good")
	bad, err := st.CreateHook(t.Context(), model.Hook{Name: "candidate-bad", Author: "alice", Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"ubuntu"}, Kind: model.ImplFragment, Body: "# test"}}})
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := st.CreateTemplate(t.Context(), model.Template{Name: "candidate-template", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{slot:disk}}\n{{param:undeclared}}", Assembly: map[string][]model.Instance{"disk": {{HookID: good.ID, HookVersion: 1}}}})
	if err != nil {
		t.Fatal(err)
	}
	page := responseData[struct {
		Items []struct {
			Hook     hookResponse  `json:"hook"`
			Usable   bool          `json:"usable"`
			Problems []problemJSON `json:"problems"`
		} `json:"items"`
		Next string `json:"next"`
	}](t, httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=disk&index=1", tpl.ID), "", token))
	seenGood, seenBad := false, false
	for _, candidate := range page.Items {
		if candidate.Hook.ID == good.ID {
			seenGood = true
			if !candidate.Usable || len(candidate.Problems) != 0 {
				t.Fatal(candidate)
			}
		}
		if candidate.Hook.ID == bad.ID {
			seenBad = true
			if candidate.Usable {
				t.Fatal(candidate)
			}
			found := false
			for _, p := range candidate.Problems {
				found = found || p.Code == "impl_missing_for_series"
				if p.Subject.Path != "disk/1" {
					t.Fatal(p)
				}
			}
			if !found {
				t.Fatal(candidate)
			}
		}
	}
	if !seenGood || !seenBad {
		t.Fatal(page)
	}
	if w := httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=missing", tpl.ID), "", token); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
}

func TestAuthoringSourceComparisonDoesNotModifyDerivedTemplate(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	source, err := st.CreateTemplate(t.Context(), model.Template{Name: "source", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "# original\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n"})
	if err != nil {
		t.Fatal(err)
	}
	derived, err := st.DeriveTemplate(t.Context(), model.TemplateKey{ID: source.ID, Version: source.Version}, "derived", "alice")
	if err != nil {
		t.Fatal(err)
	}
	if err = st.SubmitTemplate(t.Context(), model.TemplateKey{ID: source.ID, Version: 1}, source.Rev, repository.Audit{Actor: "alice", Action: "template.submit", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	if err = st.ApproveTemplate(t.Context(), model.TemplateKey{ID: source.ID, Version: 1}, source.Rev+1, "reviewer", repository.Audit{Actor: "reviewer", Action: "template.approve", At: fixedAt}); err != nil {
		t.Fatal(err)
	}
	newer, err := st.NewTemplateVersion(t.Context(), source.ID, 1, "alice")
	if err != nil {
		t.Fatal(err)
	}
	newer.Body = "updated"
	if _, err = st.SaveTemplateDraft(t.Context(), newer); err != nil {
		t.Fatal(err)
	}
	diff := responseData[struct {
		Original templateResponse `json:"original"`
		Current  templateResponse `json:"current"`
		Changed  bool             `json:"changed"`
	}](t, httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/source-diff", derived.ID), "", token))
	if !diff.Changed || diff.Original.Body != source.Body || diff.Current.Body != "updated" {
		t.Fatal(diff)
	}
	unchanged, err := st.GetTemplate(t.Context(), model.TemplateKey{ID: derived.ID, Version: derived.Version})
	if err != nil || unchanged.Body != source.Body {
		t.Fatal(unchanged, err)
	}
}

// This reader exercises the real handler and core validator without coupling the
// ordinal/cursor regression to PostgreSQL availability. Integration cases above
// cover the real persistence/authentication boundaries.
type authoringReadFixture struct {
	repository.Hooks
	repository.Templates
	template model.Template
	hooks    []model.Hook
}

func (f *authoringReadFixture) GetTemplate(context.Context, model.TemplateKey) (model.Template, error) {
	return f.template, nil
}
func (f *authoringReadFixture) ListHooks(_ context.Context, q repository.HookQuery) (repository.Page[model.Hook], error) {
	if q.Cursor == "next" {
		return repository.Page[model.Hook]{Items: []model.Hook{f.hooks[2]}}, nil
	}
	return repository.Page[model.Hook]{Items: []model.Hook{f.hooks[1]}, Next: "next"}, nil
}
func (f *authoringReadFixture) ListHookVersions(_ context.Context, id string) ([]model.Hook, error) {
	var versions []model.Hook
	for _, hook := range f.hooks {
		if hook.ID == id {
			versions = append(versions, hook)
		}
	}
	return versions, nil
}
func (f *authoringReadFixture) GetHooks(_ context.Context, keys []model.HookKey) (map[model.HookKey]model.Hook, error) {
	result := map[model.HookKey]model.Hook{}
	for _, key := range keys {
		for _, hook := range f.hooks {
			if hook.Key() == key {
				result[key] = hook
			}
		}
	}
	return result, nil
}

func TestAuthoringCandidateVersionsCursorAndReplacementOrdinal(t *testing.T) {
	first := model.Hook{ID: identity.NewID(), Name: "a-candidate", Version: 1, State: model.VersionApproved, Impls: []model.Impl{{Family: "linux", Series: []catalog.Series{"rocky"}, Kind: model.ImplFragment, Body: "# test"}}}
	newer := first
	newer.Version = 2
	newer.State = model.VersionDraft
	newer.Impls = []model.Impl{{Family: "linux", Series: []catalog.Series{"ubuntu"}, Kind: model.ImplFragment, Body: "# test"}}
	second := first
	second.ID = identity.NewID()
	second.Name = "b-candidate"
	tpl := model.Template{ID: identity.NewID(), Version: 1, Name: "ordinal-template", Author: "alice", Family: "linux", Series: []catalog.Series{"rocky"}, Body: "{{slot:disk}}\n", Assembly: map[string][]model.Instance{"disk": {{HookID: "missing", HookVersion: 1}, {HookID: second.ID, HookVersion: 1}}}}
	fixture := &authoringReadFixture{template: tpl, hooks: []model.Hook{first, newer, second}}
	srv := &server{deps: Deps{AuthoringEditor: authoring.NewEditor(authoring.EditorDeps{Templates: fixture, Hooks: fixture})}}
	engine := gin.New()
	engine.GET("/console/v1/templates/:id/versions/:version/hook-candidates", func(c *gin.Context) { c.Set(ctxUser, &model.User{Username: "reader", Role: model.RoleAdmin}); c.Next() }, srv.hookCandidates)
	s := &Server{Engine: engine}
	base := fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=disk&index=2&limit=1", tpl.ID)
	page := responseData[hookCandidatePage](t, httpCall(s, "GET", base, "", ""))
	if len(page.Items) != 2 || page.Next == "" {
		t.Fatal("all versions of first identity and next cursor required", page)
	}
	sawOld, sawNew := false, false
	for _, candidate := range page.Items {
		if candidate.Hook.ID != first.ID {
			t.Fatal(candidate)
		}
		if candidate.Hook.Version == 1 {
			sawOld = true
			if !candidate.Usable || len(candidate.Problems) != 0 {
				t.Fatal(candidate)
			}
		}
		if candidate.Hook.Version == 2 {
			sawNew = true
			if candidate.Usable {
				t.Fatal(candidate)
			}
			for _, problem := range candidate.Problems {
				if problem.Subject.Path != "disk/2" {
					t.Fatal(problem)
				}
			}
		}
	}
	if !sawOld || !sawNew {
		t.Fatal(page)
	}
	after := responseData[hookCandidatePage](t, httpCall(s, "GET", base+"&cursor="+page.Next, "", ""))
	if len(after.Items) != 1 || after.Items[0].Hook.ID != second.ID || after.Next != "" {
		t.Fatal(after)
	}
	for _, index := range []string{"0", "-1", "4", "bad", "999999999999999999999999999999999"} {
		w := httpCall(s, "GET", fmt.Sprintf("/console/v1/templates/%s/versions/1/hook-candidates?slot=disk&index=%s", tpl.ID, index), "", "")
		if w.Code != 400 {
			t.Fatal(index, w.Code)
		}
	}
	if fixture.template.Assembly["disk"][1].HookID != second.ID {
		t.Fatal("candidate read changed the stored assembly")
	}
}
