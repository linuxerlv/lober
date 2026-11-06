package media

import (
	"errors"
	"github.com/lober/lober/internal/domain/model"
	"github.com/lober/lober/internal/domain/repository"
	"testing"
)

func Test进度三次重读耗尽(t *testing.T) {
	f := &fixture{changing: true, source: model.Source{ISOID: "image", Rev: 1}}
	_, err := setup(f).Progress(t.Context(), admin, "image")
	if !errors.Is(err, repository.ErrConflict) || f.ranges != 3 {
		t.Fatal(err, f.ranges)
	}
}
func Test进度返回确认区间(t *testing.T) {
	f := &fixture{source: model.Source{ISOID: "image", Rev: 1, TotalSize: 12}}
	p, err := setup(f).Progress(t.Context(), admin, "image")
	if err != nil || len(p.Sources) != 1 || len(p.Sources[0].Ranges) != 1 || p.Sources[0].Ranges[0].Start != 4 || p.Sources[0].Ranges[0].Length != 6 {
		t.Fatal(p, err)
	}
}

func TestProgressHidesStoredSourceErrorDetails(t *testing.T) {
	for _, tc := range []struct{ stored, want string }{
		{"Get https://mirror.private.invalid/medium.iso?signature=demo-secret: no such host", "镜像来源处理失败"},
		{"镜像服务器返回 HTTP 503\nhttps://mirror.private.invalid/?signature=demo-secret", "镜像来源处理失败"},
		{"镜像服务器返回 HTTP 503", "镜像服务器返回 HTTP 503"},
		{"", ""},
	} {
		f := &fixture{source: model.Source{ISOID: "image", Kind: model.SourceRemote, State: model.SourceFailed, Rev: 1, Error: tc.stored}}
		p, err := setup(f).Progress(t.Context(), admin, "image")
		if err != nil || len(p.Sources) != 1 || p.Sources[0].Error != tc.want {
			t.Errorf("进度错误：%+v, %v；期望错误 %q", p, err, tc.want)
		}
		if f.source.Error != tc.stored {
			t.Fatal("读进度时改写了旧记录")
		}
	}
}
