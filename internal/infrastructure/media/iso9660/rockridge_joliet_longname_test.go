package iso9660

import (
	"io/fs"
	"strings"
	"testing"
)

// 双目录指向同一长文件，但 Joliet 的名字被截短；另一个文件用内容区分目录选择。
func TestRockRidgeLongNameTakesPrecedenceOverJoliet(t *testing.T) {
	longName := strings.Repeat("a", 64) + "-" + strings.Repeat("file", 19) + ".xml.zst"
	shortName := longName[:103]
	continuation := nm(longName)
	primary := entries(
		rrRoot(),
		record([]byte{1}, 20, testSector, 2),
		record([]byte("LONG.;1"), 600, 8, 0, ce(700, 200, uint32(len(continuation)))...),
		record([]byte("CHOICE.;1"), 601, 7, 0, nm("choice.txt")...),
	)
	joliet := entries(
		record([]byte{0}, 400, testSector, 2),
		record([]byte{1}, 400, testSector, 2),
		record(ucs(shortName+";1"), 600, 8, 0),
		record(ucs("choice.txt;1"), 602, 6, 0),
	)
	b := image(primary, joliet)
	copy(b[700*testSector+200:], continuation)
	copy(b[600*testSector:], "metadata")
	copy(b[601*testSector:], "primary")
	copy(b[602*testSector:], "joliet")

	f := mount(t, b)
	requireFile(t, f, longName, "metadata")
	requireFile(t, f, "choice.txt", "primary")
	listed, err := fs.ReadDir(f, ".")
	if err != nil {
		t.Fatal(err)
	}
	foundLong, foundShort := false, false
	for _, entry := range listed {
		foundLong = foundLong || entry.Name() == longName
		foundShort = foundShort || entry.Name() == shortName
	}
	if !foundLong || foundShort {
		t.Fatalf("directory names: full=%t truncated=%t; want full only", foundLong, foundShort)
	}
}
