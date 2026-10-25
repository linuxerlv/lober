"""真实空库、异目录恢复、离线控制面子资格。只使用本轮专用目录。"""
import argparse
import ctypes
from contextlib import contextmanager
import fcntl
import hashlib
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import shutil
import signal
import socket
import stat
import subprocess
import sys
import time
import urllib.error
import urllib.request

import gate
from gate import digest, validate_candidate

BASE = Path('/home/dev/lober-production-qualification-20261025')
OWNER = 'production-qualification-20261025'
PHASES = {
    'fresh': ['empty_database', 'seed_required', 'bootstrap_password', 'web_assets',
              'business_seed', 'browser_console', 'restart_preserves_admin', 'consistent_backup'],
    'restore': ['backup_integrity', 'restored_runtime_integrity', 'private_mount', 'database_restore', 'business_equal',
                'restored_flow', 'source_preserved', 'recovery_budget'],
    'offline': ['restored_runtime_integrity', 'private_network', 'external_unreachable', 'offline_login',
                'offline_web_assets', 'offline_browser_console', 'offline_import'],
}


class DeploymentError(RuntimeError):
    pass


def require(condition, message):
    if not condition:
        raise DeploymentError(message)


def write_json(path, data):
    with Path(path).open('x', encoding='utf-8') as out:
        os.chmod(path, 0o600)
        json.dump(data, out, ensure_ascii=False, indent=2)
        out.write('\n')


def check_owner(root=BASE, expected_root=BASE):
    root = Path(root)
    require(root == Path(expected_root) and root.resolve() == root, 'Wrong qualification root')
    info = root.stat()
    require(stat.S_IMODE(info.st_mode) == 0o700 and info.st_uid == os.getuid(), 'Root owner/mode mismatch')
    marker = root / '.qualification-owner'
    require(not marker.is_symlink() and marker.is_file() and marker.stat().st_uid == os.getuid() and marker.read_text().strip() == OWNER,
            'Qualification owner marker mismatch')


def inside(path, parent=BASE):
    path = Path(path)
    require(path.is_absolute() and path == path.resolve() and path.is_relative_to(parent)
            and path != parent, 'Path outside owned root or contains symlink')
    return path


def create_private(path):
    path = Path(path)
    require(not path.exists() and not path.is_symlink(), 'Refusing an existing directory')
    path.mkdir(mode=0o700)
    return path


def file_map(root):
    root = Path(root)
    require(root.is_dir() and not root.is_symlink(), 'File tree is absent or a symlink')
    result = {}
    for path in sorted(root.rglob('*')):
        require(not path.is_symlink(), 'Symlinks are forbidden in backup/tool trees')
        if path.is_dir():
            continue
        require(path.is_file(), 'Nonregular file in backup/tool tree')
        result[str(path.relative_to(root))] = {'size': path.stat().st_size, 'sha256': digest(path)}
    return result


def verify_files(root, expected):
    require(isinstance(expected, dict) and file_map(root) == expected, 'File inventory differs: missing, extra, or changed bytes')


def verify_restored_runtime(backup, inventory_path, expected_sha, runtime):
    require(digest(inventory_path) == expected_sha, 'Fixed backup inventory changed')
    inventory = json.loads(Path(inventory_path).read_text())
    require('database.dump' in inventory, 'Database dump missing from fixed backup inventory')
    verify_files(backup, inventory)
    expected = {name[len('runtime/'):]: value for name, value in inventory.items() if name.startswith('runtime/')}
    require(expected, 'Fixed runtime inventory absent')
    verify_files(runtime, expected)
    return len(expected)


def cleanup_resources(*resources):
    errors = []
    for resource in resources:
        if resource is not None:
            try:
                resource.stop()
            except BaseException as exc:
                errors.append(type(resource).__name__ + ': ' + type(exc).__name__)
    return not errors, errors


def check_candidate(manifest, source_root):
    require(all(re.fullmatch(r'[0-9a-f]{40}', str(manifest.get(k, '')))
                for k in ('product_sha', 'test_sha')) and bool(manifest.get('run_id')), 'Missing frozen source/run identity')
    errors = validate_candidate(manifest, source_root, manifest.get('binary', ''))
    require(not errors, '; '.join(errors))


def process_identity(pid):
    proc = Path('/proc') / str(pid)
    raw = (proc / 'stat').read_text()
    fields = raw[raw.rfind(')') + 2:].split()
    exe = (proc / 'exe').resolve(strict=True)
    return {'pid': pid, 'starttime': int(fields[19]), 'cwd': str((proc / 'cwd').resolve(strict=True)),
            'exe': str(exe), 'exe_sha256': digest(exe),
            'argv_sha256': hashlib.sha256((proc / 'cmdline').read_bytes()).hexdigest()}


def verify_process(saved):
    try:
        require(process_identity(saved['pid']) == saved, 'Process identity changed; refusing to signal')
    except (OSError, KeyError) as exc:
        raise DeploymentError('Registered process identity unavailable; refusing to signal') from exc


def descendant_pids(pid):
    found = []
    pending = [pid]
    while pending:
        parent = pending.pop()
        tasks = Path('/proc') / str(parent) / 'task'
        if not tasks.exists():
            continue
        for path in tasks.glob('*/children'):
            try:
                children = path.read_text().split()
            except FileNotFoundError:
                continue
            for value in children:
                child = int(value)
                if child not in found:
                    found.append(child)
                    pending.append(child)
    return found


def child_processes(pid):
    found = []
    for child in descendant_pids(pid):
        try:
            found.append(process_identity(child))
        except FileNotFoundError:
            continue
    return found


def live_process(pid):
    try:
        raw = (Path('/proc') / str(pid) / 'stat').read_text()
        return raw[raw.rfind(')') + 2:].split()[0] != 'Z'
    except FileNotFoundError:
        return False


def namespaces():
    return {name: os.readlink('/proc/self/ns/' + name) for name in ('mnt', 'net', 'pid')}


def assert_namespace(host, current, offline=False):
    require(current.get('mnt') and current['mnt'] != host.get('mnt'), 'Mount namespace still equals host')
    if 'pid' in host:
        require(current.get('pid') and current['pid'] != host['pid'], 'PID namespace still equals host')
    if offline:
        require(current.get('net') and current['net'] != host.get('net'), 'Network namespace still equals host')


def validate_phase(row):
    errors = []
    start, end = row.get('started_ns'), row.get('ended_ns')
    if type(start) is not int or type(end) is not int or end < start:
        return ['Phase timestamps incomplete']
    required = row.get('required')
    if row.get('phase') not in PHASES or required != PHASES[row['phase']]:
        return ['Declared assertions differ from the fixed phase contract']
    checks = row.get('assertions', [])
    if not required or not isinstance(checks, list):
        return ['Required assertions absent']
    names = [a.get('name') for a in checks]
    if names != required or len(names) != len(set(names)):
        errors.append('Required assertions missing, duplicated, or unordered')
    for item in checks:
        a, b = item.get('started_ns'), item.get('ended_ns')
        if type(a) is not int or type(b) is not int or not start <= a <= b <= end:
            errors.append('Assertion timestamps incomplete')
        if item.get('status') != 'PASS' or not item.get('evidence') or not re.fullmatch(r'[0-9a-f]{64}', item.get('evidence_sha256', '')):
            errors.append('Assertion failed or evidence incomplete')
    if row.get('cleanup_complete') is not True:
        errors.append('Owned processes cleanup incomplete')
    return errors


