// 本文件实现 SUSE 系（openSUSE Leap / SLES）的抽取逻辑（对应任务
// t07-suse-import，依据 SPEC §4.5 第四行、§2.1 导入流程）。
//
// SPEC §2.1 的"识别特征文件"表（本文件所在包 identify.go 顶部注释引用的
// 那张表）只列了 RHEL/Ubuntu/Debian/ESXi/Windows 五行，没有 SUSE——这是
// SPEC 的一处空白，本任务要求"自己查证"。以下是核实过程与结论，供
// identify.go 的维护者（该文件归 t02 一轨）参考：
//
//  1. openSUSE 官方 linuxrc 项目（负责在真实安装介质上做仓库识别的那段
//     代码）自己维护的文档 linuxrc_repo.md
//     （https://github.com/openSUSE/linuxrc/blob/master/linuxrc_repo.md，
//     已用 curl 直接取到全文核对）第 1 节明确写着：
//     "linuxrc identifies this repo by checking for a file '/content' in
//     the installation repository" —— 但紧接着的警告是
//     "*(This is obsolete since SLE15/openSUSE Leap 15, the newer products
//     use the repomd repositories on the installation media, see below.)*"
//     第 2 节接着说当前（SLE15 / openSUSE Leap 15 及以后）的识别方式是
//     检查 '/repodata/repomd.xml'，且"There is no longer a content file.
//     Instead, there is now a CHECKSUMS file"。
//
//     结论：任务描述里提到的"content 文件"是 SUSE 旧版介质
//     （SLE≤14 / openSUSE≤13.x）的识别特征，对当前仍在维护、值得导入的
//     版本（openSUSE Leap 15.x、SLES 15+）已经过时。**已核实**，不是
//     臆断——见下方 contractGaps 说明，本文件不采用 content 作为判据。
//
//  2. 直接对官方下载服务器发起 HTTP 请求核实当前版本的真实路径（**已
//     核实**，2026-09-23 通过 curl 逐一确认，均返回 HTTP 200）：
//     - https://download.opensuse.org/distribution/leap/15.6/repo/oss/repodata/repomd.xml
//     - https://download.opensuse.org/distribution/leap/15.6/repo/oss/boot/x86_64/loader/linux
//     - https://download.opensuse.org/distribution/leap/15.6/repo/oss/boot/x86_64/loader/initrd
//     - https://download.opensuse.org/distribution/leap/15.6/repo/oss/media.1/products
//     同一路径下 content / content.asc / content.key 三者均返回 HTTP 404，
//     与结论 1 一致。
//
//  3. **单独 repodata/repomd.xml 不能作为 identify.go 的判据**（本文件
//     内部检测用得，identify.go 不能直接照搬）：RHEL/Rocky/AlmaLinux 现代
//     版本同样是 RPM-MD 仓库，也有 repodata/repomd.xml，两者会撞车。
//     identify.go 现有判据用 .treeinfo（RHEL 专属，SUSE 没有）排在前面，
//     所以只要 SUSE 判据排在 .treeinfo 判据之后、且再加上一个 SUSE 专属
//     标记（media.1/products——linuxrc_repo.md 第 3 节点名这是"multi
//     -repository medium"的判据，RHEL/Debian/Ubuntu 都没有这个文件），
//     组合判据就不会与既有四个家族冲突。
//
//  4. 引导制品路径与 RHEL 不同：RHEL 是 images/pxeboot/{vmlinuz,initrd.img}
//     （由 .treeinfo 里 [images-xen]/[images-x86_64] 段记录具体文件名），
//     SUSE 固定是 boot/<arch>/loader/{linux,initrd}——不是从元数据文件解析
//     出来的相对路径，是 SUSE 安装介质的固定目录结构（结论 2 的官方镜像
//     实测已确认对 x86_64 成立）。
//
// 仓库树：SUSE 用 zypper 仓库，落盘时把 repodata/ 与 suse/ 两个顶层目录
// 都收进 destDir/repo/ 下（与 RHEL/Debian 把仓库树收进 repo/ 子目录的
// 约定一致，docs/contracts/filesystem.md 硬约束第一条：制品目录下的路径
// 与制品集里存的相对路径逐字符一致）。
package imports

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
)

// suseLoaderDir、suseKernelName、suseInitrdName 是 SUSE 安装介质固定的
// 引导制品路径（本文件头部注释结论 2、4：boot/x86_64/loader/{linux,initrd}，
// 已对官方镜像实测确认）。当前只覆盖 x86_64——与本包其余家族的抽取器一致
// （RHEL/Debian/Ubuntu 的抽取代码同样只处理 x86_64 路径，未来加其它架构
// 属于独立的任务范围，不在本次改动内）。
const (
	suseLoaderDir  = "boot/x86_64/loader"
	suseKernelName = "linux"
	suseInitrdName = "initrd"
)

