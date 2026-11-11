"""Fault fixture cleanup must release active service handles before dependencies."""
from pathlib import Path
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch

import test_recovery as recovery


class CleanupTests(unittest.TestCase):
    def test_active_candidate_stops_before_unmount_and_database_proxy_close(self):
        actions = []

        class Process:
            alive = True

            def poll(self):
                return None if self.alive else 0

            def terminate(self):
                actions.append('stop-service')
                self.alive = False

            def wait(self, timeout):
                return 0

        process = Process()

        class Proxy:
            port = 43210

            def close(self):
                if process.alive:
                    raise RuntimeError('Proxy removed while candidate was alive')
                actions.append('close-proxy')

        with tempfile.TemporaryDirectory(prefix='lober-cleanup-unit-') as temporary:
            class Probe(recovery.RecoveryTests):
                _class_cleanups = []
                proc = process
                log = None
                config = None
                schema = 'enterprise_cleanup_probe'
                work = Path(temporary)

                @classmethod
                def db(cls, sql):
                    actions.append('drop-schema')

                @classmethod
                def unmount(cls):
                    if process.alive:
                        raise RuntimeError('Staging still held by live candidate')
                    actions.append('unmount')

            # Exercise the same registration order as real fixture setup.
            Probe.addClassCleanup(Probe.cleanup)
            with patch.object(recovery, 'DatabaseProxy', return_value=Proxy()), \
                    patch.object(recovery.subprocess, 'run', return_value=SimpleNamespace(returncode=0)):
                Probe.configure({'pg': {'dsn': 'host=127.0.0.1 dbname=cleanup_probe'}})
            Probe.doClassCleanups()
            self.assertEqual(Probe.tearDown_exceptions, [])
            self.assertEqual(actions, ['stop-service', 'drop-schema', 'unmount', 'close-proxy'])
