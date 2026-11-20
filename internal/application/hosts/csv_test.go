package hosts

import (
	"github.com/lober/lober/internal/domain/model"
	"reflect"
	"strings"
	"testing"
)

func TestCSV解析保留属性原文与物理行号(t *testing.T) {
	data := "\xef\xbb\xbfsn,arch,firmware,rack,note\r\nsn-1,x86_64,uefi, A01 ,\"line1\r\nline2\"\r\nSN-2,,,A02,\r\n"
	p, hs, err := parseHostCSV([]byte(data))
	repo, service, _ := importFixture()
	repo.known = map[string]model.Host{"SN-1": {SN: "SN-1"}}
	result, previewErr := service.Import(t.Context(), importActor, ImportRequest{CSV: []byte(data), DryRun: true})
	if previewErr != nil || repo.readCalls != 1 || !reflect.DeepEqual(repo.sns, []string{"SN-1", "SN-2"}) {
		t.Fatal(previewErr, repo)
	}
	p = result.Plan
	if err != nil || len(hs) != 2 || p.Total != 2 {
		t.Fatal(p, hs, err)
	}
	if hs[0].SN != "SN-1" || hs[0].Attrs["rack"] != " A01 " || hs[0].Attrs["note"] != "line1\nline2" || hs[1].Arch != "" {
		t.Fatal(hs)
	}
	if p.Rows[0].Row != 2 || p.Rows[1].Row != 4 || p.Rows[0].Action != "update" || p.Rows[1].Action != "create" {
		t.Fatal(p)
	}
}
func TestCSV文件错误(t *testing.T) {
	for _, data := range []string{"", "sn\n", "arch\nx86_64\n", "sn,sn\nSN-1,SN-2\n", "sn,rack,rack\nSN-1,a,b\n", "sn,invalid-key\nSN-1,a\n", "sn,arch\nSN-1\n", "sn,arch\n\"SN-1,x86_64\n"} {
		t.Run(strings.ReplaceAll(data, "\n", "/"), func(t *testing.T) {
			if _, _, err := parseHostCSV([]byte(data)); err == nil {
				t.Fatal("accepted malformed file", data)
			}
		})
	}
}
func Test主机CSV重复与首错次序(t *testing.T) {
	p, hs, err := parseHostCSV([]byte("sn,arch,rack\nSN-1,x86_64,a\nsn-1,x86_64,b\nTo Be Filled By O.E.M.,,\nSN-2,amd64,\nSN-3,,\xff\nSN-4,,\x00\nSN-5,,ok\n"))
	if err != nil || p.Total != 7 || len(hs) != 1 || hs[0].SN != "SN-5" {
		t.Fatal(p, hs, err)
	}
	codes := []string{"host_duplicate_in_batch", "host_duplicate_in_batch", "sn_placeholder", "host_platform_invalid", "text_not_storable", "text_not_storable"}
	for i, code := range codes {
		r := p.Rows[i]
		if r.Action != "error" || len(r.Problems) == 0 || string(r.Problems[0].Code) != code || r.Problems[0].Message == "" || r.Problems[0].Next == "" {
			t.Fatal(i, r)
		}
	}
}

func Test主机CSV重复问题置首但保留后续顺序(t *testing.T) {
	p, hs, err := parseHostCSV([]byte("sn,arch,rack\nSN-1,amd64,\xff\nsn-1,amd64,\x00\n"))
	if err != nil || len(hs) != 0 || len(p.Rows) != 2 {
		t.Fatal(p, hs, err)
	}
	for _, row := range p.Rows {
		var codes []string
		for _, problem := range row.Problems {
			codes = append(codes, string(problem.Code))
		}
		if row.Action != "error" || !reflect.DeepEqual(codes, []string{"host_duplicate_in_batch", "host_platform_invalid", "text_not_storable"}) {
			t.Fatal(row)
		}
	}
}
