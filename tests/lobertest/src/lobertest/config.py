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

    schema: str = "lobertest"


# 字段名 -> 转换函数。转换失败时要报清楚是哪个字段坏了，不能让 int()/Path()
# 的裸异常直接冒出去（那种消息不带字段名，定位不到问题出在哪个环境变量）。
_CONVERTERS = {
    "ssh_port": int,
    "ssh_key_path": Path,
    "qemu_bin": Path,
    "ovmf_code": Path,
    "iso_dir": Path,
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
