"""构建时保留所选驱动所需的固件和软链接目标。"""

import argparse
import os
import subprocess
from pathlib import Path


class Firmware:
    def __init__(self, root):
        self.root = root.resolve(strict=True)
        self.kept = set()

    def keep(self, path):
        try:
            target = path.resolve(strict=True)
        except (OSError, RuntimeError):
            return False
        if not target.is_relative_to(self.root) or not target.is_file():
            return False
        # 在整个链接链上保留入口。只保留最后一个目标会弄断多级链接。
        while path != self.root and path.is_relative_to(self.root):
            for parent in path.parents:
                if parent == self.root:
                    break
                if parent.is_symlink():
                    self.kept.add(parent)
            if path in self.kept:
                break
            self.kept.add(path)
            if not path.is_symlink():
                break
            path = path.parent / path.readlink()
            # 规范化相对路径，再走一层；不要提前解析下一层链接。
            path = Path(os.path.normpath(path))
        self.kept.add(target)
        return True

    def match(self, name, required=False):
        pattern = Path(name)
        if pattern.is_absolute() or ".." in pattern.parts:
            raise ValueError(f"无效的固件路径 {name}")
        found = False
        # Alpine 的 Linux 内核支持未压缩和 zstd 压缩固件。
        for suffix in ("", ".zst"):
            for candidate in sorted(self.root.glob(name + suffix)):
                found |= self.keep(candidate)
        if required and not found:
            raise ValueError(f"网卡驱动缺少启动固件 {name}")

    def prune(self):
        for entry in sorted(self.root.rglob("*"), reverse=True):
            if entry in self.kept:
                continue
            if entry.is_symlink() or entry.is_file():
                entry.unlink()
            elif entry.is_dir() and not any(entry.iterdir()):
                entry.rmdir()


def output(*arguments):
    return subprocess.check_output(arguments, text=True, encoding="utf-8").splitlines()


def declared(release, module):
    return output("modinfo", "--set-version", release, "--field", "firmware", str(module))


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--keep-package", action="append", default=[])
    parser.add_argument("release")
    parser.add_argument("drivers", nargs="+")
    args = parser.parse_args()
    firmware = Firmware(Path("/lib/firmware"))
    for driver in args.drivers:
        modules = {driver}
        for line in output("modprobe", "--show-depends", "--set-version", args.release, driver):
            action, _, module = line.partition(" ")
            if action == "insmod":
                modules.add(module.partition(" ")[0])
        for module in sorted(modules):
            for name in declared(args.release, module):
                if name:
                    firmware.match(name, required=True)
    for module in sorted(Path("/lib/modules", args.release).rglob("*.ko*")):
        for name in declared(args.release, module):
            if name:
                firmware.match(name)
    # Intel ICE 支持按网卡选择多个 DDP 配置包，查询驱动只列出默认别名。
    if "ice" in args.drivers:
        for path in sorted(firmware.root.glob("intel/ice/**/*")):
            firmware.keep(path)
    for package in args.keep_package:
        for name in output("apk", "info", "--contents", package):
            path = Path("/") / name
            if path.is_relative_to(firmware.root):
                firmware.keep(path)
    firmware.prune()
    for path in sorted(firmware.kept):
        print(path.relative_to(firmware.root))


if __name__ == "__main__":
    main()
