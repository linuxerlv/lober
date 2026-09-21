// 本文件实现 Windows 制品集的双固件导入（对应实现计划 P3-01 任务 1，依据
// SPEC §0.3、§2.1、§4.5、§4.6，附录 A.4-A.6）。
//
// Windows 与 Linux 是两套机制，不要套用 ExtractRHEL 的思路（见 manifest.go）：
// wimboot 只能引导 WinPE，不能引导完整 Windows（附录 A.6）——因此 Windows
// 的"镜像封装"实质是维护 install.wim 加 unattend.xml，真正的安装动作由
// WinPE 内的 DISM 或 setup.exe 执行。本文件只负责导入期的登记与校验，不
// 构建 WinPE 本身（那是人工前置步骤，见下方 WindowsExternalSources 的
// 注释）。
//
// 固件维度是硬约束（§0.3、§2.1）：boot.wim 的位宽须与固件匹配，因此同一
// Windows 版本需导两个 artifact_set（firmware 取 uefi 或 bios 的具体值，
// 不是 any）。本文件的 ExtractWindows 在落盘前用 WimArchDetector 校验这
// 一点，位宽与声明不符时导入直接失败——不能留到引导时才发现（那时运维已
// 经在机房了）。
package imports

import (
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/core/model"
)

// WindowsExternalSources 承载四类 Windows 制品文件里"不在 ISO 中"的那
// 三个（wimboot、BCD、boot.sdi）加 WinPE 镜像本身（boot.wim）的本地绝对
// 路径（计划任务 1："wimboot 不在 ISO 里，要单独获取并放进制品根目录"；
// "WinPE 镜像的来源...不是 ISO 里现成的...这是一个人工前置步骤"）。
//
// 与 ExtractRHEL(root fs.FS, destDir string) 的签名差异正是两种机制的
// 直接体现：RHEL 的全部素材来自同一个 ISO 视图 root，而 Windows 需要额外
// 一份"运维已经放在某处"的外部文件集合——导入流程只负责登记与校验这四个
// 文件是否齐全、位宽是否与声明的固件一致，不负责构建它们。
//
// 任一字段为空字符串表示该文件缺失（供测试模拟"缺任一则失败"的四种场景，
// 也对应真实运维忘记准备某个文件时的输入形态）。
type WindowsExternalSources struct {
	WimbootPath string // iPXE 侧的引导器，§4.5 菜单的 kernel 行
	BCDPath     string // WinPE 的引导配置数据，§4.5 菜单三个 initrd 行之一
	BootSDIPath string // 引导镜像描述文件，§4.5 菜单三个 initrd 行之一
	BootWIMPath string // 实际被引导的 WinPE 镜像，§4.5 菜单三个 initrd 行之一
}

// WimArchDetector 探测一个 boot.wim 文件实际的固件位宽（计划任务 1："校验
// 方式：读镜像的架构标识，与制品集声明的固件比对"）。
//
// 做成可注入接口而非直接在本文件里解析 WIM 头部的理由，与 Extractor 接口
// （见 extract.go）完全一致：真实的 WIM 格式解析（复合文件二进制结构或新版
// XML 索引）复杂且与运行环境相关，本模块交付的是"导入流程需要在哪个环节
// 校验什么"这个骨架，具体的字节级解析留给部署环境适配——测试用固定返回值
// 的假实现验证骨架本身的分支逻辑（成功/失败/两种固件各自命中），不依赖
// 真实 WIM 文件。见本文件末尾的契约缺口说明。
type WimArchDetector interface {
	// DetectFirmware 探测 bootWIMPath 对应的 boot.wim 实际支持的固件模式。
	// 返回的 FirmwareMode 必须是 FirmwareUEFI 或 FirmwareBIOS 之一；探测
	// 失败（文件损坏、格式不认识等）返回 error，调用方必须整体拒绝导入，
	// 不能假设一个默认固件继续。
	DetectFirmware(bootWIMPath string) (model.FirmwareMode, error)
}

// windowsArtifactFiles 是本文件要落盘的四个文件在 ArtifactPaths.Extra 里
// 的键名（§2.1："windows: wimboot / BCD / boot.sdi / boot.wim"）与它们各自
// 的来源判定（从 WindowsExternalSources 的哪个字段读）。集中在一张表里，
// 而不是四段几乎相同的 if 判断——新增或核对某个键时只需看这一处。
type windowsArtifactFile struct {
	key      string // ArtifactPaths.Extra 的键，同时也是落盘文件名
	path     func(WindowsExternalSources) string
	required string // 缺失时错误信息里用来指认"缺的是哪个文件"的可读名字
}

