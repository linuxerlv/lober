"""Real Linux qualification on the explicitly owned PVE guest; no synthetic signals.

Run on the testing VM, using a private state directory prepared for this campaign.
The command deliberately refuses other guest IDs, disks, MACs and OS families.
"""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import signal
import subprocess
import time
import urllib.parse

import requests

if not __debug__:
    raise RuntimeError('Qualification requires Python assertions enabled')


OS = {
    'debian': ('debian', 'preseed', 1),
    'ubuntu': ('ubuntu', 'autoinstall', 1),
    'leap15': ('leap15', 'autoyast', 1),
    'leap16': ('leap16', 'agama', 1),
    'almalinux': ('almalinux', 'kickstart', 2),
    'rocky': ('rocky', 'kickstart', 2),
}
SN = 'LOBER-LINUX-102'
MAC = 'bc:24:11:92:41:78'
GUEST = '192.168.56.50'


def require_network_first(config):
    assert config.get('boot') == 'order=net0;scsi0', 'qualification requires PXE before the owned disk'


def require_owned_serial(config):
    values = dict(field.split('=', 1) for field in config.get('smbios1', '').split(',') if '=' in field)
    serial = values.get('serial', '')
    if values.get('base64') == '1':
        serial = base64.b64decode(serial, validate=True).decode('ascii')
    assert serial == SN, 'owned guest SMBIOS serial mismatch'


def require_hook_failure(task, events):
    slot = next(s for s in task['snapshot']['slots'] if s['name'] == 'qualify')
    assert slot['exec'] and slot['timing'] == 'firstboot' and len(slot['instances']) == 1
    hook = slot['instances'][0]
    assert hook['required']
    matching = [e for e in events if e['kind'] == 'hook.report'
        and e['fields'].get('slot') == str(slot['seq'])
        and e['fields'].get('instance') == '1'
        and e['fields'].get('timing') == 'firstboot'
        and e['fields'].get('digest') == hook['digest']]
    assert len(matching) == 1 and matching[0]['fields'].get('rc') == '41', 'required qualification hook did not report RC41'


