package iso

import (
	"bytes"
	"encoding/binary"
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/lober/lober/internal/application/media/port"
	"github.com/lober/lober/internal/domain/catalog"
)

// WIMImage is one image from the WIM XML index. Version excludes BUILD as
// required by the media contract; Arch is empty for unsupported architectures.
type WIMImage struct {
	Index       int
	Name, Flags string
	Version     string
	Arch        catalog.Arch
}

const wimXMLLimit = 16 << 20

// WIMImages reads just the header and XML resource, never the image contents.
// Resource flags, rather than the global compression flag, determine whether
// XML needs decompression. The resource layout follows Microsoft's WIM reader:
// https://github.com/microsoft/go-winio/blob/main/wim/wim.go
func WIMImages(r io.ReaderAt, size int64) ([]WIMImage, error) {
	if r == nil || size < 0 {
		return nil, fmt.Errorf("WIMImages: reader and nonnegative size are required")
	}
	bad := func() error { return port.ErrWIMMalformed }
	if size < 208 {
		return nil, bad()
	}
	h := make([]byte, 208)
	if _, err := r.ReadAt(h, 0); err != nil {
		return nil, err
	}
	if string(h[:8]) != "MSWIM\x00\x00\x00" || binary.LittleEndian.Uint32(h[8:]) < 208 || int64(binary.LittleEndian.Uint32(h[8:])) > size {
		return nil, bad()
	}
	packed := binary.LittleEndian.Uint64(h[72:])
	flags := byte(packed >> 56)
	n := packed & 0x00ffffffffffffff
	off := binary.LittleEndian.Uint64(h[80:])
	original := binary.LittleEndian.Uint64(h[88:])
	if n == 0 || n > wimXMLLimit || original == 0 || original > wimXMLLimit || off < 208 || off > uint64(size) || n > uint64(size)-off {
		return nil, bad()
	}
	if flags&^byte(6) != 0 {
		return nil, bad()
	}
	data := make([]byte, int(n))
	if _, err := r.ReadAt(data, int64(off)); err != nil {
		return nil, err
	}
	if flags&4 != 0 {
		compression := binary.LittleEndian.Uint32(h[16:]) & 0x001e0000
		if compression != 0x00020000 {
			return nil, port.ErrWIMCompressed
		}
		chunk := binary.LittleEndian.Uint32(h[20:])
		if chunk != 32768 {
			return nil, port.ErrWIMCompressed
		}
		var err error
		data, err = wimXPRESSResource(data, int(original), int(chunk))
		if err != nil {
			return nil, bad()
		}
	} else if n != original {
		return nil, bad()
	}
	var err error
	data, err = wimXMLUTF8(data)
	if err != nil {
		return nil, bad()
	}
	var doc struct {
		XMLName xml.Name `xml:"WIM"`
		Images  []struct {
			Index   int    `xml:"INDEX,attr"`
			Name    string `xml:"NAME"`
			Flags   string `xml:"FLAGS"`
			Windows struct {
				Arch    string `xml:"ARCH"`
				Version struct {
					Major string `xml:"MAJOR"`
					Minor string `xml:"MINOR"`
				} `xml:"VERSION"`
			} `xml:"WINDOWS"`
		} `xml:"IMAGE"`
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	// BOM conversion already supplied UTF-8; declarations may still say UTF-16.
	decoder.CharsetReader = func(name string, in io.Reader) (io.Reader, error) {
		if strings.EqualFold(name, "utf-16") || strings.EqualFold(name, "utf-8") {
			return in, nil
		}
		return nil, fmt.Errorf("unsupported XML charset %s", name)
	}
	// Count images before allocating the decoded collection. Match XML local
	// names rather than raw text, so namespace prefixes cannot bypass the cap.
	count := 0
	for {
		token, scanErr := decoder.Token()
		if scanErr == io.EOF {
			break
		}
		if scanErr != nil {
			return nil, bad()
		}
		if element, ok := token.(xml.StartElement); ok && element.Name.Local == "IMAGE" {
			count++
			if count > 4096 {
				return nil, bad()
			}
		}
	}
	charsetReader := decoder.CharsetReader
	decoder = xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charsetReader
	if err = decoder.Decode(&doc); err != nil || len(doc.Images) == 0 {
		return nil, bad()
	}
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, bad()
		}
		if text, ok := token.(xml.CharData); !ok || strings.TrimSpace(string(text)) != "" {
			return nil, bad()
		}
	}
	images := make([]WIMImage, 0, len(doc.Images))
	seen := map[int]bool{}
	for _, image := range doc.Images {
		if image.Index <= 0 || seen[image.Index] {
			return nil, bad()
		}
		seen[image.Index] = true
		out := WIMImage{Index: image.Index, Name: image.Name, Flags: image.Flags}
		major, err1 := strconv.ParseUint(strings.TrimSpace(image.Windows.Version.Major), 10, 32)
		minor, err2 := strconv.ParseUint(strings.TrimSpace(image.Windows.Version.Minor), 10, 32)
		if err1 == nil && err2 == nil {
			out.Version = strconv.FormatUint(major, 10) + "." + strconv.FormatUint(minor, 10)
		}
		switch strings.TrimSpace(image.Windows.Arch) {
		case "9":
			out.Arch = "x86_64"
		case "12":
			out.Arch = "aarch64"
		}
		images = append(images, out)
	}
	return images, nil
}

