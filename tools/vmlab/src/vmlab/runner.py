"""装机编排：把 API、QEMU、日志三层串成一次可断言的完整装机。

一次装机的完整链路（每一步都在真实路径上，无任何绕过）：

    导入 ISO（imports.Job.Run）→ 建 hook → 两段审批 → 建模板 → 激活模板
    → 建任务 → 起目标机 → iPXE 取菜单 → 下载 kernel/initrd → anaconda 启动
    → 取 kickstart → %pre 选盘 → 装包 → %post → 重启 → firstboot → done
"""

from __future__ import annotations

import time
from dataclasses import dataclass, field
from typing import Callable

from vmlab.api import APIError, ConsoleAPI
from vmlab.qemu import TargetVM, kill_target_vms, running_vms
from vmlab.server import VMServer

# 装机期七个信号，按 model.signalTable 的 Seq 升序。状态机对 Seq 不大于当前
# 水位的信号直接丢弃（§3.6 只进不退），所以顺序本身也是断言对象。
INSTALL_SIGNALS = [
    "pre-start",
    "pre-done",
    "packages-start",
    "packages-done",
    "post-start",
    "post-done",
    "rebooting",
]

# firstboot 阶段四个（重启之后由 systemd unit 发出）。
FIRSTBOOT_SIGNALS = ["booted", "init-start", "init-done", "ready"]


@dataclass
class Progress:
    """一次轮询的快照。"""

    elapsed: int
    state: str
    phase: str
    signals: list[str]
    fail_detail: dict | None = None
    disk_bytes: int = 0

    def line(self) -> str:
        sigs = " ".join(self.signals[-6:]) if self.signals else "-"
        tail = ""
        if self.fail_detail:
            tail = " | " + str(self.fail_detail.get("logTail", ""))[:90]
        return (
            f"[{self.elapsed:5d}s] {self.state:11s} {self.phase or '-':13s} "
            f"disk={self.disk_bytes // 1024 // 1024:5d}MB | {sigs}{tail}"
        )


@dataclass
class InstallResult:
    """一次装机的最终结果。"""

    task_id: str
    state: str
    phase: str
    signals: list[str]
    elapsed: int
    fail_detail: dict | None
    disk_bytes: int
    console_tail: str

    def ok(self) -> bool:
        return self.state == "done"

    def missing_signals(self, expected: list[str]) -> list[str]:
        return [s for s in expected if s not in self.signals]

    def summary(self) -> str:
        lines = [
            f"任务 {self.task_id[:8]}  state={self.state}  phase={self.phase}  "
            f"耗时 {self.elapsed}s  盘写入 {self.disk_bytes // 1024 // 1024}MB",
            f"信号（{len(self.signals)} 个）: {' '.join(self.signals) or '无'}",
        ]
        missing = self.missing_signals(INSTALL_SIGNALS)
        if missing:
            lines.append(f"装机期缺失信号: {' '.join(missing)}")
        if self.fail_detail:
            lines.append(f"失败详情: {self.fail_detail}")
        return "\n".join(lines)


@dataclass
class InstallRunner:
    """驱动一次完整装机。"""

    server: VMServer
    api: ConsoleAPI
    vm: TargetVM = field(default_factory=TargetVM)
    budget_sec: int = 2400
    poll_sec: int = 30
    on_progress: Callable[[Progress], None] | None = None

    def run(self, host_sn: str, template_id: str, form_data: dict | None = None) -> InstallResult:
        """清残留 → 建任务 → 起机 → 轮询到终态。

        顺序有讲究：**必须先确认残留 VM 真的退出了，再建任务**。残留的目标机
        在被杀掉之前可能再发一次 /v1/boot，那会消耗掉新任务的 pending 状态
        （FindPendingTaskByIdentity 匹配 pending 与 booting 两种），于是真正的
        目标机起来时拿到 no_task（实测 2026-09-26 踩过一次）。
        """
        # 清残留目标机：它们占着 VM-S 的 socket listen 端口，新目标机会拿不到
        # 网络（iPXE 报 No configuration methods succeeded）；也可能抢走新任务。
        kill_target_vms()
        for _ in range(20):
            if self.vm.name not in running_vms():
                break
            time.sleep(1)
        else:
            raise RuntimeError(
                f"目标机 {self.vm.name} 在 20 秒内没有退出——它会占着 socket "
                "端口并可能抢走新任务，不能继续"
            )

        self.api.cancel_active_tasks()
        task_id = self.api.create_task(host_sn, template_id, form_data or {"hostname": "vmlab-t01"})

        self.vm.reset_disk()
        self.vm.start()

        return self._poll(task_id)

    def _poll(self, task_id: str) -> InstallResult:
        start = time.time()
        last_line = ""
        snap = Progress(0, "?", "?", [])

        while True:
            time.sleep(self.poll_sec)
            elapsed = int(time.time() - start)

            try:
                task = self.api.task(task_id)
            except APIError:
                # 服务重启或网络抖动，下一轮再试——不能因为一次查询失败就判
                # 装机失败。
                continue

            snap = Progress(
                elapsed=elapsed,
                state=task["state"],
                phase=task.get("currentPhase") or "",
                signals=self.server.signals_for(task_id),
                fail_detail=task.get("failureDetail"),
                disk_bytes=self.vm.disk_bytes(),
            )

            line = snap.line()
            if line[8:] != last_line[8:]:  # 跳过时间戳部分比较，只在有变化时打印
                print(line, flush=True)
                if self.on_progress:
                    self.on_progress(snap)
                last_line = line

            if snap.state in ("done", "failed"):
                break
            if elapsed >= self.budget_sec:
                print(f"[{elapsed:5d}s] 超出预算 {self.budget_sec}s，停止等待", flush=True)
                break

        return InstallResult(
            task_id=task_id,
            state=snap.state,
            phase=snap.phase,
            signals=snap.signals,
            elapsed=snap.elapsed,
            fail_detail=snap.fail_detail,
            disk_bytes=snap.disk_bytes,
            console_tail=self.vm.console_tail(),
        )


