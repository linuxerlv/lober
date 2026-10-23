package archtest

import (
	"go/ast"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const (
	pkgISO           = modulePath + "/internal/infrastructure/media"
	pkgISO9660       = pkgISO + "/iso9660"
	ruleISO     rule = "ISO 层只依赖标准库、core 与存储契约"
	ruleISO9660 rule = "ISO9660 只依赖标准库"
)

// ISO 层使用全模块目录发现与缓存跟踪，再检查本层的依赖规则。
func TestISO依赖守卫(t *testing.T) {
	root := filepath.Dir(goModPath(t))
	pkgs := packagesUnder(t, root, pkgISO)
	if len(pkgs) == 0 {
		t.Fatal("ISO layer is missing from go list")
	}
	var dirs []string
	for _, d := range moduleSourceDirs(t, filepath.Join(root, "go.mod"), root) {
		if under(d, "internal/infrastructure/media") {
			dirs = append(dirs, d)
		}
	}
	for _, v := range unlistedDirs(dirs, pkgs) {
		t.Error(v)
	}
	for _, p := range pkgs {
		for _, v := range append(ignoredFiles(p), checkSources(os.DirFS(root), p, checkISOFile)...) {
			t.Error(v)
		}
	}
}

func checkISOFile(fset *token.FileSet, pkg string, f *ast.File) []violation {
	var found []violation
	for _, imp := range f.Imports {
		name, _ := strconv.Unquote(imp.Path.Value)
		allowed := isStd(name) && name != "C" && name != "unsafe"
		r := ruleISO
		if under(pkg, pkgISO9660) {
			r = ruleISO9660
		} else {
			allowed = allowed || inLayer1(name) || name == pkgStore || under(name, pkgISO) || pkg == pkgISO && name == modulePath+"/internal/application/media/port"
		}
		if !allowed {
			found = append(found, violation{rule: r, pkg: pkg, subject: name, pos: position(fset, imp.Pos())})
		}
	}
	return found
}

func Test新模块不能漏出守卫(t *testing.T) {
	root := filepath.Dir(goModPath(t))
	dirs, err := sourceDirs(os.DirFS(filepath.Join(root, "internal")))
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range dirs {
		first := strings.Split(d, "/")[0]
		switch first {
		case "domain", "infrastructure", "application", "interfaces", "archtest":
		default:
			t.Errorf("未登记层: internal/%s", d)
		}
	}
}

func TestISORules(t *testing.T) {
	for _, c := range []struct {
		pkg, imp string
		reject   bool
	}{
		{pkgISO, "os", false}, {pkgISO, "net/http", false}, {pkgISO, "path/filepath", false},
		{pkgISO, pkgModel, false}, {pkgISO, pkgStore, false}, {pkgISO, modulePath + "/internal/domain/render", true},
		{pkgISO, "example.com/parser", true}, {pkgISO, "C", true},
		{pkgISO9660, "io/fs", false}, {pkgISO9660, pkgModel, true}, {pkgISO9660, pkgStore, true},
	} {
		f := &ast.File{Imports: []*ast.ImportSpec{{Path: &ast.BasicLit{Kind: token.STRING, Value: strconv.Quote(c.imp)}}}}
		if got := len(checkISOFile(token.NewFileSet(), c.pkg, f)) > 0; got != c.reject {
			t.Errorf("%s importing %s: rejected=%v", c.pkg, c.imp, got)
		}
	}
}
