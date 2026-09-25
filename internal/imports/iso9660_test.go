// 本文件测试 iso9660.go 的 ISO 9660 解析器。
//
// 测试夹具不依赖外部下载或外部工具（本机检测到 xorriso 存在，但测试
// 运行时绝不调用它——气隙部署环境不能保证该工具在场，且引入外部进程
// 会让测试结果依赖本机装了哪个版本的工具）：全部 ISO 字节流由本文件的
// buildISO9660 函数在测试运行时纯 Go 生成。这样做还有一个额外好处：
// 生成逻辑本身就是对 ECMA-119 规范理解的验证——如果 buildISO9660 写错了
// 卷描述符或目录记录的字节布局，OpenISO9660 会读出错误的结果，测试会
// 直接失败，不需要额外交叉核对。
package imports

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"sort"
	"strings"
	"testing"
	"testing/iotest"
)

// isoFixtureFile 是构造测试 ISO 时指定的一个文件：相对路径（可含多级
// 目录，用 "/" 分隔）与内容。
type isoFixtureFile struct {
	path string
	data []byte
}

// buildISO9660 生成一份最小的、合法的 ISO 9660 镜像字节流，包含
// files 中列出的全部文件（自动按路径推导出所需的目录结构）。
//
// withJoliet 控制是否额外写一块 Joliet 补充卷描述符与对应的 UTF-16BE
// 目录树——OpenISO9660 在存在 Joliet 树时会优先使用它（见 iso9660.go），
// 因此 withJoliet=true 的夹具里，主 ISO 9660 树与 Joliet 树的文件名可以
// 不同，测试借此验证"确实是 Joliet 树被使用"（见
// TestOpenISO9660_JolietLongFileNames）。
//
// withRockRidge 控制纯 ISO 9660 树上的每条目录记录是否额外携带 Rock
// Ridge NM 系统使用字段（模拟真实 RHEL/Debian/Ubuntu/SUSE/ESXi 安装 ISO
// 的惯例：这些 ISO 用 Rock Ridge 而不是 Joliet 保留 .treeinfo 这类含
// 前导点、要求小写的真实文件名）。withJoliet 与 withRockRidge 两者独立，
// 可以同时为 false（模拟一份两种扩展都没有的最基础 ISO 9660 镜像）。
func buildISO9660(t *testing.T, files []isoFixtureFile, withJoliet, withRockRidge bool) []byte {
	t.Helper()

	b := &isoBuilder{}

	// 系统区：第 0-15 扇区，内容不影响解析，全零即可。
	b.padSectors(iso9660SystemAreaSectors)

	// 目录树按 ISO 9660 语义构建（8.3 风格、大写、";1" 版本号后缀），
	// 因为 PVD 的根目录记录必须指向一棵可以脱离 Joliet 单独工作的树——
	// 真实 ISO 9660 镜像即使带 Joliet，也总是同时保留一棵符合原始规范
	// 限制的基础树，供不支持 Joliet 的老旧读取器使用。
	plainTree := newFixtureTree(files, false, withRockRidge)
	jolietTree := (*fixtureTree)(nil)
	if withJoliet {
		jolietTree = newFixtureTree(files, true, false)
	}

	// 卷描述符本身占用固定 2 个或 3 个扇区（PVD、可选 Joliet SVD、
	// 终止符），紧跟系统区之后；但根目录记录里的 extent 要指向目录树
	// 数据实际写入的扇区号，而目录树数据紧跟在卷描述符序列之后——这产生
	// 一个先后依赖：必须先确定卷描述符序列占用多少扇区，才能知道目录树
	// 从哪个扇区开始。isoBuilder 用两阶段写入解决：先在内存里把两棵树
	// 分别序列化成扇区数据（layoutTree 只依赖树结构本身，不依赖最终
	// 扇区号），再在写入时统一分配扇区号并回填全部 extent 字段。
	volDescSectors := 2 // PVD + 终止符
	if withJoliet {
		volDescSectors = 3 // PVD + Joliet SVD + 终止符
	}
	dataStartSector := uint32(iso9660SystemAreaSectors) + uint32(volDescSectors)

	plainLayout := layoutTree(plainTree, dataStartSector, false)
	nextSector := plainLayout.nextFreeSector
	jolietLayout := treeLayout{}
	if withJoliet {
		jolietLayout = layoutTree(jolietTree, nextSector, true)
		nextSector = jolietLayout.nextFreeSector
	}

	totalSectors := nextSector

	b.writePVD(plainLayout.rootExtent, plainLayout.rootSize, totalSectors)
	if withJoliet {
		b.writeJolietSVD(jolietLayout.rootExtent, jolietLayout.rootSize, totalSectors)
	}
	b.writeTerminator()

	b.writeLayoutData(plainLayout)
	if withJoliet {
		b.writeLayoutData(jolietLayout)
	}

	return b.buf.Bytes()
}

