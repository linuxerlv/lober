package iso

import (
	"context"
	"github.com/lober/lober/internal/store"
	"io/fs"
	"os"
	"path/filepath"
)

var windowsTools = []string{"wimboot", "winpe-busybox", "winpe-curl"}

const winpeshlContent = "[LaunchApps]\r\n%SYSTEMDRIVE%\\lober\\startnet.cmd\r\n"

func (s *Sources) extractWindows(ctx context.Context, id string, tree fs.FS, tmp string) (map[string]string, error) {
	roles := map[string]string{}
	for role, name := range map[string]string{"bcd": "boot/bcd", "boot-sdi": "boot/boot.sdi", "boot-wim": "sources/boot.wim"} {
		if err := copyImportFile(ctx, tree, name, filepath.Join(tmp, filepath.FromSlash(name))); err != nil {
			return nil, importProblem(id, "iso_artifact_missing", "artifacts."+role, role)
		}
		roles[role] = name
	}
	for _, tool := range windowsTools {
		// Tools are deployment-owned regular files. A link may point outside the injected directory.
		p := filepath.Join(s.dirs.Tools, tool)
		st, err := os.Lstat(p)
		if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
			return nil, importProblem(id, "iso_tool_missing", "tools."+tool, tool)
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, err
		}
		err = writeImportFile(ctx, f, filepath.Join(tmp, tool), 0755)
		f.Close()
		if err != nil {
			return nil, err
		}
		roles[tool] = tool
	}
	if err := os.WriteFile(filepath.Join(tmp, "winpeshl.ini"), []byte(winpeshlContent), 0644); err != nil {
		return nil, err
	}
	roles["winpeshl"] = "winpeshl.ini"
	if len(roles) != 7 {
		return nil, store.ErrInvalid
	}
	return roles, nil
}
