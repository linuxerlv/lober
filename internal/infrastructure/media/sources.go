package iso

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Free space uses platform utilities instead of ignored build-tag files, so the
// architecture guard checks every source file on every supported platform.
func freeSpace(ctx context.Context, p string) (int64, error) {
	if err := ctx.Err(); err != nil {
		return 0, err
	}
	st, err := os.Stat(p)
	if err != nil {
		return 0, fmt.Errorf("free space %s: %w", p, err)
	}
	if !st.IsDir() {
		return 0, fmt.Errorf("free space %s: not a directory", p)
	}
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "linux":
		cmd = exec.CommandContext(ctx, "df", "--output=avail", "-B1", "--", p)
	case "windows":
		absolute, err := filepath.Abs(p)
		if err != nil {
			return 0, err
		}
		literal := "'" + strings.ReplaceAll(absolute, "'", "''") + "'"
		cmd = exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", "$ErrorActionPreference='Stop'; [System.IO.DriveInfo]::new([System.IO.Path]::GetPathRoot("+literal+")).AvailableFreeSpace")
	default:
		return 0, fmt.Errorf("free space %s: unsupported platform %s", p, runtime.GOOS)
	}
	data, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("free space %s: %w", p, err)
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0, fmt.Errorf("free space %s: empty utility output", p)
	}
	n, err := strconv.ParseInt(fields[len(fields)-1], 10, 64)
	if err != nil || n < 0 {
		return 0, fmt.Errorf("free space %s: invalid utility output", p)
	}
	return n, nil
}
