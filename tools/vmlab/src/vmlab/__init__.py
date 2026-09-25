"""lober VM 实验床的自动化测试床。详见 README.md。"""

from vmlab.api import APIError, ConsoleAPI
from vmlab.config import SETTINGS
from vmlab.qemu import TargetVM, kill_target_vms, running_vms
from vmlab.runner import (
    FIRSTBOOT_SIGNALS,
    INSTALL_SIGNALS,
    DISK_SELECT_HOOK,
    InstallResult,
    InstallRunner,
)
from vmlab.server import VMServer

__all__ = [
    "APIError",
    "ConsoleAPI",
    "DISK_SELECT_HOOK",
    "FIRSTBOOT_SIGNALS",
    "INSTALL_SIGNALS",
    "InstallResult",
    "InstallRunner",
    "SETTINGS",
    "TargetVM",
    "VMServer",
    "kill_target_vms",
    "running_vms",
]
