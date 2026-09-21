// Command mkuser 为 Lober 配置文件生成一条 auth.users 记录用的 bcrypt
// 密码哈希。
//
// 部署前必跑的校验命令：
//
//	go build ./scripts/mkuser/
//	echo "a-long-enough-password" | go run ./scripts/mkuser -user admin -stdin
//	# 期望输出：一行以 $2 开头、长度 60 字符的字符串（bcrypt 哈希的固定格式）
//
// 依据：docs/superpowers/plans/2026-09-20-lober-mvp-09-integration.md 任务 8、
// SPEC.md §9.4（权限分级：控制台账号能建任务=能让机器被擦盘，密码强度与
// 存储方式因此不是可以随意放松的细节）。
//
// 为什么是独立工具而不是主二进制的子命令：本工具需要从终端交互式读密码
// （避免密码留在 shell history 里），而 cmd/lober 跑在 systemd 下、以
// Type=simple 常驻运行——主二进制不应该含任何等待终端输入的代码路径，
// 那会让它在无 TTY 的环境里挂起。两者的运行模型互斥，因此分成两个二进制。
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// bcryptCost 用 12 而非 golang.org/x/crypto/bcrypt 的默认值 10。
//
// 后果权衡（SPEC §9.4 权限分级的直接推论：控制台账号的权限上限是"建任务
// 即授权装机"，泄露成本极高，因此哈希强度值得多花一点计算成本）：
//   - cost 每加 1，bcrypt 的计算量翻倍；12 比默认 10 慢约 4 倍。
//   - 对个位数运维账号、每天几次的登录频率而言，慢 4 倍换来的延迟是
//     毫秒级的差异，用户完全感觉不到。
//   - 但对离线爆破（攻击者拿到数据库或配置文件后脱库跑字典/暴力枚举）
//     而言，同样是慢 4 倍——这是对称的计算成本，只是防守方一次性付出、
//     攻击方每次尝试都要付出，指数级放大了防守收益。
const bcryptCost = 12

// minPasswordLen 是允许的最短密码长度。拒绝短密码不是任意选取的策略，
// 而是直接对应本工具生成的账号能造成的破坏范围：控制台管理员可以创建
// 装机任务（建任务即授权，机器下次网络启动即被擦盘写入新系统），
// 短密码大幅降低了在线撞库或离线爆破找到明文的成本。
const minPasswordLen = 12

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

// run 是可测试的入口：所有 I/O 通过参数传入，避免直接依赖 os.Stdin 等
// 全局状态，方便未来补单元测试（本工具当前的验证方式是端到端的 -stdin
// 手动运行，见文件头注释）。
func run(args []string, stdin *os.File, stdout, stderr *os.File) int {
	fs := flag.NewFlagSet("mkuser", flag.ContinueOnError)
	fs.SetOutput(stderr)

	user := fs.String("user", "", "用户名（仅用于提示信息，不影响哈希输出内容）")
	fromStdin := fs.Bool("stdin", false, "非交互模式：从标准输入读一行作为密码，不做二次确认输入。\n"+
		"仅供自动化脚本使用（例如端到端测试脚本需要非交互地造用户）——\n"+
		"交互式提示时不回显、且需要二次确认输入相同的密码才继续，\n"+
		"目的是避免把密码留在 shell 命令历史里；-stdin 绕开了这层保护，\n"+
		"人工使用时请优先用默认的交互模式。")

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *user == "" {
		fmt.Fprintln(stderr, "mkuser: 缺少 -user 参数（仅用于提示信息，仍然必填以避免生成的哈希"+
			"不知道对应哪个账号）")
		fs.Usage()
		return 2
	}

	var password string
	var err error
	if *fromStdin {
		password, err = readPasswordFromStdinLine(stdin)
	} else {
		password, err = readPasswordInteractive(stdin, stderr, *user)
	}
	if err != nil {
		fmt.Fprintf(stderr, "mkuser: 读取密码失败: %v\n", err)
		return 1
	}

	if len(password) < minPasswordLen {
		fmt.Fprintf(stderr, "mkuser: 密码长度 %d 小于最短要求 %d 个字符，已拒绝。\n"+
			"这不是随意设置的策略：这个账号能创建装机任务，而建任务即授权装机"+
			"（SPEC §3.2/§9.4）——下一次目标机器网络启动就会被擦盘装新系统。\n"+
			"密码强度直接决定这个能力被盗用的难度，请使用更长的密码。\n",
			len(password), minPasswordLen)
		return 1
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcryptCost)
	if err != nil {
		fmt.Fprintf(stderr, "mkuser: bcrypt 计算失败: %v\n", err)
		return 1
	}

	// 哈希打到标准输出、提示打到标准错误——这样调用方可以直接把标准输出
	// 管道进配置文件片段（或用 $() 捕获），标准错误的提示信息不会混进去。
	fmt.Fprintln(stdout, string(hash))
	fmt.Fprintf(stderr, "mkuser: 已为用户 %q 生成 bcrypt 哈希（cost=%d，长度 %d 字符）。\n"+
		"把上面标准输出的那一行填进配置文件 auth.users[].bcrypt_hash 字段。\n",
		*user, bcryptCost, len(hash))
	return 0
}

// readPasswordFromStdinLine 从 stdin 读一行作为密码，去掉行尾换行符。
// 供自动化调用；不做二次确认、不检查是否连着一个真实终端。
func readPasswordFromStdinLine(stdin *os.File) (string, error) {
	reader := bufio.NewReader(stdin)
	line, err := reader.ReadString('\n')
	if err != nil && line == "" {
		return "", fmt.Errorf("从标准输入读取失败: %w", err)
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// readPasswordInteractive 交互式读两次密码并比对，期间关闭终端回显。
// 两次不一致时提示重试，不静默接受任意一次的结果——密码工具打错一个字符
// 而不自知，后果是生成了一个任何人都不知道的密码哈希，账号事实上被锁死。
func readPasswordInteractive(stdin *os.File, stderr *os.File, user string) (string, error) {
	fmt.Fprintf(stderr, "为用户 %q 设置密码（输入时不会显示字符）\n", user)

	for {
		fmt.Fprint(stderr, "密码: ")
		p1, err := readNoEcho(stdin)
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}

		fmt.Fprint(stderr, "请再输入一次确认: ")
		p2, err := readNoEcho(stdin)
		fmt.Fprintln(stderr)
		if err != nil {
			return "", err
		}

		if p1 != p2 {
			fmt.Fprintln(stderr, "两次输入不一致，请重新输入。")
			continue
		}
		return p1, nil
	}
}