var windowsArtifactFiles = []windowsArtifactFile{
	{key: "wimboot", path: func(s WindowsExternalSources) string { return s.WimbootPath }, required: "wimboot"},
	{key: "BCD", path: func(s WindowsExternalSources) string { return s.BCDPath }, required: "BCD"},
	{key: "boot.sdi", path: func(s WindowsExternalSources) string { return s.BootSDIPath }, required: "boot.sdi"},
	{key: "boot.wim", path: func(s WindowsExternalSources) string { return s.BootWIMPath }, required: "boot.wim"},
}

// installWIMPath 是 Windows 安装源（root fs.FS）内识别特征文件的相对路径
// （§2.1："Windows | sources/install.wim"），与 identify.go 中 Identify 函数
// 使用的路径字符串完全一致——两处都指向同一份契约，任何一处改动都必须同步
// 改另一处。
const installWIMPath = "sources/install.wim"

// ExtractWindows 执行 Windows 制品集的导入校验与落盘（计划任务 1）：
//  1. 确认 root（ISO 内容视图）中存在识别特征文件 sources/install.wim——
//     这是"该 ISO 确实是 Windows 安装源"的最终确认，不仅是 Identify 阶段
//     的判定依据，也是本函数自己再核实一遍的前提（防止调用方传入了识别
//     结果与实际内容不一致的 root）
//  2. 确认 declaredFirmware 是 uefi 或 bios 的具体值，拒绝 any——§2.1 明确
//     Windows 的固件维度取具体值，any 是 Linux 四家专属语义
//  3. 确认 ext 四个字段（wimboot / BCD / boot.sdi / boot.wim）全部非空且
//     文件存在——缺任一都要在导入时报错，不留到引导时才 404
//  4. 用 detector 探测 boot.wim 的实际固件位宽，与 declaredFirmware 比对，
//     不符则拒绝导入（§4.6 校验的导入期前哨）
//  5. 把四个文件复制到 destDir 下，返回 model.ArtifactPaths（Extra 字段
//     承载这四个键）
//
// destDir 是这一次导入（这一个固件）的制品集根目录——同一 Windows 版本
// 导两个固件时，调用方各传一个独立的 destDir，两次调用之间没有任何共享
// 状态，天然满足"互不冲突"（计划任务 1 测试要求最后一条）。
func ExtractWindows(
	root fs.FS,
	ext WindowsExternalSources,
	destDir string,
	declaredFirmware model.FirmwareMode,
	detector WimArchDetector,
) (model.ArtifactPaths, error) {
	if _, err := fs.Stat(root, installWIMPath); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf(
			"imports: ISO 内容中找不到 %s（Windows 家族的识别特征文件，SPEC §2.1）: %w",
			installWIMPath, err)
	}

	if declaredFirmware != model.FirmwareUEFI && declaredFirmware != model.FirmwareBIOS {
		return model.ArtifactPaths{}, fmt.Errorf(
			"imports: Windows 制品集声明的固件必须是 uefi 或 bios 的具体值，got %q"+
				"——any 是 Linux 四家「一套制品通吃」专属语义，Windows 需按固件各导一个"+
				"制品集（SPEC §2.1、§0.3）", declaredFirmware)
	}

	if detector == nil {
		return model.ArtifactPaths{}, fmt.Errorf(
			"imports: ExtractWindows 的 detector 参数为 nil——调用方必须注入一个 WimArchDetector" +
				"实现，否则位宽校验这道导入期前哨完全跑不起来（计划任务 1、SPEC §4.6），" +
				"这里明确报错而不是让后面的 detector.DetectFirmware 调用直接 panic")
	}

	for _, f := range windowsArtifactFiles {
		p := f.path(ext)
		if p == "" {
			return model.ArtifactPaths{}, fmt.Errorf(
				"imports: Windows 制品集缺少 %s——它不在 ISO 里，需单独获取并放进制品根目录"+
					"（附录 A.4）；若在导入时不明确报错，故障现场会推迟到引导阶段的一次"+
					"含糊的 404，那时运维已经在机房了", f.required)
		}
		if _, err := os.Stat(p); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf(
				"imports: Windows 制品集的 %s 文件不存在或不可读（路径 %q）: %w", f.required, p, err)
		}
	}

	actualFirmware, err := detector.DetectFirmware(ext.BootWIMPath)
	if err != nil {
		return model.ArtifactPaths{}, fmt.Errorf(
			"imports: 探测 boot.wim 的固件位宽失败: %w——不能假设一个默认固件继续导入", err)
	}
	if actualFirmware != declaredFirmware {
		return model.ArtifactPaths{}, fmt.Errorf(
			"imports: boot.wim 实际探测到的固件位宽是 %q，但制品集声明的是 %q"+
				"——两者不一致会导致声明 uefi 却引导 32 位 WinPE 之类的失败，且失败只会"+
				"在引导时才暴露，必须在导入时拦住（计划任务 1、SPEC §4.6）",
			actualFirmware, declaredFirmware)
	}

	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return model.ArtifactPaths{}, fmt.Errorf("imports: 创建制品目录 %q 失败: %w", destDir, err)
	}

	extra := make(map[string]string, len(windowsArtifactFiles))
	for _, f := range windowsArtifactFiles {
		src := f.path(ext)
		destPath := filepath.Join(destDir, f.key)
		if err := copyLocalFile(src, destPath); err != nil {
			return model.ArtifactPaths{}, fmt.Errorf("imports: 复制 %s 失败: %w", f.required, err)
		}
		extra[f.key] = f.key
	}

	return model.ArtifactPaths{Extra: extra}, nil
}

