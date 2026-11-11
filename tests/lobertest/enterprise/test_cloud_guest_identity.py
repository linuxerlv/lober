"""只在 lober VM 执行的双通道身份样本；不连接真实客户。"""
import base64
import hashlib
import importlib.util
import contextlib
import io
import json
import os
from pathlib import Path
import socket
import struct
import subprocess
import tempfile
import unittest
from unittest import mock

SOURCE = Path(__file__).with_name('cloud_guest_identity.py')
if SOURCE.exists():
    spec = importlib.util.spec_from_file_location('cloud_guest_identity', SOURCE)
    helper = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(helper)
else:
    helper = None


class GuestIdentityTests(unittest.TestCase):
    def setUp(self):
        if socket.gethostname() != 'lober' or os.getuid() != 1000:
            raise RuntimeError('这些样本只能在 lober VM 的 dev 用户运行')
        self.assertIsNotNone(helper, '缺少真正核对 SSH 客户身份的 helper')
        self.temp = tempfile.TemporaryDirectory(prefix='lober-identity-')
        self.addCleanup(self.temp.cleanup)
        self.base = Path(self.temp.name)
        self.key = self.base / 'login-key'
        # 只是私有文件权限/摘要样本；不会交给真实 SSH。
        self.key.write_text('sample-only-not-a-private-key\n')
        self.key.chmod(0o600)
        wire = struct.pack('>I', 11) + b'ssh-ed25519' + struct.pack('>I', 32) + b'x' * 32
        self.hostkey = 'ssh-ed25519 ' + base64.b64encode(wire).decode()
        self.identity = {'address': '192.168.201.102', 'ssh_host_public_key': self.hostkey,
                         'smbios_serial': 'PQ25L02', 'machine_id': 'a' * 32,
                         'boot_id': '730d9cb3-320d-478a-80cd-8052369f901c',
                         'root_uuid': '7d4d20a2-06ab-48e8-af24-ac52b535f508',
                         'root_disk_serial': 'PQ25_LINUX02_DISK'}
        self.owner = {'purpose': 'production-qualification-20261025', 'run_id': 'identity-sample',
                      'target_uuid': 'a1d224af-2d45-4e39-8ad3-b07355bb09fb'}
        self.target = {'uuid': self.owner['target_uuid'], 'sn': 'PQ25L02',
                       'disk_serial': 'PQ25_LINUX02_DISK', 'guest_ip': self.identity['address']}
        self.config = {'owner': self.owner, 'guest_ssh_user': 'lober',
                       'guest_ssh_identity_file': str(self.key)}
        self.raw = self.base / 'qga-original.json'
        self.raw.write_text(json.dumps(self.identity))
        self.raw.chmod(0o600)
        self.agent = {'target_uuid': self.target['uuid'], 'transport': 'libvirt-qga',
                      'collection_script_sha256': hashlib.sha256(helper.GUEST_IDENTITY_SCRIPT.encode()).hexdigest(),
                      'identity': self.identity.copy(), 'raw_path': str(self.raw),
                      'raw_sha256': hashlib.sha256(self.raw.read_bytes()).hexdigest()}
        self.calls = []

    def runner(self, argv, stdin=None, timeout=None):
        self.calls.append((argv, stdin, timeout))
        return subprocess.CompletedProcess(argv, 0, json.dumps(self.identity), '')

    def verify(self, runner=None):
        return helper.verify_guest_identity(self.config, self.target, self.agent,
                                           self.base / 'access', runner or self.runner)

    def test_correct_second_target_and_strict_access(self):
        result = self.verify()
        self.assertEqual(result['ssh_exit'], 0)
        self.assertEqual(result['smbios_serial'], 'PQ25L02')
        argv, script, timeout = self.calls[0]
        self.assertEqual(argv[0], '/usr/bin/ssh')
        self.assertEqual(script, helper.GUEST_IDENTITY_SCRIPT)
        self.assertEqual(argv[-2:], ['pq-guest', '/usr/bin/python3 -'])
        config = (self.base / 'access' / 'ssh_config').read_text()
        for rule in ('StrictHostKeyChecking yes', 'BatchMode yes', 'IdentitiesOnly yes',
                     'ProxyCommand none', 'IdentityAgent none', 'GlobalKnownHostsFile /dev/null'):
            self.assertIn(rule, config)
        self.assertNotIn('accept-new', config)
        self.assertEqual((self.base / 'access' / 'known_hosts').read_text(),
                         'pq-guest-' + self.target['uuid'] + ' ' + self.hostkey + '\n')
        self.assertEqual((self.base / 'access').stat().st_mode & 0o777, 0o700)
        self.assertEqual((self.base / 'access' / 'result.json').stat().st_mode & 0o777, 0o600)

    def test_exit_zero_wrong_machine_or_disk_address_key_boot_rejected(self):
        other_wire = struct.pack('>I', 11) + b'ssh-ed25519' + struct.pack('>I', 32) + b'y' * 32
        other_hostkey = 'ssh-ed25519 ' + base64.b64encode(other_wire).decode()
        self.assertNotEqual(other_hostkey, self.hostkey)
        for field in self.identity:
            with self.subTest(field=field):
                output = self.base / ('bad-' + field)
                wrong = self.identity.copy()
                wrong[field] = {'address': '192.168.201.10', 'machine_id': 'b' * 32,
                                'ssh_host_public_key': other_hostkey,
                                'boot_id': '00000000-0000-4000-8000-000000000000',
                                'root_uuid': '11111111-1111-4111-8111-111111111111'}.get(field, 'OTHER')
                failure = (self.assertRaisesRegex(ValueError, '^SSH 与可信客户身份不符：ssh_host_public_key$')
                           if field == 'ssh_host_public_key' else self.assertRaises(ValueError))
                with failure:
                    helper.verify_guest_identity(self.config, self.target, self.agent, output,
                        lambda *a, **kw: subprocess.CompletedProcess(a[0], 0, json.dumps(wrong), ''))
                self.assertTrue((output / 'failure.json').exists())

    def test_unknown_missing_duplicate_and_wrong_type_rejected(self):
        samples = [json.dumps({**self.identity, 'unexpected': 1}),
                   json.dumps({k: v for k, v in self.identity.items() if k != 'boot_id'}),
                   json.dumps(self.identity)[:-1] + ',"boot_id":"another"}',
                   json.dumps({**self.identity, 'machine_id': None}), '{}\n{}', 'not-json']
        for index, raw in enumerate(samples):
            with self.subTest(index=index), self.assertRaises(ValueError):
                helper.verify_guest_identity(self.config, self.target, self.agent,
                    self.base / ('invalid-' + str(index)),
                    lambda *a, **kw: subprocess.CompletedProcess(a[0], 0, raw, ''))

    def test_login_failure_and_timeout_preserve_original(self):
        with self.assertRaises(ValueError):
            self.verify(lambda *a, **kw: subprocess.CompletedProcess(a[0], 255, 'partial', 'denied'))
        self.assertEqual((self.base / 'access' / 'ssh.stderr').read_text(), 'denied')
        def timeout(*args, **kwargs):
            raise subprocess.TimeoutExpired(args[0], 2, output=b'partial-out', stderr=b'timeout-error')
        with self.assertRaises(subprocess.TimeoutExpired):
            helper.verify_guest_identity(self.config, self.target, self.agent, self.base / 'timed', timeout)
        self.assertEqual((self.base / 'timed' / 'ssh.stdout').read_text(), 'partial-out')
        self.assertEqual(json.loads((self.base / 'timed' / 'failure.json').read_text())['error_type'], 'TimeoutExpired')

    def test_trusted_channel_and_raw_tampering_rejected_before_ssh(self):
        for field, bad in [('target_uuid', '00000000-0000-4000-8000-000000000000'),
                           ('transport', 'ssh'), ('collection_script_sha256', '0' * 64),
                           ('raw_sha256', '0' * 64)]:
            with self.subTest(field=field), self.assertRaises(ValueError):
                agent = {**self.agent, field: bad}
                helper.verify_guest_identity(self.config, self.target, agent,
                                             self.base / ('trust-' + field), self.runner)
        self.assertEqual(self.calls, [])

    def test_private_file_and_config_tampering_rejected(self):
        for name in ('ssh_config', 'known_hosts', 'binding.json'):
            with self.subTest(name=name):
                def tamper(argv, **kwargs):
                    (Path(argv[2]).parent / name).write_text('changed')
                    return subprocess.CompletedProcess(argv, 0, json.dumps(self.identity), '')
                with self.assertRaises(ValueError):
                    helper.verify_guest_identity(self.config, self.target, self.agent,
                                                 self.base / ('tamper-' + name), tamper)
        self.key.chmod(0o644)
        with self.assertRaises(ValueError):
            self.verify()

    def test_symlink_and_reused_output_rejected(self):
        link = self.base / 'link-key'
        link.symlink_to(self.key)
        self.config['guest_ssh_identity_file'] = str(link)
        with self.assertRaises(ValueError):
            self.verify()
        self.config['guest_ssh_identity_file'] = str(self.key)
        reused = self.base / 'reused'
        reused.mkdir(mode=0o700)
        with self.assertRaises(ValueError):
            helper.verify_guest_identity(self.config, self.target, self.agent, reused, self.runner)

    def test_command_runner_string_result(self):
        self.assertEqual(self.verify(lambda *a, **kw: json.dumps(self.identity))['ssh_exit'], 0)

    def test_generated_config_parsed_by_openssh_without_connection(self):
        def inspect_config(argv, **kwargs):
            parsed = subprocess.run([argv[0], '-G', *argv[1:-1]], capture_output=True,
                                    text=True, timeout=5, check=True)
            settings = dict(line.split(' ', 1) for line in parsed.stdout.splitlines())
            self.assertEqual(settings['hostname'], '192.168.201.102')
            self.assertEqual(settings['hostkeyalias'], 'pq-guest-' + self.target['uuid'])
            self.assertEqual(settings['stricthostkeychecking'], 'true')
            self.assertEqual(settings['batchmode'], 'yes')
            self.assertEqual(settings['identitiesonly'], 'yes')
            self.assertEqual(settings['identityagent'], 'none')
            self.assertEqual(settings['globalknownhostsfile'], '/dev/null')
            self.assertNotIn('proxycommand', settings)
            return json.dumps(self.identity)
        self.verify(inspect_config)

    def test_first_target_does_not_depend_on_module_global(self):
        first_owner = {'purpose': 'production-qualification-20261025', 'run_id': 'pq25-linux01-20261025',
                       'target_uuid': '7fe71ebb-a34d-4155-8410-d1427ca2f83a'}
        first_target = {'uuid': first_owner['target_uuid'], 'sn': 'PQ25L01',
                        'disk_serial': 'PQ25_LINUX01_DISK', 'guest_ip': '192.168.201.101'}
        first_wire = struct.pack('>I', 11) + b'ssh-ed25519' + struct.pack('>I', 32) + b'z' * 32
        first_identity = {'address': first_target['guest_ip'],
                          'ssh_host_public_key': 'ssh-ed25519 ' + base64.b64encode(first_wire).decode(),
                          'smbios_serial': first_target['sn'], 'machine_id': 'c' * 32,
                          'boot_id': '80c266a2-c8f5-4b5f-8651-89ea4274818f',
                          'root_uuid': 'eab36fbe-0127-4ab7-94c2-e055fcf8478c',
                          'root_disk_serial': first_target['disk_serial']}
        first_raw = self.base / 'first-qga-original.json'
        first_raw.write_text(json.dumps(first_identity))
        first_raw.chmod(0o600)
        first_agent = {**self.agent, 'target_uuid': first_target['uuid'], 'identity': first_identity,
                       'raw_path': str(first_raw), 'raw_sha256': hashlib.sha256(first_raw.read_bytes()).hexdigest()}
        first_config = {**self.config, 'owner': first_owner}
        second_result = self.verify()
        first_result = helper.verify_guest_identity(first_config, first_target, first_agent,
            self.base / 'first-access', lambda *args, **kwargs: json.dumps(first_identity))
        for result, identity, owner, target, output in (
                (first_result, first_identity, first_owner, first_target, 'first-access'),
                (second_result, self.identity, self.owner, self.target, 'access')):
            with self.subTest(target_uuid=target['uuid']):
                self.assertEqual({field: result[field] for field in identity}, identity)
                self.assertEqual(result['ssh_exit'], 0)
                self.assertEqual(result['access']['owner'], owner)
                self.assertEqual(result['access']['target_uuid'], target['uuid'])
                binding = json.loads((self.base / output / 'binding.json').read_text())
                self.assertEqual(binding['target_contract'], target)
                self.assertEqual(binding['agent_evidence']['target_uuid'], target['uuid'])
                self.assertEqual((self.base / output / 'known_hosts').read_text(),
                    'pq-guest-' + target['uuid'] + ' ' + identity['ssh_host_public_key'] + '\n')
        mixed = [
            (first_config, self.target, self.agent, self.identity, False),
            (self.config, first_target, first_agent, first_identity, False),
            (first_config, first_target, self.agent, first_identity, False),
            (self.config, self.target, first_agent, self.identity, False),
            (first_config, first_target, first_agent, self.identity, True),
            (self.config, self.target, self.agent, first_identity, True),
        ]
        for index, (config, target, agent, ssh_identity, called) in enumerate(mixed):
            runner = mock.Mock(return_value=subprocess.CompletedProcess([], 0, json.dumps(ssh_identity), ''))
            with self.subTest(mixed=index), self.assertRaises(ValueError):
                helper.verify_guest_identity(config, target, agent, self.base / ('mixed-' + str(index)), runner)
            self.assertEqual(runner.called, called)

    def test_collection_script_reads_live_facts_and_rejects_ambiguous_address(self):
        files = {
            '/sys/class/dmi/id/product_serial': self.identity['smbios_serial'],
            '/etc/ssh/ssh_host_ed25519_key.pub': self.hostkey + ' root@sample',
            '/etc/machine-id': self.identity['machine_id'],
            '/proc/sys/kernel/random/boot_id': self.identity['boot_id'],
        }
        mapped = {}
        for index, (original, content) in enumerate(files.items()):
            path = self.base / ('collected-' + str(index))
            path.write_text(content)
            mapped[original] = path
        addresses = [{'addr_info': [{'family': 'inet', 'local': self.identity['address']},
                                   {'family': 'inet', 'local': '127.0.0.1'}]}]
        def sample_run(args, **kwargs):
            outputs = {('findmnt', '-n', '-o', 'SOURCE', '/'): '/dev/vda1',
                       ('lsblk', '-s', '-n', '-o', 'SERIAL', '/dev/vda1'): '\nPQ25_LINUX02_DISK\n',
                       ('findmnt', '-n', '-o', 'UUID', '/'): self.identity['root_uuid'],
                       ('ip', '-j', 'address'): json.dumps(addresses)}
            return outputs[tuple(args)]
        with mock.patch('pathlib.Path', side_effect=lambda value: mapped[value]), \
             mock.patch('subprocess.check_output', side_effect=sample_run):
            capture = io.StringIO()
            with contextlib.redirect_stdout(capture):
                exec(helper.GUEST_IDENTITY_SCRIPT, {})
            self.assertEqual(json.loads(capture.getvalue()), self.identity)
            addresses[0]['addr_info'].append({'family': 'inet', 'local': '192.168.201.10'})
            with self.assertRaisesRegex(ValueError, 'ambiguous'):
                exec(helper.GUEST_IDENTITY_SCRIPT, {})

    def test_private_key_replaced_or_permissions_changed_during_ssh(self):
        for change in ('bytes', 'mode'):
            with self.subTest(change=change):
                self.key.write_text('sample-only-not-a-private-key\n')
                self.key.chmod(0o600)
                def mutate(argv, **kwargs):
                    if change == 'bytes':
                        self.key.write_text('replaced')
                    else:
                        self.key.chmod(0o644)
                    return json.dumps(self.identity)
                with self.assertRaises(ValueError):
                    helper.verify_guest_identity(self.config, self.target, self.agent,
                                                 self.base / ('key-' + change), mutate)

    def test_expansion_paths_and_login_name_injection_rejected(self):
        self.config['guest_ssh_user'] = 'lober\nProxyCommand unexpected'
        with self.assertRaises(ValueError):
            self.verify()
        self.config['guest_ssh_user'] = 'lober'
        self.config['guest_ssh_identity_file'] = str(self.base / '%h-key')
        with self.assertRaises(ValueError):
            helper.verify_guest_identity(self.config, self.target, self.agent,
                                         self.base / 'expansion', self.runner)
        self.assertEqual(self.calls, [])


if __name__ == '__main__':
    unittest.main()
