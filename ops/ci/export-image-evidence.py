"""把指定候选的校验原件投影到回报校验账号可读的目录。"""
import argparse
import grp
import json
import os
from pathlib import Path
import re
import stat
import tempfile


SOURCE = Path("/var/lib/lober-container-ci")
DESTINATION = Path("/var/lib/lober-ci-container-evidence")
SUITES = {
    "control": ("control.json",),
    "flows": ("flows.json",),
    "events": ("events.json",),
    "recovery": ("recovery.json",),
    "upgrade": (),
    "kvm_batch": ("kvm/shared-service-start.json", "kvm/shared-service-end.json"),
}


def selected_files(manifest):
    files = []
    for suite, extra in SUITES.items():
        run = manifest[f"container_{suite}_run"]
        if not re.fullmatch(r"[0-9a-f]{32}", run):
            raise ValueError("运行编号无效")
        files.extend(Path("runs", run, name) for name in ("result.json", "check.log", *extra))
    capacity = manifest["container_capacity_run"]
    if not re.fullmatch(r"kvm-capacity\.[A-Za-z0-9]{8}", capacity):
        raise ValueError("容量编号无效")
    return files + [Path("capacity", capacity + ".json")]


def export(manifest, group):
    paths = {}
    for relative in selected_files(manifest):
        source = SOURCE / relative
        if source.resolve(strict=True) != source:
            raise ValueError("证据源路径无效")
        details = source.stat()
        if details.st_uid != 0 or details.st_mode & 0o077 or not stat.S_ISREG(details.st_mode):
            raise ValueError("证据原件必须由管理员私有保存")
        if not 0 < details.st_size <= 64 << 20:
            raise ValueError("证据原件为空或超出大小限制")
        paths[relative] = source.read_bytes()
    for relative, data in paths.items():
        path = DESTINATION / relative
        for directory in reversed([p for p in path.parents if p.is_relative_to(DESTINATION)]):
            directory.mkdir(mode=0o750, exist_ok=True)
            details = directory.lstat()
            if not stat.S_ISDIR(details.st_mode) or details.st_uid != 0 or details.st_mode & 0o022:
                raise ValueError("只读证据目录必须由管理员控制")
            os.chown(directory, 0, group)
            directory.chmod(0o750)
        if path.exists() or path.is_symlink():
            if path.is_symlink() or path.read_bytes() != data:
                raise ValueError("已导出的证据与原件不符")
            continue
        with tempfile.NamedTemporaryFile(dir=path.parent, delete=False) as stream:
            temporary = Path(stream.name)
            try:
                stream.write(data)
                stream.close()
                os.chown(temporary, 0, group)
                temporary.chmod(0o640)
                os.link(temporary, path)
            finally:
                temporary.unlink(missing_ok=True)


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("manifest", type=Path)
    args = parser.parse_args()
    if os.geteuid() != 0:
        parser.error("仅管理员可导出证据原件")
    os.umask(0o077)
    try:
        export(json.loads(args.manifest.read_text()), grp.getgrnam("lober-ci-test").gr_gid)
    except (OSError, ValueError, KeyError, TypeError) as error:
        parser.exit(1, f"导出证据失败：{error}\n")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
