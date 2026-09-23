#!/usr/bin/env bash
# mkfixture-windows.sh 造一份能通过 checkArtifactFiles（precheck 制品完整性
# 校验）的假 Windows 制品目录树，供 scripts/e2e-windows.sh 使用。
#
# 与 scripts/e2e/mkfixture.sh（RHEL）的关键差异（不是照抄改名，是家族机制
# 本身不同，见 internal/render/winpe.go 文件头注释）：
#
#  1. RHEL 的四个文件走 model.ArtifactPaths.Kernel/Initrd/Repo 三个专属
#     字段；Windows 的四个文件（wimboot/BCD/boot.sdi/boot.wim）全部走
#     ArtifactPaths.Extra 这个 map（internal/imports/extract_windows.go 的
#     windowsArtifactFiles 表，internal/render/winpe.go 的
#     windowsExtraKeys）——本脚本落盘的文件名与 Extra 里将要写的键名必须
#     一一对应，否则 precheck 会在"制品文件在盘上不存在"这一步就拒绝。
#  2. 不产出 .treeinfo/repodata 之类的 RHEL 识别特征文件——本脚本产出的树
#     不经过 internal/imports.Identify（导入路径本身就被 mvpUnsupportedFamilies
#     挡住，见 scripts/e2e-windows.sh 头部注释），不需要满足识别特征。
#  3. 一次调用只产出一份"单个固件"的制品树（对应 ExtractWindows 一次调用
#     只处理一个 destDir 的语义）。同一 Windows 版本要按固件各造一份，
#     调用方（e2e-windows.sh）分别传两个不同的目录调用本脚本两次。
#
# 用法：mkfixture-windows.sh <制品根目录> <制品集 ID>
# 产出：<制品根目录>/<制品集 ID>/{wimboot,BCD,boot.sdi,boot.wim}
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <制品根目录> <制品集 ID>" >&2
	exit 1
fi

artifact_root="$1"
set_id="$2"
set_dir="${artifact_root}/${set_id}"

mkdir -p "${set_dir}"

# 内容是什么无关紧要：precheck 只关心文件存在性（internal/statemachine/
# precheck.go 的 checkPathExists 只调 fs.Stat，不读内容），真实的 WIM
# 格式解析属于 WimArchDetector 的职责，不在 MVP 端到端验证范围内（本脚本
# 走 sqlite3 绕过导入 job，见 e2e-windows.sh 的已知限制说明）。
printf 'fake wimboot for e2e\n' >"${set_dir}/wimboot"
printf 'fake BCD for e2e\n' >"${set_dir}/BCD"
printf 'fake boot.sdi for e2e\n' >"${set_dir}/boot.sdi"
printf 'fake boot.wim for e2e\n' >"${set_dir}/boot.wim"

echo "已在 ${set_dir} 造出假 Windows 制品树（wimboot/BCD/boot.sdi/boot.wim）" >&2
