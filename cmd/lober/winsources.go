// 本文件是 imports.WindowsSourcesResolver 的生产实现，供 wire.go 注入
// imports.Job.ConfigureWindows 的第一个参数。对应本轮 m2 轨任务：解除
// Windows 导入路径的 MVP 封锁，依据 SPEC §0.3、§2.1、附录 A.4-A.6，
// internal/imports/job.go 与 internal/imports/extract_windows.go 的文件
// 末尾契约缺口说明。
//
// # 设计决策：按目录约定找文件，且按"通用 vs 专属"拆成两层
//
// wimboot/BCD/boot.sdi/boot.wim 四个文件不在 ISO 里（extract_windows.go
// 的 WindowsExternalSources 文档），是运维单独准备、放在某处的本地文件。
// 三个方向的取舍：
//
//  1. 【选中】按目录约定，配置给一个基目录，四个文件按固定名字放在
//     基目录下的固定位置——简单，运维好理解，且与 SPEC §2.1「气隙环境下
//     ISO 以登记服务器本地路径为主……运维 scp 到指定目录再登记更实际」
//     是同一种运维模式：这四个文件同理，运维会 scp 上去，不会指望走
//     WebUI 上传（几百 MB 到几 GB 走浏览器体验很差，与 ISO 是同一个
//     判据）。
//
//  2. 配置里显式列四个路径（例如四个 YAML 字段）：最直白，但每新增一个
//     Windows 版本或每多一个固件（同一版本按固件导两个 artifact_set，
//     §2.1）就要在配置文件里手写一遍四个绝对路径，运维负担随制品集数量
//     线性增长——一个气隙环境可能同时维护 Windows Server 2019/2022 各
//     两个固件、外加几个 Windows 10/11 版本，很快就会变成配置文件里一长串
//     容易复制粘贴出错的路径列表。目录约定只需要运维把文件放对位置，
//     不需要跟着每个制品集编辑配置文件、不需要重启进程。
//
//  3. 从数据库读：需要改 internal/core/contract 与 model 的 schema，本
//     任务铁律明确禁止改动 internal/imports/** 之外由其它任务拥有的模块，
//     且这本质是"把方案 2 的路径列表存进数据库而不是 YAML"，运维负担的
//     根源（每个制品集单独登记四个路径）并未改变，只是换了存储位置。
//
//     选方案 1，但进一步拆成两层，而不是四个文件全部按
//     <base>/<artifact_set_id>/ 存放：
//
//     <base>/wimboot                       —— 共享，不随 artifact_set_id 变化
//     <base>/<artifact_set_id>/BCD         —— 制品集专属
//     <base>/<artifact_set_id>/boot.sdi    —— 制品集专属
//     <base>/<artifact_set_id>/boot.wim    —— 制品集专属
//
// 依据（任务明确要求想清楚的区别）：wimboot 是 iPXE 项目提供的通用引导器
// （§4.5 菜单的 kernel 行），只负责把 WinPE 拉起来，与具体 Windows 版本、
// 与固件位宽都无关——同一份 wimboot 二进制能引导任意版本、任意固件的
// WinPE。而 BCD/boot.sdi/boot.wim 三者来自某一次具体的 WinPE 构建
// （附录 A.4-A.6：boot.wim 是实际被引导的 WinPE 镜像，BCD 是它的引导
// 配置，boot.sdi 是引导镜像描述文件），且 boot.wim 的位宽必须与固件匹配
// （SPEC §2.1：同一 Windows 版本要按固件导两个 artifact_set）——这三者
// 天然是"每个 artifact_set 各自一份"，不能跨 artifact_set 共享，共享会
// 导致 uefi 制品集意外引用了 bios 的 boot.wim（或反过来）。
//
// 若不做这个区分、把 wimboot 也塞进每个 <artifact_set_id>/ 子目录，代价
// 是：运维每准备一个新的 artifact_set（哪怕只是同一个 WinPE 换个固件）
// 都要重新复制一份完全相同的 wimboot 文件——不仅浪费磁盘（wimboot 虽小
// 但会有几十上百份完全相同的副本），更重要的是给人一种"这个文件也是
// 制品集专属的"错误印象，未来有人想升级 iPXE 版本的 wimboot 时会不清楚
// 是否需要给每个 artifact_set 都替换一遍。拆成两层后，升级 wimboot 只需
// 替换 <base>/wimboot 这一份文件，全部 artifact_set 立即生效；而 BCD/
// boot.sdi/boot.wim 三者的"制品集专属"语义也在目录结构上被直接体现，
// 不需要额外文档才能看懂。
//
// # 判据复核：运维在气隙环境下真能照着做吗
//
// 能。运维准备一个新 Windows 版本（或给已有版本新增一个固件）时的操作
// 序列：
//  1. 若 <base>/wimboot 已存在（多数情况——同一套 iPXE 部署长期不变），
//     跳过这一步；否则从 iPXE 项目下载一次，scp 到 <base>/wimboot。
//  2. 在服务器上跑 mkdir -p <base>/<artifact_set_id>/。
//  3. 用 DISM 或类似工具在本机或跳板机构建好 boot.wim（含 unattend.xml
//     的封装逻辑，见 extract_windows.go 文件头："真正的安装动作由 WinPE
//     内的 DISM 或 setup.exe 执行"这条设计前提，本文件不涉及），连同
//     对应的 BCD、boot.sdi 一并 scp 到 <base>/<artifact_set_id>/。
//  4. 控制台登记 artifact_set 时填的 ID 与这个子目录名一致（运维自己
//     选定、自己保证一致——与"文件名与制品集 ID 对应"这条约定在
//     ESXi/RHEL 等家族里已经是运维熟悉的操作模式，见 deploy/README.md
//     的对应章节）。
//
// 全程只涉及 scp 与 mkdir，不需要编辑 YAML、不需要重启进程，与 SPEC §2.1
// 对气隙环境的判断（"以登记服务器本地路径为主……运维 scp 到指定目录再
// 登记更实际"）完全一致。
package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lober/lober/internal/imports"
)

