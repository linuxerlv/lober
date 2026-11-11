// Package isotest contains shared ISO fixtures and the opt-in real-media switch.
// Production code must not import this test utility package.
package isotest

import (
	"encoding/binary"
	"io/fs"
	"os"
	"sort"
	"strings"
	"testing"
	"unicode/utf16"
)

// Path returns the opt-in real media path or skips when the switch is unset.
func Path(t testing.TB) string {
	t.Helper()
	name := os.Getenv("LOBER_TEST_REAL_ISO")
	if name == "" {
		t.Skip("LOBER_TEST_REAL_ISO is unset; real ISO coverage is opt-in")
	}
	info, err := os.Stat(name)
	if err != nil {
		t.Fatalf("LOBER_TEST_REAL_ISO %q: %v", name, err)
	}
	if !info.Mode().IsRegular() {
		t.Fatalf("LOBER_TEST_REAL_ISO %q is not a regular file", name)
	}
	return name
}

// Image builds a deterministic ISO image containing the supplied files.
// It uses Joliet to preserve case and dotfiles. Invalid fixture paths panic.
func Image(files map[string][]byte) []byte {
	root := &imageNode{name: ".", children: map[string]*imageNode{}}
	keys := make([]string, 0, len(files))
	for key := range files {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if !fs.ValidPath(key) || key == "." || strings.ContainsRune(key, 0) {
			panic("invalid ISO fixture path")
		}
		parts := strings.Split(key, "/")
		dir := root
		for i, name := range parts {
			n := dir.children[name]
			if n == nil {
				n = &imageNode{name: name, parent: dir}
				dir.children[name] = n
			}
			if i == len(parts)-1 {
				if n.children != nil {
					panic("ISO fixture file/directory conflict")
				}
				n.data = files[key]
			} else {
				if n.data != nil {
					panic("ISO fixture file/directory conflict")
				}
				if n.children == nil {
					n.children = map[string]*imageNode{}
				}
				dir = n
			}
		}
	}
	var nodes []*imageNode
	var visit func(*imageNode)
	visit = func(n *imageNode) {
		nodes = append(nodes, n)
		names := make([]string, 0, len(n.children))
		for name := range n.children {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			visit(n.children[name])
		}
	}
	visit(root)
	next := uint32(20)
	for _, n := range nodes {
		n.sector = next
		if n.children != nil {
			n.size = uint32(len(directoryBytes(n)))
		} else {
			n.size = uint32(len(n.data))
		}
		next += (n.size + 2047) / 2048
	}
	if next < 21 {
		next = 21
	}
	b := make([]byte, int(next)*2048)
	for _, n := range nodes {
		if n.children != nil {
			copy(b[int(n.sector)*2048:], directoryBytes(n))
		} else {
			copy(b[int(n.sector)*2048:], n.data)
		}
	}
	primary := &imageNode{name: ".", sector: 19, size: 2048, children: map[string]*imageNode{}}
	copy(b[19*2048:], directoryBytes(primary))
	for _, item := range []struct {
		sector int
		typ    byte
		n      *imageNode
	}{{16, 1, primary}, {17, 2, root}} {
		d := b[item.sector*2048 : (item.sector+1)*2048]
		d[0] = item.typ
		copy(d[1:6], "CD001")
		d[6] = 1
		num32(d[80:88], next)
		num16(d[120:124], 1)
		num16(d[124:128], 1)
		num16(d[128:132], 2048)
		copy(d[156:], imageRecord([]byte{0}, item.n))
		if item.typ == 2 {
			copy(d[88:91], "%/E")
		}
	}
	b[18*2048] = 255
	copy(b[18*2048+1:], "CD001")
	b[18*2048+6] = 1
	return b
}

type imageNode struct {
	name         string
	data         []byte
	children     map[string]*imageNode
	parent       *imageNode
	sector, size uint32
}

func num32(b []byte, n uint32) {
	binary.LittleEndian.PutUint32(b, n)
	binary.BigEndian.PutUint32(b[4:], n)
}
func num16(b []byte, n uint16) {
	binary.LittleEndian.PutUint16(b, n)
	binary.BigEndian.PutUint16(b[2:], n)
}
func imageRecord(name []byte, n *imageNode) []byte {
	length := 33 + len(name)
	if len(name)%2 == 0 {
		length++
	}
	if length > 255 {
		panic("ISO fixture identifier too long")
	}
	b := make([]byte, length)
	b[0] = byte(length)
	num32(b[2:10], n.sector)
	num32(b[10:18], n.size)
	b[18] = 126
	b[19] = 10
	b[20] = 13
	if n.children != nil {
		b[25] = 2
	}
	num16(b[28:32], 1)
	b[32] = byte(len(name))
	copy(b[33:], name)
	return b
}
func directoryBytes(n *imageNode) []byte {
	parent := n.parent
	if parent == nil {
		parent = n
	}
	records := [][]byte{imageRecord([]byte{0}, n), imageRecord([]byte{1}, parent)}
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		units := utf16.Encode([]rune(name))
		encoded := make([]byte, len(units)*2)
		for i, u := range units {
			binary.BigEndian.PutUint16(encoded[i*2:], u)
		}
		records = append(records, imageRecord(encoded, n.children[name]))
	}
	var b []byte
	for _, r := range records {
		if len(b)%2048+len(r) > 2048 {
			b = append(b, make([]byte, 2048-len(b)%2048)...)
		}
		b = append(b, r...)
	}
	if len(b)%2048 != 0 {
		b = append(b, make([]byte, 2048-len(b)%2048)...)
	}
	return b
}
