// 本文件定义抽取器接口：把一个本地 ISO 路径变成可遍历的文件系统视图。
// 对应实现计划任务 9，依据 SPEC §2.1。
//
// 用 fs.FS 而不是自定义接口（任务 9 明确要求）：标准库的抽象已经够用，
// 且 testing/fstest 直接提供内存实现——不需要自己写替身，测试不依赖
// 几 GB 的真实 ISO 下载。
package imports

import (
	"context"
	"fmt"
	"io/fs"
)

// Extractor 把一个本地 ISO 路径变成一个可遍历的文件系统视图（任务 9）。
//
// 真实实现（生产环境）用解压或挂载 ISO 文件；测试注入
// testing/fstest.MapFS——两者都满足 fs.FS，调用方（识别与抽取逻辑）不需要
// 关心具体是哪一种。
type Extractor interface {
	// Open 打开 isoPath 对应的 ISO，返回其内容的文件系统视图。
	// 视图的根目录对应 ISO 内容的根目录（不含任何额外的路径前缀）。
	Open(ctx context.Context, isoPath string) (fs.FS, error)
}

// MountExtractor 是 Extractor 的真实实现：直接在用户态解析 ISO 9660
// 镜像的字节内容，得到 fs.FS 视图（本轮 m3 轨实现，取代此前的骨架）。
//
// 为什么不是真的 mount(8)：读 ISO 内容不需要挂载——ISO 9660 是有公开
// 规范的文件系统格式（ECMA-119），可以纯用户态解析，不需要 Linux 特权
// 或 loop 设备。真的调用 mount(8) 在本仓库当前的 Windows 开发环境里
// 连语义都不成立，会导致这部分代码永远无法在本机验证（违反「完成前必须
// 真的执行验证」的铁律）。方向选择与论证的完整版本见
// iso9660.go 文件头注释与 docs/real-machine/17-iso读取实现.md。
//
// 名字仍叫 MountExtractor（而不是改名为 ISO9660Extractor 之类）：本任务
// 授权范围是「实现 MountExtractor.Open」，不改 Extractor 接口，改名字
// 会影响调用方的构造函数名（NewMountExtractor），那超出了「只写你拥有
// 的文件」的边界——cmd/lober 装配点如果已经引用了 NewMountExtractor
// 这个名字，改名就是破坏性变更。
type MountExtractor struct {
	// mountPointBase 是历史遗留字段：早期骨架设计假设需要一个挂载点
	// 父目录。当前实现（纯用户态解析）不需要任何挂载点，本字段不再被
	// Open 使用，但保留字段本身与 NewMountExtractor 的签名不变——删掉
	// 它会改变构造函数签名，任何现有调用方 NewMountExtractor(x) 都要
	// 跟着改，这超出「不得让任何现存测试变红」的授权（本包内的
	// TestExtract_MountExtractorNotYetImplemented 就是这样一个现有
	// 调用方，见 extract_test.go）。
	mountPointBase string
}

// NewMountExtractor 构造一个真实的 ISO 9660 抽取器。
func NewMountExtractor(mountPointBase string) *MountExtractor {
	return &MountExtractor{mountPointBase: mountPointBase}
}

// Open 打开 isoPath 对应的本地文件，解析其 ISO 9660 内容，返回文件系统
// 视图。
//
// 用 OpenISO9660File 而不是「os.Open 一次拿到 *os.File，把它整个生命
// 周期传给 OpenISO9660」：本函数不持有跨越整个 fs.FS 生命周期的文件
// 描述符（完整论证见下方 OpenISO9660File 调用处的注释与
// iso9660.go 里 OpenISO9660File 的文档）。同时，真实发行版 ISO 动辄
// 几 GB，OpenISO9660 内部通过 io.ReaderAt 做扇区级随机读取，不要求
// （也不应该）把整份 ISO 一次性读进内存。
func (m *MountExtractor) Open(ctx context.Context, isoPath string) (fs.FS, error) {
	// OpenISO9660File 本身不接受 ctx（它是纯同步的字节解析，没有可取消的
	// IO 等待点——每次 ReadAt 都是对本地文件的一次性开-读-关，不是网络
	// 调用），因此本函数只在解析开始前检查一次 ctx 是否已经被取消，不在
	// 解析过程中反复检查：调用方若在 Job.Run 尚未真正开始抽取前就取消了
	// ctx，这里能省掉一次注定失败的解析；若在解析过程中取消，本函数仍会
	// 解析完成才返回——真实 ISO 目录树的解析耗时是毫秒级（只解析目录
	// 记录，不读文件内容），这个不可中断的窗口远小于后续抽取步骤（复制
	// 内核/initrd/仓库树，那部分同样不接受 ctx，见 manifest.go 的
	// copyTreeFromFS），保持与既有代码同样的取消粒度。
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("imports: 打开 ISO 前 context 已取消: %w", err)
	}

	// 用 OpenISO9660File 而不是「os.Open 一次、把 *os.File 传给
	// OpenISO9660」：后者会让返回的 fs.FS 在整个生命周期内占着一个操作
	// 系统文件描述符，而 Extractor 接口没有对应的 Close 方法、job.go
	// （本任务不拥有）也从未调用任何清理——这在 Windows 下会直接表现为
	// 「文件正在被使用，无法删除/移动」（本包的
	// TestExtract_MountExtractorReadsRealISO9660File 在开发过程中就
	// 真实触发过这个问题：t.TempDir() 清理时报
	// ERROR_SHARING_VIOLATION）。OpenISO9660File 按需开关文件，不持有
	// 跨越整个 fs.FS 生命周期的文件描述符，完整论证见 iso9660.go 里
	// OpenISO9660File 的文档注释。
	fsys, err := OpenISO9660File(isoPath)
	if err != nil {
		return nil, fmt.Errorf("imports: 解析 ISO 9660 内容失败（路径 %q): %w", isoPath, err)
	}

	return fsys, nil
}

// 编译期断言：MountExtractor 必须满足 Extractor。
var _ Extractor = (*MountExtractor)(nil)