class Evidence:
    def __init__(self, phase, output, common):
        self.output = create_private(output)
        self.row = {**common, 'phase': phase, 'scope': 'control-plane-subqualification',
                    'offline_installation': 'NOT_RUN', 'started_ns': time.time_ns(),
                    'required': PHASES[phase], 'assertions': [], 'cleanup_complete': False}

    @contextmanager
    def check(self, name):
        item = {'name': name, 'started_ns': time.time_ns()}
        log = self.output / (name + '.log')
        payload = {}
        try:
            yield payload
            item['status'] = 'PASS'
        except BaseException:
            item['status'] = 'FAIL'
            payload['error'] = 'Assertion failed; inspect restricted process logs'
            raise
        finally:
            write_json(log, payload)
            item.update(ended_ns=time.time_ns(), evidence=log.name, evidence_sha256=digest(log))
            self.row['assertions'].append(item)

    def finish(self, cleanup, failure=None):
        self.row.update(ended_ns=time.time_ns(), cleanup_complete=cleanup)
        errors = validate_phase(self.row)
        for item in self.row['assertions']:
            path = self.output / item['evidence']
            if not path.is_file() or digest(path) != item['evidence_sha256']:
                errors.append('Raw assertion evidence missing or changed')
        if failure:
            errors.append(failure)
        self.row.update(status='FAIL' if errors else 'PASS', problems=errors,
                        count=len(self.row['assertions']), failed=sum(a['status'] != 'PASS' for a in self.row['assertions']))
        write_json(self.output / 'result.json', self.row)
        return not errors


def command(argv, log, *, env=None, cwd=None, timeout=90, expected=0):
    with Path(log).open('xb') as out:
        os.chmod(log, 0o600)
        result = subprocess.run([str(x) for x in argv], stdout=out, stderr=subprocess.STDOUT,
                                env=env, cwd=cwd, timeout=timeout)
    require(result.returncode == expected, 'Command failed; restricted command output retained')


def query_sources(pg, log):
    command([pg.pg_bin / 'psql', '-XAt', '-c',
             "SELECT COALESCE(json_agg(t ORDER BY t.iso_id,t.role),'[]'::json) FROM iso_sources t"], log, env=pg.env)
    result = json.loads(Path(log).read_text())
    require(isinstance(result, list) and result, 'Persisted source rows absent')
    return result


class PostgreSQL:
    def __init__(self, root, pg_bin):
        self.root, self.pg_bin, self.identity = create_private(root), Path(pg_bin), None
        self.proc, self.log, self.start_attempted = None, None, False
        self.data = self.root / 'data'
        self.sock = create_private(self.root / 's')
        require(len(str(self.sock).encode()) + 17 < 108, 'PostgreSQL socket path too long; use a shorter work-root name')
        self.port = 20000 + secrets.randbelow(30000)
        self.env = os.environ.copy()
        for key in list(self.env):
            if key.startswith('PG') or key == 'LOBER_PG_DSN':
                self.env.pop(key)
        self.env.update(PGHOST=str(self.sock), PGPORT=str(self.port), PGUSER=pwd.getpwuid(os.getuid()).pw_name,
                        PGDATABASE='postgres', PGCONNECT_TIMEOUT='5')
        self.dsn = f'host={self.sock} port={self.port} dbname=postgres user={self.env["PGUSER"]} sslmode=disable'

    def start(self):
        require(os.getuid() != 0, 'initdb must never run as root')
        command([self.pg_bin / 'initdb', '-D', self.data, '--auth-local=trust', '--auth-host=reject', '--no-locale', '--encoding=UTF8'], self.root / 'initdb.log', env=self.env)
        self.start_attempted = True
        self.log = (self.root / 'postgres.log').open('xb')
        previous = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGTERM, signal.SIGINT, signal.SIGALRM})
        try:
            self.proc = subprocess.Popen([str(self.pg_bin / 'postgres'), '-D', str(self.data), '-k', str(self.sock),
                '-p', str(self.port), '-c', 'listen_addresses=', '-c', 'unix_socket_permissions=0700'],
                cwd=self.data, env=self.env, stdout=self.log, stderr=self.log, start_new_session=True,
                preexec_fn=lambda: signal.pthread_sigmask(signal.SIG_SETMASK, previous))
            self.register()
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, previous)
        self.wait_ready()

    def register(self):
        require(self.proc is not None and self.proc.poll() is None, 'Owned postgres process exited before registration')
        pid = self.proc.pid
        self.identity = process_identity(pid)
        require(Path(self.identity['exe']) == self.pg_bin / 'postgres', 'Wrong PostgreSQL executable')
        require(Path(self.identity['cwd']) == self.data, 'Wrong PostgreSQL working directory')
        args = (Path('/proc') / str(pid) / 'cmdline').read_bytes().split(b'\0')
        require(str(self.data).encode() in args, 'PostgreSQL data directory mismatch')
        write_json(self.root / 'registered-process.json', {**self.identity, 'pg_data': str(self.data), 'socket': str(self.sock)})

    def wait_ready(self):
        deadline = time.monotonic() + 30
        while time.monotonic() < deadline:
            require(self.proc.poll() is None, 'Owned postgres exited during startup wait')
            result = subprocess.run([str(self.pg_bin / 'pg_isready'), '-q'], env=self.env, timeout=5)
            if result.returncode == 0:
                pid = int((self.data / 'postmaster.pid').read_text().splitlines()[0])
                require(pid == self.proc.pid, 'PostgreSQL PID file differs from held child')
                return
            time.sleep(.05)
        raise DeploymentError('PostgreSQL startup wait timed out')

    def stop(self):
        if self.proc is not None and self.proc.poll() is None:
            if self.identity is None:
                # 即使启动等待被中断，仍持有自己创建的直接子进程；不能猜别的PID。
                self.register()
            verify_process(self.identity)
            os.kill(self.proc.pid, signal.SIGINT)
            self.proc.wait(timeout=30)
        require(self.proc is None or self.proc.poll() is not None, 'Owned PostgreSQL exit unconfirmed')
        if self.start_attempted:
            pidfile = self.data / 'postmaster.pid'
            if pidfile.exists():
                pid = int(pidfile.read_text().splitlines()[0])
                require(not live_process(pid), 'Postmaster PID file still identifies a live process')
        if self.log and not self.log.closed:
            self.log.close()


