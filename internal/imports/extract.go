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

// MountExtractor 是 Extractor 的真实实现：通过挂载 ISO 文件到一个临时
// 挂载点、再用 os.DirFS 包装该挂载点，得到 fs.FS 视图。
//
// 选挂载而不是解压：挂载零磁盘占用（ISO 本身已经是完整的文件系统镜像），
// 解压需要额外的磁盘空间存一份解压后的副本，对几 GB 级别的 ISO 是不必要
// 的开销——真正需要落盘的产物（kernel/initrd/仓库树）由后续的抽取逻辑
// 选择性复制，不是把整个 ISO 无差别解压一遍。
//
// mount/umount 是操作系统命令而非本文件自己实现文件系统解析：ISO9660/
// UDF 格式的解析细节完全交给操作系统内核，这是最低风险的选择——手写
// 一个 ISO9660 解析器的正确性风险远大于调用系统已经充分测试过的挂载
// 支持。这也是为什么 MountExtractor 不参与任何单元测试（任务 9 要求
// 「不依赖真 ISO」），它只在集成环境里被真实使用，单元测试全部针对
// Extractor 接口的假实现。
type MountExtractor struct {
	// mountPointBase 是挂载点的父目录，真实挂载点会在其下创建一个以
	// ISO 内容摘要或时间戳命名的子目录。留空则使用系统临时目录。
	mountPointBase string
}

// NewMountExtractor 构造一个真实的挂载式抽取器。
func NewMountExtractor(mountPointBase string) *MountExtractor {
	return &MountExtractor{mountPointBase: mountPointBase}
}

// Open 挂载 isoPath 并返回其文件系统视图。
//
// 当前实现：本模块只交付「导入 job 使用抽取器接口」这个骨架，真实挂载
// 命令（mount -o loop）依赖具体运行环境的特权与工具链（Linux 下的
// mount(8)，在本仓库当前的 Windows 开发环境里连语义都不成立），且计划
// 任务 9/10/11 的测试要求全部通过内存文件系统验证、明确不依赖真 ISO。
// 因此 MountExtractor.Open 目前返回「未实现」错误，真正的 mount/umount
// 调用留给部署环境适配（不改变 Extractor 接口，只需替换本结构体的方法体）。
func (m *MountExtractor) Open(ctx context.Context, isoPath string) (fs.FS, error) {
	return nil, fmt.Errorf("imports: MountExtractor.Open 尚未实现真实挂载逻辑"+
		"（ISO=%q）——本模块交付的是抽取器接口与基于内存文件系统的识别/抽取逻辑，"+
		"真实的 mount(8) 调用依赖部署环境（Linux 特权、loop 设备），"+
		"留给部署适配阶段接入，不改变 Extractor 接口本身", isoPath)
}

// 编译期断言：MountExtractor 必须满足 Extractor。
var _ Extractor = (*MountExtractor)(nil)
