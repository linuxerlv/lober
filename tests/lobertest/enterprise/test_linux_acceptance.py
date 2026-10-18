"""Checker regressions only; these are not actual OS qualification results."""
import copy
import unittest
from linux_install import require_hook_failure, require_network_first


class LinuxAcceptanceChecks(unittest.TestCase):
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
