package iso

import (
	"bytes"
	"encoding/binary"
	"errors"
	"github.com/lober/lober/internal/application/media/port"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"
)

func makeWIM(resource []byte, flags uint32, compressed bool, chunkSize, original int) []byte {
	b := make([]byte, 208+len(resource))
	copy(b, "MSWIM\x00\x00\x00")
	binary.LittleEndian.PutUint32(b[8:], 208)
	binary.LittleEndian.PutUint32(b[12:], 0x10d00)
	binary.LittleEndian.PutUint32(b[16:], flags)
	binary.LittleEndian.PutUint32(b[20:], uint32(chunkSize))
	binary.LittleEndian.PutUint16(b[40:], 1)
	binary.LittleEndian.PutUint16(b[42:], 1)
	n := uint64(len(resource))
	if compressed {
		n |= 4 << 56
	}
	binary.LittleEndian.PutUint64(b[72:], n)
	binary.LittleEndian.PutUint64(b[80:], 208)
	binary.LittleEndian.PutUint64(b[88:], uint64(original))
	copy(b[208:], resource)
	return b
}

func TestWIMXPRESSMatchesAndBounds(t *testing.T) {
	// Fixed 9-bit codes: literal 'a' followed by distance=1,length=5
	// symbol 258. Bits are 001100001 100000010, in little-endian words.
	packed := append(bytes.Repeat([]byte{0x99}, 256), 0xc0, 0x30, 0x00, 0x80, 0x00, 0x00)
	got, err := wimXPRESS(packed, 6)
	if err != nil || string(got) != "aaaaaa" {
		t.Fatalf("got %q, %v", got, err)
	}
	if _, err := wimXPRESS(packed, 5); err == nil {
		t.Fatal("accepted match beyond declared output")
	}
	for i := 0; i < 260; i++ {
		if _, err := wimXPRESS(packed[:i], 6); err == nil {
			t.Fatalf("accepted short stream %d", i)
		}
	}
	bad := bytes.Clone(packed)
	for i := 0; i < 256; i++ {
		bad[i] = 0xff
	}
	if _, err := wimXPRESS(bad, 6); err == nil {
		t.Fatal("accepted oversubscribed table")
	}
	bad = bytes.Clone(packed)
	for i := 0; i < 256; i++ {
		bad[i] = 0
	}
	if _, err := wimXPRESS(bad, 6); err == nil {
		t.Fatal("accepted empty table")
	}
}

func TestWIMXPRESSMultiChunkResource(t *testing.T) {
	xml := []byte(`<WIM><IMAGE INDEX="1"><NAME>Windows</NAME><FLAGS>Core</FLAGS><DESCRIPTION>` + strings.Repeat("a", 40000) + `</DESCRIPTION></IMAGE></WIM>`)
	first := wimLiteralXPRESS(xml[:32768])
	second := wimLiteralXPRESS(xml[32768:])
	resource := binary.LittleEndian.AppendUint32(nil, uint32(len(first)))
	resource = append(resource, first...)
	resource = append(resource, second...)
	b := makeWIM(resource, 0x20002, true, 32768, len(xml))
	got, err := WIMImages(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(got) != 1 {
		t.Fatalf("got %+v, %v", got, err)
	}
	for _, offset := range []uint32{0, ^uint32(0)} {
		bad := bytes.Clone(b)
		binary.LittleEndian.PutUint32(bad[208:], offset)
		if _, err := WIMImages(bytes.NewReader(bad), int64(len(bad))); err == nil {
			t.Fatal("accepted bad chunk table")
		}
	}
}

func TestWIMRejectsDuplicateImageIndexAndTrailingXML(t *testing.T) {
	for _, xml := range []string{`<WIM><IMAGE INDEX="1"/><IMAGE INDEX="1"/></WIM>`, `<WIM><IMAGE INDEX="1"/></WIM><WIM/>`, `<WIM><IMAGE INDEX="0"/></WIM>`} {
		b := makeWIM([]byte(xml), 0, false, 32768, len(xml))
		if _, err := WIMImages(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Fatal("accepted malformed XML")
		}
	}
}

func TestWIMCapsImageCount(t *testing.T) {
	var xml strings.Builder
	xml.WriteString("<WIM>")
	for i := 1; i <= 4097; i++ {
		xml.WriteString(`<IMAGE INDEX="` + strconv.Itoa(i) + `"><NAME>Windows</NAME><FLAGS>Core</FLAGS></IMAGE>`)
	}
	xml.WriteString("</WIM>")
	data := []byte(xml.String())
	b := makeWIM(data, 0, false, 32768, len(data))
	if _, err := WIMImages(bytes.NewReader(b), int64(len(b))); err == nil {
		t.Fatal("accepted unbounded image collection")
	}
}

func TestWIMXPRESSExtendedLengths(t *testing.T) {
	// A complete two-symbol tree: 'a'=0 and length-extension match=1.
	base := make([]byte, 260)
	base[97/2] = 0x10
	base[271/2] = 0x10
	binary.LittleEndian.PutUint16(base[256:], 0x4000)
	for _, tc := range []struct {
		want      int
		extension []byte
	}{{30, []byte{11}}, {1000, []byte{255, 0xe4, 0x03}}} {
		src := append(bytes.Clone(base), tc.extension...)
		got, err := wimXPRESS(src, tc.want)
		if err != nil || !bytes.Equal(got, bytes.Repeat([]byte{'a'}, tc.want)) {
			t.Fatalf("want %d got %d, %v", tc.want, len(got), err)
		}
	}
	for _, extension := range [][]byte{nil, {255}, {255, 0, 0}, {255, 0, 0, 0xff, 0xff, 0xff, 0xff}} {
		if _, err := wimXPRESS(append(bytes.Clone(base), extension...), 1000); err == nil {
			t.Fatalf("accepted bad extension %x", extension)
		}
	}
}

func FuzzWIMImages(f *testing.F) {
	xml := []byte(`<WIM><IMAGE INDEX="1"><NAME>Windows</NAME><FLAGS>Core</FLAGS></IMAGE></WIM>`)
	f.Add(makeWIM(xml, 0, false, 32768, len(xml)))
	f.Add(makeWIM(wimLiteralXPRESS(xml), 0x20002, true, 32768, len(xml)))
	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > wimXMLLimit+208 {
			return
		}
		_, _ = WIMImages(bytes.NewReader(data), int64(len(data)))
	})
}

