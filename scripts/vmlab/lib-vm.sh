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
# 特性位，QEMU 11.0.1 的 `qemu64` 默认特性集足以通过 glibc 检查（未逐一核对
# 特性位，用的是端到端验证）。结论：**不需要 Nehalem 或手动拼特性位的候选，
# `qemu64` 本身就够用**，因第一个候选已通过，未实际尝试候补方案。
#
# 取证方式：直接引导 ISO 内的 installer 内核（`-kernel`/`-initrd`），不经
# GRUB 菜单——OpenCloudOS 的 grub.cfg 默认不带 `console=ttyS0`，走
# `-cdrom` 走完整 GRUB 流程会导致串口拿不到任何输出（视频终端 vs 串口
# 终端的选择在 GRUB/内核层，不在 QEMU 层），因此改为提取 vmlinuz/initrd.img
# 后自己拼 `-append "console=ttyS0 ..."`。`-cdrom` 仍然挂着，只是不再靠它
# 触发引导，而是给 `inst.stage2=hd:LABEL=...` 提供 stage2 数据源。
#
# 另一个取证过程中撞到的坑，权威说明见 vmlab_winpath 的函数注释：QEMU 是
# 原生 Windows 二进制，判别变量是「参数整体是不是一个纯 POSIX 绝对路径」，
# 与路径深浅无关——`-cdrom /d/foo.iso` 这种裸参数能过 MSYS 的 argv 翻译，
# 但 `-serial file:/d/foo.log` 这种复合参数（路径嵌在字符串中间）翻译不到，
# 不看这条会得出「浅路径能用」的错误结论（本文件最初就是这样错的）。
#
# ---------------------------------------------------------------------------
# 用法
# ---------------------------------------------------------------------------
#
#   . "$(dirname "${BASH_SOURCE[0]}")/vmlab/lib-vm.sh"
#   vmlab_require || exit 1
#   "${VMLAB_QEMU_BIN}" "${VMLAB_CPU_ARGS[@]}" -M "${VMLAB_MACHINE}" -m "${VMLAB_MEM_MB}" ... 其它参数
#
#   vmlab_check_cpu /d/some-rhel-family.iso OC-9-6-x86_64   # 自检，见下方函数注释

# ---------------------------------------------------------------------------
# VMLAB_ROOT / VMLAB_TMP_DIR：仓库根与运行期产物落盘根
# ---------------------------------------------------------------------------
#
# 必须在 source 这个文件时（而不是某个函数被调用时）就解析好——本文件是被
# source 的库，不是被执行的脚本，调用方 source 之后大概率会 cd 到别处
# 继续干活（六个家族的装机脚本各自建自己的 workdir）。若把这段解析挪进
# 函数体，解析时机变成「函数被调用时」，而那时 cwd 早就不是仓库内的路径了。
# 只有 BASH_SOURCE[0] 恰好是绝对路径时才仍然解析得对——调用方用相对路径
# source（本文件行首用法示例就是相对形式）且中途换过 cwd 时就会失败。
#
# 本文件上一版就是写在函数体里的，review 时实测出后果：解析失败得到空串，
# `cache_dir` 退化成 `/.tmp/vmlab/extract/<LABEL>`（盘符根、仓库外），7z 把
# 上百 MB 的 initrd 解到仓库外面，且那个路径不受 .gitignore 的 `/.tmp/`
# 管辖。这是 review 阶段发现的隐患，不是已发生的事故——本仓库真正为
# 「同一份逻辑散落多处」付过代价的那次记在 scripts/e2e/lib-db.sh:23-26
# （仓库可用性判据常量复制三份，导致 Debian 装机 100% 卡死，commit 65883a5）。
#
# 固定在顶层、source 时求值一次，消除这个时序依赖。
#
# 用 BASH_SOURCE[0] 而不是 $0：这个文件被 source 时 $0 是调用方脚本的路径，
# BASH_SOURCE[0] 才是这个文件自己的路径——两者在被 source 的场景下语义不同，
# 用错会解析出调用方所在目录而不是这个库所在目录。
VMLAB_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
VMLAB_TMP_DIR="${VMLAB_ROOT}/.tmp/vmlab"

