// 本文件实现 Ubuntu 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。
//
// Ubuntu 的内核与 initrd 在 casper 目录下（真实 Ubuntu Server ISO 惯例：
// `casper/vmlinuz`、`casper/initrd`）。**Ubuntu 的"仓库"不是 deb 仓库而是
// 一个 squashfs 镜像**（`casper/filesystem.squashfs`）——curtin 解包它，
// 因此抽取要把那个镜像文件也搬到制品目录，并在 ArtifactPaths.Extra 里
// 记下它的位置（计划任务 5）。这与 RHEL/Debian 都不同：RHEL/Debian 各自
// 有一棵可浏览的仓库目录树（.repo/.pool 之类），Ubuntu 没有——它只有一个
// 整体镜像文件，装机时不解压、curtin 直接对镜像文件操作。
package imports

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
)

const (
	ubuntuInstallerKernelName   = "vmlinuz"
	ubuntuInstallerInitrdName   = "initrd"
	ubuntuInstallerSquashfsName = "filesystem.squashfs"
)

// UbuntuExtraKeySquashfs 是 ArtifactPaths.Extra 中 squashfs 镜像对应的键名
// （对外可见，供 job.go/precheck.go 等未来消费方引用，不必各自重新约定
// 一个字符串常量）。
const UbuntuExtraKeySquashfs = "squashfs"

// findUbuntuCasperFile 在 root 的 casper 目录下查找名为 fileName 的文件，
// 返回其相对路径。与 Debian 的 findDebianInstallerFile 不同，Ubuntu 的
// 内核/initrd/squashfs 都固定落在 casper/ 目录下（不像 Debian 的
// install.<arch>/ 那样随架构变化子目录名），因此这里不需要递归遍历整个
// ISO，直接尝试 "casper/" + fileName 这个固定路径。
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

// ExtractUbuntu 执行 Ubuntu 系的抽取（计划任务 5）：
//  1. 从 casper/ 目录取内核、initrd、squashfs 镜像
//  2. 复制内核与 initrd 到 destDir 下，固定文件名 vmlinuz/initrd（SPEC
//     §4.5 的 Ubuntu 菜单示例："kernel .../vmlinuz ... initrd .../initrd"）
//  3. 复制 squashfs 镜像到 destDir 下，记入 ArtifactPaths.Extra[squashfs]
//     （不建仓库树——Ubuntu 没有可浏览的仓库目录树，装机时 curtin 直接对
//     镜像文件操作，不解压、不需要 nginx 发一堆小文件）
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
	squashfsSrc, err := findUbuntuCasperFile(root, ubuntuInstallerSquashfsName)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 查找 Ubuntu squashfs 镜像失败"+
			"（缺少此文件时机器无法完成 curtin 解包，装机会在此阶段静默卡死）: %w", err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	const kernelDestName = "vmlinuz"
	const initrdDestName = "initrd"
	const squashfsDestName = "casper/filesystem.squashfs"

	if err := copyFileFromFS(root, kernelSrc, filepath.Join(destDir, kernelDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制内核文件失败（来源 %q）: %w", kernelSrc, err)
	}
	if err := copyFileFromFS(root, initrdSrc, filepath.Join(destDir, initrdDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 initrd 文件失败（来源 %q）: %w", initrdSrc, err)
	}

	squashfsDestPath := filepath.Join(destDir, squashfsDestName)
	if err := os.MkdirAll(filepath.Dir(squashfsDestPath), 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建 squashfs 目标目录失败: %w", err)
	}
	if err := copyFileFromFS(root, squashfsSrc, squashfsDestPath); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 squashfs 镜像失败（来源 %q）: %w", squashfsSrc, err)
	}

	return model.ArtifactPaths{
		Kernel: kernelDestName,
		Initrd: []string{initrdDestName},
		// Repo 留空：Ubuntu 没有可浏览的仓库目录树（本文件头部注释），
		// 与 RHEL/Debian 都要求 Repo 非空这一点不同——渲染器侧目前也
		// 不会为 Ubuntu 读取 ctx.Artifact.Paths.Repo（autoinstall.go 的
		// AssembleAutoinstall 没有引用这个字段），因此留空不会造成
		// 悬空引用。
		Extra: map[string]string{
			UbuntuExtraKeySquashfs: squashfsDestName,
		},
	}, nil
}