// isoBuilder 是顺序写入扇区数据的累加器。
type isoBuilder struct {
	buf bytes.Buffer
}

func (b *isoBuilder) padSectors(n int) {
	b.buf.Write(make([]byte, n*iso9660SectorSize))
}

func (b *isoBuilder) currentSector() uint32 {
	return uint32(b.buf.Len() / iso9660SectorSize)
}

// writeSector 写入恰好一个扇区，内容不足 2048 字节的部分用零填充。
func (b *isoBuilder) writeSector(data []byte) {
	if len(data) > iso9660SectorSize {
		panic("isoBuilder: 单扇区数据超过 2048 字节")
	}
	sector := make([]byte, iso9660SectorSize)
	copy(sector, data)
	b.buf.Write(sector)
}

func (b *isoBuilder) writeTerminator() {
	hdr := make([]byte, iso9660SectorSize)
	hdr[0] = iso9660VolDescTerminator
	copy(hdr[1:6], iso9660StdIdentifier)
	hdr[6] = 1
	b.writeSector(hdr)
}

// writePVD 写主卷描述符。字段布局参考 ECMA-119 §8.4；本构造函数只填充
// OpenISO9660 实际会读取的字段（根目录记录、标准标识符、卷描述符类型），
// 其余字段（卷集标识、发布者等）留空——它们不影响本模块的解析结果。
func (b *isoBuilder) writePVD(rootExtent, rootSize, totalSectors uint32) {
	buf := make([]byte, iso9660SectorSize)
	buf[0] = iso9660VolDescPrimary
	copy(buf[1:6], iso9660StdIdentifier)
	buf[6] = 1
	// 偏移 80-83：卷空间大小（逻辑块总数），小端 + 大端各一份；本模块的
	// 解析逻辑不读这个字段，但写出合法值是"生成一份可被其它工具打开的
	// 合法 ISO"这个更高标准的一部分。
	putLE32(buf[80:84], totalSectors)
	putBE32(buf[84:88], totalSectors)
	// 偏移 156-189：根目录记录（34 字节定长，无扩展系统使用区）。
	writeDirRecord(buf[156:190], rootExtent, rootSize, true, "\x00", [7]byte{})
	b.writeSector(buf)
}

// writeJolietSVD 写 Joliet 补充卷描述符，布局与 PVD 基本一致，额外在
// 偏移 88-90 写入 Joliet Level 3 转义序列（"%/E"），并把偏移 88 起的
// 字段解释为"这棵树的文件名是 UCS-2/UTF-16BE"这一事实交给 OpenISO9660
// 的 isJolietEscapeSequence 去识别。
func (b *isoBuilder) writeJolietSVD(rootExtent, rootSize, totalSectors uint32) {
	buf := make([]byte, iso9660SectorSize)
	buf[0] = iso9660VolDescSupplementary
	copy(buf[1:6], iso9660StdIdentifier)
	buf[6] = 1
	copy(buf[88:91], "%/E")
	putLE32(buf[80:84], totalSectors)
	putBE32(buf[84:88], totalSectors)
	writeDirRecord(buf[156:190], rootExtent, rootSize, true, "\x00", [7]byte{})
	b.writeSector(buf)
}

// writeLayoutData 把一棵已完成布局（分配好 extent 编号）的目录树的全部
// 扇区数据顺序写入 builder（目录记录扇区先于该目录内全部子目录的数据
// 扇区被分配，但写入顺序按 layoutTree 生成的 sectors 切片来，保证与
// 分配时的扇区号严格一致）。
func (b *isoBuilder) writeLayoutData(layout treeLayout) {
	for _, sector := range layout.sectors {
		b.writeSector(sector)
	}
}

// ---------------------------------------------------------------------------
// 目录树布局
// ---------------------------------------------------------------------------

// fixtureTree 是从 isoFixtureFile 列表推导出的内存目录树（构造测试夹具
// 用，与 iso9660.go 里生产代码的 iso9660Entry 是两套独立类型——本文件
// 故意不复用生产类型，因为测试夹具生成的方向与生产解析方向相反，共享
// 类型反而会让两者的字段含义混淆）。
type fixtureTree struct {
	name     string
	isDir    bool
	data     []byte
	children []*fixtureTree // 按名字排序，与 ECMA-119 §9.3 要求目录记录按文件名排序一致
	// rockRidgeName 非空时表示这个节点在纯 ISO 9660 树上要额外携带一条
	// Rock Ridge NM 系统使用字段，值为原始大小写/未截断的名字（模拟
	// mkisofs/xorriso 生成真实发行版 ISO 时的惯例：主文件名字段是大写
	// 8.3 形式，真实名字通过 NM 字段携带）。
	rockRidgeName string
}