# ---------------------------------------------------------------------------
# VMLAB_QEMU_BIN / VMLAB_7Z：外部工具绝对路径
# ---------------------------------------------------------------------------
#
# 都装在 D:\msys\ucrt64（msys2 的 ucrt64 子系统），不在 PATH 上，因此用
# 绝对路径而不是假设 `command -v` 能找到。允许用环境变量覆盖，方便换机器
# 时不用改这个文件。是否存在的校验放在 vmlab_require，这里只定义路径。
VMLAB_QEMU_BIN="${VMLAB_QEMU_BIN:-/d/msys/ucrt64/bin/qemu-system-x86_64.exe}"
VMLAB_7Z="${VMLAB_7Z:-/d/msys/ucrt64/bin/7z.exe}"

# ---------------------------------------------------------------------------
# VMLAB_MACHINE / VMLAB_MEM_MB / VMLAB_CPU_ARGS：目标机基础参数（实测胜出组合）
# ---------------------------------------------------------------------------
#
# 机器类型与内存大小单独拉成常量而不是内联在调用处：任务 5 要挂 OVMF
# pflash 走 UEFI，`-M pc`（传统 BIOS/SeaBIOS 机器类型）大概率要换成
# `-M q35`（更贴近真实 UEFI 主板拓扑），WHPX + q35 是需要单独验证的组合，
# 现在先给这两个值一个名字，等任务 5 验证出结果后改一处而不是改散在各脚本
# 里的內联字面量。
#
# CPU/加速器参数用数组：展开成命令行参数时不需要关心内部有没有空格或
# 逗号，`"${VMLAB_CPU_ARGS[@]}"` 直接铺开即可。
VMLAB_MACHINE="${VMLAB_MACHINE:-pc}"
VMLAB_MEM_MB="${VMLAB_MEM_MB:-3072}"
VMLAB_CPU_ARGS=(
	-accel whpx,kernel-irqchip=off
	-cpu qemu64
)

# ---------------------------------------------------------------------------
# vmlab_require：确认 QEMU 与 7z 可用
# ---------------------------------------------------------------------------
#
# 仿 scripts/e2e/lib-db.sh 的 db_require 先例：把「这台机器能不能跑 vmlab」
# 的校验收敛到一处，调用方在真正开始干活前调一次，失败时报出试过的路径，
# 不是一句「找不到」让人自己去猜。
#
# QEMU 排在 7z 前面校验：QEMU 是这个库存在的理由，比 7z（只是取内核用的
# 辅助工具）更要紧，先前一版恰好校验反了（7z 校验了、QEMU 没校验）。
vmlab_require() {
	if [ ! -x "${VMLAB_QEMU_BIN}" ]; then
		echo "错误: 找不到 QEMU: ${VMLAB_QEMU_BIN}" >&2
		echo "可用环境变量 VMLAB_QEMU_BIN 指定绝对路径后重试。" >&2
		return 1
	fi
	if [ ! -x "${VMLAB_7Z}" ]; then
		echo "错误: 找不到 7z: ${VMLAB_7Z}（用于从 ISO 内提取 installer 内核）" >&2
		echo "可用环境变量 VMLAB_7Z 指定绝对路径后重试。" >&2
		return 1
	fi
	return 0
}

