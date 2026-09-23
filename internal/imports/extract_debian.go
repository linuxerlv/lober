// 本文件实现 Debian 系的抽取逻辑（对应实现计划 phase2-01 任务 5，依据
// SPEC §2.1、§4.6 第三项）。
//
// Debian 的内核与 initrd 在 installer 目录下（按架构分子目录，真实 Debian
// ISO 惯例是 `install.amd/`、`install.arm64/` 等），文件名固定为 `linux`
// 与 `initrd.gz`（真实 Debian 官方 ISO 内该目录下的文件名）。仓库树是
// `dists/` 与 `pool/` 两棵（计划任务 5），与 RHEL 单棵 `repo/` 树不同，
// 但落盘后统一收进同一个 `repo/` 目录，供渲染器的 RepoURL 统一拼接
// （与 manifest.go 的 ExtractRHEL 采用同一套"固定输出文件名"约定，
// docs/contracts/filesystem.md 硬约束第一条）。
package imports

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
)

// debianInstallerKernelName、debianInstallerInitrdName 是真实 Debian ISO
// installer 目录下内核与 initrd 的文件名——不同 Debian 版本/架构下
// installer 子目录名不同（`install.amd`、`install.arm64` 等），但目录内
// 文件名本身固定为 `linux` 与 `initrd.gz`（Debian 官方 debian-installer
// 打包惯例，与 RHEL 的 `vmlinuz` 不同拼写——之前这里错写成了 `vmlinuz`，
// 与本文件头部注释、实现计划 phase2-01 任务 5 的描述、以及 SPEC §4.5 的
// Debian 菜单示例 `kernel .../linux` 三处都不一致，是一处真实 bug：
// 用真实 Debian netinst ISO 目录树跑会在 install.<arch>/ 下找不到
// vmlinuz 这个文件名，报"找不到内核"，而真实原因是文件名找错了）。
const (
	debianInstallerKernelName = "linux"
	debianInstallerInitrdName = "initrd.gz"
)

// findDebianInstallerFile 在 root 下递归查找名为 fileName 的文件，返回其
// 相对路径。Debian 的 installer 子目录名随架构变化（`install.amd`、
// `install.arm64` 等），不存在类似 RHEL `.treeinfo` 那样的单一特征文件
// 记录具体路径，因此用遍历查找——只在根目录附近查找（不深入 pool/dists
// 两棵仓库树，那两棵体积巨大且不可能包含内核/initrd，深入遍历只会浪费
// 时间），一旦找到即返回。
//
// 找不到时返回空字符串与非 nil error，调用方据此判定"这不是一份完整的
// Debian installer 制品"（计划任务 5 测试要求第四条："缺关键文件时导入
// 置失败且原因具体"）。
func findDebianInstallerFile(root fs.FS, fileName string) (string, error) {
	var found string
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if found != "" {
			return fs.SkipAll
		}
		// dists/ 与 pool/ 是仓库树，体积巨大且不含内核/initrd，跳过以避免
		// 无意义的深度遍历。
		if d.IsDir() && (p == "dists" || p == "pool") {
			return fs.SkipDir
		}
		if !d.IsDir() && path.Base(p) == fileName {
			found = p
			return fs.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("imports: 遍历查找 %q 失败: %w", fileName, err)
	}
	if found == "" {
		return "", fmt.Errorf("imports: 在 ISO 内容中找不到名为 %q 的文件"+
			"（Debian installer 目录下应含此文件，真实路径形如 install.<arch>/%s）",
			fileName, fileName)
	}
	return found, nil
}

// ExtractDebian 执行 Debian 系的抽取（计划任务 5）：
//  1. 递归查找内核（linux）与 initrd（initrd.gz），并校验 dists/ 存在
//  2. 复制到 destDir 下，固定文件名 linux/initrd.gz（SPEC §4.5 的 Debian
//     菜单示例："kernel .../linux ... initrd .../initrd.gz"——内核落盘后
//     改名为 linux 而不是保留 vmlinuz，是本函数与 RHEL 抽取的一处关键
//     差异：RHEL 保留 vmlinuz 这个名字，Debian 官方菜单示例明确用的是
//     linux，两者都是"从源文件名到固定输出文件名"的映射，只是映射目标
//     不同——遵循各自发行版菜单示例的真实拼写，不强行统一）
//  3. 把 dists/ 与 pool/ 两棵仓库树都复制到 destDir/repo/ 下
//  4. 返回 model.ArtifactPaths
func ExtractDebian(root fs.FS, destDir string) (model.ArtifactPaths, error) {
	kernelSrc, err := findDebianInstallerFile(root, debianInstallerKernelName)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 查找 Debian 内核失败: %w", err)
	}
	initrdSrc, err := findDebianInstallerFile(root, debianInstallerInitrdName)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 查找 Debian initrd 失败: %w", err)
	}
	// dists/ 是 Debian 的软件包索引树，preseed 装机在选包阶段依赖它——没有
	// 就不是一份完整的 installer 制品。这里显式校验存在且是目录，而不是让
	// 后面的 copyTreeFromFS 在缺失时静默产出一个空 repo/ 目录：一个内核/
	// initrd 都在但 repo/ 是空的制品集会被标记 ready，机器引导到选包阶段
	// 才会发现问题，比在导入时立刻报错晚得多。
	if info, statErr := fs.Stat(root, "dists"); statErr != nil || !info.IsDir() {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 在 ISO 内容中找不到 dists/ 目录" +
			"（Debian 的软件包索引树，缺失时不是一份完整的 installer 制品，" +
			"preseed 装机会在选包阶段失败）")
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	const kernelDestName = "linux"
	const initrdDestName = "initrd.gz"

	if err := copyFileFromFS(root, kernelSrc, filepath.Join(destDir, kernelDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制内核文件失败（来源 %q）: %w", kernelSrc, err)
	}
	if err := copyFileFromFS(root, initrdSrc, filepath.Join(destDir, initrdDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 initrd 文件失败（来源 %q）: %w", initrdSrc, err)
	}

	const repoDestName = "repo"
	repoDestDir := filepath.Join(destDir, repoDestName)
	if err := copyTreeFromFS(root, ".", repoDestDir, map[string]bool{
		kernelSrc: true, initrdSrc: true,
	}); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 建仓库树失败: %w", err)
	}

	return model.ArtifactPaths{
		Kernel: kernelDestName,
		Initrd: []string{initrdDestName},
		Repo:   repoDestName + "/",
	}, nil
}
