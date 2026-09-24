// 本文件实现一个最小的、只读的 ISO 9660 文件系统解析器（对应本轮 m3 轨
// 任务：给 MountExtractor.Open 接上真实的 ISO 内容读取）。
//
// 为什么不真的 mount：读 ISO 内容不需要挂载——ISO 9660 是一个有公开规范
// （ECMA-119，等同 ISO/IEC 9660:1988）的文件系统格式，可以纯用户态解析，
// 不需要 Linux 特权或 loop 设备。本仓库当前所有端到端脚本都在 Windows 与
// Linux 两边跑（scripts/e2e*.sh），一个依赖 mount(8) 的实现在本机（Windows
// 开发环境）永远无法验证，这本身就违反了"完成前必须真的执行验证"的铁律。
//
// 为什么不调外部工具：本项目的依赖白名单不允许新增 go.mod 依赖，而调用
// 7z/bsdtar/isoinfo 这类外部二进制会在气隙部署环境里引入"这个工具是否
// 已安装"的新前提条件，且引入了 os/exec 之外的运行时依赖。纯 Go 实现零
// 外部依赖、可在任何平台单测、结果确定性高（同一份 ISO 字节流两次解析
// 结果完全一致，不受本机安装了哪个版本的 isoinfo 影响）。
//
// 只支持只读：本模块的全部使用场景（Identify、ExtractRHEL/Debian/Ubuntu/
// SUSE/ESXi/Windows）都只读 ISO 内容，从未写入，因此没有实现任何写路径。
//
// 为什么两个扩展都要支持（而不是只选一个）：本模块要读取的特征路径
// 分成两类，各自依赖不同的扩展才能被精确匹配到——
//
//   - Linux 系发行版（RHEL/Debian/Ubuntu/SUSE/ESXi）的特征路径含前导点
//     或要求保留小写（.treeinfo、.disk/info、dists/、repodata/repomd.xml、
//     boot.cfg、mboot.efi 等）：这些 ISO 的制作工具（mkisofs/genisoimage/
//     xorriso）惯例上启用 Rock Ridge（RRIP）而不是 Joliet 来保留真实
//     文件名——纯 ISO 9660 层会把文件名强制大写、做 8.3 截断、加 ";1"
//     版本号后缀（".treeinfo" 这类以点开头的名字在 8.3 规则下的表现
//     尤其糟糕：点前的主文件名部分是空字符串），与 identify.go 里写死
//     的精确小写路径不匹配。
//   - Windows 安装 ISO 的特征路径（sources/install.wim）：这类 ISO 惯例
//     上启用 Joliet 而不是 Rock Ridge。
//
// 支持的扩展：
//   - Joliet（补充卷描述符，转义序列 %/@、%/C、%/E）：完整支持，解析
//     UCS-2/UTF-16BE 编码的长文件名，不做 8.3 截断、不强制大写。存在
//     Joliet SVD 时，本解析器整棵目录树优先用它（见 OpenISO9660）。
//   - Rock Ridge（RRIP）：只支持 NM（备用名字）系统使用字段
//     （parseRockRidgeNM），这是恢复原始大小写/前导点文件名所需的最小
//     子集。**明确不支持**的部分：
//     -- CE 延续区（一个 NM 名字若长到系统使用区本身放不下、需要跳到
//     另一个 extent 继续），本解析器只读系统使用区内能直接拿到的
//     片段，不跟随 CE 指针，长名字会被截断（真实世界的这几个特征
//     文件名都很短，不会触发这个限制，但若某天要读取一个几十字符
//     的深层路径就可能受影响）。
//     -- PX（POSIX 权限位/属主）、SL（符号链接）、CL/PL（重定位目录）
//     等其它 RRIP 字段一律不解析——本模块的全部调用方
//     （Identify/ExtractXxx）只需要"文件在哪、多大、是不是目录"
//     三件事，不需要 Unix 权限位，也不需要跟随符号链接。
package imports

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// iso9660SectorSize 是 ISO 9660 固定的逻辑扇区大小（ECMA-119 §6.1.2：
// "the Logical Block Size shall be 2048"，几乎所有 ISO 9660 镜像的逻辑块
// 大小都是这个值，本解析器不支持其它块大小——真实世界的 ISO 分发镜像
// （RHEL/Debian/Ubuntu/SUSE/ESXi/Windows 安装源）无一例外使用 2048）。
const iso9660SectorSize = 2048

// minDirRecordLen 是一条目录记录的定长部分字节数（ECMA-119 §9.1：偏移 0 到 32
// 的固定字段加上 1 字节文件名，最短的合法记录就是 "." 与 ".." 那两条）。
//
// 它同时是**扇区尾部填充的判据**：剩余字节少于这个数时，那段尾巴必然是填充而
// 不是一条记录（记录不得跨逻辑块，§6.8.1）。见 scanDirectorySector。
const minDirRecordLen = 34

// iso9660SystemAreaSectors 是系统区占用的扇区数（ECMA-119 §6.2.1：
// 第 0-15 扇区保留给系统区，不属于 ISO 9660 数据结构本身），卷描述符序列
// 从第 16 扇区开始。
const iso9660SystemAreaSectors = 16

// iso9660StdIdentifier 是每个卷描述符头 5 字节固定的标准标识符
// （ECMA-119 §8.1.2："CD001"）。
const iso9660StdIdentifier = "CD001"