func wimXMLUTF8(data []byte) ([]byte, error) {
	if bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf}) {
		return data[3:], nil
	}
	if len(data) < 2 || !(data[0] == 0xff && data[1] == 0xfe || data[0] == 0xfe && data[1] == 0xff) {
		return data, nil
	}
	if len(data)%2 != 0 {
		return nil, io.ErrUnexpectedEOF
	}
	var order binary.ByteOrder = binary.LittleEndian
	if data[0] == 0xfe {
		order = binary.BigEndian
	}
	u := make([]uint16, (len(data)-2)/2)
	for i := range u {
		u[i] = order.Uint16(data[2+i*2:])
	}
	for i := 0; i < len(u); i++ {
		if u[i] >= 0xd800 && u[i] <= 0xdbff {
			if i+1 >= len(u) || u[i+1] < 0xdc00 || u[i+1] > 0xdfff {
				return nil, fmt.Errorf("invalid UTF-16 surrogate")
			}
			i++
		} else if u[i] >= 0xdc00 && u[i] <= 0xdfff {
			return nil, fmt.Errorf("invalid UTF-16 surrogate")
		}
	}
	return []byte(string(utf16.Decode(u))), nil
}

// WIM chunk offsets are relative to the end of their table; equal compressed
// and original chunk sizes mean raw data, even for a compressed resource.
func wimXPRESSResource(data []byte, original, chunk int) ([]byte, error) {
	count := (original + chunk - 1) / chunk
	table := (count - 1) * 4
	if table > len(data) {
		return nil, io.ErrUnexpectedEOF
	}
	out := make([]byte, 0, original)
	start := table
	for i := 0; i < count; i++ {
		end := len(data)
		if i < count-1 {
			v := uint64(binary.LittleEndian.Uint32(data[i*4:]))
			if v > uint64(len(data)-table) {
				return nil, io.ErrUnexpectedEOF
			}
			end = table + int(v)
		}
		if end <= start {
			return nil, fmt.Errorf("invalid WIM chunk offsets")
		}
		want := min(chunk, original-len(out))
		src := data[start:end]
		if len(src) == want {
			out = append(out, src...)
		} else {
			decoded, err := wimXPRESS(src, want)
			if err != nil {
				return nil, err
			}
			out = append(out, decoded...)
		}
		start = end
	}
	return out, nil
}

