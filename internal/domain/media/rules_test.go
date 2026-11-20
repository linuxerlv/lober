package media

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"testing"
)

func Test镜像识别只接受可靠版本架构(t *testing.T) {
	for _, c := range []struct {
		value Identified
		path  string
	}{
		{Identified{Version: "", Arch: "x86_64"}, "version"},
		{Identified{Version: "10.0", Arch: "unknown"}, "arch"},
	} {
		var p model.Problem
		if err := CheckIdentified(c.value); !errors.As(err, &p) || p.Code != "iso_version_missing" || p.Subject.Path != c.path {
			t.Fatalf("无效识别结果被接受: %v", err)
		}
	}
	if err := CheckIdentified(Identified{Version: "10.0", Arch: "x86_64"}); err != nil {
		t.Fatal(err)
	}
}

func Test镜像恢复必须等所有登记来源及必需来源完成(t *testing.T) {
	item := model.ISO{Series: "debian"}
	main := model.Source{State: model.SourceFetched}
	extra := model.Source{Role: "netboot", State: model.SourceFetched}
	if AllFetched(item, []model.Source{main}) {
		t.Fatal("缺网络介质仍可恢复")
	}
	if !AllFetched(item, []model.Source{main, extra}) {
		t.Fatal("齐备来源无法恢复")
	}
	extra.State = model.SourceFetching
	if AllFetched(item, []model.Source{main, extra}) {
		t.Fatal("未下载完仍可恢复")
	}
}