// 卷描述符类型码（ECMA-119 §8.1.1）。
const (
	iso9660VolDescPrimary       = 1
	iso9660VolDescSupplementary = 2
	iso9660VolDescTerminator    = 255
)

// iso9660DirFlagDirectory 是目录记录 Flags 字段（第 25 字节，ECMA-119
// §9.1.6）里标记"这是一个目录"的位（bit 1）。
const iso9660DirFlagDirectory = 0x02

// jolietEscapeSequences 是补充卷描述符标识 Joliet 的三个转义序列
// （ECMA-119 附录无此内容，是 Joliet 规范自身定义的事实标准，位于卷描述符
// 第 89-90 字节，即偏移 88-89，对应 UCS-2 Level 1/2/3）：
//   - "%/@" → Level 1（仅 UCS-2 中的 ASCII 兼容子集）
//   - "%/C" → Level 2
//   - "%/E" → Level 3（最常见，Windows 与市面工具默认用这一级）
//
// 三者对本解析器的处理方式完全一致（都是"这是 Joliet 用 UCS-2/UTF-16BE
// 编码的目录树"），本模块不区分级别本身的差异（级别只影响允许的字符集，
// 不影响本模块关心的目录结构与文件内容读取）。
var jolietEscapeSequences = [][]byte{
	[]byte("%/@"),
	[]byte("%/C"),
	[]byte("%/E"),
}

// ISO9660FS 是解析完成后的 ISO 9660 文件系统视图，实现 fs.FS（含
// fs.StatFS、fs.ReadDirFS 以获得 fs.Stat/fs.ReadDir/fs.WalkDir 的高效
// 支持，避免它们退化到"打开再读全部内容"的慢路径）。
//
// 整棵目录树在 OpenISO9660 时一次性递归解析进内存（entries 字段），不是
// 惰性按需解析：ISO 目录记录本身很小（每个文件的记录也就几十字节），
// 而发行版 ISO 的文件数量在千级，一次性解析进内存的开销远小于反复打开
// 底层 ReaderAt 做多次 seek+read；这个决定也让 fs.WalkDir 与 identify.go
// 里的多次 fs.Stat 查询都变成纯内存操作，不必每次都重新遍历目录扇区。
type ISO9660FS struct {
	entries map[string]*iso9660Entry // 键为规范化后的相对路径（不含开头斜杠，"." 表示根）
	reader  io.ReaderAt              // 底层 ISO 数据源，文件内容的实际字节读取都经它发起
}

// iso9660Entry 是内存目录树里的一个节点。
type iso9660Entry struct {
	name    string // 本节点的名字（不含路径前缀），根节点为 "."
	isDir   bool
	size    int64
	extent  uint32 // 数据区起始逻辑块号（目录节点：其目录记录所在的块；文件节点：文件内容起始块）
	modTime time.Time
	// children 只在 isDir 为 true 时有意义，键为子节点名字。
	children map[string]*iso9660Entry
}

// OpenISO9660 解析 r 中的 ISO 9660 镜像并返回其文件系统视图。
//
// r 必须支持 ReadAt（本模块用它做扇区级随机读取，不要求整份 ISO 能一次性
// 读进内存——真实发行版 ISO 动辄几 GB，io.ReaderAt 让实现只在需要某个
// 目录/文件的字节范围时才发起对应的读取）。
//
// 解析失败（魔数不对、扇区越界、目录记录截断）均返回明确错误，不会
// panic——ISO 文件损坏或路径写错都是可预期的运行期状况，不是编程错误。
func OpenISO9660(r io.ReaderAt) (*ISO9660FS, error) {
	pvd, joliet, err := readVolumeDescriptors(r)
	if err != nil {
		return nil, err
	}

	// 优先用 Joliet：若存在，其目录树携带原始大小写与长文件名，与
	// identify.go/extract_*.go 里写死的精确路径字符串匹配（见文件头注释）。
	chosen := pvd
	if joliet != nil {
		chosen = *joliet
	}

	root := &iso9660Entry{name: ".", isDir: true, children: map[string]*iso9660Entry{}}
	if err := readDirectoryTree(r, chosen.rootExtent, chosen.rootSize, root, chosen.useUTF16, 0); err != nil {
		return nil, fmt.Errorf("imports: 解析 ISO 9660 目录树失败: %w", err)
	}

	entries := map[string]*iso9660Entry{}
	flattenEntries(root, ".", entries)

	return &ISO9660FS{entries: entries, reader: r}, nil
}

// OpenISO9660File 是 OpenISO9660 面向真实本地文件的包装：不持有一个
// 长期打开的 *os.File，而是用 perCallOpenReaderAt（本文件下方定义）按需
// 打开、读取、立即关闭——每次 io.ReaderAt.ReadAt 调用对应一次独立的
// os.Open/Close。
//
// 为什么不直接 os.Open 一次再把 *os.File 传给 OpenISO9660：那样得到的
// ISO9660FS 会在整个生命周期内占着一个操作系统文件描述符，而调用方
// （job.go 的 Run，本任务不拥有、不能修改）从未调用任何 Close——
// Extractor 接口的 Open 方法返回裸 fs.FS，这个接口没有对应的 Close，
// 调用方也没有机会调用。真实测试已经暴露了这个问题：Windows 下持有
// 打开的文件句柄会阻止 t.TempDir() 的清理删除该文件（"进程正在使用该
// 文件，因此无法访问"），这不是测试环境的偶然限制，而是 Windows 文件
// 锁语义的真实体现——生产环境下同样的问题会表现为文件描述符持续增长
// （每次导入 job 泄漏一个），或者运维想在导入完成后删除/移动那份 ISO
// 文件时被意外拒绝。
//
// 按需开关文件的开销（每次内容读取多一次系统调用）远小于持有泄漏
// 描述符的风险——识别与抽取阶段对同一个文件的读取次数在十到百级
// （目录记录扫描 + 少数几个特征文件/内核/initrd/仓库树里的文件），不是
// 高频路径，这个额外开销可以接受。
func OpenISO9660File(path string) (*ISO9660FS, error) {
	return OpenISO9660(&perCallOpenReaderAt{path: path})
}

