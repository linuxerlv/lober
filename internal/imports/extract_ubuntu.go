// 本文件实现 Ubuntu 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。
//
// Ubuntu 的内核与 initrd 在 casper 目录下（真实 Ubuntu Server ISO 惯例：
// `casper/vmlinuz`、`casper/initrd`，经核实在 22.04/24.04 上文件名保持
// 不变）。**Ubuntu 的"仓库"不是 deb 仓库而是一个或多个 squashfs 镜像**
// ——curtin 解包它们，因此抽取要把镜像文件搬到制品目录，并在
// ArtifactPaths.Extra 里记下每一个的位置（计划任务 5）。这与 RHEL/Debian
// 都不同：RHEL/Debian 各自有一棵可浏览的仓库目录树（.repo/.pool 之类），
// Ubuntu 没有——它是整体镜像文件，装机时不解压、curtin 直接对镜像文件
// 操作。
//
// squashfs 文件名与数量不能硬编码为单一常量（这是本文件与早期版本的关键
// 差异，核实后修正）：老版本 Ubuntu 用单一的 casper/filesystem.squashfs，
// 但 Ubuntu **Server** 自 22.04 起改成"基础层 + 覆盖层"两段式结构
// （casper/ubuntu-server-minimal.squashfs 加
// casper/ubuntu-server-minimal.ubuntu-server.squashfs，覆盖层含 WiFi
// 驱动等 Server 专属内容），其他变体（如 Xubuntu Minimal）用的是另一套
// 命名且段数还可能更多。只抽取其中一段、漏掉别的段，产出的制品集能通过
// 完整性校验（因为校验只查 Extra 里记的路径是否存在，不知道"本该有几
// 段"），但装出来的机器会缺功能组件甚至装不起来——这正是第三方工具 Cubic
// 曾经踩过的真实故障（github.com/muquit/cubic-ubuntu-server-fix 的
// README 记录了这个 bug：漏抽覆盖层导致"non-functional ISO missing
// important components"）。因此本文件的策略是保守：扫描 casper/ 目录下
// 全部 *.squashfs 文件，一个不漏地搬运，每个都以其原始文件名为键记入
// Extra，不管当前 Ubuntu 版本用的是一段、两段还是更多段。
package imports

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

const (
	ubuntuInstallerKernelName = "vmlinuz"
	ubuntuInstallerInitrdName = "initrd"
	// ubuntuSquashfsExt 是 casper/ 目录下需要被当作仓库镜像搬运的文件的
	// 扩展名——不枚举具体文件名（见本文件头部注释：不同 Ubuntu 版本/变体
	// 的 squashfs 文件名与段数都不同，唯一稳定的共同点是扩展名）。
	ubuntuSquashfsExt = ".squashfs"
)

// findUbuntuCasperFile 在 root 的 casper 目录下查找名为 fileName 的文件，
// 返回其相对路径。与 Debian 的 findDebianInstallerFile 不同，Ubuntu 的
// 内核/initrd 都固定落在 casper/ 目录下（不像 Debian 的 install.<arch>/
// 那样随架构变化子目录名），因此这里不需要递归遍历整个 ISO，直接尝试
// "casper/" + fileName 这个固定路径。
//
// 找不到时返回描述性错误（计划任务 5 测试要求第四条）。
func findUbuntuCasperFile(root fs.FS, fileName string) (string, error) {
	candidate := path.Join("casper", fileName)
	if _, err := fs.Stat(root, candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("imports: 在 ISO 内容的 casper/ 目录下找不到 %q"+
		"（期望路径 %q）", fileName, candidate)
}

// findUbuntuCasperSquashfsFiles 扫描 root 的 casper 目录，返回其下全部
// *.squashfs 文件的相对路径（按文件名排序，保证返回结果与后续 Extra 键的
// 遍历顺序在多次调用间稳定，方便测试断言与日志排查）。
//
// 不假设固定数量或固定文件名（本文件头部注释已说明原因：不同 Ubuntu
// 版本/变体的 squashfs 段数与命名都不同）。一个都找不到时返回非 nil
// error（计划任务 5 测试要求第四条，Ubuntu 特有场景：squashfs 缺失时
// curtin 解包阶段才会发现，抽取阶段就该拒绝）。
func findUbuntuCasperSquashfsFiles(root fs.FS) ([]string, error) {
	entries, err := fs.ReadDir(root, "casper")
	if err != nil {
		return nil, fmt.Errorf("imports: 读取 casper/ 目录失败: %w", err)
	}

	var found []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if !strings.HasSuffix(entry.Name(), ubuntuSquashfsExt) {
			continue
		}
		found = append(found, path.Join("casper", entry.Name()))
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("imports: 在 ISO 内容的 casper/ 目录下找不到任何 %q 文件"+
			"（不同 Ubuntu 版本/变体的具体文件名不同，但至少应有一个，缺失时机器无法"+
			"完成 curtin 解包，装机会在此阶段静默卡死）", ubuntuSquashfsExt)
	}
	sort.Strings(found)
	return found, nil
}

