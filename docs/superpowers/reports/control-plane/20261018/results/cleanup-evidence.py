import json
from pathlib import Path
import subprocess

root = Path('/home/dev/lober-control-20261018-reviewed')
rules = subprocess.check_output(['sudo', '-n', 'iptables', '-w', '2', '-S', 'OUTPUT'], text=True)
remaining_rules = [line for line in rules.splitlines() if '-connect-timeout' in line and 'enterprise_' in line]
mounts = subprocess.check_output(['findmnt', '-rn', '-t', 'tmpfs', '-o', 'TARGET'], text=True)
remaining_mounts = [line for line in mounts.splitlines() if line.startswith('/tmp/lober-enterprise-http-')]
assert not remaining_rules and not remaining_mounts, 'Enterprise fault resources remain after suite cleanup'
row = {'dedicated_timeout_rules_remaining': len(remaining_rules),
       'enterprise_tmpfs_mounts_remaining': len(remaining_mounts),
       'shared_database_stopped': False, 'existing_qa_service_changed': False,
       'scope': 'Own test dependency resources; no PVE operation performed'}
(root / 'results/cleanup.json').write_text(json.dumps(row, indent=2) + '\n')
print(json.dumps(row))