class Campaign:
    def __init__(self, root, system, negative=False):
        os.umask(0o077)
        self.root = root.resolve(strict=True)
        self.system = system
        self.series, self.format, self.version = OS[system]
        self.negative = negative
        self.private = json.loads((root / 'instance-private.json').read_text())
        self.config = json.loads((root / 'instance.json').read_text())
        assert self.config['listen'] == '0.0.0.0:18090'
        assert self.private['schema'].startswith('lober_linux_20261018_')
        query = dict(urllib.parse.parse_qsl(urllib.parse.urlsplit(self.config['pg']['dsn']).query))
        assert query.get('options') == '-csearch_path=' + self.private['schema']
        owned = json.loads((root / 'owned-pve-resources.json').read_text())
        assert owned['target_vmid'] == 102 and owned['purpose'] == 'linux-install-qualification'
        self.http = requests.Session()
        self.http.trust_env = False
        self.token = None
        self.manifest = json.loads((root / 'candidate.json').read_text())
        assert re.fullmatch(r'[a-z0-9-]+', self.manifest['run_id'])
        self.run = root / ('real-' + system + '-' + self.manifest['run_id'] + ('-required-failure' if negative else '-positive'))
        self.run.mkdir(exist_ok=True, mode=0o700)
        self.run.chmod(0o700)
        binary = Path(self.manifest['binary'])
        assert hashlib.sha256(binary.read_bytes()).hexdigest() == self.manifest['binary_sha256']
        self.qa = json.loads(Path(__file__).with_name('qualification-source.json').read_text())
        assert re.fullmatch(r'[0-9a-f]{40}', self.qa['test_sha'])
        assert self.qa['files']['linux_install.py'] == hashlib.sha256(Path(__file__).read_bytes()).hexdigest()
        assert self.qa['binary_sha256'] == self.manifest['binary_sha256'] and self.qa['run_id'] == self.manifest['run_id']
        prepared = self.run / 'candidate-at-prepare.json'
        if prepared.exists():
            assert json.loads(prepared.read_text())['binary_sha256'] == self.manifest['binary_sha256'], 'campaign candidate changed'

    def api(self, method, path, body=None, token=None, expected=200):
        auth = token or self.token
        response = self.http.request(method, 'http://127.0.0.1:18090/console/v1' + path,
            json=body, headers={'Authorization': 'Bearer ' + auth} if auth else {}, timeout=30)
        result = response.json()
        if response.status_code != expected:
            raise RuntimeError(f'{method} {path}: HTTP {response.status_code}; diagnostic bodies retained privately')
        return result.get('data')

    def login(self, username, password):
        return self.api('POST', '/auth/login', {'username': username, 'password': password})['token']

    def authenticate(self):
        # Reconcile a password change whose HTTP acknowledgment was lost.
        attempt = self.http.post('http://127.0.0.1:18090/console/v1/auth/login',
            json={'username': 'admin', 'password': self.private['admin']}, timeout=30)
        if attempt.status_code == 200:
            self.token = attempt.json()['data']['token']
        elif attempt.status_code == 401:
            seed = self.login('admin', self.private['seed'])
            self.api('POST', '/auth/password', {'currentPassword': self.private['seed'],
                'newPassword': self.private['admin']}, token=seed)
            self.token = self.login('admin', self.private['admin'])
        else:
            raise RuntimeError('Bootstrap reconciliation failed; no password reset attempted')
        self.private['passwordChanged'] = True
        (self.root / 'instance-private.json').write_text(json.dumps(self.private, indent=2))

    def pve(self, method, path, body=None):
        auth = json.loads((self.root / 'pve-session.json').read_text())
        response = self.http.request(method,
            'https://192.168.137.10:8006/api2/json/nodes/pve/qemu/102' + path,
            json=body, cookies={'PVEAuthCookie': auth['ticket']},
            headers={'CSRFPreventionToken': auth['csrf']}, verify=False, timeout=30)
        if response.status_code != 200:
            raise RuntimeError(f'Owned PVE guest {method} {path}: HTTP {response.status_code}')
        return response.json()['data']

    def check_guest(self, stopped=False):
        config = self.pve('GET', '/config')
        assert config['name'] == 'lober-linux-20261018'
        assert 'serial=LOBER_LINUX_TARGET' in config['scsi0'] and 'size=32G' in config['scsi0']
        assert MAC in config['net0'].lower() and 'bridge=vmbr1' in config['net0']
        require_owned_serial(config)
        require_network_first(config)
        assert not any(k.startswith(('scsi', 'sata', 'ide', 'virtio')) and k[-1:].isdigit()
            and k != 'scsi0' for k in config)
        if stopped:
            assert self.pve('GET', '/status/current')['status'] == 'stopped'
        return config

    def user(self, role):
        username = 'linux_qa_' + role.replace('-', '_')
        users = self.api('GET', '/users?limit=100')['items']
        if not any(u['username'] == username for u in users):
            self.api('POST', '/users', {'username': username, 'displayName': 'Linux QA ' + role,
                'role': role, 'password': self.private['admin']})
        return self.login(username, self.private['admin'])

    def approve(self, kind, draft, author, approver):
        path = f'/{kind}/{draft["id"]}/versions/{draft["version"]}'
        review = self.api('POST', path + '/submit?rev=' + str(draft['rev']), token=author)
        approved = self.api('POST', path + '/approve?rev=' + str(review['rev']), token=approver)
        assert approved['state'] == 'approved' and approved['author'] != approved['approver']
        return approved

    def prepare(self):
        self.check_guest(stopped=True)
        assert not (self.run / 'task-private.json').exists(), 'campaign already has a task; inspect it first'
        media = json.loads((self.root / ('import-' + self.series + '.json')).read_text())
        media = self.api('GET', '/isos/' + media['id'])
        assert media['state'] == 'ready' and media['series'] == self.series
        if self.system == 'debian':
            assert 'DVD-1' in media['name'] and media['artifacts'].get('repo')
        for name in ['kernel', 'initrd']:
            assert media['artifacts'].get(name)
        author, approver = self.user('hook-author'), self.user('hook-approver')
        hook_body = '''#!/bin/sh
set -eu
systemctl start qemu-guest-agent
systemctl is-active --quiet qemu-guest-agent
mkdir -p /var/lib/lober-qualification
printf 'firstboot-hook\n' >> /var/lib/lober-qualification/hook-runs
cat /etc/os-release
uname -r
'''
        if self.negative:
            hook_body += 'echo required-hook-failure-41\nexit 41\n'
        hook = self.api('POST', '/hooks', {'name': self.run.name, 'description': 'Qualification guest inspection and required-hook evidence',
            'timeoutSec': 180, 'required': True, 'params': [], 'impls': [{'family': 'linux',
                'series': [self.series], 'kind': 'script', 'body': hook_body, 'interpreter': 'sh',
                'timings': ['firstboot']}]}, token=author)
        hook = self.approve('hooks', hook, author, approver)
        draft = self.api('POST', '/templates/builtin-' + self.format + '/derive',
            {'from': self.version, 'name': self.run.name}, token=author)
        body = draft['body']
        assert body.count('{{lober:firstboot}}') == 1
        if self.format == 'preseed':
            body = body.replace('{{lober:firstboot}}', '{{slot:qualify@firstboot}} && {{lober:firstboot}}')
        elif self.format == 'autoinstall':
            body = body.replace('- {{lober:firstboot}}', '- {{slot:qualify@firstboot}}\n    - {{lober:firstboot}}')
        else:
            indent = '      ' if self.format == 'agama' else ''
            body = body.replace('{{lober:firstboot}}', '{{slot:qualify@firstboot}}\n' + indent + '{{lober:firstboot}}')
        draft['body'] = body
        draft['assembly']['qualify'] = [{'hookID': hook['id'], 'hookVersion': hook['version']}]
        payload = {k: draft[k] for k in ['name', 'family', 'series', 'body', 'params', 'bindings', 'assembly', 'rev']}
        draft = self.api('PUT', f'/templates/{draft["id"]}/versions/{draft["version"]}', payload, token=author)
        template = self.approve('templates', draft, author, approver)
        hosts = self.api('GET', '/hosts?limit=100')['items']
        host = next((h for h in hosts if h['sn'] == SN), None)
        if host:
            assert host['attrs'].get('qualification') == 'linux-20261018'
        else:
            self.api('POST', '/hosts', {'sn': SN, 'arch': 'x86_64', 'firmware': 'bios',
                'attrs': {'qualification': 'linux-20261018'}})
        entered = {'account/1/pwhash': self.private['guestHash'], 'sshkey/1/key': (self.root / 'guest-key.pub').read_text().strip(),
            'network/1/hostname': 'lober-' + self.system, 'network/1/interface': 'ens18',
            'packages/1/packages': 'curl\nqemu-guest-agent'}
        if self.system == 'ubuntu':
            entered['tpl/hostname'] = 'lober-ubuntu'
        if self.system == 'leap16':
            products = media['offers'].get('products', [])
            assert len(products) == 1, 'real Agama product inventory must resolve before creating a task'
            entered['tpl/product'] = products[0]['value']
        selection = {'isoID': media['id'], 'templateID': template['id'], 'templateVersion': template['version']}
        params = self.api('POST', '/wizard/params', {**selection, 'hostSNs': [SN], 'entered': {SN: entered}})
        (self.run / 'params-private.json').write_text(json.dumps(params, indent=2))
        assert not params['common'] and not params['hosts'][0]['problems'], 'wizard rejected qualification parameters'
        created = self.api('POST', '/wizard/tasks', {**selection, 'hosts': [{'sn': SN, 'entered': entered}]})
        task = created['tasks'][0]
        assert task['hostSN'] == SN and task['state'] == 'pending'
        (self.run / 'task-private.json').write_text(json.dumps(task, indent=2))
        (self.run / 'candidate-at-prepare.json').write_text(json.dumps(self.manifest, indent=2))
        self.save('PREPARED', task=task['id'], media=media['id'], version=media['version'])

    def task(self):
        frozen = json.loads((self.run / 'task-private.json').read_text())
        return self.api('GET', '/tasks/' + frozen['id'])

    def start(self):
        self.check_guest(stopped=True)
        assert self.task()['state'] == 'pending'
        print('Owned guest start operation:', self.pve('POST', '/status/start'), flush=True)

    def events(self, task):
        result, after = [], 0
        while True:
            items = self.api('GET', f'/tasks/{task["id"]}/events?after_seq={after}&limit=100')['items']
            if not items:
                return result
            assert all(e['seq'] > after for e in items)
            result.extend(items)
            after = items[-1]['seq']

    def wait(self):
        previous = None
        for _ in range(540):
            task = self.task()
            if task['state'] != previous:
                print(self.system, task['state'], flush=True)
                previous = task['state']
            if task['state'] in ['done', 'failed', 'canceled']:
                events = self.events(task)
                (self.run / 'events-private.json').write_text(json.dumps(events, indent=2))
                expected = 'failed' if self.negative else 'done'
                self.save('TERMINAL' if task['state'] == expected else 'FAIL', task=task['id'], state=task['state'], events=len(events))
                assert task['state'] == expected, 'unexpected terminal state; inspect private events and serial capture'
                if self.negative:
                    require_hook_failure(task, events)
                    codes = [p['code'] for p in task['canRetry']]
                    assert 'task_retry_after_reboot' in codes
                    rejected = self.api('POST', '/tasks/' + task['id'] + '/retry', expected=422)
                    assert any(p['code'] == 'task_retry_after_reboot' for p in rejected['problems'])
                    self.save('EXPECTED_FAILURE', task=task['id'], hook_rc=41, retry_http=422,
                        retry_code='task_retry_after_reboot')
                return
            time.sleep(10)
        self.save('TIMEOUT', task=self.task()['id'])
        raise RuntimeError('Real install exceeded campaign budget; no success recorded')

    def agent(self, command):
        result = self.pve('POST', '/agent/exec', {'command': ['/bin/sh', '-c', command]})
        for _ in range(60):
            status = self.pve('GET', '/agent/exec-status?pid=' + str(result['pid']))
            if status.get('exited'):
                assert status.get('exitcode') == 0, 'guest inspection failed'
                return status.get('out-data', '')
            time.sleep(1)
        raise RuntimeError('guest inspection agent timed out')

    def inspect(self):
        self.check_guest()
        task = self.task()
        assert task['state'] == ('failed' if self.negative else 'done')
        ssh = subprocess.run(['ssh', '-o', 'BatchMode=yes', '-o', 'ConnectTimeout=10',
            '-o', 'StrictHostKeyChecking=accept-new', '-o', 'UserKnownHostsFile=' + str(self.run / 'known_hosts'),
            '-i', str(self.root / 'guest-key'), 'lober@' + GUEST,
            'cat /etc/os-release; uname -r; findmnt -n -o SOURCE /; ip -4 route'],
            capture_output=True, text=True, timeout=30)
        (self.run / 'ssh.log').write_text(ssh.stdout + ssh.stderr)
        assert ssh.returncode == 0, 'installed guest SSH/network check failed'
        evidence = self.agent('''set -eu
printf 'OS_RELEASE='; base64 -w0 /etc/os-release; echo
printf 'BOOT_ID='; cat /proc/sys/kernel/random/boot_id
printf 'ROOT='; findmnt -n -o SOURCE /
printf 'ROOT_UUID='; findmnt -n -o UUID /
root_device=$(findmnt -n -o SOURCE / | cut -d '[' -f 1)
lsblk -snro SERIAL "$root_device" | grep -qx LOBER_LINUX_TARGET
printf 'HOOK_RUNS='; wc -l < /var/lib/lober-qualification/hook-runs
printf 'AGENT='; systemctl is-active qemu-guest-agent
printf 'CREDENTIAL_FILES='; find /var/lib/lober -maxdepth 1 -type f \\( -name '*.token' -o -name 'hook-*' -o -name 'script-*' -o -name 'firstboot.pending' \\) | wc -l
printf 'RETAINED_AUTH_PATHS_BEGIN\\n'
grep -rlE '/v1/(answer|signal)/[^/[:space:]]+/[A-Za-z0-9_.=-]{32,}/' /var/lib/lober /var/lib/cloud /var/log/installer /var/adm/autoinstall/cache /root 2>/dev/null || :
printf 'RETAINED_AUTH_PATHS_END\\n'
''')
        (self.run / 'guest-root.log').write_text(evidence)
        fields = dict(line.split('=', 1) for line in evidence.splitlines() if '=' in line)
        release = base64.b64decode(fields['OS_RELEASE']).decode()
        expected_id = {'debian': 'debian', 'ubuntu': 'ubuntu', 'leap15': 'opensuse-leap',
            'leap16': 'opensuse-leap', 'almalinux': 'almalinux', 'rocky': 'rocky'}[self.system]
        identity = dict(line.split('=', 1) for line in release.splitlines() if '=' in line)
        assert identity['ID'].strip('"') == expected_id
        expected_version = {'debian': '13', 'ubuntu': '24.04', 'leap15': '15.6',
            'leap16': '16.0', 'almalinux': '9.8', 'rocky': '9.8'}[self.system]
        assert identity['VERSION_ID'].strip('"') == expected_version
        assert fields['ROOT'].startswith('/dev/') and fields['HOOK_RUNS'] == '1' and fields['AGENT'] == 'active'
        assert fields['ROOT_UUID'], 'installed filesystem identity missing'
        if not self.negative:
            assert fields['CREDENTIAL_FILES'] == '0', 'runtime credentials were retained after done'
            retained = evidence.split('RETAINED_AUTH_PATHS_BEGIN\n', 1)[1].split('RETAINED_AUTH_PATHS_END', 1)[0].strip()
            assert not retained, 'installer/cloud state still retains machine credentials'
        return fields

    def qualify(self):
        assert not self.negative, 'required failure is evidence, never positive qualification'
        first = self.inspect()
        (self.run / 'first-boot-root.log').write_text((self.run / 'guest-root.log').read_text())
        (self.run / 'first-boot-ssh.log').write_text((self.run / 'ssh.log').read_text())
        task_id = self.task()['id']
        self.check_guest()
        capture_path = self.run / 'repeat-boot.pcap'
        with capture_path.open('wb') as packets, (self.run / 'repeat-capture.log').open('w') as log:
            capture = subprocess.Popen(['sudo', '-n', 'tcpdump', '-i', 'eth1', '-U', '-s', '0', '-w', '-',
                'host ' + GUEST + ' and tcp port 18090'], stdout=packets, stderr=log)
            try:
                for _ in range(30):
                    if capture.poll() is not None:
                        raise RuntimeError('owned reboot capture did not start')
                    if capture_path.stat().st_size >= 24:
                        break
                    time.sleep(.1)
                else:
                    raise RuntimeError('owned reboot capture did not become ready')
                self.pve('POST', '/status/reboot')
                deadline = time.monotonic() + 600
                while time.monotonic() < deadline:
                    try:
                        second = self.inspect()
                        if second['BOOT_ID'] != first['BOOT_ID']:
                            assert second['ROOT_UUID'] == first['ROOT_UUID'], 'root filesystem changed during repeat boot'
                            assert self.task()['id'] == task_id and self.task()['state'] == 'done'
                            break
                    except (requests.RequestException, subprocess.TimeoutExpired, AssertionError):
                        pass
                    time.sleep(5)
                else:
                    raise RuntimeError('repeat boot did not pass installed guest checks')
            finally:
                if capture.poll() is None:
                    capture.send_signal(signal.SIGINT)
                capture.wait(timeout=15)
        packets = capture_path.read_bytes()
        assert b'GET /v1/boot?sn=LOBER-LINUX-102' in packets, 'repeat boot did not contact machine gateway'
        assert b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n' in packets, 'gateway did not deliver local disk boot'
        self.save('PASS', task=task_id, first_boot_id=first['BOOT_ID'], repeat_boot_id=second['BOOT_ID'],
            root_uuid=second['ROOT_UUID'], checks=['real-media', 'PXE', 'disk-install', 'required-firstboot-hook',
                'done-events', 'SSH', 'guest-agent-root-inspection', 'credential-cleanup', 'repeat-gateway-localboot', 'same-root-filesystem'])
        return

    def save(self, status, **fields):
        result = {'status': status, 'system': self.system, 'negative': self.negative,
            'at': int(time.time()), 'product_sha': self.manifest['product_sha'],
            'test_sha': self.manifest['test_sha'], 'binary_sha256': self.manifest['binary_sha256'],
            'harness_test_sha': self.qa['test_sha'],
            'harness_sha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
            'evidence_sha256': {p.name: hashlib.sha256(p.read_bytes()).hexdigest()
                for p in self.run.iterdir() if p.suffix in ['.log', '.pcap']}, **fields}
        (self.run / 'result.json').write_text(json.dumps(result, indent=2))
        print(json.dumps(result), flush=True)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action', choices=['prepare', 'start', 'wait', 'inspect', 'qualify'])
    parser.add_argument('--state-dir', type=Path, required=True)
    parser.add_argument('--os', choices=tuple(OS), required=True)
    parser.add_argument('--required-failure', action='store_true')
    args = parser.parse_args()
    campaign = Campaign(args.state_dir, args.os, args.required_failure)
    campaign.authenticate()
    getattr(campaign, args.action)()
