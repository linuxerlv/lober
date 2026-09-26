"""共享 fixture。

用 module 级 fixture 而不是往 pytest 模块上挂属性：后者在 `-k` 过滤掉前置用例
时会直接 AttributeError，而装机用例恰恰经常要单独点名跑（一次半小时，不想每次
都从导入 ISO 开始）。fixture 会按依赖自动补上前置。
"""

from __future__ import annotations

import hashlib

import pytest

from vmlab import DISK_SELECT_HOOK, ConsoleAPI, VMServer

ISO_DEVICE = "/dev/vdb"
HOST_SN = "VMLAB-T01"
HOST_MAC = "52:54:00:30:30:01"
PACKAGES = "%packages\n@^minimal-environment\nopenssh-server\n%end\n"


@pytest.fixture(scope="session")
def server():
    with VMServer() as s:
        yield s


@pytest.fixture(scope="session")
def api(server):
    return ConsoleAPI(server=server)


@pytest.fixture(scope="session")
def rhel_artifact(api) -> dict:
    """导入 OpenCloudOS 9.6，复用已 ready 的同名制品集。

    复用而不是每次重导：一次导入要复制 1.6G（实测 30-40 秒），而制品集内容
    由 ISO 决定、不随代码变。若 lober 改动影响了导入产物，改名重跑即可。
    """
    name = "opencloudos-9.6-vmlab-v2"
    for a in api.call("GET", "/artifact-sets"):
        if a["name"] == name and a["state"] == "ready":
            return a
    artifact_id = api.import_artifact(ISO_DEVICE, name)
    return api.wait_artifact_ready(artifact_id, timeout=900)


@pytest.fixture(scope="session")
def disk_hook(api) -> dict:
    """选盘 hook，推到 active。

    **按 digest 复用而不是按名字**：digest 是正文的 sha256，内容一改它就变。
    按名字找会拿到内容已过期的旧版本——实测踩过一次：给 hook 加了 biosboot 分区
    之后，fixture 仍然复用着旧的那个（has_biosboot=False），于是装机继续以同一个
    原因失败，而代码里明明已经改好了。
    """
    want_digest = hashlib.sha256(DISK_SELECT_HOOK.encode("utf-8")).hexdigest()
    for h in api.call("GET", "/hooks"):
        if h["bodySha256"] == want_digest and h["state"] == "active":
            return h
    hook = api.create_hook(
        name="vmlab-diskselect",
        stage="pre",
        body=DISK_SELECT_HOOK,
        os_families=["rhel"],
    )
    return api.activate_hook(hook["id"])


@pytest.fixture(scope="session")
def rhel_template(api, rhel_artifact, disk_hook) -> dict:
    """RHEL 模板，推到 active。

    每次新建而不复用：模板要 pin 住制品集与 hook digest（SPEC §2.6 决策固化），
    而这两者在测试会话之间可能变。新建一个是几十毫秒的事。
    """
    tpl = api.create_template(
        name="vmlab-rhel-minimal",
        artifact_set_id=rhel_artifact["id"],
        answer_template=PACKAGES,
        hook_refs=[{"hookId": disk_hook["id"], "stage": "pre", "order": 10, "required": True}],
        disk_policy="by-hook",
    )
    return api.activate_template(tpl)


@pytest.fixture(scope="session")
def rhel_host(api) -> str:
    return api.ensure_host(HOST_SN, HOST_MAC, "x86_64", "bios")
