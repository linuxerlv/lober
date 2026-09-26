// 本文件判定制品集的身份三项：架构、OS 版本、固件模式。
//
// **为什么需要它。** 在它出现之前，导入路径（job.go → SetArtifactSetContent）
// 只写 OSFamily 与 Paths，`ArtifactSet.Arch` 与 `Firmware` 永远是零值。而
// statemachine/precheck.go 的两道硬闸门直接拿它们比对：
//
//	checkArch:     actualArch == artifact.Arch  →  "x86_64" == ""  →  拒绝
//	checkFirmware: artifact.MatchesFirmware(actualFirmware)  →  "" 不是 any  →  拒绝
//
// 于是真实导入产出的制品集能顺利进到 ready 状态，却在**每一次** /v1/boot 被
// 拒，报的还是「机器上报架构 "x86_64"，任务制品集要求架构 ""」这种把人往
// 机器侧和 iPXE 侧带的错误信息。
//
// 这个缺口此前被七个 e2e 脚本完整掩盖：它们一律绕过导入、用 psql 直接把
// artifact_sets 的 arch/firmware UPDATE 成 x86_64/any（scripts/e2e/lib-db.sh
// 的 db_artifact_ready），因此「导入管线已接上」这个结论一直只在单元测试层
// 成立——与 commit dab857b（ISO 9660 解析器在真实镜像上必然失败）是同一类
// 问题：管线的上一环修通之后，下一环仍然断着，而测试夹具两头都替它接上了。
package imports

import (
	"errors"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/lober/lober/internal/core/model"
)

// ErrArchUndetectable 表示这个家族的镜像元数据里找不到架构判据。
//
// 刻意做成**导入期硬失败**而不是留空或兜底成 x86_64，理由是两种替代都更差：
//
//   - 留空：制品集进到 ready，但每次装机都在 /v1/boot 被 precheck 拒，
//     错误信息指向机器侧，排查的人要先怀疑一遍 iPXE 与固件才可能回到导入。
//   - 兜底成 x86_64：更危险。制品集声明 x86_64、机器实际是 arm64 时，
//     precheck 的 arch 闸门反而**放行**，内核在目标机上直接加载失败——
//     那是一个没有任何服务端记录的失败，只能到 BMC 控制台上看。
//
// 早失败一次，胜过每次装机都失败一次。补一个家族的判据时，往
// detectArch 的分派里加一支，并配一份真实镜像的测试夹具。
var ErrArchUndetectable = errors.New("imports: 无法从镜像元数据判定架构")

// DetectIdentity 判定制品集身份。
//
// declaredFirmware 只对 Windows 有意义（那是调用方登记时声明、并由
// ExtractWindows 用 boot.wim 实测位宽校验过的值），其余家族一律按本文件的
// 规则改写，不采纳调用方的声明——详见 firmwareFor。
func DetectIdentity(root fs.FS, family model.OSFamily, declaredFirmware model.FirmwareMode) (model.ArtifactIdentity, error) {
	arch, version, err := detectArchAndVersion(root, family)
	if err != nil {
		return model.ArtifactIdentity{}, err
	}
	return model.ArtifactIdentity{
		OSFamily:  family,
		OSVersion: version,
		Arch:      arch,
		Firmware:  firmwareFor(family, declaredFirmware),
	}, nil
}

// firmwareFor 按家族定固件维度。规则来自 model.ArtifactSet.Firmware 的字段
// 文档，不是本文件的发明：
//
//	Linux 四家   any    一套 kernel/initrd 通吃 BIOS 与 UEFI
//	Windows      声明值  boot.wim 位宽决定，同一版本按固件导两个制品集（§2.1）
//	ESXi         uefi    §0.3：仅 UEFI，BIOS 需 syslinux 链，不在覆盖范围
//
// ESXi 这一支刻意**覆盖**调用方的声明而不是校验后报错：ExtractESXi 已经对
// 非 uefi 的声明值做过硬拒绝（extract_esxi.go），走到这里说明抽取已经通过，
// 再报一次错只会把同一件事说两遍；而把它规范化成 uefi 能保证即便将来某条
// 调用路径漏了那道校验，落库的值仍然是对的。
func firmwareFor(family model.OSFamily, declared model.FirmwareMode) model.FirmwareMode {
	switch family {
	case model.OSFamilyWindows:
		return declared
	case model.OSFamilyESXi:
		return model.FirmwareUEFI
	default:
		return model.FirmwareAny
	}
}