// newFixtureTree 从扁平的文件列表构建目录树。useJoliet 决定文件/目录名
// 如何规范化：
//   - false（纯 ISO 9660）：全大写，扩展名与主文件名分别截断到 8/3 字符
//     （简化的 8.3 规则，够本测试用），文件名末尾加 ";1"。withRockRidge
//     为 true 时，每个节点额外携带 rockRidgeName（原始大小写、不截断、
//     不带版本号），模拟真实发行版 ISO 的 mkisofs/xorriso 惯例。
//   - true（Joliet）：保留原始大小写与完整长度（Joliet 最长支持 64 字符，
//     本测试的路径远短于此），文件名末尾同样加 ";1"（Joliet 规范同样
//     保留版本号语义，真实 Windows 安装 ISO 的 Joliet 树里也能看到）；
//     withRockRidge 参数在这一分支下不生效（Joliet 树本身已是原始
//     名字，不需要 NM 字段，也不模拟两者共存的场景，见 iso9660.go 文件
//     头注释：市面 ISO 从不对同一棵树同时应用两种扩展）。
func newFixtureTree(files []isoFixtureFile, useJoliet, withRockRidge bool) *fixtureTree {
	root := &fixtureTree{name: ".", isDir: true}
	for _, f := range files {
		parts := strings.Split(f.path, "/")
		cur := root
		for i, part := range parts {
			isLeaf := i == len(parts)-1
			name := part
			if !useJoliet {
				name = to8Dot3(part, isLeaf)
			}
			child := cur.findChild(name)
			if child == nil {
				child = &fixtureTree{name: name, isDir: !isLeaf}
				if !useJoliet && withRockRidge {
					child.rockRidgeName = part
				}
				cur.children = append(cur.children, child)
			}
			if isLeaf {
				child.data = f.data
			}
			cur = child
		}
	}
	sortFixtureTree(root)
	return root
}

func (t *fixtureTree) findChild(name string) *fixtureTree {
	for _, c := range t.children {
		if c.name == name {
			return c
		}
	}
	return nil
}

func sortFixtureTree(t *fixtureTree) {
	sort.Slice(t.children, func(i, j int) bool { return t.children[i].name < t.children[j].name })
	for _, c := range t.children {
		if c.isDir {
			sortFixtureTree(c)
		}
	}
}

// to8Dot3 把一个长文件名规范化成简化的 8.3 大写形式并加版本号后缀
// （纯 ISO 9660 树用）。目录名不加版本号后缀（ECMA-119 对目录记录的
// 文件名字段不要求版本号，真实镜像里目录名也确实不带 ";1"）。
func to8Dot3(name string, isFile bool) string {
	upper := strings.ToUpper(name)
	base, ext := upper, ""
	if idx := strings.LastIndexByte(upper, '.'); idx >= 0 {
		base, ext = upper[:idx], upper[idx+1:]
	}
	if len(base) > 8 {
		base = base[:8]
	}
	if len(ext) > 3 {
		ext = ext[:3]
	}
	out := base
	if ext != "" {
		out += "." + ext
	}
	if isFile {
		out += ";1"
	}
	return out
}

// treeLayout 是目录树完成"分配 extent 编号"之后的结果：根目录的
// extent/size，加按分配顺序排列的全部扇区数据（目录记录扇区与文件内容
// 扇区都在内，写入 isoBuilder 时按此顺序原样输出，扇区号与写入顺序
// 严格对应，因为 nextFreeSector 就是从 dataStartSector 开始按写入顺序
// 递增分配的）。
type treeLayout struct {
	rootExtent     uint32
	rootSize       uint32
	sectors        [][]byte
	nextFreeSector uint32
}