// perCallOpenReaderAt 是一个 io.ReaderAt：每次 ReadAt 调用独立完成
// 「打开文件 → seek 到指定偏移 → 读取 → 关闭文件」，不持有跨调用的文件
// 描述符（OpenISO9660File 的文档说明了引入它的理由）。
type perCallOpenReaderAt struct {
	path string
}

func (p *perCallOpenReaderAt) ReadAt(buf []byte, off int64) (int, error) {
	f, err := os.Open(p.path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	return f.ReadAt(buf, off)
}

// volDescInfo 是从一个卷描述符里提取出的、后续目录树解析需要的字段。
type volDescInfo struct {
	rootExtent uint32
	rootSize   uint32
	useUTF16   bool // true 表示这是 Joliet SVD，目录记录里的文件名要按 UTF-16BE 解码
}

// readVolumeDescriptors 从第 16 扇区开始顺序读取卷描述符，返回主卷描述符
// （必须存在，否则报错）与可选的 Joliet 补充卷描述符（不存在则为 nil）。
func readVolumeDescriptors(r io.ReaderAt) (pvd volDescInfo, joliet *volDescInfo, err error) {
	var havePVD bool

	for sector := iso9660SystemAreaSectors; ; sector++ {
		buf := make([]byte, iso9660SectorSize)
		if _, readErr := r.ReadAt(buf, int64(sector)*iso9660SectorSize); readErr != nil {
			return volDescInfo{}, nil, fmt.Errorf(
				"imports: 读取卷描述符扇区 %d 失败（%w）——ISO 在到达卷描述符终止符之前"+
					"就结束了，这不是一份完整的 ISO 9660 镜像", sector, readErr)
		}

		if string(buf[1:6]) != iso9660StdIdentifier {
			return volDescInfo{}, nil, fmt.Errorf(
				"imports: 扇区 %d 的标准标识符不是 %q（读到 %q）——"+
					"这不是一份有效的 ISO 9660 镜像，或文件已损坏/截断", sector, iso9660StdIdentifier, buf[1:6])
		}

		descType := buf[0]
		switch descType {
		case iso9660VolDescTerminator:
			if !havePVD {
				return volDescInfo{}, nil, fmt.Errorf(
					"imports: 到达卷描述符终止符（扇区 %d）之前没有找到主卷描述符——"+
						"这不是一份有效的 ISO 9660 镜像", sector)
			}
			return pvd, joliet, nil

		case iso9660VolDescPrimary:
			info, parseErr := parseVolumeDescriptorFields(buf, false)
			if parseErr != nil {
				return volDescInfo{}, nil, fmt.Errorf("imports: 解析主卷描述符失败: %w", parseErr)
			}
			pvd = info
			havePVD = true

		case iso9660VolDescSupplementary:
			if isJolietEscapeSequence(buf[88:91]) {
				info, parseErr := parseVolumeDescriptorFields(buf, true)
				if parseErr != nil {
					return volDescInfo{}, nil, fmt.Errorf("imports: 解析 Joliet 补充卷描述符失败: %w", parseErr)
				}
				joliet = &info
			}
			// 非 Joliet 的补充卷描述符（比如 UDF 桥接卷）本模块不关心，跳过。

		default:
			// 其它类型（如启动记录）不影响文件内容读取，跳过。
		}
	}
}

// isJolietEscapeSequence 判定 seq（补充卷描述符偏移 88-90 的三字节，
// Escape Sequences 字段的前 3 字节）是否精确匹配 jolietEscapeSequences 中
// 的某一个转义序列（"%/@"、"%/C"、"%/E" 之一）。
func isJolietEscapeSequence(seq []byte) bool {
	for _, esc := range jolietEscapeSequences {
		if len(seq) >= len(esc) && string(seq[:len(esc)]) == string(esc) {
			return true
		}
	}
	return false
}

// parseVolumeDescriptorFields 从卷描述符扇区字节里提取根目录记录的
// extent/size 两个字段（ECMA-119 §8.4/§8.5：主卷描述符与补充卷描述符
// 的字段布局在本模块关心的这几个字段上完全一致，唯一区别是补充卷描述符
// 需要标记 useUTF16）。
//
// 根目录记录本身内嵌在卷描述符的第 157-190 字节（偏移 156，长度 34，
// ECMA-119 §8.4.14），本函数直接把这段字节交给 parseDirectoryRecord。
func parseVolumeDescriptorFields(buf []byte, useUTF16 bool) (volDescInfo, error) {
	if len(buf) < 190 {
		return volDescInfo{}, errors.New("卷描述符扇区长度不足，无法读取根目录记录")
	}
	rec, _, err := parseDirectoryRecord(buf[156:190])
	if err != nil {
		return volDescInfo{}, fmt.Errorf("解析根目录记录失败: %w", err)
	}
	return volDescInfo{rootExtent: rec.extent, rootSize: rec.size, useUTF16: useUTF16}, nil
}

// dirRecordFields 是从一条原始目录记录字节里解出的字段（ECMA-119 §9.1）。
type dirRecordFields struct {
	extent  uint32
	size    uint32
	isDir   bool
	name    string
	dateRaw [7]byte
	// rockRidgeName 是从系统使用区里的 Rock Ridge NM 字段解出的备用名字
	// （非空表示存在），用于保留 Linux 发行版 ISO 里区分大小写、含前导点
	// 的真实文件名（如 .treeinfo、.disk）——这些名字在纯 ISO 9660 层会被
	// 强制大写并做 8.3 截断，Rock Ridge 的 NM 字段就是为了绕过这个限制而
	// 存在（RRIP §4.1.4）。本模块只解析 NM 字段本身，不解析 CE 延续区
	// （见 finalizeEntryName 附近的说明与文件头注释的支持范围声明）。
	rockRidgeName string
}

// parseDirectoryRecord 解析一条目录记录，返回解出的字段与该记录在输入
// 切片中占用的总长度（第 0 字节，Length of Directory Record）。
//
// 目录记录布局（ECMA-119 §9.1，本模块只读小端字段，跳过大端冗余字段）：
//
//	偏移 0      : 记录长度（1 字节）
//	偏移 2-5    : Extent 起始逻辑块号（小端 32 位，偏移 6-9 是同值大端，跳过）
//	偏移 10-13  : 数据长度（小端 32 位，偏移 14-17 同值大端，跳过）
//	偏移 18-24  : 记录日期时间（7 字节）
//	偏移 25     : Flags
//	偏移 32     : 文件名长度（1 字节）
//	偏移 33...  : 文件名本体（长度见上一字段）
//
// 文件名之后可能有一个填充字节（若文件名长度为偶数，ECMA-119 §9.1.12
// 要求记录总长为偶数）与系统使用区（Rock Ridge 等扩展字段所在，本模块
// 不解析，直接按记录总长跳过）。
func parseDirectoryRecord(buf []byte) (dirRecordFields, int, error) {
	if len(buf) < minDirRecordLen {
		// 走到这里说明调用方传进了一段短于一条记录的尾巴。**扇区尾部的填充
		// 不该走到这里**——那是 scanDirectorySector 的循环条件负责挡掉的正常
		// 情况（见那个函数的文档）。因此这里保留报错：它现在的含义是
		// 「调用方违反了本函数的契约」，而不是「ISO 坏了」。
		return dirRecordFields{}, 0, fmt.Errorf(
			"目录记录长度 %d 字节小于最小定长部分 %d 字节——调用方应当先确认"+
				"剩余空间足够一条记录（扇区尾部的填充由 scanDirectorySector 负责跳过）",
			len(buf), minDirRecordLen)
	}

	recLen := int(buf[0])
	if recLen == 0 {
		// 记录长度为 0 是 ECMA-119 §6.8.1.1 明确定义的"本扇区剩余部分是
		// 填充，下一条记录从下一扇区开始"的哨兵值，不是错误。调用方
		// （readDirectoryTree）据此换到下一扇区，本函数把这个信号原样
		// 透传出去（recLen=0），不在此处报错。
		return dirRecordFields{}, 0, nil
	}
	if recLen < 34 || recLen > len(buf) {
		return dirRecordFields{}, 0, fmt.Errorf(
			"目录记录声明长度 %d 超出可用字节数 %d 或小于最小值 34——ISO 已截断或损坏",
			recLen, len(buf))
	}

	nameLen := int(buf[32])
	if 33+nameLen > recLen {
		return dirRecordFields{}, 0, fmt.Errorf(
			"目录记录文件名长度 %d 超出记录总长 %d 的剩余空间——ISO 已截断或损坏", nameLen, recLen)
	}

	rec := dirRecordFields{
		extent: le32(buf[2:6]),
		size:   le32(buf[10:14]),
		isDir:  buf[25]&iso9660DirFlagDirectory != 0,
	}
	copy(rec.dateRaw[:], buf[18:25])

	rawName := buf[33 : 33+nameLen]
	rec.name = decodeDirRecordName(rawName)

	// 系统使用区紧跟在文件名之后：文件名若为偶数长度，ECMA-119 §9.1.12
	// 要求再补 1 字节填充使字段整体长度为偶数；系统使用区从这之后开始，
	// 一直延伸到记录末尾（recLen）。Rock Ridge 的 NM 字段（若存在）就在
	// 这段区域里，本函数扫描它来找一个可能存在的"真实文件名"。
	suspStart := 33 + nameLen
	if nameLen%2 == 0 {
		suspStart++
	}
	if suspStart < recLen {
		rec.rockRidgeName = parseRockRidgeNM(buf[suspStart:recLen])
	}

	return rec, recLen, nil
}

// rockRidgeSignatureNM 是 Rock Ridge Interchange Protocol 定义的"备用
// 名字"系统使用字段的两字节签名（RRIP §4.1.4）。
const rockRidgeSignatureNM = "NM"

// parseRockRidgeNM 在系统使用区字节里扫描 SUSP 格式的条目
// （签名 2 字节 + 长度 1 字节 + 版本 1 字节 + 载荷），寻找签名为 "NM" 的
// 条目并返回其携带的名字片段。
//
// 只处理不需要 CE 延续区的情况（NM 条目本身携带的名字若跨越多个 NM
// 条目分片，本函数会把各片段按出现顺序拼接——RRIP §4.1.4 允许一个逻辑
// 名字由多个 NM 条目组成，用 flags 的 CONTINUE 位（0x01）标记"后面还有
// 下一片"；本函数据此拼接，但如果 NM 条目本身指向系统使用区之外的 CE
// 延续区（另一个 extent），本函数不会跟随过去——那种情况下返回已拼接
// 的部分名字，调用方按"能拿到多少算多少"处理，不视为错误（真实世界的
// 短文件名极少需要 CE 延续区，需要它的场景是文件名长到系统使用区本身
// 放不下，这不是本模块要覆盖的 ISO 特征文件路径的典型情况）。
func parseRockRidgeNM(susp []byte) string {
	var name strings.Builder
	found := false

	for len(susp) >= 4 {
		sig := string(susp[0:2])
		entryLen := int(susp[2])
		if entryLen < 4 || entryLen > len(susp) {
			break // 条目长度字段本身不合理，系统使用区已到末尾或损坏，停止扫描
		}

		if sig == rockRidgeSignatureNM {
			flags := susp[4]
			payload := susp[5:entryLen]
			name.Write(payload)
			found = true
			if flags&0x01 == 0 { // CONTINUE 位未设置，名字到此结束
				break
			}
		}

		susp = susp[entryLen:]
	}

	if !found {
		return ""
	}
	return name.String()
}

// decodeDirRecordName 把目录记录里的原始文件名字节还原成本模块使用的
// 名字字符串。特殊值 0x00 与 0x01（各恰好 1 字节）分别代表"." 与".."
// （ECMA-119 §9.1.11），本模块把它们原样识别但不放进目录树（当前目录/
// 父目录记录会被 readDirectoryTree 跳过，不产出对应的树节点）。
//
// 名字的字节编码本身（ASCII 还是 UTF-16BE）由调用方按"这棵树是不是
// Joliet 树"决定，本函数只负责剥离版本号后缀（";1" 等，纯 ISO 9660
// 规范要求所有文件名带版本号，ECMA-119 §7.5，Joliet 树里同样保留这个
// 约定）——调用方在得到这个"剥完版本号但字节尚未按编码解码"的结果后，
// 若是 Joliet 树还需要额外调用 decodeUTF16BE。
func decodeDirRecordName(raw []byte) string {
	if len(raw) == 1 && raw[0] == 0x00 {
		return "."
	}
	if len(raw) == 1 && raw[0] == 0x01 {
		return ".."
	}
	return string(raw)
}

// stripVersionSuffixASCII 剥离形如 "NAME.EXT;1" 里的 ";1" 版本号后缀
// （ASCII/纯 ISO 9660 文件名场景）。
func stripVersionSuffixASCII(name string) string {
	if idx := strings.LastIndexByte(name, ';'); idx >= 0 {
		return name[:idx]
	}
	return name
}

// stripVersionSuffixUTF16BE 在原始字节（尚未解码）层面剥离 Joliet 文件名
// 的版本号后缀。Joliet 文件名用 UTF-16BE 编码，";1" 这四个 ASCII 字符
// 在 UTF-16BE 里是 "\x00;\x00 1"（每个字符占 2 字节，高位字节为
// 0x00——分号与数字 1 都在 Basic Latin 范围内）。用字节而非先解码再找
// 分号，是因为分号本身就是 ASCII 字符，在 UTF-16BE 大端序里第二字节
// 才是它的码点值，按 2 字节步长扫描比先整体解码再用字符串查找更直接。
func stripVersionSuffixUTF16BE(raw []byte) []byte {
	for i := 0; i+1 < len(raw); i += 2 {
		if raw[i] == 0x00 && raw[i+1] == ';' {
			return raw[:i]
		}
	}
	return raw
}

// decodeUTF16BE 把 Joliet 目录记录里的原始 UTF-16BE 字节解码成 Go
// 字符串。
func decodeUTF16BE(raw []byte) (string, error) {
	if len(raw)%2 != 0 {
		return "", fmt.Errorf("UTF-16BE 文件名字节数 %d 不是偶数——记录已损坏", len(raw))
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = uint16(raw[2*i])<<8 | uint16(raw[2*i+1])
	}
	return string(utf16.Decode(units)), nil
}

// le32 把 4 字节小端切片解码为 uint32。调用方保证切片长度至少为 4。
func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// decodeRecordDate 把目录记录里的 7 字节日期时间字段（ECMA-119 §9.1.5）
// 解码为 time.Time。字段布局：年（自 1900 起偏移量）、月、日、时、分、秒、
// GMT 偏移量（15 分钟为单位，本模块忽略时区偏移，一律当作 UTC——manifest
// 与 identify 都不依赖精确时区，只是给 fs.FileInfo.ModTime 一个合理值）。
//
// 全零字段是合法的"未指定日期"（一些生成器会这样写），此时返回零值
// time.Time，调用方（iso9660Entry 的使用者）不因此报错。
func decodeRecordDate(raw [7]byte) time.Time {
	if raw == [7]byte{} {
		return time.Time{}
	}
	year := 1900 + int(raw[0])
	month := time.Month(raw[1])
	day := int(raw[2])
	hour := int(raw[3])
	minute := int(raw[4])
	second := int(raw[5])
	if month < 1 || month > 12 || day < 1 {
		return time.Time{}
	}
	return time.Date(year, month, day, hour, minute, second, 0, time.UTC)
}

// readDirectoryTree 从 extent 指向的目录数据区开始，递归解析目录记录，
// 把结果填进 parent.children。
//
// depth 用于防止畸形 ISO 里目录记录互相指向导致的无限递归（比如损坏的
// ISO 里子目录的 extent 意外指回父目录或自身）——真实 ISO 9660 目录嵌套
// 深度受 8 级限制（不含 Rock Ridge 深层路径扩展，ECMA-119 §6.8.2.1），
// 本模块设置一个远宽松于该限制但仍是有限值的上限，超过就报错而不是
// 无限循环耗尽内存/栈空间（畸形/截断的 ISO 报明确错误、不 panic 的要求
// 里，"不无限循环"同样是必须满足的一部分——虽然它不是 panic，但对调用方
// 而言"进程挂起不返回"与 panic 一样是不可接受的失败模式）。
const iso9660MaxDirDepth = 64

func readDirectoryTree(r io.ReaderAt, extent, size uint32, parent *iso9660Entry, useUTF16 bool, depth int) error {
	if depth > iso9660MaxDirDepth {
		return fmt.Errorf(
			"imports: 目录嵌套深度超过 %d 层——ISO 可能已损坏（目录记录形成了循环引用），"+
				"拒绝继续递归以避免无限循环", iso9660MaxDirDepth)
	}

	data, err := readExtent(r, extent, size)
	if err != nil {
		return fmt.Errorf("读取目录数据区（extent=%d, size=%d）失败: %w", extent, size, err)
	}

	// 目录数据区由若干个扇区组成，每个扇区内的记录不跨扇区边界
	// （ECMA-119 §6.8.1：一条记录必须完整落在单个逻辑块内），因此逐扇区
	// 扫描，扇区内 offset 归零重新开始，而不是把整个 data 当成一条连续
	// 字节流线性扫描到底。
	for sectorStart := 0; sectorStart < len(data); sectorStart += iso9660SectorSize {
		sectorEnd := sectorStart + iso9660SectorSize
		if sectorEnd > len(data) {
			sectorEnd = len(data)
		}
		recs, err := scanDirectorySector(data[sectorStart:sectorEnd])
		if err != nil {
			return fmt.Errorf("扫描目录扇区失败（数据区内偏移 %d）: %w", sectorStart, err)
		}
		for _, rec := range recs {
			if rec.name == "." || rec.name == ".." {
				continue
			}
			if err := addChildEntry(r, rec, parent, useUTF16, depth); err != nil {
				return err
			}
		}
	}

	return nil
}

// scanDirectorySector 扫描单个目录扇区，返回其中的全部目录记录。
//
// 两个收尾条件，缺一个都会在真实 ISO 上出问题：
//
//  1. **长度字节为 0**：ECMA-119 §6.8.1.1 显式定义的「本扇区剩余是填充」哨兵。
//  2. **剩余字节不足一条记录的定长部分（34 字节）**：目录记录不得跨逻辑块
//     （§6.8.1），因此扇区尾部凑不满一条记录的那几个字节必然是填充。
//
// **第 2 条曾经缺失，那是一个在真实 ISO 上必然触发的阻断性 bug。** 扫描循环
// 原本的条件是 offset < len(sector)，于是尾部只剩 18 字节时仍去解析，撞上
// parseDirectoryRecord 的「len(buf) < 34 即报错」防御检查，整个导入以
// 「ISO 已截断或损坏」告终——而那 18 字节是完全正常的填充。
//
// 它此前没被发现，是因为全部导入测试用的都是合成镜像（buildISO9660 恰好没造出
// 尾部落在 1..33 字节这个区间的扇区）或 fstest.MapFS，而 e2e 脚本一律绕过导入
// 直接写库。**真 ISO 从未走过这条路**，直到对着一份 OpenCloudOS 9.6 镜像跑
// TestRealISO_OpenAndIdentify 才暴露出来。
//
// 后果的严重性：导入是全部六个 OS 家族装机链路的第一步，这一步失败则一台机器
// 都装不出来。
//
// 真正的损坏仍然拒绝：一条记录声明的长度超出扇区剩余空间时返回错误，不静默
// 截断——那与「尾部填充」是两回事，放行会让一棵读错的目录树被当成正常结果，
// 症状是后续找不到某个特征文件，而原因在更下面一层。
func scanDirectorySector(sector []byte) ([]dirRecordFields, error) {
	var recs []dirRecordFields

	// 循环条件是 offset+minDirRecordLen <= len(sector) 而不是 offset < len(sector)：
	// 后者会把尾部那几个字节的填充当成一条待解析的记录。
	for offset := 0; offset+minDirRecordLen <= len(sector); {
		rec, recLen, err := parseDirectoryRecord(sector[offset:])
		if err != nil {
			return nil, fmt.Errorf("解析目录记录失败（扇区内偏移 %d）: %w", offset, err)
		}
		if recLen == 0 {
			break // 长度 0 哨兵：本扇区剩余部分是填充
		}
		recs = append(recs, rec)
		offset += recLen
	}

	return recs, nil
}

// addChildEntry 把一条已解析的目录记录转换为 iso9660Entry 并挂到
// parent.children 下；若是子目录，递归解析其内容。
func addChildEntry(r io.ReaderAt, rec dirRecordFields, parent *iso9660Entry, useUTF16 bool, depth int) error {
	name, err := finalizeEntryName(rec, useUTF16)
	if err != nil {
		return fmt.Errorf("解析文件名失败（extent=%d): %w", rec.extent, err)
	}
	if name == "" {
		// 剥离版本号后清空的名字（理论上不应发生，防御性跳过而不是
		// 产出一个空名字节点污染目录树）。
		return nil
	}

	entry := &iso9660Entry{
		name:    name,
		isDir:   rec.isDir,
		size:    int64(rec.size),
		extent:  rec.extent,
		modTime: decodeRecordDate(rec.dateRaw),
	}

	if rec.isDir {
		entry.children = map[string]*iso9660Entry{}
		if err := readDirectoryTree(r, rec.extent, rec.size, entry, useUTF16, depth+1); err != nil {
			return err
		}
	}

	parent.children[name] = entry
	return nil
}

// finalizeEntryName 决定一条目录记录的最终名字，优先级：
//  1. Rock Ridge NM 字段（rec.rockRidgeName 非空时）——保留原始大小写、
//     不做 8.3 截断、不带版本号后缀，这是 RRIP 存在的目的。Joliet 树
//     不会出现 Rock Ridge 系统使用字段（两者是互斥的扩展惯例，市面 ISO
//     从不同时对同一棵树应用两者），因此这个优先级只在 useUTF16=false
//     时可能命中，但本函数不假设这一点、而是直接检查字段是否非空——
//     即使某天出现同时携带两者的 ISO，Rock Ridge 名字仍应优先，因为它
//     比 Joliet 更贴近"文件在源系统上的真实名字"。
//  2. 否则按 useUTF16 走原有逻辑：剥离版本号后缀，Joliet 树额外解码
//     UTF-16BE。
func finalizeEntryName(rec dirRecordFields, useUTF16 bool) (string, error) {
	if rec.rockRidgeName != "" {
		return rec.rockRidgeName, nil
	}
	if !useUTF16 {
		return stripVersionSuffixASCII(rec.name), nil
	}
	stripped := stripVersionSuffixUTF16BE([]byte(rec.name))
	return decodeUTF16BE(stripped)
}

// readExtent 读取从逻辑块 extent 开始、长度为 size 字节的数据。
//
// 按 size 向上取整到扇区边界读取（目录数据区的实际占用总是整数个扇区，
// ECMA-119 对目录记录的排布要求），文件内容则精确按 size 截断——本函数
// 统一按"扇区对齐读取后按 size 截断"处理，两种调用场景（目录树解析、
// 文件内容读取）共用同一套越界检查逻辑。
func readExtent(r io.ReaderAt, extent, size uint32) ([]byte, error) {
	if size == 0 {
		return nil, nil
	}
	sectors := (int64(size) + iso9660SectorSize - 1) / iso9660SectorSize
	buf := make([]byte, sectors*iso9660SectorSize)
	off := int64(extent) * iso9660SectorSize
	n, err := r.ReadAt(buf, off)
	if err != nil && !(err == io.EOF && int64(n) >= int64(size)) {
		return nil, fmt.Errorf(
			"读取 extent=%d（偏移 %d，%d 字节）失败: %w——ISO 可能已截断", extent, off, len(buf), err)
	}
	if int64(size) > int64(len(buf)) {
		return buf, nil
	}
	return buf[:size], nil
}

// flattenEntries 把递归目录树摊平成"规范化相对路径 → 节点"的映射，供
// fs.FS 实现直接按路径查找，不必每次都从根开始逐段下降。
func flattenEntries(entry *iso9660Entry, entryPath string, out map[string]*iso9660Entry) {
	out[entryPath] = entry
	if !entry.isDir {
		return
	}
	for name, child := range entry.children {
		childPath := name
		if entryPath != "." {
			childPath = entryPath + "/" + name
		}
		flattenEntries(child, childPath, out)
	}
}

// ---------------------------------------------------------------------------
// fs.FS 实现
// ---------------------------------------------------------------------------

var (
	_ fs.FS        = (*ISO9660FS)(nil)
	_ fs.StatFS    = (*ISO9660FS)(nil)
	_ fs.ReadDirFS = (*ISO9660FS)(nil)
)

// lookup 按 fs.FS 的路径规范（fs.ValidPath）查找条目，不存在时返回
// fs.ErrNotExist——identify.go 的 exists/existsDir 靠 errors.Is(err,
// fs.ErrNotExist) 之外的"err == nil 才算存在"判据工作，但保持返回值是
// fs.ErrNotExist 家族仍然重要：这是 io/fs 生态的通用约定，fs.WalkDir 等
// 标准库函数在某些路径下会检查这个具体错误值。
func (f *ISO9660FS) lookup(name string) (*iso9660Entry, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}
	entry, ok := f.entries[name]
	if !ok {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return entry, nil
}

// Open 实现 fs.FS。
func (f *ISO9660FS) Open(name string) (fs.File, error) {
	entry, err := f.lookup(name)
	if err != nil {
		return nil, err
	}
	if entry.isDir {
		return &iso9660DirHandle{fsys: f, entry: entry, path: name}, nil
	}
	return &iso9660FileHandle{entry: entry, reader: f.reader}, nil
}

// Stat 实现 fs.StatFS（fs.Stat 在 fs.FS 同时实现 StatFS 时会直接调用它，
// 避免"打开再 Stat 再关闭"的额外开销，identify.go 的 exists/existsDir
// 与 manifest.go 均通过 fs.Stat 走到这里）。
func (f *ISO9660FS) Stat(name string) (fs.FileInfo, error) {
	entry, err := f.lookup(name)
	if err != nil {
		if pe, ok := err.(*fs.PathError); ok {
			pe.Op = "stat"
		}
		return nil, err
	}
	return entryFileInfo{entry: entry}, nil
}

// ReadDir 实现 fs.ReadDirFS（fs.WalkDir 遍历目录时会优先用它，manifest.go
// 的 GenerateManifest 依赖 fs.WalkDir 能正确遍历整棵树）。
func (f *ISO9660FS) ReadDir(name string) ([]fs.DirEntry, error) {
	entry, err := f.lookup(name)
	if err != nil {
		if pe, ok := err.(*fs.PathError); ok {
			pe.Op = "readdir"
		}
		return nil, err
	}
	if !entry.isDir {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fmt.Errorf("不是目录")}
	}
	return sortedDirEntries(entry), nil
}

