#!/usr/bin/env bash
# mkfixture.sh 造一个能通过 OS 家族识别的假制品目录树，供端到端脚本
# （scripts/e2e.sh）使用。对应实现计划任务 9，依据 SPEC §2.1 的识别表。
#
# 不用真 ISO：几 GB 的下载对一个每次提交都要跑的脚本不可接受，而导入逻辑
# 关心的只有特征文件与目录结构（internal/imports/identify.go 的识别表：
# RHEL 系靠 .treeinfo）。本脚本产出的树直接落在制品根目录下的
# <artifact_set_id>/ 子目录（docs/contracts/filesystem.md 硬约束 1），
# 不经过真实的 ISO 挂载/导入 job 流程——见 scripts/e2e.sh 顶部注释里对
# 「制品集登记→导入 job」这条契约缺口的说明。
#
# 用法：mkfixture.sh <制品根目录> <制品集 ID>
# 产出：<制品根目录>/<制品集 ID>/{vmlinuz,initrd.img,.treeinfo,repo/repodata/repomd.xml}
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <制品根目录> <制品集 ID>" >&2
	exit 1
fi

artifact_root="$1"
set_id="$2"
set_dir="${artifact_root}/${set_id}"

mkdir -p "${set_dir}/repo/repodata"

# 内核与 initrd：内容是什么无关紧要，identify/precheck 只关心「文件存在」
# 与「特征文件存在」，不解析内核镜像本身。
printf 'fake kernel for e2e\n' >"${set_dir}/vmlinuz"
printf 'fake initrd for e2e\n' >"${set_dir}/initrd.img"

# .treeinfo 是 RHEL 系的识别特征文件（internal/imports/identify.go）。
cat >"${set_dir}/.treeinfo" <<'EOF'
[general]
family = Rocky Linux
version = 9.4
arch = x86_64
EOF

# 仓库元数据：docs/contracts/filesystem.md 与
# internal/statemachine/precheck.go 都把 repodata/repomd.xml 的存在性
# 作为「仓库真正可用」的判据（空目录能 stat 成功但对 installer 毫无用处）。
cat >"${set_dir}/repo/repodata/repomd.xml" <<'EOF'
<?xml version="1.0" encoding="UTF-8"?>
<repomd xmlns="http://linux.duke.edu/metadata/repo"></repomd>
EOF

echo "已在 ${set_dir} 造出假制品树" >&2
