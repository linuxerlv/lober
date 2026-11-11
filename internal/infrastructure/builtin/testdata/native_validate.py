"""Optional native parser verification; see native-content.md. No network or production dependency."""
import pathlib

import yaml

answer = pathlib.Path(__file__).parent / "answers/autoinstall-uefi.golden"
ubuntu = yaml.safe_load(answer.read_text())["autoinstall"]
assert ubuntu["identity"]["hostname"] == "lober-node"
assert ubuntu["storage"]["layout"] == {"name": "direct", "match": {"path": "/dev/sda"}, "ptable": "gpt"}
assert ubuntu["packages"] == ["curl", "systemd", "openssh-server"]
assert len(ubuntu["late-commands"]) == 2
print("Ubuntu native YAML structure/values PASS")
