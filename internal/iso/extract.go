package iso

import (
	"context"
	"errors"
	"fmt"
	"github.com/lober/lober/internal/core/catalog"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

func safeImportRel(rel string) bool {
	return fs.ValidPath(rel) && rel != "." && !strings.ContainsAny(rel, "\\:")
}

func (s *Sources) extract(ctx context.Context, id string, found Identified, main, extra fs.FS, raw *os.File, tmp string) (map[string]string, error) {
	roles := map[string]string{}
	file := func(role, name, out string, tree fs.FS) error {
		if err := copyImportFile(ctx, tree, name, filepath.Join(tmp, out)); err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return importProblem(id, "iso_artifact_missing", "artifacts."+role, role)
			}
			return err
		}
		roles[role] = out
		return nil
	}
	switch found.Format {
	case "kickstart":
		if err := copyImportTree(ctx, main, filepath.Join(tmp, "install")); err != nil {
			return nil, err
		}
		for role, name := range map[string]string{"kernel": "images/pxeboot/vmlinuz", "initrd": "images/pxeboot/initrd.img"} {
			if err := file(role, name, path.Base(name), main); err != nil {
				return nil, err
			}
		}
		st, e := fs.Stat(main, "images/install.img")
		if e != nil || st.Size() == 0 {
			return nil, importProblem(id, "iso_artifact_missing", "artifacts.stage2", "stage2")
		}
		roles["stage2"] = "install/"
		repo, err := importRepoRoot(ctx, main)
		if err != nil {
			return nil, err
		}
		for _, required := range catalog.RepoPaths(found.Series) {
			name := path.Join(repo, required)
			st, e := fs.Stat(main, name)
			if e != nil || st.IsDir() || st.Size() == 0 {
				return nil, importProblem(id, "iso_repo_incomplete", "artifacts.repo", name)
			}
		}
		roles["repo"] = "install/"
		if repo != "." {
			roles["repo"] += "" + repo + "/"
		}
	case "preseed":
		if extra == nil {
			return nil, importProblem(id, "iso_extra_source_invalid", "source.netboot", "netboot")
		}
		if err := validateDebianRepository(ctx, id, found.Arch, main); err != nil {
			return nil, err
		}
		// The netboot medium supplies the installer, while the main DVD supplies
		// the HTTP package repository referenced by the shipped preseed answer.
		if err := copyImportTree(ctx, main, filepath.Join(tmp, "install")); err != nil {
			return nil, err
		}
		roles["repo"] = "install/"
		// The legacy optical path is accepted only on media explicitly marked NETBOOT.
		arch := "amd64"
		if found.Arch == "aarch64" {
			arch = "arm64"
		}
		for role, names := range map[string][]string{"kernel": {"debian-installer/" + arch + "/linux", "install.amd/vmlinuz", "linux"}, "initrd": {"debian-installer/" + arch + "/initrd.gz", "install.amd/initrd.gz", "initrd.gz"}} {
			selected := ""
			for _, name := range names {
				if st, e := fs.Stat(extra, name); e == nil && st.Mode().IsRegular() && st.Size() > 0 {
					selected = name
					break
				}
			}
			if selected == "" {
				return nil, importProblem(id, "iso_artifact_missing", "artifacts."+role, role)
			}
			out := "linux"
			if role == "initrd" {
				out = "initrd.gz"
			}
			if err := file(role, selected, out, extra); err != nil {
				return nil, err
			}
		}
	case "autoinstall":
		if err := file("kernel", "casper/vmlinuz", "vmlinuz", main); err != nil {
			return nil, err
		}
		if err := file("initrd", "casper/initrd", "initrd", main); err != nil {
			return nil, err
		}
		if _, err := raw.Seek(0, io.SeekStart); err != nil {
			return nil, err
		}
		if err := writeImportFile(ctx, raw, filepath.Join(tmp, "install.iso"), 0644); err != nil {
			return nil, err
		}
		roles["iso"] = "install.iso"
	case "autoyast", "agama":
		if err := copyImportTree(ctx, main, filepath.Join(tmp, "install")); err != nil {
			return nil, err
		}
		boot := "boot/" + string(found.Arch) + "/loader/"
		if err := file("kernel", boot+"linux", "linux", main); err != nil {
			return nil, err
		}
		if err := file("initrd", boot+"initrd", "initrd", main); err != nil {
			return nil, err
		}
		repo := found.repoRoot
		if repo == "" {
			repo = "."
		}
		for _, required := range catalog.RepoPaths(found.Series) {
			st, e := fs.Stat(main, path.Join(repo, required))
			if e != nil || st.IsDir() || st.Size() == 0 {
				return nil, importProblem(id, "iso_repo_incomplete", "artifacts.repo", required)
			}
		}
		roles["repo"] = "install/"
		if repo != "." {
			roles["repo"] += repo + "/"
		}
		if found.Format == "agama" {
			if err := file("live-root", "LiveOS/squashfs.img", "squashfs.img", main); err != nil {
				return nil, err
			}
		}
	case "unattend":
		return s.extractWindows(ctx, id, main, tmp)
	default:
		return nil, importProblem(id, "iso_version_missing", "version", string(found.Format))
	}
	return roles, nil
}

