"""QEMU 生命周期：VM-S 与目标机的起停、串口日志落盘。

前七类是参数拼装的纯逻辑，不起真实进程——`_target_args`/`_server_args` 是
从 `start_target`/`start_server` 里拆出来的纯函数，产出 argv 列表给 QEMU，
测试直接断言列表内容。这样四条回归护栏（类别 3、4、5、7）才能在任何机器上
无条件跑：起真实 VM 现在本机做不到（没有可用的装机产物），但「会不会传出
`-cpu max`」这种问题不需要真的起进程才能回答。

类别 8（`stop_target` 幂等）需要一个真实进程（哪怕不是 QEMU）来验证「进程
已死时不抛」，标 `needs_vm`——语义上它测的是本模块管理真实进程生命周期的
能力，跳过条件与其它需要连 VM-S 的用例一致。
"""

from __future__ import annotations

import filecmp
import subprocess
import time
from pathlib import Path

import pytest

from lobertest.config import load
from lobertest.qemu import (
    _ensure_vars_copy,
    _server_args,
    _target_args,
    archived_log_path,
    kill_stale_targets,
    serial_log_path,
    stop_target,
    winpath,
)


@pytest.fixture
def settings(tmp_path):
    """独立的 targets_dir，测试互不干扰、不碰真实的 D:/qemu-vm/vmlab-targets。"""
    return load(
        env={
            "LOBERTEST_TARGETS_DIR": str(tmp_path),
        }
    )


# ----------------------------------------------------------------------
# 类别 1-2：firmware 分支。
# ----------------------------------------------------------------------


def test_target_args_uefi_has_pflash_with_entry_scoped_vars(settings, tmp_path):
    """类别 1：uefi 固件的参数列表含 if=pflash，且 vars 副本路径含 entry_id
    ——vars 是每条目独立的可写副本，共用会让引导项互相污染。"""
    args = _target_args(
        entry_id="e-001",
        firmware="uefi",
        mem="4096",
        disk_serial="",
        settings=settings,
    )

    pflash_args = [a for a in args if "if=pflash" in a]
    assert pflash_args, f"应含 if=pflash，实际参数：{args}"
    assert any("e-001" in a for a in pflash_args), (
        f"vars 副本路径应含 entry_id 以保证每条目独立，实际：{pflash_args}"
    )


def test_target_args_is_pure_does_not_write_vars_file(settings):
    """_target_args 是纯函数：只算参数列表，不该在磁盘上创建 vars 副本
    文件（那是 start_target 通过 _ensure_vars_copy 的职责）。写文件的
    副作用一旦混进参数拼装函数，纯函数测试就不再纯，且空文件/零字节
    vars 文件在真实启动时会被 QEMU 当成损坏的 NVRAM。"""
    _target_args(
        entry_id="e-001c",
        firmware="uefi",
        mem="4096",
        disk_serial="",
        settings=settings,
    )

    vars_file = settings.targets_dir / "e-001c-vars.fd"
    assert not vars_file.exists(), (
        f"_target_args 不应有副作用地创建 {vars_file}"
    )


def test_ensure_vars_copy_creates_file_matching_template_bytes(settings):
    """_ensure_vars_copy 是 _target_args side effect 的正确归宿：从
    settings.ovmf_vars_template（edk2-i386-vars.fd，QEMU 自己的固件
    descriptor 声明的 x86_64 nvram-template）逐字节拷贝，不能自造。

    大小不需要与 ovmf_code 一致——vars 与 code 是两个独立的 pflash 设备，
    各自按自己的文件大小映射（模板 540672 字节 vs code.fd 3653632 字节）。
    0 字节或全零文件都不对：真实模板在偏移 0x28 处有 _FVH 固件卷签名，
    缺这个头会被 OVMF 判成损坏的 NVRAM。"""
    vars_path = _ensure_vars_copy("e-vars-1", settings)

    assert vars_path.exists()
    assert filecmp.cmp(vars_path, settings.ovmf_vars_template, shallow=False), (
        "vars 副本内容应与模板逐字节一致"
    )
    # _FVH 固件卷签名，真实模板的标志——全零或截断文件都不会有它。
    assert vars_path.read_bytes()[0x28:0x28 + 4] == b"_FVH"


def test_ensure_vars_copy_is_idempotent_does_not_overwrite_existing(settings):
    """已存在的 vars 文件不应被覆盖——guest 可能已经往里写过 UEFI 变量
    （比如启动顺序），重新创建会丢失这些变量。"""
    vars_path = _ensure_vars_copy("e-vars-2", settings)
    vars_path.write_bytes(b"\x01" * 100)  # 模拟 guest 已写入的变量数据

    second_call_path = _ensure_vars_copy("e-vars-2", settings)

    assert second_call_path == vars_path
    assert vars_path.read_bytes()[:100] == b"\x01" * 100, "已存在的 vars 内容不应被覆盖"


