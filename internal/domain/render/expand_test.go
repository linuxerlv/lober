package render

import (
	"bytes"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
)

func testRequest(body string) Request {
	return Request{Kind: catalog.FileAnswer, Snapshot: model.Snapshot{
		Schema: model.SnapshotSchema, Family: "linux", Series: "rhel", Format: "kickstart",
		Template: model.SnapshotTemplate{Body: body, Params: map[string]model.SnapshotValue{
			"host": {Type: "hostname", Items: []string{"node.example"}},
			"pkgs": {Type: "word", List: true, Items: []string{"curl", "vim"}},
			"addr": {Type: "cidr", List: true, Items: []string{"10.0.0.2/24", "192.168.1.3/16"}},
		}},
		ISO: model.SnapshotISO{Artifacts: map[string]string{"repo": "rhel/tree/", "iso": "ubuntu/image.iso"}},
	}, Values: map[catalog.SystemValue]string{"task": "task-1", "artifacts": "https://assets.example/"}}
}

func expandRequest(r Request, slots map[string]string, parts map[catalog.SystemPart]string) ([]byte, []model.Problem) {
	e := expansion{request: r, slots: slots, parts: parts}
	return e.expand(r.Snapshot.Template.Body, r.Snapshot.Template.Params, model.Subject{Kind: "template", ID: r.Snapshot.Template.ID})
}

func TestExpandValuesAndPreserveRaw(t *testing.T) {
	body := "source={{sys:repo}} iso={{sys:iso}} task={{sys:task}} host={{param:host}}\n{{sys:lbrace}}x}} {{ ds.meta_data.local_hostname }} {{lober:TYPO}} {{param:bad|typo}}\r\n"
	r := testRequest(body)
	want := "source=https://assets.example/rhel/tree iso=https://assets.example/ubuntu/image.iso task=task-1 host=node.example\n{{x}} {{ ds.meta_data.local_hostname }} {{lober:TYPO}} {{param:bad|typo}}\r\n"
	got, problems := expandRequest(r, nil, nil)
	if len(problems) != 0 || string(got) != want {
		t.Fatalf("got %q, problems %v; want %q", got, problems, want)
	}
}

func TestExpandListLines(t *testing.T) {
	cases := []struct{ name, body, want string }{
		{"default", "before\n- {{param:pkgs}}\nafter", "before\n- curl\n- vim\nafter"},
		{"unterminated", "{{param:pkgs}}", "curl\nvim"},
		{"repeat-once", "{{param:pkgs}}={{param:pkgs}}\n", "curl=curl\nvim=vim\n"},
		{"join", "all={{param:pkgs|join}}\n", "all=curl vim\n"},
		{"join-sub", "all={{param:addr.addr|join}}\n", "all=10.0.0.2 192.168.1.3\n"},
		{"several-subs", "{{param:addr.addr}} mask={{param:addr.netmask}} raw={{param:addr}}\n", "10.0.0.2 mask=255.255.255.0 raw=10.0.0.2/24\n192.168.1.3 mask=255.255.0.0 raw=192.168.1.3/16\n"},
		{"join-and-repeat", "{{param:pkgs}} all={{param:pkgs|join}}\n", "curl all=curl vim\nvim all=curl vim\n"},
		{"crlf", "a\r\n\t{{param:pkgs}}\r\nz\r\n", "a\r\n\tcurl\r\n\tvim\r\nz\r\n"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, problems := expandRequest(testRequest(tc.body), nil, nil)
			if len(problems) != 0 || string(got) != tc.want {
				t.Fatalf("got %q, problems %v; want %q", got, problems, tc.want)
			}
		})
	}
}

func TestExpandFragmentIndentAndLocalParams(t *testing.T) {
	r := testRequest("before\n \t{{slot:pieces}}  \r\nafter\n")
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces", Instances: []model.SnapshotInstance{
		{Kind: model.ImplFragment, Body: "host={{param:host}}\n\t{{param:pkgs}}\n", Params: map[string]model.SnapshotValue{
			"host": {Type: "hostname", Items: []string{"hook.example"}},
			"pkgs": {Type: "word", List: true, Items: []string{"a", "b"}},
		}},
		{Kind: model.ImplFragment, Body: "last"},
	}}}
	got, problems := expandRequest(r, nil, nil)
	want := "before\n \thost=hook.example\n \t\ta\n \t\tb\n \tlast\r\nafter\n"
	if len(problems) != 0 || string(got) != want {
		t.Fatalf("got %q, problems %v; want %q", got, problems, want)
	}
	r.Snapshot.Slots[0].Instances = nil
	got, problems = expandRequest(r, nil, nil)
	if len(problems) != 0 || string(got) != "before\nafter\n" {
		t.Fatalf("empty fragment got %q, %v", got, problems)
	}
}

