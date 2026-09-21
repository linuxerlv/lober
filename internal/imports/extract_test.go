package imports

import (
	"context"
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"
)

// fakeExtractor 是 Extractor 的测试替身：不读任何真实 ISO，直接返回预置的
// 内存文件系统（任务 9：测试注入内存文件系统，不依赖真 ISO）。
type fakeExtractor struct {
	root fs.FS
	err  error
}

func (f fakeExtractor) Open(ctx context.Context, isoPath string) (fs.FS, error) {
	if f.err != nil {
		return nil, f.err
	}
	return f.root, nil
}

var _ Extractor = fakeExtractor{}

func TestExtract_FakeExtractorReturnsInjectedFS(t *testing.T) {
	root := fstest.MapFS{".treeinfo": &fstest.MapFile{Data: []byte("x")}}
	e := fakeExtractor{root: root}

	got, err := e.Open(context.Background(), "/tmp/whatever.iso")
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if _, statErr := fs.Stat(got, ".treeinfo"); statErr != nil {
		t.Fatalf("返回的文件系统应含注入的 .treeinfo 文件: %v", statErr)
	}
}

func TestExtract_MountExtractorNotYetImplemented(t *testing.T) {
	e := NewMountExtractor("")
	_, err := e.Open(context.Background(), "/tmp/whatever.iso")
	if err == nil {
		t.Fatal("MountExtractor 当前应返回「未实现」错误——真实 mount(8) 调用留给部署环境适配")
	}
	if errors.Is(err, context.Canceled) {
		t.Fatalf("错误不应是 context.Canceled 之类的无关错误，got %v", err)
	}
}
