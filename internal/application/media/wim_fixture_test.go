package media_test

import (
	"encoding/binary"
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

func importWIM() []byte {
	xml := `<WIM><IMAGE INDEX="1"><NAME>Windows Server 2025 Standard</NAME><FLAGS>ServerStandard</FLAGS><WINDOWS><ARCH>9</ARCH><VERSION><MAJOR>10</MAJOR><MINOR>0</MINOR><BUILD>26100</BUILD></VERSION></WINDOWS></IMAGE></WIM>`
	words := utf16.Encode([]rune(xml))
	body := []byte{0xff, 0xfe}
	for _, w := range words {
		body = binary.LittleEndian.AppendUint16(body, w)
	}
	b := make([]byte, 208)
	copy(b, []byte{'M', 'S', 'W', 'I', 'M', 0, 0, 0})
	binary.LittleEndian.PutUint32(b[8:], 208)
	binary.LittleEndian.PutUint32(b[12:], 0x10d00)
	binary.LittleEndian.PutUint32(b[44:], 1)
	binary.LittleEndian.PutUint64(b[72:], uint64(len(body)))
	binary.LittleEndian.PutUint64(b[80:], 208)
	binary.LittleEndian.PutUint64(b[88:], uint64(len(body)))
	return append(b, body...)
}
