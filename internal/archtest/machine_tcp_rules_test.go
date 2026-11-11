package archtest

import (
	"go/parser"
	"go/token"
	"testing"
)

func Test机器日志TCP仅增加精确传输依赖(t *testing.T) {
	for _, tc := range []struct {
		imp     string
		allowed bool
	}{
		{"context", true},
		{"net", true},
		{"net/netip", true},
		{"sync", true},
		{"bufio", true},
		{"time", true},
		{"errors", true},
		{pkgExecutionPort, true},
		{pkgModel, true},
		{"database/sql", false},
		{"os", false},
		{"os/exec", false},
		{"net/http", false},
		{"path/filepath", false},
		{"io", false},
		{"sync/atomic", false},
		{pkgStore, false},
		{pkgExecution, false},
	} {
		t.Run(tc.imp, func(t *testing.T) {
			fset := token.NewFileSet()
			file, err := parser.ParseFile(fset, "tcp_transport.go", importing(tc.imp), 0)
			if err != nil {
				t.Fatal(err)
			}
			got := checkMachineFile(fset, pkgEvents, file)
			if tc.allowed {
				if len(got) != 0 {
					t.Fatal("TCP 必需的精确依赖被拒绝", tc.imp, got)
				}
			} else if len(got) != 1 || got[0].rule != ruleMachine {
				t.Fatal("日志传输获得了数据库、文件、进程或其他越界能力", tc.imp, got)
			}
		})
	}
}