func TestExpandOneLineSlotAndPart(t *testing.T) {
	r := testRequest("run={{slot:setup@pre}} ; {{lober:cleanup}}\n")
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "setup", Exec: true, Timing: "pre"}}
	got, problems := expandRequest(r, map[string]string{"setup": "sh /run/lober-1"}, map[catalog.SystemPart]string{"cleanup": "rm -f /run/token"})
	if len(problems) != 0 || string(got) != "run=sh /run/lober-1 ; rm -f /run/token\n" {
		t.Fatalf("got %q, %v", got, problems)
	}
	for _, bad := range []string{"x\ny", "x\ry", "\"x\"", "'x'", "x<y", "x>y", "x&y", "x\\y", "x#y", "x: y"} {
		t.Run(bad, func(t *testing.T) {
			for _, body := range []string{"{{slot:setup@pre}}", "{{lober:cleanup}}"} {
				r.Snapshot.Template.Body = body
				got, problems := expandRequest(r, map[string]string{"setup": bad}, map[catalog.SystemPart]string{"cleanup": bad})
				if got != nil || len(problems) != 1 || problems[0].Code != "render_line_invalid" {
					t.Fatalf("unsafe %q: got %q, %v", bad, got, problems)
				}
			}
		})
	}
}

func TestExpandProblemsDiscardPartialOutput(t *testing.T) {
	cases := []struct {
		name, body string
		change     func(*Request)
		code       model.Code
	}{
		{"missing-system", "ok {{sys:gateway}}", nil, "render_system_value_missing"},
		{"missing-base", "{{sys:repo}}", func(r *Request) { delete(r.Values, "artifacts") }, "render_system_value_missing"},
		{"missing-role", "{{sys:iso}}", func(r *Request) { delete(r.Snapshot.ISO.Artifacts, "iso") }, "render_artifact_missing"},
		{"caller-repo", "{{sys:repo}}", func(r *Request) { r.Values["repo"] = "https://other/" }, "render_system_value_reserved"},
		{"missing-param", "{{param:missing}}", nil, "param_required"},
		{"missing-sub", "{{param:host.ip}}", nil, "param_sub_unavailable"},
		{"inline-fragment", "before {{slot:pieces}}", func(r *Request) { r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces"}} }, "fragment_slot_not_alone"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := testRequest(tc.body)
			if tc.change != nil {
				tc.change(&r)
			}
			got, problems := expandRequest(r, nil, nil)
			if got != nil || len(problems) != 1 || problems[0].Code != tc.code {
				t.Fatalf("got %q, %v; want %s", got, problems, tc.code)
			}
		})
	}
}

func TestRenderDispatchAndPreconditions(t *testing.T) {
	for _, kind := range []catalog.FileKind{catalog.FileMenu, catalog.FileAnswer, catalog.FileAux, catalog.FileScript} {
		r := testRequest("plain")
		r.Kind, r.Slot = kind, "setup"
		r.Snapshot.Slots = []model.SnapshotSlot{{Name: "setup", Exec: true, Timing: "pre"}}
		got, problems, err := Render(r)
		if kind == catalog.FileAnswer {
			if string(got) != "plain" || len(problems) != 0 || err != nil {
				t.Fatalf("answer: got %q, %v, %v", got, problems, err)
			}
			continue
		}
		if kind == catalog.FileScript {
			if got != nil || err != nil || len(problems) != 2 {
				t.Fatalf("missing runtime credentials: %q %v %v", got, problems, err)
			}
			continue
		}
		if kind == catalog.FileMenu {
			if got != nil || err != nil || len(problems) != 1 || problems[0].Code != "render_firmware_invalid" {
				t.Fatalf("menu without actual firmware: %q, %v, %v", got, problems, err)
			}
			continue
		}
		if got != nil || len(problems) != 0 || !errors.Is(err, errUnsupportedFormat) {
			t.Fatalf("%s: got %q, %v, %v", kind, got, problems, err)
		}
	}
	r := testRequest("{{sys:gateway}}")
	got, problems, err := Render(r)
	if got != nil || err != nil || len(problems) != 1 || problems[0].Code != "render_system_value_missing" {
		t.Fatalf("missing value: %q, %v, %v", got, problems, err)
	}
	r = testRequest("plain")
	r.Kind = "unknown"
	if got, problems, err := Render(r); got != nil || len(problems) != 0 || err == nil {
		t.Fatalf("unknown kind: %q, %v, %v", got, problems, err)
	}
	for _, exec := range []bool{false, true} {
		r = testRequest("plain")
		r.Kind, r.Slot = catalog.FileScript, "missing"
		r.Snapshot.Slots = []model.SnapshotSlot{{Name: "fragment", Exec: exec}}
		got, problems, err := Render(r)
		if got != nil || err != nil || len(problems) != 1 || problems[0].Code != "render_slot_invalid" {
			t.Fatalf("unknown script slot: %q, %v, %v", got, problems, err)
		}
		r.Slot = "fragment"
		got, problems, err = Render(r)
		if !exec && (got != nil || err != nil || len(problems) != 1 || problems[0].Code != "render_slot_invalid") {
			t.Fatalf("fragment script slot: %q, %v, %v", got, problems, err)
		}
	}
}

