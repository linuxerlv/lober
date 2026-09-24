// 本文件实现 manifest 生成与 RHEL 系的抽取逻辑。对应实现计划任务 10，
// 依据 SPEC §2.1、§4.6 第三项、docs/contracts/filesystem.md。
//
// ISO 是导入期制品，不是运行时制品（SPEC §2.1）：iPXE 引导的是从 ISO 里
// 取出的内核与 initrd，再用内核参数指向解压后的仓库树。因此解压与抽取在
// 导入时一次完成，装机时零解压、nginx 直接发静态文件。
package imports

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// ---------------------------------------------------------------------------
// .treeinfo 解析
// ---------------------------------------------------------------------------

// treeinfoImages 是从 .treeinfo 里解析出的镜像段内容：内核与 initrd 的
// 相对路径（相对于 ISO 根目录，任务 10 第一步：从特征文件里读内核与
// initrd 的路径）。
type treeinfoImages struct {
	Kernel string
	Initrd string

	// Repository 是 .treeinfo 声明的仓库子目录（相对 ISO 根），空串表示仓库
	// 就在根目录。
	//
	// **这个字段的存在理由是一个真实 ISO 上的阻断性 bug。** RHEL 9 及其衍生
	// （Rocky、AlmaLinux、OpenCloudOS）把仓库放在 BaseOS/ 下，ISO 根目录
	// 没有 repodata/；而老的 RHEL 7 系与一些精简介质确实把 repodata 放在根。
	// 两种布局都真实存在，唯一可靠的判据是 .treeinfo 自己的声明——猜不得。
	Repository string
}

// parseTreeinfo 解析 .treeinfo 文本（INI 格式），从形如
// "[images-x86_64]" 的镜像段里读 kernel 与 initrd 两个键。
//
// 段名前缀固定为 "images-"（真实 RHEL/Rocky/Fedora 系 .treeinfo 的镜像段
// 命名惯例，如 [images-x86_64]、[images-xen]），只要段名以该前缀开头就
// 尝试读取——不要求精确匹配 "images-x86_64"，因为不同架构的镜像段名后缀
// 不同，本函数不关心架构本身（架构一致性校验是 M1 Prechecker 的职责，
// 见 contract/service.go），只关心「哪个段包含 kernel/initrd 这两个键」。
//
// 缺镜像段、或镜像段里 kernel/initrd 路径为空，均返回错误（任务 10 明确
// 要求：特征文件格式异常时返回错误）。
func parseTreeinfo(data []byte) (treeinfoImages, error) {
	sections := parseINI(string(data))
	repository := treeinfoRepository(sections)

	for name, kv := range sections {
		if !strings.HasPrefix(name, "images-") {
			continue
		}
		kernel := strings.TrimSpace(kv["kernel"])
		initrd := strings.TrimSpace(kv["initrd"])
		if kernel == "" || initrd == "" {
			continue // 该镜像段本身不完整，继续找下一个（真实 ISO 常有多个镜像段，如 images-xen）
		}
		return treeinfoImages{Kernel: kernel, Initrd: initrd, Repository: repository}, nil
	}

	return treeinfoImages{}, fmt.Errorf("imports: .treeinfo 中没有找到含完整 kernel/initrd 的镜像段" +
		"（形如 [images-<arch>]）——特征文件格式异常，不猜测默认路径")
}

// treeinfoRepository 从 .treeinfo 里取仓库子目录，取不到返回空串（表示仓库
// 在 ISO 根）。
//
// 两处来源，按优先级：
//
//  1. `[general] repository`——productmd 为兼容 pre-productmd 的 treeinfo 保留
//     的那一段，真实 RHEL 9 系镜像里就有（实测 OpenCloudOS 9.6：
//     `repository = BaseOS`）。
//  2. `[variant-<主变体>] repository`——productmd 的正式位置。主变体名取
//     `[general] variant`。
//
// 先查 general 是因为它是扁平的一个键，不需要先知道主变体叫什么；查不到再按
// variant 绕一圈。两者通常一致（实测那份镜像两处都是 BaseOS）。
//
// 刻意不遍历「所有 variant-* 段，随便取一个有 repository 的」：一份 DVD 常有
// 多个变体（BaseOS 与 AppStream，或 BaseOS 与 Minimal），而 map 遍历顺序随机，
// 那样会让同一份 ISO 两次导入得到不同的仓库路径——一种只在换了机器或换了 Go
// 版本之后才显现的不确定性。
func treeinfoRepository(sections map[string]map[string]string) string {
	general := sections["general"]

	if repo := strings.TrimSpace(general["repository"]); repo != "" && repo != "." {
		return repo
	}

	variant := strings.TrimSpace(general["variant"])
	if variant == "" {
		return ""
	}
	if repo := strings.TrimSpace(sections["variant-"+variant]["repository"]); repo != "" && repo != "." {
		return repo
	}
	return ""
}

