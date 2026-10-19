"""Real local resource guards; passing these is not an OS install result."""
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

from wsl_safety import admit_memory, guest_lock, owned_disk, require_repeat_network


class WslSafetyTests(unittest.TestCase):
    def test_memory_requires_fresh_windows_and_linux_headroom(self):
        fresh = {'at': 100, 'available_bytes': 6 * 1024**3}
        admit_memory(fresh, 2048, 6 * 1024**3, now=110)
        for snapshot, linux_free in [({'at': 1, 'available_bytes': 6 * 1024**3}, 6 * 1024**3),
                                     ({'at': 111, 'available_bytes': 6 * 1024**3}, 6 * 1024**3),
                                     ({'at': 100, 'available_bytes': 2 * 1024**3}, 6 * 1024**3),
                                     ({'at': 100, 'available_bytes': float('nan')}, 6 * 1024**3),
                                     (fresh, float('nan')),
                                     (fresh, 2 * 1024**3)]:
            with self.subTest(snapshot=snapshot, linux_free=linux_free):
                with self.assertRaises(ValueError):
                    admit_memory(snapshot, 2048, linux_free, now=110)

    def test_guest_lock_refuses_a_second_system(self):
        with TemporaryDirectory() as folder:
            with guest_lock(Path(folder)/'guest.lock'):
                with self.assertRaises(BlockingIOError):
                    with guest_lock(Path(folder)/'guest.lock'):
                        self.fail('second guest admitted')
            with guest_lock(Path(folder)/'guest.lock'):
                pass

    def test_disk_must_be_owned_qcow_without_backing(self):
        with TemporaryDirectory() as folder:
            root = Path(folder)
            (root/'OWNED-BY-LOBER-WSL-QUALIFICATION').write_text('linux-install-qualification-20261019\n')
            case = root/'real-debian'; case.mkdir()
            disk = case/'target.qcow2'; disk.write_bytes(b'disk-placeholder-for-path-guard')
            info = {'format': 'qcow2', 'virtual-size': 32*1024**3, 'filename': str(disk)}
            owned_disk(root, disk, info)
            for invalid in [dict(info, format='raw'), dict(info, **{'virtual-size': 64*1024**3}),
                            dict(info, **{'backing-filename': '/dev/sda'}),
                            dict(info, **{'format-specific': {'type': 'qcow2', 'data': {'data-file': '/tmp/external-data'}}})]:
                with self.assertRaises(ValueError): owned_disk(root, disk, invalid)
            link = case/'other.qcow2'; link.symlink_to(disk)
            with self.assertRaises(ValueError): owned_disk(root, link, info)
            (root/'OWNED-BY-LOBER-WSL-QUALIFICATION').write_text('someone-else')
            with self.assertRaises(ValueError): owned_disk(root, disk, info)

    def test_repeat_boot_does_not_accept_initial_capture(self):
        request = b'GET /v1/boot?sn=LOBER-WSL-20261019&arch=x86_64'
        reply = b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n'
        packets = request + reply
        require_repeat_network(packets + packets, len(packets), 'LOBER-WSL-20261019')
        with self.assertRaises(ValueError):
            require_repeat_network(packets + b'new boot with no network proof', len(packets), 'LOBER-WSL-20261019')
        with self.assertRaises(ValueError):
            require_repeat_network(request + reply, 0, 'ANOTHER-SYSTEM')


if __name__ == '__main__':
    unittest.main()