// XPRESS uses canonical Huffman codes over 512 literal/match symbols. Bit
// words are little endian, with the most significant bit consumed first.
// https://learn.microsoft.com/en-us/openspecs/windows_protocols/ms-xca/c7ec7ba9-ca8f-448f-bb85-027c1516db1c
func wimXPRESS(src []byte, want int) ([]byte, error) {
	if len(src) < 260 || want <= 0 || want > 32768 {
		return nil, io.ErrUnexpectedEOF
	}
	var lengths [512]uint8
	var counts [16]int
	for i := range lengths {
		lengths[i] = src[i/2] >> uint((i%2)*4) & 15
		if lengths[i] != 0 {
			counts[lengths[i]]++
		}
	}
	var codes [16]int
	code := 0
	for n := 1; n < 16; n++ {
		code = (code + counts[n-1]) << 1
		codes[n] = code
		if code+counts[n] > 1<<n {
			return nil, fmt.Errorf("oversubscribed XPRESS tree")
		}
	}
	if code+counts[15] != 1<<15 {
		return nil, fmt.Errorf("incomplete XPRESS tree")
	}
	var table [32768]uint16
	for symbol, n := range lengths {
		if n == 0 {
			continue
		}
		start := codes[n] << uint(15-n)
		end := start + 1<<uint(15-n)
		for i := start; i < end; i++ {
			table[i] = uint16(symbol<<4) | uint16(n)
		}
		codes[n]++
	}
	bits := wimBits{src: src, pos: 260, word: uint32(binary.LittleEndian.Uint16(src[256:]))<<16 | uint32(binary.LittleEndian.Uint16(src[258:])), remaining: 32}
	out := make([]byte, 0, want)
	for len(out) < want {
		entry := table[bits.word>>17]
		n := int(entry & 15)
		symbol := int(entry >> 4)
		if n == 0 {
			return nil, fmt.Errorf("invalid XPRESS symbol")
		}
		if err := bits.skip(n); err != nil {
			return nil, err
		}
		if symbol < 256 {
			out = append(out, byte(symbol))
			continue
		}
		symbol -= 256
		length := symbol & 15
		offsetBits := symbol >> 4
		if length == 15 {
			if bits.pos >= len(src) {
				return nil, io.ErrUnexpectedEOF
			}
			length = int(src[bits.pos]) + 15
			bits.pos++
			if length == 270 {
				if bits.pos+2 > len(src) {
					return nil, io.ErrUnexpectedEOF
				}
				length = int(binary.LittleEndian.Uint16(src[bits.pos:]))
				bits.pos += 2
				if length == 0 {
					if bits.pos+4 > len(src) {
						return nil, io.ErrUnexpectedEOF
					}
					value := uint64(binary.LittleEndian.Uint32(src[bits.pos:]))
					bits.pos += 4
					if value > uint64(want) {
						return nil, fmt.Errorf("XPRESS match too long")
					}
					length = int(value)
				}
				if length < 270 {
					return nil, fmt.Errorf("invalid XPRESS extended length")
				}
			}
		}
		distance := 1 << uint(offsetBits)
		if offsetBits > 0 {
			distance += int(bits.word >> uint(32-offsetBits))
			if err := bits.skip(offsetBits); err != nil {
				return nil, err
			}
		}
		length += 3
		if distance > len(out) || length > want-len(out) {
			return nil, fmt.Errorf("XPRESS match outside output")
		}
		for j := 0; j < length; j++ {
			out = append(out, out[len(out)-distance])
		}
	}
	return out, nil
}

type wimBits struct {
	src       []byte
	pos       int
	word      uint32
	remaining int
}

func (b *wimBits) skip(n int) error {
	if n > b.remaining {
		return io.ErrUnexpectedEOF
	}
	b.word <<= uint(n)
	b.remaining -= n
	if b.remaining < 16 {
		if b.pos+2 > len(b.src) {
			return io.ErrUnexpectedEOF
		}
		b.word |= uint32(binary.LittleEndian.Uint16(b.src[b.pos:])) << uint(16-b.remaining)
		b.pos += 2
		b.remaining += 16
	}
	return nil
}
