#!/usr/bin/env bash
# mkfixture-debian.sh 造一个能通过 Debian OS 家族识别与抽取的假制品目录树，
# 供 scripts/e2e-debian.sh 使用。对应 Debian 端到端验证任务，依据 SPEC §2.1
# 的识别表与 internal/imports/extract_debian.go 的抽取逻辑。
#
# 不用真 ISO：几百 MB 的下载对一个每次提交都要跑的脚本不可接受，而识别/抽取
# 逻辑关心的只有特征文件与目录结构：
#   - internal/imports/identify.go：dists/ 目录存在即识别为 Debian
#     （注意 Ubuntu 的 .disk/info 判定排在 Debian 之前，本脚本不造
#     .disk/info，避免被误判为 Ubuntu）
#   - internal/imports/extract_debian.go 的 findDebianInstallerFile：
#     递归查找名为 vmlinuz/initrd.gz 的文件（不深入 dists/、pool/ 两棵
#     仓库树），源文件名固定，落盘后改名为 linux/initrd.gz
#
# 本脚本产出的树直接落在制品根目录下的 <artifact_set_id>/ 子目录
# （docs/contracts/filesystem.md 硬约束 1），不经过真实的 ISO 挂载/导入 job
# 流程——见 scripts/e2e-debian.sh 顶部注释对「制品集登记→导入 job」这条
# 契约缺口的说明（与 scripts/e2e.sh 的 RHEL 绕法同源）。
#
# 用法：mkfixture-debian.sh <制品根目录> <制品集 ID>
# 产出：<制品根目录>/<制品集 ID>/{vmlinuz,initrd.gz,dists/stable/Release,
#        pool/main/b/bash/bash_5.2-5_amd64.deb}
#
# 注意：本脚本产出的是「ExtractDebian 的输入」（模拟 ISO 内容本身），不是
# 「ExtractDebian 的输出」——与 mkfixture.sh 造 RHEL 假制品树的差异：RHEL
# e2e.sh 绕过了真实抽取（直接 sqlite3 写 paths_json 指向源文件名 vmlinuz/
# initrd.img），而本脚本配合 scripts/e2e-debian.sh 优先尝试真实抽取路径
# （见该脚本正文），因此这里的目录结构必须是 ExtractDebian 能识别与抽取的
#「ISO 内容视图」，不能直接就是 linux/initrd.gz 这两个最终文件名。
set -euo pipefail

if [ "$#" -ne 2 ]; then
	echo "用法: $0 <制品根目录> <制品集 ID>" >&2
	exit 1
fi

artifact_root="$1"
set_id="$2"
iso_dir="${artifact_root}/${set_id}"

# installer 子目录名随架构变化（真实 Debian ISO 惯例是 install.amd/、
# install.arm64/ 等），本脚本固定用 install.amd（对应 x86_64/amd64）。
mkdir -p "${iso_dir}/install.amd"
mkdir -p "${iso_dir}/dists/stable/main/binary-amd64"
mkdir -p "${iso_dir}/pool/main/b/bash"

# 内核与 initrd：内容是什么无关紧要，identify/extract 只关心「文件存在」
# 与「文件名匹配」，不解析内核镜像本身。源文件名固定为 vmlinuz/initrd.gz
# （ExtractDebian 的 debianInstallerKernelName/debianInstallerInitrdName）。
printf 'fake kernel for e2e-debian\n' >"${iso_dir}/install.amd/vmlinuz"
printf 'fake initrd for e2e-debian\n' >"${iso_dir}/install.amd/initrd.gz"

# dists/ 目录本身是 Debian 的识别特征（internal/imports/identify.go：
# existsDir(root, "dists")），内容随意，只要目录存在即可被识别；补一个
# Release 文件与一个 Packages 文件让仓库树看起来像真实内容（不是空目录），
# 与 extract_debian_test.go 的 debianFakeRoot() 造法一致。
cat >"${iso_dir}/dists/stable/Release" <<'EOF'
Codename: bookworm
EOF
cat >"${iso_dir}/dists/stable/main/binary-amd64/Packages" <<'EOF'
Package: bash
EOF

# pool/ 是第二棵仓库树，同样只要求存在性（ExtractDebian 会把它整棵复制到
# repo/ 下）。
printf 'fake deb package for e2e\n' >"${iso_dir}/pool/main/b/bash/bash_5.2-5_amd64.deb"

echo "已在 ${iso_dir} 造出假 Debian ISO 内容树（供 ExtractDebian 抽取）" >&2
