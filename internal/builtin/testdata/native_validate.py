"""Optional native parser verification; see native-content.md. No network or production dependency."""
import hashlib
import json
import pathlib
import sys

import jsonschema
import yaml
from referencing import Registry, Resource
from referencing.jsonschema import DRAFT201909

schema_dir = pathlib.Path(sys.argv[2]).resolve()
hashes = {
    "profile.schema.json": "4c05ead0907df5715020638de28898d4458d6ccbe2aee9450d7bb32da6821fb5",
    "storage.schema.json": "1b60a513b5b5c9a2c1091426a5bc1a00a640fe19a597ac2f24971548a9a0b77e",
    "storage.model.schema.json": "f58c29062f6e7c2cb93e7c380165bd11cd13e648e5f5d40bddcb36e802cd31d2",
    "iscsi.schema.json": "04be9d9414083fdd81f30b3fc80e23aeac8ec12da2c10213237a49fc94c20200",
}
resources = []
for name, expected in hashes.items():
    path = schema_dir / name
    data = path.read_bytes()
    assert hashlib.sha256(data).hexdigest() == expected, name
    schema = json.loads(data)
    schema["$id"] = path.as_uri()  # Same local-base treatment as Agama's validator.
    resources.append((path.as_uri(), Resource.from_contents(schema, default_specification=DRAFT201909)))
schema = resources[0][1].contents
profile = json.loads(pathlib.Path(sys.argv[1]).read_text())
jsonschema.Draft201909Validator(schema, registry=Registry().with_resources(resources)).validate(profile)
assert profile["storage"]["boot"] == {"configure": True, "device": "system"}
assert profile["product"]["id"] == "SLES"
assert "init" not in profile["scripts"]
print("Agama Jsonnet/v17 pinned native schema PASS")
answer = pathlib.Path(__file__).parent / "answers/autoinstall-uefi.golden"
ubuntu = yaml.safe_load(answer.read_text())["autoinstall"]
assert ubuntu["identity"]["hostname"] == "lober-node"
assert ubuntu["storage"]["layout"] == {"name": "direct", "match": {"path": "/dev/sda"}, "ptable": "gpt"}
assert ubuntu["packages"] == ["curl", "systemd", "openssh-server", "curl", "vim"]
assert len(ubuntu["late-commands"]) == 2
print("Ubuntu native YAML structure/values PASS")