// parseINI 是一个刻意从简的 INI 解析器：只支持 "[section]" 段头与
// "key = value" / "key=value" 键值行，忽略空行与以 "#"/";" 开头的注释行。
//
// 不引入第三方 INI 解析库：本模块的依赖白名单不含任何 INI 解析包
// （见模块级 import 限制），而 .treeinfo 的实际语法子集足够简单——真实
// 文件里不会出现需要处理转义、多行值、或段名重复合并这类复杂特性，手写
// 几十行覆盖到「段 + 扁平键值对」就已经是这份契约需要的全部能力。
func parseINI(text string) map[string]map[string]string {
	sections := make(map[string]map[string]string)
	currentSection := ""
	sections[currentSection] = make(map[string]string)

	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, ";") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentSection = strings.TrimSpace(line[1 : len(line)-1])
			if _, ok := sections[currentSection]; !ok {
				sections[currentSection] = make(map[string]string)
			}
			continue
		}
		idx := strings.Index(line, "=")
		if idx < 0 {
			continue // 不是键值行也不是段头，忽略（真实 .treeinfo 不应出现这类行，但不因此让整个解析失败）
		}
		key := strings.TrimSpace(line[:idx])
		value := strings.TrimSpace(line[idx+1:])
		sections[currentSection][key] = value
	}

	return sections
}

// ---------------------------------------------------------------------------
// RHEL 抽取
// ---------------------------------------------------------------------------

// ExtractRHEL 执行 RHEL 系的四步抽取（任务 10）：
//  1. 从 .treeinfo 读内核与 initrd 的路径
//  2. 把内核与 initrd 复制到 destDir 下（按 docs/contracts/filesystem.md
//     的布局：直接在制品集根目录下，文件名固定为 vmlinuz/initrd.img）
//  3. 建仓库树：把 root 的其余内容复制到 destDir/repo/ 下
//  4. 返回 model.ArtifactPaths，供调用方（job.go）连同 manifest 一并写回
//
// root 是 Identify 确认过是 RHEL 家族之后的 ISO 内容视图；destDir 是制品
// 集在磁盘上的根目录（对应文件系统契约的
// /srv/lober/artifacts/<artifact_set_id>/，本函数不关心这个路径怎么算出
// 来，调用方负责传入正确的绝对路径）。
//
// 落盘路径必须严格按文件系统契约（docs/contracts/filesystem.md 硬约束
// 第一条）：内核固定复制为 destDir/vmlinuz，initrd 固定复制为
// destDir/initrd.img，仓库树固定复制到 destDir/repo/ 下——这三个文件名/
// 目录名不是从 .treeinfo 里原样带过来的（.treeinfo 里的路径可能是
// images/pxeboot/vmlinuz 这种带子目录的形式），因为 M4 渲染器与本函数
// 必须读同一份约定才能拼出一致的 URL，用「固定输出文件名」消除任何拼接
// 歧义，比要求两个模块都记住同一段可变路径更不容易出错。
func ExtractRHEL(root fs.FS, destDir string) (model.ArtifactPaths, error) {
	treeinfoData, err := fs.ReadFile(root, ".treeinfo")
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 读取 .treeinfo 失败: %w", err)
	}

	images, err := parseTreeinfo(treeinfoData)
	if err != nil {
		return model.ArtifactPaths{}, err
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	const kernelDestName = "vmlinuz"
	const initrdDestName = "initrd.img"

	if err := copyFileFromFS(root, images.Kernel, filepath.Join(destDir, kernelDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制内核文件失败（来源 %q）: %w", images.Kernel, err)
	}
	if err := copyFileFromFS(root, images.Initrd, filepath.Join(destDir, initrdDestName)); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 initrd 文件失败（来源 %q）: %w", images.Initrd, err)
	}

	const repoDestName = "repo"
	repoDestDir := filepath.Join(destDir, repoDestName)
	if err := copyTreeFromFS(root, ".", repoDestDir, map[string]bool{
		".treeinfo": true, images.Kernel: true, images.Initrd: true,
	}); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 建仓库树失败: %w", err)
	}

	// Repo 指向 .treeinfo 声明的 repository 子目录，不是 ISO 根的副本。
	//
	// 整棵树仍然完整复制到 repo/ 下（AppStream、Minimal、images、EFI 都保留，
	// 将来若要装第二个仓库或取 stage2 镜像都还在），只是**对外声明的仓库位置**
	// 落到真正含 repodata/ 的那一层。
	//
	// 不这么做的后果有两层，都是硬失败（实测依据见
	// TestExtractRHEL_RepoPointsAtDeclaredRepository）：渲染器会把
	// `url --url=.../repo/` 交给 anaconda 而那里没有 repodata；更早一层，
	// statemachine 的 precheck 查 <repo>/repodata/repomd.xml 查不到就拒签
	// 装机 token，机器连引导菜单都拿不到。
	repoRel := repoDestName + "/"
	if images.Repository != "" {
		repoRel = path.Join(repoDestName, images.Repository) + "/"
	}

	return model.ArtifactPaths{
		Kernel: kernelDestName,
		Initrd: []string{initrdDestName},
		Repo:   repoRel,
	}, nil
}

