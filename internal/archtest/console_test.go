package archtest

import (
	"testing"
	"testing/fstest"
)

func Test控制台与装配点的依赖规则(t *testing.T) {
	for _, tt := range []struct {
		pkg, imp string
		want     rule
	}{
		{pkgConsole, "net/http", ""}, {pkgConsole, gin, ""}, {pkgConsole, pkgModel, ""},
		{pkgConsole, pkgCatalog, ""}, {pkgConsole, pkgStore, ""}, {pkgConsole, pkgRender, ""}, {pkgConsole, pkgBuiltin, ""},
		{pkgConsole, pkgAssembly, ruleConsole}, {pkgConsole, pkgISO, ruleConsole}, {pkgConsole, pkgPgstore, ruleConsole},
		{pkgConsole, "github.com/lib/pq", ruleConsole}, {pkgConsole, "C", ruleConsole},
		{pkgConsole + "/seed", pkgPgstore, ruleConsole},
		{pkgAssembly, pkgISO, ""}, {pkgAssembly, pkgPgstore, ""}, {pkgAssembly, pkgConsole, ""},
		{pkgAssembly, gin, ""}, {pkgAssembly, yamlV3, ""}, {pkgAssembly, "gorm.io/gorm", ruleAssembly},
	} {
		fset, f := parseSource(t, importing(tt.imp))
		var got []violation
		if under(tt.pkg, pkgConsole) {
			got = checkConsoleFile(fset, tt.pkg, f)
		} else {
			got = checkAssemblyFile(fset, tt.pkg, f)
		}
		if tt.want == "" {
			assertFindings(t, got, tt.pkg, nil)
		} else {
			assertFindings(t, got, tt.pkg, []finding{{tt.want, tt.imp}})
		}
	}
}

func Test总检查接上了控制台与装配点(t *testing.T) {
	fsys := fstest.MapFS{
		"internal/console/x.go": {Data: []byte(importing(pkgAssembly))},
		"cmd/lober/y.go":        {Data: []byte(importing("github.com/lib/pq"))},
	}
	got, counts := checkModule(fsys, []string{"internal/console", "cmd/lober"}, []goPackage{
		{ImportPath: pkgConsole, GoFiles: []string{"x.go"}}, {ImportPath: pkgAssembly, GoFiles: []string{"y.go"}},
	})
	assertFindings(t, got, "", []finding{{ruleConsole, pkgAssembly}, {ruleAssembly, "github.com/lib/pq"}})
	if counts[layerConsole] != 1 || counts[layerAssembly] != 1 {
		t.Fatal(counts)
	}
}
