"""验证 GitHub 应用凭据并在宿主机写入回报服务配置。"""

import argparse
import grp
import hashlib
import os
from pathlib import Path
import stat
import subprocess
import sys
import tempfile


DIRECTORY = Path("/etc/lober-ci")
REPOSITORY = "linuxerlv/lober"
APP_ID = 5708321


def private_key(path):
    if path.resolve(strict=True) != path:
        raise ValueError("密钥必须是绝对路径，且路径不能包含符号链接")
    info = path.lstat()
    if not stat.S_ISREG(info.st_mode) or info.st_uid != 0 or info.st_mode & 0o077:
        raise ValueError("密钥必须是管理员私有的普通文件")
    for parent in path.parents:
        if parent.stat().st_uid != 0 or parent.stat().st_mode & 0o022:
            raise ValueError("密钥的父目录必须由管理员控制")
    with path.open("rb") as key:
        data = key.read(32769)
    if not 0 < len(data) <= 32768:
        raise ValueError("密钥文件大小无效")
    return data


def configure(path, installation, creator, group, *, check_only=False):
    content = private_key(path)
    env = {
        "LOBER_CI_GITHUB_REPOSITORY": REPOSITORY,
        "LOBER_CI_APP_ID": str(APP_ID),
        "LOBER_CI_INSTALLATION_ID": str(installation),
        "LOBER_CI_STATUS_CREATOR_ID": str(creator),
        "LOBER_CI_APP_KEY": str(path),
    }
    check = subprocess.run([sys.executable, "-I", str(Path(__file__).with_name("app-token"))],
                           env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, timeout=30)
    if check.returncode:
        raise ValueError("应用安装令牌验证失败，请检查密钥、安装编号和仓库权限")
    if check_only:
        return

    DIRECTORY.mkdir(mode=0o750, exist_ok=True)
    details = DIRECTORY.lstat()
    if not stat.S_ISDIR(details.st_mode) or details.st_uid != 0 or details.st_mode & 0o022:
        raise ValueError("服务配置目录必须由管理员控制")
    os.chown(DIRECTORY, 0, group)
    DIRECTORY.chmod(0o750)
    # 密钥文件采用内容独立的路径。替换配置前，已经在用的密钥仍然可读。
    key_path = DIRECTORY / ("report-" + hashlib.sha256(content).hexdigest()[:16] + ".pem")
    env["LOBER_CI_APP_KEY"] = str(key_path)
    config = "".join(f"{key}={value}\n" for key, value in env.items()).encode("ascii")
    for target, data, gid, mode in ((key_path, content, group, 0o640),
                                    (DIRECTORY / "report.env", config, 0, 0o600)):
        with tempfile.NamedTemporaryFile(dir=DIRECTORY, delete=False) as stream:
            temporary = Path(stream.name)
            try:
                stream.write(data)
                stream.close()
                os.chown(temporary, 0, gid)
                temporary.chmod(mode)
                temporary.replace(target)
            finally:
                temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--key", type=Path, required=True)
    parser.add_argument("--installation-id", type=int, required=True)
    parser.add_argument("--creator-id", type=int, required=True)
    parser.add_argument("--check-only", action="store_true")
    args = parser.parse_args()
    if os.geteuid() or args.installation_id <= 0 or args.creator_id <= 0:
        parser.error("请由宿主机管理员提供有效的应用安装编号和机器人编号")
    os.umask(0o077)
    try:
        group = grp.getgrnam("lober-ci-report").gr_gid if not args.check_only else 0
        configure(args.key, args.installation_id, args.creator_id, group, check_only=args.check_only)
    except (OSError, KeyError, ValueError, subprocess.SubprocessError) as error:
        parser.exit(1, f"配置回报服务失败：{error}\n")
    print("GitHub 应用安装令牌验证通过")


if __name__ == "__main__":
    main()
