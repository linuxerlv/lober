package archtest

import (
	"testing"
	"testing/fstest"
)

func Test装配只豁免规范Wire注入文件且仍检查其导入(t *testing.T) {
	good := "//go:build wireinject\n\npackage main\nimport \"github.com/goforj/wire\"\nfunc newApp(){wire.Build()}\n"
	for _, test := range []struct {
		name, pkg, source string
		ignored           bool
	}{
		{"wire.go", pkgAssembly, good, false},
		{"hidden.go", pkgAssembly, good, true},
		{"wire.go", pkgConsole, good, true},
		{"wire.go", pkgAssembly, "//go:build linux\npackage main\nfunc newApp(){}", true},
		{"wire.go", pkgAssembly, good + "func hidden(){}\n", true},
	} {
		dir := "cmd/lober"
		if test.pkg == pkgConsole {
			dir = "internal/console"
		}
		fsys := fstest.MapFS{dir + "/" + test.name: {Data: []byte(test.source)}}
		p := goPackage{ImportPath: test.pkg, IgnoredGoFiles: []string{test.name}}
		findings := assemblyIgnoredFiles(fsys, p)
		if (len(findings) > 0) != test.ignored {
			t.Fatal(test.name, test.pkg, findings)
		}
	}
	malicious := "//go:build wireinject\npackage main\nimport (\"github.com/goforj/wire\";\"example.test/forbidden\")\nfunc newApp(){wire.Build()}\n"
	fsys := fstest.MapFS{"cmd/lober/wire.go": {Data: []byte(malicious)}}
	p := goPackage{ImportPath: pkgAssembly, IgnoredGoFiles: []string{"wire.go"}}
	if len(checkSources(fsys, p, checkAssemblyFile)) == 0 {
		t.Fatal("injector import rules bypassed")
	}
}