// detectArchAndVersion 按家族分派架构与版本的判定。
//
// 每一支只读**该家族抽取器本来就要读的那份元数据**，不新增对镜像结构的假设：
// 判据来源与 identify.go 的家族判据、以及各 extract_*.go 的输入保持一致，
// 这样「认出了家族」「抽得出制品」「判得出架构」三件事的前提是同一套文件。
func detectArchAndVersion(root fs.FS, family model.OSFamily) (model.Arch, string, error) {
	switch family {
	case model.OSFamilyRHEL:
		return archFromTreeinfo(root)
	case model.OSFamilyDebian:
		return archFromAPTBinaryDir(root)
	case model.OSFamilyUbuntu:
		return archFromDiskInfo(root)
	case model.OSFamilySUSE:
		// SUSE 的架构由抽取器自身的约束决定，不是猜的：extract_suse.go 把
		// 内核路径硬编码成 suseLoaderDir = "boot/x86_64/loader"（那份路径
		// 已对官方镜像实测确认，见该文件头部注释结论 2、4）。抽取成功蕴含
		// 这个目录存在，也就蕴含这份介质是 x86_64。
		//
		// 将来给 SUSE 支持 arm64 时，suseLoaderDir 必须先变成按架构拼接的
		// 形式——那一刻这里也要跟着改成从路径里取架构。两处的耦合是刻意的：
		// 谁都不该单独改动。
		return archFromSUSELoaderDir()
	case model.OSFamilyWindows:
		// 同理由抽取与渲染链路的约束决定：本项目的 Windows 路径是
		// wimboot + BCD/boot.sdi/boot.wim，render/winpe.go 渲染的
		// `bcdboot W:\Windows /s S: /f UEFI` 与三个 initrd 行都是 x64 形态，
		// §0.3 也没把 arm64 Windows 列进覆盖范围。
		//
		// 注意这**不是**固件维度：固件由 boot.wim 的 PE 位宽决定，已由
		// ExtractWindows 用 WimArchDetector 实测校验（那个探测器至今只有
		// 测试假实现，是既有缺口），与这里的 CPU 架构是两件事。
		return model.ArchX86_64, "", nil
	case model.OSFamilyESXi:
		// ESXi 只发行 x86_64（VMware 从未发行过 arm64 的 ESXi 通用版；
		// ESXi-Arm 是独立的 Fling 预览产品，不在 §0.3 覆盖范围内）。
		// 这不是「判定不出所以兜底」，是这个家族的取值集合只有一个元素。
		return model.ArchX86_64, "", nil
	default:
		return "", "", fmt.Errorf("%w：不认识的 OS 家族 %q，没有对应的架构判据"+
			"——新增家族时要往 detectArchAndVersion 加一支，并配一份真实镜像的夹具",
			ErrArchUndetectable, family)
	}
}

// archFromSUSELoaderDir 返回 SUSE 介质的架构。
//
// 单独成一个函数而不是直接 return 常量，是为了让它与 suseLoaderDir 之间的
// 依赖在代码里看得见：这个值的正确性完全依赖那个常量仍然是硬编码的
// "boot/x86_64/loader"。哪天它变成按架构拼接，编译器不会提醒，但这个函数的
// 实现会立刻显得可疑。
func archFromSUSELoaderDir() (model.Arch, string, error) {
	const x86LoaderDir = "boot/x86_64/loader"
	if suseLoaderDir != x86LoaderDir {
		return "", "", fmt.Errorf("%w：suseLoaderDir 已不再是 %q（现为 %q），"+
			"说明 SUSE 抽取器已支持多架构——本函数「抽取成功即 x86_64」的推断"+
			"随之失效，必须改成从该路径里解析架构",
			ErrArchUndetectable, x86LoaderDir, suseLoaderDir)
	}
	// SUSE 的版本号在 media.1/products 里（形如 "/ openSUSE-Leap-15.6"），
	// 但那一行的格式随产品线变化（SLES 与 Leap 不同），而 OSVersion 是
	// 信息性字段、不参与选集与校验，不值得为它引入一个易碎的解析。留空。
	return model.ArchX86_64, "", nil
}

