package imports

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
)

// ErrInitrdCDOnly 表示这个 initrd 是光驱版 installer initrd，不能用于 PXE 装机。
//
// 调用方据此给出可操作的提示：换 netboot 制品，而不是去调菜单参数。
var ErrInitrdCDOnly = errors.New("imports: initrd 是光驱版，不具备网络安装能力")

// initrd 组件标记。newc cpio 的头里文件名是明文 NUL 结尾串，所以判断某个组件
// 在不在，只需要在解压后的字节流里搜它的路径——不必完整解析 cpio（一个 76MB
// 的 initrd 里只要回答「这个路径存不存在」这一个问题）。
const (
	// initrdMarkerNetPreseed 是 network-preseed 组件。**它决定 `preseed/url=`
	// 会不会被读取**：没有它，内核命令行上的那个 URL 被完全忽略，服务端侧看到
	// 的现象是连一次应答文件请求都没有。
	initrdMarkerNetPreseed = "network-preseed"

	// initrdMarkerCDDetect 是 cdrom-detect 组件。官方 netboot initrd 里没有它，
	// 光驱版有。它在找不到介质时弹出交互对话框等人回答。
	initrdMarkerCDDetect = "cdrom-detect"
)

// initrdScanLimit 是扫描上限。
//
// 组件标记出现在 cpio 的文件头里，而 var/lib/dpkg/info/ 这些小文件排在归档
// 前部；实测 Debian 13 的两种 initrd（40MB netboot / 76MB 光驱版）解压后都在
// 前 64MB 内出现标记。设上限是为了给「解压一个恶意构造的超大 initrd」封顶，
// 而不是为了性能——扫不到就落到「无法判定」那一态，不阻塞导入。
const initrdScanLimit = 256 << 20

// CheckInitrdNetInstallCapable 判断 initrd 能否用于 PXE 网络装机。
//
// **这条检查存在的理由**：Debian netinst ISO 里 install.amd/initrd.gz 是光驱版
// installer initrd。把它当成制品集的 initrd，机器 PXE 引导起来之后会停在
//
//	[!!] Detect and mount installation media
//	Your installation media couldn't be mounted. ... Retry mounting? <Yes> <No>
//
// 等人按回车，而那台机器没有人在看。任务停在 booting 直到超时，服务端侧没有
// 任何报错——状态机看到的只是「引导过了、信号没来」。
//
// 两种 initrd 装的组件不同（实测对比 Debian 13）：
//
//	                     net-retriever  network-preseed  cdrom-detect
//	netinst 的光驱版          无              无             有
//	官方 netboot.tar.gz       有              有             无
//
// 这不是参数配错，是那个 initrd 物理上不具备网络安装能力：少了
// network-preseed，`preseed/url=` 不会被读取；少了 net-retriever，anna 也没法
// 从网络镜像取后续组件。
//
// 判据只认阳性证据，三态：
//
//	找到 network-preseed                     → nil（能用）
//	找到 cdrom-detect 且没有 network-preseed → ErrInitrdCDOnly（确定不能用）
//	两个都没找到 / 解不开                    → nil（无法判定，不阻塞）
//
// 第三态刻意不失败。把「没看出来」当成「不能用」会挡掉合法的自定义 installer
// 树，也会挡掉换了压缩格式（zstd/lz4）的 initrd；本检查的价值在于挡住那个已经
// 确证的失败模式，不在于当守门人。同理，本函数不返回「解压失败」这类错误——
// 解不开就是没看出来。
func CheckInitrdNetInstallCapable(path string) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("imports: 打开 initrd %q 失败: %w", path, err)
	}
	defer f.Close()

	zr, err := gzip.NewReader(f)
	if err != nil {
		// 不是 gzip（zstd / lz4 压缩的 initrd 是合法的）——落到「无法判定」。
		return nil
	}
	defer zr.Close()

	hasNetPreseed, hasCDDetect := scanInitrdMarkers(zr)
	switch {
	case hasNetPreseed:
		return nil
	case hasCDDetect:
		return fmt.Errorf("%w：找到 %s、没找到 %s。"+
			"Debian netinst ISO 里的 install.amd/initrd.gz 是光驱版 installer initrd，"+
			"PXE 引导起来会停在「[!!] Detect and mount installation media」这个交互对话框上"+
			"等人回答，而装机中的机器没有人在看。请改用官方 netboot 制品"+
			"（netboot.tar.gz 里的 debian-installer/<arch>/initrd.gz）——"+
			"少了 %s，菜单里的 preseed/url= 根本不会被读取",
			ErrInitrdCDOnly, initrdMarkerCDDetect, initrdMarkerNetPreseed, initrdMarkerNetPreseed)
	default:
		return nil
	}
}

// scanInitrdMarkers 在解压流里搜两个组件标记。
//
// 分块读并在块之间保留一段重叠，避免标记正好跨在两块边界上被漏掉——重叠长度
// 取最长标记减一就够（再长一个字节都不可能横跨）。
func scanInitrdMarkers(r io.Reader) (hasNetPreseed, hasCDDetect bool) {
	overlap := len(initrdMarkerNetPreseed)
	if n := len(initrdMarkerCDDetect); n > overlap {
		overlap = n
	}
	overlap--

	const chunk = 1 << 20
	buf := make([]byte, overlap+chunk)
	filled := 0
	var total int64

	for total < initrdScanLimit {
		n, err := io.ReadFull(r, buf[filled:])
		filled += n
		total += int64(n)

		window := buf[:filled]
		if !hasNetPreseed && containsBytes(window, initrdMarkerNetPreseed) {
			hasNetPreseed = true
		}
		if !hasCDDetect && containsBytes(window, initrdMarkerCDDetect) {
			hasCDDetect = true
		}
		// network-preseed 是决定性的阳性证据，找到就不必再扫。
		if hasNetPreseed {
			return true, hasCDDetect
		}
		if err != nil {
			// 包括 io.EOF / io.ErrUnexpectedEOF（读到结尾）与损坏的 gzip 流。
			// 两者都落到调用方的「无法判定」处理，不区分。
			return hasNetPreseed, hasCDDetect
		}

		// 保留尾部重叠，供下一轮拼接。
		copy(buf, window[filled-overlap:])
		filled = overlap
	}
	return hasNetPreseed, hasCDDetect
}

func containsBytes(haystack []byte, needle string) bool {
	return len(needle) > 0 && indexBytes(haystack, needle) >= 0
}

func indexBytes(haystack []byte, needle string) int {
	n := len(needle)
	if n == 0 || len(haystack) < n {
		return -1
	}
	first := needle[0]
	for i := 0; i+n <= len(haystack); i++ {
		if haystack[i] != first {
			continue
		}
		if string(haystack[i:i+n]) == needle {
			return i
		}
	}
	return -1
}