// ExtractUbuntu 执行 Ubuntu 系的抽取（计划任务 5）：
//  1. 从 casper/ 目录取内核、initrd，以及全部 squashfs 镜像（数量不固定，
//     见本文件头部注释）
//  2. 复制内核与 initrd 到 destDir 下，固定文件名 vmlinuz/initrd（SPEC
//     §4.5 的 Ubuntu 菜单示例："kernel .../vmlinuz ... initrd .../initrd"）
//  3. 把每一个 squashfs 镜像复制到 destDir/casper/ 下（保留原始文件名，
//     因为不同镜像之间除了文件名没有别的区分手段），记入
//     ArtifactPaths.Extra，键为该镜像的原始文件名（不建仓库树——Ubuntu
//     没有可浏览的仓库目录树，装机时 curtin 直接对镜像文件操作，不解压、
//     不需要 nginx 发一堆小文件）
//  4. 返回 model.ArtifactPaths
func ExtractUbuntu(root fs.FS, destDir string) (model.ArtifactPaths, error) {
	kernelSrc, err := findUbuntuCasperFile(root, ubuntuInstallerKernelName)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 查找 Ubuntu 内核失败: %w", err)
	}
	initrdSrc, err := findUbuntuCasperFile(root, ubuntuInstallerInitrdName)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 查找 Ubuntu initrd 失败: %w", err)
	}
	squashfsSrcs, err := findUbuntuCasperSquashfsFiles(root)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 查找 Ubuntu squashfs 镜像失败: %w", err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	const kernelDestName = "vmlinuz"
	const initrdDestName = "initrd"

	if err := copyFileFromFS(root, kernelSrc, filepath.Join(destDir, kernelDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制内核文件失败（来源 %q）: %w", kernelSrc, err)
	}
	if err := copyFileFromFS(root, initrdSrc, filepath.Join(destDir, initrdDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 initrd 文件失败（来源 %q）: %w", initrdSrc, err)
	}

	squashfsDestDir := filepath.Join(destDir, "casper")
	if err := os.MkdirAll(squashfsDestDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建 squashfs 目标目录失败: %w", err)
	}

	extra := make(map[string]string, len(squashfsSrcs))
	for _, squashfsSrc := range squashfsSrcs {
		fileName := path.Base(squashfsSrc)
		squashfsDestName := path.Join("casper", fileName)
		squashfsDestPath := filepath.Join(destDir, "casper", fileName)
		if err := copyFileFromFS(root, squashfsSrc, squashfsDestPath); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 squashfs 镜像失败（来源 %q）: %w", squashfsSrc, err)
		}
		extra[fileName] = squashfsDestName
	}

	return model.ArtifactPaths{
		Kernel: kernelDestName,
		Initrd: []string{initrdDestName},
		// Repo 留空：Ubuntu 没有可浏览的仓库目录树（本文件头部注释），
		// 与 RHEL/Debian 都要求 Repo 非空这一点不同——渲染器侧目前也
		// 不会为 Ubuntu 读取 ctx.Artifact.Paths.Repo（autoinstall.go 的
		// AssembleAutoinstall 没有引用这个字段），因此留空不会造成
		// 悬空引用。
		Extra: extra,
	}, nil
}