class Service:
    def __init__(self, runtime, binary, pg, port=None):
        self.runtime, self.binary, self.pg = Path(runtime), Path(binary), pg
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            self.port = port or sock.getsockname()[1]
        self.url = f'http://127.0.0.1:{self.port}'
        self.proc, self.identity, self.log = None, None, None
        self.workers = []
        self.browser_runs = []
        self.sequence = 0

    def request(self, method, path, token=None, body=None, raw=None, headers=None, expected=200):
        heads = {'Content-Type': 'application/json', **(headers or {})}
        if token:
            heads['Authorization'] = 'Bearer ' + token
        data = raw if raw is not None else json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(self.url + '/console/v1' + path, method=method, data=data, headers=heads)
        try:
            response = urllib.request.build_opener(urllib.request.ProxyHandler({})).open(req, timeout=20)
        except urllib.error.HTTPError as exc:
            response = exc
        with response:
            result = json.loads(response.read())
            require(response.status == expected and result.get('code') == (0 if expected == 200 else expected),
                    f'HTTP/envelope did not equal expected {expected}')
            return result.get('data')

    def login(self, username, password):
        data = self.request('POST', '/auth/login', body={'username': username, 'password': password})
        require(isinstance(data.get('token'), str) and data['token'], 'Missing login token')
        return data['token']

    def start(self, reject=False):
        self.sequence += 1
        self.log = (self.runtime / f'service-{self.sequence}.log').open('xb')
        env = os.environ.copy()
        env.update(GIN_MODE='release', LOBER_PG_DSN=self.pg.dsn)
        self.proc = subprocess.Popen([str(self.binary), '-config', str(self.runtime / 'config.json')], cwd=self.runtime,
                                     env=env, stdout=self.log, stderr=self.log)
        if reject:
            require(self.proc.wait(timeout=20) != 0, 'Empty database accepted missing seedAdmin')
            self.log.close()
            self.proc = None
            return
        for _ in range(300):
            require(self.proc.poll() is None, 'Service exited during startup')
            try:
                self.request('POST', '/auth/login', body={}, expected=400)
                self.identity = process_identity(self.proc.pid)
                require(Path(self.identity['cwd']) == self.runtime and self.identity['exe_sha256'] == digest(self.binary), 'Service process identity differs')
                write_json(self.runtime / f'registered-service-{self.sequence}.json', self.identity)
                return
            except (OSError, urllib.error.URLError):
                time.sleep(.1)
        raise DeploymentError('Service readiness exceeded 30 seconds')

    def stop(self):
        if self.proc is not None and self.proc.poll() is None:
            if self.identity is None:
                self.identity = process_identity(self.proc.pid)
                require(self.identity['exe_sha256'] == digest(self.binary) and Path(self.identity['cwd']) == self.runtime,
                        'Unready service identity mismatch')
            verify_process(self.identity)
            self.workers = child_processes(self.proc.pid)
            write_json(self.runtime / f'registered-workers-{self.sequence}.json', self.workers)
            self.proc.terminate()
            try:
                self.proc.wait(timeout=30)
            except subprocess.TimeoutExpired:
                verify_process(self.identity)
                self.proc.kill()
                self.proc.wait(timeout=10)
        for child in self.workers:
            path = Path('/proc') / str(child['pid'])
            if path.exists():
                try:
                    still_same = process_identity(child['pid']) == child
                except FileNotFoundError:
                    still_same = False
                require(not still_same, 'Registered background job survived service stop; backup refused')
        self.workers = []
        self.proc, self.identity = None, None
        if self.log and not self.log.closed:
            self.log.close()


def fetch(service, path):
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
    with opener.open(service.url + path, timeout=20) as response:
        require(response.status == 200, 'Web asset HTTP status differs')
        return response.read()


def web_assets(service, inputs):
    assets = inputs['web_assets']
    require(isinstance(assets, dict) and '/index.html' in assets and len(assets) > 1, 'Real web asset map absent')
    for url, expected in assets.items():
        require(isinstance(url, str) and url.startswith('/') and not url.startswith('//') and '?' not in url and '..' not in url,
                'Invalid web asset URL')
        payload = fetch(service, url)
        require(hashlib.sha256(payload).hexdigest() == expected, 'Embedded web asset bytes differ from release manifest')
    home = fetch(service, '/')
    require(hashlib.sha256(home).hexdigest() == assets['/index.html'] and b'<script' in home and b'assets/' in home,
            'Home page is missing real application entry scripts')
    return {'count': len(assets), 'map_sha256': hashlib.sha256(json.dumps(assets, sort_keys=True).encode()).hexdigest()}


