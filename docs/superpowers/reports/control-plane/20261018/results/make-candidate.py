import hashlib
import json
from pathlib import Path
import secrets
import sys
import tarfile

root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path('/home/dev/lober-control-20261018-final')
prior = Path('/home/dev/lober-security-20261018-final')
digest = lambda path: hashlib.sha256(path.read_bytes()).hexdigest()
identities = json.loads((root / 'identities.json').read_text())
old = json.loads((prior / 'candidate.json').read_text())
assert identities['product_sha'] == old['product_sha']
assert digest(root / 'lober-test') == old['binary_sha256']
files = {}
with tarfile.open(root / 'candidate-source.tar') as archive:
    for member in archive:
        if member.issym() or member.islnk():
            raise RuntimeError('Explicit symlink policy required')
        if member.isfile():
            files[member.name] = hashlib.sha256(archive.extractfile(member).read()).hexdigest()
product = lambda name: name in ('go.mod', 'go.sum', 'Makefile') or name.startswith(('cmd/', 'internal/', 'web/'))
current = {name: value for name, value in files.items() if product(name)}
previous = {name: value for name, value in old['files'].items() if product(name)}
assert current == previous, 'Product/build inputs changed; cannot reuse verified binary'
frontend = Path('/home/dev/lober-security-20261017/frontend')
for name, value in current.items():
    if name.startswith('web/'):
        assert digest(frontend / name) == value, 'Frontend workspace differs from candidate: ' + name
manifest = dict(identities, run_id='vm-control-20261018-' + secrets.token_hex(8),
    binary=str(root / 'lober-test'), binary_sha256=digest(root / 'lober-test'),
    source_archive=str(root / 'candidate-source.tar'), source_archive_sha256=digest(root / 'candidate-source.tar'),
    files=files, execution_host='192.168.137.21', product_input_files_verified=len(current),
    binary_origin_run_id=old['run_id'], binary_origin_manifest_sha256=digest(prior / 'candidate.json'),
    frontend_archive_sha256=old['frontend_archive_sha256'],
    fixture_sha256=digest(Path('/home/dev/lober-enterprise-20261017-f68432a/synthetic-rocky.iso')))
(root / 'candidate.json').write_text(json.dumps(manifest, indent=2) + '\n')
print(json.dumps({key: manifest[key] for key in ('product_sha', 'test_sha', 'run_id', 'binary_sha256', 'product_input_files_verified')}))