func TestExpandDeterministicWithoutMutation(t *testing.T) {
	r := testRequest("{{param:addr.addr}} {{param:pkgs|join}} {{sys:repo}} {{sys:iso}}\n")
	want, problems := expandRequest(r, nil, nil)
	if len(problems) != 0 {
		t.Fatal(problems)
	}
	for i := 0; i < 30; i++ {
		got, p := expandRequest(r, nil, nil)
		if !bytes.Equal(got, want) || !reflect.DeepEqual(p, problems) {
			t.Fatalf("non-deterministic: %q, %v", got, p)
		}
	}
	if _, ok := r.Values["repo"]; ok {
		t.Fatal("mutated caller values")
	}
	if !reflect.DeepEqual(r.Snapshot.Template.Params["addr"].Items, []string{"10.0.0.2/24", "192.168.1.3/16"}) {
		t.Fatal("mutated snapshot")
	}
}

func TestExpandPreservesInsertedBraces(t *testing.T) {
	r := testRequest("{{sys:gateway}}\n  {{slot:pieces}}\n")
	r.Values["gateway"] = "{{param:host}}"
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces", Instances: []model.SnapshotInstance{
		{Kind: model.ImplFragment, Body: "{{sys:gateway}}\n{{ ds.meta_data.local_hostname }}"},
	}}}
	got, problems := expandRequest(r, nil, nil)
	want := "{{param:host}}\n  {{param:host}}\n  {{ ds.meta_data.local_hostname }}\n"
	if len(problems) != 0 || string(got) != want {
		t.Fatalf("inserted braces got %q, %v; want %q", got, problems, want)
	}
}

func TestExpandMapOrderAndProblemPositions(t *testing.T) {
	r := testRequest("ok\n中文 {{sys:gateway}}\r\n{{param:missing}} {{sys:sn}}")
	r.Snapshot.Template.ID = "template-1"
	got, problems := expandRequest(r, nil, nil)
	if got != nil || len(problems) != 3 {
		t.Fatalf("got %q, %v", got, problems)
	}
	wantSubjects := []model.Subject{
		{Kind: "template", ID: "template-1", Line: 2, Col: 4},
		{Kind: "template", ID: "template-1", Line: 3, Col: 1},
		{Kind: "template", ID: "template-1", Line: 3, Col: 19},
	}
	for i, problem := range problems {
		if problem.Subject != wantSubjects[i] {
			t.Fatalf("problem %d location: %+v, want %+v", i, problem.Subject, wantSubjects[i])
		}
	}
	r = testRequest("{{param:host}} {{param:pkgs|join}} {{sys:repo}} {{sys:iso}}")
	a, pa := expandRequest(r, nil, nil)
	r.Snapshot.Template.Params = map[string]model.SnapshotValue{
		"pkgs": r.Snapshot.Template.Params["pkgs"], "host": r.Snapshot.Template.Params["host"],
	}
	r.Snapshot.ISO.Artifacts = map[string]string{"iso": "ubuntu/image.iso", "repo": "rhel/tree/"}
	b, pb := expandRequest(r, nil, nil)
	if !bytes.Equal(a, b) || !reflect.DeepEqual(pa, pb) {
		t.Fatalf("map ordering changed output: %q / %q", a, b)
	}
	r.Values["iso"], r.Values["repo"] = "ignored", "ignored"
	got, problems = expandRequest(r, nil, nil)
	if got != nil || len(problems) != 2 || problems[0].Code != "render_system_value_reserved" || problems[1].Code != "render_system_value_reserved" {
		t.Fatalf("reserved values: %q, %v", got, problems)
	}
	if problems[0].Message != model.NewProblem("render_system_value_reserved", model.Subject{}, "repo").Message || problems[1].Message != model.NewProblem("render_system_value_reserved", model.Subject{}, "iso").Message {
		t.Fatalf("reserved value problem order: %v", problems)
	}
}