class BrowserRun:
    """独立收养进程只拥有一次Node/Chromium子树，失败也逐个核身份收尾。"""
    def __init__(self, inputs, output):
        self.inputs, self.output = inputs, Path(output)
        self.pid, self.identity, self.wait_status = None, None, None

    def run(self, data, timeout=180):
        previous = signal.pthread_sigmask(signal.SIG_BLOCK, {signal.SIGTERM, signal.SIGINT, signal.SIGALRM})
        try:
            self.pid = os.fork()
        except BaseException:
            signal.pthread_sigmask(signal.SIG_SETMASK, previous)
            raise
        if self.pid == 0:
            try:
                signal.pthread_sigmask(signal.SIG_SETMASK, previous)
                self.worker(data, timeout)
            except BaseException:
                path = self.output / 'browser-processes.json'
                if not path.exists():
                    write_json(path, {'cleanup_complete': False, 'error': 'Browser supervisor setup/collection failed'})
                os._exit(1)
            os._exit(0)
        try:
            self.identity = process_identity(self.pid)
            write_json(self.output / 'browser-supervisor.json', self.identity)
        finally:
            signal.pthread_sigmask(signal.SIG_SETMASK, previous)
        try:
            deadline = time.monotonic() + timeout + 45
            while time.monotonic() < deadline:
                pid, status = os.waitpid(self.pid, os.WNOHANG)
                if pid:
                    self.wait_status = status
                    break
                time.sleep(.05)
            require(self.wait_status is not None, 'Browser supervisor deadline exceeded')
        finally:
            self.stop()
        summary = json.loads((self.output / 'browser-processes.json').read_text())
        require(summary.get('returncode') == 0 and summary.get('cleanup_complete') is True,
                'Browser failed or owned process tree exit unconfirmed')
        require(summary.get('expected_browser_seen') is True, 'Frozen Chromium executable was not observed')
        return summary

    def worker(self, data, timeout):
        # 子进程成为孤儿时只会转给这个专用收养进程，不会混入旁边的哨兵/PG。
        libc = ctypes.CDLL(None, use_errno=True)
        require(libc.prctl(36, 1, 0, 0, 0) == 0, 'Cannot enable dedicated browser child adoption')
        signal.signal(signal.SIGTERM, interrupted)
        signal.signal(signal.SIGINT, interrupted)
        tracked, proc, error, returncode = {}, None, None, None
        cleanup_errors = []
        log = self.output / 'browser.log'
        def collect():
            for pid in descendant_pids(os.getpid()):
                if pid not in tracked and live_process(pid):
                    try:
                        identity = process_identity(pid)
                    except (FileNotFoundError, ProcessLookupError):
                        continue
                    tracked[pid] = identity
                    try:
                        with (self.output / 'browser-registry.jsonl').open('a') as registry:
                            registry.write(json.dumps(identity) + '\n')
                    except OSError as exc:
                        cleanup_errors.append('registry: ' + type(exc).__name__)
        try:
            with log.open('xb') as out:
                proc = subprocess.Popen([self.inputs['node'], self.inputs['ui_script'], self.inputs['playwright']],
                    stdin=subprocess.PIPE, stdout=out, stderr=subprocess.STDOUT,
                    cwd=self.output, env=browser_environment(self.inputs), start_new_session=True)
                proc.stdin.write(json.dumps(data).encode())
                proc.stdin.close()
                deadline = time.monotonic() + timeout
                while time.monotonic() < deadline:
                    collect()
                    returncode = proc.poll()
                    if returncode is not None:
                        break
                    time.sleep(.02)
                require(returncode is not None, 'Browser execution timed out')
        except BaseException as exc:
            error = type(exc).__name__
        finally:
            # 收尾时不再让第二个信号跳过其余独立资源。
            signal.signal(signal.SIGTERM, signal.SIG_IGN)
            signal.signal(signal.SIGINT, signal.SIG_IGN)
            for sig in (signal.SIGTERM, signal.SIGKILL):
                collect()
                for pid, saved in reversed(list(tracked.items())):
                    if live_process(pid):
                        try:
                            verify_process(saved)
                            os.kill(pid, sig)
                        except BaseException as exc:
                            cleanup_errors.append(type(exc).__name__)
                deadline = time.monotonic() + (5 if sig == signal.SIGTERM else 3)
                while time.monotonic() < deadline:
                    collect()
                    if not any(live_process(pid) for pid in tracked):
                        break
                    time.sleep(.05)
            if proc is not None:
                try:
                    proc.wait(timeout=2)
                except BaseException as exc:
                    cleanup_errors.append(type(exc).__name__)
            collect()
            remaining = [pid for pid in tracked if live_process(pid)]
            # Node回收后，这里仅回收已退出的被收养子进程，绝不向未登记PID发信号。
            while True:
                try:
                    pid, _ = os.waitpid(-1, os.WNOHANG)
                    if not pid:
                        break
                except ChildProcessError:
                    break
            seen = any(v['exe'] == self.inputs['browser_executable'] and
                       v['exe_sha256'] == self.inputs['browser_executable_sha256'] for v in tracked.values())
            write_json(self.output / 'browser-processes.json', {'returncode': returncode, 'error': error,
                'cleanup_complete': not cleanup_errors and not remaining, 'cleanup_errors': cleanup_errors,
                'remaining_pids': remaining, 'registered_count': len(tracked), 'expected_browser_seen': seen,
                'environment': browser_environment(self.inputs), 'uid': os.getuid(), 'namespaces': namespaces()})

    def stop(self):
        if self.pid is None:
            return
        if self.wait_status is None:
            try:
                pid, status = os.waitpid(self.pid, os.WNOHANG)
                if pid:
                    self.wait_status = status
            except ChildProcessError:
                raise DeploymentError('Browser supervisor exit status unavailable')
        if self.wait_status is None:
            verify_process(self.identity)
            os.kill(self.pid, signal.SIGTERM)
            deadline = time.monotonic() + 20
            while time.monotonic() < deadline:
                pid, status = os.waitpid(self.pid, os.WNOHANG)
                if pid:
                    self.wait_status = status
                    break
                time.sleep(.05)
        require(self.wait_status is not None, 'Browser supervisor cleanup unconfirmed')
        summary = self.output / 'browser-processes.json'
        require(summary.is_file() and json.loads(summary.read_text()).get('cleanup_complete') is True,
                'Browser child process cleanup incomplete')


def browser(service, inputs, password, output):
    runner = BrowserRun(inputs, output)
    service.browser_runs.append(runner)
    summary = runner.run({'url': service.url, 'password': password})
    log = output / 'browser.log'
    lines = log.read_text().splitlines()
    result = json.loads(lines[-1])
    require(result.get('status') == 'PASS' and result.get('count') == 4, 'Incomplete real browser evidence')
    return {'browser_log_sha256': digest(log), 'count': result['count'],
            'processes_sha256': digest(output / 'browser-processes.json'),
            'actual_browser_executable_sha256': inputs['browser_executable_sha256']}


def import_media(service, token, fixture, name, local=None):
    data = Path(fixture).read_bytes()
    source = {'kind': 'path', 'addr': str(local)} if local else {'kind': 'upload', 'size': len(data), 'sha256': hashlib.sha256(data).hexdigest()}
    item = service.request('POST', '/isos', token, {'name': name, 'family': 'linux', 'series': 'rocky', 'sources': [source]})
    path = '/isos/' + item['id']
    if not local:
        service.request('PUT', path + '/content', token, raw=data,
                        headers={'Content-Type': 'application/octet-stream', 'Content-Range': f'bytes 0-{len(data)-1}/{len(data)}'})
    deadline = time.monotonic() + 60
    while time.monotonic() < deadline:
        item = service.request('GET', path, token)
        require(item['state'] != 'failed', 'Synthetic control-plane media import failed')
        if item['state'] == 'ready':
            return item
        time.sleep(.1)
    raise DeploymentError('Synthetic control-plane import timed out')


def seed_business(service, password, fixture, runtime):
    token = service.login('admin', password)
    roles = ('viewer', 'operator', 'hook-author', 'hook-approver')
    tokens = {}
    for role in roles:
        username = 'enterprise_' + role.replace('-', '_')
        service.request('POST', '/users', token, {'username': username, 'role': role, 'password': password, 'displayName': role})
        tokens[role] = service.login(username, password)
    menus = service.request('GET', '/roles/menus', token)
    viewer = next(v for v in menus['roles'] if v['role'] == 'viewer')
    service.request('PUT', '/roles/viewer/menus', token, {'menuIds': ['hosts'], 'rev': viewer['rev']})
    service.request('POST', '/hosts', tokens['viewer'], {'sn': 'DENIED'}, expected=403)
    host = service.request('POST', '/hosts', token, {'sn': 'DEPLOY-' + secrets.token_hex(6).upper(), 'attrs': {'rack': 'restore-proof'}})
    template = service.request('POST', '/templates', tokens['hook-author'], {'name': 'deployment-review', 'family': 'linux', 'series': ['rocky'],
        'body': '# {{param:hostname}}\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n',
        'params': [{'name': 'hostname', 'label': 'hostname', 'type': 'hostname', 'default': 'node'}]})
    version_path = '/templates/' + template['id'] + '/versions/1'
    review = service.request('POST', version_path + '/submit?rev=' + str(template['rev']), tokens['hook-author'])
    service.request('POST', version_path + '/approve?rev=' + str(review['rev']), tokens['hook-author'], expected=403)
    approved = service.request('POST', version_path + '/approve?rev=' + str(review['rev']), tokens['hook-approver'])
    require(approved['state'] == 'approved', 'Separate approver did not approve version')
    sentinel = runtime / 'sources' / 'local-original.iso'
    shutil.copyfile(fixture, sentinel)
    sentinel.chmod(0o600)
    media = import_media(service, token, fixture, 'synthetic-control-plane-restore-only', local=sentinel)
    pending = service.request('POST', '/isos', token, {'name': 'unfinished-upload-preserved', 'family': 'linux', 'series': 'rocky',
        'sources': [{'kind': 'upload', 'size': Path(fixture).stat().st_size, 'sha256': digest(fixture)}]})
    tasks = service.request('POST', '/wizard/tasks', token, {'isoID': media['id'], 'templateID': template['id'], 'templateVersion': 1,
        'hosts': [{'sn': host['sn'], 'entered': {'tpl/hostname': 'restore-node'}}]})['tasks']
    require(len(tasks) == 1, 'Expected exactly one legitimate pending task')
    task_path = '/tasks/' + tasks[0]['id']
    task = service.request('GET', task_path, token)
    preview = service.request('POST', task_path + '/preview', token, {'firmware': 'uefi'})
    require(any(f['name'] == 'ks.cfg' for f in preview['files']), 'Task preview missing answer file')
    endpoints = ['/users', '/roles/menus', '/hosts/' + host['sn'], version_path,
                 '/isos/' + media['id'], '/isos/' + pending['id'], task_path,
                 '/isos/' + media['id'] + '/progress', '/audit?limit=500']
    snapshot = {path: service.request('GET', path, token) for path in endpoints}
    require(any(v.get('action') == 'host.create' and v.get('targetID') == host['sn'] for v in snapshot['/audit?limit=500']['items']), 'Host audit absent')
    return {'endpoints': snapshot, 'task_path': task_path, 'preview': preview,
            'sentinel': str(sentinel), 'sentinel_sha256': digest(sentinel), 'host_sn': host['sn'],
            'selection': {'isoID': media['id'], 'templateID': template['id'], 'templateVersion': 1}}


