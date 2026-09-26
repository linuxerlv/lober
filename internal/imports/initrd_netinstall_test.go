// 本文件锁住「光驱版 initrd 不能用于 PXE 装机」在导入时就被挡住。
//
// **实测的失败模式（2026-09-26，Debian 那轮）**：Debian netinst ISO 里
// install.amd/initrd.gz 是**光驱版** installer initrd。导入把它当成制品集的
// initrd，机器 PXE 引导起来之后停在一个交互对话框上：
//
//	[!!] Detect and mount installation media
//	Your installation media couldn't be mounted. When installing from
//	CD-ROM, this probably means that the disk was not in the drive.
//	Retry mounting installation media?   <Yes>   <No>
//
// 而那台机器没有人在看。任务停在 booting 直到超时，服务端侧没有任何报错——
// 状态机看到的只是「引导过了、信号没来」。
//
// 根因是两种 initrd 装的组件不同（实测对比）：
//
//	                     net-retriever  network-preseed  cdrom-detect
//	netinst 的光驱版          无              无             有
//	官方 netboot.tar.gz       有              有             无
//
// 少了 network-preseed，菜单里的 `preseed/url=` 根本不会被读取——nginx 日志
// 里连一次应答文件请求都没有；少了 net-retriever，anna 也没法从网络镜像取
// 后续组件。这不是参数配错，是那个 initrd 物理上不具备网络安装能力。
//
// 判据只认「阳性证据」三态，不猜：
//
//	找到 network-preseed                     → 能用
//	找到 cdrom-detect 且没有 network-preseed → 确定不能用，导入失败
//	两个都没找到（换了压缩格式/换了布局）   → 无法判定，不阻塞导入
//
// 第三态刻意不失败：把「没看出来」当成「不能用」会挡掉合法的自定义 installer
// 树，而本检查的价值在于挡住那个已经确证的失败模式，不在于当守门人。
package imports

import (
	"bytes"
	"compress/gzip"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

// fakeInitrd 造一个含指定文件名的 gzip 流。newc cpio 的头里文件名是明文
// NUL 结尾串，所以按文件名做字节搜索即可判定组件是否在——不需要完整解析
// cpio（一个 76MB 的 initrd 里只要找一个路径存不存在）。
func fakeInitrd(t *testing.T, names ...string) string {
	t.Helper()
	var raw bytes.Buffer
	for _, n := range names {
		raw.WriteString("070701000000000000008180000000000000000000000001")
		raw.WriteString(n)
		raw.WriteByte(0)
	}
	path := filepath.Join(t.TempDir(), "initrd.gz")
	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("建临时 initrd 失败: %v", err)
	}
	defer f.Close()
	zw := gzip.NewWriter(f)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatalf("写 gzip 失败: %v", err)
	}
	if err := zw.Close(); err != nil {
		t.Fatalf("关 gzip 失败: %v", err)
	}
	return path
}

func TestInitrdNetInstallCapable_NetbootInitrd(t *testing.T) {
	p := fakeInitrd(t,
		"var/lib/dpkg/info/network-preseed.postinst",
		"usr/lib/debian-installer/retriever/net-retriever",
	)
	if err := CheckInitrdNetInstallCapable(p); err != nil {
		t.Errorf("官方 netboot initrd 应判为能用: %v", err)
	}
}

func TestInitrdNetInstallCapable_CDInitrdRejected(t *testing.T) {
	p := fakeInitrd(t,
		"var/lib/dpkg/info/cdrom-detect.postinst",
		"var/lib/dpkg/info/cdrom-retriever.templates",
		"var/lib/dpkg/info/file-preseed.postinst",
	)
	err := CheckInitrdNetInstallCapable(p)
	if err == nil {
		t.Fatal("光驱版 initrd 必须被拒——它缺 network-preseed，菜单里的 preseed/url= " +
			"根本不会被读取，机器会停在 [!!] Detect and mount installation media " +
			"等人按回车，而那台机器没有人在看")
	}
	if !errors.Is(err, ErrInitrdCDOnly) {
		t.Errorf("应返回 ErrInitrdCDOnly 以便调用方给出可操作的提示，实际: %v", err)
	}
	// 报错必须说清「要拿什么去换」，否则运维只知道失败不知道怎么办。
	for _, want := range []string{"netboot", "network-preseed"} {
		if !bytes.Contains([]byte(err.Error()), []byte(want)) {
			t.Errorf("报错里应出现 %q，实际: %v", want, err)
		}
	}
}

func TestInitrdNetInstallCapable_UndetectableDoesNotBlock(t *testing.T) {
	// 两个标记都没有：可能是换了压缩格式或换了布局的自定义 installer 树。
	// 把「没看出来」当成「不能用」会挡掉合法制品，所以这一态不阻塞。
	p := fakeInitrd(t, "bin/busybox", "init")
	if err := CheckInitrdNetInstallCapable(p); err != nil {
		t.Errorf("两个标记都没找到时不应阻塞导入: %v", err)
	}
}

func TestInitrdNetInstallCapable_NonGzipDoesNotBlock(t *testing.T) {
	p := filepath.Join(t.TempDir(), "initrd.gz")
	if err := os.WriteFile(p, []byte("this is not gzip at all"), 0o644); err != nil {
		t.Fatalf("写文件失败: %v", err)
	}
	if err := CheckInitrdNetInstallCapable(p); err != nil {
		t.Errorf("解不开时不应阻塞导入（zstd/lz4 压缩的 initrd 是合法的）: %v", err)
	}
}

// TestFindDebianInstallerFile_PrefersShallowest 锁住变体子目录不会被误选。
//
// **实测踩到（2026-09-26）**：Debian 13 netinst 的 install.amd/ 下同时有
//
//	install.amd/initrd.gz        裸机 PXE 要的那个
//	install.amd/gtk/initrd.gz    图形版 installer（需要 framebuffer）
//	install.amd/xen/initrd.gz    Xen 专用
//
// fs.WalkDir 按字典序访问目录项，而 "gtk" < "initrd.gz"，于是取遍历序第一个会
// 选中图形版。那个 installer 在无头机器上起不来，而装机中的机器没有人在看。
func TestFindDebianInstallerFile_PrefersShallowest(t *testing.T) {
	root := fstest.MapFS{
		"install.amd/gtk/initrd.gz": {Data: []byte("gtk")},
		"install.amd/initrd.gz":     {Data: []byte("plain")},
		"install.amd/xen/initrd.gz": {Data: []byte("xen")},
		"install.amd/linux":         {Data: []byte("k")},
	}
	got, err := findDebianInstallerFile(root, "initrd.gz")
	if err != nil {
		t.Fatalf("查找失败: %v", err)
	}
	if got != "install.amd/initrd.gz" {
		t.Errorf("选中了 %q，want install.amd/initrd.gz——"+
			"变体子目录（gtk 图形版 / xen 专用）在无头 PXE 装机上用不了", got)
	}
}
