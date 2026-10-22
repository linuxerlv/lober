package iso

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCopyImportTreeConfinesSymlinkChains(t *testing.T) {
	for _, escape := range []bool{false, true} {
		t.Run(map[bool]string{false: "safe_parent", true: "chain_escape"}[escape], func(t *testing.T) {
			src, dest := t.TempDir(), filepath.Join(t.TempDir(), "install")
			if err := os.Mkdir(filepath.Join(src, "dir"), 0755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(src, "secret"), []byte("inside"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(".", filepath.Join(src, "a")); err != nil {
				t.Fatal(err)
			}
			link, target := "dir/safe", "../secret"
			if escape {
				link, target = "escape", "a/../secret"
			}
			if err := os.Symlink(target, filepath.Join(src, filepath.FromSlash(link))); err != nil {
				t.Fatal(err)
			}
			err := copyImportTree(t.Context(), os.DirFS(src), dest)
			if escape {
				if err == nil {
					t.Fatal("accepted an OS symlink chain escaping extraction root")
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				got, err := os.ReadFile(filepath.Join(dest, filepath.FromSlash(link)))
				if err != nil || string(got) != "inside" {
					t.Fatalf("safe link: %q %v", got, err)
				}
			}
		})
	}
}
