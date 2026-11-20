"""Owned local QEMU device profile. This module does not qualify an OS."""
import json
from pathlib import Path
import socket
import subprocess
import signal
from contextlib import contextmanager

from wsl_safety import owned_disk

SN='LOBER-WSL-20261019'
MAC='02:4c:4f:42:45:01'
GATEWAY='172.30.194.1'
GUEST='172.30.194.50'


@contextmanager
def owned_qemu(args,stdout):
    """Reap on TERM/HUP/INT; parent death also kills the owned QEMU child.

    Real installs additionally require a capped systemd control group, covering
    abrupt death during the short setpriv/exec startup interval.
    """
    signals=(signal.SIGTERM,signal.SIGHUP,signal.SIGINT)
    previous={number:signal.getsignal(number) for number in signals}
    child=None
    def interrupted(number,frame):raise InterruptedError('owned supervisor interrupted by signal '+str(number))
    try:
        for number in signals:signal.signal(number,interrupted)
        child=subprocess.Popen(['/usr/bin/setpriv','--pdeathsig','KILL','--',*args],stdout=stdout,stderr=subprocess.STDOUT)
        yield child
    finally:
        for number in signals:signal.signal(number,signal.SIG_IGN)
        try:
            if child is not None and child.poll() is None:
                child.terminate()
                try:child.wait(timeout=10)
                except subprocess.TimeoutExpired:child.kill();child.wait(timeout=10)
        finally:
            for number,handler in previous.items():signal.signal(number,handler)


def launch_args(root,case,ram_mib,*,ssh_port=22221,capability_probe=False):
    root,case=Path(root).resolve(strict=True),Path(case).resolve(strict=True)
    if type(ram_mib) is not int or not (1024<=ram_mib<=8192 or capability_probe and ram_mib==64):
        raise ValueError('invalid QEMU memory budget')
    if type(ssh_port) is not int or not 1024<=ssh_port<=65535:
        raise ValueError('invalid owned SSH forward port')
    disk=case/'target.qcow2'
    info=json.loads(subprocess.check_output(['qemu-img','info','--output=json',str(disk)],text=True))
    owned_disk(root,disk,info)
    run=root/'run'
    if run.is_symlink() or not run.is_dir():raise ValueError('owned run directory missing')
    for path in (run/'guest.qmp',run/'guest.qga',case/'serial.private.raw',case/'repeat-boot.private.pcap'):
        if path.exists() or path.is_symlink():raise ValueError('refuse replacing previous guest state: '+str(path))
    return ['qemu-system-x86_64','-machine','pc,accel=kvm','-cpu','host','-m',str(ram_mib)+'M','-smp','2',
            '-nodefaults','-display','none','-vga','none','-smbios','type=1,serial='+SN,
            '-drive','if=none,id=disk0,format=qcow2,cache=none,aio=threads,file='+str(disk),
            '-device','virtio-scsi-pci,id=owned-scsi',
            '-device','scsi-hd,id=owned-disk,drive=disk0,bus=owned-scsi.0,serial=LOBER_LINUX_TARGET,bootindex=2',
            '-netdev','user,id=net0,net=172.30.194.0/24,host='+GATEWAY+',dhcpstart='+GUEST+
                       ',ipv6=off,bootfile=http://'+GATEWAY+':18091/boot.ipxe,hostfwd=tcp:127.0.0.1:'+str(ssh_port)+'-:22',
            '-device','virtio-net-pci,id=owned-nic,netdev=net0,mac='+MAC+',addr=0x12,bootindex=1',
            '-serial','file:'+str(case/'serial.private.raw'),
            '-qmp','unix:'+str(run/'guest.qmp')+',server=on,wait=off',
            '-device','virtio-serial-pci,id=owned-agent',
            '-chardev','socket,id=qga,path='+str(run/'guest.qga')+',server=on,wait=off',
            '-device','virtserialport,chardev=qga,name=org.qemu.guest_agent.0',
            '-object','filter-dump,id=repeat-capture,netdev=net0,status=off,maxlen=1024,file='+str(case/'repeat-boot.private.pcap'),
            *(['-S'] if capability_probe else [])]


def qmp(root,command,arguments=None):
    with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as connection:
        connection.settimeout(10);connection.connect(str(Path(root)/'run/guest.qmp'))
        stream=connection.makefile('rwb',buffering=0)
        try:
            greeting=json.loads(stream.readline(8<<20))
            if 'QMP' not in greeting:raise RuntimeError('invalid owned QMP greeting')
            for request in [{'execute':'qmp_capabilities','id':'cap'},
                            {'execute':command,'arguments':arguments or {},'id':'command'}]:
                stream.write((json.dumps(request)+'\n').encode())
                while True:
                    line=stream.readline(8<<20)
                    if not line:raise RuntimeError('owned QMP closed without a result')
                    answer=json.loads(line)
                    if answer.get('id')!=request['id']:continue
                    if 'error' in answer:raise RuntimeError('owned QMP command failed: '+json.dumps(answer['error']))
                    if 'return' not in answer:raise RuntimeError('owned QMP omitted its result')
                    result=answer['return'];break
            return result
        finally:stream.close()
