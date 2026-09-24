// 本文件实现 contract.Prechecker：§4.6 两层引导一致性校验的三项硬拒绝。
// 对应实现计划任务 3，依据 SPEC §0.3、§4.6。
//
// DHCP 按固件分流、网关按任务分流，两层各自正确不等于组合正确——建任务时
// 选的 OS 与眼前机器的实际固件可能不匹配，而 DHCP 对此毫无判断能力
// （它看不到任务）。校验只能在这里做。
package statemachine

import (
	"context"
	"fmt"
	"io/fs"
	"path"
	"strings"

	"github.com/lober/lober/internal/core/contract"
	"github.com/lober/lober/internal/core/model"
)

// Check 校验机器实际上报的 arch/firmware 与制品集是否匹配，并校验
// artifact.Paths 声明的文件是否都能在 files 中找到。三项按顺序校验：
// arch 一致性 → 固件模式兼容 → 制品完整性。
func (m *Machine) Check(ctx context.Context, actualArch model.Arch, actualFirmware model.FirmwareMode, artifact model.ArtifactSet, files fs.FS) error {
	if err := checkArch(actualArch, artifact); err != nil {
		return err
	}
	if err := checkFirmware(actualFirmware, artifact); err != nil {
		return err
	}
	if err := checkArtifactFiles(artifact, files); err != nil {
		return err
	}
	return nil
}

// checkArch 校验机器上报的架构与制品集架构必须相符。不符则拒绝——
// x86_64 制品发给 arm64 机器，内核直接加载失败。
func checkArch(actualArch model.Arch, artifact model.ArtifactSet) error {
	if actualArch == artifact.Arch {
		return nil
	}
	return &contract.PrecheckError{
		Failure: contract.PrecheckFailureArch,
		Detail: fmt.Sprintf("机器上报架构 %q，任务制品集要求架构 %q，两者不符",
			actualArch, artifact.Arch),
	}
}

// checkFirmware 用 M1 的固件兼容矩阵判 OS 家族能否在该固件下装，再用
// 制品集的固件匹配方法判这套制品能否发给该机器。典型拒绝是「这台机器是
// BIOS，任务要装 ESXi」。
func checkFirmware(actualFirmware model.FirmwareMode, artifact model.ArtifactSet) error {
	if !model.FirmwareCompatible(artifact.OSFamily, actualFirmware) {
		return &contract.PrecheckError{
			Failure: contract.PrecheckFailureFirmware,
			Detail: fmt.Sprintf("这台机器是 %s，任务要装 %s，该 OS 家族不支持这种固件模式",
				actualFirmware, artifact.OSFamily),
		}
	}
	if !artifact.MatchesFirmware(actualFirmware) {
		return &contract.PrecheckError{
			Failure: contract.PrecheckFailureFirmware,
			Detail: fmt.Sprintf("机器上报固件模式 %q，任务所选制品集固件为 %q，两者不符",
				actualFirmware, artifact.Firmware),
		}
	}
	return nil
}

// checkArtifactFiles 校验路径中所有文件在盘上真实存在。文件存在性检查
// 通过 fs.FS 注入，不直接调 os.Stat——测试用内存文件系统，不需要真造
// 文件，也不会因为开发机上恰好有同名文件而假通过。
func checkArtifactFiles(artifact model.ArtifactSet, files fs.FS) error {
	paths := artifactCheckPaths(artifact.Paths)
	for _, p := range paths {
		if p == "" {
			continue
		}
		if err := checkPathExists(files, p, artifact.OSFamily); err != nil {
			return &contract.PrecheckError{
				Failure: contract.PrecheckFailureArtifact,
				Detail:  fmt.Sprintf("制品文件 %q 在盘上不存在: %v", p, err),
			}
		}
	}
	return nil
}

// artifactCheckPaths 收集 ArtifactPaths 里全部需要存在性检查的路径：
// Kernel、Initrd 各项、Repo、Extra 各项。
//
// 说明（与 checkPathExists 的家族分流配合阅读）：本函数不区分「这一项是
// 文件还是仓库目录」，判定权留给 checkPathExists——它按结尾斜杠区分，
// 斜杠结尾即视为仓库目录。目前只有 Repo 一项带结尾斜杠（三个产出非空
// Repo 的抽取器都拼 repoDestName + "/"）。
func artifactCheckPaths(p model.ArtifactPaths) []string {
	out := make([]string, 0, 2+len(p.Initrd)+len(p.Extra))
	out = append(out, p.Kernel)
	out = append(out, p.Initrd...)
	out = append(out, p.Repo)
	for _, v := range p.Extra {
		out = append(out, v)
	}
	return out
}

// checkPathExists 判定给定路径在 files 中是否存在。路径以斜杠结尾则视为
// 仓库目录，按 family 查 model.RepoIndexPattern 校验其索引入口；否则直接
// 检查该文件本身。
//
// 判据按家族分流的理由与那张表本身都在 model.RepoIndexPattern（它是 OS
// 家族的固有属性，且启动自检 cmd/lober/preflight.go 与 verify 端点
// cmd/lober/verifier.go 消费同一条规则——此前三处各存一份常量副本，一处
// 修好另两处照旧，正是那个 Debian bug 能长期存在的原因）。
//
// io/fs 的路径不接受前导或结尾斜杠（fs.ValidPath 的要求），因此这里在
// 判断「是不是目录」之后统一裁剪结尾斜杠再拼接，不能把结尾斜杠原样传给
// fs.Stat / fs.Glob。
//
// 表里查不到该家族时是**硬拒绝**，不退化成「目录存在就算过」：制品完整性
// 属于 §4.6 的安全闸门，「无法确认仓库可用」与「确认仓库不可用」在这一层
// 应当同样处置（§9.1 默认拒绝）。退化放行会把这个 bug 换个形式留下来——
// 将来新增一个家族却忘了配判据时，它会静默放行一份索引不可用的仓库，症状
// 又变成「装到选包阶段才失败」。
func checkPathExists(files fs.FS, p string, family model.OSFamily) error {
	if !strings.HasSuffix(p, "/") {
		_, err := fs.Stat(files, p)
		return err
	}

	dir := strings.TrimSuffix(p, "/")
	pattern, ok := model.RepoIndexPattern(family)
	if !ok {
		return fmt.Errorf("不知道 %q 家族的仓库索引入口是哪个文件，无法确认这份仓库对 installer 可用"+
			"（需要在 model.RepoIndexPattern 里为该家族补一条判据）", family)
	}

	// fs.Glob 对不含通配符的 pattern 退化成一次 fs.Stat（返回空匹配而非
	// 错误），因此 RPM-MD 那条固定路径与 APT 那条 glob 可以走同一段代码，
	// 不需要按「是否含通配符」分两个分支。
	matches, err := fs.Glob(files, path.Join(dir, pattern))
	if err != nil {
		return fmt.Errorf("匹配仓库索引入口 %q 失败: %w", pattern, err)
	}
	if len(matches) == 0 {
		return fmt.Errorf("仓库目录存在，但其下找不到 %q（%s 家族的软件包索引入口）"+
			"——目录本身能 stat 通过，对 installer 却毫无用处", pattern, family)
	}
	return nil
}
