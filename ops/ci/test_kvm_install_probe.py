import unittest

from kvm_install_probe import case_identity, stable_guest


class InstallEvidenceTests(unittest.TestCase):
    def test_four_cases_have_distinct_identities(self):
        cases = [case_identity(name) for name in ('parallel-1', 'parallel-2', 'serial-1', 'serial-2')]
        for field in ('serial', 'mac', 'uuid'):
            self.assertEqual(len({case[field] for case in cases}), 4)
        with self.assertRaises(ValueError):
            case_identity('parallel-3')

    def test_repeat_requires_same_system_and_a_new_boot(self):
        first = {'id': 'debian', 'version': '13', 'root': '/dev/vda2', 'machine': 'a'*32,
                 'boot': '00000000-0000-4000-8000-000000000001'}
        second = {**first, 'boot': '00000000-0000-4000-8000-000000000002'}
        self.assertTrue(stable_guest(first, second))
        self.assertFalse(stable_guest(first, first))
        self.assertFalse(stable_guest(first, {**second, 'machine': 'b'*32}))