// Discover the suite from the medium, rather than assuming a Debian codename.
// This checks minimum mirror structure; package availability still needs a real
// installation, and the publisher digest is verified separately by Import.
func validateDebianRepository(ctx context.Context, id string, arch catalog.Arch, tree fs.FS) error {
	debArch := "amd64"
	if arch == "aarch64" {
		debArch = "arm64"
	}
	nonempty := func(name string) (bool, error) {
		if err := ctx.Err(); err != nil {
			return false, err
		}
		st, err := fs.Stat(tree, name)
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		if err != nil {
			return false, err
		}
		return st.Mode().IsRegular() && st.Size() > 0, nil
	}
	suites, err := fs.ReadDir(tree, "dists")
	if err != nil {
		return err
	}
	validIndex := false
	for _, suite := range suites {
		if !suite.IsDir() {
			continue
		}
		root := path.Join("dists", suite.Name())
		ok, err := nonempty(path.Join(root, "Release"))
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		for _, suffix := range []string{"", ".gz", ".xz", ".bz2"} {
			ok, err = nonempty(path.Join(root, "main", "binary-"+debArch, "Packages"+suffix))
			if err != nil {
				return err
			}
			if ok {
				validIndex = true
				break
			}
		}
		if validIndex {
			break
		}
	}
	hasPackage := false
	err = fs.WalkDir(tree, "pool", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !entry.IsDir() && strings.HasSuffix(name, ".deb") {
			ok, err := nonempty(name)
			if err != nil {
				return err
			}
			if ok {
				hasPackage = true
				return fs.SkipAll
			}
		}
		return nil
	})
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if !validIndex || !hasPackage {
		return importProblem(id, "iso_repo_incomplete", "artifacts.repo", "dists/*/Release, main/binary-"+debArch+"/Packages, pool/*.deb")
	}
	return nil
}

func importRepoRoot(ctx context.Context, tree fs.FS) (string, error) {
	text, err := identifyText(ctx, tree, ".treeinfo")
	if err != nil {
		return "", err
	}
	section := ""
	packagedir := ""
	var repos []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key, value = strings.TrimSpace(key), strings.TrimSpace(value)
		if section == "general" && key == "packagedir" {
			packagedir = value
		}
		if strings.HasPrefix(section, "variant-") && key == "repository" {
			if value != "." && !safeImportRel(value) {
				return "", fmt.Errorf("invalid .treeinfo repository %q", value)
			}
			repos = append(repos, value)
		}
	}
	if packagedir != "" && !safeImportRel(packagedir) {
		return "", fmt.Errorf("invalid .treeinfo packagedir %q", packagedir)
	}
	root := "."
	if packagedir != "" {
		root = path.Dir(packagedir)
	}
	for _, repo := range repos {
		for root != "." && repo != root && !strings.HasPrefix(repo, root+"/") {
			root = path.Dir(root)
		}
	}
	return root, nil
}

func copyImportFile(ctx context.Context, tree fs.FS, name, dest string) error {
	in, err := tree.Open(name)
	if err != nil {
		return err
	}
	defer in.Close()
	st, err := in.Stat()
	if err != nil {
		return err
	}
	if !st.Mode().IsRegular() || st.Size() == 0 {
		return fmt.Errorf("empty or nonregular artifact %s: %w", name, fs.ErrNotExist)
	}
	return writeImportFile(ctx, in, dest, st.Mode().Perm())
}
func writeImportFile(ctx context.Context, in io.Reader, dest string, mode fs.FileMode) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
		return err
	}
	out, err := os.OpenFile(dest, os.O_CREATE|os.O_EXCL|os.O_WRONLY, mode|0400)
	if err != nil {
		return err
	}
	_, err = io.Copy(out, contextReader{ctx: ctx, r: in})
	if err == nil {
		err = out.Sync()
	}
	closeErr := out.Close()
	if err == nil {
		err = closeErr
	}
	return err
}
func copyImportTree(ctx context.Context, tree fs.FS, dest string) error {
	if err := os.MkdirAll(dest, 0755); err != nil {
		return err
	}
	var links []string
	err := fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if err = ctx.Err(); err != nil {
			return err
		}
		if p == "." {
			return nil
		}
		if !safeImportRel(p) {
			return fmt.Errorf("unsafe media path %q", p)
		}
		out := filepath.Join(dest, filepath.FromSlash(p))
		if d.IsDir() {
			return os.MkdirAll(out, 0755)
		}
		if d.Type()&fs.ModeSymlink != 0 {
			target, err := fs.ReadLink(tree, p)
			if err != nil {
				return err
			}
			if target == "" || strings.ContainsAny(target, "\\:") || path.IsAbs(target) {
				return fmt.Errorf("unsafe symlink %q", p)
			}
			resolved := path.Clean(path.Join(path.Dir(p), target))
			if resolved != "." && !safeImportRel(resolved) {
				return fmt.Errorf("escaping symlink %q", p)
			}
			if err = os.MkdirAll(filepath.Dir(out), 0755); err != nil {
				return err
			}
			if err := os.Symlink(filepath.FromSlash(target), out); err != nil {
				return err
			}
			links = append(links, p)
			return nil
		}
		// Empty ordinary files are valid in an installation tree (only artifacts require nonempty).
		in, err := tree.Open(p)
		if err != nil {
			return err
		}
		defer in.Close()
		st, err := in.Stat()
		if err != nil {
			return err
		}
		if !st.Mode().IsRegular() {
			return fmt.Errorf("unsupported media node %q", p)
		}
		return writeImportFile(ctx, in, out, st.Mode().Perm())
	})
	if err != nil {
		return err
	}
	// Validate after every node exists: lexical cleaning cannot model a link
	// followed by "..". Root.Stat uses confined OS resolution for the full chain.
	root, err := os.OpenRoot(dest)
	if err != nil {
		return err
	}
	defer root.Close()
	for _, link := range links {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := root.Stat(filepath.FromSlash(link)); err != nil {
			return fmt.Errorf("unsafe or unresolved media symlink %q: %w", link, err)
		}
	}
	return nil
}
