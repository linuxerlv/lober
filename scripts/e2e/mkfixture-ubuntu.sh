#!/usr/bin/env bash
# mkfixture-ubuntu.sh 造一个 Ubuntu 制品集的假目录树，供端到端脚本
# （scripts/e2e-ubuntu.sh）使用。参照 scripts/e2e/mkfixture.sh（RHEL）的
# 结构与设计动机，按 Ubuntu 家族调整。
#
# 不用真 ISO：几百 MB 的下载对一个每次提交都要跑的脚本不可接受。
#
# 关键选择：本脚本产出的是「ExtractUbuntu 抽取完成之后」的目录形状，不是
# 「ISO 内容本身」的目录形状——这两者对 Ubuntu 而言不是一回事，与 RHEL不
# 同（RHEL 的 ExtractRHEL 基本原样保留 ISO 的目录树，所以 e2e.sh 的
# mkfixture.sh 造一份 ISO 形状的树可以直接当最终制品使用）。
# internal/imports/extract_ubuntu.go 的 ExtractUbuntu 函数：
#   - 源（ISO 内容）：casper/vmlinuz、casper/initrd、casper/filesystem.squashfs
#   - 目标（抽取产物，destDir 下）：vmlinuz、initrd、casper/filesystem.squashfs
#     （内核与 initrd 被搬到 destDir 根，只有 squashfs 保留 casper/ 前缀）
# 本轮端到端脚本走的是「制品集登记→sqlite3 绕过导入 job 直接写 ready」的
# 路径（见 e2e-ubuntu.sh 顶部注释对这处契约缺口的说明），从未真正调用过
# ExtractUbuntu——因此这里没有必要造 ISO 形状再让一个不存在的调用点去转换，
# 直接造「假装已经被抽取过」的最终形状更贴近脚本实际验证的内容：网关按
# paths_json 里的相对路径去磁盘上找文件，这些路径必须与本脚本产出的真实
# 文件位置一一对应。paths_json 由 e2e-ubuntu.sh 按下面这份形状手写（与
# ExtractUbuntu 真实产出的 model.ArtifactPaths 字段值逐一对应，见该函数
# 尾部的返回值：Kernel="vmlinuz"、Initrd=["initrd"]、
# Extra["squashfs"]="casper/filesystem.squashfs"）。
#
# 若未来 t01 的产物真正接上 RunUbuntu 调用点，这份 fixture 需要换成 ISO
# 形状（casper/vmlinuz 等），走真实的 Open→Identify→ExtractUbuntu 路径——
# 到那时 e2e-ubuntu.sh 顶部注释里「已知限制」第 2 条自然失效，需要连带
# 更新本文件与调用方式。
#
# .disk/info 特征文件（Identify() 用来判定 OS 家族，internal/imports/
# identify.go）在本脚本产出的形状里没有意义（因为不会真的调用 Identify），
# 但仍然造出来，保持与"这是一个 Ubuntu 制品集"这一事实自文档化，也不给
# 将来切回 ISO 形状的人留坑——那时 .disk/info 就会真正被用到。
#
# 用法：mkfixture-ubuntu.sh <制品根目录> <制品集 ID>
# 产出：<制品根目录>/<制品集 ID>/{.disk/info,vmlinuz,initrd,casper/filesystem.squashfs}
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <制品根目录> <制品集 ID>" >&2
	exit 1
fi

artifact_root="$1"
set_id="$2"
set_dir="${artifact_root}/${set_id}"

mkdir -p "${set_dir}/.disk" "${set_dir}/casper"

printf 'Ubuntu-Server 24.04 LTS "Noble Numbat" - Release amd64 (20260101)\n' >"${set_dir}/.disk/info"

# 抽取产物形状（不是 ISO 形状，见文件头说明）：内核与 initrd 在制品集根
# 目录，squashfs 保留 casper/ 前缀——与 ExtractUbuntu 真实返回的
# model.ArtifactPaths 字段值逐一对应。内容是什么无关紧要，
# internal/statemachine/precheck.go 的 checkArtifactFiles 只 stat 文件
# 是否存在，不解析内容。
printf 'fake ubuntu kernel for e2e\n' >"${set_dir}/vmlinuz"
printf 'fake ubuntu initrd for e2e\n' >"${set_dir}/initrd"
printf 'fake ubuntu squashfs for e2e\n' >"${set_dir}/casper/filesystem.squashfs"

echo "已在 ${set_dir} 造出假 Ubuntu 制品树（抽取产物形状）" >&2