def prepare_inputs(manifest):
    inputs = manifest.get('deployment')
    require(isinstance(inputs, dict), 'deployment inputs absent from manifest')
    for key in ('synthetic_fixture', 'node', 'playwright', 'ui_script'):
        path = Path(inputs.get(key, ''))
        require(path.is_absolute() and path.is_file() and digest(path) == inputs.get(key + '_sha256'), 'Prepared input digest mismatch: ' + key)
    require(Path(inputs['ui_script']).name == 'ui.mjs', 'Browser contract must use existing ui.mjs')
    require(isinstance(inputs.get('tools_files'), dict), 'Prepared tools map absent')
    verify_files(inputs.get('tools', ''), inputs['tools_files'])
    require(isinstance(inputs.get('browser_files'), dict) and inputs['browser_files'], 'Frozen browser installation map absent')
    verify_files(inputs.get('browser_path', ''), inputs['browser_files'])
    executable = Path(inputs.get('browser_executable', ''))
    require(executable.is_absolute() and executable.is_relative_to(Path(inputs['browser_path'])) and executable.is_file()
            and digest(executable) == inputs.get('browser_executable_sha256'), 'Frozen browser executable mismatch')
    require(isinstance(inputs.get('web_assets'), dict) and len(inputs['web_assets']) > 1, 'Prepared web asset map absent')
    return inputs


def browser_environment(inputs):
    user = pwd.getpwuid(os.getuid())
    return {'HOME': user.pw_dir, 'USER': user.pw_name, 'LOGNAME': user.pw_name,
            'PATH': '/usr/local/bin:/usr/bin:/bin', 'LANG': 'C.UTF-8',
            'PLAYWRIGHT_BROWSERS_PATH': inputs['browser_path'], 'PLAYWRIGHT_SKIP_BROWSER_DOWNLOAD': '1'}


def drop_to_dev():
    require(os.geteuid() == 0, 'Privilege drop must start in root namespace helper')
    user = pwd.getpwnam('dev')
    os.initgroups(user.pw_name, user.pw_gid)
    os.setgid(user.pw_gid)
    os.setuid(user.pw_uid)
    require(os.getuid() == user.pw_uid and os.geteuid() != 0, 'Privilege drop failed')
    # setuid会清除同用户进程检查能力；降权完成后恢复dev普通进程的默认状态。
    require(ctypes.CDLL(None, use_errno=True).prctl(4, 1, 0, 0, 0) == 0,
            'Cannot restore dev process inspection after privilege drop')
    os.environ.clear()
    os.environ.update(HOME=user.pw_dir, USER=user.pw_name, LOGNAME=user.pw_name,
                      PATH='/usr/local/bin:/usr/bin:/bin', LANG='C.UTF-8')


def make_runtime(work, manifest, inputs, port, dsn):
    runtime = create_private(work / 'runtime')
    for name in ('staging', 'artifacts', 'smb', 'sources', 'release'):
        create_private(runtime / name)
    shutil.copytree(inputs['tools'], runtime / 'tools')
    shutil.copyfile(manifest['binary'], runtime / 'release' / 'lober')
    (runtime / 'release' / 'lober').chmod(0o700)
    shutil.copyfile(manifest['source_archive'], runtime / 'release' / 'source.tar')
    shutil.copyfile(inputs['synthetic_fixture'], runtime / 'sources' / 'prepared-synthetic.iso')
    require(digest(runtime / 'release' / 'lober') == manifest['binary_sha256'] and
            digest(runtime / 'release' / 'source.tar') == manifest['source_archive_sha256'], 'Copied release bytes differ from frozen candidate')
    require(digest(runtime / 'sources' / 'prepared-synthetic.iso') == inputs['synthetic_fixture_sha256'], 'Copied prepared fixture differs')
    verify_files(runtime / 'tools', inputs['tools_files'])
    config = {'listen': f'127.0.0.1:{port}', 'pg': {'dsn': dsn, 'maxConns': 8}, 'log': {'level': 'error'},
              'iso': {k: str(runtime / k) for k in ('staging', 'artifacts', 'smb', 'tools')}}
    config['iso'].update(localRoots=[str(runtime / 'sources')], workers=1, pollMS=50)
    config.update(machine={'enabled': True, 'keyFile': str(runtime / 'staging' / 'machine-signing.key')},
                  preview={'gateway': f'http://127.0.0.1:{port}', 'artifacts': f'http://127.0.0.1:{port}/artifacts'})
    write_json(runtime / 'config.json', config)
    return runtime, config


