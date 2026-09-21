//go:build !linux && !windows

// 兜底分支：部署目标是 Linux（noecho_unix.go），开发环境是 Windows
// （noecho_windows.go）。其它 GOOS（如 darwin/BSD）不是本任务要支持的
// 平台，但仍提供一个可编译的实现，避免 go build ./... 在这些平台上因为
// 本包缺少 readNoEcho 的实现而报错——只是这个兜底版本会回显输入，不做
// 无回显处理，交互时密码会显示在终端上。
package main

import (
	"bufio"
	"fmt"
	"os"
)

func readNoEcho(f *os.File) (string, error) {
	fmt.Fprintln(os.Stderr, "警告: 当前平台没有实现终端无回显读取，输入的密码将会显示在屏幕上"+
		"（本工具的部署目标是 Linux，见 noecho_unix.go）。")
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