// archFromDiskInfo 从 Ubuntu 的 .disk/info 里取架构。
//
// **Ubuntu 是唯一必须读元数据的家族**：它的引导制品在 casper/ 下
// （casper/vmlinuz、casper/initrd），路径里不带架构，所以 ExtractUbuntu
// 对架构完全无感——它会同样顺利地抽出一份 arm64 介质的内核。不读元数据就
// 只能猜，而猜错的后果是 precheck 的 arch 闸门反而放行（见
// ErrArchUndetectable 的文档）。
//
// 真实 Ubuntu Server ISO 的 .disk/info 是一行产品描述，架构作为独立词出现：
//
//	Ubuntu-Server 24.04.1 LTS "Noble Numbat" - Release amd64 (20240827)
//
// 拆词规则是「非字母数字下划线即分隔」，逐词过 normalizeArch，命中即用。
// 留着下划线是为了 x86_64 这种写法不被拆散；不假定架构出现在第几个词，
// 也不用子串匹配——按词匹配能避免把 "ubuntu-server-amd64" 这种连写形态
// 之外的巧合当成判据。
func archFromDiskInfo(root fs.FS) (model.Arch, string, error) {
	const diskInfoPath = ".disk/info"

	data, err := fs.ReadFile(root, diskInfoPath)
	if err != nil {
		return "", "", fmt.Errorf("%w：读取 %s 失败（Ubuntu 的 casper/ 路径不带架构，"+
			"架构只能从这份元数据取）: %v", ErrArchUndetectable, diskInfoPath, err)
	}

	text := string(data)
	for _, word := range strings.FieldsFunc(text, func(r rune) bool {
		return !(r >= 'a' && r <= 'z') && !(r >= 'A' && r <= 'Z') &&
			!(r >= '0' && r <= '9') && r != '_'
	}) {
		if arch, archErr := normalizeArch(word); archErr == nil {
			return arch, ubuntuVersionFromDiskInfo(text), nil
		}
	}

	return "", "", fmt.Errorf("%w：%s 里没有可识别的架构词（内容: %q）"+
		"——真实 Ubuntu Server 介质这一行形如 "+
		`"Ubuntu-Server 24.04.1 LTS \"Noble Numbat\" - Release amd64 (20240827)"`,
		ErrArchUndetectable, diskInfoPath, strings.TrimSpace(text))
}

// ubuntuVersionFromDiskInfo 从 .disk/info 里取版本号（形如 24.04.1）。
//
// 取不到就返回空串，不报错：OSVersion 是信息性字段，不参与选集与校验
// （contract 的选集是家族/架构/固件/通道四元组），为它让整次导入失败不成
// 比例。架构取不到才是硬失败，因为那是 precheck 的比对对象。
func ubuntuVersionFromDiskInfo(text string) string {
	for _, word := range strings.Fields(text) {
		trimmed := strings.Trim(word, `"()`)
		if trimmed == "" || !(trimmed[0] >= '0' && trimmed[0] <= '9') {
			continue
		}
		if strings.Contains(trimmed, ".") {
			return trimmed
		}
	}
	return ""
}

