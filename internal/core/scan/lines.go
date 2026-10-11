package scan

import (
	"strings"
)

// Line is a line of scanned segments. End is "\n", "\r\n", or empty for
// an unterminated final line. Raw segments followed by End preserve the input.
// CRLF still follows Scan's rule: the CR is trailing whitespace, not a new line.
type Line struct {
	Segments []Segment
	End      string
}

// Lines exposes the line boundaries needed by rendering without rescanning
// placeholders. Text segments spanning several lines are split; placeholder
// metadata and rune-based positions are preserved. A terminal newline does not
// create an extra empty line. Returned slices do not alias Result.Segments.
func (r Result) Lines() []Line {
	var out []Line
	var line Line
	for _, seg := range r.Segments {
		if seg.Kind != KindText {
			line.Segments = append(line.Segments, seg)
			continue
		}
		for seg.Raw != "" {
			before, after, newline := strings.Cut(seg.Raw, "\n")
			if before != "" {
				piece := seg
				piece.Raw = before
				line.Segments = append(line.Segments, piece)
			}
			if !newline {
				break
			}
			line.End = "\n"
			if n := len(line.Segments); n > 0 {
				last := &line.Segments[n-1]
				if last.Kind == KindText && strings.HasSuffix(last.Raw, "\r") {
					last.Raw = strings.TrimSuffix(last.Raw, "\r")
					line.End = "\r\n"
					if last.Raw == "" {
						line.Segments = line.Segments[:n-1]
					}
				}
			}
			out = append(out, line)
			line = Line{}
			seg.Raw, seg.Line, seg.Col = after, seg.Line+1, 1
		}
	}
	if len(line.Segments) > 0 {
		out = append(out, line)
	}
	return out
}

// Raw returns the line's bytes without End, including unrecognized placeholders.
func (l Line) Raw() string {
	var b strings.Builder
	for _, seg := range l.Segments {
		b.WriteString(seg.Raw)
	}
	return b.String()
}
