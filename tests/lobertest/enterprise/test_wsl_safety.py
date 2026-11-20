"""Real local resource guards; passing these is not an OS install result."""
from pathlib import Path
from tempfile import TemporaryDirectory
import unittest

from wsl_safety import admit_memory, guest_lock, owned_disk, require_repeat_network, require_runtime_memory, require_guest_unit


class WslSafetyTests(unittest.TestCase):
    def test_real_guest_requires_its_own_capped_systemd_group(self):
        properties={'MainPID':'123','MemoryMax':str(2560*1024**2),'MemorySwapMax':'0','KillMode':'control-group',
                    'SendSIGKILL':'yes','Restart':'no','TimeoutStopUSec':'10s'}
        cgroup='0::/system.slice/lober-wsl-guest-20261019.service\n'
        require_guest_unit(properties,123,2048,cgroup)
        for change in [{'MainPID':'456'},{'MemoryMax':'infinity'},{'MemorySwapMax':'infinity'},
                       {'KillMode':'process'},{'SendSIGKILL':'no'},{'Restart':'always'},{'TimeoutStopUSec':'90s'}]:
            with self.assertRaises(ValueError):require_guest_unit(dict(properties,**change),123,2048,cgroup)
        with self.assertRaises(ValueError):require_guest_unit(properties,123,2048,'0::/user.slice/another.service')

    def test_running_work_stops_before_host_reserve_is_exhausted(self):
        fresh = {'at': 100, 'available_bytes': 2*1024**3}
        require_runtime_memory(fresh, 1024**3, now=110)
        for snapshot, linux_free in [({'at': 100, 'available_bytes': 1024**3}, 1024**3),
                                     (fresh, 128*1024**2),
                                     ({'at': 1, 'available_bytes': 2*1024**3}, 1024**3),
                                     ({'at': 100, 'available_bytes': float('nan')}, 1024**3)]:
            with self.assertRaises(ValueError): require_runtime_memory(snapshot, linux_free, now=110)

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
