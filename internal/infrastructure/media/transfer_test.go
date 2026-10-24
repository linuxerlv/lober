package iso

import (
	"context"
	"github.com/lober/lober/internal/domain/model"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func Test镜像传输暂存完整片后才写确认文件(t *testing.T) {
	dir := t.TempDir()
	tr := NewTransfer(Dirs{Staging: dir})
	src := model.Source{ISOID: "image", Kind: model.SourceUpload, TotalSize: 4, State: model.SourceFetching}
	if u, err := tr.BeginUpload(t.Context(), src, 0, strings.NewReader("abcde")); err == nil {
		u.Close()
		t.Fatal("接受超长片")
	}
	if _, err := os.Stat(filepath.Join(dir, "image.part")); !os.IsNotExist(err) {
		t.Fatalf("非法片触碰确认文件：%v", err)
	}
	u, err := tr.BeginUpload(t.Context(), src, 0, strings.NewReader("abcd"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Size() != 4 {
		t.Fatal("实际长度错误", u.Size())
	}
	if err = u.CheckOverlap(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if err = u.Write(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err = u.Close(); err != nil {
		t.Fatal(err)
	}
	bad, err := tr.BeginUpload(t.Context(), src, 1, strings.NewReader("XX"))
	if err != nil {
		t.Fatal(err)
	}
	if err = bad.CheckOverlap(t.Context(), []model.Range{{Start: 0, Length: 4}}); err == nil {
		t.Fatal("接受错误重叠字节")
	}
	bad.Close()
	b, err := os.ReadFile(filepath.Join(dir, "image.part"))
	if err != nil || string(b) != "abcd" {
		t.Fatal(string(b), err)
	}
	session, err := tr.BeginPublish(context.Background(), src)
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close()
	v, err := session.Verify(t.Context())
	if err != nil || v.Size != 4 || v.SHA256 != "88d4266fd4e6338d13b845fcf289579d209c897823b9217da3e161936f031589" {
		t.Fatal(v, err)
	}
	if err = session.Publish(t.Context()); err != nil {
		t.Fatal(err)
	}
	b, err = os.ReadFile(filepath.Join(dir, "image"))
	if err != nil || string(b) != "abcd" {
		t.Fatal(string(b), err)
	}
}