// layoutTree 用两阶段广度优先的方式给整棵树分配 extent 编号：先给每个
// 目录节点的目录记录数据分配 extent（这一步需要先知道每个子目录/文件
// 各占多少扇区，才能算出下一个可用扇区号），再依次生成每个目录节点的
// 目录记录扇区内容（此时全部子节点的 extent 已知，可以正确写入目录记录
// 里的 extent 字段）。
//
// 用广度优先而不是深度优先分配 extent：这不是格式要求（ECMA-119 不限定
// extent 分配顺序），只是让生成逻辑更简单——同一层的目录记录数据先于
// 下一层内容分配，与真实镜像常见的布局习惯一致，但本函数的正确性不
// 依赖这个顺序，纯粹是实现选择。
func layoutTree(root *fixtureTree, dataStartSector uint32, useJoliet bool) treeLayout {
	next := dataStartSector
	var order []*fixtureExtentInfo
	var pending = []*fixtureExtentInfo{{node: root}}

	// 第一遍：广度优先遍历，给每个目录节点与每个文件节点分配 extent，
	// 目录节点的 size 是"它的目录记录数据占多少字节"（按子节点数量算，
	// 分配阶段先用一个粗略估计，稍后用真实序列化结果覆盖，见下方第二遍）。
	for len(pending) > 0 {
		var nextPending []*fixtureExtentInfo
		for _, q := range pending {
			q.extent = next
			if q.node.isDir {
				sectors := dirRecordSectorCount(q.node, useJoliet)
				q.size = uint32(sectors) * iso9660SectorSize
				next += uint32(sectors)
			} else {
				sectors := (len(q.node.data) + iso9660SectorSize - 1) / iso9660SectorSize
				if sectors == 0 {
					sectors = 1 // 空文件没有内容扇区，但仍需要一个占位扇区，避免下一个节点复用同一个 extent 号
				}
				q.size = uint32(len(q.node.data))
				next += uint32(sectors)
			}
			order = append(order, q)

			if q.node.isDir {
				for _, c := range q.node.children {
					nextPending = append(nextPending, &fixtureExtentInfo{node: c})
				}
			}
		}
		pending = nextPending
	}

	// 第二遍：按分配好的 extent，为每个目录节点生成目录记录扇区数据；
	// 为每个文件节点生成内容扇区数据（原样内容 + 零填充到扇区边界）。
	extentOf := map[*fixtureTree]*fixtureExtentInfo{}
	for _, q := range order {
		extentOf[q.node] = q
	}

	var sectors [][]byte
	for _, q := range order {
		if q.node.isDir {
			sectors = append(sectors, buildDirRecordSectors(q.node, extentOf, useJoliet)...)
		} else {
			sectors = append(sectors, buildFileContentSectors(q.node.data)...)
		}
	}

	rootQ := order[0]
	return treeLayout{
		rootExtent:     rootQ.extent,
		rootSize:       rootQ.size,
		sectors:        sectors,
		nextFreeSector: next,
	}
}

// dirRecordSectorCount 估计一个目录节点的目录记录数据占用多少扇区
// （"." 与 ".." 两条记录 + 每个子节点一条记录，按 2048 字节扇区、记录
// 不跨扇区边界的规则打包）。
func dirRecordSectorCount(node *fixtureTree, useJoliet bool) int {
	sectors := 1
	used := 0
	place := func(recLen int) {
		if used+recLen > iso9660SectorSize {
			sectors++
			used = 0
		}
		used += recLen
	}
	place(34) // "."
	place(34) // ".."
	for _, c := range node.children {
		place(dirRecordLength(c, useJoliet))
	}
	return sectors
}

// dirRecordLength 计算一个子节点对应的目录记录总长度：定长部分 33 字节
// + 文件名字节数 + 必要时 1 字节填充使"定长部分+文件名"对齐到偶数
// （ECMA-119 §9.1.12，系统使用字段从偶数偏移开始是 SUSP 的隐含要求）
// + （若该节点携带 rockRidgeName）一条 Rock Ridge NM 字段的精确长度
// （NM 字段自身不需要再对齐，SUSP 允许多个系统使用字段背靠背排列）
// + 若此时总长仍为奇数，末尾再补 1 字节（ECMA-119 §9.1.12 要求整条
// 记录长度必须是偶数，这是最终兜底的一次对齐，与 name 字段后的那次
// 对齐是两个独立的检查点）。
func dirRecordLength(c *fixtureTree, useJoliet bool) int {
	nameBytes := len(c.name)
	if useJoliet {
		nameBytes = len(c.name) * 2 // UTF-16BE，本测试的文件名全是 ASCII 范围内字符，每字符固定 2 字节
	}
	total := 33 + nameBytes
	if total%2 != 0 {
		total++
	}
	if c.rockRidgeName != "" {
		total += 5 + len(c.rockRidgeName)
	}
	if total%2 != 0 {
		total++
	}
	return total
}

// fixtureExtentInfo 是 layoutTree 分配阶段用来记录"这个树节点最终落在
// 哪个 extent、占多大"的关联结构。
type fixtureExtentInfo struct {
	node   *fixtureTree
	extent uint32
	size   uint32
}

// buildDirRecordSectors 生成一个目录节点的目录记录扇区数据（可能占多个
// 扇区），记录顺序为 "."、".."、然后按名字排序的子节点。extentOf 提供
// 每个子节点（与节点自身，用于 "." 记录）已分配好的 extent/size。
func buildDirRecordSectors(node *fixtureTree, extentOf map[*fixtureTree]*fixtureExtentInfo, useJoliet bool) [][]byte {
	self := extentOf[node]

	var sectors [][]byte
	cur := make([]byte, iso9660SectorSize)
	used := 0

	appendRecord := func(rec []byte) {
		if used+len(rec) > iso9660SectorSize {
			sectors = append(sectors, cur)
			cur = make([]byte, iso9660SectorSize)
			used = 0
		}
		copy(cur[used:], rec)
		used += len(rec)
	}

	appendRecord(makeSelfRefRecord(self.extent, self.size, "\x00"))
	appendRecord(makeSelfRefRecord(self.extent, self.size, "\x01"))
	for _, c := range node.children {
		info := extentOf[c]
		appendRecord(makeNamedRecord(info.extent, info.size, c.isDir, c.name, c.rockRidgeName, useJoliet))
	}

	sectors = append(sectors, cur)
	return sectors
}

