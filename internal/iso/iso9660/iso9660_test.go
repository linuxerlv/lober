package iso9660

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
	"testing"
	"testing/fstest"
)

const testSector = 2048

// These fixtures encode the on-disc layout independently of the parser.
func both32(p []byte, v uint32) {
	binary.LittleEndian.PutUint32(p, v)
	binary.BigEndian.PutUint32(p[4:], v)
}
func both16(p []byte, v uint16) {
	binary.LittleEndian.PutUint16(p, v)
	binary.BigEndian.PutUint16(p[2:], v)
}
func record(name []byte, sector, size uint32, flags byte, extra ...byte) []byte {
	n := 33 + len(name)
	if len(name)%2 == 0 {
		n++
	}
	b := make([]byte, n+len(extra))
	b[0] = byte(len(b))
	both32(b[2:10], sector)
	both32(b[10:18], size)
	copy(b[18:25], []byte{126, 10, 13, 1, 2, 3, 0})
	b[25] = flags
	both16(b[28:32], 1)
	b[32] = byte(len(name))
	copy(b[33:], name)
	copy(b[n:], extra)
	return b
}
func entries(records ...[]byte) []byte {
	var b []byte
	for _, r := range records {
		if len(b)%testSector+len(r) > testSector {
			b = append(b, make([]byte, testSector-len(b)%testSector)...)
		}
		b = append(b, r...)
	}
	if len(b)%testSector != 0 {
		b = append(b, make([]byte, testSector-len(b)%testSector)...)
	}
	return b
}
func image(root []byte, joliet []byte) []byte {
	b := make([]byte, 1024*testSector)
	descriptor := func(at int, typ byte, sector uint32, content []byte) {
		d := b[at*testSector : (at+1)*testSector]
		d[0] = typ
		copy(d[1:6], "CD001")
		d[6] = 1
		both32(d[80:88], 1024)
		both16(d[120:124], 1)
		both16(d[124:128], 1)
		both16(d[128:132], testSector)
		copy(d[156:], record([]byte{0}, sector, uint32(len(content)), 2))
		if typ == 2 {
			copy(d[88:91], "%/E")
		}
		copy(b[int(sector)*testSector:], content)
	}
	descriptor(16, 1, 20, root)
	end := 17
	if joliet != nil {
		descriptor(17, 2, 400, joliet)
		end = 18
	}
	b[end*testSector] = 255
	copy(b[end*testSector+1:end*testSector+6], "CD001")
	b[end*testSector+6] = 1
	return b
}
func mount(t *testing.T, b []byte) fs.FS {
	t.Helper()
	f, err := Open(bytes.NewReader(b))
	if err != nil {
		t.Fatal(err)
	}
	return f
}
func requireFile(t *testing.T, f fs.FS, name, want string) {
	t.Helper()
	b, err := fs.ReadFile(f, name)
	if err != nil || string(b) != want {
		t.Fatalf("ReadFile(%q) = %q, %v; want %q", name, b, err, want)
	}
}
func susp(sig string, payload ...byte) []byte {
	return append([]byte{sig[0], sig[1], byte(4 + len(payload)), 1}, payload...)
}
func nm(name string) []byte { return susp("NM", append([]byte{0}, name...)...) }
func px(mode uint32) []byte {
	b := make([]byte, 40)
	both32(b, mode)
	both32(b[8:], 1)
	return susp("PX", b...)
}
func ce(sector, offset, length uint32) []byte {
	b := make([]byte, 24)
	both32(b, sector)
	both32(b[8:], offset)
	both32(b[16:], length)
	return susp("CE", b...)
}
func rrRoot() []byte { return record([]byte{0}, 20, testSector, 2, susp("SP", 0xbe, 0xef, 0)...) }
func sl(target string) []byte {
	b := []byte{0}
	for _, p := range strings.Split(target, "/") {
		flag := byte(0)
		if p == ".." {
			flag = 4
			p = ""
		}
		if p == "." {
			flag = 2
			p = ""
		}
		b = append(b, flag, byte(len(p)))
		b = append(b, p...)
	}
	return susp("SL", b...)
}
func ucs(s string) []byte {
	var b []byte
	for _, r := range s {
		b = append(b, byte(r>>8), byte(r))
	}
	return b
}