// archFromTreeinfo 从 .treeinfo 的 [general] 段取 arch 与 version。
//
// 实测真实 OpenCloudOS 9.6 minimal ISO 的 .treeinfo 有这两个键：
//
//	[general]
//	arch = x86_64
//	version = 9.6
//
// 复用 parseINI 与 parseTreeinfo 同一套解析（manifest.go）——那边已经为了
// 取仓库子目录读过 [general] 段，本函数不引入第二种 .treeinfo 解析方式。
func archFromTreeinfo(root fs.FS) (model.Arch, string, error) {
	data, err := fs.ReadFile(root, ".treeinfo")
	if err != nil {
		return "", "", fmt.Errorf("imports: 读取 .treeinfo 失败（判定架构需要它）: %w", err)
	}

	general := parseINI(string(data))["general"]
	rawArch := strings.TrimSpace(general["arch"])
	if rawArch == "" {
		return "", "", fmt.Errorf("%w：.treeinfo 的 [general] 段没有 arch 键", ErrArchUndetectable)
	}

	arch, err := normalizeArch(rawArch)
	if err != nil {
		return "", "", err
	}
	return arch, strings.TrimSpace(general["version"]), nil
}

// archFromAPTBinaryDir 从 dists/<suite>/<component>/binary-<arch>/ 的目录名
// 取架构。
//
// 为什么不读 Release 文件的 `Architectures:` 字段：真实 Debian 的 Release
// 确实有那一行，但 ExtractDebian 与它的测试夹具都只要求 dists/ 存在、不要求
// Release 的内容形态（extract_debian_test.go 的夹具里 Release 只有一行
// `Codename: trixie`）。用目录名做判据，判定前提与抽取前提一致——
// 「dists/ 树能装机」蕴含「binary-<arch>/ 目录存在」，因为那正是 apt 取
// 包索引的位置。
//
// 同理不用 pool/ 下的 .deb 文件名：那些名字里的架构后缀可能是 all
// （架构无关包），单个文件不足为判据。
func archFromAPTBinaryDir(root fs.FS) (model.Arch, string, error) {
	const binaryPrefix = "binary-"

	var found string
	walkErr := fs.WalkDir(root, "dists", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		name := path.Base(p)
		if !strings.HasPrefix(name, binaryPrefix) {
			return nil
		}
		// binary-all 是架构无关包的目录，与 binary-<真实架构> 并存，
		// 不能拿它当判据。
		suffix := strings.TrimPrefix(name, binaryPrefix)
		if suffix == "all" || suffix == "" {
			return nil
		}
		found = suffix
		return fs.SkipAll
	})
	if walkErr != nil {
		return "", "", fmt.Errorf("imports: 遍历 dists/ 失败（判定架构需要它）: %w", walkErr)
	}
	if found == "" {
		return "", "", fmt.Errorf("%w：dists/ 下找不到 binary-<架构>/ 目录"+
			"（binary-all 不算，那是架构无关包）", ErrArchUndetectable)
	}

	arch, err := normalizeArch(found)
	if err != nil {
		return "", "", err
	}
	// Debian 的版本号不在仓库树里（Release 的 Version 字段只有部分发行版
	// 填，且 netinst 介质常常留空），留空——OSVersion 是信息性字段，不参与
	// 选集与校验，留空不影响装机。
	return arch, "", nil
}

// normalizeArch 把各家族对同一架构的不同叫法归一到 model.Arch 的两个取值。
//
// 三组别名都真实存在：RPM 系用 aarch64、APT 系用 amd64/arm64、
// .treeinfo 与 uname 用 x86_64。认不出的值一律报错，不兜底——理由见
// ErrArchUndetectable 的文档（兜底成 x86_64 会让 precheck 的 arch 闸门
// 反而放行一份架构不符的制品）。
func normalizeArch(raw string) (model.Arch, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "x86_64", "amd64", "x64":
		return model.ArchX86_64, nil
	case "arm64", "aarch64":
		return model.ArchARM64, nil
	default:
		return "", fmt.Errorf("%w：认不出架构 %q，本系统只支持 x86_64 与 arm64"+
			"（若确需支持它，往 normalizeArch 加一支，并确认 iPXE 的 "+
			"${buildarch} 对该架构报什么值——两侧必须能对上）", ErrArchUndetectable, raw)
	}
}
