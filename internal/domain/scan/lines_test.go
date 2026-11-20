package scan

import (
	"strings"
	"testing"
)

func TestLinesPreserveBytesAndSegmentLocations(t *testing.T) {
	for _, src := range []string{"", "plain", "one\n\nthree\n", "中文 {{param:a}}\r\n\t{{sys:task}}\n", "{{bad:x}}\n"} {
		r, _ := Scan(src)
		var b strings.Builder
		for i, line := range r.Lines() {
			for _, seg := range line.Segments {
				if seg.Line != i+1 {
					t.Fatalf("%q: segment on line %d, want %d", src, seg.Line, i+1)
				}
				b.WriteString(seg.Raw)
			}
			b.WriteString(line.End)
		}
		if b.String() != src {
			t.Fatalf("got %q, want %q", b.String(), src)
		}
	}
	r, _ := Scan("中文 {{param:a}}\r\n\t{{sys:task}}\n")
	lines := r.Lines()
	if len(lines) != 2 || lines[0].End != "\r\n" || lines[1].End != "\n" || lines[0].Segments[1].Col != 4 || lines[1].Segments[1].Col != 2 {
		t.Fatalf("unexpected lines: %+v", lines)
	}
}