// copyLocalFile 把本地文件 src 复制到本地路径 dest（与 manifest.go 的
// copyFileFromFS 相对：那个函数的源是 fs.FS 视图内的路径，这里的源是
// WindowsExternalSources 携带的本地绝对路径，两者不能合并成一个函数——
// 一个从只读的 ISO 视图读，一个从本地文件系统读，语义不同）。
func copyLocalFile(src, dest string) error {
	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("打开源文件 %q 失败: %w", src, err)
	}
	defer in.Close()

	out, err := os.Create(dest)
	if err != nil {
		return fmt.Errorf("创建目标文件 %q 失败: %w", dest, err)
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return fmt.Errorf("复制内容到 %q 失败: %w", dest, err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// 契约缺口
// ---------------------------------------------------------------------------
//
// 1. identify.go 与 job.go 未随本文件调整。当前 Identify 对 Windows 特征
//    文件（sources/install.wim）返回 (model.OSFamilyWindows,
//    ErrUnsupportedOSFamily)，job.go 的 RunUnsupported 因此会把 Windows
//    的导入请求直接判为失败，不会走到本文件的 ExtractWindows。
//
//    本计划明确只把 internal/imports/extract_windows.go 与其测试划给
//    本任务（P3-01 的文件归属表），identify.go 与 job.go 不在列——它们是
//    MVP 七模块共享的现有文件，本任务不擅自修改以避免与其它并行任务产生
//    合并冲突。
//
//    正解：需要有人（编排方或后续集成任务）做两处小改动才能让 Windows
//    的导入真正跑起来：
//      a. identify.go 的 mvpUnsupportedFamilies 表去掉
//         model.OSFamilyWindows 这一行（或改为按配置/能力开关判断）；
//      b. job.go 新增一个 RunWindows 方法（结构比照现有 RunRHEL：打开
//         extractor → Identify 确认是 windows → 调用本文件的
//         ExtractWindows → GenerateManifest → 写回 store），且需要一种
//         方式把 WindowsExternalSources 与 declaredFirmware、
//         WimArchDetector 这几个 ExtractRHEL 不需要的额外参数传给 Job
//         （当前 Job 结构体与 RunRHEL 签名都没有对应位置）。
//
//    本文件已经交付的是"给定四个文件与声明固件，如何校验并落盘"这个纯
//    函数骨架，且它是可以独立于 identify.go/job.go 的改动单独测试通过的
//    （见 extract_windows_test.go），符合计划任务 1"产出文件：
//    internal/imports/extract_windows.go、对应测试"的字面范围。
//
// 2. WimArchDetector 没有随本文件提供生产可用的实现（只有测试用的假实现
//    fakeWimArchDetector）。真实的 WIM 格式（复合文件二进制或新版 XML
//    索引）解析需要专门的库或工具（如调用 wimlib 或解析 WIM 头部的
//    Architecture 字段），且不在本模块允许的依赖白名单内
//    （modernc.org/sqlite、go-cmp、uuid、yaml.v3、golang.org/x/crypto、
//    golang.org/x/net 均不含 WIM 解析能力）。真实实现需要在部署适配阶段
//    补上一个满足 WimArchDetector 接口的具体类型，不改变接口本身。