func sortedDirEntries(entry *iso9660Entry) []fs.DirEntry {
	names := make([]string, 0, len(entry.children))
	for name := range entry.children {
		names = append(names, name)
	}
	sort.Strings(names)

	out := make([]fs.DirEntry, 0, len(names))
	for _, name := range names {
		out = append(out, entryFileInfo{entry: entry.children[name]})
	}
	return out
}

// entryFileInfo 同时实现 fs.FileInfo 与 fs.DirEntry（两个接口在
// iso9660Entry 上的实现刚好完全重合：Name/Size/Mode/ModTime/IsDir/Sys 加
// fs.DirEntry 额外要求的 Info() (fs.FileInfo, error)，用同一个包装类型
// 避免定义两个几乎相同的类型）。
type entryFileInfo struct {
	entry *iso9660Entry
}

func (i entryFileInfo) Name() string { return i.entry.name }
func (i entryFileInfo) Size() int64  { return i.entry.size }
func (i entryFileInfo) Mode() fs.FileMode {
	if i.entry.isDir {
		return fs.ModeDir | 0o555
	}
	return 0o444 // 只读文件系统，全部文件均不可写
}
func (i entryFileInfo) ModTime() time.Time         { return i.entry.modTime }
func (i entryFileInfo) IsDir() bool                { return i.entry.isDir }
func (i entryFileInfo) Sys() any                   { return nil }
func (i entryFileInfo) Type() fs.FileMode          { return i.Mode().Type() }
func (i entryFileInfo) Info() (fs.FileInfo, error) { return i, nil }