def test_target_args_bios_has_no_pflash(settings):
    """类别 2：bios 固件不挂 pflash（没有 UEFI 变量存储的概念）。"""
    args = _target_args(
        entry_id="e-002",
        firmware="bios",
        mem="4096",
        disk_serial="",
        settings=settings,
    )

    assert not any("if=pflash" in a for a in args), f"bios 不应含 pflash：{args}"


# ----------------------------------------------------------------------
# 类别 3-5：回归护栏，锁住已经踩过的三个坑（约束 1、2、3、5）。
# ----------------------------------------------------------------------


def test_target_args_uses_whpx_and_cpu_qemu64_exactly(settings):
    """类别 3（护栏）：`-cpu` 必须**等于** `qemu64`，不是「不等于 max/host」。

    约束 1：WHPX 下 -cpu max / host 会 `WHPX: Unexpected VP exit code 4`，
    串口零输出。qemu64 是实测唯一可用的值，护栏锁住这个具体值本身——
    排除坏值会让下一个人以为随便换一个「看起来更强」的值都行。
    """
    args = _target_args(
        entry_id="e-003",
        firmware="bios",
        mem="4096",
        disk_serial="",
        settings=settings,
    )

    assert "-accel" in args
    accel_value = args[args.index("-accel") + 1]
    assert accel_value == "whpx,kernel-irqchip=off", (
        f"必须关闭 kernel-irqchip，否则刷 Ignoring request for interrupt "
        f"vector 0，实际：{accel_value}"
    )

    assert "-cpu" in args
    cpu_value = args[args.index("-cpu") + 1]
    assert cpu_value == "qemu64", (
        f"-cpu 必须精确等于 qemu64（WHPX 下 max/host 会 VP exit code 4），"
        f"实际：{cpu_value}"
    )


def test_target_args_uses_nographic_and_rng_not_serial_file(settings):
    """类别 4（护栏）：约束 1（-serial file: 写出 0 字节，必须 -nographic
    让串口走 stdout）与约束 3（缺 virtio-rng-pci 会让 sshd-keygen 卡在取熵，
    实测卡 7 分钟无进展）。"""
    args = _target_args(
        entry_id="e-004",
        firmware="bios",
        mem="4096",
        disk_serial="",
        settings=settings,
    )

    assert "-nographic" in args, "缺 -nographic：串口会走 -serial file: 写出 0 字节"
    assert "virtio-rng-pci" in args, "缺熵源设备：sshd-keygen 会挂起取熵"
    assert not any(a.startswith("file:") for a in args), (
        f"不应出现 -serial file: 形式的参数（这个 QEMU 构建上写出 0 字节）：{args}"
    )


def test_target_args_disk_serial_splits_drive_and_device(settings):
    """类别 5（护栏）：约束 2。disk_serial 非空时必须拆成
    `-drive if=none,id=hd0` + 独立的 `-device virtio-blk-pci,...,serial=`
    ——serial 是设备属性不是驱动属性，写在 -drive 上 QEMU 直接拒绝启动。"""
    args = _target_args(
        entry_id="e-005",
        firmware="bios",
        mem="4096",
        disk_serial="disk-abc123",
        settings=settings,
    )

    drive_args = [a for a in args if a.startswith("if=none") or ",if=none," in a]
    assert any("if=none" in a and "id=hd0" in a for a in args), (
        f"disk_serial 非空时磁盘应走 if=none,id=hd0：{args}"
    )
    device_args = [a for a in args if "virtio-blk-pci" in a]
    assert device_args, f"应有独立的 virtio-blk-pci 设备参数：{args}"
    assert any("drive=hd0" in a and "serial=disk-abc123" in a for a in device_args), (
        f"virtio-blk-pci 设备应挂 drive=hd0 与 serial=disk-abc123：{device_args}"
    )
    # serial 绝不能出现在 -drive 本身上（那是拒绝启动的写法）。
    for a in args:
        if a.startswith("if=none") or (",if=none," in a and "virtio-blk-pci" not in a):
            assert "serial=" not in a, f"-drive 参数不能带 serial=：{a}"


