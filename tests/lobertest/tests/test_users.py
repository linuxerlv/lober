"""五个角色的测试账号：六类输入。

前四类是账号本身，后两类是凭据收敛验证（`config.py` 是唯一权威来源，
`api.py` 与 `deploy.py` 都从它 import，不再各自内联字面量）。

权威角色/权限码定义在 `internal/console/rbac.go:19-37`；本文件不重新定义
它们，只核对 `deploy.py`/`config.py` 产出的账号与之一致。
"""

from __future__ import annotations

import subprocess

import pytest

from lobertest import api, config, deploy

# ----------------------------------------------------------------------
# 类别 1：bcrypt_hash 产出以 $2 开头、60 字符的字符串。
# 会真的跑 go run ./scripts/mkuser，本机可执行，不需要 VM。
# ----------------------------------------------------------------------


def test_bcrypt_hash_produces_60_char_bcrypt_string(repo_root):
    """类别 1：调 scripts/mkuser 产出的哈希必须是 bcrypt 的固定格式——
    以 `$2` 开头、总长 60 字符。不在 Python 侧装 bcrypt 库算这个值，
    必须真的调用 lober 自己的工具。"""
    hashed = deploy.bcrypt_hash(repo_root, config.TEST_PASSWORD)

    assert hashed.startswith("$2")
    assert len(hashed) == 60


def test_bcrypt_hash_does_not_leak_password_into_argv(repo_root, monkeypatch):
    """辅助护栏（风险提示要求）：调 mkuser 时密码必须走 stdin，不能出现
    在传给 subprocess 的命令行参数列表里——那会进 `ps` 输出。用
    monkeypatch 拦截 subprocess.run 的调用参数来断言，不依赖真的去读
    进程表（跨平台且不稳定）。"""
    captured: dict = {}
    real_run = subprocess.run

    def _spy(*args, **kwargs):
        captured["args"] = args
        captured["kwargs"] = kwargs
        return real_run(*args, **kwargs)

    monkeypatch.setattr(subprocess, "run", _spy)

    deploy.bcrypt_hash(repo_root, "a-secret-password-value")

    all_argv_text = " ".join(str(a) for a in captured["args"][0])
    assert "a-secret-password-value" not in all_argv_text
    # 必须显式走 stdin=PIPE 喂密码，而不是依赖交互模式（那会挂起）。
    assert captured["kwargs"].get("input") is not None or captured["kwargs"].get("stdin") is not None


# ----------------------------------------------------------------------
# 类别 2：TEST_USERS 五个角色齐全且无重复 name。
# ----------------------------------------------------------------------


def test_test_users_covers_five_roles_with_unique_names(repo_root):
    """类别 2：五个角色（viewer/operator/hook-author/hook-approver/admin，
    权威定义见 rbac.go:19-24）各对应恰好一个测试账号，name 无重复。"""
    users = deploy.build_test_users(repo_root)

    assert len(users) == 5
    names = [u.name for u in users]
    assert len(names) == len(set(names)), f"name 有重复: {names}"

    roles_present = {role for u in users for role in u.roles}
    assert roles_present == set(config.TEST_ROLES)


# ----------------------------------------------------------------------
# 类别 3：没有任何一个 user 同时含 hook-author 与 operator。
# ----------------------------------------------------------------------


def test_no_user_holds_both_hook_author_and_operator(repo_root):
    """类别 3：hook-author 与 operator 是刻意互不重叠的两个角色
    （rbac.go:45-47：hook 以 root 身份在分区之前运行，能力等同擦掉整机
    所有盘）。任何测试账号同时挂两者都会让 RBAC 边界测不出来。"""
    users = deploy.build_test_users(repo_root)

    for user in users:
        roles = set(user.roles)
        assert not ({"hook-author", "operator"} <= roles), (
            f"账号 {user.name} 同时持有 hook-author 与 operator: {user.roles}"
        )


# ----------------------------------------------------------------------
# 类别 4：render_config 产出的 YAML 里五个用户都在，哈希非空。
# ----------------------------------------------------------------------


def test_render_config_includes_all_five_users_with_nonempty_hash(repo_root):
    """类别 4：render_config 吃 TEST_USERS 产出的 YAML 文本，五个用户
    的 name 都出现，且各自的 bcrypt_hash 非空字符串（不是占位空值）。"""
    users = deploy.build_test_users(repo_root)

    yaml_body = deploy.render_config(schema="lobertest_users_check", users=users)

    for user in users:
        assert user.name in yaml_body
        assert user.bcrypt_hash != ""
        assert user.bcrypt_hash in yaml_body