func TestPrimaryNamesAndNestedFiles(t *testing.T) {
	dir := entries(record([]byte{0}, 500, testSector, 2), record([]byte{1}, 20, testSector, 2), record([]byte("BOOTX64.EFI;1"), 600, 6, 0))
	b := image(entries(record([]byte{0}, 20, testSector, 2), record([]byte{1}, 20, testSector, 2), record([]byte("EFI"), 500, uint32(len(dir)), 2), record([]byte("README.;1"), 601, 5, 0)), nil)
	copy(b[500*testSector:], dir)
	copy(b[600*testSector:], "kernel")
	copy(b[601*testSector:], "hello")
	f := mount(t, b)
	requireFile(t, f, "efi/bootx64.efi", "kernel")
	requireFile(t, f, "readme", "hello")
	if err := fstest.TestFS(f, "efi", "efi/bootx64.efi", "readme"); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"", "../readme", "/readme", "efi/../readme"} {
		if _, err := f.Open(p); !errors.Is(err, fs.ErrInvalid) {
			t.Errorf("Open(%q): %v", p, err)
		}
	}
}
func TestJolietPreferredAndCasePreserved(t *testing.T) {
	p := entries(rrRoot(), record([]byte("PRIMARY.;1"), 600, 7, 0, nm("RockRidge")...))
	j := entries(record([]byte{0}, 400, testSector, 2), record(ucs("Mixed安装.TXT;1"), 601, 6, 0))
	b := image(p, j)
	copy(b[600*testSector:], "primary")
	copy(b[601*testSector:], "joliet")
	f := mount(t, b)
	requireFile(t, f, "Mixed安装.TXT", "joliet")
	if _, err := f.Open("RockRidge"); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal(err)
	}
}
func TestRockRidgeNamesPermissionsLinksAndContinuation(t *testing.T) {
	extra := append(nm("MixedCase"), px(0100751)...)
	link := append(nm("Link"), px(0120777)...)
	link = append(link, sl("MixedCase")...)
	continued := nm("ContinuedName")
	b := image(entries(rrRoot(), record([]byte("FILE.;1"), 600, 7, 0, extra...), record([]byte("LINK.;1"), 0, 0, 0, link...), record([]byte("LONG.;1"), 601, 5, 0, ce(700, 200, uint32(len(continued)))...)), nil)
	copy(b[600*testSector:], "payload")
	copy(b[601*testSector:], "other")
	copy(b[700*testSector+200:], continued)
	f := mount(t, b)
	requireFile(t, f, "MixedCase", "payload")
	requireFile(t, f, "Link", "payload")
	requireFile(t, f, "ContinuedName", "other")
	got, err := fs.ReadLink(f, "Link")
	if err != nil || got != "MixedCase" {
		t.Fatalf("ReadLink = %q %v", got, err)
	}
	info, err := fs.Stat(f, "MixedCase")
	if err != nil || info.Mode().Perm() != 0751 {
		t.Fatalf("mode: %v %v", info, err)
	}
	var paths []string
	if err := fs.WalkDir(f, ".", func(p string, d fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		paths = append(paths, p)
		if p == "Link" && d.Type()&fs.ModeSymlink == 0 {
			t.Error("link loses symlink type")
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if fmt.Sprint(paths) != "[. ContinuedName Link MixedCase]" {
		t.Fatal(paths)
	}
}
func TestLargeDirectoryAndSectorPadding(t *testing.T) {
	r := [][]byte{record([]byte{0}, 20, 0, 2)}
	for i := 0; i < 12000; i++ {
		r = append(r, record([]byte(fmt.Sprintf("F%05d.TXT;1", i)), 900, 1, 0))
	}
	d := entries(r...)
	b := image(d, nil)
	copy(b[900*testSector:], "x")
	f := mount(t, b)
	list, err := fs.ReadDir(f, ".")
	if err != nil || len(list) != 12000 {
		t.Fatalf("entries: %d %v", len(list), err)
	}
	requireFile(t, f, "f11999.txt", "x")
}
func TestMultiExtentFile(t *testing.T) {
	b := image(entries(record([]byte{0}, 20, testSector, 2), record([]byte("BIG.BIN;1"), 600, testSector, 0x80), record([]byte("BIG.BIN;1"), 602, 4, 0)), nil)
	copy(b[600*testSector:601*testSector], bytes.Repeat([]byte{'a'}, testSector))
	copy(b[602*testSector:], "tail")
	f := mount(t, b)
	requireFile(t, f, "big.bin", strings.Repeat("a", testSector)+"tail")
}
func TestRejectsMalformedImagesAndPaths(t *testing.T) {
	base := image(entries(record([]byte{0}, 20, testSector, 2), record([]byte("OK;1"), 600, 1, 0)), nil)
	for _, tc := range []struct {
		name string
		edit func([]byte)
	}{
		{"signature", func(b []byte) { b[16*testSector+1] = 'X' }},
		{"root out of bounds", func(b []byte) { both32(b[16*testSector+158:], 0xffffffff) }},
		{"block size", func(b []byte) { both16(b[16*testSector+128:], 0) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := bytes.Clone(base)
			tc.edit(b)
			if _, err := Open(bytes.NewReader(b)); err == nil {
				t.Fatal("accepted malformed image")
			}
		})
	}
	for _, b := range [][]byte{nil, make([]byte, 100), make([]byte, 17*testSector)} {
		if _, err := Open(bytes.NewReader(b)); err == nil {
			t.Fatal("accepted short/non-ISO input")
		}
	}
	for _, tc := range []struct {
		name string
		r    []byte
	}{
		{"truncated record", []byte{20}},
		{"file extent", record([]byte("BAD;1"), 0xffffffff, 0xffffffff, 0)},
		{"path traversal", record([]byte("BAD;1"), 600, 0, 0, nm("../escape")...)},
		{"CE cycle", record([]byte("BAD;1"), 600, 0, 0, ce(700, 0, 28)...)},
		{"CE allocation", record([]byte("BAD;1"), 600, 0, 0, ce(700, 0, 0xffffffff)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			b := image(entries(rrRoot(), tc.r), nil)
			copy(b[700*testSector:], ce(700, 0, 28))
			f := mount(t, b)
			if _, err := fs.ReadDir(f, "."); err == nil {
				t.Fatal("accepted bad directory")
			}
		})
	}
}
func TestSymlinksDoNotEscapeOrLoop(t *testing.T) {
	r := [][]byte{rrRoot()}
	for _, v := range []struct{ name, target string }{{"escape", "../outside"}, {"a", "b"}, {"b", "a"}} {
		x := append(nm(v.name), px(0120777)...)
		x = append(x, sl(v.target)...)
		r = append(r, record([]byte(strings.ToUpper(v.name)), 0, 0, 0, x...))
	}
	f := mount(t, image(entries(r...), nil))
	for _, p := range []string{"escape", "a"} {
		if _, err := f.Open(p); err == nil {
			t.Fatalf("Open(%s) accepted unsafe/cyclic link", p)
		}
	}
	if target, err := fs.ReadLink(f, "escape"); err != nil || target != "../outside" {
		t.Fatalf("ReadLink = %q %v", target, err)
	}
}

type failingReader struct{}

func (failingReader) Size() int64                       { return 100 * testSector }
func (failingReader) ReadAt([]byte, int64) (int, error) { return 0, io.ErrUnexpectedEOF }
func TestReadFailure(t *testing.T) {
	if _, err := Open(failingReader{}); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatal(err)
	}
}

func TestDirectoryRecordStraddlesSector(t *testing.T) {
	// A defensive reader also accepts a record straddling a sector, rather than
	// silently dropping its tail. Normal directory padding is tested separately.
	d := record([]byte{0}, 20, 4096, 2)
	for len(d) < 2030 {
		left := 2030 - len(d)
		n := min(left, 255)
		if left-n > 0 && left-n < 34 {
			n -= 34 - (left - n)
		}
		padding := make([]byte, n-34)
		d = append(d, record([]byte{1}, 20, 4096, 2, padding...)...)
	}
	d = append(d, record([]byte("CROSS.TXT;1"), 600, 5, 0)...)
	d = append(d, make([]byte, 4096-len(d))...)
	b := image(d, nil)
	copy(b[600*testSector:], "cross")
	requireFile(t, mount(t, b), "cross.txt", "cross")
}

func TestFileReaderSeekingAndDirectoryPagination(t *testing.T) {
	b := image(entries(record([]byte{0}, 20, testSector, 2), record([]byte("DATA;1"), 600, 6, 0), record([]byte("SECOND;1"), 601, 1, 0)), nil)
	copy(b[600*testSector:], "abcdef")
	f := mount(t, b)
	file, err := f.Open("data")
	if err != nil {
		t.Fatal(err)
	}
	ra, ok := file.(io.ReaderAt)
	if !ok {
		t.Fatal("file does not support ReaderAt")
	}
	got := make([]byte, 4)
	n, err := ra.ReadAt(got, 3)
	if n != 3 || err != io.EOF || string(got[:n]) != "def" {
		t.Fatalf("ReadAt %q %d %v", got, n, err)
	}
	seek := file.(io.Seeker)
	if _, err := seek.Seek(-2, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	n, err = file.Read(got)
	if n != 2 || err != io.EOF || string(got[:n]) != "ef" {
		t.Fatalf("Read after seek %q %d %v", got, n, err)
	}
	file.Close()
	if _, err := file.Read(got); !errors.Is(err, fs.ErrClosed) {
		t.Fatal(err)
	}
	dir, err := f.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer dir.Close()
	rd := dir.(fs.ReadDirFile)
	for _, name := range []string{"data", "second"} {
		entries, err := rd.ReadDir(1)
		if err != nil || len(entries) != 1 || entries[0].Name() != name {
			t.Fatalf("ReadDir(1) %v %v", entries, err)
		}
	}
	if _, err := rd.ReadDir(1); err != io.EOF {
		t.Fatal(err)
	}
}

func TestNestedRelativeSymlinkAndNonTraversedDirectoryLink(t *testing.T) {
	x := append(nm("Alias"), px(0120777)...)
	x = append(x, sl("../Payload")...)
	dir := entries(record([]byte{0}, 500, testSector, 2), record([]byte{1}, 20, testSector, 2), record([]byte("LINK;1"), 0, 0, 0, x...))
	b := image(entries(rrRoot(), record([]byte("DIR"), 500, uint32(len(dir)), 2, nm("Dir")...), record([]byte("FILE;1"), 600, 7, 0, nm("Payload")...)), nil)
	copy(b[500*testSector:], dir)
	copy(b[600*testSector:], "payload")
	f := mount(t, b)
	requireFile(t, f, "Dir/Alias", "payload")
	info, err := fs.Lstat(f, "Dir/Alias")
	if err != nil || info.Mode()&fs.ModeSymlink == 0 {
		t.Fatalf("Lstat %v %v", info, err)
	}
}

func TestMalformedMultiExtentAndDirectoryCycles(t *testing.T) {
	for _, tc := range []struct {
		name   string
		record []byte
	}{
		{"unfinished multi extent", record([]byte("BIG;1"), 600, testSector, 0x80)},
		{"directory cycle", record([]byte("LOOP"), 20, testSector, 2)},
		{"duplicate names", append(record([]byte("SAME;1"), 600, 0, 0), record([]byte("SAME;1"), 601, 0, 0)...)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := mount(t, image(entries(record([]byte{0}, 20, testSector, 2), tc.record), nil))
			if _, err := fs.ReadDir(f, "."); err == nil {
				t.Fatal("accepted malformed directory")
			}
		})
	}
}

type countingReader struct {
	*bytes.Reader
	reads int
}

func (r *countingReader) ReadAt(b []byte, off int64) (int, error) {
	r.reads++
	return r.Reader.ReadAt(b, off)
}
func TestDirectoryReadsAreBufferedBySector(t *testing.T) {
	records := [][]byte{record([]byte{0}, 20, testSector, 2)}
	for i := 0; i < 1000; i++ {
		records = append(records, record([]byte(fmt.Sprintf("F%04d;1", i)), 600, 0, 0))
	}
	d := entries(records...)
	r := &countingReader{Reader: bytes.NewReader(image(d, nil))}
	f, err := Open(r)
	if err != nil {
		t.Fatal(err)
	}
	r.reads = 0
	list, err := fs.ReadDir(f, ".")
	if err != nil || len(list) != 1000 {
		t.Fatalf("ReadDir: %d %v", len(list), err)
	}
	if r.reads > len(d)/testSector+1 {
		t.Fatalf("directory uses %d random reads for %d sectors", r.reads, len(d)/testSector)
	}
}

func TestJolietSemicolonWithoutVersionRemainsInName(t *testing.T) {
	b := image(entries(record([]byte{0}, 20, testSector, 2)), entries(record([]byte{0}, 400, testSector, 2), record(ucs("Semi;Colon"), 600, 1, 0)))
	copy(b[600*testSector:], "x")
	requireFile(t, mount(t, b), "Semi;Colon", "x")
}

func TestRockRidgeRejectsUnfinishedNamesAndLinks(t *testing.T) {
	for _, tc := range []struct {
		name  string
		extra []byte
	}{
		{"name", susp("NM", append([]byte{1}, "Partial"...)...)},
		{"link", susp("SL", 1, 0, 6, 't', 'a', 'r', 'g', 'e', 't')},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := mount(t, image(entries(rrRoot(), record([]byte("FILE;1"), 600, 0, 0, tc.extra...)), nil))
			if _, err := fs.ReadDir(f, "."); err == nil {
				t.Fatal("accepted unterminated Rock Ridge continuation")
			}
		})
	}
}

func FuzzISO(f *testing.F) {
	f.Add(image(entries(record([]byte{0}, 20, testSector, 2), record([]byte("FILE;1"), 600, 1, 0)), nil))
	f.Add(image(entries(rrRoot(), record([]byte("BAD;1"), 600, 0, 0, ce(700, 0, 0xffffffff)...)), nil))
	f.Add([]byte("not an ISO"))
	f.Fuzz(func(t *testing.T, b []byte) {
		if len(b) > 4<<20 {
			return
		}
		filesystem, err := Open(bytes.NewReader(b))
		if err == nil {
			_, _ = fs.ReadDir(filesystem, ".")
		}
	})
}
