"""Check the exact-version peer family in a pnpm lock, without installing it."""
import json
from pathlib import Path
import re
import sys

lock = Path(sys.argv[1]).read_text()
rows = re.findall(r"^  '(@tiptap/[^']+?@([0-9]+\.[0-9]+\.[0-9]+))':$", lock, re.M)
versions = sorted({version for _, version in rows})
print(json.dumps({'packages': len(rows), 'versions': versions,
                  'status': 'PASS' if versions == ['3.32.0'] else 'FAIL'}))
if not rows or versions != ['3.32.0']:
    raise SystemExit(1)
