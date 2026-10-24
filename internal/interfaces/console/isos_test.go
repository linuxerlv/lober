package console

import (
	"context"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/lober/lober/internal/domain/catalog"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	iso "github.com/lober/lober/internal/infrastructure/media"
	"github.com/lober/lober/internal/infrastructure/persistence/pgstore"
)

type sourceCall struct {
	Item                 model.ISO
	Kind, Addr, Role, ID string
	Size, Offset         int64
	SHA256, Body         string
	Audit                repository.Audit
}
type mockISOSource struct {
	mu                 sync.Mutex
	calls              map[string][]sourceCall
	staging, artifacts int64
	err, spaceErr      error
}

func (m *mockISOSource) record(method string, v sourceCall) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls[method] = append(m.calls[method], v)
	return m.err
}
func (m *mockISOSource) Register(_ context.Context, item model.ISO, kind, addr, role string, size int64, digest string, a repository.Audit) error {
	return m.record("register", sourceCall{Item: item, Kind: kind, Addr: addr, Role: role, Size: size, SHA256: digest, Audit: a})
}
func (m *mockISOSource) Upload(_ context.Context, id, role string, offset int64, r io.Reader, a repository.Audit) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return m.record("upload", sourceCall{ID: id, Role: role, Offset: offset, Body: string(b), Audit: a})
}
func (m *mockISOSource) Cancel(_ context.Context, id string, a repository.Audit) error {
	return m.record("cancel", sourceCall{ID: id, Audit: a})
}
func (m *mockISOSource) Retry(_ context.Context, id string, a repository.Audit) error {
	return m.record("retry", sourceCall{ID: id, Audit: a})
}
func (m *mockISOSource) Reimport(_ context.Context, id string, a repository.Audit) error {
	return m.record("reimport", sourceCall{ID: id, Audit: a})
}
func (m *mockISOSource) FreeSpace(context.Context) (int64, int64, error) {
	return m.staging, m.artifacts, m.spaceErr
}

type deletionCall struct {
	Item    model.ISO
	Sources []model.Source
	Rev     int64
	Audit   repository.Audit
}
type isoDeletionStore interface {
	GetISO(context.Context, string) (model.ISO, error)
	SourcesOf(context.Context, string) ([]model.Source, error)
	DeleteISO(context.Context, string, int64, repository.Audit) error
}
type mockISODeletion struct {
	st    isoDeletionStore
	calls []deletionCall
	err   error
}

