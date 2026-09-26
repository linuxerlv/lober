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

    # ssh_host/ssh_port（provision 任务实测订正，原默认值 10.20.20.1:2222
    # 从未真正可达）：D:/qemu-vm/debian-cloud/start-vm.sh 起 VM-S 时只挂了
    # 一张网卡（QEMU SLIRP，`-netdev user,...`），guest 侧固定地址是
    # SLIRP 网段的 10.10.10.10，SLIRP 把 Windows 宿主的
    # 127.0.0.1:2222 hostfwd 到 guest 的 10.10.10.10:22。10.20.20.1 是
    # *装机 VLAN*（一张独立的 socket 网卡）那一侧的地址——这台 VM-S 从未
    # 配过第二张网卡，10.20.20.1 在这台机器上不对应任何真实接口，从
    # Windows 宿主也永远连不上（不是 SLIRP 转发得到的地址）。起真实目标机、
    # 配第二张网卡是任务 9（L2 装机层）的范围，与本任务（只搭 VM-S）无关。
    ssh_host: str = "127.0.0.1"
    ssh_port: int = 2222
    ssh_user: str = "debian"
    ssh_key_path: Path = Path("D:/qemu-vm/debian-cloud/id_ed25519")

    # console_base_url（同上一并订正）：原默认值 https://console.vmlab.internal
    # 同时错在两处——(1) scheme 是 HTTPS，但 vmlab 时代签的证书把人面
    # listen 写成了 guest 内部回环 127.0.0.1:443，QEMU 的 SLIRP hostfwd
    # 没法转发到 guest 自己的 loopback，这个地址从未真正可达；(2)
    # console.vmlab.internal 这个域名在 Windows 宿主没有任何 DNS 记录或
    # hosts 映射，即使地址可达也解析不出去。
    #
    # 改为明文 HTTP + IP:port 直连，不恢复 HTTPS + 域名——这是 provision
    # 阶段的刻意简化，不是把生产设计判断反过来：nginx 生产模板要求人面
    # listen 具体 IP、机器面永久明文这两条约束本身没变，变的只是「这台
    # 单网卡测试 VM 上人面具体 IP 是多少、要不要在测试床里额外实践 HTTPS
    # 这项可选加固」。测试客户端（Python requests）验证自签 CA 证书链
    # 会引入额外的信任步骤，对一次性测试 VM 收益不成比例。
    #
    # 8090 端口与具体 IP 的选择原因见 tests/lobertest/provision/
    # nginx-lober.conf 文件头注释（该文件是这个决定的落地产物，本字段与
    # 它必须保持一致；provision.md 记录了对应的 QEMU hostfwd）。
    console_base_url: str = "http://127.0.0.1:8090/console/v1"

    pxe_subnet: str = "10.20.20.0/24"
    tftp_root: str = "/srv/tftp"

    qemu_bin: Path = Path("D:/msys/ucrt64/bin/qemu-system-x86_64.exe")
    ovmf_code: Path = Path("D:/msys/ucrt64/share/qemu/edk2-x86_64-code.fd")

    # x86_64 的 UEFI vars 模板。这个 MSYS 包没有 edk2-x86_64-vars.fd，但
    # 这不是缺件——share/qemu/firmware/60-edk2-x86_64.json 明确声明
    # nvram-template 就是 i386 那份（varstore 格式与架构无关，QEMU 只发
    # 一份）。不能自造：模板开头（偏移 0x28）有 _FVH 固件卷签名，全零文件
    # 缺这个头会被 OVMF 判成损坏的 NVRAM。
    ovmf_vars_template: Path = Path("D:/msys/ucrt64/share/qemu/edk2-i386-vars.fd")

    iso_dir: Path = Path("D:/qemu-vm/isos")

    # 目标机（待装机的裸机）磁盘与串口日志的落盘目录。与 VM-S 自身
    # （D:/qemu-vm/debian-cloud）分开：目标机每条目一块盘、每轮一份日志，
    # 数量会积累，不该跟 VM-S 的固定文件混在一起。
    targets_dir: Path = Path("D:/qemu-vm/vmlab-targets")

    # 目标机 root 账户的登录私钥，对应 TEST_ROOT_SSH_PUBLIC_KEY（见下方那段
    # 说明）。放仓库外：私钥不入库。任务 9 用它做 L2 的验收证据之二
    # （SSH 登进新装的机器）。
    target_root_key_path: Path = Path("D:/qemu-vm/lobertest-keys/lobertest-target-root_ed25519")

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

# VM-S 上 lober_test 这个 Postgres 角色的密码（任务 5/8 实测暴露的问题，
# provision 阶段补的常量）。原 render_config 写的是空字符串，在这台 VM-S
# 的 pg_hba.conf 下无法通过——TCP 连接（host all all 127.0.0.1/32）的认证
# 方式是 scram-sha-256，不是 trust/peer，空密码在 fe_sendauth 阶段就被拒绝
# （`fe_sendauth: no password supplied`）。lober 进程本身走 TCP 连
# 127.0.0.1:5432（store/postgres 的 host 字段），不是 unix socket，因此吃不到
# `local ... peer` 那两条本机免密规则——那两条只对 unix socket 连接生效。
#
# 与 TEST_PASSWORD 同一性质：唯一权威来源在这里，deploy.py 的
# render_config 与 provision 脚本/文档都从这里取值，不各自内联字面量，
# 避免两处漂移出不同密码（症状会是「lober -check 一直 rc!=0，日志里是
# password authentication failed，但看两处配置字面上都『对』」）。
TEST_PG_PASSWORD = "lobertest_pw"

# 要注入每台新装机器 root 账户的 SSH 公钥（provision 任务实测暴露：
# render_config 原先产出 `root_ssh_public_keys: []`，而
# internal/config/config.go:626 的 Validate 明确拒绝空列表——"空列表会
# 静默产出一台装完后没人能登录的机器"，这条校验是启动时硬性拒绝，不是
# 静默降级，`lober -check` 会直接非零退出，wait_ready 的判据 1 因此永远
# 过不去）。
#
# 这是一把**专门生成的、与 VM-S 自身登录无关的一次性测试密钥**，不是
# VM-S 的 D:/qemu-vm/debian-cloud/id_ed25519——两者语义完全不同：后者是
# Windows 宿主登录 VM-S 本身用的凭据，前者是配置里声明"装完的目标机
# root 账户应该信任哪把公钥"。任务 9 会用对应私钥去登录装好的机器，
# 那是 L2 的验收证据之二。
#
# **私钥不入库**，放在仓库外的 D:/qemu-vm/lobertest-keys/ 下，由
# target_root_key_path 按路径引用——与 ssh_key_path 指向 VM-S 自身密钥
# 是同一个惯例。公钥可以内联（公钥本来就是公开的）。
#
# 为什么私钥不能提交：这把公钥会被注入为**每一台**经本测试床装出来的机器
# 的 root 授权。lober 本身就是裸机装机系统，测试配置有变成生产配置的倾向；
# 私钥一旦进 git 历史极难清除，而这个做法会被后来者照抄。「这只是临时测试
# 机」不是把私钥写进版本库的理由——放仓库外一样能用，代价是零。
TEST_ROOT_SSH_PUBLIC_KEY = (
    "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIFNkfSmdKbizb6+tWWRwGDCAxi0FB0UfpG8dVmOnB7aS "
    "lobertest-target-root"
)

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
    "ovmf_vars_template": Path,
    "iso_dir": Path,
    "targets_dir": Path,
    "target_root_key_path": Path,
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
