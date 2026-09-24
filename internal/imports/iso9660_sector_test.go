// 本文件单测目录扇区的扫描规则（ECMA-119 §6.8.1：目录记录不得跨逻辑块，
// 扇区尾部不足一条记录的部分是填充）。
//
// 单独成文件而不是塞进 iso9660_test.go：那份文件围绕 buildISO9660 这个完整
// 镜像生成器组织，而这里要精确控制「扇区尾部剩几个字节」，用手工拼字节比让
// 生成器凑出某个特定剩余长度直接得多。
package imports

import (
	"testing"
)

// makeDirRecord 拼一条合法的目录记录，返回恰好 recLen 字节。
//
// 只填扫描逻辑会读到的字段：长度、extent、size、标志位、文件名长度与文件名。
// 其余字段（时间戳等）留零——本测试关心的是「扫描到哪里停」，不是字段解码。
func makeDirRecord(t *testing.T, recLen int, name string, isDir bool) []byte {
	t.Helper()
	if recLen < 33+len(name) {
		t.Fatalf("recLen %d 装不下 33 字节定长部分加 %d 字节文件名", recLen, len(name))
	}
	rec := make([]byte, recLen)
	rec[0] = byte(recLen)
	// extent（偏移 2，both-endian 各 4 字节）与 size（偏移 10）留 0：本测试的
	// 记录不会被真的去读数据区。
	if isDir {
		rec[25] = 0x02 // 标志位的目录位
	}
	rec[32] = byte(len(name))
	copy(rec[33:], name)
	return rec
}

// TestScanDirectorySector_StopsOnShortTail 验证扇区尾部剩余不足一条记录
// （1..33 字节）时正常收尾，不报「ISO 已截断或损坏」。
//
// **这是真实 ISO 上的一个阻断性 bug。** 扫描循环的条件曾是 offset < len(sector)，
// 于是当尾部只剩 18 字节时仍会去解析，撞上 parseDirectoryRecord 的
// 「len(buf) < 34 即报错」防御检查，整个导入以
//
//	解析目录记录失败（扇区内偏移 2030）: 目录记录长度 18 字节小于最小定长部分
//	34 字节——ISO 已截断或损坏
//
// 告终。而那 18 字节是 ECMA-119 §6.8.1 规定的正常填充，镜像完全没问题。
//
// 后果的严重性：导入是**所有六个 OS 家族装机链路的第一步**，这一步失败则
// 一台机器都装不出来。而它此前没被发现，是因为全部导入测试用的都是合成镜像
// 或 MapFS，而 e2e 脚本一律绕过导入直接写库——真 ISO 从未走过这条路。
func TestScanDirectorySector_StopsOnShortTail(t *testing.T) {
	sector := make([]byte, iso9660SectorSize)
	offset := 0
	place := func(rec []byte) {
		copy(sector[offset:], rec)
		offset += len(rec)
	}

	// "." 与 ".."：各 34 字节，文件名是单字节 0x00 / 0x01。
	place(makeDirRecord(t, 34, "\x00", true))
	place(makeDirRecord(t, 34, "\x01", true))

	// 再塞 49 条 40 字节的文件记录（33 定长 + 6 名字 + 1 对齐 = 40）。
	// 68 + 49*40 = 2028，于是扇区尾部剩 2048-2028 = 20 字节填充，
	// 正落在「不足 34 字节」这个触发区间里。
	const fileRecLen = 40
	const fileCount = 49
	for i := 0; i < fileCount; i++ {
		name := string([]byte{byte('A' + i%26), 'B', 'C', '.', ';', '1'})
		place(makeDirRecord(t, fileRecLen, name, false))
	}
	if want := 34*2 + fileRecLen*fileCount; offset != want {
		t.Fatalf("夹具自身算错了：已用 %d 字节, want %d", offset, want)
	}
	tail := iso9660SectorSize - offset
	if tail < 1 || tail > 33 {
		t.Fatalf("夹具没落在触发区间：尾部剩 %d 字节，需要 1..33", tail)
	}

	recs, err := scanDirectorySector(sector)
	if err != nil {
		t.Fatalf("扫描一个尾部剩 %d 字节填充的合法扇区失败: %v —— "+
			"扇区尾部不足一条记录是 ECMA-119 §6.8.1 规定的正常填充，不是损坏；"+
			"在这里报错会让真实 ISO 的导入在第一步就失败，而六个 OS 家族的装机"+
			"链路都以导入为起点", tail, err)
	}
	if len(recs) != 2+fileCount {
		t.Errorf("扫描出 %d 条记录, want %d（含 . 与 ..）", len(recs), 2+fileCount)
	}
}

// TestScanDirectorySector_StopsOnZeroLength 验证长度字节为 0 的哨兵仍然生效
// （那是 ECMA-119 §6.8.1.1 定义的「本扇区剩余是填充」显式信号，与上一条的
// 隐式情形是两个独立的收尾条件，两者都要成立）。
func TestScanDirectorySector_StopsOnZeroLength(t *testing.T) {
	sector := make([]byte, iso9660SectorSize)
	copy(sector, makeDirRecord(t, 34, "\x00", true))
	copy(sector[34:], makeDirRecord(t, 34, "\x01", true))
	// 偏移 68 起全是 0，第一个字节即长度 0 的哨兵。

	recs, err := scanDirectorySector(sector)
	if err != nil {
		t.Fatalf("遇到长度 0 哨兵时报错: %v", err)
	}
	if len(recs) != 2 {
		t.Errorf("扫描出 %d 条记录, want 2", len(recs))
	}
}

// TestScanDirectorySector_RejectsTruncatedRecord 验证真正的损坏仍然被拒绝：
// 一条记录声明的长度超出了扇区剩余空间。
//
// 这条与上面两条是对照组——修掉「把正常填充误判成损坏」不能变成「对真正的
// 损坏也放行」。声明长度 200 但扇区只剩 100 字节，那是真的读不下去。
func TestScanDirectorySector_RejectsTruncatedRecord(t *testing.T) {
	sector := make([]byte, 100)
	rec := makeDirRecord(t, 34, "\x00", true)
	rec[0] = 200 // 声明长度远超扇区剩余
	copy(sector, rec)

	if _, err := scanDirectorySector(sector); err == nil {
		t.Error("一条声明长度 200 字节、而扇区只剩 100 字节的记录被接受了 —— " +
			"那是真正的损坏，必须拒绝；修「正常填充被误判」不能顺手把真损坏也放过")
	}
}
