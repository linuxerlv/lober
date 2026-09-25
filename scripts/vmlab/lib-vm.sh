#!/usr/bin/env bash
# lib-vm.sh 是 vmlab 实验床的 QEMU 参数常量库：把「用哪个 CPU model、开哪个
# 加速器才能在本机跑起来」这一件事固化成常量，供后续 vmlab 脚本 source。
#
# ---------------------------------------------------------------------------
# 为什么需要这个文件（WHPX 下 CPU model 的坑）
# ---------------------------------------------------------------------------
#
# 本机走 Windows Hypervisor Platform（WHPX）加速，不是 KVM/TCG，行为上有
# 两条硬约束是先验已知、不需要重新试错的：
#
#   1. `-cpu max` 与 `-cpu host` 在 WHPX 下必然 `WHPX: Unexpected VP exit
#      code 4`，串口零输出——WHPX 不支持把宿主 CPU 的全部特性位透传进去，
#      遇到它不认的特性位直接连进 vCPU 都失败，不会有任何来自 guest 内部
#      的诊断信息。
#   2. 必须显式加 `kernel-irqchip=off`，否则 guest 侧狂刷
#      `Ignoring request for interrupt vector 0`——WHPX 的内核态 irqchip
#      实现与这套中断投递路径不兼容。
#
# 真正需要靠实测才能回答的问题是：**该用哪个具体的 CPU model**。原计划的
# 推断是 `-cpu qemu64` 只实现到 x86-64-v1（缺 SSE4.2/POPCNT），而 RHEL 9
# 系的 glibc 把 x86-64-v2 定为最低 baseline，预期会在 anaconda 启动前报
# `Fatal glibc error: CPU does not support x86-64-v2`。
#
# 实测结果推翻了这个推断（2026-09-25，QEMU 11.0.1）：`-cpu qemu64` 直接
# 引导 OpenCloudOS 9.6（RHEL 9 系）的 installer 内核，全程无 glibc 报错，
# 一路走到 anaconda 的文本/VNC 模式选择提示——即已进入交互式安装界面，说明
# glibc 的 CPU 检查通过了。连跑两次结果一致（均在同一处提示停住，无异常）。
#
# 为什么推断会错：x86-64-v2 baseline 真正卡的是 SSE4.2/POPCNT/CX16 这几个
# 特性位，而 QEMU 的 `qemu64` CPU model 定义本身会随 QEMU 版本演进——
# 11.0.1 里的 `qemu64` 默认特性集已经比早期定义丰富，具体包含了什么足以
# 通过 glibc 检查的特性位未逐一比对（此处用的是端到端验证，不是特性位核
# 对），但结论是确定的：**不需要 Nehalem 或手动拼 `+ssse3,+sse4.1,+sse4.2,
# +popcnt` 这类候选，`qemu64` 本身就够用**。这两个候选原计划里作为候补，
# 因为第一个候选已经通过，未实际尝试。
#
# 取证方式：直接引导 ISO 内的 installer 内核（`-kernel`/`-initrd`），不经
# GRUB 菜单——OpenCloudOS 的 grub.cfg 默认不带 `console=ttyS0`，走
# `-cdrom` 走完整 GRUB 流程会导致串口拿不到任何输出（视频终端 vs 串口
# 终端的选择在 GRUB/内核层，不在 QEMU 层），因此改为提取 vmlinuz/initrd.img
# 后自己拼 `-append "console=ttyS0 ..."`。`-cdrom` 仍然挂着，只是不再靠它
# 触发引导，而是给 `inst.stage2=hd:LABEL=...` 提供 stage2 数据源。
#
# 另一个取证过程中撞到的坑：QEMU 是原生 Windows 二进制，接收 MSYS 风格
# 深层路径（`/e/src/...`）当参数会在解析阶段直接报错退出而不是超时——
# 单层驱动器根路径（`/d/foo.iso`）却能正常工作，行为不一致且没有文档，
# 因此 vmlab_check_cpu 内部统一用 `cygpath -w` 转成 `E:...` 形式再传
# 给 QEMU，细节见函数体内注释。
#
# ---------------------------------------------------------------------------
# 用法
# ---------------------------------------------------------------------------
#
#   . "$(dirname "$0")/vmlab/lib-vm.sh"
#   "${VMLAB_QEMU_BIN}" "${VMLAB_CPU_ARGS[@]}" -m 3072 ... 其它参数
#
#   vmlab_check_cpu /d/some-rhel-family.iso OC-9-6-x86_64   # 自检，见下方函数注释

