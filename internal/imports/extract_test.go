package imports

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
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

func TestExtract_MountExtractorReadsRealISO9660File(t *testing.T) {
	// 本轮 m3 轨把 MountExtractor.Open 从骨架接上了真实的 ISO 9660 解析
	// （iso9660.go），本测试验证接线本身：把一份用 buildISO9660 生成的
	// 字节流写到磁盘上的真实文件，再通过 MountExtractor（走真实的
	// os.Open + io.ReaderAt 路径，不是内存 fstest.MapFS）读出内容——
	// 这是本包里唯一一处真正落到磁盘文件的 Extractor 测试，其它测试
	// 全部用 fakeExtractor 注入内存文件系统，职责分工明确：ISO 9660
	// 字节布局的详细覆盖在 iso9660_test.go，这里只确认 MountExtractor
	// 把「本地路径」正确接到了「解析器」上。
	isoPath := filepath.Join(t.TempDir(), "test.iso")
	data := buildISO9660(t, []isoFixtureFile{
		{path: ".treeinfo", data: []byte("[images-x86_64]\nkernel = vmlinuz\n")},
	}, false, true)
	writeFile(t, isoPath, data)

	e := NewMountExtractor("")
	fsys, err := e.Open(context.Background(), isoPath)
	if err != nil {
		t.Fatalf("Open 真实 ISO 文件失败: %v", err)
	}

	got, err := fs.ReadFile(fsys, ".treeinfo")
	if err != nil {
		t.Fatalf("读取 .treeinfo 失败: %v", err)
	}
	want := "[images-x86_64]\nkernel = vmlinuz\n"
	if string(got) != want {
		t.Fatalf("内容不匹配: got %q, want %q", got, want)
	}
}

func TestExtract_MountExtractorMissingFileReturnsError(t *testing.T) {
	e := NewMountExtractor("")
	_, err := e.Open(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.iso"))
	if err == nil {
		t.Fatal("打开不存在的 ISO 路径应返回错误")
	}
}

func TestExtract_MountExtractorCanceledContextReturnsError(t *testing.T) {
	isoPath := filepath.Join(t.TempDir(), "test.iso")
	data := buildISO9660(t, []isoFixtureFile{
		{path: ".treeinfo", data: []byte("x")},
	}, false, true)
	writeFile(t, isoPath, data)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	e := NewMountExtractor("")
	_, err := e.Open(ctx, isoPath)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("ctx 已取消时应返回包含 context.Canceled 的错误，got %v", err)
	}
}