// copyFileFromFS 把 src（root 内的相对路径）复制为 destPath（本地绝对
// 路径）。
func copyFileFromFS(root fs.FS, src, destPath string) error {
	in, err := root.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件 %q 失败: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(destPath)
	if err != nil {
		return fmt.Errorf("创建目标文件 %q 失败: %w", destPath, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("复制内容到 %q 失败: %w", destPath, err)
	}
	return nil
}

// copyTreeFromFS 把 root 中 srcRoot 目录下的全部内容（跳过 skip 中列出的
// 相对路径）递归复制到本地目录 destRoot。
func copyTreeFromFS(root fs.FS, srcRoot, destRoot string, skip map[string]bool) error {
	return fs.WalkDir(root, srcRoot, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if skip[p] {
			if d.IsDir() {
				return fs.SkipDir
			}
			return nil
		}

		relPath := p
		if srcRoot != "." {
			rel, err := filepath.Rel(srcRoot, p)
			if err != nil {
				return err
			}
			relPath = rel
		}
		destPath := filepath.Join(destRoot, relPath)

		if d.IsDir() {
			return os.MkdirAll(destPath, 0o755)
		}
		return copyFileFromFS(root, p, destPath)
	})
}

// ---------------------------------------------------------------------------
// manifest 生成
// ---------------------------------------------------------------------------

// ManifestEntry 是 manifest 中一个文件的记录：相对路径与 sha256。
type ManifestEntry struct {
	RelPath string
	SHA256  string
}

// Manifest 是整个制品集的清单：每个文件的相对路径与 sha256，加一个整体
// 校验值（任务 10：服务于制品完整性校验与控制台的「校验完整性」端点）。
type Manifest struct {
	Entries []ManifestEntry
	SHA256  string // 整体校验值，由全部条目按固定顺序拼接后再算一次 sha256 得到
}

// GenerateManifest 遍历 destDir 下的全部文件，逐个计算 sha256，生成清单。
//
// 遍历顺序固定为按相对路径字典序（任务 10：manifest 含全部文件且 sha256
// 正确）——不依赖文件系统的目录遍历顺序（不同操作系统/文件系统的遍历
// 顺序不保证一致），否则同一份制品在两台服务器上导入会得到内容相同但
// 顺序不同的 manifest，进而算出不同的整体校验值，「同一份 ISO 导入两次
// 应该产出相同 manifest」这条隐含期望就被破坏了。
func GenerateManifest(destDir string) (Manifest, error) {
	var entries []ManifestEntry

	err := filepath.WalkDir(destDir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(destDir, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)

		sum, err := sha256File(p)
		if err != nil {
			return fmt.Errorf("计算 %q 的 sha256 失败: %w", rel, err)
		}
		entries = append(entries, ManifestEntry{RelPath: rel, SHA256: sum})
		return nil
	})
	if err != nil {
		return Manifest{}, fmt.Errorf("imports: 生成 manifest 失败: %w", err)
	}

	sort.Slice(entries, func(i, j int) bool { return entries[i].RelPath < entries[j].RelPath })

	overall := sha256.New()
	for _, e := range entries {
		overall.Write([]byte(e.RelPath))
		overall.Write([]byte("\x00"))
		overall.Write([]byte(e.SHA256))
		overall.Write([]byte("\n"))
	}

	return Manifest{
		Entries: entries,
		SHA256:  hex.EncodeToString(overall.Sum(nil)),
	}, nil
}

// sha256File 计算本地文件 p 的 sha256（裸十六进制小写，与
// model.HookDigest 使用的算法/编码一致，供人工用 sha256sum 交叉核对）。
func sha256File(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()

	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