# ---------------------------------------------------------------------------
# vmlab_winpath：把 POSIX 绝对路径转成 QEMU（原生 Windows 二进制）能正确
# 解析的形式
# ---------------------------------------------------------------------------
#
# 判别变量不是路径深浅，是「这个路径是不是作为裸参数传给 QEMU」——用四组
# 实测独立验证过（2026-09-25）：
#
#   | 参数形态 \ 路径深浅 | 深路径（/e/src/...）      | 浅路径（/d/foo.iso） |
#   |----------------------|---------------------------|------------------------|
#   | 裸参数（-cdrom X）    | 通（MSYS 翻译成 E:/...）  | 通（MSYS 翻译成 D:/...） |
#   | 复合参数（file:X）    | 失败（open ... failed）    | 失败（open ... failed）  |
#
# 翻译结果是**正斜杠**（实测 QEMU 回显 `-cdrom D:/no-such-shallow.iso`，
# 不是 `D:\`）——这也正是本函数用 `cygpath -m` 而不是 `-w` 的理由：`-m` 同样
# 输出正斜杠，内嵌进 `-drive file=X,if=pflash` 这类参数时不存在反斜杠被
# 当转义符的隐患，且与仓库既有 `to_win_path`（scripts/e2e*.sh 六处副本）
# 的选择一致。
#
# 深浅这一维两格结果相同，裸/复合这一维两格结果相反：MSYS 的 argv 路径
# 翻译只作用于「整个参数就是一个路径」的裸参数，不会深入 `file:X` 这种
# 路径被嵌在字符串中间的复合参数去翻译内嵌的那一段。QEMU 的 `-kernel`/
# `-initrd`/`-cdrom` 是裸参数，已实测不转也能工作，但 `-serial file:X`、
# `-drive file=X,if=pflash` 这类复合参数必须转——本函数对所有路径统一转，
# 不区分「看起来」该不该转，省得每次新增一个 QEMU 参数都要重新验证一遍
# 它是裸的还是复合的。
#
# 用 `cygpath -m` 而不是 `-w`：`-m` 输出带盘符、正斜杠的形式（如
# `E:/src/...`），嵌进 `-drive file=X,if=pflash,format=raw` 这类逗号分隔
# 的复合参数时没有反斜杠转义的隐患（`-w` 输出的反斜杠在某些参数解析路径
# 下可能被当成转义字符）；`-m` 输出的形式已实测能被 QEMU 的 `-serial
# file:`、`-drive file=,if=pflash`（UEFI OVMF 场景，任务 5 会用到）两种
# 复合参数正确解析。与仓库 scripts/e2e.sh 里 `to_win_path` 的选择一致，
# 不在同一个仓库里维护两套「转 Windows 路径」的约定。
#
# 空参数显式报错：`cygpath -m ""` 本身返回非零，但调用方若没开 `set -e`，
# 静默拿到空字符串后传给 QEMU 会变成 `-kernel ""` 这种看起来合法、实际
# 指向空路径的参数，失败现场比「函数直接报错」更难查。
vmlab_winpath() {
	local input="${1:-}"
	if [ -z "${input}" ]; then
		echo "错误: vmlab_winpath 收到空路径参数" >&2
		return 1
	fi
	if command -v cygpath >/dev/null 2>&1; then
		# 带 -a 强制绝对化：cygpath 对相对输入原样返回相对形式，而 QEMU 会按
		# 它自己的 cwd 去解析，那未必是调用方 shell 的 cwd。后续九个任务会有
		# 很多新调用点，靠函数注释里一句「请传绝对路径」约束不如让函数自己
		# 兜住。-a 对已是绝对路径的输入无副作用，对空串仍然报错（都已实测）。
		cygpath -m -a "${input}"
	else
		# 非 MSYS 环境（真正的 Linux/macOS，理论上不会跑到这个分支，因为
		# QEMU 走 WHPX 加速本就是 Windows 独有的场景）：路径解析本就一致，
		# 原样返回，不因为缺一个转换工具就整体失败。
		echo "${input}"
	fi
}

