"""只检查验收工具的拒绝条件；本文件通过不代表装机通过。"""
import copy
import contextlib
import importlib.util
import inspect
import json
import os
import signal
import hashlib
import io
import struct
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest
import types
from unittest.mock import patch


ROOT = '/var/lib/libvirt/images/lober-pq-20261025'
OWNER = {'purpose': 'production-qualification-20261025',
         'run_id': 'pq25-checker', 'target_uuid': '015d0477-b113-41b8-93a7-e24c51cf686b'}
XML = '''<domain type="kvm"><name>lober-pq25-linux01</name>
<uuid>015d0477-b113-41b8-93a7-e24c51cf686b</uuid><memory unit="MiB">4096</memory><vcpu>2</vcpu>
<os><type arch="x86_64" machine="q35">hvm</type><smbios mode="sysinfo"/></os>
<sysinfo type="smbios"><system><entry name="serial">PQ25L01</entry></system></sysinfo>
<devices><disk type="file" device="disk"><driver name="qemu" type="qcow2"/>
<source file="/var/lib/libvirt/images/lober-pq-20261025/linux01.qcow2"/>
<target dev="vda" bus="virtio"/><serial>PQ25_LINUX01_DISK</serial><boot order="2"/></disk>
<interface type="network"><mac address="52:54:00:25:10:01"/><source network="lober-pq25"/>
<model type="e1000"/><boot order="1"/></interface></devices></domain>'''
TARGET_SPEC = {'host': 'ser866667880745', 'name': 'lober-pq25-linux01',
    'uuid': '015d0477-b113-41b8-93a7-e24c51cf686b', 'sn': 'PQ25L01',
    'mac': '52:54:00:25:10:01', 'cpus': 2, 'memory_mib': 4096,
    'disk_path': ROOT + '/linux01.qcow2', 'disk_serial': 'PQ25_LINUX01_DISK',
    'disk_bytes': 17179869184, 'data_path': ROOT + '/media01.qcow2',
    'data_serial': 'PQ25_MEDIA01_DISK', 'data_bytes': 17179869184,
    'service_uuid': '895a44e1-8abd-4463-bbf7-fba8a743ed59',
    'service_mac': '52:54:00:25:10:10', 'network': 'lober-pq25', 'bridge': 'virbr-pq25',
    'subnet': '192.168.201.0/24', 'host_ip': '192.168.201.1',
    'service_ip': '192.168.201.10', 'guest_ip': '192.168.201.101',
    'candidate_port': 18090, 'artifact_port': 18082}


def module(test):
    path = Path(__file__).with_name('cloud_install.py')
    test.assertTrue(path.is_file(), '云检查器尚未实现')
    spec = importlib.util.spec_from_file_location('cloud_install_under_test', path)
    value = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(value)
    return value


def disk():
    return {'info': {'format': 'qcow2', 'virtual-size': 17179869184},
            'map': [{'start': 0, 'length': 17179869184, 'data': False, 'zero': True}]}


def snapshot():
    return {'hostname': 'ser866667880745', 'free_bytes': 40802189312,
            'vm_free_bytes': 2147483648, 'base_exists': False, 'owner': None,
            'domains': [], 'networks': [], 'addresses': [], 'routes': [],
            'bridges': [], 'host_ports': [], 'vm_ports': [],
            'auth': {'ok': True, 'strict_host_key': True, 'from_vm': True}}


