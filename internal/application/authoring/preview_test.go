package authoring

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/lober/lober/internal/application/ports"
	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/params"
	"github.com/lober/lober/internal/domain/repository"
)

type previewReader struct {
	*editorReader
	host model.Host
	iso  model.ISO
}

func (r *previewReader) GetHost(ctx context.Context, sn string) (model.Host, error) {
	if ctx.Err() != nil {
		return model.Host{}, ctx.Err()
	}
	if sn != r.host.SN {
		return model.Host{}, repository.ErrNotFound
	}
	return r.host, nil
}
func (r *previewReader) GetISO(ctx context.Context, id string) (model.ISO, error) {
	if ctx.Err() != nil {
		return model.ISO{}, ctx.Err()
	}
	if id != r.iso.ID {
		return model.ISO{}, repository.ErrNotFound
	}
	return r.iso, nil
}
func previewEditorFixture() (*previewReader, model.TemplateKey, Editor) {
	r, k := editorFixture()
	r.templates[k] = model.Template{ID: k.ID, Version: k.Version, Family: "linux", Series: []catalog.Series{"rocky"}, Body: "# {{param:message}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n", Params: []model.ParamDecl{{Name: "message", Type: "word", Default: "hello"}}}
	p := &previewReader{editorReader: r, host: model.Host{SN: "ACTUAL", Arch: "x86_64"}, iso: model.ISO{ID: "actual", Series: "rocky"}}
	return p, k, NewEditor(EditorDeps{Templates: p, Hooks: p, Hosts: p, ISOs: p, Renderer: NewPreviewRenderer(nil, ports.PreviewEndpoints{})})
}
func Test参数预览问题仍以成功结果返回(t *testing.T) {
	_, k, s := previewEditorFixture()
	got, err := s.PreviewParams(t.Context(), editorActor(), PreviewRequest{Key: k, Entered: map[params.FieldKey]string{"unknown": "x"}})
	if err != nil || len(got.Fields) != 1 || len(got.Problems) != 1 || got.Problems[0].Code != "param_unknown_field" || !reflect.DeepEqual(got.Sample, []string{"ISO", "主机", "firmware"}) {
		t.Fatal(got, err)
	}
}
func Test文件预览失败丢弃全部文件(t *testing.T) {
	_, k, s := previewEditorFixture()
	for _, r := range []PreviewRequest{{Key: k, Entered: map[params.FieldKey]string{"unknown": "x"}}, {Key: k, Series: "debian"}} {
		got, err := s.Preview(t.Context(), editorActor(), r)
		var p *ProblemsError
		if !errors.As(err, &p) || len(p.Problems) == 0 || got.Files != nil {
			t.Fatal(got, err)
		}
	}
}
func Test预览显式对象不回退且缺省次序不变(t *testing.T) {
	p, k, s := previewEditorFixture()
	for _, r := range []PreviewRequest{{Key: k, ISOID: "missing"}, {Key: k, HostSN: "missing"}} {
		if _, err := s.Preview(t.Context(), editorActor(), r); !errors.Is(err, repository.ErrNotFound) {
			t.Fatal(r, err)
		}
	}
	if _, err := s.Preview(t.Context(), editorActor(), PreviewRequest{Key: k, ISOID: "actual", Series: "debian"}); !errors.Is(err, repository.ErrInvalid) {
		t.Fatal(err)
	}
	got, err := s.Preview(t.Context(), editorActor(), PreviewRequest{Key: k})
	if err != nil || !reflect.DeepEqual(got.Sample, []string{"ISO", "主机", "firmware", "token", "finalize-token", "task", "gateway", "artifacts", "smb", "syslog-addr"}) || len(got.Files) != 2 || got.Files[0].Kind != "menu" || got.Files[1].Name != "ks.cfg" || !got.Files[0].Sample {
		t.Fatal(got, err)
	}
	tpl := p.templates[k]
	tpl.Family = "esxi"
	tpl.Series = []catalog.Series{"esxi"}
	p.templates[k] = tpl
	_, err = s.PreviewParams(t.Context(), editorActor(), PreviewRequest{Key: k})
	var ps *ProblemsError
	if !errors.As(err, &ps) || ps.Problems[0].Code != "iso_version_missing" {
		t.Fatal(err)
	}
}
func Test预览入口权限先于读取且传递取消(t *testing.T) {
	p, k, s := previewEditorFixture()
	for _, a := range []ports.Actor{{}, {Username: "x", Role: model.RoleAdmin, Disabled: true}, {Username: "x", Role: model.RoleAdmin, MustChangePassword: true}} {
		_, e1 := s.PreviewParams(t.Context(), a, PreviewRequest{Key: k})
		_, e2 := s.Preview(t.Context(), a, PreviewRequest{Key: k})
		want := ports.ErrUnauthenticated
		if a.MustChangePassword {
			want = ports.ErrPasswordChangeRequired
		}
		if !errors.Is(e1, want) || !errors.Is(e2, want) {
			t.Fatal(e1, e2)
		}
	}
	if p.reads != 0 {
		t.Fatal("未授权却读取")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Preview(ctx, editorActor(), PreviewRequest{Key: k}); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func Test文件预览原配置读取失败或取消时丢弃文件(t *testing.T) {
	p, k, _ := previewEditorFixture()
	tpl := p.templates[k]
	tpl.Family = "esxi"
	tpl.Series = []catalog.Series{"esxi"}
	tpl.Params = nil
	tpl.Body = "{{lober:firstboot}}\n{{lober:cleanup}}\n"
	p.templates[k] = tpl
	p.iso = model.ISO{ID: "actual", Family: "esxi", Series: "esxi", Version: "8.0", Arch: "x86_64", State: model.ISOReady, Artifacts: map[string]string{"mboot": "esxi/mboot.efi", "esxi-tree": "esxi", "repo": "esxi"}}
	for _, fail := range []error{errors.New("原配置读取失败"), context.Canceled} {
		s := NewEditor(EditorDeps{Templates: p, Hooks: p, Hosts: p, ISOs: p, Renderer: NewPreviewRenderer(failedBootConfig{fail}, ports.PreviewEndpoints{})})
		got, err := s.Preview(t.Context(), editorActor(), PreviewRequest{Key: k, ISOID: "actual"})
		if !errors.Is(err, fail) || got.Files != nil {
			t.Fatal(got, err)
		}
	}
}
