"""config.load() 的四类输入：全默认、环境变量覆盖、非法端口、frozen 生效。"""

from __future__ import annotations

from dataclasses import FrozenInstanceError
from pathlib import Path

import pytest

from lobertest.config import Settings, load


def test_load_with_no_env_uses_defaults():
    """全默认：env=None 时不传任何覆盖，每个字段都有默认值。"""
    settings = load(env={})

    assert isinstance(settings, Settings)
    assert isinstance(settings.ssh_port, int)
    assert isinstance(settings.ssh_key_path, Path)
    assert isinstance(settings.qemu_bin, Path)
    assert isinstance(settings.ovmf_code, Path)
    assert isinstance(settings.iso_dir, Path)
    # 字符串字段不能是空默认值——空字符串会让「用了默认值」和「忘了填」无法区分。
    assert settings.ssh_host
    assert settings.ssh_user
    assert settings.console_base_url
    assert settings.pxe_subnet
    assert settings.tftp_root
    assert settings.schema


def test_load_env_override_single_field():
    """环境变量覆盖单个字段，其余字段维持默认值。"""
    default_settings = load(env={})
    overridden = load(env={"LOBERTEST_SSH_HOST": "10.20.20.99"})

    assert overridden.ssh_host == "10.20.20.99"
    # 其余字段不受影响。
    assert overridden.ssh_port == default_settings.ssh_port
    assert overridden.ssh_user == default_settings.ssh_user
    assert overridden.console_base_url == default_settings.console_base_url


def test_load_invalid_ssh_port_raises_clear_error_not_bare_valueerror():
    """ssh_port 给非数字字符串时要报清楚是哪个字段坏了，不是裸抛 ValueError。"""
    with pytest.raises(ValueError, match="ssh_port"):
        load(env={"LOBERTEST_SSH_PORT": "not-a-number"})


def test_settings_is_frozen():
    """Settings 是 frozen dataclass：session 开始后用例不得改配置。"""
    settings = load(env={})

    with pytest.raises(FrozenInstanceError):
        settings.ssh_host = "changed"  # type: ignore[misc]
