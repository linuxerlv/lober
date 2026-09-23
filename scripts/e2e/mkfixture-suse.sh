#!/usr/bin/env bash
# mkfixture-suse.sh 造一个能通过 SUSE OS 家族识别与抽取的假制品目录树，
# 供 scripts/e2e-suse.sh 使用。对应 SUSE 端到端验证任务，依据
# internal/imports/identify.go 的识别判据与 internal/imports/extract_suse.go
# 的抽取逻辑（本脚本产出的树结构逐一对照该文件的 suseFakeRoot() 测试
# fixture 与文件头注释核实过的官方路径）。
#
# 不用真 ISO：几百 MB 的下载对一个每次提交都要跑的脚本不可接受，而识别/抽取
# 逻辑关心的只有特征文件与目录结构：
#   - internal/imports/identify.go 的 Identify：exists(root,
#     "repodata/repomd.xml") && exists(root, "media.1/products") 同时成立才
#     判定为 SUSE（单独 repodata/repomd.xml 会与现代 RHEL 撞车，见该文件
#     注释的实证说明），排在 .treeinfo/.disk/info/dists 三个判据之后。
#   - internal/imports/extract_suse.go 的 ExtractSUSE：
#       - boot/x86_64/loader/{linux,initrd}：固定路径的内核与 initrd
#         （SUSE 安装介质的固定引导目录结构，已对 download.opensuse.org
#         的 openSUSE Leap 15.6 实测确认，非猜测）
#       - repodata/repomd.xml：zypper 仓库元数据入口（必需，缺失则拒绝）
#       - media.1/products：SUSE 专属标记（必需，缺失则拒绝）
#       - suse/：仓库树第二部分（可选，缺失时跳过不算错误，见
#         extract_suse.go 对 treeRoot 循环里的 continue 分支）
#
# 本脚本产出的树直接落在制品根目录下的 <artifact_set_id>/ 子目录
# （docs/contracts/filesystem.md 硬约束 1）。
#
# 用法：mkfixture-suse.sh <制品根目录> <制品集 ID>
# 产出：<制品根目录>/<制品集 ID>/{boot/x86_64/loader/{linux,initrd},
#        repodata/repomd.xml,media.1/products,
#        suse/setup/descr/packages,suse/x86_64/bash-5.2-1.rpm}
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <制品根目录> <制品集 ID>" >&2
	exit 1
fi

artifact_root="$1"
set_id="$2"
iso_dir="${artifact_root}/${set_id}"

mkdir -p "${iso_dir}/boot/x86_64/loader"
mkdir -p "${iso_dir}/repodata"
mkdir -p "${iso_dir}/media.1"
mkdir -p "${iso_dir}/suse/setup/descr"
mkdir -p "${iso_dir}/suse/x86_64"

# 内核与 initrd：内容是什么无关紧要，identify/extract 只关心「文件存在」
# 与路径匹配，不解析内核镜像本身。源文件名固定为 linux/initrd
# （suseKernelName/suseInitrdName，与 SPEC §4.5 的 SUSE 菜单示例
# "linux + initrd + autoyast=" 逐字符一致，无需改名）。
printf 'fake kernel for e2e-suse\n' >"${iso_dir}/boot/x86_64/loader/linux"
printf 'fake initrd for e2e-suse\n' >"${iso_dir}/boot/x86_64/loader/initrd"

# repodata/repomd.xml：zypper 仓库元数据入口（ExtractSUSE 的
# suseRepodataPath，必需，缺失即拒绝整个抽取）。内容随意，只要文件存在。
printf '<?xml version="1.0"?><repomd xmlns="http://linux.duke.edu/metadata/repo"></repomd>\n' \
	>"${iso_dir}/repodata/repomd.xml"

# media.1/products：SUSE 专属的多仓库产品索引（ExtractSUSE 的
# suseMultiRepoMarkerPath，必需，缺失即拒绝；同时是 identify.go 用来把
# SUSE 与同样含 repodata/ 的现代 RHEL 区分开的独立佐证）。
printf '/ openSUSE-Leap-15.6\n' >"${iso_dir}/media.1/products"

# suse/ 目录：仓库树第二部分（ExtractSUSE 里可选，缺失时跳过不算错误，
# 但本脚本仍造出来让仓库树看起来完整，与 extract_suse_test.go 的
# suseFakeRoot() 一致）。
cat >"${iso_dir}/suse/setup/descr/packages" <<'EOF'
=Pkg: bash 5.2 1 x86_64
EOF
printf 'fake rpm package for e2e\n' >"${iso_dir}/suse/x86_64/bash-5.2-1.rpm"

echo "已在 ${iso_dir} 造出假 SUSE (openSUSE/SLES) ISO 内容树（供 ExtractSUSE 抽取）" >&2
