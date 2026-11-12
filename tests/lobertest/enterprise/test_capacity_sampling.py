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

    def test_login_pacing_is_shared_across_capacity_phases(self):
        import threading
        with tempfile.TemporaryDirectory() as directory, patch('production_capacity.private_file', side_effect=lambda p, _: p):
            root = Path(directory)
            for now, timestamp, expected_delay in [(10, 10, 0), (11, 12.2, 1.2)]:
                driver = Driver.__new__(Driver)
                driver.root, driver.login_lock = root, threading.Lock()
                driver.stop_event, driver.request = Mock(), Mock(return_value={'token': 'fixture'})
                driver.stop_event.wait.return_value = False
                with patch('production_capacity.time.monotonic', side_effect=[now, timestamp]):
                    driver.request_login({'username': 'operator'}, 0)
                if expected_delay:
                    self.assertAlmostEqual(driver.stop_event.wait.call_args.args[0], expected_delay)
                else:
                    driver.stop_event.wait.assert_not_called()