# ---------------------------------------------------------------------------
# vmlab_verdict_from_serial_log：从串口日志内容判定 CPU 兼容性自检成败
# ---------------------------------------------------------------------------
#
# 从 vmlab_check_cpu 里切出来单独成一个函数：判成败的两条 grep 规则是这个
# 自检机制里最容易写错、也最值得回归的部分，但原来焊在真实开机流程里，
# 只能靠一次几十秒的真实引导才能触达——改一个字符都要重新等一轮开机才知道
# 改对没改对。拆出来后可以直接喂固定的日志文件测试，覆盖测试用真开机很难
# 稳定复现的几种边界情况（尤其是「文件不存在」与「文件存在但是空」这两种
# ——真实场景下几乎不会遇到，但代表了「QEMU 没起来」与「QEMU 起来了但立刻
# 死了」两种不同的失败模式，值得单独测）。
#
# 输入：串口日志文件路径。
# 输出：stdout 打一行人可读的结论（成功/失败原因），供调用方 echo 之外
#       还想复用文案时不用重复拼字符串；返回码 0=通过 1=不通过。
vmlab_verdict_from_serial_log() {
	local serial_log="${1:-}"

	if [ -z "${serial_log}" ]; then
		echo "自检失败: 未提供串口日志路径"
		return 1
	fi
	if [ ! -f "${serial_log}" ]; then
		echo "自检失败: 串口日志不存在（${serial_log}），QEMU 很可能启动即失败，若旁边有 qemu-stderr.log 请先看那份"
		return 1
	fi
	if [ ! -s "${serial_log}" ]; then
		echo "自检失败: 串口日志存在但是空文件（${serial_log}），QEMU 可能连引导都没开始，若旁边有 qemu-stderr.log 请先看那份"
		return 1
	fi
	if grep -q -a -i "does not support x86-64\|fatal glibc error" "${serial_log}"; then
		echo "自检失败: glibc CPU 兼容性检查未通过（串口日志: ${serial_log}）"
		return 1
	fi
	if ! grep -q -a "Starting installer\|anaconda.*started" "${serial_log}"; then
		echo "自检失败: 串口日志里没等到 anaconda 启动横幅，可能是其它原因卡住而不是 CPU 问题（串口日志: ${serial_log}）"
		return 1
	fi

	echo "自检通过: 串口日志显示 anaconda 已启动，glibc CPU 兼容性检查已通过（串口日志: ${serial_log}）"
	return 0
}