// suseRepodataMarker、suseSetupDir 是判定"这确实是一份 SUSE 仓库树"的
// 组合标记（本文件头部注释结论 3）：
//   - repodata/repomd.xml：zypper 仓库元数据入口，SUSE 与现代 RHEL 都有，
//     单独不足以区分家族，但缺失时可以确定"这不是一份完整的仓库树"。
//   - media.1/products：SUSE 特有的多仓库产品索引（linuxrc_repo.md 第 3
//     节），RHEL/Debian/Ubuntu 都没有这个文件，本文件用它做"确实是
//     SUSE"的独立佐证，避免把一棵凑巧有 repodata/ 的 RHEL 树误判成功。
const (
	suseRepodataPath        = "repodata/repomd.xml"
	suseMultiRepoMarkerPath = "media.1/products"
)

// ExtractSUSE 执行 SUSE 系的抽取（任务 t07-suse-import）：
//  1. 确认 boot/x86_64/loader/ 下的内核（linux）与 initrd（initrd）存在
//  2. 确认 repodata/repomd.xml 存在（仓库树元数据入口）
//  3. 确认 media.1/products 存在（SUSE 专属标记，本文件头部注释结论 3——
//     缺失时说明这棵树即使有 repodata/ 也很可能不是真正的 SUSE 介质，
//     直接拒绝比"抽取出一个实际装不了的制品集"更安全）
//  4. 复制内核/initrd 到 destDir 下，文件名沿用源文件名 linux/initrd
//     （与 RHEL 保留 vmlinuz、Debian 改名 linux 的做法呼应：SUSE 官方
//     介质本身文件名就是 linux/initrd，SPEC §4.5 的菜单示例也写的是这
//     两个名字，直接沿用不需要映射）
//  5. 把 repodata/ 与 suse/ 两个顶层目录都复制到 destDir/repo/ 下
//     （media.1/ 与 boot/ 两个目录不是包管理器需要的仓库内容，不复制进
//     repo/，避免制品体积不必要地膨胀）
//  6. 返回 model.ArtifactPaths
func ExtractSUSE(root fs.FS, destDir string) (model.ArtifactPaths, error) {
	kernelSrc := filepath.ToSlash(filepath.Join(suseLoaderDir, suseKernelName))
	if _, err := fs.Stat(root, kernelSrc); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 在 ISO 内容的 %q 找不到 SUSE 内核文件"+
			"（期望路径 %q，SUSE 安装介质的固定引导目录结构）: %w", suseLoaderDir, kernelSrc, err)
	}
	initrdSrc := filepath.ToSlash(filepath.Join(suseLoaderDir, suseInitrdName))
	if _, err := fs.Stat(root, initrdSrc); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 在 ISO 内容的 %q 找不到 SUSE initrd 文件"+
			"（期望路径 %q，SUSE 安装介质的固定引导目录结构）: %w", suseLoaderDir, initrdSrc, err)
	}
	if _, err := fs.Stat(root, suseRepodataPath); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 在 ISO 内容中找不到 %q"+
			"（zypper/YaST 仓库元数据入口，缺失说明这不是一份完整的 SUSE 仓库树）: %w",
			suseRepodataPath, err)
	}
	if _, err := fs.Stat(root, suseMultiRepoMarkerPath); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 在 ISO 内容中找不到 %q"+
			"（SUSE 安装介质专属的多仓库产品索引，缺失时即使存在 repodata/ 也不能确认"+
			"这是一份真正的 SUSE 介质而不是凑巧带同名目录的其它家族）: %w",
			suseMultiRepoMarkerPath, err)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	const kernelDestName = "linux"
	const initrdDestName = "initrd"

	if err := copyFileFromFS(root, kernelSrc, filepath.Join(destDir, kernelDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制内核文件失败（来源 %q）: %w", kernelSrc, err)
	}
	if err := copyFileFromFS(root, initrdSrc, filepath.Join(destDir, initrdDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 initrd 文件失败（来源 %q）: %w", initrdSrc, err)
	}

	const repoDestName = "repo"
	repoDestDir := filepath.Join(destDir, repoDestName)
	for _, treeRoot := range []string{"repodata", "suse"} {
		if _, err := fs.Stat(root, treeRoot); err != nil {
			// suse/ 目录在极简测试 fixture 或某些精简介质变体里可能不存在
			// （真实 DVD 介质上恒定存在，但本函数不假设调用方一定给的是
			// 完整真实介质）——repodata/ 已在上面单独校验过，此处 suse/
			// 缺失不是致命错误，跳过即可，仓库树仍以 repodata/ 为准可用。
			continue
		}
		if err := copyTreeFromFS(root, treeRoot, filepath.Join(repoDestDir, treeRoot), nil); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf("imports: 建仓库树失败（%q）: %w", treeRoot, err)
		}
	}

	return model.ArtifactPaths{
		Kernel: kernelDestName,
		Initrd: []string{initrdDestName},
		Repo:   repoDestName + "/",
	}, nil
}