func TestWIMPlainXMLAndUTF16(t *testing.T) {
	xml := `<WIM><IMAGE INDEX="1"><NAME>Windows Server 2025</NAME><FLAGS>ServerStandard</FLAGS></IMAGE><IMAGE INDEX="2"><NAME>Windows 11 Pro</NAME><FLAGS>Professional</FLAGS></IMAGE></WIM>`
	u := []byte{0xff, 0xfe}
	for _, c := range utf16.Encode([]rune(xml)) {
		u = binary.LittleEndian.AppendUint16(u, c)
	}
	for _, data := range [][]byte{[]byte(xml), u} {
		// Whole-WIM LZX compression does not imply XML resource compression.
		b := makeWIM(data, 0x40002, false, 32768, len(data))
		got, err := WIMImages(bytes.NewReader(b), int64(len(b)))
		if err != nil || len(got) != 2 || got[0].Index != 1 || got[0].Flags != "ServerStandard" || got[1].Name != "Windows 11 Pro" {
			t.Fatalf("got %+v, %v", got, err)
		}
	}
}

// Independent fixed-width canonical Huffman fixture: every symbol has length 9.
func wimLiteralXPRESS(data []byte) []byte {
	out := bytes.Repeat([]byte{0x99}, 256)
	var word uint16
	used := 0
	for _, c := range data {
		for i := 8; i >= 0; i-- {
			word = word<<1 | uint16(c>>i&1)
			used++
			if used == 16 {
				out = binary.LittleEndian.AppendUint16(out, word)
				word = 0
				used = 0
			}
		}
	}
	if used > 0 {
		out = binary.LittleEndian.AppendUint16(out, word<<uint(16-used))
	}
	return append(out, 0, 0, 0, 0)
}

func TestWIMXPRESSResource(t *testing.T) {
	xml := []byte(`<WIM><IMAGE INDEX="1"><NAME>Windows 11</NAME><FLAGS>Core</FLAGS></IMAGE></WIM>`)
	packed := wimLiteralXPRESS(xml)
	b := makeWIM(packed, 0x20002, true, 32768, len(xml))
	got, err := WIMImages(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(got) != 1 || got[0].Flags != "Core" {
		t.Fatalf("got %+v, %v", got, err)
	}
	// A compressed resource may contain raw chunks where compression was not useful.
	b = makeWIM(xml, 0x20002, true, 32768, len(xml))
	if _, err = WIMImages(bytes.NewReader(b), int64(len(b))); err != nil {
		t.Fatal(err)
	}
}

func TestWIMRejectsUnsupportedAndCorruptResources(t *testing.T) {
	xml := []byte(`<WIM><IMAGE INDEX="1"><NAME>Windows</NAME><FLAGS>Core</FLAGS></IMAGE></WIM>`)
	lzx := makeWIM(xml, 0x40002, true, 32768, len(xml))
	_, err := WIMImages(bytes.NewReader(lzx), int64(len(lzx)))
	if !errors.Is(err, port.ErrWIMCompressed) {
		t.Fatal(err)
	}
	valid := makeWIM(xml, 0, false, 32768, len(xml))
	for _, mutate := range []func([]byte){
		func(b []byte) { b[0] = 'x' }, func(b []byte) { binary.LittleEndian.PutUint64(b[80:], ^uint64(0)) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[88:], 1<<50) }, func(b []byte) { binary.LittleEndian.PutUint64(b[72:], 1<<50) },
		func(b []byte) { binary.LittleEndian.PutUint64(b[88:], 1) },
	} {
		b := bytes.Clone(valid)
		mutate(b)
		if _, err := WIMImages(bytes.NewReader(b), int64(len(b))); err == nil {
			t.Fatal("accepted corrupt WIM")
		}
	}
	for i := 0; i < len(valid); i++ {
		if _, err := WIMImages(bytes.NewReader(valid[:i]), int64(i)); err == nil {
			t.Fatalf("accepted truncation %d", i)
		}
	}
}

func TestWIM不把三十二位映射成六十四位(t *testing.T) {
	xml := `<WIM><IMAGE INDEX="1"><NAME>Windows 10 Home x86</NAME><FLAGS>Core</FLAGS><WINDOWS><ARCH>0</ARCH><VERSION><MAJOR>10</MAJOR><MINOR>0</MINOR></VERSION></WINDOWS></IMAGE></WIM>`
	b := makeWIM([]byte(xml), 0, false, 32768, len(xml))
	images, err := WIMImages(bytes.NewReader(b), int64(len(b)))
	if err != nil || len(images) != 1 || images[0].Arch != "" {
		t.Fatalf("32-bit WIM incorrectly mapped: %+v, %v", images, err)
	}
}