def fresh(args, manifest, inputs, common):
    work = create_private(args.work_root / 'fresh')
    evidence = Evidence('fresh', args.output / 'fresh', common)
    pg, service, cleanup, error = None, None, False, None
    try:
        with evidence.check('empty_database') as log:
            pg = PostgreSQL(work / 'pg', args.pg_bin)
            pg.start()
            command([args.pg_bin / 'psql', '-XAt', '-c', "SELECT count(*) FROM pg_tables WHERE schemaname NOT IN ('pg_catalog','information_schema')"], work / 'empty-query.log', env=pg.env)
            require((work / 'empty-query.log').read_text().strip() == '0', 'Dedicated database is not empty')
            log.update(pg_version=subprocess.check_output([str(args.pg_bin / 'postgres'), '--version'], text=True).strip(), pg_data=str(pg.data), socket=str(pg.sock), port=pg.port)
        with socket.socket() as sock:
            sock.bind(('127.0.0.1', 0))
            port = sock.getsockname()[1]
        runtime, config = make_runtime(work, manifest, inputs, port, pg.dsn)
        service = Service(runtime, runtime / 'release' / 'lober', pg, port)
        with evidence.check('seed_required') as log:
            service.start(reject=True)
            command([args.pg_bin / 'psql', '-XAt', '-c', 'SELECT count(*) FROM users'], work / 'no-default-user.log', env=pg.env)
            require((work / 'no-default-user.log').read_text().strip() == '0', 'Missing seedAdmin created default user')
            log.update(exit_was_failure=True, stored_default_users=0, query_sha256=digest(work / 'no-default-user.log'))
        with evidence.check('bootstrap_password') as log:
            seed = secrets.token_urlsafe(30)
            config['seedAdmin'] = seed
            # 配置是本轮自有文件；写入临时文件后原子替换，旧无口令配置仍留档。
            (runtime / 'config.json').rename(runtime / 'config-no-seed.json')
            write_json(runtime / 'config.json', config)
            service.start()
            token = service.login('admin', seed)
            service.request('GET', '/hosts', token, expected=403)
            password = secrets.token_urlsafe(30)
            service.request('POST', '/auth/password', token, {'currentPassword': seed, 'newPassword': password})
            service.request('GET', '/user/info', token, expected=401)
            service.request('POST', '/auth/login', body={'username': 'admin', 'password': seed}, expected=401)
            service.request('GET', '/hosts', service.login('admin', password))
            log.update(first_login_restricted=True, old_session_revoked=True)
        with evidence.check('web_assets') as log:
            log.update(web_assets(service, inputs))
        with evidence.check('business_seed') as log:
            expected = seed_business(service, password, inputs['synthetic_fixture'], runtime)
            write_json(work / 'expected.json', expected)
            write_json(work / 'credentials.json', {'password': password})
            log.update(expected_sha256=digest(work / 'expected.json'), synthetic_control_plane_only=True)
        with evidence.check('browser_console') as log:
            log.update(browser(service, inputs, password, evidence.output))
        with evidence.check('restart_preserves_admin') as log:
            service.stop()
            # 改seed也不能覆盖已经改密的管理员。
            config['seedAdmin'] = secrets.token_urlsafe(30)
            (runtime / 'config.json').rename(runtime / 'config-first-start.json')
            write_json(runtime / 'config.json', config)
            service.start()
            token = service.login('admin', password)
            service.request('POST', '/auth/login', body={'username': 'admin', 'password': config['seedAdmin']}, expected=401)
            users = service.request('GET', '/users', token)
            require(users == expected['endpoints']['/users'], 'Restart changed administrator/user rows')
            log['stored_users_equal'] = True
        with evidence.check('consistent_backup') as log:
            service.stop()
            backup = create_private(args.work_root / 'backup')
            sources = query_sources(pg, work / 'backup-source-rows.log')
            require(any(v['path'] == expected['sentinel'] for v in sources), 'Expected absolute local source path absent')
            write_json(backup / 'source-rows.json', sources)
            command([args.pg_bin / 'pg_dump', '--format=custom', '--no-owner', '--no-privileges', '--file=' + str(backup / 'database.dump')], work / 'dump.log', env=pg.env)
            pg.stop()
            shutil.copytree(runtime, backup / 'runtime')
            shutil.copyfile(work / 'expected.json', backup / 'expected.json')
            shutil.copyfile(work / 'credentials.json', backup / 'credentials.json')
            shutil.copyfile(args.manifest, backup / 'manifest.json')
            inventory = file_map(backup)
            write_json(args.work_root / 'backup-files.json', inventory)
            verify_files(backup, inventory)
            require(any(k.endswith('machine-signing.key') for k in inventory), 'Machine signing key missing from complete backup')
            write_json(args.work_root / 'state.json', {'logical_runtime': str(runtime), 'port': port,
                'old_runtime_inode': runtime.stat().st_ino, 'backup_sha256': digest(args.work_root / 'backup-files.json'),
                'manifest_sha256': common['manifest_sha256']})
            log.update(files=len(inventory), bytes=sum(v['size'] for v in inventory.values()), inventory_sha256=digest(args.work_root / 'backup-files.json'))
    except BaseException as exc:
        error = type(exc).__name__ + ': ' + str(exc) if isinstance(exc, DeploymentError) else type(exc).__name__
    finally:
        cleanup, cleanup_errors = cleanup_resources(*(service.browser_runs if service else []), service, pg)
        if cleanup_errors:
            error = (error or '') + '; ' + '; '.join(cleanup_errors)
        return evidence.finish(cleanup, error)


