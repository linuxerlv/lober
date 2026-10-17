import hashlib, json, tarfile
from pathlib import Path
root = Path('/home/dev/lober-security-20261017')
with tarfile.open(root / 'pre-parser-dist.tar.gz') as tar:
    before = {member.name: hashlib.sha256(tar.extractfile(member).read()).hexdigest()
              for member in tar if member.isfile() and member.name.endswith('.css')}
current = root / 'frontend/internal/console/ui'
after = {str(path.relative_to(current)): hashlib.sha256(path.read_bytes()).hexdigest()
         for path in (current / 'dist').rglob('*.css')}
assert before and before == after, 'Production CSS changed after targeted parser override'
print(json.dumps({'production_css_files': len(after), 'status': 'PASS',
                  'meaning': 'Every emitted CSS filename and byte digest matches the pre-parser-upgrade build'}))
