"""Real, one-at-a-time WSL QEMU campaign; never calls PVE or synthesizes signals."""
import argparse
import base64
import hashlib
import json
import os
from pathlib import Path
import re
import socket
import subprocess
import time
import urllib.parse

import requests

from linux_install import Campaign, OS
from wsl_qemu import SN, MAC, launch_args, qmp, owned_qemu
from wsl_safety import MARKER, PURPOSE, GUEST_UNIT, admit_memory, guest_lock, owned_disk, require_repeat_network, require_runtime_memory, require_guest_unit

ROOT=Path('/home/mgr/lober-wsl-20261019')
HOST_MEMORY=Path('/mnt/e/src/lober/.tmp/linux-qualification/wsl-host-memory.json')


def digest(path):
    result=hashlib.sha256()
    with Path(path).open('rb') as stream:
        for block in iter(lambda:stream.read(1<<20),b''):result.update(block)
    return result.hexdigest()


class WslCampaign(Campaign):
    sn=SN
    guest='127.0.0.1'
    ssh_port=22221
    qualification_tag='wsl-linux-20261019'

    def __init__(self,root,system,negative=False):
        os.umask(0o077)
        if Path(root)!=ROOT or Path(root).is_symlink() or not (Path(root)/MARKER).is_file():
            raise ValueError('only the explicitly owned WSL campaign directory is permitted')
        self.root=Path(root).resolve(strict=True)
        if (self.root/MARKER).read_text().strip()!=PURPOSE:raise ValueError('WSL ownership marker mismatch')
        self.system,self.negative=system,negative
        self.series,self.format,self.version=OS[system]
        self.private=json.loads((self.root/'instance-private.json').read_text())
        self.config=json.loads((self.root/'instance.json').read_text())
        assert self.config['listen']=='127.0.0.1:18090'
        assert re.fullmatch(r'lober_wsl_20261019_[0-9a-f]{8}',self.private['schema'])
        query=dict(urllib.parse.parse_qsl(urllib.parse.urlsplit(self.config['pg']['dsn']).query))
        assert query.get('options')=='-csearch_path='+self.private['schema']
        self.profile=json.loads((self.root/'wsl-profile.json').read_text())
        assert self.profile['sn']==SN and self.profile['mac']==MAC and self.profile['purpose']==PURPOSE
        self.ram=self.profile['guest_memory_mib'][system]
        self.http=requests.Session();self.http.trust_env=False;self.token=None
        self.manifest=json.loads((self.root/'candidate.json').read_text())
        assert re.fullmatch(r'[a-z0-9-]+',self.manifest['run_id'])
        binary=Path(self.manifest['binary'])
        assert binary.parent==self.root and digest(binary)==self.manifest['binary_sha256']
        self.qa=json.loads(Path(__file__).with_name('qualification-source.json').read_text())
        assert re.fullmatch(r'[0-9a-f]{40}',self.qa['test_sha'])
        expected={'linux_install.py','wsl_install.py','wsl_safety.py','wsl_qemu.py'}
        assert set(self.qa['files'])==expected
        assert all(digest(Path(__file__).with_name(name))==value for name,value in self.qa['files'].items())
        assert self.qa['binary_sha256']==self.manifest['binary_sha256'] and self.qa['run_id']==self.manifest['run_id']
        assert self.qa['profile_sha256']==digest(self.root/'wsl-profile.json')
        self.run=self.root/('real-'+system+'-'+self.manifest['run_id']+('-required-failure' if negative else '-positive'))
        if self.run.is_symlink():raise ValueError('case directory indirection is forbidden')
        self.run.mkdir(exist_ok=True,mode=0o700);self.run.chmod(0o700)
        prepared=self.run/'candidate-at-prepare.json'
        if prepared.exists():assert json.loads(prepared.read_text())['binary_sha256']==self.manifest['binary_sha256']

    def pve(self,*args,**kwargs):
        raise RuntimeError('PVE operations are forbidden in the WSL campaign')

    def _live(self):
        path=self.run/'runtime-private.json'
        if not path.exists():return None
        info=json.loads(path.read_text());process=Path('/proc')/str(info['pid'])
        try:
            started=process.joinpath('stat').read_text().rsplit(') ',1)[1].split()[19]
            actual=process.joinpath('cmdline').read_bytes().rstrip(b'\0').split(b'\0')
        except FileNotFoundError:return None
        assert started==info['start_ticks'] and [v.decode() for v in actual]==info['argv'],'owned QEMU process identity changed'
        return info

    def check_guest(self,stopped=False):
        live=self._live();disk=self.run/'target.qcow2'
        if stopped:
            assert live is None,'the owned guest is still running'
            if not disk.exists():subprocess.run(['qemu-img','create','-q','-f','qcow2',str(disk),'32G'],check=True)
            info=json.loads(subprocess.check_output(['qemu-img','info','--output=json',str(disk)],text=True))
            owned_disk(self.root,disk,info)
            return info
        assert live is not None,'owned guest is not running'
        assert qmp(self.root,'query-kvm')=={'enabled':True,'present':True}
        blocks=[b['inserted'] for b in qmp(self.root,'query-block') if 'inserted' in b]
        assert len(blocks)==1 and blocks[0]['file']==str(disk)
        owned_disk(self.root,disk,blocks[0]['image'])
        for device,prop,wanted in [('owned-nic','bootindex',1),('owned-disk','bootindex',2),('owned-disk','serial','LOBER_LINUX_TARGET')]:
            assert qmp(self.root,'qom-get',{'path':'/machine/peripheral/'+device,'property':prop})==wanted
        return blocks[0]['image']

    def prepare(self):
        with guest_lock(self.root/'run/guest.lock'):super().prepare()

    def memory(self):
        snapshot=json.loads(HOST_MEMORY.read_text())
        fields={line.split(':')[0]:int(line.split()[1])*1024 for line in Path('/proc/meminfo').read_text().splitlines()}
        return snapshot,fields['MemAvailable']

    def start(self):
        shown=subprocess.check_output(['systemctl','show',GUEST_UNIT,'-p','MainPID','-p','MemoryMax','-p','MemorySwapMax',
                                       '-p','KillMode','-p','SendSIGKILL','-p','Restart','-p','TimeoutStopUSec'],text=True)
        properties=dict(line.split('=',1) for line in shown.splitlines() if '=' in line)
        require_guest_unit(properties,os.getpid(),self.ram,Path('/proc/self/cgroup').read_text())
        with guest_lock(self.root/'run/guest.lock'):
            self.check_guest(stopped=True)
            assert self.task()['state']=='pending'
            snapshot,available=self.memory();admit_memory(snapshot,self.ram,available)
            args=launch_args(self.root,self.run,self.ram,ssh_port=self.ssh_port)
            supervision={'status':'STARTING','ram_mib':self.ram,'admission':snapshot,'started':int(time.time())}
            def record(): (self.run/'supervision.json').write_text(json.dumps(supervision,indent=2))
            record()
            with (self.run/'qemu.private.log').open('w') as log,owned_qemu(args,log) as child:
                try:
                    started=Path('/proc',str(child.pid),'stat').read_text().rsplit(') ',1)[1].split()[19]
                    (self.run/'runtime-private.json').write_text(json.dumps({'pid':child.pid,'start_ticks':started,'argv':args},indent=2))
                    for _ in range(100):
                        assert child.poll() is None,'owned QEMU exited before its monitor became ready'
                        if (self.root/'run/guest.qmp').exists():break
                        time.sleep(.1)
                    self.check_guest();supervision['status']='RUNNING';record()
                    print('Owned WSL guest running; serial is private. One-guest lock held.',flush=True)
                    while child.poll() is None:
                        observed,free=self.memory();require_runtime_memory(observed,free)
                        supervision['windows_min_available_bytes']=min(supervision.get('windows_min_available_bytes',observed['available_bytes']),observed['available_bytes'])
                        record();time.sleep(2)
                    assert child.returncode==0,'owned QEMU exited unsuccessfully'
                    supervision.update(status='STOPPED',exit=child.returncode);record()
                except BaseException as error:
                    supervision.update(status='ABORT_RESOURCE' if isinstance(error,ValueError) else 'FAIL_RUNTIME',reason=str(error));record();raise

    def task(self):
        path=self.run/'supervision.json'
        if path.exists() and json.loads(path.read_text())['status'] in ('ABORT_RESOURCE','FAIL_RUNTIME'):
            raise RuntimeError('owned guest supervision failed; inspect private evidence before another action')
        return super().task()

    def stop(self):
        self.check_guest();qmp(self.root,'quit')

    def _qga(self,command,arguments):
        with socket.socket(socket.AF_UNIX,socket.SOCK_STREAM) as client:
            client.settimeout(15);client.connect(str(self.root/'run/guest.qga'))
            stream=client.makefile('rwb',buffering=0)
            try:
                nonce=time.time_ns()//1000
                stream.write(b'\xff'+(json.dumps({'execute':'guest-sync-delimited','arguments':{'id':nonce}})+'\n').encode())
                while True:
                    line=stream.readline(8<<20).lstrip(b'\xff')
                    if not line:raise RuntimeError('guest agent closed during synchronization')
                    answer=json.loads(line)
                    if answer.get('return')==nonce:break
                stream.write((json.dumps({'execute':command,'arguments':arguments})+'\n').encode())
                answer=json.loads(stream.readline(8<<20))
                if 'return' not in answer:raise RuntimeError('guest agent command failed')
                return answer['return']
            finally:stream.close()

    def agent(self,command):
        pid=self._qga('guest-exec',{'path':'/bin/sh','arg':['-c',command],'capture-output':True})['pid']
        for _ in range(90):
            status=self._qga('guest-exec-status',{'pid':pid})
            if status.get('exited'):
                assert status.get('exitcode')==0,'root guest inspection failed'
                return base64.b64decode(status.get('out-data','')).decode()
            time.sleep(1)
        raise RuntimeError('guest inspection agent timed out')

    def qualify(self):
        assert not self.negative,'required failure is never positive OS qualification'
        first=self.inspect();task_id=self.task()['id']
        for name in ('guest-root.log','ssh.log'):(self.run/('first-boot-'+name)).write_bytes((self.run/name).read_bytes())
        capture=self.run/'repeat-boot.private.pcap'
        before=capture.stat().st_size
        qmp(self.root,'qom-set',{'path':'/objects/repeat-capture','property':'status','value':'on'})
        try:
            self.agent('systemd-run --unit=lober-qualification-reboot --on-active=10s /usr/bin/systemctl reboot')
            for _ in range(120):
                try:
                    second=self.inspect()
                    if second['BOOT_ID']!=first['BOOT_ID']:
                        assert second['ROOT_UUID']==first['ROOT_UUID'],'root filesystem changed during repeat boot'
                        assert self.task()['id']==task_id and self.task()['state']=='done'
                        break
                except (OSError,RuntimeError,AssertionError,requests.RequestException,subprocess.TimeoutExpired):pass
                time.sleep(5)
            else:raise RuntimeError('repeat network boot did not pass installed guest checks')
        finally:qmp(self.root,'qom-set',{'path':'/objects/repeat-capture','property':'status','value':'off'})
        assert capture.stat().st_size<16*(1<<20),'repeat capture exceeded its evidence budget'
        require_repeat_network(capture.read_bytes(),before,SN)
        self.save('PASS',task=task_id,first_boot_id=first['BOOT_ID'],repeat_boot_id=second['BOOT_ID'],root_uuid=second['ROOT_UUID'],
                  checks=['real-media','PXE','disk-install','required-firstboot-hook','done-events','SSH-forward','guest-agent-root-inspection',
                          'credential-cleanup','repeat-gateway-localboot','same-root-filesystem'],
                  network_scope='One isolated SLIRP guest; does not qualify physical DHCP or bridged source attribution')

    def save(self,status,**fields):
        result={'status':status,'system':self.system,'negative':self.negative,'platform':'WSL2-KVM-SLIRP','at':int(time.time()),
                'product_sha':self.manifest['product_sha'],'test_sha':self.manifest['test_sha'],
                'binary_sha256':self.manifest['binary_sha256'],'harness_test_sha':self.qa['test_sha'],
                'harness_files_sha256':self.qa['files'],'profile_sha256':self.qa['profile_sha256'],
                'evidence_sha256':{p.name:digest(p) for p in self.run.iterdir() if p.suffix in ('.log','.pcap')},**fields}
        (self.run/'result.json').write_text(json.dumps(result,indent=2));print(json.dumps(result),flush=True)


if __name__=='__main__':
    parser=argparse.ArgumentParser(description=__doc__)
    parser.add_argument('action',choices=['prepare','start','wait','inspect','qualify','stop'])
    parser.add_argument('--state-dir',type=Path,default=ROOT);parser.add_argument('--os',choices=tuple(OS),required=True)
    parser.add_argument('--required-failure',action='store_true');args=parser.parse_args()
    campaign=WslCampaign(args.state_dir,args.os,args.required_failure)
    if args.action!='stop':campaign.authenticate()
    getattr(campaign,args.action)()