def restore_or_offline(args, manifest, inputs, common, phase, payload):
    work = Path(payload['stage_work'])
    evidence = Evidence(phase, args.output / phase, common)
    pg, service, error, cleanup = None, None, None, False
    disaster = payload['disaster_ns']
    try:
        require(json.loads((args.work_root / (phase + '-disaster.json')).read_text())['declared_ns'] == disaster,
                'Disaster declaration timestamp differs from durable original')
        state = json.loads((args.work_root / 'state.json').read_text())
        logical = Path(state['logical_runtime'])
        current = namespaces()
        expected = json.loads((args.work_root / 'backup' / 'expected.json').read_text())
        password = json.loads((args.work_root / 'backup' / 'credentials.json').read_text())['password']
        def verify_copy(log):
            require(os.path.samefile(logical, work / 'runtime'), 'Logical runtime is not the new restored copy')
            count = verify_restored_runtime(args.work_root / 'backup', args.work_root / 'backup-files.json',
                                           state['backup_sha256'], logical)
            log.update(files=count, fixed_inventory_sha256=state['backup_sha256'], before_pg_or_service_start=True)
        if phase == 'restore':
            with evidence.check('backup_integrity') as log:
                verify_files(args.work_root / 'backup', json.loads((args.work_root / 'backup-files.json').read_text()))
                require(digest(args.work_root / 'backup-files.json') == state['backup_sha256'], 'Backup inventory replaced')
                log['inventory_sha256'] = state['backup_sha256']
            with evidence.check('restored_runtime_integrity') as log:
                verify_copy(log)
            with evidence.check('private_mount') as log:
                assert_namespace(payload['host_namespaces'], current)
                require(logical.stat().st_ino != state['old_runtime_inode'] and os.path.samefile(logical, work / 'runtime'), 'Old actual runtime still visible')
                require(not os.access(args.work_root / 'fresh' / 'pg', os.R_OK), 'Old PostgreSQL directory still readable')
                mounts = Path('/proc/self/mountinfo').read_text()
                require(' shared:' not in mounts, 'Namespace mount propagation is not private')
                with (evidence.output / 'mountinfo.log').open('x') as raw:
                    raw.write(mounts)
                log.update(host=payload['host_namespaces'], current=current, bind_target=str(logical), bind_source=str(work / 'runtime'), old_actual_hidden=True)
        else:
            with evidence.check('restored_runtime_integrity') as log:
                verify_copy(log)
            with evidence.check('private_network') as log:
                assert_namespace(payload['host_namespaces'], current, offline=True)
                links = json.loads(subprocess.check_output(['ip', '-j', 'link'], text=True))
                routes = json.loads(subprocess.check_output(['ip', '-j', 'route', 'show', 'table', 'all'], text=True))
                routes6 = json.loads(subprocess.check_output(['ip', '-j', '-6', 'route', 'show', 'table', 'all'], text=True))
                require([v['ifname'] for v in links] == ['lo'] and not any(v.get('dst') == 'default' for v in routes + routes6), 'Offline namespace has external interface/default route')
                log.update(host=payload['host_namespaces'], current=current, links=links, routes=routes, ipv6_routes=routes6)
            with evidence.check('external_unreachable') as log:
                attempts = []
                for address in ('1.1.1.1', '8.8.8.8'):
                    with socket.socket() as sock:
                        sock.settimeout(3)
                        code = sock.connect_ex((address, 443))
                    require(code != 0, 'Offline namespace reached an external address')
                    attempts.append({'address': address, 'port': 443, 'errno': code})
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
                try:
                    with opener.open('https://www.postgresql.org/', timeout=5):
                        raise DeploymentError('Official external website unexpectedly reachable')
                except (OSError, urllib.error.URLError) as exc:
                    log.update(attempts=attempts, official_url='https://www.postgresql.org/', official_error=type(exc).__name__)
        pg = PostgreSQL(work / 'pg', args.pg_bin)
        pg.start()
        def database_restore():
            command([args.pg_bin / 'pg_restore', '--no-owner', '--no-privileges', '--exit-on-error', '--dbname=postgres', args.work_root / 'backup' / 'database.dump'], work / 'restore.log', env=pg.env)
        if phase == 'restore':
            with evidence.check('database_restore') as log:
                database_restore()
                sources = query_sources(pg, work / 'restored-source-rows.log')
                require(sources == json.loads((args.work_root / 'backup' / 'source-rows.json').read_text()), 'Absolute source fields changed during restore')
                log.update(new_pg_data=str(pg.data), new_socket=str(pg.sock), no_business_sql_rewrite=True)
        else:
            database_restore()
        service = Service(logical, logical / 'release' / 'lober', pg, state['port'])
        require(digest(service.binary) == manifest['binary_sha256'], 'Restored release binary differs from frozen candidate')
        # 备份日志保留；新运行日志使用从未存在的序号。
        service.sequence = 100 if phase == 'restore' else 200
        service.start()
        token = service.login('admin', password)
        if phase == 'restore':
            with evidence.check('business_equal') as log:
                for path, value in expected['endpoints'].items():
                    actual = service.request('GET', path, token)
                    require(actual == value, 'Restored business data differs: ' + path.split('?')[0])
                preview = service.request('POST', expected['task_path'] + '/preview', token, {'firmware': 'uefi'})
                require(preview == expected['preview'], 'Restored preview files/bytes differ')
                viewer = service.login('enterprise_viewer', password)
                service.request('POST', '/hosts', viewer, {'sn': 'FORBIDDEN'}, expected=403)
                require(service.request('GET', '/user/info', viewer)['role'] == 'viewer', 'Restored viewer role differs')
                log.update(endpoint_count=len(expected['endpoints']), task_snapshot_equal=True, preview_equal=True)
            with evidence.check('restored_flow') as log:
                host = service.request('POST', '/hosts', token, {'sn': 'RECOVERED-' + secrets.token_hex(6).upper(), 'attrs': {}})
                created = service.request('POST', '/wizard/tasks', token, {**expected['selection'], 'hosts': [{'sn': host['sn'], 'entered': {'tpl/hostname': 'new-after-restore'}}]})
                require(len(created['tasks']) == 1 and created['tasks'][0]['state'] == 'pending', 'Restored representative flow failed')
                log['legitimate_new_task'] = created['tasks'][0]['id']
            with evidence.check('source_preserved') as log:
                require(digest(expected['sentinel']) == expected['sentinel_sha256'], 'Restored local source byte digest differs')
                scratch = import_media(service, token, logical / 'sources' / 'prepared-synthetic.iso', 'restore-cleanup-local-source', local=Path(expected['sentinel']))
                service.request('DELETE', '/isos/' + scratch['id'] + '?rev=' + str(scratch['rev']), token)
                service.request('GET', '/isos/' + scratch['id'], token, expected=404)
                # 实际删除后再重启，让持久化清理日志重新检查本地原件。
                service.stop()
                service.start()
                token = service.login('admin', password)
                require(digest(expected['sentinel']) == expected['sentinel_sha256'], 'Background cleanup removed source sentinel')
                log.update(sentinel_sha256=expected['sentinel_sha256'], original_logical_path_unchanged=True, actual_local_iso_deleted_and_service_restarted=True)
            with evidence.check('recovery_budget') as log:
                elapsed = (time.time_ns() - disaster) / 1e9
                require(0 <= elapsed <= 3600, 'Full recovery exceeded 60 minute budget')
                log.update(disaster_declared_ns=disaster, all_assertions_complete_ns=time.time_ns(), rto_seconds=elapsed, successful_backup_point_loss=0)
        else:
            with evidence.check('offline_login') as log:
                service.request('GET', '/hosts', token)
                log['actual_admin_login'] = True
            with evidence.check('offline_web_assets') as log:
                log.update(web_assets(service, inputs))
            with evidence.check('offline_browser_console') as log:
                log.update(browser(service, inputs, password, evidence.output))
            with evidence.check('offline_import') as log:
                item = import_media(service, token, logical / 'sources' / 'prepared-synthetic.iso', 'offline-synthetic-control-plane-only')
                log.update(iso_id=item['id'], state=item['state'], synthetic_control_plane_only=True)
    except BaseException as exc:
        error = type(exc).__name__ + ': ' + str(exc) if isinstance(exc, DeploymentError) else type(exc).__name__
    finally:
        cleanup, cleanup_errors = cleanup_resources(*(service.browser_runs if service else []), service, pg)
        if cleanup_errors:
            error = (error or '') + '; ' + '; '.join(cleanup_errors)
        return evidence.finish(cleanup, error)