# ---------------------------------------------------------------------------
# VMLAB_QEMU_BIN：QEMU 可执行文件绝对路径
# ---------------------------------------------------------------------------
#
# QEMU 装在 D:\msys\ucrt64（msys2 的 ucrt64 子系统），不在 PATH 上，因此用
# 绝对路径而不是假设 `command -v qemu-system-x86_64` 能找到。允许用环境变量
# 覆盖，方便换机器时不用改这个文件。
VMLAB_QEMU_BIN="${VMLAB_QEMU_BIN:-/d/msys/ucrt64/bin/qemu-system-x86_64.exe}"

# ---------------------------------------------------------------------------
# VMLAB_CPU_ARGS：目标机 -cpu 与 -accel 参数（实测胜出组合）
# ---------------------------------------------------------------------------
#
# 数组形式是为了在展开成命令行参数时不需要关心内部有没有空格或逗号——
# `"${VMLAB_CPU_ARGS[@]}"` 直接铺开即可，调用方不用关心里面具体拆成几个
# token。
VMLAB_CPU_ARGS=(
	-accel whpx,kernel-irqchip=off
	-cpu qemu64
)

# ---------------------------------------------------------------------------
# vmlab_check_cpu：自检——直接引导某个 ISO 的 installer 内核，验证在当前
# VMLAB_CPU_ARGS 下能否越过 CPU 兼容性检查、跑到 anaconda 交互提示
# ---------------------------------------------------------------------------
#
# 用法：vmlab_check_cpu <iso绝对路径> <FSLABEL>
#
#   iso绝对路径  形如 /d/OpenCloudOS-9.6-x86_64-minimal.iso
#   FSLABEL      ISO 内 stage2 所在文件系统的 LABEL，用于拼
#                `inst.stage2=hd:LABEL=<FSLABEL>`——OpenCloudOS 9.6 是
#                `OC-9-6-x86_64`（见 ISO 内 boot/grub2/grub.cfg 的
#                `search --set=root -l '<LABEL>'`）。不同 ISO 这个值不同，
#                因此做成参数而不是硬编码。
#
# 判成败的方式：没有交互、不依赖显示界面——把串口整段重定向到文件，跑完后
# grep 这个文件。找到 anaconda 的启动横幅（"Starting installer" /
# "anaconda ... started"）即认为通过了 CPU 兼容性检查（因为 glibc 的
# x86-64-v2 检查发生在 anaconda 进程启动之前，能看到这行横幅说明检查已经
# 通过）；若文件里出现 "does not support x86-64" 之类的 glibc 报错，或者
# 全程没等到横幅，判失败。
#
# 有超时保护（默认 90 秒，可用第三个参数覆盖）：真实安装内核从引导到
# anaconda 起来大约 30-60 秒（取决于 initrd 解压速度），90 秒对自检足够，
# 且这只是「能不能过 CPU 检查」的验证，不是完整装机，不需要更长。超时后
# 用 `timeout` 发 SIGTERM 杀掉 QEMU 进程，不会有残留 VM 占内存。
#
# 提取 vmlinuz/initrd.img 的产物缓存在 .tmp/vmlab/extract/<FSLABEL>/ 下，
# 重复调用不用每次都从 1.6G 的 ISO 里重新解 100+MB 的 initrd（extract 一次
# 要花几秒到十几秒，取决于磁盘）；.tmp/ 已在 .gitignore 里，不会污染仓库。
vmlab_check_cpu() {
	local iso="$1"
	local fslabel="$2"
	local boot_timeout="${3:-90}"

	if [ -z "${iso}" ] || [ -z "${fslabel}" ]; then
		echo "用法: vmlab_check_cpu <iso绝对路径> <FSLABEL> [超时秒数]" >&2
		return 1
	fi
	if [ ! -f "${iso}" ]; then
		echo "错误: ISO 不存在: ${iso}" >&2
		return 1
	fi

	local sevenzip="/d/msys/ucrt64/bin/7z.exe"
	if [ ! -x "${sevenzip}" ]; then
		echo "错误: 找不到 7z: ${sevenzip}（用于从 ISO 内提取 installer 内核）" >&2
		return 1
	fi

	local repo_root
	repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
	local cache_dir="${repo_root}/.tmp/vmlab/extract/${fslabel}"
	local kernel="${cache_dir}/vmlinuz"
	local initrd="${cache_dir}/initrd.img"

	if [ ! -f "${kernel}" ] || [ ! -f "${initrd}" ]; then
		mkdir -p "${cache_dir}"
		# 用 7z 的 `e`（extract，不是 `x`）会自动拍平路径层级——ISO 内是
		# images/pxeboot/vmlinuz，但 `e` 加 `-o cache_dir` 后直接落在
		# cache_dir 根下，不会带出 images/pxeboot/ 这层子目录，不需要额外 mv。
		if ! "${sevenzip}" e -y "${iso}" "images/pxeboot/vmlinuz" "images/pxeboot/initrd.img" -o"${cache_dir}" >/dev/null 2>&1; then
			echo "错误: 从 ISO 提取 vmlinuz/initrd.img 失败: ${iso}" >&2
			return 1
		fi
		if [ ! -f "${kernel}" ] || [ ! -f "${initrd}" ]; then
			echo "错误: 提取后仍找不到 ${kernel} 或 ${initrd}（ISO 内路径可能不是 images/pxeboot/，非本函数支持范围内的 ISO 结构）" >&2
			return 1
		fi
	fi

	local serial_log="${cache_dir}/serial-check.log"
	rm -f "${serial_log}"

	# QEMU 是原生 Windows 二进制，接收 MSYS 风格路径（/e/... 这种深层路径）
	# 时不可靠：实测 `-serial file:/e/src/...` 会在参数解析阶段直接报
	# `open ... failed` 后退出（不是超时，是立即失败），但单层驱动器根路径
	# 形如 `/d/foo.iso` 却能正常工作——这个不一致的行为看起来是 MSYS2 的
	# argv 路径翻译只覆盖了部分场景，具体规则没有文档化，不值得花时间猜。
	# 稳妥做法是不依赖这层翻译：统一用 `cygpath -w` 转成 `E:\...` 这种
	# Windows 原生形式再传给 QEMU，四个路径参数（kernel/initrd/cdrom/
	# serial file）全部转换，不区分哪个「看起来」应该没问题。
	local win_kernel win_initrd win_iso win_serial_log
	win_kernel="$(cygpath -w "${kernel}")"
	win_initrd="$(cygpath -w "${initrd}")"
	win_iso="$(cygpath -w "${iso}")"
	win_serial_log="$(cygpath -w "${serial_log}")"

	timeout "${boot_timeout}" "${VMLAB_QEMU_BIN}" \
		-M pc \
		"${VMLAB_CPU_ARGS[@]}" \
		-m 3072 \
		-kernel "${win_kernel}" \
		-initrd "${win_initrd}" \
		-append "console=ttyS0 inst.stage2=hd:LABEL=${fslabel} inst.text quiet" \
		-cdrom "${win_iso}" \
		-nographic \
		-serial "file:${win_serial_log}" \
		-monitor none \
		-no-reboot >/dev/null 2>&1
	# 不检查上面这条命令自身的退出码：超时是预期路径（装机内核不会自己
	# 退出，跑到 anaconda 提示符就会一直等交互输入），成败判据全部来自
	# 串口日志内容，见下方。

	if grep -q -a -i "does not support x86-64\|fatal glibc error" "${serial_log}"; then
		echo "自检失败: glibc CPU 兼容性检查未通过，串口日志: ${serial_log}" >&2
		return 1
	fi
	if ! grep -q -a "Starting installer\|anaconda.*started" "${serial_log}"; then
		echo "自检失败: 串口日志里没等到 anaconda 启动横幅，可能是其它原因卡住（不一定是 CPU 问题），串口日志: ${serial_log}" >&2
		return 1
	fi

	return 0
}
