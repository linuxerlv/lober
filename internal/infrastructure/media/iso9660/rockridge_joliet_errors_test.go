package iso9660

import (
	"bytes"
	"io/fs"
	"testing"
)

// 双目录同时存在时，主目录中的损坏不能被 Joliet 隐藏。
func TestDualVolumeRejectsInvalidRockRidge(t *testing.T) {
	joliet := entries(record([]byte{0}, 400, testSector, 2), record(ucs("safe.txt;1"), 600, 1, 0))
	for _, tc := range []struct {
		name  string
		extra []byte
	}{
		{"short SP", []byte{'S', 'P'}},
		{"bad SP length", []byte{'S', 'P', 6, 1, 0xbe, 0xef, 0}},
		{"bad SP version", []byte{'S', 'P', 7, 2, 0xbe, 0xef, 0}},
		{"bad SP magic", []byte{'S', 'P', 7, 1, 0, 0xef, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := record([]byte{0}, 20, testSector, 2, tc.extra...)
			if _, err := Open(bytes.NewReader(image(entries(root), joliet))); err == nil {
				t.Fatal("accepted invalid SP with Joliet present")
			}
		})
	}
	for _, tc := range []struct {
		name    string
		records [][]byte
	}{
		{"bad NM", [][]byte{record([]byte("BAD;1"), 600, 1, 0, susp("NM", 2, 'x')...)}},
		{"unsafe NM", [][]byte{record([]byte("BAD;1"), 600, 1, 0, nm("../escape")...)}},
		{"outside CE", [][]byte{record([]byte("BAD;1"), 600, 1, 0, ce(0xfffffff0, 0, 28)...)}},
		{"outside extent", [][]byte{record([]byte("BAD;1"), 0xfffffff0, 1, 0, nm("bad")...)}},
		{"duplicate names", [][]byte{record([]byte("ONE;1"), 600, 1, 0, nm("same")...), record([]byte("TWO;1"), 601, 1, 0, nm("same")...)}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := entries(append([][]byte{rrRoot()}, tc.records...)...)
			f, err := Open(bytes.NewReader(image(root, joliet)))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := fs.ReadDir(f, "."); err == nil {
				t.Fatal("accepted invalid Rock Ridge directory with Joliet present")
			}
		})
	}
}