def test_target_args_no_disk_serial_uses_if_virtio_shorthand(settings):
    """类别 5 补充：disk_serial 为空时用 if=virtio 简写，不引入设备拆分的
    复杂度（没有 by-id 需求就不需要显式 serial）。"""
    args = _target_args(
        entry_id="e-005b",
        firmware="bios",
        mem="4096",
        disk_serial="",
        settings=settings,
    )

    assert any("if=virtio" in a for a in args), f"应用 if=virtio 简写：{args}"
    assert not any("virtio-blk-pci" in a for a in args), (
        f"disk_serial 为空时不应出现独立的 virtio-blk-pci 设备：{args}"
    )


# ----------------------------------------------------------------------
# 类别 6：路径绝对化。
# ----------------------------------------------------------------------


def test_winpath_makes_relative_path_absolute():
    """类别 6：相对路径原样透给 QEMU 会以 QEMU 的 cwd 解析，不是调用方
    期望的目录——winpath 必须强制绝对化。"""
    result = winpath("relative/x")

    assert Path(result).is_absolute(), f"winpath 应产出绝对路径，实际：{result}"


def test_winpath_absolute_input_stays_absolute():
    result = winpath("D:/already/absolute/x")

    assert Path(result).is_absolute()


# ----------------------------------------------------------------------
# 类别 7：串口日志按 mtime 归档，不覆盖。
# ----------------------------------------------------------------------


def test_serial_log_path_archives_existing_log_by_mtime_without_deleting(settings):
    """类别 7（护栏）：约束 6。旧日志存在时先归档到 archived_log_path，
    不删除——装机失败常要跨轮次对比，旧 harness 有过一次因覆盖丢失
    anaconda 错误输出的教训。"""
    old_log = serial_log_path("e-007", settings=settings)
    old_log.parent.mkdir(parents=True, exist_ok=True)
    old_log.write_text("上一轮的 anaconda 错误输出")

    # 调用两次：第二次调用时上一次产出的路径已经"存在"，应触发归档。
    new_path = serial_log_path("e-007", settings=settings)

    assert new_path == old_log, "serial_log_path 本身应保持固定名字（供当前轮写入）"
    assert not old_log.exists() or old_log.read_text() != "上一轮的 anaconda 错误输出", (
        "旧内容应已被归档挪走，固定路径不应再持有旧内容"
    )

    # 旧内容必须能在某个归档路径下找到，而不是被删除。
    archived_files = list(old_log.parent.glob("e-007-*"))
    archived_contents = [f.read_text() for f in archived_files if f != old_log]
    assert "上一轮的 anaconda 错误输出" in archived_contents, (
        f"旧日志内容应被归档而非删除，归档目录下的文件：{list(old_log.parent.iterdir())}"
    )


def test_archived_log_path_uses_stamp_in_filename(settings):
    path = archived_log_path("e-007b", "0926-120000", settings=settings)

    assert "e-007b" in path.name
    assert "0926-120000" in path.name


# ----------------------------------------------------------------------
# 类别 8：stop_target 幂等，需要真实进程。
# ----------------------------------------------------------------------


@pytest.mark.needs_vm
def test_stop_target_on_already_dead_process_does_not_raise(settings):
    """类别 8：进程已经退出时 stop_target 不应抛异常——内存约束下一条目
    才能起，调用方会在「不确定是否已停」的情况下重复调用。"""
    # 用一个真实、短命的子进程模拟"已经死掉的目标机"，不依赖真实 QEMU 起得来。
    proc = subprocess.Popen(
        ["python", "-c", "pass"],
        stdout=subprocess.DEVNULL,
        stderr=subprocess.DEVNULL,
    )
    proc.wait(timeout=10)  # 确保已退出
    time.sleep(0.2)

    stop_target("e-008", proc=proc, settings=settings)  # 不应抛


# ----------------------------------------------------------------------
# _server_args：VM-S 自身的参数拼装（复用同一套 accel/cpu 约束）。
# ----------------------------------------------------------------------


def test_server_args_uses_whpx_and_cpu_qemu64(settings):
    """VM-S 与目标机共享同一条 WHPX/cpu 约束（约束 1 对两者都成立，
    start-vm.sh 的既有实践也是 qemu64）。"""
    args = _server_args(settings=settings)

    assert "-cpu" in args
    assert args[args.index("-cpu") + 1] == "qemu64"
    assert "-accel" in args
    assert args[args.index("-accel") + 1] == "whpx,kernel-irqchip=off"


def test_kill_stale_targets_returns_list_of_ints():
    """kill_stale_targets 的返回类型契约：按 -name 清理，返回被杀的 PID
    列表。本机当前没有残留目标机进程时应返回空列表而不是抛异常。"""
    pids = kill_stale_targets()

    assert isinstance(pids, list)
    assert all(isinstance(p, int) for p in pids)
