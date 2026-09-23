#!/usr/bin/env bash
# mkfixture-esxi.sh 造一个 ESXi 家族的假制品目录树，供端到端脚本
# （scripts/e2e-esxi.sh）使用。与 scripts/e2e/mkfixture.sh（RHEL 家族）同一
# 定位，只是特征文件与目录结构换成 ESXi 的（SPEC §2.1 的识别表：ESXi 用
# boot.cfg + mboot.efi，不是 .treeinfo）。
#
# 不用真 ISO，理由与 mkfixture.sh 相同：几 GB 的下载对一个每次提交都要跑的
# 脚本不可接受，本脚本产出的树只关心 internal/imports/identify.go 与
# internal/imports/extract_esxi.go 关心的特征文件与目录结构。
#
# boot.cfg 的内容不是随意占位符，而是与 internal/render/esxi_test.go 的
# rawBootCfgTemplate 同构的真实 ESXi boot.cfg 语法（prefix=/kernelopt=/
# modules= 三行俱全，modules= 后面列出的每个模块文件本脚本都会真的造出来）
# ——这样即使本轮分派未接线（见 e2e-esxi.sh 的发现记录），这份 fixture 本身
# 仍是一份「若分派接好，RenderESXiBootCfg 能正确处理」的输入，不会因为
# fixture 造得偷懒而在未来验证分派接线时又要重造一遍。
#
# 用法：mkfixture-esxi.sh <制品根目录> <制品集 ID>
# 产出：<制品根目录>/<制品集 ID>/{mboot.efi,boot.cfg,jumpstrt.gz,useropts.gz,k.b00,chain.b00}
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <制品根目录> <制品集 ID>" >&2
	exit 1
fi

artifact_root="$1"
set_id="$2"
set_dir="${artifact_root}/${set_id}"

mkdir -p "${set_dir}"

# mboot.efi：ESXi 的 UEFI 引导器。内容是什么无关紧要——identify.go 的
# Identify 与 extract_esxi.go 的 ExtractESXi 都只关心「文件存在」，不解析
# PE/EFI 镜像本身。
printf 'fake mboot.efi for e2e (esxi uefi bootloader placeholder)\n' >"${set_dir}/mboot.efi"

# modules= 行列出的四个模块文件——extract_esxi.go 的完整性校验会逐个 Stat
# 这些文件（缺一个就整体拒绝导入，见该文件顶部注释），因此这里必须真的
# 造出来，不能只在 boot.cfg 里写文件名。
printf 'fake jumpstart module for e2e\n' >"${set_dir}/jumpstrt.gz"
printf 'fake useropts module for e2e\n' >"${set_dir}/useropts.gz"
printf 'fake kernel module for e2e\n' >"${set_dir}/k.b00"
printf 'fake chain module for e2e\n' >"${set_dir}/chain.b00"

# boot.cfg：ESXi 的引导配置文件，identify.go 的第二个特征文件。语法与
# internal/render/esxi_test.go 的 rawBootCfgTemplate 同构（prefix=<ISO 内
# 相对路径占位值>、kernelopt=<不含 ks= 的原始内核选项>、
# modules=<--- 分隔的相对路径列表>）——三行都是原始未改写版本，改写是
# RenderESXiBootCfg 的职责（本 fixture 只提供"渲染前"的输入）。
cat >"${set_dir}/boot.cfg" <<'EOF'
bootstate=0
title=Loading ESXi installer
timeout=5
prefix=
kernel=/mboot.efi
kernelopt=runweasel ks=cdrom:/KS.CFG
modules=jumpstrt.gz --- useropts.gz --- k.b00 --- chain.b00
build=8.0.0-2000000
updated=0
EOF

echo "已在 ${set_dir} 造出 ESXi 假制品树" >&2
