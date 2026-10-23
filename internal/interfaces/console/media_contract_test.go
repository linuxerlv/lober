package console

import (
	"encoding/json"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
)

func mediaErrorEnvelope(t *testing.T, w *httptest.ResponseRecorder, status int, message string) {
	t.Helper()
	var got envelope
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if w.Code != status || got.Code != status || got.Message != message || got.Data != nil {
		t.Fatalf("状态=%d，内容=%s；期望=%d %s", w.Code, w.Body.String(), status, message)
	}
}

func Test镜像十一入口描述及审计契约(t *testing.T) {
	s := &server{}
	want := map[string][2]string{
		"GET /console/v1/isos":                {PermISORead, ""},
		"GET /console/v1/isos/:id":            {PermISORead, ""},
		"POST /console/v1/isos":               {PermISOWrite, "iso.create"},
		"POST /console/v1/isos/:id/sources":   {PermISOWrite, "iso.source.register"},
		"GET /console/v1/isos/:id/progress":   {PermISORead, ""},
		"PUT /console/v1/isos/:id/content":    {PermISOWrite, ""},
		"POST /console/v1/isos/:id/cancel":    {PermISOWrite, "iso.cancel"},
		"POST /console/v1/isos/:id/retry":     {PermISOWrite, "iso.retry"},
		"POST /console/v1/isos/:id/reimport":  {PermISOWrite, "iso.reimport"},
		"POST /console/v1/isos/:id/deprecate": {PermISOWrite, "iso.deprecate"},
		"DELETE /console/v1/isos/:id":         {PermISOWrite, "iso.delete"},
	}
	got := map[string][2]string{}
	for _, rt := range s.isoRoutes() {
		key := rt.method + " " + rt.path
		if _, exists := got[key]; exists {
			t.Fatal("重复入口", key)
		}
		got[key] = [2]string{rt.perm, rt.audit}
		if key == "PUT /console/v1/isos/:id/content" && (rt.requestMedia != "application/octet-stream" || reflect.TypeOf(rt.headers) != reflect.TypeOf(chunkHeaders{})) {
			t.Fatal("上传内容描述改变", rt)
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatal(got)
	}
}

func Test镜像多错仍按原先顺序返回(t *testing.T) {
	s, st, _, deletion, token := newISOServer(t)
	// 无效版本先返回，不能先进入删除流程并读出内部错误。
	deletion.err = repository.ErrNotFound
	mediaErrorEnvelope(t, httpCall(s, "DELETE", "/console/v1/isos/missing?rev=bad", "", token), 400, model.ConsoleBadRequest)
	mediaErrorEnvelope(t, httpCall(s, "DELETE", "/console/v1/isos/missing?rev=1", "", token), 404, model.ConsoleNotFound)
	// JSON 已能解析时，缺失镜像先于非法来源字段；坏 JSON 仍先被拒绝。
	mediaErrorEnvelope(t, httpCall(s, "POST", "/console/v1/isos/missing/sources", `{"kind":"wrong","role":"../escape","size":-1}`, token), 404, model.ConsoleNotFound)
	mediaErrorEnvelope(t, httpCall(s, "POST", "/console/v1/isos/missing/sources", `{`, token), 400, model.ConsoleBadRequest)
	item := readyISO(t, st, "upload-order", "rocky")
	_, err := st.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceRemote, URL: "https://example.test/image.iso", TotalSize: 4, State: model.SourcePending}, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	_, err = st.SaveSource(t.Context(), model.Source{ISOID: item.ID, Role: "upload", Kind: model.SourceUpload, TotalSize: 4, State: model.SourcePending}, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, id, role, header string
		status                 int
		message                string
	}{
		{"范围语法先于来源键", "missing", "../escape", "broken", 400, model.ConsoleContentRange},
		{"来源键先于存在性", "missing", "../escape", "bytes 0-8/*", 400, model.ConsoleBadRequest},
		{"存在性先于边界", "missing", "", "bytes 0-8/*", 404, model.ConsoleNotFound},
		{"来源种类先于边界", item.ID, "", "bytes 0-8/*", 409, model.ConsoleConflict},
		{"上传才检查边界", item.ID, "upload", "bytes 0-8/*", 400, model.ConsoleContentRange},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/console/v1/isos/"+tc.id+"/content?role="+tc.role, strings.NewReader("x"))
			req.Header.Set("Authorization", "Bearer "+token)
			req.Header.Set("Content-Range", tc.header)
			req.Header.Set("Content-Type", "application/octet-stream")
			w := httptest.NewRecorder()
			s.Engine.ServeHTTP(w, req)
			mediaErrorEnvelope(t, w, tc.status, tc.message)
		})
	}
}

func Test镜像非法输入先于网络安全拒绝(t *testing.T) {
	s, st, _, _, token := newISOServer(t)
	setMedia(s, st, st, iso.New(st, iso.Dirs{Staging: t.TempDir(), Artifacts: t.TempDir()}), &mockISODeletion{st: st})
	for _, sources := range []string{
		`[{"kind":"url","addr":"http://127.0.0.1/image.iso","sha256":"BAD"}]`,
		`[{"kind":"url","addr":"http://127.0.0.1/image.iso"},{"kind":"upload","role":"netboot","size":0}]`,
	} {
		w := httpCall(s, "POST", "/console/v1/isos", `{"name":"preflight","family":"linux","series":"debian","sources":`+sources+`}`, token)
		mediaErrorEnvelope(t, w, 400, model.ConsoleBadRequest)
		if w.Header().Get("Location") != "" {
			t.Fatal("预检失败不应创建镜像")
		}
	}
	page, err := st.ListISOs(t.Context(), repository.ISOQuery{})
	if err != nil || len(page.Items) != 0 {
		t.Fatal(page, err)
	}
	w := httpCall(s, "POST", "/console/v1/isos", `{"name":"network-rejected","family":"linux","series":"rocky","sources":[{"kind":"url","addr":"http://127.0.0.1/image.iso"}]}`, token)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "iso_source_rejected") || w.Header().Get("Location") == "" {
		t.Fatal(w.Code, w.Body.String(), w.Header())
	}
	id := strings.TrimPrefix(w.Header().Get("Location"), "/console/v1/isos/")
	if _, err := st.GetISO(t.Context(), id); err != nil {
		t.Fatal(err)
	}
	sources, err := st.SourcesOf(t.Context(), id)
	if err != nil || len(sources) != 0 {
		t.Fatal(sources, err)
	}
	audits, err := st.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: id, Action: "iso.create"})
	if err != nil || len(audits.Items) != 1 {
		t.Fatal(audits, err)
	}
}