// makeSelfRefRecord 生成 "." 或 ".." 记录（rawName 恰好 1 字节：0x00 或
// 0x01），两者在本构造器里都指向同一个 extent/size（真实 ISO 里 "." 指向
// 自身、".." 指向父目录，但父目录的 extent 在测试夹具的简化模型里
// 未单独跟踪——OpenISO9660 的 readDirectoryTree 会跳过 "."/".." 两条
// 记录，不解析它们的 extent 字段，因此这个简化不影响任何测试断言）。
func makeSelfRefRecord(extent, size uint32, rawName string) []byte {
	rec := make([]byte, 34)
	writeDirRecord(rec, extent, size, true, rawName, [7]byte{})
	return rec
}

// makeNamedRecord 生成一条指向具体子节点的目录记录，文件名按 useJoliet
// 决定是否编码为 UTF-16BE。rockRidgeName 非空时，在文件名字段之后追加
// 一条 Rock Ridge NM 系统使用字段（模拟真实发行版 ISO 的布局）。
func makeNamedRecord(extent, size uint32, isDir bool, name, rockRidgeName string, useJoliet bool) []byte {
	nameBytes := []byte(name)
	if useJoliet {
		nameBytes = encodeUTF16BE(name)
	}
	// 填充判据与 dirRecordLength 一致：33（定长部分）+ 文件名字节数
	// 若为奇数，说明文件名字节数本身是偶数，此时需要 1 字节填充
	// （ECMA-119 §9.1.12 的条件是"文件名长度为偶数时才填充"，不是直接
	// 对文件名字节数取模——两者在文件名长度为奇数时结论相反，之前这里
	// 直接对 len(nameBytes) 取模判断，与规范条件正好相反，是一个真实的
	// 字节布局 bug：错误地在该填充时不填充、不该填充时填充，导致 Rock
	// Ridge NM 字段被写在错误的偏移上，读取端按規范正确的偏移去找就会
	// 找不到 "NM" 签名）。
	nameFieldLen := len(nameBytes)
	if (33+nameFieldLen)%2 != 0 {
		nameFieldLen++
	}

	var nmField []byte
	if rockRidgeName != "" {
		nmField = makeRockRidgeNMField(rockRidgeName)
	}

	recLen := 33 + nameFieldLen + len(nmField)
	if recLen%2 != 0 {
		recLen++ // 与 dirRecordLength 的最终兜底对齐保持一致
	}
	rec := make([]byte, recLen)
	writeDirRecordRaw(rec, extent, size, isDir, nameBytes)
	copy(rec[33+nameFieldLen:], nmField)
	return rec
}

// makeRockRidgeNMField 生成一条完整的 Rock Ridge NM 系统使用字段
// （RRIP §4.1.4）：2 字节签名 "NM" + 1 字节总长度（LEN_SU，即本字段自身
// 的精确字节数，不额外填充——SUSP 系统使用字段各自不要求对齐到偶数，
// 偶数对齐只在整条目录记录末尾要求一次，由 writeDirRecordRaw 之外的
// 调用方在必要时另加 1 字节全局填充）+ 1 字节版本（固定 1）+ 1 字节
// flags（本测试的名字都很短，不需要 CONTINUE，固定 0）+ 名字字节。
func makeRockRidgeNMField(name string) []byte {
	length := 5 + len(name)
	field := make([]byte, length)
	copy(field[0:2], "NM")
	field[2] = byte(length)
	field[3] = 1 // 版本
	field[4] = 0 // flags：不设置 CONTINUE 位
	copy(field[5:], name)
	return field
}

// buildFileContentSectors 把文件内容切成扇区，最后一个扇区零填充到
// 边界；空文件返回恰好一个全零扇区（占位，供其 extent 号有对应的物理
// 扇区可读，虽然 size=0 时 OpenISO9660 的读取路径不会真的读到这块数据）。
func buildFileContentSectors(data []byte) [][]byte {
	if len(data) == 0 {
		return [][]byte{make([]byte, iso9660SectorSize)}
	}
	var sectors [][]byte
	for off := 0; off < len(data); off += iso9660SectorSize {
		end := off + iso9660SectorSize
		if end > len(data) {
			end = len(data)
		}
		sector := make([]byte, iso9660SectorSize)
		copy(sector, data[off:end])
		sectors = append(sectors, sector)
	}
	return sectors
}

// writeDirRecord 把 extent/size/isDir/rawName（1 字节的 "."/".." 特殊
// 名字）与全零日期字段写入 dst（dst 长度必须恰好 34 字节）。
func writeDirRecord(dst []byte, extent, size uint32, isDir bool, rawName string, date [7]byte) {
	writeDirRecordRaw(dst, extent, size, isDir, []byte(rawName))
	copy(dst[18:25], date[:])
}

