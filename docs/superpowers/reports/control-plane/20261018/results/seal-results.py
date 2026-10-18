"""Seal fresh command evidence; never re-label an old test run as current."""
import hashlib
import json
from pathlib import Path
import re
import shutil
import sys

root = Path(sys.argv[1]) if len(sys.argv) > 1 else Path('/home/dev/lober-control-20261018-final')
out = root / 'results'
sys.path.insert(0, str(root / 'source/tests/lobertest/enterprise'))
from gate import digest, validate_candidate

manifest = json.loads((root / 'candidate.json').read_text())
assert not validate_candidate(manifest, root / 'source', root / 'lober-test')
common = {key: manifest[key] for key in ('product_sha', 'test_sha', 'run_id', 'binary_sha256')}
common['manifest_sha256'] = digest(root / 'candidate.json')
for name, expected in [('gate', 30), ('control', 88), ('flows', 4), ('events', 4), ('recovery', 5)]:
    row = json.loads((out / (name + '.json')).read_text())
    assert row['status'] == 'PASS' and row['count'] == expected and row['skipped'] == 0 and row['failed'] == 0
    assert int((out / (name + '.exit')).read_text()) == 0
    assert all(row[key] == value for key, value in common.items())
    assert row['evidence_sha256'] == digest(out / row['evidence'])

def record(name, count, **extra):
    assert int((out / (name + '.exit')).read_text()) == 0
    path = out / (name + '.log')
    assert path.stat().st_size > 0
    (out / (name + '.json')).write_text(json.dumps(dict(common,
        status='PASS', count=count, skipped=0, failed=0, evidence=path.name,
        evidence_sha256=digest(path), **extra), indent=2) + '\n')

origin = json.loads((Path('/home/dev/lober-control-20261018-final') / 'candidate.json').read_text())
product_input = lambda name: name in ('go.mod', 'go.sum', 'Makefile') or name.startswith(('cmd/', 'internal/', 'web/'))
assert {name: value for name, value in origin['files'].items() if product_input(name)} == {
    name: value for name, value in manifest['files'].items() if product_input(name)}
origin_digest = digest(Path('/home/dev/lober-control-20261018-final/candidate.json'))
for name in ('backend', 'frontend'):
    for suffix in ('.log', '.exit'):
        shutil.copy2(Path('/home/dev/lober-control-20261018-final/results') / (name + suffix), out / (name + suffix))
backend = (out / 'backend.log').read_text()
assert 'go test -race' in backend and 'wire diff' in backend
packages = set(re.findall(r'^ok\s+(\S+)', backend, re.M))
assert len(packages) == 18
record('backend', 18, count_unit='Go packages with tests', command='make check; go test -count=1 ./...',
       execution_run_id=origin['run_id'], execution_manifest_sha256=origin_digest,
       verified_identical_product_inputs=True)
frontend = re.sub(r'\x1b\[[0-?]*[ -/]*[@-~]', '', (out / 'frontend.log').read_text())
assert re.search(r'Test Files\s+25 passed', frontend) and re.search(r'Tests\s+88 passed', frontend)
assert '11 successful, 11 total' in frontend
record('frontend', 88, count_unit='Vitest cases', command='pnpm check:console (types/lint/Vitest/build)',
       execution_run_id=origin['run_id'], execution_manifest_sha256=origin_digest,
       verified_identical_product_inputs=True)
shutil.copy2(Path('/home/dev/lober-control-20261018-final/candidate.json'), out / 'baseline-candidate.json')
shutil.copy2(root / 'candidate.json', out / 'candidate.json')

reasons = {
 'fuzz_iso': ('NOT_RUN', 'Previous dependency batch passed 10-minute ISO fuzz; not repeated in this test-only batch.'),
 'fuzz_wim': ('NOT_RUN', 'Previous dependency batch passed 10-minute WIM fuzz; not repeated in this test-only batch.'),
 'security_scan': ('NOT_RUN', 'Previous dependency batch: 0 called/imported advisories, 1 module-only advisory; not repeated here.'),
 'frontend_audit': ('BLOCKED', 'Previous batch still had node-forge/braces/smol-toml advisories; not re-audited or accepted here.'),
 'real_media': ('BLOCKED', 'Synthetic integration media only; qualified real distribution media/netboot manifest absent.'),
 'pxe_state': ('BLOCKED', 'Seventh-layer gateway and machine installation state machine remain unimplemented.'),
 'real_install': ('BLOCKED', 'No real installation performed; requires qualified media and seventh layer.'),
 'capacity_soak': ('NOT_RUN', 'Capacity and 24-hour mixed workload qualification not run.'),
 'supported_upgrade': ('BLOCKED', 'Approved migration starting point and supported full-system restore qualification absent.')}
for name, (status, reason) in reasons.items():
    path = out / (name + '.log')
    path.write_text(status + ': ' + reason + '\n')
    (out / (name + '.json')).write_text(json.dumps(dict(common, status=status, reason=reason,
        count=0, failed=0, skipped=0, evidence=path.name, evidence_sha256=digest(path)), indent=2) + '\n')

secret = Path('/home/dev/lober-enterprise-20261017-f68432a/test-pg.dsn').read_text().strip()
for path in out.iterdir():
    if path.is_file():
        text = path.read_text()
        if secret in text or re.search(r'postgres(?:ql)?://\S+|-----BEGIN (?:OPENSSH|RSA|EC) PRIVATE KEY-----|Bearer [A-Za-z0-9._-]{15,}', text):
            raise RuntimeError('Restricted material in public evidence: ' + path.name)
print(json.dumps({'sealed': True, 'run_id': common['run_id'], 'counts': [30, 88, 4, 4, 5], 'release_qualified': False}))
