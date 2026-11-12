"""准备并核对一台隔离 KVM 来宾的网络装机。"""
import json
import hashlib
import os
from pathlib import Path
import re
import secrets
import subprocess
import sys
import time
import urllib.error
import urllib.request


BASE = 'http://192.0.2.10:18090/console/v1'
ROCKY = Path('/run/lober-ci-verified-media/rocky.iso')
ROCKY_SHA = 'aac6ac3ce781b91a91ce78463405f66c611a5dca4b3840c79e5e01d97302f6c8'
SERIAL = 'LOBER-KVM-PXE-PROBE'


def api(path, token=None, body=None, method=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(BASE + path, headers=headers, method=method,
                                     data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=15) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        try:
            problems = (json.loads(error.read(65536)).get('data') or {}).get('problems', [])
            codes = [p['code'] for p in problems if re.fullmatch(r'[a-z0-9_]{1,64}', p.get('code', ''))]
        except (AttributeError, TypeError, ValueError):
            codes = []
        raise RuntimeError(f'{path}: HTTP {error.code}; {", ".join(codes)}') from None
    if result['code'] != 0:
        raise RuntimeError(f'{path}: invalid response code')
    return result['data']


def admin_login(work):
    seed = json.loads((work / 'config.json').read_text())['seedAdmin']
    password = secrets.token_urlsafe(32)
    token = api('/auth/login', body={'username': 'admin', 'password': seed})['token']
    api('/auth/password', token, {'currentPassword': seed, 'newPassword': password})
    return api('/auth/login', body={'username': 'admin', 'password': password})['token']


def import_media(token, series, name, sources, expected):
    item = api('/isos', token, {'name': name, 'family': 'linux', 'series': series, 'sources': sources})
    deadline = time.monotonic() + 900
    while time.monotonic() < deadline:
        item = api('/isos/' + item['id'], token)
        if item['state'] == 'ready':
            break
        if item['state'] == 'failed':
            raise RuntimeError(f'{name} 镜像导入失败')
        time.sleep(1)
    else:
        raise RuntimeError(f'{name} 镜像导入超时')
    if item['sha256'] != expected:
        raise RuntimeError(f'导入后的 {name} 镜像校验不符')
    print(f"{name} 已由候选导入，版本 {item['version']}", flush=True)
    return item


def guest_parameters(work):
    key = work / 'guest-key'
    subprocess.run(['/usr/bin/ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', key], check=True)
    password_hash = subprocess.check_output(['/usr/bin/openssl', 'passwd', '-6', '-stdin'],
                                            input=secrets.token_urlsafe(32) + '\n', text=True).strip()
    return {'partition/1/disk': 'vda', 'account/1/username': 'lober', 'account/1/pwhash': password_hash,
            'sshkey/1/key': key.with_suffix('.pub').read_text().strip()}


def create_install_task(work, token, selection, entered):
    api('/hosts', token, {'sn': SERIAL, 'arch': 'x86_64', 'firmware': 'bios', 'attrs': {}})
    params = api('/wizard/params', token, {**selection, 'hostSNs': [SERIAL], 'entered': {SERIAL: entered}})
    if params['common'] or params['hosts'][0]['problems']:
        for part in [*params['common'], *params['hosts'][0]['problems']]:
            print('参数检查', {k: v for k, v in part.items() if k in ('key', 'code', 'message')}, flush=True)
        raise RuntimeError('装机参数校验失败')
    task = api('/wizard/tasks', token, {**selection, 'hosts': [{'sn': SERIAL, 'entered': entered}]})['tasks'][0]
    (work / 'install-session.json').write_text(json.dumps({'token': token, 'task': task['id']}))
    print(f"装机任务 {task['id']} 已进入 {task['state']}", flush=True)


def prepare(work):
    token = admin_login(work)
    item = import_media(token, 'rocky', 'Rocky 10.2 minimal', [{'kind': 'path', 'addr': str(ROCKY)}], ROCKY_SHA)
    selection = {'isoID': item['id'], 'templateID': 'builtin-kickstart', 'templateVersion': 6}
    entered = {**guest_parameters(work), 'network/1/hostname': 'lober-kvm-rocky',
               'packages/1/packages': 'curl\nopenssh-server'}
    create_install_task(work, token, selection, entered)


def prepare_debian(work, netinst=False):
    media = Path('/run/lober-ci-verified-media')
    image = media / ('debian-netinst.iso' if netinst else 'debian-dvd.iso')
    mini = media / 'debian-mini.iso'
    expected_sha512 = (
        'ef04d0276850d70e4aaec0b55003628660e9631b26e35215a70ab3c8c707294a4b12731bc73c75a932902f703a7c278310ca99df28d6b36fb06d5618d8d0f678'
        if netinst else
        '305cc7a3f0d4e293ff979cc6c047d7fe46454853a0f170a0520acd8aab37c2f8839969c817afac2b4324ef5944e0038502b7fcca5dfcd48b5b77544ef1f14cee'
    )
    mini_sha256 = '34e4af5d0aa0886a4b7338dce796b9bc27ef1e6dcf44cdcc4b22c100eb492388'
    sha256, sha512 = hashlib.sha256(), hashlib.sha512()
    with image.open('rb') as stream:
        for block in iter(lambda: stream.read(1024 * 1024), b''):
            sha256.update(block)
            sha512.update(block)
    with mini.open('rb') as stream:
        observed_mini = hashlib.file_digest(stream, 'sha256').hexdigest()
    if sha512.hexdigest() != expected_sha512 or observed_mini != mini_sha256:
        raise RuntimeError('Debian 镜像与签名清单校验值不符')
    token = admin_login(work)
    name = 'Debian 13.7 netinst diagnostic' if netinst else 'Debian 13.7 DVD-1'
    item = import_media(token, 'debian', name, [
        {'kind': 'path', 'addr': str(image)}, {'kind': 'path', 'addr': str(mini), 'role': 'netboot'},
    ], sha256.hexdigest())
    credentials = {}
    for role in ('hook-author', 'hook-approver'):
        username, password = role.replace('-', '_'), secrets.token_urlsafe(32)
        api('/users', token, {'username': username, 'displayName': username, 'role': role, 'password': password})
        credentials[role] = api('/auth/login', body={'username': username, 'password': password})['token']
    author, approver = credentials['hook-author'], credentials['hook-approver']
    draft = api('/templates/builtin-preseed/derive', author, {'from': 6, 'name': 'debian-isolated-kvm'})
    version_path = f"/templates/{draft['id']}/versions/{draft['version']}"
    body = draft['body'] + '\nd-i apt-setup/services-select multiselect\nd-i mirror/suite string trixie\n'
    payload = {key: draft[key] for key in ('name', 'family', 'series', 'params', 'bindings', 'assembly', 'rev')}
    draft = api(version_path, author, {**payload, 'body': body}, method='PUT')
    submitted = api(f"{version_path}/submit?rev={draft['rev']}", author, {})
    approved = api(f"{version_path}/approve?rev={submitted['rev']}", approver, {})
    if approved['state'] != 'approved':
        raise RuntimeError('Debian 验收模板未获独立账号审批')
    selection = {'isoID': item['id'], 'templateID': approved['id'], 'templateVersion': approved['version']}
    entered = {**guest_parameters(work), 'network/1/hostname': 'lober-kvm-debian',
               'packages/1/packages': 'curl\nopenssh-server\nqemu-guest-agent',
               'tpl/mirror_host': '192.0.2.10', 'tpl/mirror_path': '/debian'}
    create_install_task(work, token, selection, entered)


def wait(work):
    state = json.loads((work / 'install-session.json').read_text())
    task_path = '/tasks/' + state['task']
    deadline = time.monotonic() + int(os.environ['LOBER_INSTALL_WAIT_SECONDS'])
    previous = None
    while time.monotonic() < deadline:
        guest_pid = os.environ.get('LOBER_INSTALL_GUEST_PID')
        if guest_pid and not Path('/proc', guest_pid).exists():
            raise RuntimeError('来宾在任务完成前退出，检查串口日志')
        task = api(task_path, state['token'])
        if task['state'] != previous:
            previous = task['state']
            print('任务状态', previous, flush=True)
        if previous == 'done':
            events = api(task_path + '/events?limit=100', state['token'])['items']
            sequences = [event['seq'] for event in events]
            required = {'machine.installing', 'machine.init_start', 'machine.ready'}
            if (task['hostSN'] != SERIAL or sequences != sorted(set(sequences))
                    or any(event['taskID'] != state['task'] for event in events)
                    or not required.issubset(event['kind'] for event in events)):
                raise RuntimeError('装机任务缺少同一来宾的安装、首启或就绪记录')
            for event in events:
                if event['kind'] in required:
                    print('装机事件', json.dumps({key: event[key] for key in ('taskID', 'seq', 'at', 'kind')}), flush=True)
            return
        if previous in ('failed', 'cancelled'):
            raise RuntimeError('候选报告装机失败或取消')
        time.sleep(3)
    raise RuntimeError('来宾装机等待超时')


if __name__ == '__main__':
    operation, path = sys.argv[1:]
    {'prepare': prepare, 'prepare-debian': prepare_debian,
     'prepare-debian-netinst': lambda work: prepare_debian(work, netinst=True), 'wait': wait}[operation](Path(path))