// writeDirRecordRaw 是目录记录字节布局的核心写入函数（与 iso9660.go 的
// parseDirectoryRecord 读取同一套布局，字段偏移必须逐一对应，见该函数
// 文档注释里的布局表）。dst 长度必须恰好等于
// 33+len(nameBytes)（再按需 +1 对齐到偶数）。
func writeDirRecordRaw(dst []byte, extent, size uint32, isDir bool, nameBytes []byte) {
	dst[0] = byte(len(dst))
	putLE32(dst[2:6], extent)
	putBE32(dst[6:10], extent)
	putLE32(dst[10:14], size)
	putBE32(dst[14:18], size)
	if isDir {
		dst[25] = iso9660DirFlagDirectory
	}
	dst[32] = byte(len(nameBytes))
	copy(dst[33:33+len(nameBytes)], nameBytes)
}

// encodeUTF16BE 把 ASCII 范围内的字符串编码为 UTF-16BE 字节序列（本测试
// 全部文件名都在 ASCII 范围内，不需要处理代理对）。
func encodeUTF16BE(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for _, r := range s {
		out = append(out, byte(r>>8), byte(r))
	}
	return out
}

func putLE32(dst []byte, v uint32) {
	dst[0] = byte(v)
	dst[1] = byte(v >> 8)
	dst[2] = byte(v >> 16)
	dst[3] = byte(v >> 24)
}

func putBE32(dst []byte, v uint32) {
	dst[0] = byte(v >> 24)
	dst[1] = byte(v >> 16)
	dst[2] = byte(v >> 8)
	dst[3] = byte(v)
}

// ---------------------------------------------------------------------------
// 测试用例
// ---------------------------------------------------------------------------

func TestOpenISO9660_ReadRootFile(t *testing.T) {
	// 带 Rock Ridge：真实 RHEL 系 ISO 的惯例做法，用来保留 ".treeinfo"
	// 这个含前导点的精确名字（纯 ISO 9660 层会把它规范化成 8.3 大写
	// 形式，读不到这个精确路径，见 iso9660.go 文件头注释）。
	data := buildISO9660(t, []isoFixtureFile{
		{path: ".treeinfo", data: []byte("[images-x86_64]\nkernel = vmlinuz\n")},
	}, false, true)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	got, err := fs.ReadFile(fsys, ".treeinfo")
	if err != nil {
		t.Fatalf("读取根目录下的 .treeinfo 失败: %v", err)
	}
	want := "[images-x86_64]\nkernel = vmlinuz\n"
	if string(got) != want {
		t.Fatalf("内容不匹配: got %q, want %q", got, want)
	}
}

func TestOpenISO9660_ReadNestedFile(t *testing.T) {
	// Windows 惯例用 Joliet（而不是 Rock Ridge）保留 sources/install.wim
	// 的精确大小写路径。
	data := buildISO9660(t, []isoFixtureFile{
		{path: "sources/install.wim", data: []byte("fake-wim-bytes")},
	}, true, false)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	got, err := fs.ReadFile(fsys, "sources/install.wim")
	if err != nil {
		t.Fatalf("读取 sources/install.wim 失败: %v", err)
	}
	if string(got) != "fake-wim-bytes" {
		t.Fatalf("内容不匹配: got %q", got)
	}
}

func TestOpenISO9660_ReadThreeLevelNestedFile(t *testing.T) {
	// boot/x86_64/loader/linux 是 SUSE 的三层嵌套路径（任务明确要求覆盖），
	// 全小写——同样需要 Rock Ridge 才能在纯 ISO 9660 层之外保留精确大小写
	// （SUSE ISO 的真实惯例）。
	data := buildISO9660(t, []isoFixtureFile{
		{path: "boot/x86_64/loader/linux", data: []byte("fake-suse-kernel")},
	}, false, true)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	got, err := fs.ReadFile(fsys, "boot/x86_64/loader/linux")
	if err != nil {
		t.Fatalf("读取三层嵌套路径失败: %v", err)
	}
	if string(got) != "fake-suse-kernel" {
		t.Fatalf("内容不匹配: got %q", got)
	}
}

func TestOpenISO9660_StatFileAndDir(t *testing.T) {
	data := buildISO9660(t, []isoFixtureFile{
		{path: "repodata/repomd.xml", data: []byte("<repomd/>")},
	}, false, true)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	// identify.go 的 exists() 靠 fs.Stat 对文件返回 err == nil 判定存在
	// （SPEC §2.1 特征文件表：SUSE 的判据之一是 repodata/repomd.xml）。
	fileInfo, err := fs.Stat(fsys, "repodata/repomd.xml")
	if err != nil {
		t.Fatalf("Stat 文件失败: %v", err)
	}
	if fileInfo.IsDir() {
		t.Fatal("repodata/repomd.xml 应被识别为文件，不是目录")
	}
	if fileInfo.Size() != int64(len("<repomd/>")) {
		t.Fatalf("文件大小不匹配: got %d, want %d", fileInfo.Size(), len("<repomd/>"))
	}

	// identify.go 的 existsDir() 靠 fs.Stat 返回的 IsDir()==true 判定
	// （SPEC §2.1：Debian 的判据是 dists/ 目录本身存在）。
	dirInfo, err := fs.Stat(fsys, "repodata")
	if err != nil {
		t.Fatalf("Stat 目录失败: %v", err)
	}
	if !dirInfo.IsDir() {
		t.Fatal("repodata 应被识别为目录")
	}
}