def privileged_namespace(payload_file):
    # 这里只允许固定运行根和两个固定阶段；不会在主挂载空间执行mount。
    require(os.getuid() == 0, 'Namespace setup requires sudo unshare')
    payload_file = inside(Path(payload_file), BASE / 'operations' / 'deployment')
    dev = pwd.getpwnam('dev')
    require(payload_file.stat().st_uid == dev.pw_uid and stat.S_IMODE(payload_file.stat().st_mode) == 0o600,
            'Namespace request owner/mode mismatch')
    payload = json.loads(payload_file.read_text())
    phase = payload['phase']
    require(phase in ('restore', 'offline'), 'Unsupported privileged phase')
    work_root = inside(Path(payload['work_root']), BASE / 'operations' / 'deployment')
    logical = work_root / 'fresh' / 'runtime'
    stage_work = work_root / phase
    require(Path(payload['stage_work']) == stage_work and stage_work.is_dir() and stage_work.stat().st_uid == dev.pw_uid,
            'Unexpected namespace stage root')
    assert_namespace(payload['host_namespaces'], namespaces(), offline=phase == 'offline')
    for path in (logical, stage_work / 'runtime', work_root / 'fresh' / 'pg'):
        inside(path)
        require(path.is_dir() and path.stat().st_uid == dev.pw_uid, 'Mount target/source not owned by dev')
    subprocess.run(['mount', '--make-rprivate', '/'], check=True)
    subprocess.run(['mount', '-t', 'proc', '-o', 'nosuid,nodev,noexec', 'proc', '/proc'], check=True)
    require(os.getpid() == 1, 'Namespace worker is not PID 1 after private proc mount')
    subprocess.run(['mount', '--bind', str(stage_work / 'runtime'), str(logical)], check=True)
    subprocess.run(['mount', '-t', 'tmpfs', '-o', 'size=4096,mode=000', 'qualification-hide-old-pg', str(work_root / 'fresh' / 'pg')], check=True)
    if phase == 'offline':
        subprocess.run(['ip', 'link', 'set', 'lo', 'up'], check=True)
    drop_to_dev()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    signal.signal(signal.SIGALRM, interrupted)
    signal.alarm(840)
    args = argparse.Namespace(work_root=work_root, output=Path(payload['output']), pg_bin=Path(payload['pg_bin']))
    manifest = json.loads(Path(payload['manifest']).read_text())
    check_owner()
    check_candidate(manifest, payload['source_root'])
    require(manifest['files'].get('tests/lobertest/enterprise/gate.py') == digest(gate.__file__), 'Loaded candidate checker differs from frozen source')
    inputs = prepare_inputs(manifest)
    return 0 if restore_or_offline(args, manifest, inputs, payload['common'], phase, payload) else 1


def main():
    if len(sys.argv) == 3 and sys.argv[1] == '--_namespace':
        return privileged_namespace(sys.argv[2])
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phase', choices=('fresh', 'restore', 'offline', 'all'), required=True)
    for name in ('manifest', 'source-root', 'output', 'work-root', 'pg-bin'):
        parser.add_argument('--' + name, type=Path, required=True)
    args = parser.parse_args()
    signal.signal(signal.SIGTERM, interrupted)
    signal.signal(signal.SIGINT, interrupted)
    check_owner()
    require(os.getuid() != 0, 'Start qualification as dev, never root')
    for name in ('output', 'work_root'):
        inside(getattr(args, name), BASE / 'operations' / 'deployment')
    require(args.work_root != args.output and not args.work_root.is_relative_to(args.output)
            and not args.output.is_relative_to(args.work_root), 'Work/output directories must be separate siblings')
    manifest = json.loads(args.manifest.read_text())
    check_candidate(manifest, args.source_root)
    require(manifest['files'].get('tests/lobertest/enterprise/gate.py') == digest(gate.__file__), 'Loaded candidate checker differs from frozen source')
    inputs = prepare_inputs(manifest)
    require(args.pg_bin.resolve() == Path('/usr/lib/postgresql/16/bin'), 'Only inventoried PostgreSQL 16 tools permitted')
    require(shutil.disk_usage(BASE).free >= 1024 ** 3, 'Less than 1 GiB actual free disk')
    for name in ('initdb', 'pg_isready', 'postgres', 'pg_dump', 'pg_restore', 'psql'):
        require((args.pg_bin / name).is_file(), 'Required PostgreSQL tool absent')
    common = {'run_id': manifest['run_id'], 'product_sha': manifest['product_sha'], 'test_sha': manifest['test_sha'],
              'manifest_sha256': digest(args.manifest), 'binary_sha256': digest(manifest['binary']),
              'executor_sha256': digest(__file__), 'gate_sha256': digest(gate.__file__),
              'prepared_inputs': {k: inputs[k + '_sha256'] for k in ('synthetic_fixture', 'node', 'playwright', 'ui_script', 'browser_executable')},
              'browser_installation_map_sha256': hashlib.sha256(json.dumps(inputs['browser_files'], sort_keys=True).encode()).hexdigest()}
    lock = BASE / 'leases' / 'heavy.lock'
    with lock.open('a') as lease:
        fcntl.flock(lease, fcntl.LOCK_EX | fcntl.LOCK_NB)
        if args.phase in ('fresh', 'all'):
            create_private(args.work_root)
            create_private(args.output)
            if not fresh(args, manifest, inputs, common):
                return 1
        else:
            require(args.work_root.is_dir() and args.output.is_dir(), 'Fresh phase state absent')
        phases = ('restore', 'offline') if args.phase == 'all' else (args.phase,) if args.phase != 'fresh' else ()
        for phase in phases:
            disaster_ns = time.time_ns()
            write_json(args.work_root / (phase + '-disaster.json'), {'phase': phase, 'declared_ns': disaster_ns})
            require(shutil.disk_usage(BASE).free >= 1024 ** 3, 'Less than 1 GiB free disk before namespace phase')
            state = json.loads((args.work_root / 'state.json').read_text())
            require(state['manifest_sha256'] == common['manifest_sha256'], 'Fresh state candidate differs')
            verify_files(args.work_root / 'backup', json.loads((args.work_root / 'backup-files.json').read_text()))
            stage_work = create_private(args.work_root / phase)
            shutil.copytree(args.work_root / 'backup' / 'runtime', stage_work / 'runtime')
            payload = {'phase': phase, 'work_root': str(args.work_root), 'stage_work': str(stage_work),
                       'output': str(args.output), 'pg_bin': str(args.pg_bin), 'manifest': str(args.manifest),
                       'source_root': str(args.source_root), 'common': common, 'host_namespaces': namespaces(),
                       'disaster_ns': disaster_ns}
            path = args.work_root / (phase + '-namespace-request.json')
            write_json(path, payload)
            argv = ['sudo', '-n', 'unshare', '--mount', '--pid', '--fork', '--kill-child=SIGTERM']
            if phase == 'offline':
                argv.append('--net')
            argv += [sys.executable, str(Path(__file__).resolve()), '--_namespace', str(path)]
            command(argv, stage_work / 'namespace-launch.log', timeout=900)
            result = json.loads((args.output / phase / 'result.json').read_text())
            require(result.get('phase') == phase and not validate_phase(result) and result['status'] == 'PASS', 'Namespace phase incomplete')
            for check in result['assertions']:
                log = args.output / phase / check['evidence']
                require(log.is_file() and digest(log) == check['evidence_sha256'], 'Namespace assertion evidence incomplete')
    return 0


def interrupted(signum, frame):
    raise DeploymentError('Qualification interrupted by signal ' + str(signum))


if __name__ == '__main__':
    os.umask(0o077)
    try:
        raise SystemExit(main())
    except (DeploymentError, OSError, ValueError, subprocess.SubprocessError) as exc:
        # 不回显可能带有口令、参数或来源地址的系统异常。
        print(json.dumps({'status': 'FAIL', 'error': type(exc).__name__, 'detail': str(exc) if isinstance(exc, DeploymentError) else 'Restricted details retained'}))
        raise SystemExit(1)
