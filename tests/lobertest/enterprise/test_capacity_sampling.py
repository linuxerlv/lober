from pathlib import Path
import tempfile
import unittest
from unittest.mock import Mock, patch

from production_capacity import Driver, live_registered_child


class CapacitySamplingTests(unittest.TestCase):
    def test_exited_child_and_reused_pid_are_distinct_from_live_child(self):
        with tempfile.TemporaryDirectory() as directory:
            proc = Path(directory)
            registered = {'pid': 123, 'start_ticks': '45'}
            self.assertFalse(live_registered_child(registered, proc))
            (proc/'123').mkdir()
            status = proc/'123/stat'
            for state, start, live in [('S', '45', True), ('Z', '45', False), ('S', '46', False)]:
                status.write_text('123 (postgres backend) ' + ' '.join([state] + ['0']*18 + [start]))
                self.assertEqual(live_registered_child(registered, proc), live)

    @patch('production_capacity.time.sleep')
    @patch('production_capacity.live_registered_child')
    def test_resamples_same_child_and_keeps_persistent_errors(self, live, sleep):
        driver = Driver.__new__(Driver)
        original = {'pid': 123, 'start_ticks': '45'}
        sample = {'alive': True, 'identity_ok': True}
        live.return_value = True
        driver.proc_sample = Mock(side_effect=[FileNotFoundError(), sample])
        self.assertEqual(driver.sample_registered_child(original), sample)
        driver.proc_sample = Mock(side_effect=PermissionError())
        with self.assertRaises(PermissionError):
            driver.sample_registered_child(original)
        self.assertEqual(driver.proc_sample.call_count, 4)
        live.return_value = False
        self.assertIsNone(driver.sample_registered_child(original))
