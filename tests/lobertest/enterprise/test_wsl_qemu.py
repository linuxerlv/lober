"""Actual 64MiB KVM device probe, with vCPUs paused; never an OS install PASS."""
import json
from pathlib import Path
import socket
import signal
import sys
import subprocess
from tempfile import TemporaryDirectory
import time
import unittest

from wsl_qemu import launch_args, qmp, SN, MAC, owned_qemu
from wsl_install import WslCampaign


class WslQemuProbe(unittest.TestCase):
    def test_terminated_supervisor_cannot_leave_unmonitored_qemu(self):
        for death in (signal.SIGTERM,signal.SIGKILL):
            with self.subTest(death=death),TemporaryDirectory(prefix='lober-death-') as folder:
                root=Path(folder);case=root/'real-debian-probe';case.mkdir();(root/'run').mkdir()
                (root/'OWNED-BY-LOBER-WSL-QUALIFICATION').write_text('linux-install-qualification-20261019\n')
                subprocess.run(['qemu-img','create','-q','-f','qcow2',str(case/'target.qcow2'),'32G'],check=True)
                with socket.socket() as reserve:
                    reserve.bind(('127.0.0.1',0));port=reserve.getsockname()[1]
                args=launch_args(root,case,64,ssh_port=port,capability_probe=True)
                (root/'args.json').write_text(json.dumps(args))
                code='''import json,pathlib,sys,time
sys.path.insert(0,sys.argv[2])
from wsl_qemu import owned_qemu,qmp
from wsl_safety import guest_lock
root=pathlib.Path(sys.argv[1])
with guest_lock(root/'run/guest.lock'),(root/'qemu.log').open('w') as log:
    with owned_qemu(json.loads((root/'args.json').read_text()),log) as child:
        while not (root/'run/guest.qmp').exists():
            assert child.poll() is None
            time.sleep(.01)
        assert qmp(root,'query-kvm')['enabled']
        (root/'pid').write_text(str(child.pid))
        while True:time.sleep(1)
'''
                with (root/'supervisor.log').open('w') as log:
                    supervisor=subprocess.Popen([sys.executable,'-c',code,str(root),str(Path(__file__).parent)],stdout=log,stderr=subprocess.STDOUT)
                    pid=None
                    try:
                        for _ in range(200):
                            if supervisor.poll() is not None:self.fail((root/'supervisor.log').read_text())
                            if (root/'pid').exists():pid=int((root/'pid').read_text());break
                            time.sleep(.02)
                        self.assertIsNotNone(pid,'paused guest probe never became ready')
                        supervisor.send_signal(death);supervisor.wait(timeout=15)
                        for _ in range(200):
                            stat=Path('/proc',str(pid),'stat')
                            if not stat.exists() or stat.read_text().rsplit(') ',1)[1].split()[0]=='Z':break
                            time.sleep(.02)
                        else:self.fail('supervisor death left a running QEMU')
                        from wsl_safety import guest_lock
                        with guest_lock(root/'run/guest.lock'):pass
                    finally:
                        if supervisor.poll() is None:supervisor.terminate();supervisor.wait(timeout=15)

    def test_actual_devices_keep_network_first_and_capture_switchable(self):
        with TemporaryDirectory(prefix='lober-kvm-') as folder:
            root=Path(folder)
            (root/'OWNED-BY-LOBER-WSL-QUALIFICATION').write_text('linux-install-qualification-20261019\n')
            case=root/'real-debian-probe';case.mkdir()
            (root/'run').mkdir()
            subprocess.run(['qemu-img','create','-q','-f','qcow2',str(case/'target.qcow2'),'32G'],check=True)
            with socket.socket() as reserve:
                reserve.bind(('127.0.0.1',0));port=reserve.getsockname()[1]
            args=launch_args(root,case,64,ssh_port=port,capability_probe=True)
            with (case/'qemu.log').open('w') as log,owned_qemu(args,log) as child:
                try:
                    for _ in range(100):
                        if child.poll() is not None:self.fail((case/'qemu.log').read_text())
                        if (root/'run/guest.qmp').exists():break
                        time.sleep(.05)
                    self.assertEqual(qmp(root,'query-kvm'),{'enabled':True,'present':True})
                    started=Path('/proc',str(child.pid),'stat').read_text().rsplit(') ',1)[1].split()[19]
                    runtime={'pid':child.pid,'start_ticks':started,'argv':args}
                    (case/'runtime-private.json').write_text(json.dumps(runtime))
                    campaign=object.__new__(WslCampaign);campaign.root=root;campaign.run=case
                    campaign.check_guest()
                    (case/'runtime-private.json').write_text(json.dumps(dict(runtime,start_ticks='another-process')))
                    with self.assertRaises(AssertionError):campaign.check_guest()
                    (case/'runtime-private.json').write_text(json.dumps(runtime))
                    self.assertEqual(qmp(root,'qom-get',{'path':'/machine/peripheral/owned-nic','property':'bootindex'}),1)
                    self.assertEqual(qmp(root,'qom-get',{'path':'/machine/peripheral/owned-disk','property':'bootindex'}),2)
                    self.assertEqual(qmp(root,'qom-get',{'path':'/machine/peripheral/owned-disk','property':'serial'}),'LOBER_LINUX_TARGET')
                    blocks=qmp(root,'query-block')
                    self.assertEqual([b['inserted']['file'] for b in blocks if 'inserted' in b],[str(case/'target.qcow2')])
                    self.assertEqual(qmp(root,'qom-get',{'path':'/objects/repeat-capture','property':'status'}),'off')
                    qmp(root,'qom-set',{'path':'/objects/repeat-capture','property':'status','value':'on'})
                    self.assertEqual(qmp(root,'qom-get',{'path':'/objects/repeat-capture','property':'status'}),'on')
                    qmp(root,'qom-set',{'path':'/objects/repeat-capture','property':'status','value':'off'})
                    self.assertIn('serial='+SN,args[args.index('-smbios')+1])
                    self.assertIn('mac='+MAC,args[args.index('-netdev')+3])
                    campaign.stop();self.assertEqual(child.wait(timeout=10),0)
                finally:
                    if child.poll() is None:child.terminate();child.wait(timeout=10)


if __name__=='__main__':unittest.main()