// iso9660FileHandle 是打开的文件的句柄。内容在 Open 时尚未读取（只有
// entry 里的 extent/size 两个元数据）——真正的字节读取发生在 Read 被调用
// 时，直接对 reader（ISO9660FS.reader，由 Open 传入）发起 ReadAt。
//
// 不在 iso9660Entry 本身保留一份对底层 ReaderAt 的引用：那会让目录树的
// 每个节点都多一个字段，而 reader 对整棵树是同一个值，放在 ISO9660FS
// 与每个打开的句柄上各存一份即可，不需要在树节点上重复。
type iso9660FileHandle struct {
	entry  *iso9660Entry
	reader io.ReaderAt
	pos    int64
}

func (h *iso9660FileHandle) Stat() (fs.FileInfo, error) { return entryFileInfo{entry: h.entry}, nil }

func (h *iso9660FileHandle) Read(p []byte) (int, error) {
	if h.pos >= h.entry.size {
		return 0, io.EOF
	}
	remaining := h.entry.size - h.pos
	if int64(len(p)) > remaining {
		p = p[:remaining]
	}
	off := int64(h.entry.extent)*iso9660SectorSize + h.pos
	n, err := h.reader.ReadAt(p, off)
	h.pos += int64(n)
	if err == io.EOF && int64(n) == int64(len(p)) {
		err = nil // 精确读满请求长度时不算 EOF，EOF 留给下一次 Read 在 pos>=size 时返回
	}
	return n, err
}

