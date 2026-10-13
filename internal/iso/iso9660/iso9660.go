package iso9660

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"io/fs"
	"path"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

// SizeReader provides random access and the complete image length.
type SizeReader interface {
	io.ReaderAt
	Size() int64
}

// FileReader adapts an open file using the length from its Stat result.
// The caller owns the reader and must keep it open while using the filesystem.
type FileReader struct {
	io.ReaderAt
	Length int64
}

func (r FileReader) Size() int64 { return r.Length }

// Open opens a read-only ISO filesystem, preferring Joliet names, then Rock
// Ridge names, then lowercased ISO names. It does not take ownership of r.
func Open(r SizeReader) (fs.FS, error) {
	if r == nil || r.Size() < 18*2048 {
		return nil, fmt.Errorf("ISO image is too short")
	}
	var primary, joliet []byte
	terminated := false
	for sector := int64(16); sector < 16+256; sector++ {
		b := make([]byte, 2048)
		if err := readFull(r, b, sector*2048, r.Size()); err != nil {
			return nil, err
		}
		if string(b[1:6]) != "CD001" || b[6] != 1 {
			return nil, fmt.Errorf("invalid ISO volume descriptor at sector %d", sector)
		}
		if b[0] == 255 {
			terminated = true
			break
		}
		if b[0] == 1 && primary == nil {
			primary = b
		}
		if b[0] == 2 && b[88] == '%' && b[89] == '/' && strings.ContainsRune("@CE", rune(b[90])) {
			joliet = b
		}
	}
	if !terminated || primary == nil {
		return nil, fmt.Errorf("missing primary volume descriptor or terminator")
	}
	d := primary
	if joliet != nil {
		d = joliet
	}
	block, err := number16(d[128:132])
	if err != nil || block < 512 || block > 2048 || 2048%block != 0 {
		return nil, fmt.Errorf("invalid ISO logical block size")
	}
	volume, err := number32(d[80:88])
	if err != nil || volume == 0 || int64(volume)*int64(block) > r.Size() {
		return nil, fmt.Errorf("ISO volume exceeds image")
	}
	f := &filesystem{r: r, limit: int64(volume) * int64(block), block: int64(block), joliet: joliet != nil}
	length := int(d[156])
	if length < 34 || length > len(d)-156 {
		return nil, fmt.Errorf("invalid root directory record")
	}
	root, _, err := f.parseRecord(d[156:156+length], nil, false)
	if err != nil {
		return nil, err
	}
	if !root.mode.IsDir() || root.size == 0 {
		return nil, fmt.Errorf("invalid root directory")
	}
	root.name = "."
	f.root = root
	// SUSP's SP entry in the root self record enables Rock Ridge and specifies
	// the reserved prefix of the System Use Area for all other records.
	if !f.joliet {
		raw, err := f.recordAt(root.extents[0].offset, root.extents[0].size)
		if err != nil {
			return nil, err
		}
		sua, err := systemUse(raw)
		if err != nil {
			return nil, err
		}
		if len(sua) >= 7 && string(sua[:2]) == "SP" && sua[2] == 7 && sua[3] == 1 && sua[4] == 0xbe && sua[5] == 0xef {
			f.rockRidge = true
			f.skip = int(sua[6])
		}
	}
	return f, nil
}

type extent struct{ offset, size int64 }
type node struct {
	name     string
	mode     fs.FileMode
	size     int64
	modified time.Time
	extents  []extent
	target   string
	parent   *node
}
type filesystem struct {
	r                 SizeReader
	limit, block      int64
	joliet, rockRidge bool
	skip              int
	root              *node
}

var _ fs.ReadLinkFS = (*filesystem)(nil)

