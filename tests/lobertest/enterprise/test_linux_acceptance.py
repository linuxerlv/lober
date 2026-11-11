"""Checker regressions only; these are not actual OS qualification results."""
import copy
import base64
import unittest
import json
import subprocess
import sys
from pathlib import Path
from tempfile import TemporaryDirectory
from unittest.mock import patch
from linux_install import Campaign, OS, SN, require_hook_failure, require_network_first, require_owned_serial


class LinuxAcceptanceChecks(unittest.TestCase):
    def test_current_campaign_uses_only_supported_linux_and_current_templates(self):
        self.assertEqual(OS, {
            'rhel': ('rhel', 'kickstart', 5),
            'almalinux': ('almalinux', 'kickstart', 5),
            'rocky': ('rocky', 'kickstart', 5),
            'ubuntu': ('ubuntu', 'autoinstall', 4),
            'debian': ('debian', 'preseed', 5),
        })

    def assert_retired_os_rejected(self, entry):
        for system in ('leap15', 'leap16', 'sles15', 'sles16', 'openeuler', 'opencloudos'):
            with self.subTest(entry=entry, system=system):
                result = subprocess.run([
                    sys.executable, str(Path(__file__).with_name(entry)), 'prepare',
                    '--state-dir', 'missing-state-fixture', '--os', system,
                ], capture_output=True, text=True, timeout=15)
                self.assertEqual(result.returncode, 2, result.stderr)
                self.assertIn('invalid choice', result.stderr)

    def test_retired_os_is_rejected_before_reading_state(self):
        self.assert_retired_os_rejected('linux_install.py')

    @unittest.skipUnless(sys.platform == 'linux', 'WSL 工具依赖 Linux 文件锁')
    def test_wsl_rejects_retired_os_before_reading_state(self):
        self.assert_retired_os_rejected('wsl_install.py')

    def test_cancelled_task_stops_wait_without_sleeping(self):
        campaign = object.__new__(Campaign)
        campaign.system, campaign.negative = 'debian', False
        campaign.manifest = {'product_sha': 'a' * 40, 'test_sha': 'b' * 40, 'binary_sha256': 'c' * 64}
        campaign.qa = {'test_sha': 'b' * 40}
        campaign.task = lambda: {'id': 'cancelled-fixture', 'state': 'cancelled'}
        campaign.events = lambda task: []
        with TemporaryDirectory() as folder:
            campaign.run = Path(folder)
            with patch('linux_install.time.sleep', side_effect=RuntimeError('cancelled task kept polling')):
                with self.assertRaisesRegex(AssertionError, 'unexpected terminal state'):
                    campaign.wait()
            result = json.loads((campaign.run / 'result.json').read_text())
            self.assertEqual((result['status'], result['state']), ('FAIL', 'cancelled'))

    def test_pve_encoded_serial_keeps_machine_identity(self):
        require_owned_serial({'smbios1': 'uuid=example,serial=' + SN})
        encoded = base64.b64encode(SN.encode()).decode()
        require_owned_serial({'smbios1': 'uuid=example,serial=' + encoded + ',base64=1'})
        with self.assertRaises(AssertionError):
            require_owned_serial({'smbios1': 'uuid=example,serial=OTHER'})
    def test_disk_first_is_not_gateway_reboot_evidence(self):
        with self.assertRaises(AssertionError):
            require_network_first({'boot': 'order=scsi0;net0'})
        require_network_first({'boot': 'order=net0;scsi0'})

    def test_timeout_or_another_hook_is_not_required_hook_failure(self):
        task = {'snapshot': {'slots': [{'name': 'qualify', 'seq': 6, 'exec': True, 'timing': 'firstboot',
            'instances': [{'required': True, 'digest': 'a' * 64}]}]}}
        good = {'kind': 'hook.report', 'fields': {'slot': '6', 'instance': '1', 'timing': 'firstboot', 'digest': 'a' * 64, 'rc': '41'}}
        for field, value in [('slot', '5'), ('instance', '2'), ('timing', 'post-target'), ('digest', 'b' * 64), ('rc', '0')]:
            wrong = copy.deepcopy(good)
            wrong['fields'][field] = value
            with self.assertRaises(AssertionError):
                require_hook_failure(task, [wrong])
        with self.assertRaises(AssertionError):
            require_hook_failure(task, [{'kind': 'machine.timeout', 'fields': {}}])
        require_hook_failure(task, [good])