func (h *iso9660FileHandle) Close() error { return nil }

// iso9660DirHandle 是打开的目录的句柄，实现 fs.ReadDirFile（部分调用方
// 可能直接 Open 一个目录再调用 ReadDir 方法，而不是走 fs.ReadDirFS 的
// 快路径——标准库 io/fs 的通用契约要求目录的 fs.File 同时实现
// fs.ReadDirFile）。
type iso9660DirHandle struct {
	fsys    *ISO9660FS
	entry   *iso9660Entry
	path    string
	offset  int
	entries []fs.DirEntry
}

func (h *iso9660DirHandle) Stat() (fs.FileInfo, error) { return entryFileInfo{entry: h.entry}, nil }
func (h *iso9660DirHandle) Read([]byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: h.path, Err: fmt.Errorf("是一个目录")}
}
func (h *iso9660DirHandle) Close() error { return nil }

func (h *iso9660DirHandle) ReadDir(n int) ([]fs.DirEntry, error) {
	if h.entries == nil {
		h.entries = sortedDirEntries(h.entry)
	}
	remaining := len(h.entries) - h.offset
	if n <= 0 {
		out := h.entries[h.offset:]
		h.offset = len(h.entries)
		return out, nil
	}
	if remaining == 0 {
		return nil, io.EOF
	}
	if n > remaining {
		n = remaining
	}
	out := h.entries[h.offset : h.offset+n]
	h.offset += n
	return out, nil
}
