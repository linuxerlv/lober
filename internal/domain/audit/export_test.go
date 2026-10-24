package audit

import (
	"bytes"
	"encoding/csv"
	"strings"
	"testing"
)

func Test审计CSV文本转义(t *testing.T) {
	for _, raw := range []string{"=1+2", "+SUM(1,2)", "-1+2", "@SUM(1,2)", "\t=1+2", " \r\n=1+2", "＝1+2", "＋SUM(1,2)", "－1+2", "＠SUM(1,2)", "\u3000=1+2", "\ufeff=1+2", "\u200b＠sum", "\u2060＋sum", "\tplain", "\rplain", "\nplain", `=1+2";=1+2`, `=1+2'";,=1+2`} {
		if got := SpreadsheetText(raw); got != "\t"+raw {
			t.Fatal(raw, got)
		}
		var data bytes.Buffer
		w := csv.NewWriter(&data)
		if err := w.Write([]string{SpreadsheetText(raw)}); err != nil {
			t.Fatal(err)
		}
		w.Flush()
		if !strings.HasPrefix(data.String(), "\"\t") {
			t.Fatal("text prefix is outside quoted field", data.String())
		}
		rows, err := csv.NewReader(&data).ReadAll()
		// encoding/csv.Reader normalizes embedded CRLF to LF.
		if err != nil || len(rows) != 1 || len(rows[0]) != 1 || rows[0][0] != "\t"+strings.ReplaceAll(raw, "\r\n", "\n") {
			t.Fatal("CSV field split or altered", rows, err)
		}
	}
	for _, raw := range []string{"", "普通文本", "123", "'already text", "a=b"} {
		if got := SpreadsheetText(raw); got != raw {
			t.Fatal(raw, got)
		}
	}
}

func Test审计时间范围(t *testing.T) {
	for _, tc := range []struct {
		from, to int64
		want     bool
	}{{0, 0, true}, {3, 0, true}, {3, 3, true}, {3, 4, true}, {3, 2, false}, {-1, 0, false}, {0, -1, false}} {
		if ValidRange(tc.from, tc.to) != tc.want {
			t.Fatal(tc)
		}
	}
}
