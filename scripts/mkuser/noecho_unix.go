//go:build linux

// 本文件实现终端无回显读取的 Linux 分支——deploy/systemd/lober.service
// 面向的部署目标就是 Linux，本工具在其它系统上编译时用
// noecho_fallback.go 的可回显读取兜底（见该文件注释）。
//
// 实现原理与 golang.org/x/term 相同：读取当前 termios 设置，关闭 ECHO
// 标志位后写回，读完一行再恢复原设置。之所以不直接依赖
// golang.org/x/term：本仓库 go.mod 当前没有声明这个模块（只有
// golang.org/x/sys 作为其它依赖的间接依赖已经存在且可直接编译使用），
// 而本任务的铁律禁止修改 go.mod/go.sum 或跑 go get/go mod tidy。
// TCGETS/TCSETS 的 ioctl 请求号在不同 Unix 系统上并不相同（Linux 用
// TCGETS/TCSETS，BSD/Darwin 用 TIOCGETA/TIOCSETA），因此本文件的 build
// tag 精确限定为 linux，不泛化到其它 Unix——泛化需要为每个系统单独确认
// 常量与函数签名，而本任务的部署目标只有 Linux。
package main

import (
	"bufio"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

// readNoEcho 从 f 读一行，期间关闭终端本地回显。f 必须是一个真实终端的
// 文件描述符；若不是终端（例如被重定向自文件或管道，如测试或 CI 环境），
// ioctl 会失败，此时退化为直接按行读取（不视为错误）。
func readNoEcho(f *os.File) (string, error) {
	fd := int(f.Fd())

	oldState, err := unix.IoctlGetTermios(fd, unix.TCGETS)
	if err != nil {
		return readLine(f)
	}

	newState := *oldState
	newState.Lflag &^= unix.ECHO
	if err := unix.IoctlSetTermios(fd, unix.TCSETS, &newState); err != nil {
		return "", fmt.Errorf("关闭终端回显失败: %w", err)
	}
	defer unix.IoctlSetTermios(fd, unix.TCSETS, oldState)

	return readLine(f)
}

func readLine(f *os.File) (string, error) {
	reader := bufio.NewReader(f)
	line, err := reader.ReadString('\n')
	if err != nil {
		// 允许最后一行没有换行符就结束（例如手动 Ctrl-D）。
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