// fileWindowsSourcesResolver 是 imports.WindowsSourcesResolver 的生产
// 实现：按上方文件头注释的目录约定，从本地文件系统按 artifact_set_id
// 拼出四个文件各自的路径。
type fileWindowsSourcesResolver struct {
	// base 是 config.Store.WindowsSourcesPath（cfg.Store.WindowsSourcesPath），
	// 由 wire.go 在装配时传入。可能是空字符串（对应「未配置」，见
	// config.go 的 Store.WindowsSourcesPath 字段文档）——ResolveWindowsSources
	// 在这种情况下明确报错，不拼出误导性的相对路径。
	base string
}

// newFileWindowsSourcesResolver 构造一个 fileWindowsSourcesResolver。
func newFileWindowsSourcesResolver(base string) *fileWindowsSourcesResolver {
	return &fileWindowsSourcesResolver{base: base}
}

// windowsSourceFile 是本解析器要定位的一个文件的描述：期望路径怎么拼、
// 缺失时错误信息里用什么名字指认——与 extract_windows.go 的
// windowsArtifactFile 表是同一种"把重复的四段逻辑收进一张表"的做法，
// 但这里多了 shared 字段区分两层目录约定（见文件头注释）。
type windowsSourceFile struct {
	name   string // 缺失时错误信息里的可读名字，也是磁盘上的文件名
	shared bool   // true 表示落在 <base>/ 下（wimboot），false 表示落在 <base>/<id>/ 下
}

var windowsSourceFiles = []windowsSourceFile{
	{name: "wimboot", shared: true},
	{name: "BCD", shared: false},
	{name: "boot.sdi", shared: false},
	{name: "boot.wim", shared: false},
}

// ResolveWindowsSources 实现 imports.WindowsSourcesResolver。
func (r *fileWindowsSourcesResolver) ResolveWindowsSources(_ context.Context, artifactSetID string) (imports.WindowsExternalSources, error) {
	if r.base == "" {
		return imports.WindowsExternalSources{}, fmt.Errorf(
			"cmd/lober: Windows 外部文件基目录未配置（store.windows_sources_path 为空）" +
				"——无法为任何 artifact_set 解析 wimboot/BCD/boot.sdi/boot.wim 的路径，" +
				"运维需要在配置文件里填写该字段并把这些文件 scp 到对应位置")
	}
	if artifactSetID == "" {
		return imports.WindowsExternalSources{}, fmt.Errorf(
			"cmd/lober: artifactSetID 为空字符串，无法拼出制品集专属子目录路径")
	}

	resolved := make(map[string]string, len(windowsSourceFiles))
	for _, f := range windowsSourceFiles {
		var path string
		if f.shared {
			path = filepath.Join(r.base, f.name)
		} else {
			path = filepath.Join(r.base, artifactSetID, f.name)
		}
		if _, err := os.Stat(path); err != nil {
			return imports.WindowsExternalSources{}, fmt.Errorf(
				"cmd/lober: Windows 外部文件 %s 不存在或不可读（路径 %q，制品集 %q）: %w"+
					"——它不在 ISO 里，需要运维单独准备并放到约定位置"+
					"（wimboot 共享放在基目录下，其余三个文件放在基目录下以制品集 ID"+
					"命名的子目录里，见 winsources.go 文件头的目录约定），"+
					"不能假设一组默认路径继续导入", f.name, path, artifactSetID, err)
		}
		resolved[f.name] = path
	}

	return imports.WindowsExternalSources{
		WimbootPath: resolved["wimboot"],
		BCDPath:     resolved["BCD"],
		BootSDIPath: resolved["boot.sdi"],
		BootWIMPath: resolved["boot.wim"],
	}, nil
}

// 编译期断言：fileWindowsSourcesResolver 满足 imports.WindowsSourcesResolver。
var _ imports.WindowsSourcesResolver = (*fileWindowsSourcesResolver)(nil)
