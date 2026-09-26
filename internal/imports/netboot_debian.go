package imports

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// ErrNetbootIncomplete 表示 netboot 包里找不到内核或 initrd。
var ErrNetbootIncomplete = errors.New("imports: netboot 包里找不到内核或 initrd")

// applyDebianNetboot 用 netboot 包里的内核与 initrd 覆盖 ISO 内的那两份。
//
// **为什么需要这一步。** Debian netinst ISO 里的 install.amd/initrd.gz 是光驱版
// installer initrd：缺 network-preseed（于是菜单里的 `preseed/url=` 完全不会被
// 读取）与 net-retriever（于是 anna 没法从网络镜像取后续组件），PXE 引导起来会
// 停在「[!!] Detect and mount installation media」等人回答，而装机中的机器没有
// 人在看（实测 2026-09-26）。
//
// 能做网络安装的 initrd 由 Debian 单独分发在
// dists/<suite>/main/installer-<arch>/current/images/netboot/netboot.tar.gz。
//
// **为什么不从 ISO 的 pool/ 里自己装配一个。** 那两个组件的 udeb 确实在 ISO 的
// pool/main/ 下，把它们解包叠进光驱版 initrd、再把 cdrom-detect 从 dpkg 状态库里
// 摘掉，技术上可行——实测试过，得到的 initrd 引导时 kernel panic
// （`No working init found`）。真要做对等于在导入路径里重造 debian-installer 的
// 构建流程，且每个 Debian 版本的 udeb 集合都可能变。用上游已经构建好的制品是
// 更少的代码和更少的假设。
//
// 只换内核与 initrd 两个引导期文件，repo/ 树（dists/pool 软件包索引）仍然来自
// ISO——那是装机时真正要下载几百个包的地方，netboot 包里没有它。
//
// paths 的其余字段原样带出：本函数只改 Kernel 与 Initrd。
func applyDebianNetboot(netbootPath, destDir string, paths model.ArtifactPaths) (model.ArtifactPaths, error) {
	kernelDest := filepath.Join(destDir, paths.Kernel)
	if paths.Kernel == "" {
		return model.ArtifactPaths{}, fmt.Errorf("imports: ExtractDebian 没有给出内核文件名，无法套用 netboot 制品")
	}
	if len(paths.Initrd) == 0 {
		return model.ArtifactPaths{}, fmt.Errorf("imports: ExtractDebian 没有给出 initrd 文件名，无法套用 netboot 制品")
	}
	initrdDest := filepath.Join(destDir, paths.Initrd[0])

	gotKernel, gotInitrd, err := extractNetbootTarGz(netbootPath, kernelDest, initrdDest)
	if err != nil {
		return model.ArtifactPaths{}, err
	}
	if !gotKernel || !gotInitrd {
		return model.ArtifactPaths{}, fmt.Errorf("%w（%q 里 kernel=%t initrd=%t）："+
			"期望包内有 debian-installer/<arch>/{linux,initrd.gz}，"+
			"请确认下载的是 netboot.tar.gz 而不是 mini.iso 或其它变体",
			ErrNetbootIncomplete, netbootPath, gotKernel, gotInitrd)
	}

	// 换上来的 initrd 也要过同一道检查——拿错了包（比如下了光驱版的 mini.iso
	// 解出来的 initrd）时，失败要发生在导入期，不是机器停在对话框上时。
	if err := CheckInitrdNetInstallCapable(initrdDest); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: netboot 包 %q 里的 initrd 仍然不能用于 PXE 装机: %w",
			netbootPath, err)
	}
	return paths, nil
}

// extractNetbootTarGz 从 netboot.tar.gz 里取出内核与 initrd，写到给定目标路径。
//
// 包内布局是 ./debian-installer/<arch>/{linux,initrd.gz}（还有 pxelinux 的配置
// 与一个指向它的 ./pxelinux.0 之类，本函数不关心）。按**文件名**匹配而不是按
// 完整路径：arch 目录名随架构变（amd64 / arm64 / i386），写死会让别的架构静默
// 取不到文件。
//
// 同时排除 xen/ 子目录下的同名文件——那是 Xen 专用变体，不是给裸机 PXE 用的。
func extractNetbootTarGz(netbootPath, kernelDest, initrdDest string) (gotKernel, gotInitrd bool, err error) {
	f, err := os.Open(netbootPath)
	if err != nil {
		return false, false, fmt.Errorf("imports: 打开 netboot 包 %q 失败: %w", netbootPath, err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		return false, false, fmt.Errorf("imports: netboot 包 %q 不是 gzip 流: %w", netbootPath, err)
	}
	defer zr.Close()

	tr := tar.NewReader(zr)
	for {
		hdr, readErr := tr.Next()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return gotKernel, gotInitrd, fmt.Errorf("imports: 读 netboot 包 %q 失败: %w", netbootPath, readErr)
		}
		if hdr.Typeflag != tar.TypeReg {
			continue
		}
		name := path.Clean(strings.TrimPrefix(path.Clean(hdr.Name), "./"))
		if strings.Contains(name, "/xen/") {
			continue // Xen 专用变体，不是裸机 PXE 要的
		}
		switch path.Base(name) {
		case "linux":
			if gotKernel {
				continue
			}
			if err := writeStream(tr, kernelDest); err != nil {
				return gotKernel, gotInitrd, err
			}
			gotKernel = true
		case "initrd.gz":
			if gotInitrd {
				continue
			}
			if err := writeStream(tr, initrdDest); err != nil {
				return gotKernel, gotInitrd, err
			}
			gotInitrd = true
		}
		if gotKernel && gotInitrd {
			break
		}
	}
	return gotKernel, gotInitrd, nil
}

// writeStream 把 r 写到 dest：先写同目录下的临时文件再 rename。
//
// 直接覆盖写会让「写到一半失败」留下一个长度对不上的内核或 initrd，而那个制品集
// 可能已经是 ready 状态——机器引导时才会发现。同目录临时文件保证 rename 是同一
// 文件系统内的原子操作。
func writeStream(r io.Reader, dest string) error {
	tmp, err := os.CreateTemp(filepath.Dir(dest), filepath.Base(dest)+".tmp-*")
	if err != nil {
		return fmt.Errorf("imports: 建临时文件（目标 %q）失败: %w", dest, err)
	}
	tmpName := tmp.Name()
	defer func() {
		tmp.Close()
		os.Remove(tmpName) // rename 成功后这次 Remove 找不到文件，无害
	}()

	if _, err := io.Copy(tmp, r); err != nil {
		return fmt.Errorf("imports: 写临时文件（目标 %q）失败: %w", dest, err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("imports: 同步临时文件（目标 %q）失败: %w", dest, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("imports: 关闭临时文件（目标 %q）失败: %w", dest, err)
	}
	if err := os.Rename(tmpName, dest); err != nil {
		return fmt.Errorf("imports: 替换 %q 失败: %w", dest, err)
	}
	return nil
}