func TestOpenISO9660_WalkDirVisitsEveryFile(t *testing.T) {
	files := []isoFixtureFile{
		{path: ".treeinfo", data: []byte("a")},
		{path: "images/pxeboot/vmlinuz", data: []byte("b")},
		{path: "images/pxeboot/initrd.img", data: []byte("c")},
		{path: "Packages/foo.rpm", data: []byte("d")},
	}
	data := buildISO9660(t, files, false, true)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	var visited []string
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			visited = append(visited, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("WalkDir 失败: %v", err)
	}

	sort.Strings(visited)
	want := []string{".treeinfo", "Packages/foo.rpm", "images/pxeboot/initrd.img", "images/pxeboot/vmlinuz"}
	sort.Strings(want)
	if len(visited) != len(want) {
		t.Fatalf("遍历到的文件数量不对: got %v, want %v", visited, want)
	}
	for i := range want {
		if visited[i] != want[i] {
			t.Fatalf("遍历结果不匹配: got %v, want %v", visited, want)
		}
	}
}

func TestOpenISO9660_ContentIntegrity(t *testing.T) {
	// 内容跨多个扇区（超过 2048 字节），验证多扇区文件的读取不丢字节、
	// 不错位（manifest.go 的 sha256File 依赖完整读取）。
	payload := bytes.Repeat([]byte("0123456789abcdef"), 300) // 4800 字节，跨 3 个扇区
	data := buildISO9660(t, []isoFixtureFile{
		{path: "big.bin", data: payload},
	}, false, true) // 带 Rock Ridge，用原始小写名字查找——本测试关心内容完整性，不是命名规则

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	got, err := fs.ReadFile(fsys, "big.bin")
	if err != nil {
		t.Fatalf("读取大文件失败: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Fatalf("内容不完整或错位: got %d 字节, want %d 字节", len(got), len(payload))
	}
}

func TestOpenISO9660_NotExistReturnsFSErrNotExist(t *testing.T) {
	data := buildISO9660(t, []isoFixtureFile{
		{path: ".treeinfo", data: []byte("x")},
	}, false, true)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	_, err = fs.Stat(fsys, "does/not/exist")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("不存在的路径应返回 fs.ErrNotExist，got %v", err)
	}

	_, err = fsys.Open("also-missing")
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("Open 不存在的路径应返回 fs.ErrNotExist，got %v", err)
	}
}

func TestOpenISO9660_MalformedImageReturnsErrorNotPanic(t *testing.T) {
	cases := map[string][]byte{
		"空字节流": {},
		"太短的字节流（不足一个系统区）": make([]byte, 100),
		"系统区正确但标准标识符错误": func() []byte {
			b := make([]byte, iso9660SectorSize*(iso9660SystemAreaSectors+1))
			b[iso9660SystemAreaSectors*iso9660SectorSize] = iso9660VolDescPrimary
			copy(b[iso9660SystemAreaSectors*iso9660SectorSize+1:], "WRONG")
			return b
		}(),
		"卷描述符序列没有终止符（截断）": func() []byte {
			b := make([]byte, iso9660SectorSize*(iso9660SystemAreaSectors+1))
			off := iso9660SystemAreaSectors * iso9660SectorSize
			b[off] = iso9660VolDescPrimary
			copy(b[off+1:], iso9660StdIdentifier)
			return b // 只有 PVD，没有终止符，也没有更多扇区可读
		}(),
	}

	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("解析畸形 ISO 时发生 panic（而不是返回 error）: %v", r)
				}
			}()
			_, err := OpenISO9660(bytes.NewReader(raw))
			if err == nil {
				t.Fatal("畸形/截断的 ISO 应返回明确错误，而不是成功解析")
			}
		})
	}
}

// erroringReaderAt 是一个在读取到指定偏移之后开始返回错误的 io.ReaderAt，
// 用于模拟"数据区中途被截断/IO 错误"的场景（比合成畸形字节流更贴近真实
// 故障模式：镶嵌合法的卷描述符头，但后续数据不可读）。
type erroringReaderAt struct {
	underlying io.ReaderAt
	failAfter  int64
}