func readFull(r io.ReaderAt, b []byte, off, limit int64) error {
	if off < 0 || off > limit || int64(len(b)) > limit-off {
		return fmt.Errorf("ISO extent outside image")
	}
	n, err := r.ReadAt(b, off)
	if n == len(b) {
		return nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return err
}
func number32(b []byte) (uint32, error) {
	v := binary.LittleEndian.Uint32(b)
	if v != binary.BigEndian.Uint32(b[4:]) {
		return 0, fmt.Errorf("inconsistent both-endian ISO number")
	}
	return v, nil
}
func number16(b []byte) (uint16, error) {
	v := binary.LittleEndian.Uint16(b)
	if v != binary.BigEndian.Uint16(b[2:]) {
		return 0, fmt.Errorf("inconsistent both-endian ISO number")
	}
	return v, nil
}
func systemUse(raw []byte) ([]byte, error) {
	if len(raw) < 34 {
		return nil, fmt.Errorf("short directory record")
	}
	end := 33 + int(raw[32])
	if raw[32]%2 == 0 {
		end++
	}
	if end > len(raw) {
		return nil, fmt.Errorf("truncated file identifier")
	}
	return raw[end:], nil
}
func (f *filesystem) recordAt(off, left int64) ([]byte, error) {
	var length [1]byte
	if err := readFull(f.r, length[:], off, f.limit); err != nil {
		return nil, err
	}
	if length[0] == 0 {
		return nil, nil
	}
	if length[0] < 34 || int64(length[0]) > left {
		return nil, fmt.Errorf("invalid directory record length")
	}
	b := make([]byte, int(length[0]))
	if err := readFull(f.r, b, off, f.limit); err != nil {
		return nil, err
	}
	return b, nil
}
func (f *filesystem) parseRecord(b []byte, parent *node, extensions bool) (*node, bool, error) {
	if len(b) < 34 || int(b[0]) != len(b) {
		return nil, false, fmt.Errorf("invalid directory record")
	}
	if b[26] != 0 || b[27] != 0 {
		return nil, false, fmt.Errorf("interleaved ISO files are unsupported")
	}
	sector, err := number32(b[2:10])
	if err != nil {
		return nil, false, err
	}
	size, err := number32(b[10:18])
	if err != nil {
		return nil, false, err
	}
	offset := (int64(sector) + int64(b[1])) * f.block
	if offset > f.limit || int64(size) > f.limit-offset {
		return nil, false, fmt.Errorf("directory entry extent outside image")
	}
	namesize := int(b[32])
	if namesize == 0 || namesize > len(b)-33 {
		return nil, false, fmt.Errorf("invalid file identifier length")
	}
	id := b[33 : 33+namesize]
	n := &node{mode: 0444, size: int64(size), extents: []extent{{offset, int64(size)}}, parent: parent}
	if b[25]&2 != 0 {
		n.mode = fs.ModeDir | 0555
	}
	if id[0] == 0 && len(id) == 1 {
		n.name = "."
	} else if id[0] == 1 && len(id) == 1 {
		n.name = ".."
	} else {
		if f.joliet {
			if len(id)%2 != 0 {
				return nil, false, fmt.Errorf("odd Joliet identifier length")
			}
			units := make([]uint16, len(id)/2)
			for i := range units {
				units[i] = binary.BigEndian.Uint16(id[2*i:])
				if units[i] >= 0xd800 && units[i] <= 0xdfff {
					return nil, false, fmt.Errorf("invalid UCS-2 Joliet identifier")
				}
			}
			n.name = string(utf16.Decode(units))
		} else {
			n.name = strings.ToLower(string(id))
		}
		if semi := strings.LastIndexByte(n.name, ';'); semi >= 0 && versionSuffix(n.name[semi+1:]) {
			n.name = n.name[:semi]
		}
		if !f.joliet {
			n.name = strings.TrimSuffix(n.name, ".")
		}
	}
	if b[18] != 0 {
		n.modified = time.Date(1900+int(b[18]), time.Month(b[19]), int(b[20]), int(b[21]), int(b[22]), int(b[23]), 0, time.FixedZone("ISO", int(int8(b[24]))*15*60))
	}
	if extensions && f.rockRidge {
		sua, err := systemUse(b)
		if err != nil {
			return nil, false, err
		}
		if len(sua) > 0 {
			if f.skip > len(sua) {
				return nil, false, fmt.Errorf("invalid SUSP skip")
			}
			sua = sua[f.skip:]
			if err := f.rockRidgeEntries(n, sua); err != nil {
				return nil, false, err
			}
		}
	}
	if n.name != "." && n.name != ".." && (!fs.ValidPath(n.name) || strings.ContainsAny(n.name, "/\x00") || !utf8.ValidString(n.name)) {
		return nil, false, fmt.Errorf("unsafe ISO file name %q", n.name)
	}
	return n, b[25]&0x80 != 0, nil
}

func versionSuffix(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func (f *filesystem) children(dir *node) ([]*node, error) {
	if !dir.mode.IsDir() {
		return nil, fmt.Errorf("not a directory: %w", fs.ErrInvalid)
	}
	var children []*node
	seen := make(map[string]bool)
	var pending *node
	for _, e := range dir.extents {
		reader := bufio.NewReaderSize(io.NewSectionReader(f.r, e.offset, e.size), int(f.block))
		var padding [2048]byte
		for pos := int64(0); pos < e.size; {
			length, err := reader.ReadByte()
			if err != nil {
				return nil, err
			}
			if length == 0 {
				skip := min(f.block-(e.offset+pos)%f.block, e.size-pos)
				if _, err := io.ReadFull(reader, padding[:skip-1]); err != nil {
					return nil, err
				}
				pos += skip
				continue
			}
			if length < 34 || int64(length) > e.size-pos {
				return nil, fmt.Errorf("invalid directory record length")
			}
			raw := make([]byte, int(length))
			raw[0] = length
			if _, err := io.ReadFull(reader, raw[1:]); err != nil {
				return nil, err
			}
			pos += int64(len(raw))
			if len(raw) >= 34 && raw[32] == 1 && (raw[33] == 0 || raw[33] == 1) {
				continue
			}
			n, more, err := f.parseRecord(raw, dir, true)
			if err != nil {
				return nil, err
			}
			if n.name == "." || n.name == ".." {
				return nil, fmt.Errorf("reserved alternate ISO name")
			}
			if n.mode.IsDir() {
				for a := dir; a != nil; a = a.parent {
					if n.extents[0].offset == a.extents[0].offset {
						return nil, fmt.Errorf("cyclic ISO directory")
					}
				}
			}
			if pending != nil {
				if pending.name != n.name || n.mode.IsDir() || pending.mode != n.mode {
					return nil, fmt.Errorf("inconsistent multi-extent ISO file")
				}
				pending.extents = append(pending.extents, n.extents...)
				pending.size += n.size
				if !more {
					pending = nil
				}
				continue
			}
			if seen[n.name] {
				return nil, fmt.Errorf("duplicate ISO file name %q", n.name)
			}
			seen[n.name] = true
			children = append(children, n)
			if len(children) > 1000000 {
				return nil, fmt.Errorf("ISO directory has too many entries")
			}
			if more {
				if n.mode.IsDir() {
					return nil, fmt.Errorf("multi-extent directory is unsupported")
				}
				pending = n
			}
		}
	}
	if pending != nil {
		return nil, fmt.Errorf("unterminated multi-extent ISO file")
	}
	sort.Slice(children, func(i, j int) bool { return children[i].name < children[j].name })
	return children, nil
}

func (f *filesystem) resolve(name string, follow bool) (*node, error) {
	if !fs.ValidPath(name) {
		return nil, fs.ErrInvalid
	}
	n := f.root
	parts := strings.Split(name, "/")
	links := 0
	for len(parts) > 0 {
		part := parts[0]
		parts = parts[1:]
		if !n.mode.IsDir() {
			return nil, fs.ErrInvalid
		}
		switch part {
		case "", ".":
			continue
		case "..":
			if n == f.root {
				return nil, fs.ErrInvalid
			}
			n = n.parent
			continue
		}
		entries, err := f.children(n)
		if err != nil {
			return nil, err
		}
		at := sort.Search(len(entries), func(j int) bool { return entries[j].name >= part })
		if at == len(entries) || entries[at].name != part {
			return nil, fs.ErrNotExist
		}
		n = entries[at]
		if n.mode&fs.ModeSymlink != 0 && (follow || len(parts) > 0) {
			if n.target == "" || path.IsAbs(n.target) {
				return nil, fs.ErrInvalid
			}
			links++
			if links > 40 {
				return nil, fmt.Errorf("too many ISO symbolic links: %w", fs.ErrInvalid)
			}
			parts = append(strings.Split(n.target, "/"), parts...)
			n = n.parent
		}
	}
	return n, nil
}
func pathError(op, name string, err error) error { return &fs.PathError{Op: op, Path: name, Err: err} }
func (f *filesystem) Open(name string) (fs.File, error) {
	n, err := f.resolve(name, true)
	if err != nil {
		return nil, pathError("open", name, err)
	}
	return &file{fs: f, node: n}, nil
}
func (f *filesystem) Stat(name string) (fs.FileInfo, error) {
	n, err := f.resolve(name, true)
	if err != nil {
		return nil, pathError("stat", name, err)
	}
	return info{n}, nil
}
func (f *filesystem) Lstat(name string) (fs.FileInfo, error) {
	n, err := f.resolve(name, false)
	if err != nil {
		return nil, pathError("lstat", name, err)
	}
	return info{n}, nil
}
func (f *filesystem) ReadLink(name string) (string, error) {
	n, err := f.resolve(name, false)
	if err == nil && n.mode&fs.ModeSymlink == 0 {
		err = fs.ErrInvalid
	}
	if err != nil {
		return "", pathError("readlink", name, err)
	}
	return n.target, nil
}
func (f *filesystem) ReadDir(name string) ([]fs.DirEntry, error) {
	n, err := f.resolve(name, true)
	if err != nil {
		return nil, pathError("readdir", name, err)
	}
	children, err := f.children(n)
	if err != nil {
		return nil, pathError("readdir", name, err)
	}
	entries := make([]fs.DirEntry, len(children))
	for i, n := range children {
		entries[i] = info{n}
	}
	return entries, nil
}

type info struct{ *node }

func (n info) Name() string               { return n.name }
func (n info) Size() int64                { return n.size }
func (n info) Mode() fs.FileMode          { return n.mode }
func (n info) ModTime() time.Time         { return n.modified }
func (n info) IsDir() bool                { return n.mode.IsDir() }
func (n info) Sys() any                   { return nil }
func (n info) Type() fs.FileMode          { return n.mode.Type() }
func (n info) Info() (fs.FileInfo, error) { return n, nil }

type file struct {
	fs        *filesystem
	node      *node
	offset    int64
	closed    bool
	entries   []fs.DirEntry
	dirOffset int
}

func (f *file) Close() error { f.closed = true; return nil }
func (f *file) Stat() (fs.FileInfo, error) {
	if f.closed {
		return nil, fs.ErrClosed
	}
	return info{f.node}, nil
}
func (f *file) Read(b []byte) (int, error) {
	n, err := f.ReadAt(b, f.offset)
	f.offset += int64(n)
	return n, err
}
func (f *file) ReadAt(b []byte, off int64) (int, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}
	if f.node.mode.IsDir() {
		return 0, fs.ErrInvalid
	}
	if off < 0 {
		return 0, fs.ErrInvalid
	}
	if len(b) == 0 {
		return 0, nil
	}
	if off >= f.node.size {
		return 0, io.EOF
	}
	total := 0
	for _, e := range f.node.extents {
		if off >= e.size {
			off -= e.size
			continue
		}
		count := min(int64(len(b)-total), e.size-off)
		n, err := f.fs.r.ReadAt(b[total:total+int(count)], e.offset+off)
		total += n
		if n != int(count) {
			if err == nil {
				err = io.ErrUnexpectedEOF
			}
			return total, err
		}
		if total == len(b) {
			return total, nil
		}
		off = 0
	}
	return total, io.EOF
}
func (f *file) Seek(off int64, whence int) (int64, error) {
	if f.closed {
		return 0, fs.ErrClosed
	}
	base := int64(0)
	switch whence {
	case io.SeekStart:
	case io.SeekCurrent:
		base = f.offset
	case io.SeekEnd:
		base = f.node.size
	default:
		return 0, fs.ErrInvalid
	}
	if off < -base || off > int64(^uint64(0)>>1)-base {
		return 0, fs.ErrInvalid
	}
	f.offset = base + off
	return f.offset, nil
}
func (f *file) ReadDir(count int) ([]fs.DirEntry, error) {
	if f.closed {
		return nil, fs.ErrClosed
	}
	if !f.node.mode.IsDir() {
		return nil, fs.ErrInvalid
	}
	if f.entries == nil {
		nodes, err := f.fs.children(f.node)
		if err != nil {
			return nil, err
		}
		f.entries = make([]fs.DirEntry, len(nodes))
		for i, n := range nodes {
			f.entries[i] = info{n}
		}
	}
	if f.dirOffset >= len(f.entries) {
		if count > 0 {
			return nil, io.EOF
		}
		return []fs.DirEntry{}, nil
	}
	end := len(f.entries)
	if count > 0 && count < end-f.dirOffset {
		end = f.dirOffset + count
	}
	result := f.entries[f.dirOffset:end]
	f.dirOffset = end
	return result, nil
}