class CloudChecks(unittest.TestCase):
    def test_exited_single_thread_without_fds_needs_stable_zombie_identity(self):
        checker=module(self)
        for mode in ('empty','fd','thread','reused'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);proc=root/'proc/999';(proc/'fd').mkdir(parents=True);(proc/'task/999').mkdir(parents=True)
                if mode=='fd':(proc/'fd/4').touch()
                if mode=='thread':(proc/'task/1000').mkdir()
                expected={'pid':999,'uid':1000,'starttime':'1','vm_boot_id':'boot'}
                before={**expected,'state':'Z'}
                after={**before,**({'starttime':'2'} if mode=='reused' else {})}
                real_path=Path
                def mapped(path):return root/'proc'/str(path).removeprefix('/proc/').removeprefix('/proc') if str(path).startswith('/proc') else real_path(path)
                with patch.object(checker,'Path',side_effect=mapped),patch.object(checker,'VM_BASE',str(root)), \
                     patch.object(checker,'import_topology_process',side_effect=[before,after]), \
                     patch.object(checker.os,'readlink',side_effect=AssertionError('zombie cwd must not be guessed')):
                    if mode=='empty':
                        self.assertEqual(checker.import_fd_metadata(expected,[root/'machines/media/staging'])['holders'],[])
                    else:
                        with self.assertRaises(ValueError):checker.import_fd_metadata(expected,[root/'machines/media/staging'])

    def test_import_topology_reads_only_uid_parent_and_birth_metadata(self):
        checker=module(self)
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);(root/'sys/kernel/random').mkdir(parents=True);(root/'sys/kernel/random/boot_id').write_text('boot')
            for pid in (100,999):
                proc=root/str(pid);proc.mkdir();fields=['S','1','100','100']+['0']*16;fields[19]=str(pid)
                (proc/'stat').write_text(str(pid)+' (fixture) '+' '.join(fields))
            real_path=Path
            def mapped(value):return root/str(value).removeprefix('/proc/').removeprefix('/proc') if str(value).startswith('/proc') else real_path(value)
            with patch.object(checker,'Path',side_effect=mapped),patch.object(checker.os,'readlink',side_effect=AssertionError('unrelated exe/cwd read')):
                table=checker.import_processes()
            self.assertEqual(set(table),{100,999});self.assertNotIn('executable_sha256',table[999])

    def test_restricted_fd_metadata_no_write_write_failure_and_pid_reuse_keep_evidence(self):
        checker=module(self)
        for mode in ('no-write','write','unknown','reused','missing-fields','timeout','fd-no-write','fd-write','fd-unknown'):
            with self.subTest(mode=mode),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);records=root/'records';records.mkdir();proc=root/'proc/999';proc.mkdir(parents=True)
                fd_mode=mode.startswith('fd-');outcome=mode.removeprefix('fd-')
                if fd_mode:
                    (proc/'fd').mkdir();(proc/'fd/4').touch()
                fields=['S','1','999','999']+['0']*16;fields[19]='1';(proc/'stat').write_text('999 (fixture) '+' '.join(fields))
                (root/'proc/sys/kernel/random').mkdir(parents=True);(root/'proc/sys/kernel/random/boot_id').write_text('boot')
                expected={'pid':999,'uid':os.getuid(),'starttime':'1','vm_boot_id':'boot'}
                value={'schema':'lober-import-fd-metadata-v1','state':'PRESENT','identity':expected,
                    'holders':[{'kind':'write','fd':4}] if outcome=='write' else []}
                if outcome=='unknown':value['state']='UNKNOWN'
                if mode=='reused':value['identity']={**expected,'starttime':'2'}
                if mode=='missing-fields':del value['identity']
                real_path=Path
                def mapped(path):return root/'proc'/str(path).removeprefix('/proc/').removeprefix('/proc') if str(path).startswith('/proc') else real_path(path)
                real_readlink=os.readlink
                def readlink(path,*args,**kwargs):
                    if Path(path)==proc/'cwd':
                        if fd_mode:return '/unrelated'
                        raise PermissionError('restricted cwd')
                    if fd_mode and Path(path)==proc/'fd/4':raise PermissionError('restricted fd')
                    return real_readlink(path,*args,**kwargs)
                sources=[]
                def command(args,source,timeout):
                    sources.append(source);self.assertEqual(args,['sudo','-n','/usr/bin/python3','-I','-']);self.assertEqual(timeout,10)
                    compile(source,'readonly-single-pid','exec')
                    for forbidden in ('environ','cmdline','os.kill','pidfd_send_signal'):self.assertNotIn(forbidden,source)
                    if mode=='timeout':raise subprocess.TimeoutExpired(args,10)
                    return json.dumps(value)
                with patch.object(checker,'Path',side_effect=mapped),patch.object(checker,'VM_BASE',str(root)), \
                    patch.object(checker.os,'readlink',side_effect=readlink), \
                    patch.object(checker,'command',side_effect=command):
                    if outcome in ('no-write','write'):
                        rows=checker.import_write_holders([root/'machines/media/staging'],records)
                        self.assertEqual(bool(rows),outcome=='write')
                    else:
                        with self.assertRaises((ValueError,subprocess.TimeoutExpired)):checker.import_write_holders([root/'machines/media/staging'],records)
                self.assertEqual(len(sources),1)
                self.assertTrue(list(records.glob('proc-read-failed-*.json')))
                self.assertTrue(list(records.glob('proc-probe-*.begin.json')));self.assertTrue(list(records.glob('proc-probe-*.end.json')))

    def test_import_pidfd_identity_change_or_selected_disappearance_never_signals_replacement(self):
        checker=module(self);expected={'pid':999,'starttime':'1','uid':os.getuid(),'pgrp':999,'parent':1,
            'cmdline_sha256':'a'*64,'vm_boot_id':'boot','executable_path':'/fixture','executable_sha256':'b'*64}
        for actual in ({**expected,'starttime':'2'},FileNotFoundError('selected child disappeared')):
            with self.subTest(actual=str(actual)),patch.object(checker.os,'pidfd_open',return_value=55), \
                 patch.object(checker.os,'close'),patch.object(checker,'import_process',**({'side_effect':actual} if isinstance(actual,BaseException) else {'return_value':actual})), \
                 patch.object(checker.signal,'pidfd_send_signal') as send:
                with self.assertRaises((ValueError,FileNotFoundError)):checker.send_import_signal(expected,signal.SIGSTOP)
                send.assert_not_called()
    def test_import_stop_freezes_exact_root_before_unrelated_restricted_executable(self):
        checker=module(self)
        with tempfile.TemporaryDirectory(prefix='import-stop-fixture-') as directory:
            root=Path(directory);events=[]
            settings={'iso':{name:str(root/'machines/media'/name) for name in ('staging','artifacts','smb','tools')}}
            checker.write_once(root/'config.json',json.dumps(settings))
            parent={'owner':OWNER,'pid':100,'pgrp':100,'session':100,'uid':os.getuid(),'starttime':'1',
                'cmdline_sha256':'a'*64,'vm_boot_id':'boot','executable_path':'/candidate','executable_sha256':'b'*64,
                'parent':1,'state':'S','config_path':str(root/'config.json'),'config_sha256':checker.sha256(root/'config.json')}
            child={**parent,'pid':101,'starttime':'2','parent':100}
            checker.write_once(root/'instance.json',json.dumps(parent))
            manifest={'cloud':{'owner':OWNER,'instance_record_sha256':checker.sha256(root/'instance.json')}}
            killed=set()
            def identity(pid):
                events.append(('full',pid))
                if pid==999:raise PermissionError('unrelated restricted executable')
                if pid in killed:raise FileNotFoundError()
                return parent if pid==100 else child
            def table():
                events.append(('topology',0))
                if ('signal',100,signal.SIGSTOP) not in events:
                    identity(999)  # 旧代码在冻结准确根前先做全局exe读取。
                return {100:parent,101:child,999:{'pid':999,'parent':1,'uid':os.getuid(),'starttime':'old','vm_boot_id':'boot'}}
            def send(item,sig):
                events.append(('signal',item['pid'],sig))
                if sig==signal.SIGKILL:killed.add(item['pid'])
            with patch.object(checker,'VM_BASE',str(root)),patch.object(checker,'import_process',side_effect=identity), \
                 patch.object(checker,'import_processes',side_effect=table),patch.object(checker,'send_import_signal',side_effect=send), \
                 patch.object(checker,'import_write_holders',return_value=[]):
                proof=checker.stop_import_instance({'instance_record_file':str(root/'instance.json')},manifest,root)
            self.assertTrue(proof['imports_stopped']);self.assertEqual(killed,{100,101})
            self.assertLess(events.index(('signal',100,signal.SIGSTOP)),events.index(('topology',0)))
            self.assertNotIn(('full',999),events)
    def test_short_unapproved_resource_is_rejected_without_lease_or_protective_stop(self):
        checker=module(self)
        with tempfile.TemporaryDirectory(prefix='main-short-approval-fixture-') as directory:
            root=Path(directory)
            resource={'owner':OWNER,'target':TARGET_SPEC,'review':{'approved':False,'root_reviewer':'','independent_reviewer':''}}
            checker.short_file(root,'resource.json',resource)
            contract={'owner':OWNER,'target':TARGET_SPEC,'bindings':{'target_contract_sha256':checker.canonical_sha256(resource)}}
            checker.short_file(root,'contract.json',contract)
            config={'no_task_contract_file':str(root/'contract.json'),'no_task_target_contract_file':str(root/'resource.json')}
            manifest={'cloud':{'owner':OWNER,'target':TARGET_SPEC,'no_task_contract_sha256':checker.sha256(root/'contract.json'),
                'no_task_target_contract_file_sha256':checker.sha256(root/'resource.json')}}
            with patch.object(checker,'load_cloud_helper',return_value=types.SimpleNamespace(prepare_no_task_probe=lambda value:None)), \
                 patch.object(checker,'host_command') as host,patch.object(checker,'SafetyLease') as lease:
                with self.assertRaises(ValueError):checker.validate_short_approval(config,manifest)
                host.assert_not_called();lease.assert_not_called()
    def test_short_entry_is_independent_of_verified_flag_and_never_creates_a_task(self):
        for active in (False,True):
            with self.subTest(active=active):self.exercise_short_entry(active=active)

    def test_short_no_task_accepts_reviewed_linux01_generation(self):
        self.exercise_short_entry(generation=True)

    def test_short_no_task_rejects_unapproved_or_changed_generation_before_actions(self):
        for fault in ('unapproved','uuid','disk-path','serial','round'):
            with self.subTest(fault=fault):self.exercise_short_entry(generation=True,fault=fault)

    def exercise_short_entry(self, *, active=False, generation=False, fault=None):
        """编排替身，不是原始网络证据；唯一允许结果仍待根核验。"""
        checker=module(self)
        with tempfile.TemporaryDirectory(prefix='main-short-fixture-') as directory:
            root=Path(directory);events=[]
            owner={**OWNER,'target_uuid':'7fe71ebb-a34d-4155-8410-d1427ca2f83a'};target={**TARGET_SPEC,'uuid':owner['target_uuid']}
            if generation:
                target.update(disk_path=ROOT+'/targets/linux01-g01/system.qcow2',disk_serial='PQ25_LINUX01_G01')
            if fault=='uuid':owner['target_uuid']=target['uuid']='6df3e5fd-e1bf-4114-8471-613eebd26e52'
            checker.short_file(root,'runtime.json',{'fixture_only':True});checker.short_file(root,'instance.json',{'fixture_only':True})
            resource=checker.propose_target_contract(owner,target,[ROOT+'/linux01.qcow2']) if generation else {
                'schema':'lober-cloud-target-resource-v1','owner':owner,'target':target,'executes_mutation':False,
                'failed_disk_delete_or_reuse_allowed':False}
            resource['review']={'approved':fault!='unapproved','root_reviewer':'root-fixture','independent_reviewer':'other-fixture'}
            if fault=='disk-path':target['disk_path']=ROOT+'/targets/linux02-g01/system.qcow2'
            if fault=='serial':target['disk_serial']='PQ25_LINUX01_G02'
            checker.short_file(root,'resource.json',resource)
            checker.short_file(root,'origin.json',{'deployment_receipt_sha256':checker.sha256(root/'runtime.json')})
            import test_cloud_packet_capture as packets
            packet=packets.no_task_contract(b'')
            for key in ('pcap_sha256','pcap_size','capture_start_ns','capture_end_ns'):packet.pop(key)
            frozen={'schema':'lober-cloud-capture-session-v1','host':target['host'],'bridge':target['bridge'],'network':target['network'],
                'targets':{target['uuid']:{'packet_mode':'NO_TASK','target_sha256':checker.canonical_sha256(target),
                    'packet_contract':packet,'boot_chain':{'firmware_sha256':'a'*64,'dhcp_config_sha256':'b'*64,
                        'user_class_sha256':hashlib.sha256(b'').hexdigest(),'boot_file':'pq25.ipxe','required_tftp':['pq25.ipxe']}}},
                'tftp_port_range':[40000,40010],'max_bytes':64*1024**2,'max_seconds':60,'tcp_reset_policy':'no-rst'}
            bindings={'product_sha':'a'*40,'test_sha':'b'*40,'binary_sha256':'c'*64,
                'runtime_record_sha256':checker.sha256(root/'runtime.json'),'instance_record_sha256':checker.sha256(root/'instance.json'),
                'target_contract_sha256':checker.canonical_sha256(resource)}
            checks={key:checker.sha256(Path(checker.__file__).with_name(name+'.py')) for key,name in
                (('probe_sha256','cloud_no_task_probe'),('parser_sha256','cloud_packet_capture'),('capture_session_sha256','cloud_capture_session'))}
            contract={'schema':'lober-cloud-no-task-probe-v1','scope':'NO_TASK','task_policy':'TOTAL_TASKS_ZERO',
                'owner':{**owner,**({'run_id':'other-round'} if fault=='round' else {})},'target':target,'bindings':bindings,'checkers':checks,'capture_contract':frozen,
                'operations':{'start_source_sha256':hashlib.sha256(b'START-FIXTURE').hexdigest(),
                              'stop_source_sha256':hashlib.sha256(b'STOP-FIXTURE').hexdigest()},'max_seconds':60}
            checker.short_file(root,'contract.json',contract)
            manifest={**{key:bindings[key] for key in ('product_sha','test_sha','binary_sha256')},'cloud':{
                'owner':owner,'target':target,'instance_record_sha256':bindings['instance_record_sha256'],
                'database_origin_file':str(root/'origin.json'),'database_origin_sha256':checker.sha256(root/'origin.json'),
                'no_task_contract_sha256':checker.sha256(root/'contract.json'),
                'no_task_target_contract_file_sha256':checker.sha256(root/'resource.json')}}
            if generation:
                manifest['cloud'].update(target_contract=resource,target_contract_sha256=checker.canonical_sha256(resource))
            config={'no_task_contract_file':str(root/'contract.json'),'no_task_runtime_record_file':str(root/'runtime.json'),
                'no_task_target_contract_file':str(root/'resource.json'),'instance_record_file':str(root/'instance.json'),
                'short_observation_seconds':5,'short_failure_evidence_verified':False}
            class API:
                def authenticate(self):pass
                def api(self,method,path):
                    if method!='GET':raise AssertionError('short禁止任务POST')
                    events.append('GET');page={'items':[{'state':'pending'}] if active else [],'next':'','total':int(active)}
                    self.last_raw_response={'path':'/console/v1'+path,'status':200,
                        'payload':json.dumps({'code':0,'data':page,'message':''}).encode()}
                    return page
            class Captured:
                def __init__(self,*args):
                    events.append('capture-start');self.output=root/'capture';self.output.mkdir(mode=0o700)
                    checker.short_file(self.output,'capture-receipt-private.json',{'fixture_only':True})
                    anchor=checker.short_file(self.output,'anchor.json',{'fixture_only':True})
                    self.anchor_file=Path(anchor['path']);self.anchor_sha256=anchor['sha256']
                def check(self):pass
                def close(self):events.append('capture-close')
                def analyze(self):events.append('parse');return {'fixture_only':True}
            def action(config,manifest,contract,context,output,kind):
                events.append(kind);return checker.short_file(output,'action-'+kind+'.json',{'fixture_only':True,'context':context})
            def disk_snapshot(config,manifest,context,output,label):
                events.append(label);return checker.short_file(output,label+'.json',{'fixture_only':True,'context':context})
            def verify(receipt,contract,*,anchor):
                self.assertFalse(receipt['root_verified']);self.assertEqual(checker.canonical_sha256(receipt),anchor['receipt_sha256'])
                self.assertEqual(len(receipt['records']),11)
                for ref in receipt['records'].values():self.assertEqual(checker.sha256(ref['path']),ref['sha256'])
                return {'status':'ROOT_REVIEW_REQUIRED','short_failure_evidence_verified':False}
            probe=types.SimpleNamespace(prepare_no_task_probe=__import__('cloud_no_task_probe').prepare_no_task_probe,verify_no_task_probe=verify)
            with patch.object(checker,'load_cloud_helper',return_value=probe),patch.object(checker,'verify_instance'), \
                patch.object(checker,'Console',return_value=API()),patch.object(checker,'capture_contract',return_value=frozen), \
                patch.object(checker,'Capture',Captured),patch.object(checker,'short_host_action',side_effect=action), \
                patch.object(checker,'short_disk_snapshot',side_effect=disk_snapshot), \
                patch.object(checker,'target_action_source',return_value='START-FIXTURE'), \
                patch.object(checker,'owned_client_stop_source',return_value='STOP-FIXTURE'), \
                patch.object(checker.time,'monotonic',side_effect=[0,10]),patch.object(checker,'SafetyLease') as lease, \
                patch.object(checker,'host_command') as host:
                if fault:
                    with self.assertRaises(ValueError):checker.validate_short_approval(config,manifest)
                    with self.assertRaises(ValueError):checker.short_no_task(config,manifest,root)
                    checker.short_file(root,'identity.json',owner)
                    config['identity_file']=str(root/'identity.json')
                    checker.short_file(root,'config.json',config)
                    manifest['cloud'].update(config_sha256=checker.sha256(root/'config.json'),executor_sha256=checker.sha256(checker.__file__),
                        review={'approved':True,'executor_sha256':checker.sha256(checker.__file__),
                            'root_reviewer':'root-fixture','independent_reviewer':'other-fixture'})
                    checker.short_file(root,'manifest.json',manifest)
                    with patch.object(checker.socket,'gethostname',return_value='lober'),patch.object(checker,'check_manifest'), \
                        patch.object(checker,'check_run_identity'),patch.object(checker,'validate_phase_helpers'):
                        with self.assertRaises(ValueError):
                            checker.reviewed_phase('short',root/'config.json',root/'manifest.json',root/'unstarted-output')
                    self.assertEqual(events,[]);lease.assert_not_called();host.assert_not_called()
                elif active:
                    with self.assertRaises(ValueError):checker.short_no_task(config,manifest,root)
                    self.assertNotIn('capture-start',events)
                else:
                    checker.validate_short_approval(config,manifest)
                    result=checker.short_no_task(config,manifest,root)
                    receipt=checker.private_json(result['short_receipt']['path'])
                    self.assertFalse(receipt['root_verified'])
                    if generation:
                        self.assertEqual(receipt['owner']['target_uuid'],'7fe71ebb-a34d-4155-8410-d1427ca2f83a')
                        saved=checker.private_json(receipt['records']['target_contract']['path'])
                        self.assertEqual(saved['target']['disk_path'],ROOT+'/targets/linux01-g01/system.qcow2')
                        self.assertEqual(saved['target']['disk_serial'],'PQ25_LINUX01_G01')
                        self.assertEqual(saved['preserve_failed_disks'],[ROOT+'/linux01.qcow2'])
                    self.assertEqual(result['status'],'ROOT_REVIEW_REQUIRED');self.assertFalse(result['short_failure_evidence_verified'])
                    self.assertLess(events.index('stop'),events.index('capture-close'))
                    self.assertEqual(events.count('GET'),4)
    def test_install_error_defers_capture_close_until_outer_writer_stop(self):
        checker=module(self);closed=[]
        case={'capture':types.SimpleNamespace(close=lambda:closed.append('close'))}
        with patch.object(checker,'begin_install_case',return_value=case),patch.object(checker,'phase_resources'), \
             patch.object(checker,'poll_install_case',side_effect=ConnectionError('API lost')):
            with self.assertRaises(ConnectionError):checker.install({}, {},Path('/unused'))
        self.assertEqual(closed,[],'必须先由SafetyLease停客户/导入，不能先阻塞capture.close')

    def test_short_collectors_preserve_real_envelope_and_offline_blank_disk_raw_refs(self):
        checker=module(self)
        with tempfile.TemporaryDirectory(prefix='main-short-fixture-') as directory:
            root=Path(directory);calls=[];context={'owner':OWNER,'target_sha256':checker.canonical_sha256(TARGET_SPEC),'bindings':{}}
            class API:
                def api(self,method,path):
                    calls.append((method,path));data={'items':[],'next':'','total':0}
                    self.last_raw_response={'path':'/console/v1'+path,'status':200,
                        'payload':json.dumps({'code':0,'message':'','data':data}).encode()}
                    return data
            ref=checker.short_api_snapshot(API(),TARGET_SPEC,context,root,'api-before')
            frame=checker.private_json(ref['path'])
            self.assertEqual(len(frame['requests']),2);self.assertTrue(all(method=='GET' for method,_ in calls))
            self.assertEqual(frame['requests'][0]['path'],'/console/v1/tasks?page=1&page_size=100')
            self.assertEqual(json.loads(Path(frame['requests'][0]['body']['path']).read_text())['data']['total'],0)
            raw={'domain_state':'shut off','at_ns':123,'source_identity':{'device':1,'inode':2},'domain_xml':XML,
                'owner':json.dumps(OWNER),'info':json.dumps(disk()['info']),'map':json.dumps(disk()['map'])}
            def host(config,source,**kwargs):compile(source,'short-disk-source','exec');return json.dumps(raw)
            with patch.object(checker,'host_command',side_effect=host):
                ref=checker.short_disk_snapshot({}, {'cloud':{'owner':OWNER,'target':TARGET_SPEC}},context,root,'disk-before')
            record=checker.private_json(ref['path'])
            self.assertEqual(record['source_identity'],{'device':1,'inode':2})
            self.assertEqual(Path(record['domain_xml']['path']).read_text(),XML)
            self.assertEqual(json.loads(Path(record['map']['path']).read_text()),disk()['map'])

    def test_async_resource_monitor_interrupts_a_blocked_main_operation(self):
        checker=module(self);calls=[];old=signal.getsignal(signal.SIGUSR1)
        def probe():
            calls.append(True)
            if len(calls)>1:raise ValueError('mock host reserve below 6GiB')
        with self.assertRaises(RuntimeError):
            with checker.PhaseWatch(probe,interval=.01):checker.time.sleep(.2)
        self.assertEqual(signal.getsignal(signal.SIGUSR1),old)
        self.assertGreaterEqual(len(calls),2)
    def test_actual_task_creation_checks_snapshot_and_preserves_bad_created_response(self):
        checker=module(self)
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);key='ssh-ed25519 FIXTURE-ONLY';(root/'key.pub').write_text(key)
            builtin='{{lober:firstboot}}';created=[]
            manifest={'run_id':OWNER['run_id'],'product_sha':'a'*40,'test_sha':'b'*40,'binary_sha256':'c'*64,
                'cloud':{'owner':OWNER,'target':TARGET_SPEC,'prepare_chain_sha256':'d'*64,
                    'short_failure_evidence_sha256':'e'*64,'guest_public_key_sha256':hashlib.sha256(key.encode()).hexdigest(),
                    'builtin_template':{'id':'builtin-preseed','version':3,'body_sha256':hashlib.sha256(builtin.encode()).hexdigest()}}}
            prepared={'iso':{'id':'iso-own'}};short={'owner':OWNER,'no_task_created':True,'client_stopped':True,
                'failed_disks_preserved':True,'root_verified':True}
            class API:
                credentials={'author_username':'author','author_password':'fixture','approver_username':'other',
                    'approver_password':'fixture','guest_password_hash':'fixture-only'}
                def authenticate(self):pass
                def login(self,*args):return 'fixture-token'
                def approve(self,kind,draft,*args):return {**draft,'state':'approved','author':'a','approver':'b'}
                def api(self,method,path,body=None,**kwargs):
                    if method=='GET' and path.startswith('/isos/'):return prepared['iso']
                    if path=='/hooks':return {**body,'id':'hook','version':1,'rev':1}
                    if method=='GET' and path=='/templates/builtin-preseed/versions/3':return {'builtin':True,'state':'approved','body':builtin}
                    if path=='/templates/builtin-preseed/derive':return {'id':'template','version':1,'name':'fixture','family':'linux',
                        'series':['debian'],'body':builtin,'assembly':{},'params':[],'bindings':{},'rev':1}
                    if method=='PUT':return {**body,'id':'template','version':1}
                    if method=='GET' and path.startswith('/hosts'):return {'items':[]}
                    if path=='/hosts':return body
                    if path=='/wizard/params':return {'common':[],'hosts':[{'problems':[]}]}
                    if path=='/wizard/tasks':
                        task={'id':'actual-created-response','hostSN':'PQ25L01','state':'pending','isoID':'iso-own',
                            'templateID':'template','templateVersion':1,'snapshot':{'family':'OTHER-PLATFORM'}}
                        created.append(task);return {'tasks':[task]}
                    raise AssertionError((method,path))
            config={'short_failure_evidence_verified':True,'short_failure_evidence_file':'short','prepare_chain_file':'prepare',
                'guest_public_key':str(root/'key.pub'),'required_failure':False}
            with patch.object(checker,'check_manifest'),patch.object(checker,'Console',return_value=API()), \
                patch.object(checker,'bound_json',side_effect=lambda path,digest:short if path=='short' else prepared), \
                patch.object(checker,'check_prepare_originals'),patch.object(checker,'check_prepare_binding'), \
                patch.object(checker,'check_task_binding',wraps=checker.check_task_binding) as binding:
                with self.assertRaises(ValueError):checker.start_api(config,manifest,root)
                binding.assert_called_once()
            self.assertEqual(len(created),1)
            self.assertEqual(json.loads((root/'created-task-response-private.json').read_text()),created[0])
            self.assertFalse((root/'start-chain-private.json').exists())

    def test_snapshot_full_iso_template_hook_slot_and_target_must_match(self):
        checker=module(self);body='echo exact-hook\n'
        iso={'id':'iso-own','name':'DVD','version':'13.7.0','arch':'x86_64','artifacts':{'kernel':'nested/kernel','initrd':'nested/initrd','repo':'nested/repo'}}
        prepared={'iso':iso}
        template={'id':'tpl','name':'exact','version':2,'body':'exact-template','state':'approved','author':'a','approver':'b'}
        hook={'id':'hook','version':3,'state':'approved','author':'a','approver':'b',
            'impls':[{'family':'linux','series':['debian'],'body':body}]}
        task={'hostSN':'PQ25L01','isoID':'iso-own','templateID':'tpl','templateVersion':2,
            'snapshot':{'family':'linux','series':'debian','format':'preseed',
                'host':{'sn':'PQ25L01','arch':'x86_64','firmware':'bios','attrs':{'run_id':OWNER['run_id'],'target_uuid':OWNER['target_uuid']}},
                'iso':copy.deepcopy(iso),'template':{key:template[key] for key in ('id','name','version','body')},
                'slots':[{'name':'qualify','exec':True,'timing':'firstboot','instances':[{'hookID':'hook','hookVersion':3,
                    'body':body,'digest':hashlib.sha256(body.encode()).hexdigest(),'required':True,'interpreter':'sh'}]},
                         {'name':'partition','instances':[{'params':{'disk':{'items':['vda']}}}]}]}}
        checker.check_task_binding(task,prepared,template,hook,OWNER)
        for mutate in (lambda value:value['snapshot']['iso']['artifacts'].update(kernel='other/kernel'),
            lambda value:value['snapshot']['host']['attrs'].update(target_uuid='other'),
            lambda value:value['snapshot']['template'].update(body='other'),
            lambda value:value['snapshot']['slots'][0]['instances'][0].update(digest='9'*64),
            lambda value:value['snapshot']['slots'][1]['instances'][0]['params']['disk'].update(items=['vdb'])):
            bad=copy.deepcopy(task);mutate(bad)
            with self.assertRaises(ValueError):checker.check_task_binding(bad,prepared,template,hook,OWNER)

    def test_receipt_cannot_relax_protocols_or_limits_after_capture(self):
        checker=module(self);row={'target_sha256':checker.canonical_sha256(TARGET_SPEC),
            'packet_contract':{'task_id':'task','http':[{'required':'exact'}],'tftp':{'pq25.ipxe':{'sha256':'a'*64}}},
            'boot_chain':{'firmware_sha256':'b'*64}}
        frozen={'targets':{OWNER['target_uuid']:row},'max_bytes':1024,'max_seconds':60,'tcp_reset_policy':'no-rst'}
        receipt={'owner':OWNER,'target_uuid':OWNER['target_uuid'],'target_sha256':checker.canonical_sha256(TARGET_SPEC),
            'contract_sha256':checker.canonical_sha256(frozen),'packet_contract':copy.deepcopy(row['packet_contract']),
            'boot_chain':row['boot_chain'],'max_bytes':1024,'max_seconds':60,'tcp_reset_policy':'no-rst'}
        checker.check_capture_receipt_binding(receipt,frozen,TARGET_SPEC,OWNER)
        for key,value in [('packet_contract',{'task_id':'task','http':[],'tftp':{}}),('max_bytes',999999),('boot_chain',{})]:
            with self.subTest(key=key),self.assertRaises(ValueError):
                checker.check_capture_receipt_binding({**receipt,key:value},frozen,TARGET_SPEC,OWNER)

    def test_preseed_target_generation_list_is_exact_and_never_overwritten(self):
        checker=module(self);first={**TARGET_SPEC,'disk_path':ROOT+'/targets/linux01-g01/system.qcow2','disk_serial':'PQ25_LINUX01_G01'}
        second={**first,'disk_path':ROOT+'/targets/linux01-g02/system.qcow2','disk_serial':'PQ25_LINUX01_G02'}
        frozen=[first,second];saved=copy.deepcopy(frozen)
        self.assertTrue(checker.target_in_contract(frozen,second));self.assertEqual(frozen,saved)
        self.assertTrue(checker.target_in_contract([TARGET_SPEC,first,second],TARGET_SPEC))
        self.assertFalse(checker.target_in_contract([first,first],first))
        self.assertFalse(checker.target_in_contract(frozen,{**second,'uuid':'b6f109dc-b63c-47ba-8c2f-cab948297eee'}))
        self.assertFalse(checker.target_in_contract(frozen,{**second,'disk_path':ROOT+'/linux01.qcow2'}))

    def test_actual_runtime_collector_binds_proc_executable_config_environment_database_and_listener(self):
        checker=module(self)
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);proc=root/'proc/777';proc.mkdir(parents=True)
            location=root/'machines/runtime';location.mkdir(parents=True)
            binary=root/'candidate';binary.write_bytes(b'candidate-fixture')
            (proc/'exe').symlink_to(binary)
            (proc/'stat').write_text('777 (lober) S 1 777 777 '+'0 '*16)
            (proc/'environ').write_bytes(b'LOBER_PG_DSN=fixture-dsn\0')
            config=location/'config.json'
            settings={'listen':'192.168.201.10:18090','pg':{'dsn':'unused-file-dsn'},'machine':{'enabled':True},
                'preview':{'gateway':'http://192.168.201.10:18090','artifacts':'http://192.168.201.10:18082'},
                'iso':{name:str(root/'machines/media'/name) for name in ('staging','artifacts','smb','tools')}}
            checker.write_once(config,json.dumps(settings));checker.write_once(location/'.owner.json',json.dumps(OWNER))
            (proc/'cmdline').write_bytes(str(binary).encode()+b'\0-config\0'+str(config).encode()+b'\0')
            database={'database':'pq25_fixture','schema':'public','database_oid':'1','server_address':'127.0.0.1',
                'server_port':55432,'postmaster_started':'fixture-start'}
            launch={'root_actor':'root','root_record_ref':'FIXTURE_ONLY','owner':OWNER,'pid':777,'starttime':'1',
                'binary_sha256':checker.sha256(binary),'config_sha256':checker.sha256(config)}
            checker.write_once(root/'launch.json',json.dumps(launch))
            origin={'owner':OWNER,'binary_sha256':checker.sha256(binary),'config_sha256':checker.sha256(config),
                'dsn_sha256':hashlib.sha256(b'fixture-dsn').hexdigest(),'deployment_receipt_file':str(root/'launch.json'),
                'deployment_receipt_sha256':checker.sha256(root/'launch.json'),'database':database}
            checker.write_once(root/'database-origin.json',json.dumps(origin))
            manifest={'binary':str(binary),'binary_sha256':checker.sha256(binary),'cloud':{'owner':OWNER,
                'database_origin_file':str(root/'database-origin.json'),'database_origin_sha256':checker.sha256(root/'database-origin.json')}}
            identity={'pid':777,'pgrp':777,'uid':os.getuid(),'starttime':'1','vm_boot_id':'boot',
                'cmdline_sha256':checker.sha256(proc/'cmdline')}
            def command(args,**kwargs):
                if args[0]=='psql':
                    self.assertNotIn('fixture-dsn',args);self.assertEqual(kwargs['env']['PGDATABASE'],'fixture-dsn')
                    self.assertIn('default_transaction_read_only=on',kwargs['env']['PGOPTIONS']);return json.dumps(database)
                self.assertEqual(args,['ss','-H','-lntp'])
                return 'LISTEN 0 128 192.168.201.10:18090 0.0.0.0:* users:(("lober",pid=777,fd=3))\n'
            real_path=Path
            def mapped(value):return root/'proc' if str(value)=='/proc' else real_path(value)
            with patch.object(checker,'Path',side_effect=mapped),patch.object(checker,'VM_BASE',str(root)), \
                 patch.object(checker,'process_identity',return_value=identity),patch.object(checker,'command',side_effect=command):
                observed=checker.observe_instance({'pid':777,'config_path':str(config),'owner':OWNER},manifest)
                checker.write_once(root/'instance.json',json.dumps(observed));manifest['cloud']['instance_record_sha256']=checker.sha256(root/'instance.json')
                checker.verify_instance({'instance_record_file':str(root/'instance.json')},manifest)
                database['database']='lober'
                with self.assertRaises(ValueError):checker.verify_instance({'instance_record_file':str(root/'instance.json')},manifest)
                database['database']='pq25_fixture'
                other=root/'other';other.write_bytes(b'other-candidate');(proc/'exe').unlink();(proc/'exe').symlink_to(other)
                with self.assertRaises(ValueError):checker.verify_instance({'instance_record_file':str(root/'instance.json')},manifest)

    def test_inspect_entry_calls_identity_helper_and_rejects_cross_boot_system_evidence(self):
        checker=module(self);identity={'smbios_serial':'PQ25L01','machine_id':'m'*32,'boot_id':'boot',
            'root_uuid':'fs','root_disk_serial':'PQ25_LINUX01_DISK','address':'192.168.201.101',
            'ssh_host_public_key':'fixture-public-key'}
        for wrong in (False,True):
            with self.subTest(wrong=wrong),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);manifest={'cloud':{'owner':OWNER,'target':TARGET_SPEC}}
                system={**identity,'boot_id':'other' if wrong else 'boot'};calls=[]
                def verify(config,target,agent,output,runner):
                    calls.append(agent);self.assertEqual(config['owner'],OWNER);self.assertEqual(target,TARGET_SPEC)
                    self.assertEqual(agent['identity'],json.loads(Path(agent['raw_path']).read_text()))
                    self.assertEqual(runner,checker.command)
                    return {**identity,'ssh_exit':0,'access':{'fixture':True}}
                helper=types.SimpleNamespace(GUEST_IDENTITY_SCRIPT='print("identity")',verify_guest_identity=verify)
                with patch.object(checker,'load_cloud_helper',return_value=helper), \
                     patch.object(checker,'guest_agent_read',side_effect=[identity,system]):
                    if wrong:
                        with self.assertRaises(ValueError):checker.inspect_guest({},manifest,root,'case')
                    else:self.assertEqual(checker.inspect_guest({},manifest,root,'case')['ssh_exit'],0)
                self.assertEqual(calls[0]['target_uuid'],OWNER['target_uuid'])
                self.assertTrue((root/'case-agent-identity-private.json').is_file())

    def test_capture_adapter_registers_before_start_and_never_marks_unknown_close_stopped(self):
        checker=module(self);checker.ACTIVE_CAPTURES.clear()
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);events=[]
            class Session:
                def __init__(self,*args):self.args=args
                def start(self):
                    self_registered=checker.ACTIVE_CAPTURES[-1]
                    self.assertion=self_registered.session is self;events.append('start')
                def check(self):events.append('check')
                def close(self):events.append('close');raise ValueError('stopped unknown')
            helper=types.SimpleNamespace(CaptureSession=Session,verify_receipt=lambda value:None)
            manifest={'cloud':{'owner':OWNER,'target':TARGET_SPEC}}
            with patch.object(checker,'load_cloud_helper',return_value=helper),patch.object(checker,'capture_contract',return_value={'frozen':True}):
                capture=checker.Capture({},manifest,root,'install',{'id':'task'})
                self.assertTrue(capture.session.assertion);capture.check()
                with self.assertRaises(ValueError):capture.close()
                self.assertIsNone(capture.receipt)
                with self.assertRaises(ValueError):capture.close()
            self.assertEqual(events,['start','check','close','close'])

    def test_qualify_entry_uses_parsed_exchange_and_replays_original_body_only(self):
        checker=module(self)
        for invalid_packet in (False,True):
            with self.subTest(invalid_packet=invalid_packet),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);task=self.good_evidence()['task'];task['hostSN']='PQ25L01'
                events=self.good_evidence()['events'];events[0]['at']=100
                manifest={'cloud':{'owner':OWNER,'target':TARGET_SPEC,'prepare_chain_sha256':'a'*64,
                    'start_chain_sha256':'b'*64,'instance_record_sha256':'c'*64,'media':{'version':'13'}},
                    'binary_sha256':'d'*64}
                started={'owner':OWNER,'prepare_chain_sha256':'a'*64,'instance_record_sha256':'c'*64,
                    'executor_sha256':checker.sha256(checker.__file__),'task':task,'template':{},'hook':{}}
                prepared={'iso':{'id':'iso'}}
                install={'owner':OWNER,'target_uuid':OWNER['target_uuid'],'sha256':'e'*64,
                    'path':checker.VM_BASE+'/machines/fixture-install.pcap','packet_contract':{'task_id':'task-real'}}
                repeated={'sha256':'f'*64};requests=[]
                class Response:
                    status=409
                    def __enter__(self):return self
                    def __exit__(self,*args):return False
                def open_request(request,**kwargs):requests.append(request);return Response()
                def request(method,path,*args,**kwargs):
                    if path.endswith('/retry'):return {'problems':[{'code':'task_retry_after_reboot'}]}
                    if path.startswith('/isos/'):return prepared['iso']
                    return task
                api=types.SimpleNamespace(authenticate=lambda:None,api=request,events=lambda task_id:events,
                    opener=types.SimpleNamespace(open=open_request))
                boot='/v1/boot?sn=PQ25L01&arch=x86_64&firmware=bios'
                row={'method':'GET','target_sha256':hashlib.sha256(boot.encode()).hexdigest(),'response_status':200,
                     'response_body_sha256':hashlib.sha256(b'#!ipxe\nexit || sanboot --no-describe --drive 0x80\n').hexdigest()}
                class Captured:
                    receipt=repeated
                    anchor_file=root/'repeat-anchor.json'
                    anchor_sha256='8'*64
                    def check(self):pass
                    def close(self):return repeated
                    def analyze(self):
                        if invalid_packet:raise ValueError('wrong TCP flow')
                        return {'http':[row]}
                helper=types.SimpleNamespace(analyze_session=lambda *args,**kwargs:{'http':[{'callback':True,'response_status':200,'reference':{'actual':True}}]},
                    extract_verified_callback=lambda *args,**kwargs:{'target':'/v1/signal/task-real/token/ready','body':b'original-body',
                        'content_type':'application/octet-stream'})
                def bound(path,digest):return {'start':started,'prepare':prepared,'install':install,'anchor':{}}[path]
                first={**self.good_evidence()['guest'],'smbios_serial':'PQ25L01'}
                second={**first,'boot_id':'second'}
                config={'start_chain_file':'start','prepare_chain_file':'prepare','required_failure':False,
                    'api_url':'http://192.168.201.10:18090/console/v1','install_capture_receipt_file':'install',
                    'capture_contract_files':{'install':'fixture-install-contract','repeat':'fixture-repeat-contract'}}
                manifest['cloud']['install_capture_receipt_sha256']='9'*64
                config['install_capture_anchor_file']='anchor';manifest['cloud']['install_capture_anchor_sha256']='7'*64
                with patch.object(checker,'bound_json',side_effect=bound),patch.object(checker,'Console',return_value=api), \
                    patch.object(checker,'check_prepare_binding'),patch.object(checker,'check_prepare_originals'), \
                    patch.object(checker,'check_start_binding'),patch.object(checker,'check_task_binding'),patch.object(checker,'inspect_guest',side_effect=[first,second]), \
                    patch.object(checker,'Capture',return_value=Captured()),patch.object(checker,'run_approved_host_script',return_value='rebooted'), \
                    patch.object(checker,'load_cloud_helper',return_value=helper),patch.object(checker,'verify_instance'), \
                    patch.object(checker,'capture_contract'),patch.object(checker,'check_capture_receipt_binding'), \
                    patch.object(checker.os,'link'),patch.object(checker,'check_run_identity',return_value={'installation_helpers':{}}), \
                    patch.object(checker,'check_qualification',return_value={'status':'PASS'}) as checked:
                    if invalid_packet:
                        with self.assertRaises(ValueError):checker.qualify(config,manifest,root)
                        self.assertFalse(requests);checked.assert_not_called()
                    else:
                        self.assertEqual(checker.qualify(config,manifest,root)['status'],'PASS')
                        self.assertEqual(requests[0].data,b'original-body');checked.assert_called_once()
                        self.assertTrue((root/'packet-evidence-private.json').is_file())

    def test_generated_target_stop_and_qga_guard_compile_and_new_plan_preserves_failed_disk(self):
        checker=module(self);manifest={'cloud':{'owner':OWNER,'target':TARGET_SPEC}}
        source=checker.owned_client_stop_source(manifest)
        compile(source,'owned-client-stop','exec')
        self.assertIn("virsh('destroy',target['uuid'])",source)
        self.assertIn('old customer QEMU not confirmed gone',source)
        guard=checker.owned_client_guard_source(manifest);compile(guard,'guard','exec')
        self.assertNotIn("virsh('destroy'",guard)
        manifest['cloud'].update(network_state_contract={'active_xml_sha256':'a'*64,'inactive_xml_sha256':'b'*64,'host_policy':{}},
            client_definition_sha256='c'*64)
        for phase in ('start','reboot'):compile(checker.target_action_source(manifest,phase),phase,'exec')
        second={**TARGET_SPEC,'name':'lober-pq25-linux02','uuid':'b6f109dc-b63c-47ba-8c2f-cab948297eee',
            'sn':'PQ25L02','mac':'52:54:00:25:10:02','guest_ip':'192.168.201.102',
            'disk_path':ROOT+'/targets/linux02-g01/system.qcow2','disk_serial':'PQ25_LINUX02_G01'}
        owner={**OWNER,'target_uuid':second['uuid']}
        plan=checker.propose_target_contract(owner,second,[ROOT+'/linux01.qcow2'])
        self.assertFalse(plan['review']['approved']);self.assertFalse(plan['executes_mutation'])
        checker.check_target(plan['domain_xml'],owner,owner,disk(),blank=True,target=second)
        policy=checker.isolation_policy('vm','pqtest0',[TARGET_SPEC,second])
        self.assertIn('192.168.201.102',json.dumps(policy));self.assertNotEqual(policy,checker.isolation_policy('vm','pqtest0'))
        with self.assertRaises(ValueError):checker.propose_target_contract(owner,second,[])

    def test_binary_capture_transport_logs_only_hash_and_preserves_partial_on_failure(self):
        checker=module(self)
        for failure in (False,True):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);root.chmod(0o700);payload=b'PRIVATE-PCAP-TOKEN'
                fake=types.SimpleNamespace(pid=12345,returncode=0,stdin=types.SimpleNamespace(fileno=lambda:45,close=lambda:None),stdout=types.SimpleNamespace(fileno=lambda:44),
                    poll=lambda:0,wait=lambda **kwargs:0)
                selector=types.SimpleNamespace(__enter__=lambda self:self,__exit__=lambda *args:False)
                class Selector:
                    written=False
                    def __enter__(self):return self
                    def __exit__(self,*args):return False
                    def register(self,*args):pass
                    def unregister(self,*args):pass
                    def select(self,*args):
                        if not self.written:self.written=True;return [(types.SimpleNamespace(data='input'),True)]
                        return [(types.SimpleNamespace(data='output'),True)]
                reads=[payload,OSError('stream broken')] if failure else [payload,b'']
                with patch.object(checker,'COMMAND_RECORDS',root),patch.object(checker.subprocess,'Popen',return_value=fake), \
                     patch.object(checker.selectors,'DefaultSelector',return_value=Selector()), \
                     patch.object(checker.os,'set_blocking'),patch.object(checker.os,'write',side_effect=lambda fd,data:len(data)), \
                     patch.object(checker.os,'read',side_effect=reads),patch.object(checker.shutil,'disk_usage',return_value=types.SimpleNamespace(free=2*checker.GIB)):
                    if failure:
                        with self.assertRaises(OSError):checker.stream_command(['ssh-fixture'],'fixture',root/'capture.pcap',10)
                    else:
                        value=checker.stream_command(['ssh-fixture'],'fixture',root/'capture.pcap',10)
                        self.assertEqual(value['sha256'],hashlib.sha256(payload).hexdigest())
                self.assertEqual((root/'capture.pcap').read_bytes(),payload)
                self.assertNotIn(payload,b''.join(path.read_bytes() for path in root.glob('stream-*.json')))

    def test_parallel_entry_starts_both_before_poll_and_closes_both_on_error(self):
        checker=module(self)
        for failure in (False,True):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as directory:
                events=[];cases=[({'required_failure':False},{'id':number}) for number in (1,2)]
                def begin(config,manifest,folder):
                    number=manifest['id'];events.append('start'+str(number))
                    return {'number':number,'capture':types.SimpleNamespace(close=lambda:events.append('close'+str(number)))}
                def poll(case):
                    number=case['number'];events.append('poll'+str(number))
                    if failure and number==1:raise ConnectionError('API failed')
                    return {'id':str(number)}
                with patch.object(checker,'manifest_target',side_effect=lambda m:{'sn':'SN'+str(m['id'])}), \
                     patch.object(checker,'begin_install_case',side_effect=begin),patch.object(checker,'poll_install_case',side_effect=poll):
                    if failure:
                        with self.assertRaises(ConnectionError):checker.parallel_install(cases,Path(directory))
                    else:self.assertEqual(len(checker.parallel_install(cases,Path(directory))),2)
                self.assertEqual(events[:2],['start1','start2'])
                if failure:self.assertEqual(events,['start1','start2','poll1'])
                else:self.assertEqual(events[-2:],['close1','close2'])

    def test_real_start_entry_requeries_iso_and_rejects_changed_prepare_original_before_task_creation(self):
        checker=module(self)
        for bad in ('iso-id','iso-sha','receipt'):
            with self.subTest(bad=bad),tempfile.TemporaryDirectory() as directory:
                root=Path(directory);calls=[]
                iso={'id':'iso-own','family':'linux','series':'debian','arch':'x86_64','version':'13.7.0',
                    'state':'ready','sha256':'d'*64,'artifacts':{'kernel':'k','initrd':'i','repo':'r'}}
                manifest={'run_id':OWNER['run_id'],'product_sha':'a'*40,'test_sha':'b'*40,'binary_sha256':'c'*64,'run_identity_sha256':'2'*64,
                    'cloud':{'owner':OWNER,'target':TARGET_SPEC,'executor_sha256':'e'*64,'instance_record_sha256':'f'*64,
                        'database_origin_sha256':'1'*64,'media':{'version':'13.7.0','dvd':{'sha256':'d'*64}}}}
                prepared={key:manifest[key] for key in ('product_sha','test_sha','binary_sha256','run_id')}
                for name,value in [('import-private.json',{'id':'iso-own'}),('import-ready-private.json',iso),('media-verified.json',{})]:
                    checker.write_once(root/name,json.dumps(value))
                prepared.update(owner=OWNER,executor_sha256='e'*64,instance_record_sha256='f'*64,iso=iso,
                    database_origin_sha256='1'*64,media_manifest_sha256=checker.canonical_sha256(manifest['cloud']['media']),
                    import_receipt_sha256=checker.sha256(root/'import-private.json'),
                    evidence_sha256={name:checker.sha256(root/name) for name in ('import-private.json','import-ready-private.json','media-verified.json')})
                checker.write_once(root/'prepare.json',json.dumps(prepared));manifest['cloud']['prepare_chain_sha256']=checker.sha256(root/'prepare.json')
                checker.write_once(root/'short.json',json.dumps({'owner':OWNER,'no_task_created':True,'client_stopped':True,
                    'failed_disks_preserved':True,'root_verified':True,'fixture_only':True}))
                manifest['cloud']['short_failure_evidence_sha256']=checker.sha256(root/'short.json')
                if bad=='receipt':(root/'import-private.json').write_text('{"id":"other"}')
                current={**iso,**({'id':'other'} if bad=='iso-id' else {'sha256':'9'*64} if bad=='iso-sha' else {})}
                def request(method,path,*args,**kwargs):calls.append((method,path));return current
                api=types.SimpleNamespace(authenticate=lambda:None,api=request)
                with patch.object(checker,'check_manifest'),patch.object(checker,'Console',return_value=api):
                    with self.assertRaises(ValueError):checker.start_api({'prepare_chain_file':str(root/'prepare.json'),
                        'short_failure_evidence_file':str(root/'short.json'),'short_failure_evidence_verified':True},manifest,root)
                self.assertFalse(any(method=='POST' for method,_ in calls))

    def test_real_reviewed_entry_stops_writers_for_errors_signals_and_final_record_failure(self):
        import fcntl
        for failure in ('space','api','ssh-timeout','term','final-record'):
            with self.subTest(failure=failure),tempfile.TemporaryDirectory() as directory:
                checker=module(self);base=Path(directory);(base/'machines').mkdir();(base/'leases').mkdir()
                lock=base/'leases/heavy.lock';lock.touch();events=[]
                config={'identity_file':str(base/'owner.json')}
                manifest={'run_id':OWNER['run_id'],'product_sha':'a'*40,'test_sha':'b'*40,'binary_sha256':'c'*64,'run_identity_sha256':'2'*64,
                    'cloud':{'owner':OWNER,'target':TARGET_SPEC,'executor_sha256':checker.sha256(checker.__file__),
                     'review':{'approved':True,'executor_sha256':checker.sha256(checker.__file__),
                               'root_reviewer':'root','independent_reviewer':'other'}}}
                for name,value in [('owner',OWNER),('config',config),('manifest',manifest)]:
                    if name=='manifest':manifest['cloud']['config_sha256']=checker.sha256(base/'config.json')
                    checker.write_once(base/(name+'.json'),json.dumps(value))
                def install(*args):
                    events.append('dispatch')
                    checker.ACTIVE_CAPTURES.append(types.SimpleNamespace(close=lambda:events.append('close-capture')))
                    error={'space':ValueError('space'), 'api':ConnectionError('api'),
                        'ssh-timeout':subprocess.TimeoutExpired('ssh',1),'term':InterruptedError('SIGTERM')}.get(failure)
                    if error:raise error
                    return {'id':'task','state':'done'}
                def stop(*args,**kwargs):
                    with lock.open() as other,self.assertRaises(BlockingIOError):fcntl.flock(other,fcntl.LOCK_EX|fcntl.LOCK_NB)
                    events.append('stop-client');return json.dumps({'client_stopped':True})
                def imports(*args):events.append('stop-imports');return {'imports_stopped':True}
                real_write=checker.write_once
                def write(path,body):
                    if path.name=='result.json' and failure=='final-record':raise OSError('result unavailable')
                    return real_write(path,body)
                with patch.object(checker,'VM_BASE',str(base)),patch.object(checker.socket,'gethostname',return_value='lober'), \
                    patch.object(checker,'check_manifest'),patch.object(checker,'check_run_identity'),patch.object(checker,'phase_resources'), \
                    patch.object(checker,'validate_phase_helpers'), \
                    patch.object(checker,'PhaseWatch',return_value=contextlib.nullcontext()),patch.object(checker,'install',side_effect=install), \
                    patch.object(checker,'host_command',side_effect=stop),patch.object(checker,'stop_import_instance',side_effect=imports), \
                    patch.object(checker,'write_once',side_effect=write):
                    with self.assertRaises((ValueError,ConnectionError,subprocess.TimeoutExpired,InterruptedError,OSError)):
                        checker.reviewed_phase('start',base/'config.json',base/'manifest.json',base/'machines/output')
                self.assertEqual(events,['dispatch','stop-client','stop-imports','close-capture'])
                with lock.open() as other:fcntl.flock(other,fcntl.LOCK_EX|fcntl.LOCK_NB)
                receipts=list((base/'machines/output').glob('cleanup-*.json'))
                self.assertTrue(receipts);self.assertTrue(json.loads(receipts[-1].read_text())['lock_released'])
    def test_stop_import_tree_only_kills_frozen_instance_and_recorded_children(self):
        checker=module(self)
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);events=[]
            configuration={'iso':{name:str(root/'machines/media'/name) for name in ('staging','artifacts','smb','tools')}}
            checker.write_once(root/'config.json',json.dumps(configuration))
            frozen={'owner':OWNER,'pid':100,'pgrp':100,'session':100,'uid':os.getuid(),'starttime':'1',
                'cmdline_sha256':'a'*64,'vm_boot_id':'boot','executable_path':'/candidate','executable_sha256':'b'*64,
                'config_path':str(root/'config.json'),'config_sha256':checker.sha256(root/'config.json')}
            child={**frozen,'pid':101,'parent':100,'starttime':'2','state':'S'}
            parent={**frozen,'parent':1,'state':'S'}
            checker.write_once(root/'instance.json',json.dumps(frozen))
            manifest={'cloud':{'owner':OWNER,'instance_record_sha256':checker.sha256(root/'instance.json')}}
            killed=set()
            def identity(pid):
                if pid in killed:raise FileNotFoundError()
                return parent if pid==100 else child
            def send(pid,sig):
                events.append((pid,sig))
                if sig==signal.SIGKILL:killed.add(pid)
            with patch.object(checker,'import_processes',return_value={100:parent,101:child}), \
                 patch.object(checker,'import_process',side_effect=identity),patch.object(checker.os,'killpg',side_effect=send), \
                 patch.object(checker.os,'kill',side_effect=send),patch.object(checker,'VM_BASE',str(root)), \
                 patch.object(checker,'import_write_holders',return_value=[]), \
                 patch.object(checker,'send_import_signal',side_effect=lambda item,sig:send(item['pid'],sig)):
                value=checker.stop_import_instance({'instance_record_file':str(root/'instance.json')},manifest,root)
            self.assertTrue(value['imports_stopped']);self.assertEqual(killed,{100,101})
            self.assertEqual(events[0],(100,signal.SIGSTOP));self.assertTrue((root/'import-stop/tree.json').is_file())

    def test_explicit_second_target_and_disk_generation_do_not_change_legacy_constants(self):
        checker=module(self);second=copy.deepcopy(TARGET_SPEC)
        second.update(name='lober-pq25-linux02',uuid='b6f109dc-b63c-47ba-8c2f-cab948297eee',sn='PQ25L02',
            mac='52:54:00:25:10:02',guest_ip='192.168.201.102',disk_path=ROOT+'/targets/linux02-g01/system.qcow2',
            disk_serial='PQ25_LINUX02_G01')
        owner={**OWNER,'target_uuid':second['uuid']}
        self.assertEqual(checker.resolve_target(owner,second),second)
        evidence=self.good_evidence();evidence['task']['hostSN']=second['sn']
        for guest in (evidence['guest'],evidence['repeat']['guest']):
            guest.update(smbios_serial=second['sn'],root_disk_serial=second['disk_serial'],address=second['guest_ip'])
        checker.check_qualification(evidence,False,target=second)
        with self.assertRaises(ValueError):checker.check_qualification(evidence,False)
        self.assertEqual(checker.SN,'PQ25L01');self.assertEqual(checker.SERIAL,'PQ25_LINUX01_DISK')
        for key,value in [('disk_path','/old/data.qcow2'),('mac',checker.SERVICE_MAC),('guest_ip','192.168.100.2'),
                          ('disk_serial','PQ25_LINUX01_DISK')]:
            with self.subTest(key=key),self.assertRaises(ValueError):checker.resolve_target(owner,{**second,key:value})

    def exercise_download_success_entry(self, fail_final=False, fail_guard=None, raise_record_error=False):
        """小文件代替耗时镜像；真实 entry/runner/整文件核对/硬链接/收尾都执行。"""
        import fcntl
        checker=module(self);events=[];lock_fds=[];stdout=io.StringIO()
        runner_source=(Path(checker.VM_BASE)/'machines/cloud-download-v1/download-run.py').read_text()
        fixtures={'mini.iso':b'mini-fixture','debian-13.7.0-amd64-DVD-1.iso':b'dvd-fixture'}
        plan=checker.download_plan()
        for image in plan['images']:
            image.update(size=len(fixtures[image['name']]),sha256=hashlib.sha256(fixtures[image['name']]).hexdigest())
        real_write=checker.write_once;real_flock=fcntl.flock
        def write(path,body):
            if Path(path).name=='result.json' and fail_final:raise OSError('final record unavailable')
            return real_write(path,body)
        def flock(handle,operation):
            lock_fds.append(handle.fileno());events.append('lock_acquired');return real_flock(handle,operation)
        class Child:
            identity={'pid':54321,'starttime':'123','uid':1000}
            def __init__(self):self.returncode=None;self.reaped=False
            def start(self,spec,partial,status,*args):
                Path(partial).write_bytes(fixtures[spec['name']])
                real_write(status,json.dumps({'status':'VERIFIED','bytes':spec['size'],'sha256':spec['sha256']}))
                events.append('start')
            def poll(self):
                if fail_guard and not self.reaped:return None
                if not self.reaped:events.append('reap');self.reaped=True;self.returncode=0
                return self.returncode
            def stop_and_wait(self):
                if not self.reaped:events.append('reap');self.reaped=True;self.returncode=-15
                return {'pid':54321,'reaped':True,'exit':self.returncode}
            def close(self):
                if not self.reaped:raise AssertionError('closed before reap')
                events.append('close')
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);(base/'leases').mkdir();(base/'leases/heavy.lock').touch()
            media=base/'machines/media';media.mkdir(parents=True)
            identity=base/'identity.json';identity.write_text(json.dumps(OWNER));identity.chmod(0o600)
            config=base/'config.json';config.write_text(json.dumps({'identity_file':str(identity)}));config.chmod(0o600)
            runner=base/'download-run.py';runner.write_text(runner_source)
            executor=base/'download-executor.py';executor.write_bytes(Path(checker.__file__).read_bytes())
            manifest=base/'manifest.json'
            frozen={'schema':'lober-cloud-download-v1','scope':'download-only','owner':OWNER,
                'review':{'approved':True,'root_reviewer':'root','independent_reviewer':'independent'},
                'executor_sha256':hashlib.sha256(executor.read_bytes()).hexdigest(),
                'runner_sha256':hashlib.sha256(runner.read_bytes()).hexdigest(),
                'config_sha256':hashlib.sha256(config.read_bytes()).hexdigest(),'vm_uid':os.getuid(),'plan':plan,
                'limits':{'max_seconds_per_image':3600,'idle_seconds':45,'host_poll_seconds':5,'host_timeout_seconds':12}}
            manifest.write_text(json.dumps(frozen));manifest.chmod(0o600)
            observed={**self.download_observation(),'mount':str(media)};output=media/'official-download-fixture'
            loader=types.SimpleNamespace(name='download_only',loader=types.SimpleNamespace(exec_module=lambda module:None))
            state={'events':events};self.download_success_observed=state
            try:
                with patch.object(checker,'VM_BASE',str(base)),patch.object(checker.socket,'gethostname',return_value='lober'), \
                     patch.object(checker,'download_plan',return_value=plan),patch.object(checker,'observe_download',return_value=(observed,{})), \
                     patch.object(checker,'verify_download_trust',return_value={'fixture':True}),patch.object(checker,'DownloadChild',Child), \
                     patch.object(checker,'download_local_guard',side_effect=ValueError('local resource lost') if fail_guard=='local' else None), \
                     patch.object(checker,'host_command',side_effect=TimeoutError('host disconnected') if fail_guard=='host' else None,return_value=json.dumps(observed)), \
                     patch.object(checker,'write_once',side_effect=write),patch.object(checker.time,'monotonic',return_value=10), \
                     patch.object(fcntl,'flock',side_effect=flock),patch.object(importlib.util,'spec_from_file_location',return_value=loader), \
                     patch.object(importlib.util,'module_from_spec',return_value=checker),patch.dict(sys.modules), \
                     patch.object(sys,'argv',[str(runner),'--config',str(config),'--manifest',str(manifest),'--output',str(output)]),contextlib.redirect_stdout(stdout):
                    exec(compile(runner_source,str(runner),'exec'),{'__file__':str(runner),'__name__':'__main__'})
            except SystemExit as error:state['exit_code']=error.code
            except Exception as error:
                state.update(exit_code=1,error_type=type(error).__name__)
                if raise_record_error:raise
            finally:
                state['stdout']=stdout.getvalue()
                state['files']={p.name:p.read_bytes() for p in output.iterdir() if p.is_file()}
                for descriptor in lock_fds:
                    try:os.fstat(descriptor)
                    except OSError:events.append('lock_closed')
                    else:raise AssertionError('lock descriptor still open')
            return state

    def test_download_final_record_failure_cannot_return_or_print_success(self):
        state=self.exercise_download_success_entry(fail_final=True)
        self.assertNotEqual(state['exit_code'],0,'最终记录失败仍退出0')
        self.assertNotIn('SIGNED_IMAGES_DOWNLOADED_NOT_IMPORTED',state['stdout'])
        self.assertNotIn('result.json',state['files'])
        for name,body in [('mini.iso',b'mini-fixture'),('debian-13.7.0-amd64-DVD-1.iso',b'dvd-fixture')]:
            self.assertEqual(state['files'][name],body);self.assertEqual(state['files'][name+'.part'],body)
        self.assertLess(state['events'].index('reap'),state['events'].index('close'))
        self.assertLess(state['events'].index('close'),state['events'].index('lock_closed'))

    def test_download_success_entry_persists_final_record_before_success(self):
        state=self.exercise_download_success_entry()
        self.assertEqual(state['exit_code'],0)
        result=json.loads(state['files']['result.json'])
        self.assertEqual(result['status'],'SIGNED_IMAGES_DOWNLOADED_NOT_IMPORTED')
        self.assertEqual(len(result['downloads']),2)
        self.assertTrue(all(row['reaped'] for row in result['children_stopped']))
        self.assertIn('SIGNED_IMAGES_DOWNLOADED_NOT_IMPORTED',state['stdout'])
        self.assertLess(state['events'].index('close'),state['events'].index('lock_closed'))

    def test_download_entry_local_and_host_failures_stop_own_child_and_preserve_partial(self):
        for fault in ('local','host'):
            with self.subTest(fault=fault):
                state=self.exercise_download_success_entry(fail_guard=fault)
                self.assertNotEqual(state['exit_code'],0)
                self.assertNotIn('SIGNED_IMAGES_DOWNLOADED_NOT_IMPORTED',state['stdout'])
                result=json.loads(state['files']['result.json'])
                self.assertEqual(result['status'],'FAIL_PARTIAL_PRESERVED')
                self.assertTrue(result['children_stopped'][0]['reaped'])
                self.assertEqual(state['files']['mini.iso.part'],b'mini-fixture')
                self.assertLess(state['events'].index('reap'),state['events'].index('close'))
                self.assertLess(state['events'].index('close'),state['events'].index('lock_closed'))

    def download_observation(self):
        return {'hostname': 'lober', 'vm_uuid': '895a44e1-8abd-4463-bbf7-fba8a743ed59',
            'owner': OWNER, 'media_owner': 'production-qualification-20261025',
            'mount': '/home/dev/lober-production-qualification-20261025/machines/media',
            'source': '/dev/vdb', 'filesystem_uuid': '7efe0e2b-6fb6-46f7-bc93-1bc0246ee80a',
            'serial': 'PQ25_MEDIA01_DISK', 'disk_bytes': 17179869184,
            'filesystem_type': 'ext4', 'options': ['rw','nodev','nosuid'],
            'host': 'ser866667880745', 'host_owner': OWNER,
            'host_data_bindings': [{'source': '/var/lib/libvirt/images/lober-pq-20261025/media01.qcow2',
                'target': 'vdb', 'bus': 'virtio', 'serial': 'PQ25_MEDIA01_DISK', 'format': 'qcow2'}] * 2,
            'host_free': 40802189312, 'root_free': 2147483648, 'media_free': 12 * 1024 ** 3}

    def test_download_rejects_wrong_media_or_space_without_lowering_original_budget(self):
        checker = module(self)
        value = self.download_observation()
        checker.check_download_observation(value, OWNER, initial=True, remaining=4060086272)
        for key, bad in [('filesystem_uuid', 'other'), ('serial', 'old-disk'), ('owner', {}),
                         ('host_owner', {}), ('source', '/dev/vda'), ('disk_bytes', 34359738368),
                         ('host_data_bindings', []), ('host_free', 38 * 1024 ** 3 - 1),
                         ('root_free', 1024 ** 3 - 1), ('media_free', 4060086272 + 3992977408 + 1024 ** 3 - 1)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_download_observation({**value, key: bad}, OWNER, initial=True, remaining=4060086272)
        checker.check_download_observation({**value, 'host_free': 6 * 1024 ** 3}, OWNER, initial=False, remaining=0)
        with self.assertRaises(ValueError):
            checker.check_download_observation({**value, 'host_free': 6 * 1024 ** 3 - 1}, OWNER, initial=False, remaining=0)

    def test_download_stream_checks_every_chunk_and_keeps_partial_on_resource_loss(self):
        checker = module(self)
        payload = b'abcd'
        spec = {'size': 4, 'sha256': hashlib.sha256(payload).hexdigest()}
        class Response(io.BytesIO):
            status = 200
            headers = {'Content-Length': '4'}
            def read(self, size): return super().read(min(size, 2))
        written = []
        output = io.BytesIO()
        def guard(total):
            written.append(total)
            if total == 2: raise ValueError('resource lost')
        with self.assertRaises(ValueError):
            checker.download_stream(spec, Response(payload), output, guard, 60)
        self.assertEqual(output.getvalue(), b'ab')
        self.assertIn(0, written)
        self.assertIn(2, written)
        output = io.BytesIO()
        result = checker.download_stream(spec, Response(payload), output, lambda total: None, 60)
        self.assertEqual((output.getvalue(), result['bytes'], result['sha256']), (payload, 4, spec['sha256']))

    def test_download_stream_rejects_bad_status_length_hash_timeout_and_interruption(self):
        checker = module(self)
        spec = {'size': 4, 'sha256': hashlib.sha256(b'abcd').hexdigest()}
        class Response(io.BytesIO):
            status = 200
            headers = {'Content-Length': '4'}
        for payload, status, length in [(b'abcd', 206, '4'), (b'abcd', 200, '5'),
                                        (b'abce', 200, '4'), (b'ab', 200, '4'), (b'abcde', 200, '4')]:
            response = Response(payload);response.status = status;response.headers = {'Content-Length': length}
            with self.subTest(payload=payload, status=status, length=length), self.assertRaises(ValueError):
                checker.download_stream(spec, response, io.BytesIO(), lambda total: None, 60)
        output = io.BytesIO()
        with patch.object(checker.time, 'monotonic', side_effect=[0, 0, 61]), self.assertRaises(TimeoutError):
            checker.download_stream(spec, Response(b'abcd'), output, lambda total: None, 60)
        self.assertEqual(output.getvalue(), b'')
        class Interrupted(Response):
            def read(self, size): raise InterruptedError('TERM')
        with self.assertRaises(InterruptedError):
            checker.download_stream(spec, Interrupted(b'abcd'), io.BytesIO(), lambda total: None, 60)

    def test_download_supervisor_stops_and_reaps_only_its_child_on_guard_failure(self):
        checker = module(self)
        class Child:
            identity = {'pid': 54321, 'starttime': '123', 'uid': 1000}
            def __init__(self): self.stopped = False
            def poll(self): return None
            def stop_and_wait(self): self.stopped = True;return {'pid': 54321, 'reaped': True}
        for failure in (ValueError, TimeoutError, InterruptedError):
            child = Child()
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                path = Path(directory) / 'own.part';path.write_bytes(b'ab')
                def fail(total): raise failure('guard failure')
                with self.assertRaises(failure):
                    checker.supervise_download(child, path, fail, max_seconds=60, idle_seconds=45)
                self.assertTrue(child.stopped)
                self.assertEqual(path.read_bytes(), b'ab')

    def test_download_supervisor_enforces_idle_and_total_deadlines(self):
        checker = module(self)
        class Child:
            def poll(self): return None
            def stop_and_wait(self): self.stopped=True;return {'reaped':True}
        for max_seconds in (30, 60):
            child=Child();child.stopped=False
            with tempfile.TemporaryDirectory() as directory:
                partial=Path(directory)/'own.part';partial.write_bytes(b'ab')
                with patch.object(checker.time,'monotonic',side_effect=[0,0,46]), \
                     patch.object(checker.time,'sleep'), self.assertRaises(TimeoutError):
                    checker.supervise_download(child,partial,lambda total:None,max_seconds,45)
                self.assertTrue(child.stopped)
                self.assertEqual(partial.read_bytes(),b'ab')

    def test_download_entry_records_failure_and_reaps_before_releasing_lock(self):
        checker=module(self)
        events=[]
        class Child:
            identity={'pid':54321,'starttime':'123','uid':1000}
            returncode=None
            def start(self,spec,partial,*args):Path(partial).write_bytes(b'ab');events.append('start')
            def poll(self):return self.returncode
            def stop_and_wait(self):self.returncode=-15;events.append('reap');return {'pid':54321,'reaped':True}
            def close(self):
                if self.returncode is None:raise AssertionError('closed before reap')
                events.append('close')
        with tempfile.TemporaryDirectory() as directory:
            base=Path(directory);(base/'leases').mkdir();(base/'leases/heavy.lock').touch()
            media=base/'machines/media';media.mkdir(parents=True)
            identity=base/'identity.json';identity.write_text(json.dumps(OWNER));identity.chmod(0o600)
            config=base/'config.json';config.write_text(json.dumps({'identity_file':str(identity)}));config.chmod(0o600)
            manifest=base/'manifest.json'
            frozen={'schema':'lober-cloud-download-v1','scope':'download-only','owner':OWNER,
                'review':{'approved':True,'root_reviewer':'root','independent_reviewer':'independent'},
                'executor_sha256':hashlib.sha256(Path(checker.__file__).read_bytes()).hexdigest(),
                'config_sha256':hashlib.sha256(config.read_bytes()).hexdigest(),'vm_uid':os.getuid(),
                'plan':checker.download_plan(),'limits':{'max_seconds_per_image':3600,'idle_seconds':45,'host_poll_seconds':5,'host_timeout_seconds':12}}
            manifest.write_text(json.dumps(frozen));manifest.chmod(0o600)
            observed={**self.download_observation(),'mount':str(media)}
            output=media/'official-download-fixture'
            with patch.object(checker,'VM_BASE',str(base)), patch.object(checker.socket,'gethostname',return_value='lober'), \
                 patch.object(checker,'observe_download',return_value=(observed,{})), \
                 patch.object(checker,'verify_download_trust',return_value={'fixture':True}), \
                 patch.object(checker,'download_local_guard'), patch.object(checker,'DownloadChild',Child), \
                 patch.object(checker,'host_command',side_effect=TimeoutError('host disconnected')), \
                 patch.object(checker.time,'monotonic',return_value=10), self.assertRaises(TimeoutError):
                checker.download_entry(config,manifest,output)
            result=json.loads((output/'result.json').read_text())
            self.assertEqual(result['status'],'FAIL_PARTIAL_PRESERVED')
            self.assertTrue(result['lock_release_after_reap'])
            self.assertTrue(result['children_stopped'][0]['reaped'])
            self.assertEqual((output/'mini.iso.part').read_bytes(),b'ab')
            self.assertLess(events.index('reap'),events.index('close'))

    def test_download_local_guard_detects_remount_and_both_local_reserve_losses(self):
        checker=module(self)
        identity={'mount_id':'7','st_dev':123}
        Usage=type('Usage',(),{})
        for actual,root_free,media_free in [({'mount_id':'8'},2*1024**3,12*1024**3),
                (identity,1024**3-1,12*1024**3),(identity,2*1024**3,3992977408+1024**3-1)]:
            first=Usage();first.free=root_free;second=Usage();second.free=media_free
            with patch.object(checker,'download_mount_identity',return_value=actual), \
                 patch.object(checker.shutil,'disk_usage',side_effect=[first,second]),self.assertRaises(ValueError):
                checker.download_local_guard('/fixture',identity,0)

    def test_download_child_stop_uses_pinned_pidfd_and_waits_for_exact_child(self):
        checker = module(self)
        child = checker.DownloadChild.__new__(checker.DownloadChild)
        child.pid, child.pidfd, child.returncode = 54321, 81, None
        child.identity = {'pid': 54321, 'starttime': '123', 'uid': 1000}
        with patch.object(checker.os, 'waitpid', side_effect=[(0, 0), (54321, signal.SIGTERM)]), \
             patch.object(checker.signal, 'pidfd_send_signal') as pinned, \
             patch.object(checker.os, 'kill', side_effect=AssertionError('must not signal arbitrary PID')):
            result = child.stop_and_wait()
        self.assertTrue(result['reaped'])
        pinned.assert_called_once_with(81, signal.SIGTERM)

    def test_download_plan_requires_signed_image_sizes_and_https(self):
        checker = module(self)
        plan = checker.download_plan()
        checker.check_download_plan(plan)
        for key, value in [('size', 67108863), ('sha256', '0' * 64), ('url', 'http://mirror.invalid/mini.iso')]:
            altered = copy.deepcopy(plan);altered['images'][0][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):checker.check_download_plan(altered)

    def test_download_trust_reuses_signed_chain_and_rejects_changed_files_or_signers(self):
        checker=module(self)
        mini='34e4af5d0aa0886a4b7338dce796b9bc27ef1e6dcf44cdcc4b22c100eb492388'
        sums=mini+'  ./netboot/mini.iso\n'
        release='Origin: Debian\nCodename: trixie\nVersion: 13.7\nSHA256:\n '+hashlib.sha256(sums.encode()).hexdigest()+' '+str(len(sums.encode()))+' main/installer-amd64/20250803+deb13u7/images/SHA256SUMS\n'
        files={'dvd_sums':'347b6c67a3cc0b7ddb60b178f683470c4e2b7ac426c996d9337a2ff36c1a32d2  debian-13.7.0-amd64-DVD-1.iso\n',
            'dvd_signature':'fixture-signature','release':release,'release_signature':'fixture-signature',
            'netboot_sums':sums,'cd_keyring':'fixture-keyring','archive_keyring':'fixture-keyring'}
        dvd='[GNUPG:] VALIDSIG DF9B9C49EAA9298432589D76DA87E80D6294BE9B\n'
        archive=''.join('[GNUPG:] VALIDSIG '+key+'\n' for key in ('B8B80B5B623EAB6AD8775C45B7C5D7D6350947F8','04B54C3CDCA79751B16BC6B5225629DF75B188BD','41587F7DB8C774BCCF131416762F67A0B2C39DE4'))
        with tempfile.TemporaryDirectory() as directory:
            root=Path(directory);trust={}
            for name,body in files.items():
                path=root/name;path.write_text(body)
                trust[name]={'path':str(path),'size':len(body.encode()),'sha256':hashlib.sha256(body.encode()).hexdigest()}
            frozen={'trust':trust,'plan':checker.download_plan()}
            output=root/'valid';output.mkdir()
            with patch.object(checker,'command',side_effect=[dvd,archive]):checker.verify_download_trust(frozen,output)
            self.assertTrue((output/'signature-chain.json').is_file())
            for statuses in ([dvd.replace('DF9B9C49EAA9298432589D76DA87E80D6294BE9B','0'*40),archive], [dvd,archive.splitlines()[0]+'\n']):
                with patch.object(checker,'command',side_effect=statuses),self.assertRaises(ValueError):
                    checker.verify_download_trust(frozen,root)
            with patch.object(checker,'command',side_effect=subprocess.CalledProcessError(1,['gpgv'])),self.assertRaises(subprocess.CalledProcessError):
                checker.verify_download_trust(frozen,root)
            (root/'dvd_sums').write_text('changed')
            with self.assertRaises(ValueError):checker.verify_download_trust(frozen,root)

    def test_download_child_creation_failure_preserves_partial_and_restores_signal_mask(self):
        checker=module(self)
        child=checker.DownloadChild()
        with tempfile.TemporaryDirectory() as directory:
            partial=Path(directory)/'own.part'
            with patch.object(checker.os,'fork',side_effect=OSError('resource unavailable')), \
                 patch.object(checker.signal,'pthread_sigmask',return_value=set()) as mask,self.assertRaises(OSError):
                child.start({},partial,Path(directory)/'status',Path(directory),{},0,99,60)
            self.assertIsNotNone(child.poll())
            self.assertTrue(child.file.closed)
            self.assertTrue(partial.is_file())
            mask.assert_any_call(signal.SIG_SETMASK,set())

    def test_download_unapproved_manifest_fails_before_resource_probes(self):
        checker=module(self)
        with tempfile.TemporaryDirectory() as directory:
            manifest=Path(directory)/'manifest.json';config=Path(directory)/'config.json'
            manifest.write_text(json.dumps({'schema':'lober-cloud-download-v1','scope':'download-only','review':{'approved':False}}))
            config.write_text('{}');manifest.chmod(0o600);config.chmod(0o600)
            with patch.object(checker,'observe_download',side_effect=AssertionError('must not probe')),self.assertRaises(ValueError):
                checker.download_entry(config,manifest,Path(directory)/'output')

    def collect_resume_snapshot(self, active_names, command_error=False):
        checker = module(self)
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory)
            failed = base / 'records-network-123'
            failed.mkdir()
            for path in (base / 'failure.json', failed / 'failure.json'):
                path.write_text('{"phase":"network"}')
                path.chmod(0o600)
            for name in ('install-host-isolation-actual.json', 'activate-network-actual.json'):
                (failed / name).write_text('{}')
            (base / 'linux01.qcow2').write_bytes(b'fixture')
            net = checker.definitions(OWNER)['network.xml']
            service = '<domain><name>lober</name><uuid>895a44e1-8abd-4463-bbf7-fba8a743ed59</uuid><devices/></domain>'
            policy = {'nftables': [row['add'] for row in checker.isolation_policy('host', 'virbr-pq25')['nftables']]}
            outputs = {
                ('virsh', '-c', 'qemu:///system', 'net-list', '--name'): active_names,
                ('virsh', '-c', 'qemu:///system', 'net-dumpxml', 'lober-pq25'): net,
                ('virsh', '-c', 'qemu:///system', 'net-dumpxml', 'lober-pq25', '--inactive'): net,
                ('virsh', '-c', 'qemu:///system', 'domstate', OWNER['target_uuid']): 'shut off\n',
                ('virsh', '-c', 'qemu:///system', 'dumpxml', OWNER['target_uuid'], '--inactive'): XML,
                ('nft', '-j', 'list', 'table', 'inet', 'lober_pq25'): json.dumps(policy),
                ('qemu-img', 'info', '--force-share', '--output=json', str(base / 'linux01.qcow2')): json.dumps(disk()['info']),
                ('qemu-img', 'map', '--force-share', '--output=json', str(base / 'linux01.qcow2')): json.dumps(disk()['map']),
            }
            for suffix in ((), ('--inactive',)):
                outputs[('virsh', '-c', 'qemu:///system', 'dumpxml', '895a44e1-8abd-4463-bbf7-fba8a743ed59', *suffix)] = service
            def read_only_command(args, *unused, **kwargs):
                key = tuple(args)
                if key == ('virsh', '-c', 'qemu:///system', 'net-list', '--name') and command_error:
                    raise subprocess.CalledProcessError(1, args, stderr='read failed')
                if key not in outputs:
                    raise AssertionError('非核准只读命令：' + repr(args))
                return outputs[key]
            host = checker.HostPreparation.__new__(checker.HostPreparation)
            host.base, host.owner = base, OWNER
            with patch.object(checker, 'command', side_effect=read_only_command):
                actual = host.resume_network_snapshot({'failed_records': failed.name})
            contract = {'owner': OWNER, 'scope': 'continue-active-network-before-service-attach',
                        'failed_records': failed.name, 'fingerprints': checker.network_resume_fingerprints(actual)}
            return checker, actual, contract

    def test_real_resume_collection_accepts_exact_active_network(self):
        checker, actual, contract = self.collect_resume_snapshot('default\nlober-pq25\n')
        self.assertIs(actual.get('network_active'), True, '真实采集缺少活动状态')
        checker.check_network_resume(actual, contract, OWNER)

    def test_real_resume_collection_rejects_stopped_or_similarly_named_network(self):
        for names in ('default\n', 'default\nlober-pq25-old\n', ''):
            with self.subTest(names=names):
                checker, actual, contract = self.collect_resume_snapshot(names)
                self.assertIs(actual.get('network_active'), False, '必须按准确网名读取活动状态')
                with self.assertRaises(ValueError):
                    checker.check_network_resume(actual, contract, OWNER)

    def test_real_resume_collection_propagates_active_list_read_failure(self):
        with self.assertRaises(subprocess.CalledProcessError):
            self.collect_resume_snapshot('lober-pq25\n', command_error=True)

    def test_network_continue_only_accepts_the_frozen_unattached_partial_state(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_network_resume'), '精确部分状态继续检查尚未实现')
        net = checker.definitions(OWNER)['network.xml']
        service = '<domain><name>lober</name><uuid>895a44e1-8abd-4463-bbf7-fba8a743ed59</uuid><devices/></domain>'
        policy = checker.isolation_policy('host', 'virbr-pq25')
        snapshot = {'owner': OWNER, 'failed_records': 'records-network-123', 'failure_sha256': 'a' * 64,
            'failed_record_sha256': 'b' * 64, 'target_state': 'shut off', 'network_active': True,
            'target_xml': XML, 'target_disk': disk(), 'inactive_network_xml': net,
            'active_network_xml': net.replace("<forward mode='nat'/>", "<forward mode='nat'><nat><port start='1024' end='65535'/></nat></forward>"),
            'service_live_xml': service, 'service_inactive_xml': service,
            'host_policy': {'nftables': [row['add'] for row in policy['nftables']]},
            'step_flags': {'isolation_recorded': True, 'network_recorded': True, 'attachment_started': False}}
        contract = {'owner': OWNER, 'scope': 'continue-active-network-before-service-attach',
            'failed_records': 'records-network-123', 'fingerprints': checker.network_resume_fingerprints(snapshot)}
        checker.check_network_resume(snapshot, contract, OWNER)
        for key, value in [('failure_sha256', 'c' * 64), ('target_state', 'running'), ('network_active', False),
                           ('service_inactive_xml', service.replace('<devices/>', '<devices><interface/></devices>'))]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_network_resume({**snapshot, key: value}, contract, OWNER)
        bad = copy.deepcopy(snapshot)
        bad['step_flags']['attachment_started'] = True
        with self.assertRaises(ValueError):
            checker.check_network_resume(bad, contract, OWNER)
        bad = copy.deepcopy(snapshot)
        bad['host_policy']['nftables'].pop()
        with self.assertRaises(ValueError):
            checker.check_network_resume(bad, contract, OWNER)
    def test_observed_active_network_nat_default_is_the_only_allowed_normalization(self):
        checker = module(self)
        self.assertIn('active', inspect.signature(checker.check_network).parameters, '活动网络默认NAT规范化尚未实现')
        expected = checker.definitions(OWNER)['network.xml']
        actual = expected.replace("<forward mode='nat'/>", "<forward mode='nat'><nat><port start='1024' end='65535'/></nat></forward>")
        checker.check_network(actual, OWNER, active=True)
        with self.assertRaises(ValueError):
            checker.check_network(actual, OWNER, active=False)
        for before, after in [("start='1024'", "start='1025'"), ("end='65535'", "end='65534'"),
                              ('192.168.201.1', '192.168.200.1'), ('virbr-pq25', 'virbr-inst'),
                              ('52:54:00:25:10:fe', '52:54:00:25:10:ff'), ('undionly.kpxe', 'old.kpxe')]:
            with self.subTest(before=before), self.assertRaises(ValueError):
                checker.check_network(actual.replace(before, after), OWNER, active=True)
    def test_same_version_iso_cannot_replace_frozen_prepare_chain(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_prepare_binding'), '准备结果和实际ISO绑定尚未实现')
        iso = {'id': 'owned-iso', 'name': 'pq25-DVD', 'family': 'linux', 'series': 'debian',
               'state': 'ready', 'version': '13.7.0', 'arch': 'x86_64', 'sha256': 'd' * 64,
               'artifacts': {'kernel': 'debian/linux', 'initrd': 'debian/initrd.gz', 'repo': 'debian/install/'}, 'rev': 7}
        manifest = {'product_sha': 'a' * 40, 'test_sha': 'b' * 40, 'binary_sha256': 'c' * 64, 'run_id': OWNER['run_id'],
            'cloud': {'owner': OWNER, 'executor_sha256': 'e' * 64, 'instance_record_sha256': 'f' * 64,
                      'media': {'version': '13.7.0', 'dvd': {'sha256': 'd' * 64}, 'netboot': {'sha256': '1' * 64}}}}
        canonical = lambda value: hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()
        prepared = {k: manifest[k] for k in ('product_sha', 'test_sha', 'binary_sha256', 'run_id')}
        prepared.update(owner=OWNER, executor_sha256='e' * 64, instance_record_sha256='f' * 64,
                        media_manifest_sha256=canonical(manifest['cloud']['media']), iso=iso)
        checker.check_prepare_binding(prepared, manifest, iso)
        for key, value in [('id', 'another-same-version'), ('sha256', '9' * 64), ('state', 'deprecated')]:
            bad = {**iso, key: value}
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_prepare_binding(prepared, manifest, bad)
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'prepare.json'
            path.write_text(json.dumps(prepared));path.chmod(0o600)
            expected = hashlib.sha256(path.read_bytes()).hexdigest()
            checker.bound_json(path, expected)
            path.write_text(json.dumps({**prepared, 'iso': {**iso, 'id': 'replaced'}}))
            with self.assertRaises(ValueError):
                checker.bound_json(path, expected)

    def test_running_instance_must_match_frozen_pid_binary_and_socket(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_instance_binding'), '实际运行实例绑定尚未实现')
        frozen = {'owner': OWNER, 'pid': 777, 'starttime': '12345', 'uid': 1000, 'pgrp': 777,
                  'session': 777, 'vm_boot_id': 'vm-boot', 'cmdline_sha256': 'a' * 64,
                  'executable_path': '/owned/candidate', 'executable_sha256': 'b' * 64,
                  'config_path': '/owned/config.json', 'config_sha256': 'c' * 64,
                  'socket': '192.168.201.10:18090','environment_sha256':'d'*64,
                  'database_origin_sha256':'e'*64,'database':{'database':'pq25_fixture'}}
        checker.check_instance_binding(frozen, dict(frozen), OWNER, '/owned/candidate', 'b' * 64)
        for key, value in [('pid', 778), ('starttime', '999'), ('executable_sha256', '9' * 64),
                           ('socket', '0.0.0.0:8080'), ('pgrp', 1), ('config_sha256', '8' * 64),
                           ('database',{'database':'lober'}),('environment_sha256','9'*64)]:
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_instance_binding(frozen, {**frozen, key: value}, OWNER, '/owned/candidate', 'b' * 64)

    def test_ssh_exit_zero_from_another_machine_cannot_count_as_guest_login(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_ssh_binding'), 'SSH和agent身份比较尚未实现')
        identity = {'smbios_serial': 'PQ25L01', 'machine_id': 'm' * 32, 'boot_id': 'boot',
                    'root_uuid': 'root-uuid', 'root_disk_serial': 'PQ25_LINUX01_DISK'}
        access = {'owner': OWNER, 'target_uuid': OWNER['target_uuid'], 'address': '192.168.201.101',
                  'host_public_key': 'ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA'}
        agent = {**identity, 'ssh_host_public_key': access['host_public_key'], 'address': access['address']}
        checker.check_ssh_binding(agent, identity, access, OWNER, ssh_exit=0)
        for key in identity:
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_ssh_binding(agent, {**identity, key: 'another-machine'}, access, OWNER, ssh_exit=0)
        with self.assertRaises(ValueError):
            checker.check_ssh_binding({**agent, 'ssh_host_public_key': 'ssh-ed25519 OTHER'}, identity, access, OWNER, ssh_exit=0)
        with self.assertRaises(ValueError):
            checker.check_ssh_binding(agent, identity, access, OWNER, ssh_exit=255)
    def test_existing_firewall_rules_cannot_disappear_or_reorder(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_existing_rules_preserved'), '旧规则差异检查尚未实现')
        def row(port):
            return {'rule': {'family': 'inet', 'table': 'old', 'chain': 'input', 'handle': port,
                'expr': [{'match': {'op': '==', 'left': {'payload': {'protocol': 'tcp', 'field': 'dport'}}, 'right': port}}, {'accept': None}]}}
        before = {'nftables': [row(22), row(8080)]}
        checker.check_existing_rules_preserved(before, {'nftables': [row(18090), row(22), row(8080)]}, 'lober_pq25')
        for rows in ([row(22)], [row(8080), row(22)]):
            with self.assertRaises(ValueError):
                checker.check_existing_rules_preserved(before, {'nftables': rows}, 'lober_pq25')
    def test_private_umask_does_not_block_new_storage_traversal(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'initialize_storage_dirs'), '显式目录权限尚未实现')
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory) / 'new-only'
            old = os.umask(0o077)
            try:
                checker.initialize_storage_dirs(base, OWNER)
            finally:
                os.umask(old)
            self.assertEqual(base.stat().st_mode & 0o777, 0o755)
            self.assertEqual((base / 'tftp').stat().st_mode & 0o777, 0o755)
            self.assertEqual((base / '.owner.json').stat().st_mode & 0o777, 0o600)
            with self.assertRaises(ValueError):
                checker.initialize_storage_dirs(base, OWNER)

    def test_frozen_attachment_xml_cannot_be_replaced_or_linked(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'read_frozen_xml'), '附加XML字节核对尚未实现')
        raw = "<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='" + ROOT + "/media01.qcow2'/><target dev='vdb' bus='virtio'/><serial>PQ25_MEDIA01_DISK</serial></disk>"
        with tempfile.TemporaryDirectory() as directory:
            path = Path(directory) / 'disk.xml'
            path.write_text(raw + '\n')
            path.chmod(0o600)
            checker.read_frozen_xml(path, raw, 'disk')
            for before, after in [('media01.qcow2', '../lober.qcow2'), ('PQ25_MEDIA01_DISK', 'OLD'),
                                  ("dev='vdb'", "dev='vda'")]:
                path.write_text(raw.replace(before, after) + '\n')
                with self.assertRaises(ValueError):
                    checker.read_frozen_xml(path, raw, 'disk')
            path.unlink()
            other = Path(directory) / 'other.xml'
            other.write_text(raw + '\n')
            path.symlink_to(other)
            with self.assertRaises(ValueError):
                checker.read_frozen_xml(path, raw, 'disk')

    def test_service_attach_must_preserve_both_live_and_persistent_devices(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_service_delta'), '两份实际配置差异检查尚未实现')
        before = '<domain><name>lober</name><uuid>895a44e1-8abd-4463-bbf7-fba8a743ed59</uuid><devices><disk type="file" device="disk"><source file="/old/root.qcow2"/><target dev="vda" bus="virtio"/></disk></devices></domain>'
        device = "<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='" + ROOT + "/media01.qcow2'/><target dev='vdb' bus='virtio'/><serial>PQ25_MEDIA01_DISK</serial></disk>"
        after = before.replace('</devices>', device + '</devices>')
        checker.check_service_delta(before, after, device, 'disk')
        with self.assertRaises(ValueError):
            checker.check_service_delta(before, after.replace('/old/root.qcow2', '/old/replaced.qcow2'), device, 'disk')
        with self.assertRaises(ValueError):
            checker.check_service_pair(before, after, device, 'disk', before_attach=True)

    def test_empty_or_changed_isolation_and_active_network_must_block_start(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'isolation_policy'), '实际规则内容检查尚未实现')
        expected = checker.isolation_policy('vm', 'pqnew0')
        actual = {'nftables': [entry['add'] for entry in expected['nftables']]}
        checker.check_policy(actual, expected)
        with self.assertRaises(ValueError):
            checker.check_policy({'nftables': actual['nftables'][:1]}, expected)
        wrong = copy.deepcopy(actual)
        wrong['nftables'].pop()
        with self.assertRaises(ValueError):
            checker.check_policy(wrong, expected)
        network = checker.definitions(OWNER)['network.xml']
        checker.check_network(network, OWNER)
        with self.assertRaises(ValueError):
            checker.check_network(network.replace('192.168.201.1', '192.168.200.1'), OWNER)

    def test_command_timeout_keeps_started_and_partial_output_records(self):
        checker = module(self)
        with tempfile.TemporaryDirectory() as directory:
            checker.COMMAND_RECORDS = Path(directory)
            with self.assertRaises(subprocess.TimeoutExpired):
                checker.command([sys.executable, '-u', '-c', 'import time; print("partial-write-step",flush=True); time.sleep(30)'], timeout=.3)
            rows = [json.loads(p.read_text()) for p in Path(directory).glob('command-*.json')]
            self.assertTrue(any(r.get('state') == 'STARTED' for r in rows), '命令开始前未登记')
            self.assertTrue(any(r.get('state') == 'TIMEOUT' and 'partial-write-step' in r.get('stdout', '') for r in rows), '超时输出未保留')

    def test_partial_live_attach_failure_keeps_both_actual_states(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'apply_attachment'), '部分附加结果保留尚未实现')
        before = '<domain><name>lober</name><uuid>895a44e1-8abd-4463-bbf7-fba8a743ed59</uuid><devices/></domain>'
        device = "<disk type='file' device='disk'><driver name='qemu' type='qcow2'/><source file='" + ROOT + "/media01.qcow2'/><target dev='vdb' bus='virtio'/><serial>PQ25_MEDIA01_DISK</serial></disk>"
        class Backend:
            def __init__(self):
                self.states = {'live': before, 'inactive': before}
            def dump(self, side):
                return self.states[side]
            def attach(self, side, body):
                if side == 'inactive':
                    raise RuntimeError('persistent failed after live success')
                self.states['live'] = before.replace('<devices/>', '<devices>' + body + '</devices>')
        with tempfile.TemporaryDirectory() as directory:
            backend = Backend()
            with self.assertRaises(RuntimeError):
                checker.apply_attachment(backend, device, 'disk', Path(directory))
            partial = json.loads((Path(directory) / 'attachment-failure.json').read_text())
            self.assertIn('PQ25_MEDIA01_DISK', partial['actual']['live'])
            self.assertNotIn('PQ25_MEDIA01_DISK', partial['actual']['inactive'])
            self.assertFalse(partial['safe_to_retry'])

    def test_failed_cleanup_does_not_release_heavy_lease(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'SafetyLease'), '失败收尾锁保护尚未实现')
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'heavy.lock').touch()
            count = []
            def stop():
                # 独立进程真实争锁，不能在停止尚未证实时取到锁。
                result = subprocess.run([sys.executable, '-c', 'import fcntl,sys; f=open(sys.argv[1]); fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)', str(root / 'heavy.lock')], capture_output=True)
                self.assertNotEqual(result.returncode, 0)
                count.append(1)
                return {'client_stopped': len(count) >= 3, 'imports_stopped': len(count) >= 3}
            lease = checker.SafetyLease(root / 'heavy.lock', root, stop, retry_seconds=.01)
            lease.acquire()
            lease.finish(failed=True)
            self.assertEqual(len(count), 3)
            result = subprocess.run([sys.executable, '-c', 'import fcntl,sys; f=open(sys.argv[1]); fcntl.flock(f,fcntl.LOCK_EX|fcntl.LOCK_NB)', str(root / 'heavy.lock')], capture_output=True)
            self.assertEqual(result.returncode, 0)

    def test_failure_matrix_and_real_term_take_the_guarded_stop_path(self):
        checker = module(self)
        self.assertTrue(hasattr(checker.SafetyLease, '__enter__'), '共同异常停止入口尚未实现')
        for failure in ('resource', 'api_disconnect', 'ssh_timeout', 'capture_failed', 'define_partial', 'attach_partial', 'TERM'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as directory:
                root = Path(directory)
                (root / 'heavy.lock').touch()
                (root / 'old-disk-sentinel').write_text('unchanged')
                stopped = []
                def stop():
                    stopped.append(True)
                    return {'client_stopped': True, 'imports_stopped': True}
                with self.assertRaises((RuntimeError, InterruptedError)):
                    with checker.SafetyLease(root / 'heavy.lock', root, stop):
                        (root / 'new-failed-disk').write_text('preserve')
                        if failure == 'TERM':
                            os.kill(os.getpid(), signal.SIGTERM)
                        raise RuntimeError(failure)
                self.assertTrue(stopped)
                self.assertEqual((root / 'old-disk-sentinel').read_text(), 'unchanged')
                self.assertEqual((root / 'new-failed-disk').read_text(), 'preserve')
    def test_signed_release_must_bind_same_version_netboot_checksums(self):
        checker = module(self)
        self.assertTrue(hasattr(checker, 'check_media_chain'), '介质信任链检查器尚未实现')
        import hashlib
        checksums = 'b' * 64 + '  ./netboot/mini.iso\n'
        digest = hashlib.sha256(checksums.encode()).hexdigest()
        release = 'Origin: Debian\nVersion: 13.7\nCodename: trixie\nSHA256:\n ' + digest + ' ' + str(len(checksums)) + ' main/installer-amd64/20250803+deb13u7/images/SHA256SUMS\n'
        checker.check_media_chain(release, checksums, '13.7.0', '20250803+deb13u7', 'b' * 64)
        for bad_release, bad_sums, bad_version in [(release.replace(digest, 'a' * 64), checksums, '13.7.0'),
                                                   (release, checksums, '13.6.0'),
                                                   (release, checksums.replace('b' * 64, 'c' * 64), '13.7.0')]:
            with self.assertRaises(ValueError):
                checker.check_media_chain(bad_release, bad_sums, bad_version, '20250803+deb13u7', 'b' * 64)

    def test_new_target_must_have_exact_owner_and_blank_disk(self):
        checker = module(self)
        checker.check_target(XML, OWNER, OWNER, disk(), blank=True)
        for bad in ('lober-e2e-t4', '102'):
            with self.subTest(target=bad), self.assertRaises(ValueError):
                checker.check_target(XML.replace('lober-pq25-linux01', bad), OWNER, OWNER, disk(), blank=True)
        for old in ('/var/lib/libvirt/images/lober.qcow2', '/var/lib/libvirt/images/lober-e2e-t4-d1-7g.qcow2'):
            with self.subTest(path=old), self.assertRaises(ValueError):
                checker.check_target(XML.replace(ROOT + '/linux01.qcow2', old), OWNER, OWNER, disk(), blank=True)
        wrong_owner = {**OWNER, 'run_id': 'old'}
        with self.assertRaises(ValueError):
            checker.check_target(XML, wrong_owner, OWNER, disk(), blank=True)
        for key, value in [('backing-filename', '/old.qcow2'), ('format', 'raw'), ('virtual-size', 34359738368)]:
            bad = disk()
            bad['info'][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_target(XML, OWNER, OWNER, bad, blank=True)
        for extents in ([], [{'start': 0, 'length': 512, 'data': False, 'zero': True}],
                        [{'start': 0, 'length': 17179869184, 'data': True, 'zero': False}]):
            with self.subTest(extents=extents), self.assertRaises(ValueError):
                checker.check_target(XML, OWNER, OWNER, {**disk(), 'map': extents}, blank=True)

    def test_identity_network_and_disk_first_must_be_rejected(self):
        checker = module(self)
        for before, after in [('PQ25L01', 'R3LQVQ9'), ('52:54:00:25:10:01', '52:54:00:a1:20:14'),
                              ('network="lober-pq25"', 'network="lober-install"'),
                              ('<boot order="1"/>', '<boot order="3"/>'),
                              ('PQ25_LINUX01_DISK', 'OLD_DISK'), ('arch="x86_64"', 'arch="aarch64"')]:
            with self.subTest(before=before), self.assertRaises(ValueError):
                checker.check_target(XML.replace(before, after), OWNER, OWNER, disk(), blank=True)

    def test_resources_and_all_existing_conflicts_must_be_rejected(self):
        checker = module(self)
        checker.check_preflight(snapshot(), OWNER)
        changes = [('hostname', 'other'), ('free_bytes', 40802189311),
                   ('vm_free_bytes', 1073741823), ('base_exists', True),
                   ('domains', [XML]), ('networks', ['<network><name>lober-pq25</name></network>']),
                   ('addresses', ['192.168.201.5/24']), ('routes', ['192.168.201.0/24']),
                   ('bridges', ['virbr-pq25']), ('vm_ports', [18090]), ('host_ports', [18082]),
                   ('auth', {'ok': False, 'strict_host_key': True, 'from_vm': True})]
        for key, value in changes:
            bad = snapshot()
            bad[key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_preflight(bad, OWNER)

    def test_registered_hash_alone_cannot_claim_an_old_target(self):
        checker = module(self)
        value = snapshot()
        value.update(base_exists=True, owner=OWNER, registered_target_sha256='a' * 64,
                     actual_target_sha256='a' * 64, actual_target_xml=XML, target_disk=disk())
        checker.check_preflight(value, OWNER, existing=True)
        value['actual_target_xml'] = XML.replace('lober-pq25-linux01', 'lober-e2e-t4')
        with self.assertRaises(ValueError):
            checker.check_preflight(value, OWNER, existing=True)

    def test_candidate_or_executor_digest_change_must_be_rejected(self):
        checker = module(self)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'binary').write_bytes(b'candidate')
            (root / 'archive').write_bytes(b'source')
            (root / 'executor').write_bytes(b'executor')
            import hashlib
            digest = lambda path: hashlib.sha256((root / path).read_bytes()).hexdigest()
            value = {'product_sha': 'a' * 40, 'test_sha': 'b' * 40, 'run_id': 'pq25-checker',
                     'binary': str(root / 'binary'), 'binary_sha256': digest('binary'),
                     'source_archive': str(root / 'archive'), 'source_archive_sha256': digest('archive'),
                     'files': {'executor': digest('executor')},
                     'cloud': {'owner': OWNER, 'executor_sha256': digest('executor'),
                               'target': TARGET_SPEC,
                               'source_root': str(root),
                               'executor': str(root / 'executor'), 'system': 'debian',
                               'architecture': 'x86_64', 'firmware': 'bios'}}
            checker.check_manifest(value)
            wrong = copy.deepcopy(value)
            wrong['cloud']['target']['sn'] = 'R3LQVQ9'
            with self.assertRaises(ValueError):
                checker.check_manifest(wrong)
            for filename in ('binary', 'archive', 'executor'):
                old = (root / filename).read_bytes()
                (root / filename).write_bytes(b'changed')
                with self.subTest(filename=filename), self.assertRaises(ValueError):
                    checker.check_manifest(value)
                (root / filename).write_bytes(old)

    def test_no_terminal_or_login_or_repeat_disk_evidence_is_not_pass(self):
        checker = module(self)
        evidence = self.good_evidence()
        checker.check_qualification(evidence, negative=False)
        for key in ('events', 'ssh_exit', 'root_uuid', 'machine_id', 'sentinel_sha256',
                    'network_boot_request', 'localboot_response', 'retry_rejected', 'duplicate_callback_rejected'):
            bad = copy.deepcopy(evidence)
            if key in bad['guest']:
                bad['guest'][key] = 1 if key == 'ssh_exit' else ''
            elif key in bad['repeat']:
                bad['repeat'][key] = False
            else:
                bad[key] = []
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_qualification(bad, negative=False)
        for key in ('root_uuid', 'machine_id', 'sentinel_sha256'):
            bad = copy.deepcopy(evidence)
            bad['repeat']['guest'][key] = 'changed'
            with self.subTest(changed=key), self.assertRaises(ValueError):
                checker.check_qualification(bad, negative=False)
        bad = copy.deepcopy(evidence)
        bad['task']['state'] = 'pending'
        with self.assertRaises(ValueError):
            checker.check_qualification(bad, negative=False)

    def test_success_must_include_exact_frozen_required_hook_result(self):
        checker = module(self)
        value = self.good_evidence()
        value['events'] = [e for e in value['events'] if e['kind'] != 'hook.report']
        with self.assertRaises(ValueError):
            checker.check_qualification(value, negative=False)

    def test_only_the_frozen_required_hook_rc41_counts_as_expected_failure(self):
        checker = module(self)
        evidence = self.good_evidence()
        evidence['task']['state'] = 'failed'
        evidence['events'] = evidence['events'][:2] + [{'kind': 'hook.report', 'taskID': 'task-real', 'seq': 3,
            'fields': {'slot': '6', 'instance': '1', 'timing': 'firstboot', 'digest': 'a' * 64, 'rc': '41'}}]
        evidence['repeat']['task_state'] = 'failed'
        checker.check_qualification(evidence, negative=True)
        for key, value in [('slot', '5'), ('instance', '2'), ('digest', 'b' * 64), ('rc', '0')]:
            bad = copy.deepcopy(evidence)
            bad['events'][2]['fields'][key] = value
            with self.subTest(key=key), self.assertRaises(ValueError):
                checker.check_qualification(bad, negative=True)

    @staticmethod
    def good_evidence():
        guest = {'ssh_exit': 0, 'os_id': 'debian', 'version_id': '13', 'arch': 'x86_64',
                 'firmware': 'bios', 'root_disk_serial': 'PQ25_LINUX01_DISK',
                 'root_source': '/dev/vda2', 'root_uuid': 'filesystem-uuid', 'machine_id': 'm' * 32,
                 'sentinel_sha256': 'f' * 64, 'boot_id': 'first-boot', 'hook_runs': 1,
                 'credential_files': 0, 'retained_auth_paths': [], 'address': '192.168.201.101',
                 'default_gateway': '192.168.201.1', 'partitions': ['/dev/vda1', '/dev/vda2']}
        return {'media_version': '13', 'task': {'id': 'task-real', 'hostSN': 'PQ25L01', 'state': 'done',
            'snapshot': {'slots': [{'name': 'qualify', 'seq': 6, 'timing': 'firstboot', 'exec': True,
                                  'instances': [{'required': True, 'digest': 'a' * 64}]}]}},
            'events': [{'kind': 'machine.installing', 'taskID': 'task-real', 'seq': 1, 'fields': {}},
                       {'kind': 'machine.init_start', 'taskID': 'task-real', 'seq': 2, 'fields': {}},
                       {'kind': 'hook.report', 'taskID': 'task-real', 'seq': 3,
                        'fields': {'slot': '6', 'instance': '1', 'timing': 'firstboot', 'digest': 'a' * 64, 'rc': '0'}},
                       {'kind': 'machine.ready', 'taskID': 'task-real', 'seq': 4, 'fields': {}}],
            'guest': guest, 'repeat': {'task_id': 'task-real', 'task_state': 'done',
                'guest': {**guest, 'boot_id': 'second-boot'}, 'network_boot_request': True,
                'localboot_response': True, 'retry_rejected': True, 'duplicate_callback_rejected': True}}


if __name__ == '__main__':
    unittest.main()
