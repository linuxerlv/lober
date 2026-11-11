"""只在本轮独立网络与挂载空间内执行有界故障。"""
import json
import os
from pathlib import Path
import pwd
import re
import secrets
import socket
import struct
import subprocess
import sys
import tempfile
import threading


SOURCE_IP = '192.0.2.1'
DEV = 'lober-ci-dev'


def fault(args, work, work_fd, uid, gid):
    if args and args[0] == 'iptables':
        port = args[10] if len(args) == 17 else ''
        comment = args[14] if len(args) == 17 else ''
        for action in ('-A', '-D'):
            rule = ['iptables', '-w', '2', action, 'OUTPUT', '-p', 'tcp', '-d',
                    SOURCE_IP, '--dport', port, '-m', 'comment', '--comment', comment, '-j', 'DROP']
            if args == rule and port.isdecimal() and 1024 <= int(port) <= 65535 and re.fullmatch(
                    r'enterprise_[0-9a-f]{16}-connect-timeout', comment):
                return subprocess.run(['/usr/sbin/iptables', *rule[1:]], timeout=4,
                                      env={'XTABLES_LOCKFILE': str(work.parent / 'xtables.lock')}).returncode
        return 2
    mount = ['mount', '-t', 'tmpfs', '-o', f'size=8m,mode=0700,uid={uid},gid={gid}', 'tmpfs']
    if args[:-1] not in (mount, ['umount']):
        return 2
    target = Path(args[-1])
    if (target.parent.parent != work or target.name != 'staging'
            or not target.parent.name.startswith('lober-enterprise-http-')):
        return 2
    flags = os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW
    parent = os.open(target.parent.name, flags, dir_fd=work_fd)
    try:
        fd = os.open('staging', flags, dir_fd=parent)
        try:
            if os.fstat(fd).st_uid != uid:
                return 2
            command = ['/usr/bin/mount', '-in', '--no-canonicalize', *mount[1:]] if args[:-1] == mount else [
                '/usr/bin/umount', '-iln', '--no-canonicalize']
            return subprocess.run([*command, f'/proc/self/fd/{fd}'], pass_fds=(fd,), timeout=4).returncode
        finally:
            os.close(fd)
    finally:
        os.close(parent)


def serve(listener, work, work_fd, uid, gid):
    while True:
        with listener.accept()[0] as connection:
            connection.settimeout(2)
            _, peer_uid, _ = struct.unpack('3i', connection.getsockopt(socket.SOL_SOCKET, socket.SO_PEERCRED, 12))
            code = 2
            if peer_uid == uid:
                try:
                    with connection.makefile('rb') as request:
                        data = request.read(4097)
                    args = json.loads(data)
                    if len(data) <= 4096 and isinstance(args, list) and all(isinstance(arg, str) for arg in args):
                        code = fault(args, work, work_fd, uid, gid)
                except (OSError, ValueError, subprocess.SubprocessError):
                    pass
            try:
                connection.sendall(str(code).encode())
            except OSError:
                pass


def main(sha, mode='run'):
    if os.geteuid() != 0 or not re.fullmatch('[0-9a-f]{40}', sha):
        return 2
    if any(os.path.samestat(os.stat(f'/proc/self/ns/{ns}'), os.stat(f'/proc/1/ns/{ns}'))
           for ns in ('net', 'mnt')):
        return 2
    links = json.loads(subprocess.check_output(['/usr/sbin/ip', '-j', 'link', 'show']))
    if [row['ifname'] for row in links] != ['lo'] or mode not in ('run', 'probe'):
        return 2
    subprocess.run(['/usr/sbin/ip', 'link', 'set', 'lo', 'up'], check=True)
    subprocess.run(['/usr/sbin/ip', 'address', 'add', SOURCE_IP + '/32', 'dev', 'lo'], check=True)
    user = pwd.getpwnam(DEV)
    root = Path('/var/lib/lober-ci-dev')
    candidate = root / sha
    results = candidate / ('logs' if mode == 'run' else 'diagnostic-recovery')
    as_dev = {'user': user.pw_uid, 'group': user.pw_gid, 'extra_groups': [], 'umask': 0o077}
    address = 'lober-recovery-' + secrets.token_hex(16)
    with tempfile.TemporaryDirectory(prefix='lober-recovery-') as temporary, socket.socket(socket.AF_UNIX) as listener:
        sandbox = Path(temporary)
        sandbox.chmod(0o711)
        work = sandbox / 'work'
        work.mkdir(mode=0o700)
        work_fd = os.open(work, os.O_RDONLY | os.O_DIRECTORY | os.O_NOFOLLOW)
        os.chown(work, user.pw_uid, user.pw_gid)
        listener.bind('\0' + address)
        listener.listen()
        threading.Thread(target=serve, args=(listener, work, work_fd, user.pw_uid, user.pw_gid), daemon=True).start()
        env = {'PATH': '/opt/lober-ci/ops/ci/recovery-bin:/usr/local/bin:/usr/bin:/bin',
               'HOME': str(root), 'TMPDIR': str(work), 'PYTHONUTF8': '1',
               'LOBER_CI_FAULT_SOCKET': address, 'LOBER_ENTERPRISE_VM_IP': SOURCE_IP,
               'LOBER_TEST_PG_DSN': 'host=127.0.0.1 port=5432 user=lober-ci-dev dbname=lober_ci sslmode=disable',
               'LOBER_ENTERPRISE_BINARY': str(candidate / 'lober'),
               'LOBER_ENTERPRISE_FIXTURE': str(candidate / 'synthetic-rocky.iso')}
        # 使用本阶段测试库的 Unix 套接字。临时 TCP 端口只在本轮独立网络内可见。
        with subprocess.Popen(['/usr/bin/systemd-socket-activate', '--listen=127.0.0.1:5432',
                               '/usr/lib/systemd/systemd-socket-proxyd', '/run/lober-ci-db-dev/.s.PGSQL.5432'],
                              env=env, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL, **as_dev) as proxy:
            try:
                for attempt in range(20):
                    try:
                        with socket.create_connection(('127.0.0.1', 5432), timeout=1):
                            break
                    except OSError:
                        if proxy.poll() is not None or attempt == 19:
                            raise
                        threading.Event().wait(0.05)
                return subprocess.run(['/usr/bin/python3', '-E', '-s', '-B',
                                       'tests/lobertest/enterprise/run.py', 'recovery',
                                       '--output', str(results), '--manifest', str(candidate / 'candidate.json'),
                                       '--product-sha', sha, '--test-sha', sha], cwd=candidate / 'source',
                                      env=env, timeout=480, **as_dev).returncode
            finally:
                proxy.terminate()


if __name__ == '__main__':
    sys.exit(main(*sys.argv[1:]))
