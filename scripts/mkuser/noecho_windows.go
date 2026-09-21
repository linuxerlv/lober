//go:build windows

// 本文件实现终端无回显读取的 Windows 分支。部署目标是 Linux/systemd
// （见 noecho_unix.go 顶部注释），本文件的存在只是为了让开发者能在
// Windows 上直接构建与运行本工具（本仓库的开发环境是 Windows 主机），
// 不代表 Windows 是任何一种部署形态。
//
// 实现原理：读取控制台当前模式，关闭 ENABLE_ECHO_INPUT 标志位后写回，
// 读完一行再恢复原模式——与 Linux 分支关闭 termios 的 ECHO 标志位是同一
// 思路在两套不同 API 下的对应实现。
package main

import (
	"bufio"
	"fmt"
	"os"

	"golang.org/x/sys/windows"
)

// readNoEcho 从 f 读一行，期间关闭控制台本地回显。f 必须是一个真实
// 控制台的句柄；若不是（例如被重定向自文件或管道），GetConsoleMode
// 会失败，此时退化为直接按行读取（不视为错误）。
func readNoEcho(f *os.File) (string, error) {
	handle := windows.Handle(f.Fd())

	var oldMode uint32
	if err := windows.GetConsoleMode(handle, &oldMode); err != nil {
		return readLine(f)
	}

	newMode := oldMode &^ windows.ENABLE_ECHO_INPUT
	if err := windows.SetConsoleMode(handle, newMode); err != nil {
		return "", fmt.Errorf("关闭终端回显失败: %w", err)
	}
	defer windows.SetConsoleMode(handle, oldMode)

	return readLine(f)
}

func readLine(f *os.File) (string, error) {
	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil {
		if line == "" {
			return "", err
		}
	}
	return trimNewline(line), nil
}

func trimNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}
