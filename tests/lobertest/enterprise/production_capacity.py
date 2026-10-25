"""本轮专用容量驱动器。检查器自检、探查、容量、持续运行、真实装机分别记账。

只用标准库。正式运行需要精确冻结清单、完整介质、独立服务、独立库和重任务锁。
历史数据用真实接口创建后取消；取消不代表装机成功。原件只写一次。
"""
import argparse
import collections
import concurrent.futures
import contextlib
import hashlib
import http.client
import ipaddress
import json
import math
import os
from pathlib import Path
import random
import re
import signal
import socket
import statistics
import subprocess
import sys
import threading
import time
import urllib.parse
import urllib.request
import uuid

BASE = Path('/home/dev/lober-production-qualification-20261025')
CATEGORIES = ('host_list', 'host_detail', 'task_list', 'task_detail', 'create_read',
              'upload_import', 'event_reconnect', 'idle_observation')
READ_CATEGORIES = CATEGORIES[:4]


class LoadWindowClosed(Exception):
    """固定负载时间已到；不再开始下一项业务调用。"""


FORMAL_PROFILE = {
    'version': 1, 'mode': 'qualification', 'seed': 20261025,
    'hosts': 10000, 'historical_tasks': 100000, 'console_sessions': 20,
    'sse_connections': 50, 'artifact_downloads': 4, 'normal_seconds': 1800,
    'burst_multiplier': 2, 'burst_seconds': 300, 'recovery_seconds': 600,
    'soak_seconds': 86400, 'sample_seconds': 60, 'sample_gap_seconds_max': 65,
    'startup_seconds_max': 60, 'read_p95_seconds_max': 1.0,
    'unexpected_server_error_ratio_strict_max': .001,
    'rss_median_ratio_max': 1.2, 'window_seconds': 1800,
    'idle_fd_tolerance': 8, 'idle_connection_tolerance': 8,
    'root_free_bytes_min': 1073741824, 'data_free_bytes_min': 2147483648,
    'mem_available_bytes_min': 134217728, 'swap_free_bytes_min': 268435456,
    'product_rss_bytes_max': 1073741824, 'driver_rss_bytes_max': 536870912,
    'request_timeout_seconds': 20, 'import_timeout_seconds': 120,
    'download_timeout_seconds': 120, 'request_period_seconds': 1,
    'read_activity_fraction_min': .9,
    'create_period_seconds': 60, 'import_period_seconds': 1800,
    'event_reconnect_seconds': 120, 'idle_period_seconds': 600,
    'idle_seconds': 10, 'session_refresh_seconds': 1800,
    'seed_probe_hosts': 10, 'seed_probe_tasks': 100,
    'seed_disk_safety_multiplier': 3, 'artifact_size_bytes_min': 67108864,
    'categories': list(CATEGORIES),
    'cleanup_seconds': 30, 'session_expiry_margin_seconds': 60,
}


def sha(path):
    h = hashlib.sha256()
    with Path(path).open('rb') as f:
        for block in iter(lambda: f.read(1024*1024), b''): h.update(block)
    return h.hexdigest()


def schema_for(run_id):
    return 'capacity_' + hashlib.sha256(run_id.encode()).hexdigest()[:16]


def valid_schema(schema, run_id):
    return bool(run_id) and schema == schema_for(run_id)


def profile_problems(profile):
    # 容差也冻结。执行后不能靠改容差救回失败结果。
    return [] if profile == FORMAL_PROFILE else ['profile differs from frozen formal contract']


def write_once(path, value):
    path = Path(path)
    fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
    with os.fdopen(fd, 'w', encoding='utf-8') as f:
        json.dump(value, f, ensure_ascii=False, indent=2)
        f.write('\n'); f.flush(); os.fsync(f.fileno())


class Journal:
    def __init__(self, path, context=None):
        self.file = os.fdopen(os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600), 'w')
        self.lock = threading.RLock()
        self.context = context or (lambda: {})

    def add(self, **row):
        with self.lock:
            self.file.write(json.dumps({'utc_unix': time.time(), 'at': time.monotonic(),
                                       **self.context(), **row}, separators=(',', ':')) + '\n')
            self.file.flush()

    def close(self):
        with self.lock:
            self.file.flush(); os.fsync(self.file.fileno()); self.file.close()


def validate_url(url, allowed):
    u = urllib.parse.urlsplit(url)
    try: addr = ipaddress.ip_address(u.hostname or '')
    except ValueError: raise ValueError('only explicit private IP targets are permitted') from None
    if (u.scheme != 'http' or u.username or u.password or u.fragment or u.port == 8080
            or not (addr.is_loopback or addr.is_private) or addr.is_unspecified
            or u.hostname not in allowed):
        raise ValueError('target outside frozen private service/install network')
    return u


def error_summary(envelope, status):
    message = envelope.get('message', '')
    known = '第 7 层（状态机）尚未实现，暂不能取消或重试任务'
    category = 'task_control_unavailable' if message == known else {
        401: 'authentication_rejected', 403: 'permission_rejected', 409: 'conflict', 422: 'request_rejected'}.get(status, 'http_error' if status >= 400 else 'success')
    data = envelope.get('data')
    codes = [p.get('code') for p in data.get('problems', []) if isinstance(p, dict)
             and isinstance(p.get('code'), str) and re.fullmatch('[a-z][a-z0-9_]{0,80}', p['code'])] if isinstance(data, dict) else []
    return {'category': category, 'envelope_code': envelope.get('code'), 'problem_codes': codes,
            'message_sha256': hashlib.sha256(str(message).encode()).hexdigest()}


def process_identity(pid):
    p = Path('/proc') / str(pid)
    fields = (p / 'stat').read_text().rsplit(')', 1)[1].split()
    return {'pid': pid, 'start_ticks': fields[19], 'cwd': str((p/'cwd').resolve(strict=True)),
            'exe': str((p/'exe').resolve(strict=True)), 'exe_sha256': sha(p/'exe'),
            'boot_id': Path('/proc/sys/kernel/random/boot_id').read_text().strip()}


def same_process(registered):
    try:
        actual = process_identity(registered['pid'])
        return all(actual[k] == registered.get(k) for k in actual)
    except (OSError, KeyError, ValueError): return False


def private_file(path, root):
    p = Path(path)
    if p.is_symlink() or not p.resolve(strict=True).is_relative_to(root):
        raise ValueError('private file outside owned work root')
    st = p.stat()
    if st.st_uid != os.getuid() or st.st_mode & 0o077: raise ValueError('private file must be owned and mode 0600')
    return p


def read_rows(path):
    with Path(path).open() as f:
        for line in f:
            yield json.loads(line)


def bound_rows(path, identity, phase, phase_id):
    binding = {**identity, 'phase': phase, 'phase_id': phase_id}
    for row in read_rows(path):
        if any(row.get(k) != v for k, v in binding.items()): raise ValueError('raw record crossed phase/run identity')
        yield row


def percentile(values, q):
    values = sorted(values)
    return values[max(0, math.ceil(q*len(values))-1)] if values else None


def stage_bundle_problems(expected, identity, row, command, registry, lifecycle):
    errors = []
    binding = {**identity, 'phase': expected, 'phase_id': row.get('phase_id')}
    if not re.fullmatch('[a-f0-9]{32}', row.get('phase_id', '')): errors.append('missing independent phase identity')
    for name, record in [('result', row), ('command', command), ('registry', registry)]:
        if any(record.get(k) != v for k, v in binding.items()): errors.append(name+' identity/phase mismatch')
    argv = command.get('argv', [])
    if '--phase' not in argv or argv[argv.index('--phase')+1:argv.index('--phase')+2] != [expected]:
        errors.append('original command phase mismatch')
    inputs = row.get('inputs', {})
    if not inputs or inputs != command.get('inputs'): errors.append('original command input paths mismatch')
    for key in ('manifest', 'profile', 'output', 'work-root'):
        if '--'+key not in argv or argv[argv.index('--'+key)+1:argv.index('--'+key)+2] != [inputs.get(key)]:
            errors.append('original command input mismatch: '+key)
    driver = registry.get('processes', {}).get('driver', {})
    if not driver or driver != row.get('driver_identity') or driver.get('cwd') != command.get('cwd'):
        errors.append('registered driver/command identity mismatch')
    if not lifecycle or lifecycle[0].get('status') != 'RUNNING' or lifecycle[-1].get('status') != 'COMPLETED':
        errors.append('lifecycle incomplete')
    if any(any(r.get(k) != v for k, v in binding.items()) for r in lifecycle): errors.append('lifecycle binding mismatch')
    if not row.get('cleanup_complete') or row.get('residuals'): errors.append('cleanup not proven complete')
    started, ended = row.get('started_at'), row.get('ended_at')
    if not isinstance(started, (float, int)) or not isinstance(ended, (float, int)) or ended <= started:
        errors.append('phase start/end missing or reversed')
    elif (command.get('started_at') != started or not lifecycle or lifecycle[0].get('at', 0) < started
          or lifecycle[-1].get('at', math.inf) > ended): errors.append('phase command/lifecycle time mismatch')
    if expected != 'seed':
        window = row.get('window', {})
        begins = [r for r in lifecycle if r.get('kind') == 'measurement_begin']
        ends = [r for r in lifecycle if r.get('kind') == 'measurement_end']
        duration = {'normal': 1800, 'burst': 900, 'soak': 86400}.get(expected, math.inf)
        if (not begins or len(begins) != 1 or len(ends) != 1 or begins[0].get('window') != window
                or ends[0].get('window_id') != window.get('id') or not window.get('id')
                or window.get('end', 0)-window.get('start', 0) != duration
                or not isinstance(started, (int, float)) or not isinstance(ended, (int, float))
                or not started <= window.get('start', 0) < window.get('end', math.inf) <= ended
                or ends[0].get('at', 0) < window.get('end', math.inf)):
            errors.append('formal window/lifecycle mismatch')
    return errors


def seed_evidence_problems(dataset, requests, database):
    hosts, created, cancelled = set(), set(), set()
    for r in requests:
        if not r.get('ok') or r.get('category') != 'seed': continue
        if r.get('method') == 'POST' and r.get('path') == '/hosts' and r.get('object_id'): hosts.add(r['object_id'])
        if r.get('method') == 'POST' and r.get('path') == '/wizard/tasks' and r.get('object_id'): created.add(r['object_id'])
        if r.get('method') == 'POST' and r.get('path', '').endswith('/cancel') and r.get('object_id'): cancelled.add(r['object_id'])
    reads, sql_rows = [], []
    for r in database:
        if r.get('kind') == 'dataset_observation' and r.get('exit') == 0: reads.append(r.get('observation'))
        if r.get('exit') == 0 and "'snapshot_digest'" in r.get('sql', ''):
            with contextlib.suppress(ValueError): sql_rows.append(json.loads(r['stdout']))
    return ([] if len(hosts) == dataset.get('hosts') == 10000 and len(created) == 100000
            and created == cancelled and reads and reads[-1] == dataset and sql_rows
            and all(sql_rows[-1].get(k) == dataset.get(k) for k in ('hosts', 'historical_tasks', 'snapshot_digest', 'task_ids_digest', 'host_ids_digest'))
            and hashlib.md5(''.join(sorted(hosts)).encode()).hexdigest() == dataset.get('host_ids_digest')
            and hashlib.md5(''.join(sorted(created)).encode()).hexdigest() == dataset.get('task_ids_digest')
            else ['seed raw API operations/query counts do not prove original dataset'])


def invariant_chain_problems(baseline, current):
    keys = ('snapshot_digest', 'history_digest', 'source_digest', 'historical_tasks')
    return [] if all(baseline.get(k) and current.get(k) == baseline[k] for k in keys) else ['frozen seed history/source baseline changed']


def audit_time_problems(task, entries):
    needed = ['task.create']+(['task.cancel'] if task['state'] == 'cancelled' else [])
    previous, errors = task['createdAt'], []
    for action in needed:
        selected = [r for r in entries if r.get('action') == action]
        if (len(selected) != 1 or selected[0].get('targetKind') != 'task'
                or selected[0].get('targetID') != task['id'] or selected[0].get('actor') != task['createdBy']):
            errors.append('audit action/object/actor not unique'); continue
        if selected[0].get('at', -1) < previous: errors.append('audit time precedes task/create audit')
        previous = selected[0].get('at', previous)
    return errors