# --- 常用 hook 正文 ------------------------------------------------------

# BIOS + GPT 必须有 1MiB 的 biosboot 分区（GRUB 的 core.img 放那里，MBR 的
# 446 字节装不下、GPT 又没有 MBR 后那段空隙）。缺了它 anaconda 的存储校验
# 直接失败并挂在安装摘要屏等人处理，而服务端侧只看到「packages 阶段超时」
# ——本轮实测花了六轮才定位到这条。UEFI 目标机要改成 /boot/efi 的 ESP。
#
# by-hook 磁盘策略下，%pre 阶段的 hook 必须写出这个文件，主体 kickstart 用
# %include 引入它。放在 /tmp 下是因为 %pre 运行在 installer 的 ramdisk 里，
# 那时目标盘还没分区、没挂载。
DISK_SELECT_HOOK = r"""#!/bin/bash
# lober 实验床 pre hook：选盘并写出分区指令（磁盘策略 by-hook）。
set -euo pipefail

INCLUDE=/tmp/lober-part-include

# 选最大的一块非只读、非可移动块设备。跳过 loop/ram/sr/dm/md 这些不是物理盘
# 的设备。
pick_disk() {
  local best="" best_size=0 name size
  for dev in /sys/block/*; do
    name=$(basename "$dev")
    case "$name" in loop*|ram*|sr*|fd*|dm-*|md*) continue ;; esac
    if [ -f "$dev/removable" ] && [ "$(cat "$dev/removable")" = "1" ]; then continue; fi
    if [ -f "$dev/ro" ] && [ "$(cat "$dev/ro")" = "1" ]; then continue; fi
    size=$(cat "$dev/size" 2>/dev/null || echo 0)
    if [ "$size" -gt "$best_size" ]; then best="$name"; best_size="$size"; fi
  done
  [ -n "$best" ] || return 1
  # 优先用 by-id 持久名：内核名（/dev/sda）在不同启动之间会漂移，写进分区
  # 指令可能擦错盘（SPEC §10 失败模式 5）。QEMU virtio 盘可能没有 by-id
  # 链接，那时退回内核名并在日志里明确记录。
  local byid
  for byid in /dev/disk/by-id/*; do
    [ -e "$byid" ] || continue
    case "$byid" in *-part[0-9]*) continue ;; esac
    if [ "$(readlink -f "$byid")" = "/dev/$best" ]; then echo "$byid"; return 0; fi
  done
  echo "[lober] 警告: /dev/$best 没有 by-id 链接，退回内核名" >&2
  echo "/dev/$best"
}

DISK=$(pick_disk) || { echo "[lober] 找不到任何可用磁盘" >&2; exit 1; }
echo "[lober] 选中磁盘: $DISK"

cat > "$INCLUDE" <<EOF
clearpart --all --initlabel --drives=$DISK
part biosboot --fstype=biosboot --size=1 --ondisk=$DISK
part /boot --fstype=xfs --size=1024 --ondisk=$DISK
part / --fstype=xfs --grow --ondisk=$DISK
EOF

echo "[lober] 已写出 $INCLUDE:"
cat "$INCLUDE"
"""
