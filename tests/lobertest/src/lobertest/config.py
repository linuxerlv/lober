"""环境事实的唯一来源。

叶子模块，不 import 工程内任何其他模块。VM-S 地址、QEMU 路径、schema 名都
在这里定死默认值；环境变量覆盖只用于真实会变的东西（换一台 VM 跑）。
"""

from __future__ import annotations

import os
from dataclasses import dataclass
from pathlib import Path
from typing import Mapping

_ENV_PREFIX = "LOBERTEST_"


@dataclass(frozen=True)
class Settings:
    """本轮测试跑动的全部环境事实。

    session 开始时由 load() 定死一份，用例不得修改（frozen=True）。
    """

    ssh_host: str = "10.20.20.1"
    ssh_port: int = 2222
    ssh_user: str = "debian"
    ssh_key_path: Path = Path("D:/qemu-vm/debian-cloud/id_ed25519")

    console_base_url: str = "https://console.vmlab.internal/console/v1"

    pxe_subnet: str = "10.20.20.0/24"
    tftp_root: str = "/srv/tftp"

    qemu_bin: Path = Path("D:/msys/ucrt64/bin/qemu-system-x86_64.exe")
    ovmf_code: Path = Path("D:/msys/ucrt64/share/qemu/edk2-x86_64-code.fd")
    iso_dir: Path = Path("D:/qemu-vm/isos")

    # 目标机（待装机的裸机）磁盘与串口日志的落盘目录。与 VM-S 自身
    # （D:/qemu-vm/debian-cloud）分开：目标机每条目一块盘、每轮一份日志，
    # 数量会积累，不该跟 VM-S 的固定文件混在一起。
    targets_dir: Path = Path("D:/qemu-vm/vmlab-targets")

    schema: str = "lobertest"


# 五个角色的测试账号凭据——唯一权威来源（任务 6）。`api.py`（登录）与
# `deploy.py`（生成账号、写进 lober.yaml）都从这里 import，不再各自内联
# 字面量。两边一旦各自记一份、又漂移出不同的值，症状是全部 L1 用例 401，
# 且两处代码各自看起来都对，这种错误极难定位——这正是本模块存在的理由。
#
# TEST_USER_PREFIX 与 TEST_PASSWORD 是叶子常量（本模块不因此引入任何新
# import，仍然只依赖标准库），不违反「叶子模块不 import 工程内其它模块」
# 的约束；`api.py` 与 `deploy.py` 反过来 import 本模块是设计允许的方向
# （config 是叶子，两个同级模块都可以 import 它）。
TEST_USER_PREFIX = "lobertest-"

# 明文密码：scripts/mkuser 要求至少 12 个字符（main.go 的 minPasswordLen），
# 这里选的值远超这个下限，避免账号生成本身先撞到强度校验失败。
TEST_PASSWORD = "LoberTest#2026Automation"

# 五个角色名，取值与顺序对应 internal/console/rbac.go:19-24 的角色表
# （viewer/operator/hook-author/hook-approver/admin）。本模块不重新定义
# 角色语义——rbac.go 仍是权威定义处，这里只是把同一份取值抄成 Python 侧
# 可 import 的常量，供 deploy.py 生成账号时遍历。
TEST_ROLES = ("viewer", "operator", "hook-author", "hook-approver", "admin")


# 字段名 -> 转换函数。转换失败时要报清楚是哪个字段坏了，不能让 int()/Path()
# 的裸异常直接冒出去（那种消息不带字段名，定位不到问题出在哪个环境变量）。
_CONVERTERS = {
    "ssh_port": int,
    "ssh_key_path": Path,
    "qemu_bin": Path,
    "ovmf_code": Path,
    "iso_dir": Path,
    "targets_dir": Path,
}


def load(env: Mapping[str, str] | None = None) -> Settings:
    """从环境变量加载配置，覆盖 Settings 的默认值。

    `env=None` 时读 `os.environ`；显式传入 `env`（哪怕是空字典）是为了让
    测试可以在不碰真实环境变量的情况下验证覆盖逻辑。

    每个字段的环境变量名是 `LOBERTEST_<字段大写>`，例如 `ssh_host` 对应
    `LOBERTEST_SSH_HOST`。
    """
    if env is None:
        env = os.environ

    overrides: dict[str, object] = {}
    for field_name in Settings.__dataclass_fields__:
        env_key = f"{_ENV_PREFIX}{field_name.upper()}"
        if env_key not in env:
            continue

        raw_value = env[env_key]
        converter = _CONVERTERS.get(field_name)
        if converter is None:
            overrides[field_name] = raw_value
            continue

        try:
            overrides[field_name] = converter(raw_value)
        except (TypeError, ValueError) as exc:
            raise ValueError(
                f"环境变量 {env_key} 的值 {raw_value!r} 不能转换成字段 "
                f"{field_name} 所需的类型：{exc}"
            ) from exc

    return Settings(**overrides)
