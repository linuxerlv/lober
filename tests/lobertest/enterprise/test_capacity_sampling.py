from pathlib import Path
import tempfile
import unittest

from production_capacity import live_registered_child


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
