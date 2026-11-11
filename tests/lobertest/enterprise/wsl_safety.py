"""Fail-closed limits for the explicitly owned, one-at-a-time WSL test guest."""
from contextlib import contextmanager
import fcntl
import math
from pathlib import Path
import time

MIB = 1024**2
HOST_RESERVE = 1536 * MIB
QEMU_OVERHEAD = 512 * MIB
MARKER = 'OWNED-BY-LOBER-WSL-QUALIFICATION'
PURPOSE = 'linux-install-qualification-20261019'
GUEST_UNIT = 'lober-wsl-guest-20261019.service'


def require_guest_unit(properties, pid, ram_mib, cgroup):
    expected={'MainPID':str(pid),'MemoryMax':str((ram_mib+512)*MIB),'MemorySwapMax':'0',
              'KillMode':'control-group','SendSIGKILL':'yes','Restart':'no','TimeoutStopUSec':'10s'}
    if any(properties.get(key)!=value for key,value in expected.items()):
        raise ValueError('real guest requires the owned memory-capped systemd process group')
    if cgroup.strip()!='0::/system.slice/'+GUEST_UNIT:
        raise ValueError('real guest supervisor is outside its owned systemd process group')


def _observed_memory(snapshot, linux_available, now):
    now = time.time() if now is None else now
    at = snapshot.get('at', -1)
    if not isinstance(at, (int, float)) or not math.isfinite(at) or not 0 <= now-at <= 30:
        raise ValueError('Windows memory observation is stale or in the future')
    available = snapshot.get('available_bytes')
    if type(available) is not int or available < 0 or type(linux_available) is not int or linux_available < 0:
        raise ValueError('physical memory observations must be nonnegative integer byte counts')
    return available


def admit_memory(snapshot, guest_mib, linux_available, *, now=None):
    """Windows free RAM is required; WSL's virtual MemAvailable alone is unsafe."""
    if not isinstance(guest_mib, int) or not 1024 <= guest_mib <= 8192:
        raise ValueError('guest memory is outside the reviewed 1–8 GiB limits')
    available = _observed_memory(snapshot, linux_available, now)
    required = guest_mib * MIB + QEMU_OVERHEAD + HOST_RESERVE
    if available < required:
        raise ValueError(f'Windows headroom insufficient: need {required} available bytes before guest start')
    if linux_available < guest_mib * MIB + 1024 * MIB:
        raise ValueError('WSL headroom insufficient')


def require_runtime_memory(snapshot, linux_available, *, now=None):
    """An active, already budgeted process must still preserve host reserve."""
    available = _observed_memory(snapshot, linux_available, now)
    if available < HOST_RESERVE or linux_available < 384*MIB:
        raise ValueError('active work must stop: physical memory reserve exhausted')


@contextmanager
def guest_lock(path):
    """Hold for the complete lifetime of the guest, not just its launch."""
    with Path(path).open('a') as lock:
        fcntl.flock(lock, fcntl.LOCK_EX | fcntl.LOCK_NB)
        try:
            yield lock
        finally:
            fcntl.flock(lock, fcntl.LOCK_UN)


def owned_disk(root, disk, info):
    root, disk = Path(root), Path(disk)
    if root.is_symlink() or (root/MARKER).read_text().strip() != PURPOSE:
        raise ValueError('test workspace ownership marker mismatch')
    if not disk.is_file() or disk.is_symlink() or not disk.resolve().is_relative_to(root.resolve()):
        raise ValueError('disk is not an owned regular file')
    relative = disk.relative_to(root)
    if len(relative.parts) != 2 or not relative.parts[0].startswith('real-') or disk.name != 'target.qcow2':
        raise ValueError('disk path is outside the owned case directory')
    if disk.parent.is_symlink() or info.get('filename') != str(disk):
        raise ValueError('disk path indirection is forbidden')
    if info.get('format') != 'qcow2' or info.get('virtual-size') != 32*1024**3:
        raise ValueError('expected an owned 32 GiB qcow2 disk')
    if any(key in info for key in ('backing-filename', 'full-backing-filename', 'data-file')):
        raise ValueError('external backing or data files are forbidden')
    specific = info.get('format-specific', {}).get('data', {})
    if 'data-file' in specific:
        raise ValueError('qcow2 external data files are forbidden')


def require_repeat_network(packets, before_reboot, sn):
    if not 0 <= before_reboot <= len(packets):
        raise ValueError('invalid repeat capture boundary')
    fresh = packets[before_reboot:]
    if ('GET /v1/boot?sn='+sn+'&').encode() not in fresh:
        raise ValueError('repeat boot did not contact the gateway as the owned machine')
    if b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n' not in fresh:
        raise ValueError('repeat boot has no gateway local-disk reply')


if __name__ == '__main__':
    import argparse
    import json
    parser = argparse.ArgumentParser(description='Memory admission only; does not launch or qualify an OS.')
    parser.add_argument('--host-memory', type=Path, required=True)
    parser.add_argument('--guest-memory-mib', type=int, required=True)
    args = parser.parse_args()
    snapshot = json.loads(args.host_memory.read_text())
    memory = {line.split(':')[0]: int(line.split()[1])*1024
              for line in Path('/proc/meminfo').read_text().splitlines()}
    result = {'status': 'MEMORY_ADMITTED', 'at': int(time.time()), 'guest_mib': args.guest_memory_mib,
              'windows': snapshot, 'wsl_available_bytes': memory['MemAvailable'],
              'host_reserve_bytes': HOST_RESERVE, 'qemu_overhead_bytes': QEMU_OVERHEAD,
              'required_windows_available_bytes': args.guest_memory_mib*MIB+HOST_RESERVE+QEMU_OVERHEAD}
    try:
        admit_memory(snapshot, args.guest_memory_mib, memory['MemAvailable'])
    except ValueError as error:
        result.update(status='BLOCKED_MEMORY', reason=str(error))
    print(json.dumps(result, indent=2))
    raise SystemExit(0 if result['status'] == 'MEMORY_ADMITTED' else 1)
