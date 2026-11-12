"""准备并核对一台隔离 KVM 来宾的 Rocky 装机。"""
import json
from pathlib import Path
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


def api(path, token=None, body=None):
    headers = {'Content-Type': 'application/json'}
    if token:
        headers['Authorization'] = 'Bearer ' + token
    request = urllib.request.Request(BASE + path, headers=headers,
                                     data=json.dumps(body).encode() if body is not None else None)
    try:
        with urllib.request.build_opener(urllib.request.ProxyHandler({})).open(request, timeout=15) as response:
            result = json.load(response)
    except urllib.error.HTTPError as error:
        raise RuntimeError(f'{path}: HTTP {error.code}') from None
    if result['code'] != 0:
        raise RuntimeError(f'{path}: invalid response code')
    return result['data']


def prepare(work):
    seed = json.loads((work / 'config.json').read_text())['seedAdmin']
    password = secrets.token_urlsafe(32)
    token = api('/auth/login', body={'username': 'admin', 'password': seed})['token']
    api('/auth/password', token, {'currentPassword': seed, 'newPassword': password})
    token = api('/auth/login', body={'username': 'admin', 'password': password})['token']
    item = api('/isos', token, {'name': 'Rocky 10.2 minimal', 'family': 'linux', 'series': 'rocky',
                               'sources': [{'kind': 'path', 'addr': str(ROCKY)}]})
    deadline = time.monotonic() + 480
    while time.monotonic() < deadline:
        item = api('/isos/' + item['id'], token)
        if item['state'] == 'ready':
            break
        if item['state'] == 'failed':
            raise RuntimeError('Rocky 镜像导入失败')
        time.sleep(1)
    else:
        raise RuntimeError('Rocky 镜像导入超时')
    if item['sha256'] != ROCKY_SHA:
        raise RuntimeError('导入后的 Rocky 镜像校验不符')
    print(f"官方 Rocky 镜像已由候选导入，版本 {item['version']}", flush=True)
    api('/hosts', token, {'sn': SERIAL, 'arch': 'x86_64', 'firmware': 'bios', 'attrs': {}})
    key = work / 'guest-key'
    subprocess.run(['/usr/bin/ssh-keygen', '-q', '-t', 'ed25519', '-N', '', '-f', key], check=True)
    password_hash = subprocess.check_output(['/usr/bin/openssl', 'passwd', '-6', '-stdin'],
                                            input=secrets.token_urlsafe(32) + '\n', text=True).strip()
    entered = {'partition/1/disk': 'vda', 'account/1/username': 'lober', 'account/1/pwhash': password_hash,
               'sshkey/1/key': key.with_suffix('.pub').read_text().strip(), 'network/1/hostname': 'lober-kvm-rocky',
               'packages/1/packages': 'curl\nopenssh-server'}
    selection = {'isoID': item['id'], 'templateID': 'builtin-kickstart', 'templateVersion': 6}
    params = api('/wizard/params', token, {**selection, 'hostSNs': [SERIAL], 'entered': {SERIAL: entered}})
    if params['common'] or params['hosts'][0]['problems']:
        for part in [*params['common'], *params['hosts'][0]['problems']]:
            print('参数检查', {k: v for k, v in part.items() if k in ('key', 'code', 'message')}, flush=True)
        raise RuntimeError('装机参数校验失败')
    task = api('/wizard/tasks', token, {**selection, 'hosts': [{'sn': SERIAL, 'entered': entered}]})['tasks'][0]
    (work / 'install-session.json').write_text(json.dumps({'token': token, 'task': task['id']}))
    print(f"装机任务 {task['id']} 已进入 {task['state']}", flush=True)


def wait(work):
    state = json.loads((work / 'install-session.json').read_text())
    task_path = '/tasks/' + state['task']
    deadline = time.monotonic() + 900
    previous = None
    while time.monotonic() < deadline:
        task = api(task_path, state['token'])
        if task['state'] != previous:
            previous = task['state']
            print('任务状态', previous, flush=True)
        if previous == 'done':
            return
        if previous in ('failed', 'cancelled'):
            raise RuntimeError('候选报告装机失败或取消')
        time.sleep(3)
    raise RuntimeError('来宾装机等待超时')


if __name__ == '__main__':
    operation, path = sys.argv[1:]
    {'prepare': prepare, 'wait': wait}[operation](Path(path))
