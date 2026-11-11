package console

import (
	"encoding/json"
	"fmt"
	"testing"
)

func TestNumberedPaginationHTTP(t *testing.T) {
	s, st, _ := newHTTPServer(t)
	token := adminHTTP(t, s, st)
	for i := 0; i < 5; i++ {
		responseData[hostResponse](t, httpCall(s, "POST", "/console/v1/hosts", fmt.Sprintf(`{"sn":"PAGE-%d","attrs":{"rack":"numbered"}}`, i), token))
	}
	paths := []string{"hosts", "tasks", "isos", "hooks", "templates", "audit", "users"}
	for _, path := range paths {
		t.Run(path, func(t *testing.T) {
			for _, query := range []string{"page", "page=1&page=2", "page_size=-1", "page_size=99999999999999999999999", "page=0", "page=-1", "page=no", "page=9999999999999999999999", "page=1&page_size=0", "page_size=501", "page_size=no", "page=9223372036854775807&page_size=500", "page=1&cursor=", "page_size=20&limit=0"} {
				w := httpCall(s, "GET", "/console/v1/"+path+"?"+query, "", token)
				if w.Code != 400 {
					t.Fatalf("%s: want 400 got %d %s", query, w.Code, w.Body.String())
				}
			}
			data := responseData[map[string]json.RawMessage](t, httpCall(s, "GET", "/console/v1/"+path+"?page=2&page_size=1", "", token))
			if _, ok := data["total"]; !ok {
				t.Fatalf("numbered response missing total: %v", data)
			}
			wantTotal := map[string]string{"hosts": "5", "tasks": "0", "isos": "0", "hooks": "0", "templates": "0", "audit": "6", "users": "1"}[path]
			if string(data["total"]) != wantTotal {
				t.Fatalf("want total %s got %s", wantTotal, data["total"])
			}
		})
	}
	data := responseData[struct {
		Items []hostResponse
		Total int64
		Next  string
	}](t, httpCall(s, "GET", "/console/v1/hosts?prefix=PAGE-&page=2&page_size=2", "", token))
	if len(data.Items) != 2 || data.Items[0].SN != "PAGE-2" || data.Total != 5 || data.Next == "" {
		t.Fatalf("wrong filtered second page: %+v", data)
	}
	empty := responseData[map[string]json.RawMessage](t, httpCall(s, "GET", "/console/v1/hosts?prefix=missing&page=1", "", token))
	if string(empty["total"]) != "0" || string(empty["items"]) != "[]" {
		t.Fatalf("empty page: %v", empty)
	}
	legacy := responseData[map[string]json.RawMessage](t, httpCall(s, "GET", "/console/v1/hosts?limit=2", "", token))
	if _, ok := legacy["total"]; ok {
		t.Fatal("legacy response must not invent total")
	}
}