# ---------------------------------------------------------------------------
# vmlab_check_cpu：自检——直接引导某个 ISO 的 installer 内核，验证在当前
# VMLAB_CPU_ARGS 下能否越过 CPU 兼容性检查、跑到 anaconda 交互提示
# ---------------------------------------------------------------------------
#
# 用法：vmlab_check_cpu <iso绝对路径> <FSLABEL> [超时秒数]
#
#   iso绝对路径  形如 /d/OpenCloudOS-9.6-x86_64-minimal.iso
#   FSLABEL      ISO 内 stage2 所在文件系统的 LABEL，用于拼
#                `inst.stage2=hd:LABEL=<FSLABEL>`——OpenCloudOS 9.6 是
#                `OC-9-6-x86_64`（见 ISO 内 boot/grub2/grub.cfg 的
#                `search --set=root -l '<LABEL>'`）。不同 ISO 这个值不同，
#                因此做成参数而不是硬编码。
#
# 判成败：见 vmlab_verdict_from_serial_log。本函数只负责把 QEMU 跑起来、
# 把串口和 stderr 分别落盘，再把判定工作转交出去。
#
# 有超时保护（默认 90 秒，可用第三个参数覆盖）：真实安装内核从引导到
# anaconda 起来大约 30-60 秒（取决于 initrd 解压速度），90 秒对自检足够，
# 且这只是「能不能过 CPU 检查」的验证，不是完整装机，不需要更长。
# `timeout` 加 `-k 5`：先发 SIGTERM，5 秒后若进程还没退出再补一发
# SIGKILL——原生 Win32 QEMU 收到 SIGTERM 是否一定会体面退出没有验证过，
# 六个家族串行跑的场景下，一个没收干净的 3G VM 残留下来的代价不小
# （宿主总共 15.5G 内存），补一道保险比事后排查内存去哪了便宜。
#
# 提取 vmlinuz/initrd.img 的产物缓存在 VMLAB_TMP_DIR/extract/<FSLABEL>-<指纹>/
# 下，重复调用不用每次都从 1.6G 的 ISO 里重新解 100+MB 的 initrd。缓存 key
# 带上 ISO 的 size+mtime 指纹（不只是 FSLABEL）：六个家族的 ISO 若换版本
# 重下、但 FSLABEL 恰好没变（同一发行版换小版本很常见），不带指纹会静默
# 复用旧内核，表现为「换了 ISO 行为一模一样」，比报错更难查。size+mtime
# 足够区分「换了一个不同的文件」，不需要为此算完整校验和拖慢每次调用。
vmlab_check_cpu() {
	local iso="${1:-}"
	local fslabel="${2:-}"
	local boot_timeout="${3:-90}"

	if [ -z "${iso}" ] || [ -z "${fslabel}" ]; then
		echo "用法: vmlab_check_cpu <iso绝对路径> <FSLABEL> [超时秒数]" >&2
		return 1
	fi
	if [ ! -f "${iso}" ]; then
		echo "错误: ISO 不存在: ${iso}" >&2
		return 1
	fi
	if ! vmlab_require; then
		return 1
	fi

	local iso_size iso_mtime iso_fingerprint
	iso_size="$(stat -c '%s' "${iso}" 2>/dev/null || echo 0)"
	iso_mtime="$(stat -c '%Y' "${iso}" 2>/dev/null || echo 0)"
	iso_fingerprint="${iso_size}-${iso_mtime}"

	local cache_dir="${VMLAB_TMP_DIR}/extract/${fslabel}-${iso_fingerprint}"
	local kernel="${cache_dir}/vmlinuz"
	local initrd="${cache_dir}/initrd.img"

	if [ ! -f "${kernel}" ] || [ ! -f "${initrd}" ]; then
		mkdir -p "${cache_dir}"
		# 用 7z 的 `e`（extract，不是 `x`）会自动拍平路径层级——ISO 内是
		# images/pxeboot/vmlinuz，但 `e` 加 `-o cache_dir` 后直接落在
		# cache_dir 根下，不会带出 images/pxeboot/ 这层子目录。
		if ! "${VMLAB_7Z}" e -y "${iso}" "images/pxeboot/vmlinuz" "images/pxeboot/initrd.img" -o"${cache_dir}" >/dev/null 2>&1; then
			echo "错误: 从 ISO 提取 vmlinuz/initrd.img 失败: ${iso}" >&2
			return 1
		fi
		if [ ! -f "${kernel}" ] || [ ! -f "${initrd}" ]; then
			echo "错误: 提取后仍找不到 ${kernel} 或 ${initrd}（ISO 内路径可能不是 images/pxeboot/，非本函数支持范围内的 ISO 结构）" >&2
			return 1
		fi
	fi

	local serial_log="${cache_dir}/serial-check.log"
	local qemu_stderr_log="${cache_dir}/qemu-stderr.log"
	rm -f "${serial_log}" "${qemu_stderr_log}"

	local win_kernel win_initrd win_iso win_serial_log
	win_kernel="$(vmlab_winpath "${kernel}")" || return 1
	win_initrd="$(vmlab_winpath "${initrd}")" || return 1
	win_iso="$(vmlab_winpath "${iso}")" || return 1
	win_serial_log="$(vmlab_winpath "${serial_log}")" || return 1

	timeout -k 5 "${boot_timeout}" "${VMLAB_QEMU_BIN}" \
		-M "${VMLAB_MACHINE}" \
		"${VMLAB_CPU_ARGS[@]}" \
		-m "${VMLAB_MEM_MB}" \
		-kernel "${win_kernel}" \
		-initrd "${win_initrd}" \
		-append "console=ttyS0 inst.stage2=hd:LABEL=${fslabel} inst.text quiet" \
		-cdrom "${win_iso}" \
		-nographic \
		-serial "file:${win_serial_log}" \
		-monitor none \
		-no-reboot >/dev/null 2>"${qemu_stderr_log}"
	# 不检查上面这条命令自身的退出码：超时是预期路径（装机内核不会自己
	# 退出，跑到 anaconda 提示符就会一直等交互输入），成败判据全部来自
	# 串口日志内容，交给 vmlab_verdict_from_serial_log 判定。stderr 单独
	# 落盘而不是像先前一版那样 `2>&1` 丢掉——QEMU 自身报错（例如参数解析
	# 阶段的 `open ... failed`）原本就打在这里，之前把它跟 stdout 一起
	# 扔进 /dev/null，连"到底是不是 QEMU 起不来"都判断不了，找这个 bug
	# 花的时间比该花的多。

	local verdict_msg
	if verdict_msg="$(vmlab_verdict_from_serial_log "${serial_log}")"; then
		echo "${verdict_msg}"
		return 0
	fi
	echo "${verdict_msg}" >&2
	if [ -s "${qemu_stderr_log}" ]; then
		echo "QEMU stderr（${qemu_stderr_log}）：" >&2
		cat "${qemu_stderr_log}" >&2
	fi
	return 1
}
