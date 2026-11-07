"""Real bounded faults on the VM; never stops shared PostgreSQL or fills its disk."""
import errno
import hashlib
import json
import os
from pathlib import Path
import secrets
import subprocess
import time
import unittest
import urllib.parse

from db_env import postgres_env
from faults import DatabaseProxy, MediaServer
from service_fixture import EnterpriseServiceFixture, ROLES


class RecoveryTests(EnterpriseServiceFixture, unittest.TestCase):
    @classmethod
    def configure(cls, config):
        fields = postgres_env(config['pg']['dsn'])
        cls.proxy = DatabaseProxy(fields.get('PGHOST', '127.0.0.1'), int(fields.get('PGPORT', '5432')))
        # PostgreSQL protocol bytes are forwarded unchanged, including authentication.
        dsn = config['pg']['dsn']
        if '://' in dsn:
            url = urllib.parse.urlsplit(dsn)
            userinfo = url.netloc.rsplit('@', 1)[0] + '@' if '@' in url.netloc else ''
            query = dict(urllib.parse.parse_qsl(url.query))
            query['connect_timeout'] = '2'
            config['pg']['dsn'] = urllib.parse.urlunsplit(url._replace(
                netloc=userinfo + '127.0.0.1:' + str(cls.proxy.port), query=urllib.parse.urlencode(query)))
        else:
            config['pg']['dsn'] += f' host=127.0.0.1 port={cls.proxy.port} connect_timeout=2'
        cls.disk = cls.work / 'staging'
        cls.disk.mkdir()
        result = subprocess.run(['sudo', '-n', 'mount', '-t', 'tmpfs', '-o',
            f'size=8m,mode=0700,uid={os.getuid()},gid={os.getgid()}', 'tmpfs', str(cls.disk)],
            capture_output=True, timeout=10)
        if result.returncode:
            raise RuntimeError('Cannot mount bounded dedicated tmpfs for ENOSPC qualification')
        cls.mounted = True

    @classmethod
    def cleanup(cls):
        try:
            super().cleanup()
        finally:
            try:
                if getattr(cls, 'mounted', False):
                    cls.unmount()
            finally:
                if getattr(cls, 'proxy', None):
                    cls.proxy.close()

    @classmethod
    def unmount(cls):
        # Only the unique directory created above may be unmounted.
        if cls.disk.parent != cls.work or not cls.work.name.startswith('lober-enterprise-http-'):
            raise RuntimeError('Refusing unsafe fault cleanup target')
        result = subprocess.run(['sudo', '-n', 'umount', str(cls.disk)], capture_output=True, timeout=10)
        if result.returncode:
            raise RuntimeError('Dedicated tmpfs cleanup failed')

    def payload(self):
        return Path(os.environ['LOBER_ENTERPRISE_FIXTURE']).read_bytes() + secrets.token_bytes(32)

    def register(self, payload, kind='upload', addr=None):
        source = {'kind': kind, 'size': len(payload), 'sha256': hashlib.sha256(payload).hexdigest()}
        if addr:
            source['addr'] = addr
        item = self.assert_status(self.admin('POST', '/isos', {'name': 'fault-' + secrets.token_hex(4),
            'family': 'linux', 'series': 'rocky', 'sources': [source]}), 200)
        self.addCleanup(self.delete_media, item)
        return item

    def delete_media(self, item):
        current = self.assert_status(self.admin('GET', '/isos/' + item['id']), 200)
        self.assert_status(self.admin('DELETE', '/isos/' + item['id'] + '?rev=' + str(current['rev'])), 200)
        deadline = time.monotonic() + 5
        owner = self.work / 'artifacts' / 'rocky' / '9.6' / 'x86_64' / '.lober-owner'
        while owner.exists() and owner.read_text().startswith(item['id'] + '\n') and time.monotonic() < deadline:
            time.sleep(.05)

    def upload(self, item, payload):
        return self.admin('PUT', '/isos/' + item['id'] + '/content', raw=payload,
            headers={'Content-Type': 'application/octet-stream', 'Content-Range': f'bytes 0-{len(payload)-1}/{len(payload)}'})

    def wait_ready(self, item, payload):
        deadline = time.monotonic() + 25
        while time.monotonic() < deadline:
            current = self.assert_status(self.admin('GET', '/isos/' + item['id']), 200)
            if current['state'] == 'ready':
                self.assertEqual(current['sha256'], hashlib.sha256(payload).hexdigest())
                return current
            self.assertNotEqual(current['state'], 'failed', 'Media failed after dependency recovery')
            time.sleep(.1)
        self.fail('Media did not become ready after dependency recovery')

    def remote(self, payload, mode):
        # Loopback media URLs are intentionally forbidden; bind the configured VM-only IP.
        server = MediaServer(payload, os.environ['LOBER_ENTERPRISE_VM_IP'])
        self.addCleanup(server.close)
        server.mode = mode
        item = self.register(payload, 'url', server.url)
        self.assertTrue(server.cut.wait(timeout=10), 'Upstream fault never happened')
        return server, item

    def test_download_tcp_cut_then_explicit_retry_recovers(self):
        payload = self.payload()
        server, item = self.remote(payload, 'cut')
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            progress = self.assert_status(self.admin('GET', '/isos/' + item['id'] + '/progress'), 200)
            if any(source['state'] == 'failed' and source['fetched'] > 0 for source in progress['sources']):
                break
            time.sleep(.05)
        self.assertNotEqual(progress['state'], 'ready')
        self.assertTrue(any(source['state'] == 'failed' and source['error'] for source in progress['sources']))
        server.mode = 'ok'
        self.assert_status(self.admin('POST', '/isos/' + item['id'] + '/retry'), 200)
        self.wait_ready(item, payload)
        self.assertGreaterEqual(len(server.requests), 2)
        print('Fault evidence: upstream TCP cut persisted failed source, explicit retry recovered matching SHA256', flush=True)

    def test_network_connect_timeout_then_explicit_retry_recovers(self):
        payload = self.payload()
        address = os.environ['LOBER_ENTERPRISE_VM_IP']
        server = MediaServer(payload, address)
        self.addCleanup(server.close)
        server.mode = 'ok'
        # Drop only SYN traffic to this test's ephemeral media port. Existing QA
        # and PostgreSQL ports are excluded. Always remove this exact rule.
        rule = ['OUTPUT', '-p', 'tcp', '-d', address, '--dport', str(server.server.server_port),
                '-m', 'comment', '--comment', self.schema + '-connect-timeout', '-j', 'DROP']
        command = ['sudo', '-n', 'iptables', '-w', '2']
        subprocess.run(command + ['-A'] + rule, check=True, capture_output=True, timeout=10)
        try:
            item = self.register(payload, 'url', server.url)
            deadline = time.monotonic() + 170
            while time.monotonic() < deadline:
                progress = self.assert_status(self.admin('GET', '/isos/' + item['id'] + '/progress'), 200)
                if any(source['state'] == 'failed' for source in progress['sources']):
                    break
                time.sleep(.5)
            self.assertNotEqual(progress['state'], 'ready')
            self.assertEqual(server.requests, [], 'Faulted TCP connection reached the HTTP server')
            self.assertTrue(any(source['state'] == 'failed' and
                ('timed out' in source['error'] or 'timeout' in source['error']) for source in progress['sources']),
                'Real TCP connect did not reach a persisted timeout within 170 seconds')
        finally:
            subprocess.run(command + ['-D'] + rule, check=True, capture_output=True, timeout=10)
        self.assert_status(self.admin('POST', '/isos/' + item['id'] + '/retry'), 200)
        self.wait_ready(item, payload)
        self.assertGreaterEqual(len(server.requests), 1)
        print('Fault evidence: SYN packets dropped on dedicated port, real TCP timeout persisted, rule removed and retry recovered', flush=True)

    def test_disk_full_during_upload_never_publishes_partial_media(self):
        payload = self.payload()
        item = self.register(payload)
        blocker = self.disk / 'bounded-enospc-blocker'
        try:
            with blocker.open('wb', buffering=0) as output:
                with self.assertRaises(OSError) as raised:
                    while True:
                        output.write(b'x' * 65536)
            self.assertEqual(raised.exception.errno, errno.ENOSPC)
            result = self.upload(item, payload)
            self.assertIn(result[0], (422, 500))
            progress = self.assert_status(self.admin('GET', '/isos/' + item['id'] + '/progress'), 200)
            self.assertNotEqual(progress['state'], 'ready')
        finally:
            blocker.unlink(missing_ok=True)
        self.assert_status(self.upload(item, payload), 200)
        self.wait_ready(item, payload)
        print('Fault evidence: real ENOSPC on isolated 8 MiB tmpfs, upload rejected, recovery SHA256 matched', flush=True)

    def test_database_connection_loss_rejects_write_and_recovers_without_duplicate(self):
        host = self.host()
        sn = 'DBFAULT-' + secrets.token_hex(4).upper()
        before = self.proxy.accepted
        self.proxy.disconnect()
        try:
            self.assertGreater(self.proxy.dropped, 0, 'No actual database connection was severed')
            response = self.admin('POST', '/hosts', {'sn': sn, 'attrs': {}})
            self.assertGreaterEqual(response[0], 500)
        finally:
            self.proxy.blocked = False
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            response = self.admin('GET', '/hosts/' + host['sn'])
            if response[0] == 200:
                break
            time.sleep(.1)
        self.assertEqual(self.assert_status(response, 200), host)
        self.assert_status(self.admin('GET', '/hosts/' + sn), 404)
        self.assert_status(self.admin('POST', '/hosts', {'sn': sn, 'attrs': {}}), 200)
        self.assertEqual(len(self.assert_status(self.admin('GET', '/hosts?prefix=' + sn), 200)['items']), 1)
        self.assertGreater(self.proxy.accepted, before)
        print('Fault evidence: dedicated DB sockets closed, write rejected, fresh pool connection recovered exactly once', flush=True)

    def test_kill_during_download_then_process_restart_recovers_source(self):
        payload = self.payload()
        server, item = self.remote(payload, 'hold')
        deadline = time.monotonic() + 5
        while time.monotonic() < deadline:
            progress = self.assert_status(self.admin('GET', '/isos/' + item['id'] + '/progress'), 200)
            if any(source['fetched'] > 0 for source in progress['sources']):
                break
            time.sleep(.05)
        self.assertTrue(any(0 < source['fetched'] < len(payload) for source in progress['sources']))
        host = self.host()
        self.proc.kill()
        self.proc.wait(timeout=10)
        self.assertLess(self.proc.returncode, 0)
        server.mode = 'ok'
        server.release.set()
        self.start()
        self.assert_status(self.admin('GET', '/hosts'), 401)
        for role in ROLES:
            self.tokens[role] = self.login('admin' if role == 'admin' else 'enterprise_' + role.replace('-', '_'), self.password)
        self.assertEqual(self.assert_status(self.admin('GET', '/hosts/' + host['sn']), 200), host)
        self.wait_ready(item, payload)
        self.assertTrue(any(offset > 0 for offset in server.requests), 'Restart did not resume the durable checkpoint with Range')
        print('Fault evidence: SIGKILL while upstream held body; persisted ISO recovered; prior session rejected', flush=True)