func TestRenderPreflightFragmentAndSkipScriptBody(t *testing.T) {
	r := testRequest("{{slot:pieces}}")
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces", Instances: []model.SnapshotInstance{
		{Kind: model.ImplFragment, Body: "{{sys:gateway}}"},
	}}, {Name: "setup", Exec: true, Timing: "pre", Instances: []model.SnapshotInstance{
		{Kind: model.ImplScript, Body: "{{sys:sn}}", Digest: "digest"},
	}}}
	got, problems, err := Render(r)
	if got != nil || err != nil || len(problems) != 1 || problems[0].Code != "render_system_value_missing" || problems[0].Subject.Path != "pieces/1" {
		t.Fatalf("fragment preflight: %q, %v, %v", got, problems, err)
	}
	r.Kind, r.Slot = catalog.FileScript, "setup"
	r.Values["gateway"], r.Values["token"] = "https://gateway", "boot-token"
	got, problems, err = Render(r)
	if len(got) == 0 || len(problems) != 0 || err != nil {
		t.Fatalf("script body must not be expanded: %q, %v, %v", got, problems, err)
	}
}

func TestRenderProblemsIdentifyTemplateVersion(t *testing.T) {
	r := testRequest("{{sys:gateway}}")
	r.Snapshot.Template.ID, r.Snapshot.Template.Version = "template-1", 3
	_, problems, _ := Render(r)
	if len(problems) != 1 || problems[0].Subject.ID != "template-1@3" {
		t.Fatalf("template problem identity: %v", problems)
	}
	r.Kind, r.Slot = catalog.FileScript, "missing"
	_, problems, _ = Render(r)
	if len(problems) != 1 || problems[0].Subject.ID != "template-1@3" {
		t.Fatalf("slot problem identity: %v", problems)
	}
	r.Kind = catalog.FileAnswer
	r.Snapshot.Template.Body = "{{slot:pieces}}"
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces", Instances: []model.SnapshotInstance{
		{Kind: model.ImplFragment, Body: "{{sys:gateway}}"},
	}}}
	_, problems, _ = Render(r)
	if len(problems) != 1 || problems[0].Subject.ID != "template-1@3" {
		t.Fatalf("instance problem identity: %v", problems)
	}
}

func TestExpandRejectSelfReferencingFragment(t *testing.T) {
	r := testRequest("{{slot:pieces}}")
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces", Instances: []model.SnapshotInstance{
		{Kind: model.ImplFragment, Body: "{{slot:pieces}}"},
	}}}
	type result struct {
		bytes    []byte
		problems []model.Problem
	}
	finished := make(chan result, 1)
	go func() { bytes, problems := expandRequest(r, nil, nil); finished <- result{bytes, problems} }()
	select {
	case got := <-finished:
		if got.bytes != nil || len(got.problems) != 1 || got.problems[0].Code != "fragment_forbidden_placeholder" || got.problems[0].Subject.Path != "pieces/1" {
			t.Fatalf("self-referencing fragment: %q, %v", got.bytes, got.problems)
		}
	case <-time.After(time.Second):
		t.Fatal("self-referencing fragment expansion did not terminate")
	}
}

func TestExpandRejectPartInFragment(t *testing.T) {
	r := testRequest("{{slot:pieces}}")
	r.Snapshot.Slots = []model.SnapshotSlot{{Name: "pieces", Instances: []model.SnapshotInstance{
		{Kind: model.ImplFragment, Body: "ok\n{{lober:cleanup}}"},
	}}}
	got, problems := expandRequest(r, nil, map[catalog.SystemPart]string{"cleanup": "rm -f /run/token"})
	if got != nil || len(problems) != 1 || problems[0].Code != "fragment_forbidden_placeholder" || problems[0].Subject.Path != "pieces/1" || problems[0].Subject.Line != 2 {
		t.Fatalf("system part in fragment: %q, %v", got, problems)
	}
}