def activity_evidence_problems(row, requests):
    window = row['window']; begin, end = window['start'], window['end']
    active_end = min(end, begin+300) if row['phase'] == 'burst' else end
    clients = 40 if row['phase'] == 'burst' else 20
    counts, errors = collections.Counter(), []
    for r in requests:
        if r.get('category') not in READ_CATEGORIES: continue
        if r.get('window_id') != window['id']: continue
        at, session = r.get('at', -1), r.get('session', -1)
        if not begin <= at < active_end or session not in range(clients): errors.append('read outside formal window/session'); continue
        if r.get('ok') and r.get('ended_at', at+r.get('seconds', 0)) <= active_end:
            counts[(int((at-begin)//60), session, r['category'])] += 1
    for minute in range(math.ceil((active_end-begin)/60)):
        span = min(60, active_end-begin-minute*60)
        # 固定空闲周期允许10秒；其他分钟仍要求90%的读节奏。
        idle = 10 if minute % 10 == 0 else 0
        minimum = math.floor(max(0, span-idle)*.9/4)
        if any(counts[(minute, session, c)] < minimum for session in range(clients) for c in READ_CATEGORIES):
            errors.append('per-minute/session read activity insufficient'); break
    return list(dict.fromkeys(errors))


def event_lane_problems(rows, window, lanes, reconnect_seconds=120):
    rows = list(rows)
    errors = lane_evidence_problems(rows, window, lanes)
    opens_by_lane, events_by_connection = collections.defaultdict(list), collections.defaultdict(list)
    for row in rows:
        if row.get('kind') == 'open': opens_by_lane[row.get('lane')].append(row)
        elif row.get('kind') == 'event_cursor': events_by_connection[row.get('connection')].append(row)
    for lane in range(lanes):
        opens = sorted(opens_by_lane[lane], key=lambda r: r['at'])
        if not opens: continue
        if any(b['at']-a['at'] > reconnect_seconds+5 for a, b in zip(opens, opens[1:])) or opens[-1]['at'] < window['end']-reconnect_seconds-5:
            errors.append('event scheduled reconnect missing')
        cursor = 0
        for opened in opens:
            if opened.get('after_seq') != cursor: errors.append('event reconnect cursor mismatch')
            for event in events_by_connection[opened['connection']]:
                if event.get('seq') != cursor+1 or event.get('task_id') != opened.get('task_id'): errors.append('event cursor gap/duplicate/task mismatch')
                cursor = event.get('seq', cursor)
    return list(dict.fromkeys(errors))


def periodic_jobs_problems(row, requests, lifecycle, imports):
    w = row['window']; start, end = w['start'], w['end']
    active_end = min(end, start+300) if row['phase'] == 'burst' else end
    errors, creation, idle = [], {}, set()
    for r in requests:
        if r.get('window_id') != w['id'] or not start <= r.get('at', -1) < active_end or not r.get('ok'): continue
        if r.get('category') == 'create_read':
            key = (int((r['at']-start)//60), r.get('object_id'))
            creation.setdefault(key, set()).add(r.get('method'))
        if r.get('category') == 'idle_observation' and r.get('seconds', 0) >= 10:
            idle.add((int((r['at']-start)//600), r.get('session')))
    for minute in range(math.ceil((active_end-start)/60)):
        if not any(k[0] == minute and k[1] and {'POST', 'GET'} <= methods for k, methods in creation.items()):
            errors.append('create/read cycle absent'); break
    clients = 40 if row['phase'] == 'burst' else 20
    if any((period, session) not in idle for period in range(math.ceil((active_end-start)/600)) for session in range(clients)):
        errors.append('idle observation cycle absent')
    completed = [r for r in lifecycle if r.get('kind') == 'import_verified' and r.get('window_id') == w['id']]
    if len(completed) != len(imports): errors.append('full import cycle count mismatch')
    for index, item in enumerate(imports):
        matches = [r for r in completed if r.get('index') == index and r.get('input_sha256') == item['sha256']
                   and start+index*1800 <= r.get('started_at', -1) < min(active_end, start+index*1800+60)
                   and r.get('at', math.inf) <= min(active_end, start+index*1800+1800)
                   and r.get('uploaded_bytes') == item.get('size') and r.get('state') == 'ready']
        if len(matches) != 1: errors.append('missing/wrong/out-of-window verified import '+str(index))
    return list(dict.fromkeys(errors))


def lane_evidence_problems(rows, window, lanes, progress=False):
    errors, connections, advances = [], {}, collections.defaultdict(list)
    for r in rows:
        key = (r.get('lane'), r.get('connection'))
        if r.get('kind') == 'open': connections[key] = [r['at'], None]
        elif r.get('kind') == 'close' and key in connections: connections[key][1] = r['at']
        elif r.get('kind') == 'progress': advances[r.get('lane')].append((r['at'], r.get('bytes', 0)))
    for lane in range(lanes):
        intervals = sorted(v for k, v in connections.items() if k[0] == lane and v[1] is not None)
        cursor = window['start']
        for begin, end in intervals:
            if begin <= cursor and end > cursor: cursor = end
        if cursor < window['end']: errors.append('lane connection coverage gap '+str(lane))
        if progress:
            points = sorted(advances[lane])
            if any(b[0] <= a[0] or b[1] < a[1] for a, b in zip(points, points[1:])):
                errors.append('lane byte/time sequence invalid')
            for start in range(0, math.ceil((window['end']-window['start'])/60)*60, 60):
                a, b = window['start']+start, min(window['end'], window['start']+start+60)
                earlier = [n for t, n in points if t <= a]
                later = [n for t, n in points if t <= b]
                if not earlier or not later or later[-1] <= earlier[-1]:
                    errors.append('lane stopped transferring '+str(lane)); break
    return list(dict.fromkeys(errors))


class EventDecoder:
    """解析HTTP分块和事件帧；分块终止与TCP仍连接是两件事。"""
    def __init__(self, chunked, after=0):
        if not chunked: raise ValueError('event stream must use explicit chunked framing')
        self.raw, self.text, self.remaining, self.last = b'', b'', None, after

    def feed(self, block):
        self.raw += block; payload = b''
        while True:
            if self.remaining is None:
                if b'\r\n' not in self.raw: break
                header, self.raw = self.raw.split(b'\r\n', 1)
                self.remaining = int(header.split(b';', 1)[0], 16)
                if self.remaining == 0: raise EOFError('HTTP event stream completed')
                if self.remaining > 16*1024*1024: raise ValueError('event chunk too large')
            if len(self.raw) < self.remaining+2: break
            if self.raw[self.remaining:self.remaining+2] != b'\r\n': raise ValueError('invalid HTTP chunk terminator')
            payload += self.raw[:self.remaining]; self.raw = self.raw[self.remaining+2:]; self.remaining = None
        self.text += payload.replace(b'\r\n', b'\n'); events = []
        if len(self.text) > 16*1024*1024: raise ValueError('unterminated event too large')
        while b'\n\n' in self.text:
            frame, self.text = self.text.split(b'\n\n', 1)
            fields = {}
            for line in frame.split(b'\n'):
                if b':' in line:
                    k, v = line.split(b':', 1); fields[k] = v.lstrip()
            if b'data' not in fields: continue
            event = json.loads(fields[b'data'])
            seq = int(fields[b'id'])
            if event['seq'] != seq or seq != self.last+1: raise ValueError('event cursor gap/duplicate/mismatch')
            self.last = seq; events.append(event)
        return events


def verify_path_coverage(required, declared):
    wanted = {str(Path(p).resolve()) for p in required.values()}
    supplied = {str(Path(p).resolve()) for p in declared}
    if not wanted <= supplied: raise ValueError('actual write path missing from resource_paths')


def plan_storage(samples, devices, remaining_batches, wal_reserve):
    if len(samples) < 3 or remaining_batches < 0: raise ValueError('warmup and two measured increments required')
    result = collections.Counter()
    for field, role, safety in [('relation_bytes', 'data', 3), ('log_bytes', 'logs', 2)]:
        deltas = [samples[i+1][field]-samples[i][field] for i in range(len(samples)-1)]
        if any(n < 0 for n in deltas): raise ValueError('negative storage increment')
        result[devices[role]] += max(deltas)*remaining_batches*safety
    result[devices['wal']] += wal_reserve
    return dict(result)


class AuthSession:
    """持有者和事件流共享一个匿名代次；旧流交接后才退出旧令牌。"""
    def __init__(self, login, logout, record, clock=time.time, margin=60):
        self.login, self.logout, self.record, self.clock, self.margin = login, logout, record, clock, margin
        self.lock = threading.RLock(); self.generations = {}; self.generation = 0

    def current(self):
        with self.lock:
            old = self.generations.get(self.generation)
            if old is None or old['renew_at'] <= self.clock():
                response = self.login()
                if response['expiresAt'] <= self.clock(): raise ValueError('already expired login')
                if not isinstance(response['token'], str) or not response['token']: raise ValueError('empty login token')
                ttl = response['expiresAt']-self.clock()
                self.generation += 1
                self.generations[self.generation] = {'token': response['token'], 'expires': response['expiresAt'], 'refs': 0,
                    'renew_at': self.clock()+min(1800, ttl-min(self.margin, max(.05, ttl/4)))}
                self.record(kind='session_open', generation=self.generation, expires_at=response['expiresAt'])
                self.retire()
            return self.generations[self.generation]['token']

    def acquire(self):
        with self.lock:
            token = self.current(); gen = self.generation
            self.generations[gen]['refs'] += 1
            return token, gen

    def release(self, generation):
        with self.lock:
            self.generations[generation]['refs'] -= 1; self.retire()

    def retire(self):
        for gen, item in list(self.generations.items()):
            if gen != self.generation and not item['refs']:
                if item['expires'] > self.clock(): self.logout(item['token'])
                self.record(kind='session_retired', generation=gen, expired=item['expires'] <= self.clock())
                del self.generations[gen]

    def close(self):
        with self.lock:
            if any(g['refs'] for g in self.generations.values()): raise RuntimeError('event streams still own session generation')
            self.generation = -1; self.retire()


def installation_problems(report, identity, root, contract=None):
    """消费Task5原件与冻结检查器；单机工具没有第二目标合同时继续阻塞。"""
    errors = []
    try:
        if report.get('status') != 'PASS' or report.get('synthetic') is not False:
            raise ValueError('actual non-synthetic installation result required')
        if not contract or not contract.get('installation_checkers') or not contract.get('installation_targets'):
            raise ValueError('reviewed common checker/target contract missing')
        for k in ('run_id', 'product_sha', 'test_sha', 'binary_sha256', 'run_identity_sha256'):
            if report.get(k) != identity.get(k) or not identity.get(k): raise ValueError('installation identity mismatch')
        import importlib.util
        groups = []
        task_sets = []
        for name in ('parallel', 'serial'):
            cases = report[name]
            if len(cases) != 2: raise ValueError('two actual cases required for each comparison')
            seen_guests, seen_tasks, seen_sns, intervals = set(), set(), set(), []
            for case in cases:
                if case.get('status') != 'PASS' or case.get('synthetic') is not False:
                    raise ValueError('actual non-synthetic case required')
                checker_ref = case['checker']; checker = (root/checker_ref['path']).resolve(strict=True)
                if (not checker.is_relative_to(BASE) or sha(checker) != checker_ref['sha256']
                        or contract['installation_checkers'].get(str(checker)) != checker_ref['sha256']):
                    raise ValueError('external checker not frozen/reviewed in common identity')
                spec = importlib.util.spec_from_file_location('frozen_capacity_install_checker_'+checker_ref['sha256'], checker)
                module = importlib.util.module_from_spec(spec); spec.loader.exec_module(module)
                for k in ('run_id', 'product_sha', 'test_sha', 'binary_sha256', 'run_identity_sha256'):
                    if case.get(k) != identity[k]: raise ValueError('case identity mismatch')
                files = case['files']
                loaded = {}
                for label in ('result', 'manifest', 'qualification', 'timeline', 'target', 'guest', 'repeat_guest'):
                    ref = files[label]; p = (root/ref['path']).resolve(strict=True)
                    if not p.is_relative_to(BASE) or sha(p) != ref['sha256']: raise ValueError('case original absent/changed')
                    loaded[label] = json.loads(p.read_text())
                result, evidence, target, timeline = (loaded[k] for k in ('result', 'qualification', 'target', 'timeline'))
                if result.get('manifest_sha256') != files['manifest']['sha256']:
                    raise ValueError('Task5 original manifest digest mismatch')
                if any(loaded['manifest'].get(k) != identity[k] for k in ('run_id', 'product_sha', 'test_sha', 'binary_sha256', 'run_identity_sha256')):
                    raise ValueError('Task5 frozen manifest/common identity mismatch')
                if result.get('phase') != 'qualify' or result.get('status') != 'PASS': raise ValueError('real qualification result missing')
                if any(result.get(k) != identity[k] for k in ('run_id', 'product_sha', 'test_sha', 'binary_sha256')): raise ValueError('Task5 result identity mismatch')
                if result.get('executor_sha256') != checker_ref['sha256']: raise ValueError('Task5 checker identity mismatch')
                if target['uuid'] != case['target_uuid'] or evidence['task']['id'] != case['task_id']: raise ValueError('target/task mismatch')
                if contract['installation_targets'].get(target['uuid']) != target: raise ValueError('target not frozen by Task4 common contract')
                if target['uuid'] in seen_guests or case['task_id'] in seen_tasks: raise ValueError('duplicate actual machine/task')
                if target['sn'] in seen_sns: raise ValueError('duplicate machine serial')
                if evidence['guest']['smbios_serial'] != target['sn'] or evidence['task']['hostSN'] != target['sn']: raise ValueError('guest identity mismatch')
                # 当前Task5检查器固定单个目标常量。第二机必须由Task5核准的通用合同提供，不修改其全局常量绕过。
                if target['sn'] != getattr(module, 'SN', None): raise ValueError('Task5 checker lacks reviewed contract for this target')
                if module.check_qualification(evidence, False).get('status') != 'PASS': raise ValueError('external checker rejected actual guest evidence')
                if loaded['guest'] != evidence['guest'] or loaded['repeat_guest'] != evidence['repeat']['guest']:
                    raise ValueError('guest acceptance data differs from actual raw guest output')
                for filename, digest in result.get('files', {}).items():
                    basedir = (root/files['result']['path']).resolve().parent
                    p = (basedir/filename).resolve()
                    if not p.is_relative_to(basedir) or not p.is_file() or sha(p) != digest: raise ValueError('Task5 referenced raw original changed')
                for label in ('qualification', 'timeline', 'guest', 'repeat_guest'):
                    if files[label]['sha256'] not in result['files'].values(): raise ValueError('original not bound by actual Task5 result')
                if any(evidence['raw'][k] not in result['files'].values() for k in ('install_capture_sha256', 'repeat_capture_sha256')):
                    raise ValueError('actual network captures missing from result originals')
                start, end = timeline['install_started_at'], timeline['guest_accepted_at']
                if not start < end or timeline['task_id'] != case['task_id'] or timeline['target_uuid'] != target['uuid']:
                    raise ValueError('real installation timeline invalid')
                starts = [e['at'] for e in evidence['events'] if e['kind'] == 'machine.installing']
                ready = [e['at'] for e in evidence['events'] if e['kind'] == 'machine.ready']
                if not starts or not ready or start != min(starts) or not start < max(ready) <= end:
                    raise ValueError('timeline does not match real committed install events')
                intervals.append((start, max(ready))); seen_guests.add(target['uuid']); seen_tasks.add(case['task_id']); seen_sns.add(target['sn'])
            groups.append(intervals)
            task_sets.append(seen_tasks)
        if task_sets[0] & task_sets[1]: raise ValueError('serial comparison reuses parallel task')
        if min(e for _, e in groups[0])-max(s for s, _ in groups[0]) <= 0: raise ValueError('installations were not concurrent')
        if min(e for _, e in groups[1])-max(s for s, _ in groups[1]) > 0: raise ValueError('serial baseline was concurrent')
    except (KeyError, ValueError, OSError, TypeError, AttributeError, ImportError) as error:
        errors.append('real concurrent install evidence refused: '+type(error).__name__)
    return errors


def request_summary(rows):
    # 长测请求逐行汇总，只保留浮点耗时。不会把上百万个请求字典同时放进内存。
    result = {'count': 0, 'server_errors': 0, 'other_errors': 0, 'categories': {}}
    categories = {c: {'count': 0, 'successes': 0, 'errors': 0, 'values': []} for c in CATEGORIES}
    for r in rows:
        if r.get('category') not in CATEGORIES: continue
        if r.get('category') != 'idle_observation':
            result['count'] += 1
            result['server_errors'] += bool(r.get('server_error'))
            result['other_errors'] += not r.get('ok') and not r.get('server_error')
        c = categories[r['category']]
        c['count'] += 1; c['successes'] += bool(r.get('ok')); c['errors'] += not r.get('ok')
        c['values'].append(r['seconds'])
    for name, c in categories.items():
        values = c.pop('values')
        c.update(p50=percentile(values, .5), p95=percentile(values, .95), p99=percentile(values, .99))
        result['categories'][name] = c
    return result


def download_summary(rows, size, expected_sha):
    workers = set()
    count, singles, parallel = 0, [], []
    for d in rows:
        if not (d.get('verified') and d.get('bytes') == size and d.get('sha256') == expected_sha
                and d.get('seconds', 0) > 0): continue
        if d.get('kind') == 'load': workers.add(d.get('worker')); count += 1
        elif d.get('kind') == 'single_baseline': singles.append(d)
        elif d.get('kind') == 'parallel_baseline': parallel.append(d)
    parallel_start = min((d['elapsed']-d['seconds'] for d in parallel), default=0)
    parallel_end = max((d['elapsed'] for d in parallel), default=0)
    overlap = min((d['elapsed'] for d in parallel), default=0)-max((d['elapsed']-d['seconds'] for d in parallel), default=0)
    return {'verified_workers': len(workers), 'verified_count': count,
            'single_verified': len(singles), 'parallel_verified': len({d['worker'] for d in parallel}) if overlap > 0 else 0,
            'single_bytes_per_second': singles[0]['bytes']/singles[0]['seconds'] if singles else None,
            'parallel_bytes_per_second': sum(d['bytes'] for d in parallel)/(parallel_end-parallel_start)
            if parallel_end > parallel_start else None}


def check_phase(profile, identity, row, samples, requests, downloads, expected_phase=None):
    problems = profile_problems(profile)
    def require(ok, msg):
        if not ok: problems.append(msg)
    require(all(row.get(k) == v for k, v in identity.items()), 'frozen identity mismatch')
    phase = row.get('phase')
    if expected_phase is not None: require(phase == expected_phase, 'directory/phase substitution rejected')
    duration = {'normal': 1800, 'burst': 900, 'soak': 86400}.get(phase)
    require(duration is not None, 'unknown qualification phase')
    require(row.get('mode') == 'qualification', 'probe cannot qualify')
    require(row.get('status') == 'COMPLETED' and row.get('reason') == 'duration_elapsed'
            and not row.get('interrupted'), 'phase incomplete/interrupted')
    require(duration is not None and row.get('elapsed_seconds', 0) >= duration
            and row.get('requested_seconds') == duration, 'duration insufficient')
    require(row.get('startup_seconds', math.inf) <= profile['startup_seconds_max'], 'client startup budget exceeded')
    require(row.get('sampling_errors') == 0 and row.get('worker_errors') == 0, 'sampling or worker failures')
    require(row.get('owner_verified') is True and row.get('process_identity_verified') is True, 'owner/process mismatch')
    dataset = row.get('dataset', {})
    require(dataset.get('hosts', 0) >= 10000 and dataset.get('historical_tasks', 0) >= 100000
            and dataset.get('violations') == 0, 'dataset insufficient or inconsistent')
    before, after = row.get('invariants_before', {}), row.get('invariants_after', {})
    require(before.get('violations') == 0 and after.get('violations') == 0
            and before.get('snapshot_digest') and before.get('snapshot_digest') == after.get('snapshot_digest'),
            'database invariant/snapshot failure')
    baseline = row.get('download_baseline', {})
    require(baseline.get('single_verified', 0) >= 1 and baseline.get('parallel_verified', 0) >= 4,
            'single/four download digest comparison absent')
    require(row.get('artifact_size', 0) >= profile['artifact_size_bytes_min'], 'real source too small/absent')
    require(row.get('unique_sessions', 0) >= (40 if phase == 'burst' else 20), 'independent sessions absent')
    require(bool(samples), 'no samples')
    times = [s.get('elapsed', -1) for s in samples]
    require(bool(times) and 0 <= times[0] <= 1 and times[-1] >= (duration or math.inf), 'sample boundaries missing')
    require(all(0 < b-a <= profile['sample_gap_seconds_max'] for a, b in zip(times, times[1:])), 'sample gap/order invalid')
    for a, b in zip(samples, samples[1:]):
        if phase != 'burst' or b.get('elapsed', 0) < 300:
            require(b.get('download_bytes', 0) > a.get('download_bytes', 0), 'download progress absent between samples')
    for s in samples:
        t = s.get('elapsed', -1)
        recovery = phase == 'burst' and t >= 300
        multiplier = 2 if phase == 'burst' and not recovery else 1
        if not recovery and t < (duration or 0):
            require(s.get('sessions', 0) >= 20*multiplier and s.get('streams', 0) >= 50*multiplier
                    and s.get('downloads', 0) >= 4*multiplier, 'client class absent at sample')
        require(not s.get('sample_errors'), 'resource sample failed')
        require(s.get('oom_kill') == samples[0].get('oom_kill'), 'OOM observed')
        for key in ('root_free_bytes', 'data_free_bytes', 'mem_available_bytes', 'swap_free_bytes'):
            require(s.get(key, -1) >= profile[key+'_min'], key + ' below stop threshold')
        for role in ('product', 'database', 'driver'):
            p = s.get('processes', {}).get(role, {})
            require(p.get('alive') is True and p.get('identity_ok') is True, 'process lost or replaced: '+role)
            if role in ('product', 'driver'):
                require(p.get('rss_bytes', math.inf) <= profile[role+'_rss_bytes_max'], role+' RSS exceeds stop threshold')
    stats = requests if isinstance(requests, dict) else request_summary(requests)
    require(stats['count'] > 0, 'zero request denominator')
    require(all(stats['categories'][c]['successes'] > 0 for c in CATEGORIES), 'required successful request category missing')
    require(stats['other_errors'] == 0, 'unexpected business/client error')
    if stats['count']:
        require(stats['server_errors']/stats['count'] < .001, 'server error budget exceeded')
    for c in READ_CATEGORIES:
        require(stats['categories'][c]['p95'] is not None and stats['categories'][c]['p95'] <= 1,
                c+' p95 exceeds 1s or absent')
        active_seconds = 300 if phase == 'burst' else (duration or 0)
        clients = 40 if phase == 'burst' else 20
        require(stats['categories'][c]['count'] >= active_seconds*clients*.9/4,
                c+' activity insufficient for fixed mixed workload')
    good = downloads if isinstance(downloads, dict) else download_summary(downloads, row.get('artifact_size'), row.get('artifact_sha256'))
    require(good['verified_workers'] >= (8 if phase == 'burst' else 4), 'full verified downloads absent')
    require(good['single_verified'] >= 1 and good['parallel_verified'] >= 4,
            'raw single/four download comparison missing or wrong digest')
    if phase == 'soak' and samples:
        for role in ('product', 'database', 'driver'):
            first = [s['processes'][role] for s in samples if s['elapsed'] <= 1800]
            last = [s['processes'][role] for s in samples if s['elapsed'] >= 86400-1800]
            if not first or not last: problems.append('RSS windows incomplete'); continue
            rss1 = statistics.median(p['rss_bytes'] for p in first)
            require(rss1 > 0 and statistics.median(p['rss_bytes'] for p in last) <= rss1*1.2,
                    role+' first/final RSS growth exceeds 20%')
            for field, tolerance in (('fds', 8), ('connections', 8)):
                require(statistics.median(p[field] for p in last) <= statistics.median(p[field] for p in first)+tolerance,
                        role+' '+field+' accumulates')
    if phase == 'burst' and samples:
        idle = row.get('idle_baseline', {})
        tail = samples[-1]
        require(tail.get('queue') == 0, 'queue did not drain')
        for role in ('product', 'database'):
            for field, tolerance in (('fds', 8), ('connections', 8)):
                require(tail.get('processes', {}).get(role, {}).get(field, math.inf)
                        <= idle.get(role, {}).get(field, -math.inf)+tolerance, 'burst failed idle recovery')
    return list(dict.fromkeys(problems))


class Driver:
    def __init__(self, args):
        if sys.platform != 'linux': raise ValueError('all tests and execution must run on lober VM')
        self.args, self.root = args, args.work_root.resolve(strict=True)
        if self.root == BASE or not self.root.is_relative_to(BASE): raise ValueError('work root outside owned subtree')
        if self.root.stat().st_uid != os.getuid() or self.root.stat().st_mode & 0o077:
            raise ValueError('work root must be owned and mode 0700')
        self.manifest = json.loads(private_file(args.manifest, BASE).read_text())
        self.profile = json.loads(private_file(args.profile, BASE).read_text())
        if profile_problems(self.profile): raise ValueError('profile differs from fixed contract')
        self.c = self.manifest['capacity']
        self.run_id = self.manifest['run_id']
        if self.c['run_id'] != self.run_id or not valid_schema(self.c['database']['schema'], self.run_id):
            raise ValueError('run/schema ownership mismatch')
        self.schema = self.c['database']['schema']
        owner = private_file(BASE/'.qualification-owner', BASE)
        if sha(owner) != self.c['owner_sha256']: raise ValueError('base owner identity mismatch')
        schema_owner = json.loads(private_file(self.c['database']['owner_file'], self.root).read_text())
        if schema_owner != {'run_id': self.run_id, 'schema': self.schema}:
            raise ValueError('database owner file mismatch')
        self.identity = {'manifest_sha256': sha(args.manifest), 'profile_sha256': sha(args.profile),
                         'executor_sha256': sha(__file__), 'run_id': self.run_id,
                         'product_sha': self.manifest['product_sha'], 'test_sha': self.manifest['test_sha']}
        shared = private_file(self.c['run_identity_file'], BASE)
        common = json.loads(shared.read_text())
        self.common_identity = common
        if sha(shared) != self.c['run_identity_sha256'] or any(common.get(k) != self.manifest[k] for k in ('run_id', 'product_sha', 'test_sha', 'binary_sha256')):
            raise ValueError('common installation/capacity run identity mismatch')
        self.identity.update(run_identity_sha256=sha(shared), binary_sha256=self.manifest['binary_sha256'])
        if (self.identity['profile_sha256'] != self.c['profile_sha256']
                or self.identity['executor_sha256'] != self.c['executor_sha256']): raise ValueError('executor/profile not frozen')
        for name in ('product_sha', 'test_sha'):
            if not re.fullmatch('[0-9a-f]{40}', self.manifest[name]): raise ValueError('full source SHA required')
        for name, digest_key in (('binary', 'binary_sha256'), ('source_archive', 'source_archive_sha256')):
            if sha(self.manifest[name]) != self.manifest[digest_key]: raise ValueError('candidate binary/archive mismatch')
        source = Path(self.c['source_root']).resolve(strict=True)
        if not source.is_relative_to(BASE) or not self.manifest.get('files'): raise ValueError('frozen source file map absent')
        for name, expected in self.manifest['files'].items():
            p = (source/name).resolve(strict=True)
            if not p.is_relative_to(source) or sha(p) != expected: raise ValueError('source file mismatch')
        self.url = self.c['service_url'].rstrip('/')
        validate_url(self.url, self.c['allowed_private_ips'])
        self.media = self.c['artifact']
        validate_url(self.media['url'], self.c['allowed_private_ips'])
        if (self.media['size'] < self.profile['artifact_size_bytes_min']
                or not re.fullmatch('[0-9a-f]{64}', self.media['sha256'])): raise ValueError('full real image identity absent')
        receipt = private_file(self.media['qualification_file'], BASE)
        if sha(receipt) != self.media['qualification_sha256']: raise ValueError('media qualification receipt changed')
        evidence = json.loads(receipt.read_text())
        if (evidence.get('status') != 'PASS' or evidence.get('synthetic') is not False
                or evidence.get('sha256') != self.media['sha256'] or evidence.get('size') != self.media['size']):
            raise ValueError('real qualified media receipt missing')
        self.auth = json.loads(private_file(self.c['auth_file'], self.root).read_text())
        if sha(self.c['auth_file']) != self.c['auth_sha256']: raise ValueError('frozen credentials changed')
        credentials = [self.auth['seed'], *self.auth['sessions']]
        if any(set(r) != {'username', 'password'} or not re.fullmatch('[a-zA-Z0-9_]{1,60}', r['username'])
               or not isinstance(r['password'], str) or len(r['password']) < 32 for r in credentials):
            raise ValueError('private random credentials with at least 32 characters required')
        if len({r['username'] for r in credentials}) != len(credentials): raise ValueError('all seed/session usernames must differ')
        self.config = json.loads(private_file(self.c['config_file'], self.root).read_text())
        if sha(self.c['config_file']) != self.c['config_sha256']: raise ValueError('frozen service config changed')
        if self.config.get('machine', {}).get('enabled') is not True: raise ValueError('real API cancellation requires machine.enabled=true')
        private_file(self.config['machine']['keyFile'], self.root)
        if self.config['machine'].get('syslogListen'): raise ValueError('capacity service must not open a new syslog listener')
        if self.config.get('preview', {}).get('gateway') != self.url: raise ValueError('machine gateway must be this private service')
        validate_url(self.config['preview']['artifacts'], self.c['allowed_private_ips'])
        from db_env import postgres_env
        self.pg_env = postgres_env(self.config['pg']['dsn'])
        if self.pg_env.get('PGDATABASE') != self.c['database']['name'] or self.pg_env.get('PGOPTIONS') != '-c search_path='+self.schema:
            raise ValueError('database connection must select exactly this owned schema')
        if self.c['database']['name'] in ('postgres', 'template0', 'template1'): raise ValueError('system database refused')
        if self.config['listen'] != urllib.parse.urlsplit(self.url).netloc: raise ValueError('service config/URL mismatch')
        self.registered = dict(self.c['processes'])
        if set(self.registered) != {'product', 'database'}: raise ValueError('explicit product/database process identities required')
        for role, p in self.registered.items():
            if p['pid'] <= 1 or not Path(p['cwd']).resolve().is_relative_to(self.root) or not same_process(p):
                raise ValueError('registered process identity mismatch')
            if role == 'product' and p['exe_sha256'] != self.manifest['binary_sha256']:
                raise ValueError('running product not candidate')
        argv = Path('/proc', str(self.registered['product']['pid']), 'cmdline').read_bytes().rstrip(b'\0').decode().split('\0')
        if '-config' not in argv or (Path(self.registered['product']['cwd'])/argv[argv.index('-config')+1]).resolve() != Path(self.c['config_file']).resolve():
            raise ValueError('running product does not use this frozen configuration')
        self.registered['driver'] = process_identity(os.getpid())
        if not Path.cwd().resolve().is_relative_to(self.root): raise ValueError('driver cwd outside work root')
        self.output_root = args.output.resolve()
        if not self.output_root.is_relative_to(self.root): raise ValueError('output outside work root')
        self.output_root.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.phase_dir = self.output_root/args.phase
        self.phase_dir.mkdir(mode=0o700)  # 拒绝覆盖；重新运行必须使用新的输出目录。
        self.phase_id, self.window = uuid.uuid4().hex, None
        self.input_paths = {'manifest': str(args.manifest.resolve()), 'profile': str(args.profile.resolve()),
                            'output': str(args.output.resolve()), 'work-root': str(args.work_root.resolve())}
        context = lambda: {**self.identity, 'phase': args.phase, 'phase_id': self.phase_id,
                           'window_id': self.window['id'] if self.window else None}
        self.log = Journal(self.phase_dir/'lifecycle.jsonl', context)
        self.requests = Journal(self.phase_dir/'requests.jsonl', context)
        self.samples = Journal(self.phase_dir/'samples.jsonl', context)
        self.downloads = Journal(self.phase_dir/'downloads.jsonl', context)
        self.lanes = Journal(self.phase_dir/'lanes.jsonl', context)
        self.sessions_log = Journal(self.phase_dir/'sessions.jsonl', context)
        self.children = Journal(self.phase_dir/'processes.jsonl', context)
        self.sql_log = Journal(self.phase_dir/'database.jsonl', context)
        self.logs = (self.log, self.requests, self.samples, self.downloads, self.children, self.sql_log, self.lanes, self.sessions_log)
        self.start = time.monotonic()
        self.started_utc = time.time()
        self.sockets, self.child_procs, self.auth_books, self.threads = set(), {}, {}, []
        self.io_lock = threading.RLock()
        self.cleanup_complete, self.residuals, self.shutdown_deadline = False, [], None
        self.stop_event = threading.Event()
        self.interrupted = False
        self.worker_errors = 0
        self.sampling_errors = 0
        self.active = collections.Counter()
        self.lane_bytes = collections.Counter()
        self.active_download_workers = set()
        self.lock = threading.Lock()
        self.request_condition = threading.Condition(self.lock)
        self.business_inflight, self.load_requests_closed = 0, False
        self.download_ready = collections.Counter()
        self.measurement = threading.Event()
        self.session_tokens = set()
        self.tokens = {}
        self.hosts = []
        self.task_ids = []
        self.import_used = 0
        self.lock_file = None
        self.resources = self.c['resource_paths']
        if len(self.resources) < 1 or any(not Path(p).resolve().is_relative_to(BASE) for p in self.resources):
            raise ValueError('owned resource mount paths required')
        all_imports = []
        for phase, needed in (('normal', 1), ('burst', 1), ('soak', 48)):
            entries = self.c['imports'][phase]
            if len(entries) != needed: raise ValueError('imports.'+phase+' must contain exactly '+str(needed)+' distinct valid media files')
            for entry in entries:
                private_file(entry['path'], self.root)
                if type(entry['synthetic']) is not bool: raise ValueError('import synthetic classification required')
                all_imports.append(entry['sha256'])
        if len(set(all_imports)) != len(all_imports): raise ValueError('imports must have distinct content across all phases')
        write_once(self.phase_dir/'registry.json', {**context(), 'work_root': str(self.root),
                                                   'processes': self.registered})
        write_once(self.phase_dir/'command.json', {'argv': sys.argv, 'cwd': str(Path.cwd()),
                                                  'started_utc_unix': self.started_utc, 'started_at': self.start,
                                                  'inputs': self.input_paths,
                                                  **context()})
        self.log.add(status='RUNNING', phase=args.phase, **self.identity)
        signal.signal(signal.SIGTERM, self.on_signal)
        signal.signal(signal.SIGINT, self.on_signal)

    def on_signal(self, signum, _frame):
        self.interrupted = True
        self.log.add(kind='signal', signum=signum)
        self.stop_event.set()
        deadline = time.monotonic()+self.profile['cleanup_seconds']
        self.shutdown_deadline = min(self.shutdown_deadline, deadline) if self.shutdown_deadline is not None else deadline
        self.abort_io()

    def track_io(self, resource):
        with self.io_lock: self.sockets.add(resource)
        return resource

    def release_io(self, resource):
        with self.io_lock: self.sockets.discard(resource)
        resource.close()

    def abort_io(self):
        with self.io_lock: resources = list(self.sockets)
        for resource in resources:
            sock = resource if isinstance(resource, socket.socket) else getattr(resource, 'sock', None)
            if sock:
                with contextlib.suppress(OSError): sock.shutdown(socket.SHUT_RDWR)
            with contextlib.suppress(OSError): resource.close()

    def cleanup(self, threads=(), settle=False):
        deadline = self.shutdown_deadline or time.monotonic()+self.profile['cleanup_seconds']
        self.shutdown_deadline = deadline
        settled = True
        if settle:
            # 只结算窗口内已开始的业务请求。长连接随后关闭，不另给一轮等待预算。
            with self.request_condition:
                self.load_requests_closed = True
                while self.business_inflight and not self.stop_event.is_set() and time.monotonic() < deadline:
                    self.request_condition.wait(timeout=min(.1, max(0, deadline-time.monotonic())))
                settled = self.business_inflight == 0 and not self.stop_event.is_set()
                self.log.add(kind='request_settlement', complete=settled,
                             remaining=self.business_inflight, deadline=deadline)
        self.abort_io()
        for proc, registered in list(self.child_procs.values()):
            if proc.poll() is None and same_process(registered): proc.terminate()
        for thread in threads: thread.join(timeout=max(0, deadline-time.monotonic()))
        for proc, registered in list(self.child_procs.values()):
            if proc.poll() is None and same_process(registered): proc.kill()
            with contextlib.suppress(subprocess.TimeoutExpired): proc.wait(timeout=max(0, deadline-time.monotonic()))
        self.residuals = [t.name for t in threads if t.is_alive()]
        self.residuals += [str(p.pid) for p, _ in self.child_procs.values() if p.poll() is None]
        if not self.residuals:
            for session, book in self.auth_books.items():
                try:
                    if not same_process(self.registered['product']):
                        book.generations.clear()
                        self.sessions_log.add(kind='session_owner_service_gone', session=session)
                    book.close()
                except Exception: self.residuals.append('session-'+str(session))
        self.cleanup_complete = not self.residuals
        self.log.add(kind='cleanup', complete=self.cleanup_complete, residuals=self.residuals, deadline=deadline)
        return self.cleanup_complete and settled

    def acquire(self):
        import fcntl
        self.lock_file = (BASE/'leases/heavy.lock').open('r+')
        fcntl.flock(self.lock_file, fcntl.LOCK_EX | fcntl.LOCK_NB)
        self.log.add(kind='heavy_lock_acquired', inode=os.fstat(self.lock_file.fileno()).st_ino)

    def sql(self, query):
        command = ['psql', '-X', '-q', '-A', '-t', '-v', 'ON_ERROR_STOP=1']
        started = time.time()
        # SQL只读；事务强制只读、限制执行时间，凭据仅在环境中。
        payload = 'BEGIN READ ONLY; SET LOCAL statement_timeout=15000; '+query+'; COMMIT;'
        done = self.command(command, payload, self.pg_env, 25)
        self.sql_log.add(command=command, sql=payload, started=started, ended=time.time(),
                         exit=done.returncode, stdout=done.stdout, stderr=done.stderr)
        if done.returncode: raise RuntimeError('owned read-only database query failed')
        return done.stdout.strip()

    def command(self, argv, payload, env, timeout):
        proc = subprocess.Popen(argv, stdin=subprocess.PIPE, stdout=subprocess.PIPE,
                                stderr=subprocess.PIPE, text=True, env=env)
        registered = process_identity(proc.pid)
        self.child_procs[proc.pid] = (proc, registered)
        self.children.add(kind='spawn', command=argv, process=registered)
        try: stdout, stderr = proc.communicate(payload, timeout=timeout)
        except subprocess.TimeoutExpired:
            if same_process(registered): proc.kill()
            stdout, stderr = proc.communicate()
            self.children.add(kind='exit', process=registered, exit=proc.returncode, timed_out=True)
            raise
        self.children.add(kind='exit', process=registered, exit=proc.returncode, timed_out=False)
        self.child_procs.pop(proc.pid, None)
        return subprocess.CompletedProcess(argv, proc.returncode, stdout, stderr)

    def db_state(self):
        # 全库检查限定本轮schema；快照摘要只覆盖冻结历史行，运行中新任务另检查约束。
        prefix = self.c['dataset']['host_prefix']
        if not re.fullmatch(r'CAP-[a-z0-9]{8}-', prefix): raise ValueError('owned host prefix invalid')
        expected_prefix = 'CAP-'+hashlib.sha256(self.run_id.encode()).hexdigest()[:8]+'-'
        if prefix != expected_prefix: raise ValueError('host prefix not bound to run')
        q = """SELECT json_build_object(
          'schema', current_schema(),
          'hosts',(SELECT count(*) FROM hosts WHERE sn LIKE '%s%%'),
          'historical_tasks',(SELECT count(*) FROM tasks WHERE created_by='%s' AND state='cancelled'),
          'host_ids_digest',(SELECT md5(coalesce(string_agg(sn,'' ORDER BY sn),'')) FROM hosts WHERE sn LIKE '%s%%'),
          'task_ids_digest',(SELECT md5(coalesce(string_agg(id,'' ORDER BY id),'')) FROM tasks WHERE created_by='%s' AND state='cancelled'),
          'snapshot_digest',(SELECT md5(coalesce(string_agg(id||md5(snapshot::text),'' ORDER BY id),'')) FROM tasks WHERE created_by='%s' AND state='cancelled'),
          'snapshot_bytes_min',(SELECT coalesce(min(octet_length(snapshot::text)),0) FROM tasks),
          'snapshot_bytes_max',(SELECT coalesce(max(octet_length(snapshot::text)),0) FROM tasks),
          'storage_bytes',(SELECT coalesce(sum(pg_total_relation_size(c.oid)),0) FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace WHERE n.nspname=current_schema() AND c.relkind='r'),
          'violations',
            (SELECT count(*) FROM (SELECT host_sn FROM tasks WHERE state NOT IN ('done','failed','cancelled') GROUP BY host_sn HAVING count(*)>1) a)+
            (SELECT count(*) FROM tasks t LEFT JOIN hosts h ON h.sn=t.host_sn LEFT JOIN isos i ON i.id=t.iso_id LEFT JOIN template_versions v ON v.template_id=t.template_id AND v.version=t.template_version LEFT JOIN users u ON u.username=t.created_by WHERE h.sn IS NULL OR i.id IS NULL OR v.template_id IS NULL OR u.username IS NULL OR t.snapshot->'Host'->>'SN' IS DISTINCT FROM t.host_sn OR t.snapshot->'ISO'->>'ID' IS DISTINCT FROM t.iso_id OR t.snapshot->'Template'->>'ID' IS DISTINCT FROM t.template_id OR (t.snapshot->'Template'->>'Version')::int IS DISTINCT FROM t.template_version OR (t.snapshot->>'Schema')::int IS DISTINCT FROM 1 OR t.snapshot->>'Family' IS DISTINCT FROM i.family OR t.snapshot->>'Series' IS DISTINCT FROM i.series)+
            (SELECT count(*) FROM tasks t WHERE (SELECT count(*) FROM audit_log a WHERE a.target_kind='task' AND a.target_id=t.id AND a.action='task.create' AND a.actor=t.created_by AND a.at>=t.created_at)<>1)+
            (SELECT count(*) FROM tasks t WHERE state='cancelled' AND (SELECT count(*) FROM audit_log a WHERE a.target_kind='task' AND a.target_id=t.id AND a.action='task.cancel' AND a.actor=t.created_by AND a.at>=t.created_at AND a.at>=(SELECT max(ca.at) FROM audit_log ca WHERE ca.target_kind='task' AND ca.target_id=t.id AND ca.action='task.create' AND ca.actor=t.created_by))<>1)+
            (SELECT count(*) FROM audit_log a JOIN tasks t ON a.target_kind='task' AND a.target_id=t.id WHERE a.action IN ('task.create','task.cancel') AND a.actor<>t.created_by)+
            (SELECT count(*) FROM tasks t WHERE last_seq IS DISTINCT FROM (SELECT coalesce(max(e.seq),0) FROM task_events e WHERE e.task_id=t.id))+
            (SELECT count(*) FROM tasks t WHERE last_seq<>(SELECT count(*) FROM task_events e WHERE e.task_id=t.id))+
            (SELECT count(*) FROM pg_constraint WHERE connamespace=current_schema()::regnamespace AND NOT convalidated),
          'queue',(SELECT count(*) FROM isos WHERE state IN ('acquiring','importing'))+
                  (SELECT count(*) FROM tasks WHERE state NOT IN ('done','failed','cancelled')),
          'db_locks_waiting',(SELECT count(*) FROM pg_locks WHERE NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())))""" % (prefix, self.auth['seed']['username'], prefix, self.auth['seed']['username'], self.auth['seed']['username'])
        if not re.fullmatch('[a-zA-Z0-9_]{1,60}', self.auth['seed']['username']): raise ValueError('seed username invalid')
        row = json.loads(self.sql(q))
        if row['schema'] != self.schema or row['violations']: raise RuntimeError('database invariants failed')
        history = self.sql("SELECT md5(coalesce(string_agg(id||host_sn||state||md5(snapshot::text)||created_by||created_at::text||last_seq::text,'' ORDER BY id),'')) FROM tasks WHERE created_by='"+self.auth['seed']['username']+"' AND state='cancelled'")
        row['history_digest'] = history
        row['source_digest'] = self.source_state()
        self.sql_log.add(kind='dataset_observation', observation=row, exit=0)
        return row

    def admit_load_request(self, counted=False):
        with self.request_condition:
            started = time.monotonic()
            if self.window is not None:
                end = min(self.window['end'], self.window['start']+300) if self.args.phase == 'burst' else self.window['end']
                if self.load_requests_closed or self.stop_event.is_set() or started >= end:
                    raise LoadWindowClosed()
            if counted: self.business_inflight += 1
            return started

    def request(self, method, path, token=None, body=None, raw=None, headers=None, category='setup', session=-1):
        u = urllib.parse.urlsplit(self.url)
        timeout = self.profile['request_timeout_seconds']
        if self.shutdown_deadline is not None:
            timeout = min(timeout, self.shutdown_deadline-time.monotonic())
            if timeout <= 0: raise TimeoutError('common cleanup deadline elapsed')
        h = {'Content-Type': 'application/json', **(headers or {})}
        payload = raw if raw is not None else (json.dumps(body).encode() if body is not None else None)
        # 只有内部认证建立/退休可越过业务截止；仍受统一收尾截止限制。
        control = category == 'setup' and method == 'POST' and path in ('/auth/login', '/auth/logout')
        business = not control
        started = self.admit_load_request(counted=True) if business else time.monotonic()
        started_utc = time.time()
        status, ok, result, connection, response, response_socket = 0, False, None, None, None, None
        diagnostic = {'category': 'transport_error', 'envelope_code': None, 'problem_codes': []}
        book, generation = (token, None) if isinstance(token, AuthSession) else (None, None)
        try:
            connection = self.track_io(http.client.HTTPConnection(u.hostname, u.port, timeout=timeout))
            if book: token, generation = book.acquire()
            if book: session = book.session
            if token: h['Authorization'] = 'Bearer '+token
            connection.request(method, '/console/v1'+path, body=payload, headers=h)
            response = connection.getresponse(); status = response.status
            if response.fp is not None: response_socket = self.track_io(response.fp.raw._sock)
            data = response.read(16*1024*1024)
            envelope = json.loads(data)
            diagnostic = error_summary(envelope, status)
            ok = status == 200 and envelope.get('code') == 0
            if not ok: raise RuntimeError('HTTP/envelope failure')
            result = envelope.get('data')
            return result
        finally:
            try:
                try:
                    if connection: self.release_io(connection)
                    if response: response.close()
                    if response_socket: self.release_io(response_socket)
                    if generation is not None: book.release(generation)
                finally:
                    object_id = None
                    if isinstance(result, dict):
                        object_id = result.get('id') or result.get('sn')
                        if result.get('tasks'): object_id = result['tasks'][0].get('id')
                    self.requests.add(method=method, path=path, category=category, session=session,
                                      seconds=time.monotonic()-started, elapsed=started-self.start,
                                      at=started, ended_at=time.monotonic(), object_id=object_id,
                                      started_utc_unix=started_utc, ended_utc_unix=time.time(),
                                      status=status, ok=ok, server_error=status >= 500,
                                      diagnostic=diagnostic,
                                      request_bytes=len(payload or b''),
                                      request_sha256=hashlib.sha256(payload or b'').hexdigest())
            finally:
                if business:
                    with self.request_condition:
                        self.business_inflight -= 1
                        self.request_condition.notify_all()

    def login(self, credential, session):
        def establish(): return self.request('POST', '/auth/login', body=credential, session=session)
        book = AuthSession(establish, lambda token: self.request('POST', '/auth/logout', token, session=session),
                           lambda **r: self.sessions_log.add(session=session, **r),
                           margin=self.profile['session_expiry_margin_seconds'])
        book.session = session
        book.current()
        self.auth_books[session] = book
        return book

    def legacy_login(self, credential, session):
        row = self.request('POST', '/auth/login', body=credential, session=session)
        token = row.get('token')
        if not isinstance(token, str) or len(token) < 32: raise RuntimeError('login did not return independent token')
        with self.lock:
            if token in self.session_tokens: raise RuntimeError('session token reused')
            self.session_tokens.add(token)
        return token

    def verify_api_dataset(self, token):
        data = self.request('GET', '/hosts?page=1&page_size=20', token, category='host_list')
        if not data.get('items'): raise RuntimeError('real host query empty')
        tasks = self.request('GET', '/tasks?page=1&page_size=20&state=cancelled', token, category='task_list')
        if not tasks.get('items'): raise RuntimeError('real task query empty')
        self.task_ids = [t['id'] for t in tasks['items']]
        self.hosts = [h['sn'] for h in data['items']]
        for t in tasks['items']:
            current = self.request('GET', '/tasks/'+t['id'], token, category='task_detail')
            if current['snapshot'] != t['snapshot'] or current['state'] != 'cancelled':
                raise RuntimeError('API historical task snapshot/state mismatch')
            audits = self.request('GET', '/audit?target_kind=task&target_id='+t['id']+'&limit=50', token)
            if audit_time_problems(current, audits['items']): raise RuntimeError('API task audit actor/action/time mismatch')
        for collection, expected in ((data, self.dataset['hosts']), (tasks, self.dataset['historical_tasks'])):
            if collection.get('total') != expected: raise RuntimeError('API/database counts disagree (schema must be exclusive)')

    def new_host_task(self, token, serial, category='seed'):
        host = self.request('POST', '/hosts', token, {'sn': serial, 'arch': self.c['dataset']['arch'],
            'firmware': self.c['dataset']['firmware'], 'attrs': {'qualification': self.run_id,
            'purpose': 'capacity-cancelled-history', 'rack': 'capacity'}}, category=category)
        return host

    def history_task(self, token, sn, category='seed'):
        body = {**self.c['dataset']['selection'], 'hosts': [{'sn': sn, 'entered': self.c['dataset']['entered']}]}
        item = self.request('POST', '/wizard/tasks', token, body, category=category)['tasks'][0]
        cancelled = self.request('POST', '/tasks/'+item['id']+'/cancel', token, category=category)
        if cancelled['state'] != 'cancelled' or cancelled['snapshot'] != item['snapshot']:
            raise RuntimeError('API cancellation/snapshot contract failed')
        return item['id']

    def source_state(self):
        bindings = self.c['source_bindings']
        if not any(b['iso_id'] == self.c['dataset']['selection']['isoID'] and b['purpose'] == 'seed' for b in bindings):
            raise ValueError('seed ISO has no explicit source binding')
        if not any(b['iso_id'] == self.media['iso_id'] and b['purpose'] == 'download'
                   and b['url'] == self.media['url'] and b['sha256'] == self.media['sha256'] for b in bindings):
            raise ValueError('download URL not bound to qualified ISO source')
        frozen = []
        for binding in bindings:
            iso_id = binding['iso_id']
            if not re.fullmatch('[a-zA-Z0-9_-]{1,80}', iso_id): raise ValueError('invalid bound ISO identity')
            iso = json.loads(self.sql("SELECT row_to_json(i) FROM isos i WHERE id='"+iso_id+"'"))
            sources = json.loads(self.sql("SELECT coalesce(json_agg(s ORDER BY role),'[]') FROM iso_sources s WHERE iso_id='"+iso_id+"'"))
            if iso['state'] != 'ready' or iso['sha256'] != binding['sha256'] or not sources: raise RuntimeError('bound source changed/not ready')
            files = []
            for source in sources:
                path = Path(source['path']) if source['kind'] == 'local' else Path(self.config['iso']['staging'])/(iso_id+('.'+source['role'] if source['role'] else ''))
                actual = path.resolve(strict=True)
                if not actual.is_relative_to(self.root) or source['state'] != 'fetched': raise RuntimeError('source ownership/state invalid')
                digest = sha(actual)
                if source['fetched'] != actual.stat().st_size or source['total_size'] != actual.stat().st_size or source['expected_sha256'] != digest:
                    raise RuntimeError('source bytes/digest differ from row')
                if source['role'] == '' and (digest != binding['sha256'] or actual.stat().st_size != binding['size']
                        or str(actual) != str(Path(binding['local_file']).resolve())): raise RuntimeError('main source/declared file differ')
                files.append({'path': str(actual), 'size': actual.stat().st_size, 'sha256': digest})
            artifacts = Path(self.config['iso']['artifacts']).resolve()
            tree = artifacts/iso['series']/iso['version']/iso['arch']
            if (tree/'.lober-owner').read_text() != iso_id+'\n'+iso['sha256']+'\n': raise RuntimeError('artifact owner mismatch')
            for role, rel in iso['artifacts'].items():
                path = (artifacts/rel).resolve(strict=True)
                if not path.is_relative_to(tree.resolve()): raise RuntimeError('artifact path escaped owned publication')
                candidates = sorted(path.rglob('*')) if path.is_dir() else [path]
                for p in candidates:
                    if p.is_symlink() or not p.resolve().is_relative_to(tree.resolve()): raise RuntimeError('artifact link/ownership invalid')
                    if p.is_file(): files.append({'path': str(p), 'size': p.stat().st_size, 'sha256': sha(p)})
            frozen.append({'binding': binding, 'iso': iso, 'sources': sources, 'files': files})
        ids = json.loads(self.sql("SELECT coalesce(json_agg(id),'[]') FROM isos"))
        for key in ('staging', 'artifacts', 'smb'):
            for p in Path(self.config['iso'][key]).rglob('*'):
                if p.is_symlink() or not p.resolve().is_relative_to(self.root): raise RuntimeError('temporary path outside owned root')
                if ('.tmp-' in p.name or p.name.endswith('.part') or p.name.startswith('.retry-')) and not any(i in p.name for i in ids):
                    raise RuntimeError('temporary file lacks this schema owner')
        digest = hashlib.sha256(json.dumps(frozen, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        self.sql_log.add(kind='source_observation', source_digest=digest, originals=frozen, exit=0)
        return digest

    def configure_storage(self):
        info = json.loads(self.sql("SELECT json_build_object('data_directory',current_setting('data_directory'),'port',current_setting('port'),'sockets',current_setting('unix_socket_directories'),'database',current_database(),'tablespaces',(SELECT coalesce(json_agg(pg_tablespace_location(oid)) FILTER (WHERE pg_tablespace_location(oid)<>''),'[]') FROM pg_tablespace))"))
        data = Path(info['data_directory']).resolve(strict=True)
        if info['database'] != self.c['database']['name'] or info['tablespaces']: raise ValueError('wrong PG instance or unbudgeted tablespace')
        if int((data/'postmaster.pid').read_text().splitlines()[0]) != self.registered['database']['pid']:
            raise ValueError('SQL connection does not match registered postmaster')
        if self.pg_env.get('PGPORT', '5432') != info['port'] or self.pg_env.get('PGHOST') not in info['sockets'].split(','):
            raise ValueError('PG must use frozen dedicated instance socket/port')
        paths = {'data': str(data), 'wal': str((data/'pg_wal').resolve(strict=True)), 'logs': str(self.output_root)}
        paths.update({k: str(Path(self.config['iso'][k]).resolve(strict=True)) for k in ('staging', 'artifacts', 'smb', 'tools')})
        paths.update({'local_'+str(i): str(Path(p).resolve(strict=True)) for i, p in enumerate(self.config['iso'].get('localRoots', []))})
        if any(not Path(p).is_relative_to(self.root) for p in paths.values()): raise ValueError('write directory outside owned work root')
        verify_path_coverage(paths, self.resources)
        self.write_paths, self.devices, self.volumes = paths, {}, {}
        for role, path in paths.items():
            device = str(os.stat(path).st_dev); self.devices[role] = device
            self.volumes.setdefault(device, {'path': path, 'roles': []})['roles'].append(role)
        for device, volume in self.volumes.items():
            volume['floor'] = max(self.profile['root_free_bytes_min'] if device == str(os.stat('/').st_dev) else 0,
                                  self.profile['data_free_bytes_min'] if any(k != 'logs' for k in volume['roles']) else 0)
        self.log.add(kind='storage_layout', pg=info, paths=paths, volumes=self.volumes)

    def admit_storage(self, budgets):
        for device, amount in budgets.items():
            volume = self.volumes[device]; st = os.statvfs(volume['path'])
            if st.f_bavail*st.f_frsize-amount < volume['floor']: raise RuntimeError('original-scale filesystem budget blocked: '+device)
        ref = self.c['resource_evidence']; path = private_file(ref['path'], BASE)
        if sha(path) != ref['sha256']: raise ValueError('host resource receipt changed')
        observed = json.loads(path.read_text())
        if observed.get('run_identity_sha256') != self.identity['run_identity_sha256'] or not 0 <= time.time()-observed['observed_utc_unix'] <= 300:
            raise ValueError('fresh host resource evidence required before admission')
        if observed['host_free_bytes']-sum(budgets.values()) < observed['host_reserve_bytes']:
            raise RuntimeError('host filesystem original-scale budget blocked')

    def seed(self):
        self.acquire()
        self.configure_storage()
        self.guard_resources(self.sample())
        token = self.login(self.auth['seed'], -1)
        initial = self.db_state()
        if initial['hosts'] or initial['historical_tasks']: raise RuntimeError('seed requires empty dedicated dataset')
        prefix = self.c['dataset']['host_prefix']
        pilot_hosts = self.profile['seed_probe_hosts']
        measured = []
        for batch in range(3):
            for n in range(batch*10, (batch+1)*10): self.new_host_task(token, prefix+f'{n:05d}')
            for n in range(batch*100, (batch+1)*100):
                if self.stop_event.is_set(): raise InterruptedError('seed interrupted')
                self.history_task(token, prefix+f'{batch*10+n%10:05d}')
            snap = self.storage_budget_snapshot()
            snap['relation_bytes'] = self.db_state()['storage_bytes']
            measured.append(snap)
        budgets = plan_storage(measured, self.devices, remaining_batches=997, wal_reserve=measured[-1]['wal_reserve_bytes'])
        write_once(self.phase_dir/'seed-budget.json', {'initial': initial, 'warmup_then_two_batches': measured,
            'budgets_by_filesystem': budgets, 'hosts': 10000, 'historical_tasks': 100000,
            'method': 'first allocation excluded; max of two equal warm increments; original scale retained'})
        self.admit_storage(budgets)
        for n in range(30, 10000):
            if self.stop_event.is_set(): raise InterruptedError('seed interrupted')
            self.new_host_task(token, prefix+f'{n:05d}')
            if n % 100 == 0: self.guard_resources(self.sample())
        for n in range(300, 100000):
            if self.stop_event.is_set(): raise InterruptedError('seed interrupted')
            self.history_task(token, prefix+f'{n%10000:05d}')
            if n % 100 == 0: self.guard_resources(self.sample())
        self.dataset = self.db_state()
        self.verify_api_dataset(token)
        write_once(self.output_root/'seed-baseline.json', {**self.identity, 'phase_id': self.phase_id, 'dataset': self.dataset})
        # ANALYZE 是唯一非只读SQL；只对当前schema逐张已有表执行，不改数据或结构。
        tables = ('hosts', 'tasks', 'audit_log', 'task_events')
        command = ['psql', '-X', '-q', '-v', 'ON_ERROR_STOP=1']
        sql = ';'.join('ANALYZE '+self.schema+'.'+t for t in tables)+';'
        before = time.time()
        done = self.command(command, sql, self.pg_env, 60)
        self.sql_log.add(command=command, sql=sql, started=before, ended=time.time(), exit=done.returncode,
                         stdout=done.stdout, stderr=done.stderr)
        if done.returncode: raise RuntimeError('dataset ANALYZE failed')
        return {'dataset': self.dataset, 'method': 'real-api-cancelled-history', 'actual_install_success': 0,
                'baseline_sha256': sha(self.output_root/'seed-baseline.json')}

    def storage_budget_snapshot(self):
        settings = json.loads(self.sql("SELECT json_build_object('max_wal_bytes',pg_size_bytes(current_setting('max_wal_size')),'keep_wal_bytes',pg_size_bytes(current_setting('wal_keep_size')),'archive_mode',current_setting('archive_mode'),'replication_slots',(SELECT count(*) FROM pg_replication_slots),'wal_lsn',pg_current_wal_lsn()::text)"))
        if settings['archive_mode'] != 'off' or settings['replication_slots']:
            raise RuntimeError('WAL retention requires explicit reviewed budget; no silent size extrapolation')
        files = [p for p in self.phase_dir.iterdir() if p.is_file()]
        return {'log_bytes': sum(p.stat().st_size for p in files),
                'wal_settings': settings,
                'wal_reserve_bytes': settings['max_wal_bytes']*2+settings['keep_wal_bytes'],
                'volume_free_bytes': {p: os.statvfs(p).f_bavail*os.statvfs(p).f_frsize for p in ['/', *self.resources]}}

    def proc_sample(self, registered):
        p = Path('/proc')/str(registered['pid'])
        fields = (p/'stat').read_text().rsplit(')', 1)[1].split()
        status = dict(line.split(':', 1) for line in (p/'status').read_text().splitlines())
        sockets = set()
        for fd in (p/'fd').iterdir():
            with contextlib.suppress(FileNotFoundError):
                target = os.readlink(fd)
                if target.startswith('socket:['): sockets.add(target[8:-1])
        connections = 0
        for name in ('tcp', 'tcp6'):
            for line in (p/'net'/name).read_text().splitlines()[1:]:
                columns = line.split()
                if columns[9] in sockets and columns[3] != '0A': connections += 1
        for line in (p/'net/unix').read_text().splitlines()[1:]:
            columns = line.split()
            if columns[6] in sockets and columns[5] == '03': connections += 1
        return {'alive': fields[0] != 'Z', 'identity_ok': same_process(registered),
                'rss_bytes': int(status.get('VmRSS', '0 kB').split()[0])*1024,
                'swap_bytes': int(status.get('VmSwap', '0 kB').split()[0])*1024,
                'fds': len(list((p/'fd').iterdir())), 'connections': connections,
                'cpu_ticks': int(fields[11])+int(fields[12]),
                'io': (p/'io').read_text(), 'threads': int(status['Threads'])}

    def descendants(self):
        links = {}
        for p in Path('/proc').iterdir():
            if p.name.isdigit():
                with contextlib.suppress(OSError, ValueError):
                    links[int(p.name)] = int((p/'stat').read_text().rsplit(')', 1)[1].split()[1])
        roles = {p['pid']: role for role, p in self.registered.items()}
        while True:
            added = {pid: roles[ppid] for pid, ppid in links.items() if ppid in roles and pid not in roles}
            if not added: break
            roles.update(added)
        original = {p['pid'] for p in self.registered.values()}
        result = []
        for pid, role in roles.items():
            if pid not in original:
                with contextlib.suppress(OSError, ValueError):
                    registered = process_identity(pid)
                    result.append((role, registered))
        return result

    def sample(self, measurement='preflight'):
        row = {'elapsed': time.monotonic()-(self.window['start'] if measurement == 'qualification' else self.start), 'measurement': measurement,
               'sample_errors': [], 'processes': {}}
        for role, registered in self.registered.items():
            try: row['processes'][role] = self.proc_sample(registered)
            except (OSError, ValueError, KeyError): row['sample_errors'].append('process:'+role)
        children = self.descendants()
        for role, registered in children:
            try:
                child = self.proc_sample(registered)
                # 子进程资源也算进所属服务；原始分项另留。RSS包含共享页，不冒充独占内存。
                parent = row['processes'][role]
                for key in ('rss_bytes', 'swap_bytes', 'fds', 'connections', 'cpu_ticks', 'threads'):
                    parent[key] += child[key]
                parent.setdefault('children', []).append({'identity': registered, 'resource': child})
            except (OSError, ValueError, KeyError):
                # 已退出的短子进程由spawn/exit登记证明；不能吞掉仍存在的身份异常。
                if Path('/proc', str(registered['pid'])).exists(): row['sample_errors'].append('child:'+role)
        try:
            mem = dict(line.split(':', 1) for line in Path('/proc/meminfo').read_text().splitlines())
            for key, name in (('mem_available_bytes', 'MemAvailable'), ('swap_free_bytes', 'SwapFree')):
                row[key] = int(mem[name].split()[0])*1024
            vm = dict(line.split() for line in Path('/proc/vmstat').read_text().splitlines())
            row['oom_kill'] = int(vm['oom_kill'])
            row['vmstat'] = {k: vm[k] for k in ('pswpin', 'pswpout', 'pgmajfault')}
            row['diskstats'] = Path('/proc/diskstats').read_text()
            row['network'] = Path('/proc/net/dev').read_text()
            root_stat = os.statvfs('/')
            row['root_free_bytes'] = root_stat.f_bavail*root_stat.f_frsize
            volumes = {p: os.statvfs(p) for p in self.resources}
            row['data_free_bytes'] = min(st.f_bavail*st.f_frsize for st in volumes.values())
            row['volumes'] = {p: {'free_bytes': st.f_bavail*st.f_frsize} for p, st in volumes.items()}
            db = json.loads(self.sql("SELECT json_build_object('queue',(SELECT count(*) FROM isos WHERE state IN ('acquiring','importing'))+(SELECT count(*) FROM tasks WHERE state NOT IN ('done','failed','cancelled')),'db_locks_waiting',(SELECT count(*) FROM pg_locks WHERE NOT granted AND database=(SELECT oid FROM pg_database WHERE datname=current_database())))"))
            row.update(queue=db['queue'], db_locks_waiting=db['db_locks_waiting'])
            # 新子进程每次都记录身份；即便它们很快结束，也不扩大stop权限。
            self.children.add(kind='descendants', processes=[{'role': role, **p} for role, p in children])
        except (OSError, ValueError, RuntimeError, subprocess.SubprocessError): row['sample_errors'].append('system/database')
        with self.lock: row.update(self.active)
        self.samples.add(**row)
        if row['sample_errors']: self.sampling_errors += 1
        return row

    def guard_resources(self, row):
        if row['sample_errors']: raise RuntimeError('resource sampling failed')
        for key in ('root_free_bytes', 'data_free_bytes', 'mem_available_bytes', 'swap_free_bytes'):
            if row[key] < self.profile[key+'_min']: raise RuntimeError('resource stop threshold: '+key)
        for role, p in row['processes'].items():
            if not p['alive'] or not p['identity_ok']: raise RuntimeError('owned process lost/replaced')
            if role in ('driver', 'product') and p['rss_bytes'] > self.profile[role+'_rss_bytes_max']:
                raise RuntimeError('RSS stop threshold: '+role)

    def count(self, key, amount):
        with self.lock: self.active[key] += amount

    def worker_failed(self, category):
        with self.lock: self.worker_errors += 1
        self.log.add(kind='worker_error', category=category)
        self.stop_event.set()

    def open_download(self, worker, kind):
        if kind == 'load': self.admit_load_request()
        u = urllib.parse.urlsplit(self.media['url'])
        conn = self.track_io(http.client.HTTPConnection(u.hostname, u.port, timeout=20))
        connection_id = uuid.uuid4().hex
        response, response_socket = None, None
        try:
            conn.request('GET', urllib.parse.urlunsplit(('', '', u.path or '/', u.query, '')))
            response = conn.getresponse()
            if response.status != 200 or int(response.getheader('Content-Length', '-1')) != self.media['size']:
                raise RuntimeError('download full length/status mismatch')
            response_socket = self.track_io(response.fp.raw._sock)
            first = response.read(1)
            if not first: raise EOFError('no actual image byte')
            at = time.monotonic()
            if kind == 'load':
                self.lanes.add(kind='open', transport='download', lane=worker, connection=connection_id)
                with self.lock:
                    self.active['download_bytes'] += len(first)
                    self.lane_bytes[worker] += len(first)
                    self.lanes.add(kind='progress', transport='download', lane=worker,
                                   connection=connection_id, bytes=self.lane_bytes[worker])
                    # 两条同路连接只增加这一路的引用数，不能顶替另一条路。
                    self.download_ready[worker] += 1
                    self.active['downloads'] += 1
            return {'conn': conn, 'response': response, 'socket': response_socket, 'id': connection_id, 'first': first, 'at': at}
        except Exception:
            self.release_io(conn)
            if response: response.close()
            if response_socket: self.release_io(response_socket)
            raise

    def close_download(self, prepared, worker, kind):
        if kind == 'load':
            with self.lock:
                self.lanes.add(kind='close', transport='download', lane=worker, connection=prepared['id'])
                self.download_ready[worker] -= 1
                self.active['downloads'] -= 1
        self.release_io(prepared['conn'])
        prepared['response'].close()
        self.release_io(prepared['socket'])

    def download(self, worker, kind='load', quit_event=None, prepared=None, overlap=False):
        prepared = prepared or self.open_download(worker, kind)
        started, size, digest, verified = prepared['at'], 0, hashlib.sha256(), False
        next_request, last_record, first_block = None, prepared['at'] if kind == 'load' else 0, True
        try:
            block = prepared['first']
            while block:
                size += len(block); digest.update(block)
                with self.lock:
                    if kind != 'load' or not first_block:
                        self.active['download_bytes'] += len(block)
                        self.lane_bytes[worker] += len(block)
                    lane_total = self.lane_bytes[worker]
                now = time.monotonic()
                if kind == 'load' and not first_block and (now-last_record >= 5 or size == self.media['size']):
                    self.lanes.add(kind='progress', transport='download', lane=worker,
                                   connection=prepared['id'], bytes=lane_total)
                    last_record = now
                first_block = False
                if quit_event and (quit_event.is_set() or self.stop_event.is_set()): break
                if overlap and next_request is None and 0 < self.media['size']-size <= 2*1024*1024:
                    next_request = self.open_download(worker, kind)
                block = prepared['response'].read(min(256*1024, self.media['size']-size)) if size < self.media['size'] else b''
                if not block and size < self.media['size']: raise EOFError('truncated full image')
            verified = size == self.media['size'] and digest.hexdigest() == self.media['sha256']
            if not verified and not (quit_event and (quit_event.is_set() or self.stop_event.is_set())):
                raise RuntimeError('full image digest mismatch')
            return next_request if overlap else verified
        except Exception:
            if next_request: self.close_download(next_request, worker, kind)
            raise
        finally:
            self.close_download(prepared, worker, kind)
            self.downloads.add(worker=worker, connection=prepared['id'], kind=kind, method='GET', bytes=size,
                               sha256=digest.hexdigest(), verified=verified, started_at=started,
                               seconds=time.monotonic()-started, elapsed=time.monotonic()-self.start)

    def download_loop(self, worker, quit_event):
        prepared = None
        try:
            while not quit_event.is_set() and not self.stop_event.is_set():
                current, prepared = prepared, None
                prepared = self.download(worker, quit_event=quit_event, prepared=current, overlap=True)
        except LoadWindowClosed:
            pass
        except Exception:
            if not quit_event.is_set() and not self.stop_event.is_set(): self.worker_failed('download')
        finally:
            if prepared:
                with contextlib.suppress(OSError): self.close_download(prepared, worker, 'load')

    def stream_loop(self, worker, quit_event):
        u = urllib.parse.urlsplit(self.url)
        book = self.tokens[worker % len(self.tokens)]
        task_id = self.task_ids[worker % len(self.task_ids)]
        current, last = None, 0
        def close(item):
            if item:
                self.lanes.add(kind='close', transport='event', lane=worker, connection=item['id'], cursor=last)
                self.count('streams', -1)
                self.release_io(item['socket']); book.release(item['generation'])
        try:
            while not quit_event.is_set() and not self.stop_event.is_set():
                self.admit_load_request()
                token, generation = book.acquire()
                candidate, adopted = None, False
                try:
                    candidate = self.track_io(socket.create_connection((u.hostname, u.port), timeout=20))
                    connection_id = uuid.uuid4().hex
                    request = ('GET /console/v1/tasks/'+task_id+'/stream?after_seq='+str(last)+
                               ' HTTP/1.1\r\nHost: '+u.netloc+'\r\nAuthorization: Bearer '+token+
                               '\r\nAccept: text/event-stream\r\n\r\n')
                    before = time.monotonic(); candidate.sendall(request.encode()); data = b''
                    while b'\r\n\r\n' not in data:
                        chunk = candidate.recv(4096)
                        if not chunk: raise EOFError('event handshake ended')
                        data += chunk
                        if len(data) > 1024*1024: raise ValueError('event handshake too large')
                    header, body = data.split(b'\r\n\r\n', 1)
                    if b' 200 ' not in header.split(b'\r\n', 1)[0] or b'text/event-stream' not in header.lower():
                        raise ValueError('event handshake refused')
                    decoder = EventDecoder(b'transfer-encoding: chunked' in header.lower(), after=last)
                    self.lanes.add(kind='open', transport='event', lane=worker, connection=connection_id,
                                   after_seq=last, task_id=task_id, generation=generation)
                    self.count('streams', 1)
                    close(current)
                    current = {'socket': candidate, 'id': connection_id, 'generation': generation}
                    adopted = True
                    candidate = None
                    self.requests.add(category='event_reconnect', session=worker % len(self.tokens),
                                      ok=True, server_error=False, seconds=time.monotonic()-before, at=before,
                                      ended_at=time.monotonic(), elapsed=before-self.start, status=200)
                    current['socket'].settimeout(.2)
                    deadline = time.monotonic()+self.profile['event_reconnect_seconds']
                    while True:
                        for event in decoder.feed(body):
                            if event['taskID'] != task_id: raise ValueError('event crossed task')
                            last = event['seq']
                            self.lanes.add(kind='event_cursor', transport='event', lane=worker, connection=current['id'],
                                           seq=last, task_id=task_id,
                                           digest=hashlib.sha256(json.dumps(event, sort_keys=True).encode()).hexdigest())
                        body = b''
                        if quit_event.is_set() or self.stop_event.is_set(): break
                        book.current()
                        if time.monotonic() >= deadline or generation != book.generation: break
                        try:
                            body = current['socket'].recv(65536)
                            if not body: raise EOFError('event socket ended before scheduled handover')
                        except socket.timeout: pass
                except Exception:
                    if candidate: self.release_io(candidate)
                    if not adopted: book.release(generation)
                    raise
        except LoadWindowClosed:
            pass
        except Exception:
            if not quit_event.is_set() and not self.stop_event.is_set(): self.worker_failed('event framing/cursor/connection')
        finally:
            close(current)

    def console_loop(self, worker, quit_event):
        rng = random.Random(20261025+worker)
        self.count('sessions', 1)
        last_create, last_idle = -1, -1
        turn = 0
        try:
            while not self.measurement.wait(.1):
                if quit_event.is_set() or self.stop_event.is_set(): return
            while not quit_event.is_set() and not self.stop_event.is_set():
                before = time.monotonic()
                active_end = self.window['start']+300 if self.args.phase == 'burst' else self.window['end']
                if before >= active_end: return
                token = self.tokens[worker]
                selected = turn % 4
                if selected == 0:
                    page = rng.randint(1, 500)
                    result = self.request('GET', f'/hosts?page={page}&page_size=20', token, category='host_list', session=worker)
                    if not isinstance(result.get('items'), list) or not result['items']: raise RuntimeError('host list result invalid')
                elif selected == 1:
                    sn = self.c['dataset']['host_prefix']+f'{rng.randrange(10000):05d}'
                    result = self.request('GET', '/hosts/'+sn, token, category='host_detail', session=worker)
                    if result.get('sn') != sn: raise RuntimeError('host detail identity invalid')
                elif selected == 2:
                    result = self.request('GET', f'/tasks?page={rng.randint(1, 5000)}&page_size=20', token,
                                 category='task_list', session=worker)
                    if not isinstance(result.get('items'), list) or not result['items']: raise RuntimeError('task list result invalid')
                else:
                    selected_id = rng.choice(self.task_ids)
                    result = self.request('GET', '/tasks/'+selected_id, token, category='task_detail', session=worker)
                    if result.get('id') != selected_id or result.get('state') != 'cancelled' or not result.get('snapshot'):
                        raise RuntimeError('task detail identity/snapshot invalid')
                create_period = int((before-self.window['start'])//60)
                idle_period = int((before-self.window['start'])//600)
                if worker == 0 and create_period > last_create:
                    serial = self.c['dataset']['host_prefix']+'live-'+self.args.phase+'-'+str(turn)
                    self.new_host_task(token, serial, category='create_read')
                    self.request('GET', '/hosts/'+serial, token, category='create_read', session=worker)
                    last_create = create_period
                if idle_period > last_idle:
                    # 每个独立会话保持登录及事件/下载连接，只暂停该会话读操作。
                    idle_start = time.monotonic()
                    quit_event.wait(self.profile['idle_seconds'])
                    self.requests.add(category='idle_observation', session=worker, ok=True, server_error=False,
                                      seconds=time.monotonic()-idle_start, elapsed=idle_start-self.start,
                                      at=idle_start, ended_at=time.monotonic())
                    last_idle = idle_period
                turn += 1
                quit_event.wait(max(0, self.profile['request_period_seconds']-(time.monotonic()-before)))
        except LoadWindowClosed:
            pass
        except Exception:
            if not quit_event.is_set() and not self.stop_event.is_set(): self.worker_failed('console')
        finally: self.count('sessions', -1)

    def import_loop(self, quit_event):
        try:
            entries = self.c['imports'][self.args.phase]
            needed = math.ceil({'normal': 1800, 'burst': 300, 'soak': 86400}[self.args.phase]/1800)
            if len(entries) < needed: raise RuntimeError('distinct valid upload/import inputs missing')
            while not self.measurement.wait(.1):
                if quit_event.is_set() or self.stop_event.is_set(): return
            for index in range(needed):
                if quit_event.wait(max(0, self.window['start']+index*1800-time.monotonic())): return
                if quit_event.is_set() or self.stop_event.is_set(): return
                item = entries[index]
                p = private_file(item['path'], self.root)
                if p.stat().st_size != item['size'] or sha(p) != item['sha256']: raise RuntimeError('import input changed')
                # 上传前为完整原件和导入副本各留一份空间；实际过程中仍每分钟看护。
                budget = collections.Counter()
                budget[self.devices['staging']] += item['size']
                budget[self.devices['artifacts']] += item['projected_artifact_bytes']
                if item['family'] == 'windows': budget[self.devices['smb']] += item['size']
                for device, amount in budget.items():
                    volume = self.volumes[device]; st = os.statvfs(volume['path'])
                    if st.f_bavail*st.f_frsize-amount < volume['floor']: raise RuntimeError('per-filesystem import budget blocked')
                # 各phase使用不同已冻结文件，不能靠重复内容触发去重当成导入。
                started_at = time.monotonic()
                iso = self.request('POST', '/isos', self.tokens[0], {'name': self.run_id+'-'+self.args.phase+'-'+str(index),
                    'family': item['family'], 'series': item['series'], 'sources': [{'kind': 'upload',
                    'size': item['size'], 'sha256': item['sha256']}]}, category='upload_import')
                offset = 0
                with p.open('rb') as f:
                    for block in iter(lambda: f.read(1024*1024), b''):
                        self.request('PUT', '/isos/'+iso['id']+'/content', self.tokens[0], raw=block,
                            headers={'Content-Type': 'application/octet-stream',
                                     'Content-Range': f'bytes {offset}-{offset+len(block)-1}/{item["size"]}'}, category='upload_import')
                        offset += len(block)
                deadline = time.monotonic()+self.profile['import_timeout_seconds']
                while True:
                    current = self.request('GET', '/isos/'+iso['id'], self.tokens[0], category='upload_import')
                    if current['state'] == 'ready': break
                    if current['state'] == 'failed' or time.monotonic() >= deadline: raise RuntimeError('real import failed/timed out')
                    if quit_event.wait(1) or self.stop_event.is_set(): return
                if current.get('sha256') != item['sha256'] or not current.get('artifacts'):
                    raise RuntimeError('ready import content/artifacts mismatch')
                self.log.add(kind='import_verified', id=iso['id'], index=index, started_at=started_at,
                             uploaded_bytes=offset, state=current['state'], input_sha256=item['sha256'], synthetic=item['synthetic'])
        except LoadWindowClosed:
            pass
        except Exception:
            if not quit_event.is_set() and not self.stop_event.is_set(): self.worker_failed('upload_import')

    def load(self):
        self.acquire()
        self.configure_storage()
        phase = self.args.phase
        duration = {'normal': 1800, 'burst': 900, 'soak': 86400}[phase]
        self.dataset = self.db_state()
        baseline = json.loads((self.output_root/'seed-baseline.json').read_text())
        seeded = json.loads((self.output_root/'seed/result.json').read_text())
        if sha(self.output_root/'seed-baseline.json') != seeded.get('baseline_sha256') or any(baseline.get(k) != v for k, v in self.identity.items()):
            raise RuntimeError('seed baseline identity changed')
        if invariant_chain_problems(baseline['dataset'], self.dataset): raise RuntimeError('history/source changed between stages')
        if self.dataset['hosts'] < 10000 or self.dataset['historical_tasks'] < 100000:
            raise RuntimeError('original dataset missing; no downscaled qualification')
        seed = self.login(self.auth['seed'], -1)
        self.verify_api_dataset(seed)
        invariants = self.dataset
        idle_baseline = self.sample()['processes']
        self.guard_resources(self.sample())
        if not self.download(-1, 'single_baseline'): raise RuntimeError('single download failed')
        with concurrent.futures.ThreadPoolExecutor(max_workers=4) as pool:
            verified = list(pool.map(lambda w: self.download(w, 'parallel_baseline'), range(4)))
        if not all(verified): raise RuntimeError('four download verification failed')
        multiplier = 2 if phase == 'burst' else 1
        if len(self.auth['sessions']) < 20*multiplier: raise RuntimeError('independent session credentials missing')
        usernames = [r['username'] for r in self.auth['sessions'][:20*multiplier]]
        if len(set(usernames)) != len(usernames): raise RuntimeError('distinct test users required')
        for w in range(20*multiplier): self.tokens[w] = self.login(self.auth['sessions'][w], w)
        quit_event = threading.Event()
        threads = []
        def spawn(fn, *args):
            t = threading.Thread(target=fn, args=args, daemon=True); threads.append(t); t.start()
        self.threads = threads
        for w in range(20*multiplier): spawn(self.console_loop, w, quit_event)
        for w in range(50*multiplier): spawn(self.stream_loop, w, quit_event)
        for w in range(4*multiplier): spawn(self.download_loop, w, quit_event)
        spawn(self.import_loop, quit_event)
        startup = time.monotonic()
        try:
            while True:
                with self.lock:
                    ready = (all(self.active[k] >= n*multiplier for k, n in (('sessions', 20), ('streams', 50)))
                             and all(self.download_ready[lane] > 0 for lane in range(4*multiplier)))
                    if ready:
                        measurement_start = time.monotonic()
                        self.window = {'id': uuid.uuid4().hex, 'start': measurement_start, 'end': measurement_start+duration,
                                       'started_utc_unix': time.time()}
                        self.log.add(kind='measurement_begin', window=self.window)
                        self.measurement.set()
                if ready: break
                if self.stop_event.wait(.1) or time.monotonic()-startup >= 60: raise RuntimeError('client startup failed')
            startup_elapsed = time.monotonic()-startup
            next_sample = 0
            oom_first = None
            while True:
                elapsed = time.monotonic()-measurement_start
                if phase == 'burst' and elapsed >= 300 and not quit_event.is_set():
                    quit_event.set()
                    if not self.cleanup(threads, settle=True): raise RuntimeError('burst cleanup incomplete')
                    self.log.add(kind='burst_load_stopped', elapsed=time.monotonic()-measurement_start)
                sample = self.sample(measurement='qualification')
                self.guard_resources(sample)
                if oom_first is None: oom_first = sample['oom_kill']
                if sample['oom_kill'] != oom_first: raise RuntimeError('OOM observed')
                if self.stop_event.is_set(): raise InterruptedError('phase interrupted or client failed')
                if elapsed >= duration: break
                next_sample += 60
                self.stop_event.wait(max(0, min(duration, next_sample)-(time.monotonic()-measurement_start)))
            self.log.add(kind='measurement_end', window_id=self.window['id'])
            quit_event.set()
            if not self.cleanup(threads, settle=True): raise RuntimeError('load cleanup incomplete')
            if self.stop_event.is_set(): raise InterruptedError('phase interrupted during settlement')
            after = self.db_state()
            if invariant_chain_problems(baseline['dataset'], after): raise RuntimeError('frozen history/source changed within stage')
            return {'requested_seconds': duration, 'elapsed_seconds': time.monotonic()-measurement_start,
                    'startup_seconds': startup_elapsed, 'dataset': self.dataset,
                    'invariants_before': invariants, 'invariants_after': after,
                    'download_baseline': {'single_verified': 1, 'parallel_verified': 4},
                    'unique_sessions': len(usernames), 'artifact_size': self.media['size'],
                    'artifact_sha256': self.media['sha256'],
                    'idle_baseline': idle_baseline}
        finally:
            quit_event.set()
            if not self.cleanup(threads): self.worker_errors += 1

    def stop(self):
        stopped, refused = [], []
        for phase in ('seed', 'normal', 'burst', 'soak'):
            path = self.output_root/phase/'registry.json'
            if not path.is_file(): continue
            row = json.loads(path.read_text())
            if any(row.get(k) != v for k, v in self.identity.items()): raise ValueError('stop identity mismatch')
            p = row['processes']['driver']
            if not Path(p['cwd']).resolve().is_relative_to(self.root): raise ValueError('stop cwd outside owned root')
            if p['pid'] == os.getpid(): raise ValueError('stop self refused')
            if same_process(p):
                os.kill(p['pid'], signal.SIGTERM)
                stopped.append(p)
                self.log.add(kind='owned_driver_SIGTERM', process=p)
                deadline = getattr(self, 'stop_deadline', None) or time.monotonic()+30
                self.stop_deadline = deadline
                while same_process(p) and time.monotonic() < deadline: time.sleep(.2)
                if same_process(p):
                    refused.append(phase)
                    self.log.add(kind='stop_timeout', process=p)
            elif Path('/proc', str(p['pid'])).exists():
                refused.append(phase)
                self.log.add(kind='stop_refuses_reused_PID', phase=phase)
        if refused: raise ValueError('stop refuses reused PID or timed out; retry remains available')
        return {'stopped': stopped, 'refused_phases': refused,
                'scope': 'owned registered drivers only; service and database retained'}

    def evaluate(self):
        self.acquire()
        problems, phase_results, phase_digests = [], {}, {}
        phase_ids, previous_end, frozen_baseline = set(), None, None
        for phase in ('seed', 'normal', 'burst', 'soak'):
            root = self.output_root/phase
            path = root/'result.json'
            if not path.is_file(): problems.append(phase+': result absent'); continue
            row = json.loads(path.read_text())
            phase_digests[phase] = sha(path)
            if any(row.get(k) != v for k, v in self.identity.items()): problems.append(phase+': identity mismatch')
            for name, expected in row.get('evidence', {}).items():
                file = (root/name).resolve()
                if not file.is_relative_to(root) or not file.is_file() or sha(file) != expected:
                    problems.append(phase+': evidence missing/changed')
            required = {'command.json', 'registry.json', 'lifecycle.jsonl', 'requests.jsonl',
                        'samples.jsonl', 'downloads.jsonl', 'processes.jsonl', 'database.jsonl', 'lanes.jsonl', 'sessions.jsonl'}
            if not required <= set(row.get('evidence', {})): problems.append(phase+': raw evidence absent')
            if not required <= {p.name for p in root.iterdir() if p.is_file()}: continue
            def records(filename): return bound_rows(root/filename, self.identity, phase, row.get('phase_id'))
            lifecycle = list(read_rows(root/'lifecycle.jsonl'))
            for filename in ('lifecycle.jsonl', 'requests.jsonl', 'samples.jsonl', 'downloads.jsonl', 'processes.jsonl', 'database.jsonl', 'lanes.jsonl', 'sessions.jsonl'):
                for _ in records(filename): pass
            problems += [phase+': '+e for e in stage_bundle_problems(phase, self.identity, row,
                json.loads((root/'command.json').read_text()), json.loads((root/'registry.json').read_text()), lifecycle)]
            if row.get('phase_id') in phase_ids: problems.append('phase_id reused across stages')
            phase_ids.add(row.get('phase_id'))
            if previous_end is not None and row.get('started_at', -1) < previous_end: problems.append('stage order/overlap invalid')
            previous_end = row.get('ended_at', math.inf)
            if phase == 'seed':
                if (row.get('status') != 'COMPLETED' or row.get('dataset', {}).get('hosts', 0) < 10000
                        or row.get('dataset', {}).get('historical_tasks', 0) < 100000
                        or row.get('actual_install_success') != 0): problems.append('seed incomplete')
                problems += seed_evidence_problems(row.get('dataset', {}), read_rows(root/'requests.jsonl'), read_rows(root/'database.jsonl'))
                frozen_baseline = row.get('dataset')
                if not (self.output_root/'seed-baseline.json').is_file() or sha(self.output_root/'seed-baseline.json') != row.get('baseline_sha256'):
                    problems.append('frozen seed baseline original missing/changed')
                continue
            if not (root/'samples.jsonl').is_file(): continue
            all_samples = list(read_rows(root/'samples.jsonl'))
            # 启动前资源记录不在计时覆盖内；独立保存但不混入正式分钟采样。
            samples = [s for s in all_samples if s.get('measurement') == 'qualification']
            window = row.get('window', {})
            requests = request_summary(r for r in read_rows(root/'requests.jsonl')
                if r.get('window_id') == window.get('id') and window.get('start', math.inf) <= r.get('at', -1) < window.get('end', -math.inf))
            downloads = download_summary(read_rows(root/'downloads.jsonl'), row.get('artifact_size'), row.get('artifact_sha256'))
            found = check_phase(self.profile, self.identity, row, samples, requests, downloads, expected_phase=phase)
            if window and phase == row.get('phase'):
                found += activity_evidence_problems(row, read_rows(root/'requests.jsonl'))
                found += periodic_jobs_problems(row, read_rows(root/'requests.jsonl'), lifecycle, self.c['imports'][phase])
                active_window = {**window, 'end': window['start']+300} if phase == 'burst' else window
                multiplier = 2 if phase == 'burst' else 1
                found += lane_evidence_problems((r for r in read_rows(root/'lanes.jsonl') if r.get('transport') == 'download'), active_window, 4*multiplier, progress=True)
                found += event_lane_problems((r for r in read_rows(root/'lanes.jsonl') if r.get('transport') == 'event'), active_window, 50*multiplier)
            for current in (row.get('invariants_before', {}), row.get('invariants_after', {})):
                found += invariant_chain_problems(frozen_baseline or {}, current)
            problems += [phase+': '+p for p in found]
            phase_results[phase] = {'requests': requests, 'downloads': downloads}
        concurrent = self.c.get('concurrent_installation')
        # 本工具不创建装机结果；两台真实并发装机必须有单独冻结的检查器结论。
        if not concurrent: problems.append('real concurrent installation evidence absent')
        else:
            p = private_file(concurrent['path'], BASE)
            report = json.loads(p.read_text())
            if sha(p) != concurrent['sha256']: problems.append('concurrent original changed')
            problems += installation_problems(report, self.identity, p.parent, self.common_identity)
        write_once(self.phase_dir/'evaluation.json', {**self.identity, 'phase_result_sha256': phase_digests,
                   'problems': problems, 'latency_distributions': phase_results})
        write_once(self.phase_dir/'capacity_soak.json', {**self.identity,
            'status': 'FAIL' if problems else 'PASS', 'count': 0 if problems else 4,
            'failed': len(problems), 'skipped': 0, 'problems': problems,
            'binary_sha256': self.manifest['binary_sha256'],
            'evidence': 'evaluation.json', 'evidence_sha256': sha(self.phase_dir/'evaluation.json'),
            'scope': 'full original capacity + 24h + separately verified concurrent installation',
            'latency_distributions': phase_results})
        if problems: raise RuntimeError('qualification evidence incomplete or failed; inspect capacity_soak.json')
        return {'capacity_soak': 'PASS'}

    def run(self):
        status, reason, extra = 'FAILED', 'exception', {}
        try:
            if self.args.phase == 'seed': extra = self.seed()
            elif self.args.phase in ('normal', 'burst', 'soak'): extra = self.load()
            elif self.args.phase == 'stop': extra = self.stop()
            elif self.args.phase == 'evaluate': extra = self.evaluate()
            status = 'COMPLETED'
            reason = 'duration_elapsed' if self.args.phase in ('normal', 'burst', 'soak') else 'phase_finished'
            return 0
        except Exception as error:
            # 不输出异常原文，避免HTTP/连接库异常携带私有认证信息。
            reason = 'interrupted' if self.interrupted else type(error).__name__
            self.log.add(kind='failure', reason=reason,
                         detail=str(error) if type(error) in (RuntimeError, ValueError, KeyError) else 'private transport failure')
            return 1
        finally:
            if not self.cleanup_complete: self.cleanup(self.threads)
            while self.residuals:
                # 有残留时保留锁和RUNNING现场，不写完成。独立stop可再次触发主动关闭。
                self.log.add(kind='cleanup_incomplete_lock_retained', residuals=self.residuals)
                time.sleep(.5)
                self.shutdown_deadline = time.monotonic()+self.profile['cleanup_seconds']
                if self.cleanup(self.threads): break
                status, reason = 'FAILED', 'cleanup_incomplete'
            self.log.add(status=status, reason=reason, ended_utc_unix=time.time(),
                         elapsed_seconds=time.monotonic()-self.start)
            for log in self.logs: log.close()
            evidence = {p.name: sha(p) for p in self.phase_dir.iterdir() if p.is_file()}
            write_once(self.phase_dir/'result.json', {**self.identity, **extra, 'phase': self.args.phase,
                'phase_id': self.phase_id, 'window': self.window, 'started_at': self.start, 'ended_at': time.monotonic(),
                'inputs': self.input_paths, 'driver_identity': self.registered['driver'],
                'cleanup_complete': self.cleanup_complete, 'residuals': self.residuals,
                'mode': 'qualification', 'status': status, 'reason': reason,
                'interrupted': self.interrupted, 'sampling_errors': self.sampling_errors,
                'worker_errors': self.worker_errors, 'owner_verified': True,
                'process_identity_verified': all(same_process(p) for role, p in self.registered.items() if role != 'driver'),
                'elapsed_seconds': extra.get('elapsed_seconds', time.monotonic()-self.start),
                'evidence': evidence, 'ended_utc_unix': time.time()})
            if self.lock_file: self.lock_file.close()


def stop_entry(args):
    """独立停止不读介质/源码包，不要求产品或PG活着，每次尝试保留新目录。"""
    if sys.platform != 'linux': raise ValueError('stop must execute on lober VM')
    root = args.work_root.resolve(strict=True)
    if root == BASE or not root.is_relative_to(BASE) or root.stat().st_uid != os.getuid() or root.stat().st_mode & 0o077:
        raise ValueError('wrong stop work root')
    manifest = json.loads(private_file(args.manifest, BASE).read_text()); c = manifest['capacity']
    if sha(private_file(BASE/'.qualification-owner', BASE)) != c['owner_sha256'] or c['run_id'] != manifest['run_id']:
        raise ValueError('stop owner/run mismatch')
    if sha(__file__) != c['executor_sha256']: raise ValueError('stop requires same frozen executor')
    output = args.output.resolve(strict=True)
    if not output.is_relative_to(root): raise ValueError('stop output escaped work root')
    identity = {k: manifest[k] for k in ('run_id', 'product_sha', 'test_sha', 'binary_sha256')}
    identity.update(manifest_sha256=sha(args.manifest), profile_sha256=c['profile_sha256'],
                    executor_sha256=c['executor_sha256'], run_identity_sha256=c['run_identity_sha256'])
    attempts = output/'stop-attempts'; attempts.mkdir(mode=0o700, exist_ok=True)
    attempt = attempts/uuid.uuid4().hex; attempt.mkdir(mode=0o700)
    d = object.__new__(Driver); d.root, d.output_root, d.identity = root, output, identity
    d.log = Journal(attempt/'stop.jsonl', lambda: identity)
    d.stop_deadline = time.monotonic()+30
    write_once(attempt/'command.json', {**identity, 'argv': sys.argv, 'started_at': time.monotonic()})
    try:
        result = d.stop(); result.update(status='COMPLETED', **identity); code = 0
    except (ValueError, OSError) as error:
        result = {**identity, 'status': 'FAILED', 'reason': str(error)}; code = 1
    finally: d.log.close()
    write_once(attempt/'result.json', {**result, 'evidence_sha256': sha(attempt/'stop.jsonl')})
    return code


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--phase', choices=('seed', 'normal', 'burst', 'soak', 'evaluate', 'stop'), required=True)
    for name in ('manifest', 'profile', 'output', 'work-root'):
        parser.add_argument('--'+name, required=True, type=Path)
    args = parser.parse_args()
    try: return stop_entry(args) if args.phase == 'stop' else Driver(args).run()
    except Exception as error:
        print(json.dumps({'status': 'BLOCKED', 'phase': args.phase, 'reason': type(error).__name__,
                          'detail': str(error) if type(error) in (ValueError, KeyError) else 'preflight refused'}))
        return 2


if __name__ == '__main__': raise SystemExit(main())