func (m *mockISODeletion) Delete(ctx context.Context, id string, rev int64, a repository.Audit) error {
	if m.err != nil {
		return m.err
	}
	item, err := m.st.GetISO(ctx, id)
	if err != nil {
		return err
	}
	sources, err := m.st.SourcesOf(ctx, id)
	if err != nil {
		return err
	}
	m.calls = append(m.calls, deletionCall{item, sources, rev, a})
	return m.st.DeleteISO(ctx, id, rev, a)
}
func newISOServer(t *testing.T) (*Server, *pgstore.Store, *mockISOSource, *mockISODeletion, string) {
	t.Helper()
	s, st, _ := newHTTPServer(t)
	src := &mockISOSource{calls: map[string][]sourceCall{}, staging: 1 << 40, artifacts: 1 << 40}
	deletion := &mockISODeletion{st: st}
	setMedia(s, st, st, src, deletion)
	return s, st, src, deletion, adminHTTP(t, s, st)
}
func readyISO(t *testing.T, st repository.ISOs, name string, series catalog.Series) model.ISO {
	t.Helper()
	item, err := st.CreateISO(t.Context(), model.ISO{Name: name, Family: series.Family(), Series: series}, repository.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	item.Version = "9.6"
	item.Arch = catalog.ArchX8664
	item.State = model.ISOReady
	item, err = st.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	return item
}
func TestISO登记多来源与真实参数(t *testing.T) {
	s, st, src, _, token := newISOServer(t)
	item := responseData[isoResponse](t, httpCall(s, "POST", "/console/v1/isos", `{"name":"Debian 13","family":"linux","series":"debian","sources":[{"kind":"url","addr":"https://mirror/debian.iso","sha256":"`+strings.Repeat("a", 64)+`"},{"kind":"upload","role":"netboot","addr":"netboot.tar.gz","size":100}]}`, token))
	if item.State != model.ISOAcquiring || item.Rev != 1 || item.Artifacts == nil || item.Offers == nil {
		t.Fatal(item)
	}
	calls := src.calls["register"]
	if len(calls) != 2 {
		t.Fatal(calls)
	}
	if calls[0].Item.ID != item.ID || calls[0].Kind != "url" || calls[0].Addr != "https://mirror/debian.iso" || calls[0].SHA256 != strings.Repeat("a", 64) || calls[1].Role != "netboot" || calls[1].Size != 100 || calls[1].Addr != "" {
		t.Fatal(calls)
	}
	for _, c := range calls {
		if c.Audit.Actor != "admin" || c.Audit.Action != "iso.create" || c.Audit.At != fixedAt {
			t.Fatal(c)
		}
	}
	audits, err := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, a := range audits.Items {
		if a.TargetID == item.ID && a.Action == "iso.create" && a.Actor == "admin" {
			found = true
		}
	}
	if !found {
		t.Fatal("missing create audit")
	}
}

func Test镜像创建后来源失败保留位置及已提交事实(t *testing.T) {
	s, st, _, _, token := newISOServer(t)
	setMedia(s, st, st, newRealMediaControl(st, iso.Dirs{Staging: t.TempDir(), Artifacts: t.TempDir()}), &mockISODeletion{st: st})
	w := httpCall(s, "POST", "/console/v1/isos", `{"name":"partial","family":"linux","series":"debian","sources":[{"kind":"upload","size":4},{"kind":"path","addr":"/outside/netboot.iso","role":"netboot"}]}`, token)
	id := strings.TrimPrefix(w.Header().Get("Location"), "/console/v1/isos/")
	if w.Code != 422 || id == "" || id == w.Header().Get("Location") || !strings.Contains(w.Body.String(), "iso_source_rejected") {
		t.Fatal(w.Code, w.Body.String(), w.Header().Get("Location"))
	}
	item, err := st.GetISO(t.Context(), id)
	if err != nil || item.State != model.ISOAcquiring || item.Rev != 1 {
		t.Fatal(item, err)
	}
	src, exists, err := st.GetSource(t.Context(), id, "")
	if err != nil || !exists || src.State != model.SourcePending || src.Kind != model.SourceUpload || src.TotalSize != 4 || src.Rev != 1 {
		t.Fatal(src, exists, err)
	}
	_, exists, err = st.GetSource(t.Context(), id, "netboot")
	if err != nil || exists {
		t.Fatal(exists, err)
	}
	audits, err := st.ListAudit(t.Context(), repository.AuditQuery{TargetKind: "iso", TargetID: id, Action: "iso.create"})
	if err != nil || len(audits.Items) != 2 {
		t.Fatal(audits, err)
	}
	for _, a := range audits.Items {
		if a.Actor != "admin" || a.At != fixedAt {
			t.Fatal(a)
		}
	}
}
func TestISO坏登记不产生记录(t *testing.T) {
	s, st, src, _, token := newISOServer(t)
	sources := []string{`[]`, `[{"kind":"ftp","addr":"x"}]`, `[{"kind":"url","addr":""}]`, `[{"kind":"upload","size":0}]`, `[{"kind":"upload","size":-1}]`, `[{"kind":"url","addr":"x","sha256":"ABC"}]`, `[{"kind":"url","addr":"x"},{"kind":"url","addr":"y"}]`, `[{"kind":"url","addr":"x","role":"netboot"}]`, `[{"kind":"url","addr":"x"},{"kind":"url","addr":"y","role":"other"}]`}
	for _, source := range sources {
		w := httpCall(s, "POST", "/console/v1/isos", `{"name":"x","family":"linux","series":"rocky","sources":`+source+`}`, token)
		if w.Code != 400 {
			t.Fatal(source, w.Code, w.Body.String())
		}
	}
	for _, body := range []string{`{}`, `{"name":"x","family":"windows","series":"rocky","sources":[{"kind":"url","addr":"x"}]}`} {
		if w := httpCall(s, "POST", "/console/v1/isos", body, token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	page, err := st.ListISOs(t.Context(), repository.ISOQuery{})
	if err != nil || len(page.Items) != 0 || len(src.calls["register"]) != 0 {
		t.Fatal(page, err, src.calls)
	}
}
func TestISO余量累加与错误分类(t *testing.T) {
	s, st, src, _, token := newISOServer(t)
	src.staging = 100
	body := `{"name":"Debian","family":"linux","series":"debian","sources":[{"kind":"upload","size":80},{"kind":"upload","role":"netboot","size":80}]}`
	w := httpCall(s, "POST", "/console/v1/isos", body, token)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "60") || !strings.Contains(w.Body.String(), "iso_space_insufficient") {
		t.Fatal(w.Code, w.Body.String())
	}
	page, _ := st.ListISOs(t.Context(), repository.ISOQuery{})
	if len(page.Items) != 0 {
		t.Fatal("space rejection wrote ISO")
	}
	src.spaceErr = errors.New("private staging path")
	if w := httpCall(s, "POST", "/console/v1/isos", body, token); w.Code != 500 || strings.Contains(w.Body.String(), "private") {
		t.Fatal(w.Code, w.Body.String())
	}
	src.spaceErr = nil
	src.staging = math.MaxInt64
	body = fmt.Sprintf(`{"name":"Debian","family":"linux","series":"debian","sources":[{"kind":"upload","size":%d},{"kind":"upload","role":"netboot","size":80}]}`, int64(math.MaxInt64))
	if w := httpCall(s, "POST", "/console/v1/isos", body, token); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	src.err = &repository.ProblemsError{Kind: repository.ErrInvalid, Problems: []model.Problem{model.NewProblem("iso_source_rejected", model.Subject{Kind: model.KindISO}, model.ISOReasonURL)}}
	w = httpCall(s, "POST", "/console/v1/isos", `{"name":"x","family":"linux","series":"rocky","sources":[{"kind":"url","addr":"http://127.0.0.1/x.iso"}]}`, token)
	if w.Code != 422 || !strings.Contains(w.Body.String(), "iso_source_rejected") {
		t.Fatal(w.Code, w.Body.String())
	}
	if w.Header().Get("Location") == "" {
		t.Fatal("failed registration must expose created ISO identity for recovery")
	}
}
func TestISO读取过滤分页废弃和删除接线(t *testing.T) {
	s, st, _, deletion, token := newISOServer(t)
	rocky := readyISO(t, st, "Rocky", catalog.SeriesRocky)
	readyISO(t, st, "Debian", catalog.SeriesDebian)
	p := responseData[isoPage](t, httpCall(s, "GET", "/console/v1/isos?limit=1", "", token))
	if len(p.Items) != 1 || p.Items[0].Name != "Debian" || p.Next == "" {
		t.Fatal(p)
	}
	p = responseData[isoPage](t, httpCall(s, "GET", "/console/v1/isos?cursor="+p.Next+"&limit=1", "", token))
	if len(p.Items) != 1 || p.Items[0].Name != "Rocky" {
		t.Fatal(p)
	}
	p = responseData[isoPage](t, httpCall(s, "GET", "/console/v1/isos?series=rocky&state=ready", "", token))
	if len(p.Items) != 1 || p.Items[0].ID != rocky.ID {
		t.Fatal(p)
	}
	responseData[isoResponse](t, httpCall(s, "GET", "/console/v1/isos/"+rocky.ID, "", token))
	path := "/console/v1/isos/" + rocky.ID
	for _, rev := range []string{"", "bad", "0", "-1"} {
		if w := httpCall(s, "POST", path+"/deprecate?rev="+rev, "", token); w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	responseData[any](t, httpCall(s, "POST", fmt.Sprintf("%s/deprecate?rev=%d", path, rocky.Rev), "", token))
	if w := httpCall(s, "POST", fmt.Sprintf("%s/deprecate?rev=%d", path, rocky.Rev), "", token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	item, err := st.GetISO(t.Context(), rocky.ID)
	if err != nil || item.State != model.ISODeprecated {
		t.Fatal(item, err)
	}
	deletion.err = errors.New("journal failure")
	if w := httpCall(s, "DELETE", fmt.Sprintf("%s?rev=%d", path, item.Rev), "", token); w.Code != 500 {
		t.Fatal(w.Code, w.Body.String())
	}
	if _, err := st.GetISO(t.Context(), rocky.ID); err != nil {
		t.Fatal("failed deletion adapter bypassed", err)
	}
	deletion.err = nil
	responseData[any](t, httpCall(s, "DELETE", fmt.Sprintf("%s?rev=%d", path, item.Rev), "", token))
	if len(deletion.calls) != 1 || deletion.calls[0].Item.ID != item.ID || deletion.calls[0].Audit.Action != "iso.delete" || deletion.calls[0].Sources == nil {
		t.Fatal(deletion.calls)
	}
	if w := httpCall(s, "GET", path, "", token); w.Code != 404 {
		t.Fatal(w.Code, w.Body.String())
	}
	for _, q := range []string{"limit=bad", "limit=501", "state=no", "family=no"} {
		if w := httpCall(s, "GET", "/console/v1/isos?"+q, "", token); w.Code != 400 {
			t.Fatal(q, w.Code, w.Body.String())
		}
	}
}
func TestISO操作委托与分片不审计(t *testing.T) {
	s, st, src, _, token := newISOServer(t)
	item := readyISO(t, st, "Rocky", catalog.SeriesRocky)
	path := "/console/v1/isos/" + item.ID
	for _, op := range []string{"cancel", "retry", "reimport"} {
		responseData[any](t, httpCall(s, "POST", path+"/"+op, "", token))
		cs := src.calls[op]
		if len(cs) != 1 || cs[0].ID != item.ID || cs[0].Audit.Action != "iso."+op || cs[0].Audit.Actor != "admin" {
			t.Fatal(op, cs)
		}
	}
	// Source progress is persisted independently by layer4; the console only reads it.
	saved, err := st.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceUpload, TotalSize: 19, State: model.SourcePending}, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	upload := func(header, body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("PUT", path+"/content", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Range", header)
		req.Header.Set("Content-Type", "application/octet-stream")
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, req)
		return w
	}
	before, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	responseData[any](t, upload("bytes 0-8/19", "前半段"))
	cs := src.calls["upload"]
	if len(cs) != 1 || cs[0].Offset != 0 || cs[0].Body != "前半段" || cs[0].Audit != (repository.Audit{}) {
		t.Fatal(cs)
	}
	for _, pair := range [][2]string{{"bytes 0-8/19", "x"}, {"bytes 0-1/19", "xxx"}, {"bytes 0-8/20", "前半段"}, {"bytes 0-8", "前半段"}, {"", "x"}} {
		if w := upload(pair[0], pair[1]); w.Code != 400 {
			t.Fatal(pair, w.Code, w.Body.String())
		}
	}
	if len(src.calls["upload"]) != 1 {
		t.Fatal("malformed range handed to source")
	}
	after, _ := st.ListAudit(t.Context(), repository.AuditQuery{Limit: 500})
	if len(after.Items) != len(before.Items) {
		t.Fatal("chunk audited")
	}
	src.err = repository.ErrConflict
	if w := httpCall(s, "POST", path+"/retry", "", token); w.Code != 409 {
		t.Fatal(w.Code, w.Body.String())
	}
	src.err = errors.New("secret backend")
	if w := httpCall(s, "POST", path+"/cancel", "", token); w.Code != 500 || strings.Contains(w.Body.String(), "secret") {
		t.Fatal(w.Code, w.Body.String())
	}
	_ = saved
}
func TestISO来源进度包含已持久化区间(t *testing.T) {
	s, st, _, _, token := newISOServer(t)
	item := readyISO(t, st, "Rocky", catalog.SeriesRocky)
	source, err := st.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceUpload, TotalSize: 20, State: model.SourcePending}, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	source.State = model.SourceFetching
	source, err = st.SaveSource(t.Context(), source, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveRange(t.Context(), item.ID, "", 0, 9); err != nil {
		t.Fatal(err)
	}
	source, _, _ = st.GetSource(t.Context(), item.ID, "")
	p := responseData[isoProgress](t, httpCall(s, "GET", "/console/v1/isos/"+item.ID+"/progress", "", token))
	if p.ID != item.ID || p.State != item.State || len(p.Sources) != 1 || p.Sources[0].Fetched != 9 || p.Sources[0].Rev != source.Rev || !reflect.DeepEqual(p.Sources[0].Ranges, []sourceRange{{Start: 0, Length: 9}}) {
		t.Fatal(p)
	}
}
func TestISO所有接口五角色权限(t *testing.T) {
	s, st, _, _, _ := newISOServer(t)
	for i, role := range model.Roles() {
		name := fmt.Sprintf("iso%d", i)
		seedAuthUser(t, st, name, role, "pw")
		token := loginHTTP(t, s, name, "pw").Token
		count := 0
		for _, rt := range s.Routes() {
			if rt.Perm != PermISORead && rt.Perm != PermISOWrite {
				continue
			}
			count++
			path := strings.ReplaceAll(rt.Path, ":id", "missing")
			w := httpCall(s, rt.Method, path, `{}`, token)
			allowed := rt.Perm == PermISORead || role == model.RoleAdmin
			if allowed && (w.Code == 401 || w.Code == 403) || !allowed && w.Code != 403 {
				t.Fatal(role, rt, w.Code)
			}
		}
		if count != 11 {
			t.Fatal(count)
		}
	}
}
func Test解析ContentRange严格语法与边界(t *testing.T) {
	for _, header := range []string{"bytes 0-8/19", "bytes 9-18/19", "bytes 0-8/*"} {
		start, end, total, ok := ParseContentRange(header)
		if !ok || end < start || total != -1 && end >= total {
			t.Fatal(header, start, end, total, ok)
		}
	}
	for _, header := range []string{"", "bytes", "bytes 0-8", "bytes 0-8/0", "bytes 8-0/19", "bytes -8/19", "chunk 0-8/19", "bytes +0-8/19", "bytes 0- 8/19", "bytes 0-8/19 junk", "bytes 0-9223372036854775807/*", "bytes 9223372036854775808-9223372036854775809/*"} {
		if _, _, _, ok := ParseContentRange(header); ok {
			t.Fatal("accepted bad range", header)
		}
	}
}

func TestISO补登记来源用于失败后的恢复(t *testing.T) {
	s, st, src, _, token := newISOServer(t)
	item, err := st.CreateISO(t.Context(), model.ISO{Name: "Debian", Family: catalog.FamilyLinux, Series: catalog.SeriesDebian}, repository.Audit{Actor: "seed", Action: "iso.create", At: fixedAt})
	if err != nil {
		t.Fatal(err)
	}
	item.State = model.ISOFailed
	item, err = st.UpdateISO(t.Context(), item)
	if err != nil {
		t.Fatal(err)
	}
	path := "/console/v1/isos/" + item.ID + "/sources"
	responseData[any](t, httpCall(s, "POST", path, `{"kind":"url","role":"netboot","addr":"https://mirror/mini.iso"}`, token))
	call := src.calls["register"][0]
	if call.Item.ID != item.ID || call.Item.State != model.ISOFailed || call.Role != "netboot" || call.Audit.Action != "iso.source.register" || call.Audit.Actor != "admin" {
		t.Fatal(call)
	}
	if w := httpCall(s, "POST", path, `{"kind":"url","role":"wrong","addr":"https://mirror/x.iso"}`, token); w.Code != 400 {
		t.Fatal(w.Code, w.Body.String())
	}
	if len(src.calls["register"]) != 1 {
		t.Fatal("invalid extra role handed to source")
	}
}

type progressInterleave struct {
	repository.ISOSources
	st   repository.ISOSources
	once sync.Once
}

func (p *progressInterleave) RangesOf(ctx context.Context, id, role string) ([]model.Range, error) {
	var err error
	p.once.Do(func() { err = p.st.SaveRange(ctx, id, role, 0, 9) })
	if err != nil {
		return nil, err
	}
	return p.ISOSources.RangesOf(ctx, id, role)
}
func TestISO读取进度时并发上传不混合计数与区间(t *testing.T) {
	s, st, _, _, token := newISOServer(t)
	item := readyISO(t, st, "Rocky", catalog.SeriesRocky)
	source, err := st.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceUpload, TotalSize: 20, State: model.SourcePending}, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	source.State = model.SourceFetching
	if _, err := st.SaveSource(t.Context(), source, repository.Audit{}); err != nil {
		t.Fatal(err)
	}
	setMedia(s, st, &progressInterleave{ISOSources: st, st: st}, &mockISOSource{calls: map[string][]sourceCall{}}, &mockISODeletion{st: st})
	p := responseData[isoProgress](t, httpCall(s, "GET", "/console/v1/isos/"+item.ID+"/progress", "", token))
	if len(p.Sources) != 1 || p.Sources[0].Fetched != 9 || len(p.Sources[0].Ranges) != 1 || p.Sources[0].Ranges[0].Length != p.Sources[0].Fetched {
		t.Fatal(p)
	}
}
func TestISO分片未知长度仍拒绝错误字节数(t *testing.T) {
	s, st, src, _, token := newISOServer(t)
	item := readyISO(t, st, "Rocky", catalog.SeriesRocky)
	_, err := st.SaveSource(t.Context(), model.Source{ISOID: item.ID, Kind: model.SourceUpload, TotalSize: 20, State: model.SourcePending}, repository.Audit{})
	if err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"x", "1234567890"} {
		req := httptest.NewRequest("PUT", "/console/v1/isos/"+item.ID+"/content", strings.NewReader(body))
		req.ContentLength = -1
		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Content-Range", "bytes 0-8/*")
		w := httptest.NewRecorder()
		s.Engine.ServeHTTP(w, req)
		mediaErrorEnvelope(t, w, 400, model.ConsoleBadRequest)
		if w.Code != 400 {
			t.Fatal(w.Code, w.Body.String())
		}
	}
	if len(src.calls["upload"]) != 0 {
		t.Fatal("malformed stream accepted")
	}
}
func TestISO上传OpenAPI描述二进制与必填范围头(t *testing.T) {
	s, _, _ := newHTTPServer(t)
	op := s.OpenAPI()["paths"].(map[string]any)["/console/v1/isos/{id}/content"].(map[string]any)["put"].(map[string]any)
	found := false
	for _, p := range op["parameters"].([]any) {
		parameter := p.(map[string]any)
		if parameter["name"] == "Content-Range" && parameter["in"] == "header" && parameter["required"] == true {
			found = true
		}
	}
	if !found {
		t.Fatal(op)
	}
	body := op["requestBody"].(map[string]any)["content"].(map[string]any)["application/octet-stream"].(map[string]any)["schema"].(map[string]any)
	if body["format"] != "binary" {
		t.Fatal(body)
	}
}