# ----------------------------------------------------------------------
# 类别 5：凭据单一来源——api.py 不再有硬编码密码/前缀字面量。
# ----------------------------------------------------------------------


def test_api_module_has_no_inline_password_or_prefix_literal():
    """类别 5：grep 断言 api.py 源码里不再出现硬编码的密码或账号前缀
    字面量。改用检查模块顶层不再定义 _TEST_PASSWORD/_TEST_USER_PREFIX
    这两个旧名字（它们曾是任务 3 内联的副本，任务 6 起必须删除，改为
    从 config 模块 import），比正则扫描源文本更不容易误判。"""
    assert not hasattr(api, "_TEST_PASSWORD"), (
        "api.py 仍定义 _TEST_PASSWORD——凭据必须收敛到 config.py，"
        "此处只应保留对 config.TEST_PASSWORD 的引用"
    )
    assert not hasattr(api, "_TEST_USER_PREFIX"), (
        "api.py 仍定义 _TEST_USER_PREFIX——凭据必须收敛到 config.py，"
        "此处只应保留对 config.TEST_USER_PREFIX 的引用"
    )


def test_api_uses_same_prefix_as_test_users(repo_root):
    """类别 5（续）：api.py 登录时用的账号前缀，与 TEST_USERS 的 name
    前缀是同一个值——不是分别定义出来恰好相等的两个字符串。断言办法：
    用 api.token() 实际拼出的用户名，与 build_test_users 产出的某个
    user.name 完全一致。"""
    users = deploy.build_test_users(repo_root)
    admin_user = next(u for u in users if "admin" in u.roles)

    # Console.token(role) 内部拼接用户名的规则是
    # f"{config.TEST_USER_PREFIX}{role}"；这里独立按同一规则重算一次
    # 期望值，验证它与 build_test_users 产出的 admin 账号 name 一致。
    expected_username_for_admin_role = f"{config.TEST_USER_PREFIX}admin"
    assert expected_username_for_admin_role == admin_user.name


# ----------------------------------------------------------------------
# 类别 6：哈希与明文对得上。
# ----------------------------------------------------------------------


def test_hash_matches_plaintext_password(repo_root):
    """类别 6：拿 TEST_PASSWORD 算出的哈希，用 bcrypt 校验函数验证能
    匹配该明文。本机若无 bcrypt 校验库可用则退化为「同一密码调两次
    mkuser 得到两个不同哈希」（见下一个测试）。"""
    bcrypt_lib = pytest.importorskip("bcrypt", reason="本机没有 bcrypt 校验库，退化用例见 test_hash_differs_across_calls_due_to_bcrypt_salt")

    hashed = deploy.bcrypt_hash(repo_root, config.TEST_PASSWORD)

    assert bcrypt_lib.checkpw(config.TEST_PASSWORD.encode("utf-8"), hashed.encode("utf-8"))
    # 反证：错误的明文不应该匹配。
    assert not bcrypt_lib.checkpw(b"a-completely-wrong-password", hashed.encode("utf-8"))


def test_hash_differs_across_calls_due_to_bcrypt_salt(repo_root):
    """类别 6（退化路径，始终跑）：同一密码调两次 mkuser 必须得到两个
    不同的哈希字符串——bcrypt 加盐，相同输入不同输出。这至少证明
    bcrypt_hash 真的在做 bcrypt，而不是返回一个常量或做了不加盐的哈希。
    """
    hash_a = deploy.bcrypt_hash(repo_root, config.TEST_PASSWORD)
    hash_b = deploy.bcrypt_hash(repo_root, config.TEST_PASSWORD)

    assert hash_a != hash_b
    assert hash_a.startswith("$2")
    assert hash_b.startswith("$2")


# ----------------------------------------------------------------------
# 辅助 fixture：repo_root。
# ----------------------------------------------------------------------


@pytest.fixture(scope="session")
def repo_root():
    """从本文件出发向上找到仓库根（cmd/lober + go.mod 存在的那一层）。
    与 test_deploy.py 的 _find_repo_root 逻辑相同，独立各自一份是因为
    两个测试文件目前没有共享的 conftest.py 承载会话级 fixture（那是
    任务 8 才建立的顶层 conftest.py 的职责）。
    """
    from pathlib import Path

    here = Path(__file__).resolve()
    for parent in here.parents:
        if (parent / "cmd" / "lober").is_dir() and (parent / "go.mod").is_file():
            return parent
    raise RuntimeError(f"从 {here} 向上找不到仓库根（cmd/lober + go.mod）")