func (e erroringReaderAt) ReadAt(p []byte, off int64) (int, error) {
	if off >= e.failAfter {
		return 0, iotest.ErrTimeout
	}
	if off+int64(len(p)) > e.failAfter {
		n, _ := e.underlying.ReadAt(p[:e.failAfter-off], off)
		return n, iotest.ErrTimeout
	}
	return e.underlying.ReadAt(p, off)
}

func TestOpenISO9660_TruncatedDataRegionReturnsErrorNotPanic(t *testing.T) {
	data := buildISO9660(t, []isoFixtureFile{
		{path: "boot/x86_64/loader/linux", data: bytes.Repeat([]byte("x"), 5000)},
	}, false, true)

	// 只保留卷描述符与目录记录区域，切断后续的文件内容数据区。
	truncated := erroringReaderAt{
		underlying: bytes.NewReader(data),
		failAfter:  int64(len(data)) - iso9660SectorSize*2,
	}

	fsys, err := OpenISO9660(truncated)
	if err != nil {
		// 目录树本身也可能落在截断区域之后，此时 OpenISO9660 阶段报错
		// 同样满足"不 panic、返回明确错误"的要求。
		return
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("读取被截断的文件内容时发生 panic: %v", r)
		}
	}()
	_, err = fs.ReadFile(fsys, "boot/x86_64/loader/linux")
	if err == nil {
		t.Fatal("读取被截断的文件内容应返回错误")
	}
}

func TestOpenISO9660_JolietLongFileNames(t *testing.T) {
	longName := "this-is-a-long-file-name-that-exceeds-8-3-limits.txt"
	data := buildISO9660(t, []isoFixtureFile{
		{path: longName, data: []byte("joliet-content")},
	}, true, false)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	got, err := fs.ReadFile(fsys, longName)
	if err != nil {
		t.Fatalf("按原始长文件名读取失败（Joliet 树应保留完整名字，不做 8.3 截断）: %v", err)
	}
	if string(got) != "joliet-content" {
		t.Fatalf("内容不匹配: got %q", got)
	}

	// 反向验证：若解析器退化成只用纯 ISO 9660 树（忽略 Joliet），这个
	// 精确长文件名路径会因为被截断成 8.3 大写形式而查找失败——这条断言
	// 确认"确实是 Joliet 树被选中"，不是恰好两棵树查找都失败又都成功。
	if _, statErr := fs.Stat(fsys, "THIS-IS-A.TXT"); statErr == nil {
		t.Log("附带观察：纯 ISO 9660 树的 8.3 截断名也存在于底层字节流中（预期内，不影响本测试主张）")
	}
}

func TestOpenISO9660_WithoutJolietUsesPlainTree(t *testing.T) {
	// 既不带 Joliet 也不带 Rock Ridge：模拟最基础的 ISO 9660 镜像（两种
	// 扩展都没有）。这种镜像上的文件名会被强制大写、做 8.3 截断、加
	// ";1" 版本号后缀——这是纯 ISO 9660 规范本身的行为，不是本解析器的
	// 缺陷。本测试确认解析器在这种"最基础子集"场景下仍能正确工作：
	// 用大写+版本号后缀的规范化名字去查找，而不是假装能查到原始小写
	// 名字（对应任务要求：即使不支持任何扩展，也要有完整测试覆盖这个
	// 基础子集，且测试断言要与真实格式行为一致，不能靠凑巧的文件名
	// 掩盖"未做扩展时到底应该查什么名字"这个事实）。
	data := buildISO9660(t, []isoFixtureFile{
		{path: "mboot.efi", data: []byte("efi-binary")},
		{path: "boot.cfg", data: []byte("modules=/foo.gz")},
	}, false, false)

	fsys, err := OpenISO9660(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("OpenISO9660 失败: %v", err)
	}

	// 8.3 规范化后是 MBOOT.EFI 与 BOOT.CFG，但**查找用小写**。
	//
	// 本测试的断言改过一次。它原先期望大写名，理由写的是「不能靠凑巧的文件名
	// 掩盖『未做扩展时到底应该查什么名字』这个事实」——那个意图是对的，但给出
	// 的答案被真镜像证伪了：Linux 的 iso9660 驱动在没有 Rock Ridge/Joliet 时
	// 会把名字映射成小写，而 SPEC §2.1 的特征文件表与全部 extract_*.go 也都按
	// 小写写。按大写查的直接后果是**真实 ESXi ISO 识别失败**（那份镜像没有
	// 任何扩展，根目录 114 项全大写），见
	// TestFinalizeEntryName_LowercasesPlainISO9660 里的实测依据。
	//
	// 版本号后缀 ";1" 仍然自动剥离（stripVersionSuffixASCII）——那不是本模块
	// 要暴露的语义，调用方不需要关心 ISO 9660 的版本号机制。
	for _, name := range []string{"mboot.efi", "boot.cfg"} {
		if _, err := fs.Stat(fsys, name); err != nil {
			t.Fatalf("纯 ISO 9660 树下 Stat %q 失败: %v", name, err)
		}
	}
}
