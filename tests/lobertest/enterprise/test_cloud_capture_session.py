"""只在 lober VM 运行标准库替身和小人工包；不是真实装机证据。"""
import copy
import hashlib
import json
import os
from pathlib import Path
import socket
import subprocess
import sys
import tempfile
import io
import types
import unittest
from unittest.mock import patch

import cloud_capture_session as session
import test_cloud_packet_capture as fixture

ROOT = Path('/home/dev/lober-production-qualification-20261025/machines/cloud-capture-session')
OWNER = {'purpose': session.PURPOSE, 'run_id': 'capture-fixture-only', 'target_uuid': 'a9e01e4a-7ad8-4cb5-bb0e-b79bc9138e31'}
TARGET = {'host': session.HOST, 'uuid': OWNER['target_uuid'], 'bridge': session.BRIDGE, 'network': session.NETWORK,
          'mac': fixture.CLIENT, 'guest_ip': fixture.CLIENT_IP, 'host_ip': fixture.BOOT_IP, 'service_ip': fixture.SERVICE_IP,
          'candidate_port': 18090, 'artifact_port': 18082, 'disk_path': '/var/lib/libvirt/images/lober-pq-20261025-fixture/linux01.qcow2', 'sn': 'FIXTURE'}


def contract(data=None):
    data = fixture.pcap(fixture.frames()) if data is None else data
    packet = fixture.contract(data)
    for key in ('pcap_sha256', 'pcap_size', 'capture_start_ns', 'capture_end_ns'):
        packet.pop(key)
    return {'schema': session.SCHEMA, 'host': session.HOST, 'bridge': session.BRIDGE, 'network': session.NETWORK,
            'targets': {OWNER['target_uuid']: {'target_sha256': session.canonical(TARGET), 'packet_contract': packet,
                'boot_chain': {'firmware_sha256': 'a' * 64, 'dhcp_config_sha256': 'b' * 64,
                               'user_class_sha256': fixture.digest(b''), 'boot_file': 'pq25.ipxe', 'required_tftp': ['pq25.ipxe']}}},
            'tftp_port_range': [40000, 40010], 'max_bytes': 64*1024**2, 'max_seconds': 60, 'tcp_reset_policy': 'no-rst'}


def process(folder):
    return {'pid': 12345, 'starttime': '701', 'uid': 0, 'exe': '/usr/bin/tcpdump', 'boot_id': 'frozen-boot',
            'argv': ['/usr/bin/tcpdump', '-Z', 'root', '-U', '-n', '-s', '0', '-B', '4096', '-i', session.BRIDGE,
                     '-w', folder + '/capture.pcap', session.capture_filter(contract()['targets'][OWNER['target_uuid']]['packet_contract'])]}


class FakeHost:
    def __init__(self, data=None):
        self.data = fixture.pcap(fixture.frames()) if data is None else data
        self.change = None
        self.calls = []
        self.sink_change = None
        self.capture_count = len(fixture.frames())

    def __call__(self, config, source, timeout):
        # 只读脚本中的字面量，绝不执行生成的宿主程序。
        import ast
        tree = ast.parse(source)
        request = next(ast.literal_eval(n.value) for n in tree.body if isinstance(n, ast.Assign)
                       and any(isinstance(t, ast.Name) and t.id == 'r' for t in n.targets))
        action = ast.literal_eval(tree.body[-1].value.args[0].args[0].args[1])
        self.calls.append(action)
        value = {'session_id': request['session_id'], 'owner': copy.deepcopy(request['owner']), 'target_uuid': request['target']['uuid'],
                 'bridge': session.BRIDGE, 'filter': request['filter'], 'process': process(request['folder']),
                 'capture_start_ns': 1_000_000_000, 'state': 'RUNNING'}
        if action == 'close':
            value.update(state='FROZEN', reason='REQUESTED', stopped=True, capture_end_ns=2_000_000_000,
                         host_path=request['folder'] + '/capture.pcap', sha256=fixture.digest(self.data), size=len(self.data),
                         file_identity={'device': 1, 'inode': 2, 'mtime_ns': 3, 'size': len(self.data)},
                         counts={'captured': self.capture_count, 'received by filter': self.capture_count, 'dropped by kernel': 0}, exit=0)
        if self.change:
            self.change(value)
        return json.dumps(value)

    def stream_to_private_file(self, config, source, path, timeout):
        compile(source, '<private-stream>', 'exec')
        fd = os.open(path, os.O_WRONLY | os.O_CREAT | os.O_EXCL, 0o600)
        with os.fdopen(fd, 'wb') as f:
            f.write(self.data)
        result = {'path': str(path), 'sha256': fixture.digest(self.data), 'bytes': len(self.data), 'exit': 0}
        if self.sink_change:
            self.sink_change(result)
        return result


class SessionChecks(unittest.TestCase):
    def setUp(self):
        self.assertEqual((socket.gethostname(), os.geteuid()), ('lober', 1000), '所有动态检查只能在指定 VM')
        ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(dir=ROOT)
        self.addCleanup(self.temp.cleanup)
        self.output = Path(self.temp.name)
        self.host = FakeHost()
        self.trusted = {}

    def create(self, frozen=None, owner=None, target=None, host=None):
        return session.CaptureSession({}, OWNER if owner is None else owner, TARGET if target is None else target,
                                      contract() if frozen is None else frozen, self.output, self.host if host is None else host)

    def receipt(self, data=None, frozen=None):
        self.host.data = fixture.pcap(fixture.frames()) if data is None else data
        from cloud_packet_capture import parse_pcap
        try:self.host.capture_count=parse_pcap(self.host.data,fixture.contract(self.host.data))[1]
        except ValueError:self.host.capture_count=len(fixture.frames())
        value = self.create(frozen=frozen)
        value.start(); value.check()
        receipt = value.close()
        self.trusted[receipt['session_id']] = (copy.deepcopy(value.frozen), copy.deepcopy(value.target),
            {'contract_sha256':session.canonical(value.frozen), 'target_sha256':session.canonical(value.target),
             'receipt_sha256':session.canonical(receipt)})
        return value, receipt

    def analyze(self, receipt):
        frozen,target,anchor=self.trusted[receipt['session_id']]
        return session.analyze_session(receipt,frozen,target,anchor=anchor)

    def extract(self, receipt, reference):
        frozen,target,anchor=self.trusted[receipt['session_id']]
        return session.extract_verified_callback(receipt,frozen,target,reference,anchor=anchor)

    def test_lifecycle_freezes_private_original_and_only_packet_scope(self):
        value, receipt = self.receipt()
        self.assertEqual(self.host.calls, ['start', 'check', 'close'])
        self.assertEqual(value.close(), receipt)
        evidence = self.analyze(receipt)
        self.assertEqual(evidence['scope'], 'PACKET_EVIDENCE_ONLY')
        self.assertNotIn('PRIVATE-TOKEN', json.dumps(receipt))
        self.assertEqual(self.extract(receipt, evidence['http'][0]['reference'])['body'], b'yes')

    def test_wrong_vm_rejected(self):
        with patch.object(session.socket, 'gethostname', return_value='wrong'):
            with self.assertRaises(ValueError): self.create()

    def test_wrong_bridge_uuid_owner_and_target_digest_rejected(self):
        for field, wrong in [('bridge', 'virbr0'), ('uuid', '3a44e8be-e275-4298-9435-837fb455d1a3'), ('host', 'other'), ('candidate_port', 8080)]:
            with self.subTest(field=field):
                target = dict(TARGET, **{field: wrong})
                with self.assertRaises(ValueError): self.create(target=target)
        for key, wrong in [('purpose', 'other'), ('run_id', 'bad/run')]:
            with self.subTest(key=key):
                with self.assertRaises(ValueError): self.create(owner=dict(OWNER, **{key: wrong}))
        frozen = contract(); frozen['targets'][OWNER['target_uuid']]['target_sha256'] = '0'*64
        with self.assertRaises(ValueError): self.create(frozen=frozen)

    def test_contract_unknown_keys_or_posthoc_empty_boot_chain_rejected(self):
        for change in [lambda c: c.update(extra=True), lambda c: c.update(bridge='virbr0'),
                       lambda c: c.update(tftp_port_range=[1, 65535]), lambda c: c.update(max_bytes=128*1024**2),
                       lambda c: c.update(tcp_reset_policy='any-window'),
                       lambda c: c['targets'][OWNER['target_uuid']]['packet_contract'].update(tftp={}),
                       lambda c: c['targets'][OWNER['target_uuid']]['boot_chain'].update(required_tftp=[])]:
            with self.subTest(change=change):
                frozen = contract(); change(frozen)
                with self.assertRaises(ValueError): self.create(frozen=frozen)

    def test_two_explicit_targets_supported_without_assuming_first_uuid(self):
        frozen = contract()
        second = dict(TARGET, uuid='3a44e8be-e275-4298-9435-837fb455d1a3', mac='52:54:00:25:10:02', guest_ip='192.168.201.102')
        row = copy.deepcopy(frozen['targets'][OWNER['target_uuid']]); row['target_sha256'] = session.canonical(second)
        row['packet_contract'].update(client_mac=second['mac'], client_ip=second['guest_ip'])
        frozen['targets'][second['uuid']] = row
        value = self.create(frozen=frozen, owner=dict(OWNER, target_uuid=second['uuid']), target=second)
        self.assertIn('192.168.201.102', value.request['filter'])

    def test_no_binary_sink_or_public_directory_rejected(self):
        with self.assertRaises(ValueError): self.create(host=lambda *a, **k: '')
        self.output.chmod(0o755)
        with self.assertRaises(ValueError): self.create()

    def test_intent_record_failure_prevents_host_mutation(self):
        with patch.object(session, 'private_write', side_effect=OSError('disk full')):
            with self.assertRaises(OSError): self.create()
        self.assertFalse(self.host.calls)

    def test_start_is_one_shot_and_close_before_start_is_safe(self):
        value = self.create()
        self.assertEqual(value.close()['state'], 'NEVER_STARTED')
        value.start()
        with self.assertRaises(ValueError): value.start()

    def test_host_identity_filter_owner_or_uuid_mismatch_rejected(self):
        for key, wrong in [('filter', 'ip'), ('bridge', 'virbr0'), ('owner', {}), ('target_uuid', 'other'), ('session_id', 'other')]:
            with self.subTest(key=key):
                value = self.create(); self.host.change = lambda row, k=key, v=wrong: row.update(**{k:v})
                with self.assertRaises(ValueError): value.start()
                self.host.change = None

    def test_early_exit_timeout_capacity_and_unknown_stop_fail_closed(self):
        for reason in ['EARLY_EXIT', 'TIMEOUT', 'CAPACITY_EXCEEDED']:
            value = self.create(); value.start()
            self.host.change = lambda row, reason=reason: row.update(state='FAILED', reason=reason, stopped=True)
            with self.assertRaises(ValueError): value.check()
            with self.assertRaises(ValueError): value.close()
            self.host.change = None
        value = self.create(); value.start()
        self.host.change = lambda row: row.update(stopped=False)
        with self.assertRaisesRegex(ValueError, '禁止释放'): value.close()

    def test_close_record_write_failure_does_not_return_receipt(self):
        value = self.create(); value.start()
        with patch.object(session, 'private_write', side_effect=OSError('disk full')):
            with self.assertRaises(OSError): value.close()
        self.assertIsNone(value.receipt)

    def test_bad_sink_metadata_or_host_path_rejected(self):
        value = self.create(); value.start()
        self.host.sink_change = lambda row: row.update(sha256='0'*64)
        with self.assertRaises(ValueError): value.close()
        self.host.sink_change = None
        value = self.create(); value.start()
        self.host.change = lambda row: row.update(host_path='/old/capture.pcap')
        with self.assertRaises(ValueError): value.close()

    def test_tamper_exchange_symlink_permissions_or_drop_rejected(self):
        for mutation in ['bytes', 'exchange', 'symlink', 'mode', 'drop', 'count']:
            with self.subTest(mutation=mutation):
                _, receipt = self.receipt(); path = Path(receipt['path'])
                if mutation == 'bytes': path.write_bytes(path.read_bytes()[:-1]+b'x')
                if mutation == 'exchange':
                    data=path.read_bytes(); path.unlink(); path.write_bytes(data); path.chmod(0o600)
                if mutation == 'symlink':
                    target=path.with_suffix('.copy'); path.rename(target); path.symlink_to(target)
                if mutation == 'mode': path.chmod(0o644)
                if mutation == 'drop': receipt['counts']['dropped by kernel']=1
                if mutation == 'count': receipt['counts']['captured']+=1
                with self.assertRaises(ValueError): self.analyze(receipt)

    def test_no_dhcp_tftp_bad_http_wrong_task_truncation_rejected(self):
        cases = [fixture.frames()[4:], [p for p in fixture.frames() if not p[0].startswith(('rrq','oack','ack0','data','ack1','ack2'))],
                 fixture.frames(fixture.http_frames(res=fixture.response(500))),
                 fixture.frames(fixture.http_frames(req=fixture.request('/v1/signal/other/TOKEN/ready')))]
        for items in cases:
            with self.subTest(items=len(items)):
                value, receipt = self.receipt(fixture.pcap(items))
                receipt['counts']['captured']=len(items)
                with self.assertRaises(ValueError): self.analyze(receipt)
        _, receipt = self.receipt(fixture.pcap(fixture.frames())[:-1])
        with self.assertRaises(ValueError): self.analyze(receipt)

    def test_changed_parser_contract_or_partial_reference_cannot_extract(self):
        _, receipt = self.receipt()
        frozen,target,anchor=self.trusted[receipt['session_id']]
        changed=copy.deepcopy(frozen); changed['targets'][target['uuid']]['packet_contract']['task_id']='other'
        with self.assertRaises(ValueError): session.analyze_session(receipt, changed, target, anchor=anchor)
        with self.assertRaises(ValueError): self.extract(receipt, {'request_index':0})

    def test_process_reuse_or_changed_uid_exe_argv_rejected_in_main(self):
        for key, wrong in [('starttime','999'), ('uid',1000), ('exe','/bin/sh'), ('argv',['tcpdump','-i','any'])]:
            with self.subTest(key=key):
                value=self.create(); value.start()
                self.host.change=lambda row,key=key,wrong=wrong: row['process'].update(**{key:wrong})
                with self.assertRaises(ValueError): value.check()
                self.host.change=None

    def test_tftp_range_and_real_dhcp_branch_must_match_frozen_chain(self):
        data=fixture.pcap(fixture.frames()).replace(b'\x9cA', b'\x9c?')  # 40001 → 39999
        _,receipt=self.receipt(data)
        with self.assertRaisesRegex(ValueError,'端口'): self.analyze(receipt)
        _,receipt=self.receipt()
        receipt['boot_chain']['user_class_sha256']='0'*64
        with self.assertRaises(ValueError): self.analyze(receipt)

    def test_no_rst_policy_rejects_actual_rst_and_explicit_policy_is_required(self):
        items=fixture.frames()+[('rst',fixture.tcp(fixture.CLIENT_IP,fixture.SERVICE_IP,42000,18090,
            101+len(fixture.request()),501+len(fixture.response()),0x14))]
        _,receipt=self.receipt(fixture.pcap(items));receipt['counts']['captured']=len(items)
        with self.assertRaisesRegex(ValueError,'RST'):self.analyze(receipt)
        receipt['tcp_reset_policy']='rfc5961-exact-rcv-nxt'
        with self.assertRaises(ValueError):self.analyze(receipt)
        frozen=contract();frozen['tcp_reset_policy']='rfc5961-exact-rcv-nxt'
        _,pre_frozen_receipt=self.receipt(fixture.pcap(items),frozen=frozen)
        self.assertEqual(self.analyze(pre_frozen_receipt)['http'][0]['response_status'],200)

    def test_review_posthoc_rst_relaxation_must_be_rejected(self):
        items=fixture.frames()+[('rst',fixture.tcp(fixture.CLIENT_IP,fixture.SERVICE_IP,42000,18090,
            101+len(fixture.request()),501+len(fixture.response()),0x14))]
        _,receipt=self.receipt(fixture.pcap(items));receipt['counts']['captured']=len(items)
        receipt['tcp_reset_policy']='rfc5961-exact-rcv-nxt'
        with self.assertRaises(ValueError):self.analyze(receipt)

    def test_independent_anchor_rejects_posthoc_metadata_for_analysis_and_callback(self):
        _,original=self.receipt()
        reference=self.analyze(original)['http'][0]['reference']
        mutations={
            'firmware':lambda r:r['boot_chain'].update(firmware_sha256='c'*64),
            'dhcp-config':lambda r:r['boot_chain'].update(dhcp_config_sha256='c'*64),
            'user-class':lambda r:r['boot_chain'].update(user_class_sha256='c'*64),
            'bytes':lambda r:r.update(max_bytes=r['max_bytes']+1),
            'seconds':lambda r:r.update(max_seconds=r['max_seconds']+100),
            'window-start':lambda r:r.update(capture_start_ns=0),
            'window-end':lambda r:r.update(capture_end_ns=r['capture_end_ns']+1000000000),
            'target-sha':lambda r:r.update(target_sha256='c'*64),
            'target-uuid':lambda r:r.update(target_uuid='3a44e8be-e275-4298-9435-837fb455d1a3'),
            'owner':lambda r:r['owner'].update(run_id='other-run'),
            'contract-sha':lambda r:r.update(contract_sha256='c'*64),
            'reset':lambda r:r.update(tcp_reset_policy='rfc5961-exact-rcv-nxt'),
        }
        for name,change in mutations.items():
            with self.subTest(name=name):
                receipt=copy.deepcopy(original);change(receipt)
                with self.assertRaises(ValueError):self.analyze(receipt)
                with self.assertRaises(ValueError):self.extract(receipt,reference)

    def test_complete_contract_still_binds_fields_if_receipt_digest_is_reissued(self):
        _,original=self.receipt()
        frozen,target,anchor=self.trusted[original['session_id']]
        reference=self.analyze(original)['http'][0]['reference']
        for key,wrong in [('max_bytes',1024),('max_seconds',100),('tcp_reset_policy','rfc5961-exact-rcv-nxt'),
                          ('target_sha256','c'*64)]:
            with self.subTest(key=key):
                changed=copy.deepcopy(original);changed[key]=wrong
                reissued=dict(anchor,receipt_sha256=session.canonical(changed))
                with self.assertRaises(ValueError):session.analyze_session(changed,frozen,target,anchor=reissued)
                with self.assertRaises(ValueError):session.extract_verified_callback(changed,frozen,target,reference,anchor=reissued)
        for key in ['firmware_sha256','user_class_sha256']:
            changed=copy.deepcopy(original);changed['boot_chain'][key]='c'*64
            reissued=dict(anchor,receipt_sha256=session.canonical(changed))
            with self.assertRaises(ValueError):session.analyze_session(changed,frozen,target,anchor=reissued)

    def test_removing_one_of_two_required_tftp_or_http_entries_never_weakens_contract(self):
        for field in ['tftp','http']:
            with self.subTest(field=field):
                frozen=contract();row=frozen['targets'][OWNER['target_uuid']]
                if field=='tftp':
                    row['packet_contract']['tftp']['second-required.ipxe']={'size':1,'sha256':fixture.digest(b'x')}
                    row['boot_chain']['required_tftp']=sorted(row['packet_contract']['tftp'])
                else:
                    row['packet_contract']['http'].append({'name':'second-required','method':'GET','target_sha256':'e'*64,'status':200})
                _,original=self.receipt(frozen=frozen)
                with self.assertRaises(ValueError):self.analyze(original)
                changed=copy.deepcopy(original)
                if field=='tftp':
                    changed['packet_contract']['tftp'].pop('second-required.ipxe')
                    changed['boot_chain']['required_tftp']=sorted(changed['packet_contract']['tftp'])
                else:changed['packet_contract']['http'].pop()
                trusted,target,anchor=self.trusted[original['session_id']]
                reference={'capture_sha256':original['sha256'],'flow_id':'irrelevant','request_index':0,'method':'POST',
                           'target_sha256':fixture.digest(fixture.TARGET.encode()),'request_body_sha256':fixture.digest(b'yes')}
                with self.assertRaises(ValueError):self.analyze(changed)
                with self.assertRaises(ValueError):self.extract(changed,reference)
                reissued=dict(anchor,receipt_sha256=session.canonical(changed))
                with self.assertRaises(ValueError):session.analyze_session(changed,trusted,target,anchor=reissued)
                with self.assertRaises(ValueError):session.extract_verified_callback(changed,trusted,target,reference,anchor=reissued)

    def test_independent_contract_target_digest_and_anchor_are_required(self):
        _,receipt=self.receipt();frozen,target,anchor=self.trusted[receipt['session_id']]
        changed=copy.deepcopy(frozen);changed['max_seconds']+=10
        with self.assertRaises(ValueError):session.analyze_session(receipt,changed,target,anchor=anchor)
        wrong_target=dict(target,sn='other-target')
        with self.assertRaises(ValueError):session.analyze_session(receipt,frozen,wrong_target,anchor=anchor)
        with self.assertRaises(ValueError):session.analyze_session(receipt,frozen,target,anchor={})
        with self.assertRaises(TypeError):session.analyze_session(receipt,receipt['packet_contract'])

    def test_private_stream_rejects_original_exchange_and_never_logs_body(self):
        value=self.create()
        path=self.output/'host-fixture.pcap';path.write_bytes(self.host.data);path.chmod(0o600)
        terminal={'host_path':str(path),'file_identity':session.local_identity(path),'sha256':fixture.digest(self.host.data)}
        session.private_write(path.parent/'terminal.json',terminal)
        source=value._stream_script(terminal)[len(session.HOST_RUNTIME):]
        namespace={};exec(session.HOST_RUNTIME,namespace)
        namespace['context']=lambda r:None
        namespace['read']=lambda p:json.loads(p.read_text())
        namespace['file_id']=session.local_identity
        sink=io.BytesIO(); namespace['sys']=types.SimpleNamespace(stdout=types.SimpleNamespace(buffer=sink))
        exec(source,namespace)
        self.assertEqual(sink.getvalue(),self.host.data)
        class Swap(io.BytesIO):
            def write(sink,data):
                old=path.with_suffix('.old');path.rename(old);path.write_bytes(data);path.chmod(0o600)
                return super().write(data)
        namespace['sys']=types.SimpleNamespace(stdout=types.SimpleNamespace(buffer=Swap()))
        with self.assertRaisesRegex(RuntimeError,'exchanged'):exec(source,namespace)

    def test_filter_keeps_broadcast_offer_limits_ports_and_omits_large_artifact(self):
        text=session.capture_filter(contract()['targets'][OWNER['target_uuid']]['packet_contract'])
        self.assertIn('udp[36:4] = 0x52540025', text)
        self.assertIn('udp[40:2] = 0x1001', text)
        self.assertIn('portrange 40000-40010', text)
        self.assertNotIn('ether host', text)
        self.assertNotIn('18082', text)

    def test_generated_sources_compile_without_host_execution(self):
        value=self.create()
        for action in ['start','check','close']: compile(value.script(action), '<host-review-only>', 'exec')
        compile(session.HOST_RUNTIME, '<runtime-review-only>', 'exec')

    def test_host_runtime_forces_private_umask_before_tcpdump_spawn(self):
        original=os.umask(0o022)
        try:
            namespace={};exec(session.HOST_RUNTIME,namespace)
            actual=os.umask(0o077)
            self.assertEqual(actual,0o077)
        finally:os.umask(original)

    def test_actual_short_process_identity_detects_exit_and_cannot_reuse_identity(self):
        namespace={}; exec(session.HOST_RUNTIME, namespace)
        child=subprocess.Popen([sys.executable,'-c','import time; time.sleep(10)'])
        try:
            actual=namespace['identity'](child.pid)
            self.assertTrue(namespace['same'](actual))
            wrong=dict(actual,starttime=str(int(actual['starttime'])+1))
            self.assertFalse(namespace['same'](wrong))
        finally: child.terminate(); child.wait(timeout=5)
        self.assertFalse(namespace['same'](actual))


class HostSupervisorChecks(unittest.TestCase):
    def setUp(self):
        self.assertEqual((socket.gethostname(), os.geteuid()), ('lober', 1000))
        ROOT.mkdir(mode=0o700,parents=True,exist_ok=True)
        self.temp=tempfile.TemporaryDirectory(dir=ROOT); self.addCleanup(self.temp.cleanup)
        self.folder=Path(self.temp.name)
        self.namespace={}; exec(session.HOST_RUNTIME,self.namespace)
        self.namespace['context']=lambda r: self.folder
        self.namespace['read']=lambda p: json.loads(p.read_text())
        self.namespace['file_id']=lambda p: {'device':p.stat().st_dev,'inode':p.stat().st_ino,'mtime_ns':p.stat().st_mtime_ns,'size':p.stat().st_size}

    def run_supervisor(self, mutation=None):
        folder=self.folder
        request={'base':str(folder),'folder':str(folder),'owner':OWNER,'target':TARGET,'packet':contract()['targets'][OWNER['target_uuid']]['packet_contract'],
                 'filter':session.capture_filter(contract()['targets'][OWNER['target_uuid']]['packet_contract']), 'max_bytes':100000,'max_seconds':60,'session_id':'fixture-only'}
        ident=process(str(folder)); calls=[]
        namespace=self.namespace
        class Child:
            pid=12345
            returncode=None
            polls=0
            def poll(child):
                child.polls+=1
                if child.polls==3: session.private_write(folder/'stop-request.json',{'session_id':request['session_id'],'owner':OWNER})
                if mutation in {'early','identity-exited'}: child.returncode=1
                return child.returncode
            def wait(child,timeout):
                if mutation=='unknown': raise subprocess.TimeoutExpired('fixture',timeout)
                child.returncode=0
                return 0
        def popen(argv,**kw):
            calls.append(argv)
            (folder/'capture.pcap').write_bytes(fixture.pcap(fixture.frames()))
            counts='1 packets captured\n1 packets received by filter\n'+('1' if mutation=='drop' else '0')+' packets dropped by kernel\n'
            if mutation=='interface-drop':counts+='1 packets dropped by interface\n'
            if mutation=='no-statistics':counts=''
            kw['stderr'].write(('tcpdump: listening on virbr-pq25, link-type EN10MB (Ethernet)\n'+counts).encode())
            if mutation=='rotation': (folder/'capture.pcap1').write_bytes(b'other')
            if mutation=='capacity': request['max_bytes']=24
            if mutation=='timeout':request['max_seconds']=0
            return Child()
        namespace['identity']=lambda pid: ident
        if mutation in {'identity-live','identity-exited'}:
            def identity_failure(pid):raise PermissionError('fixture first identity read failed')
            namespace['identity']=identity_failure
        namespace['same']=lambda record: mutation!='reuse'
        real_write=namespace['write']
        if mutation=='record':
            def fail_record(path,value):
                if path.name=='process.json':raise OSError('fixture full')
                real_write(path,value)
            namespace['write']=fail_record
        with patch.object(subprocess,'Popen',side_effect=popen), patch.object(os,'pidfd_open',side_effect=lambda pid:os.open('/dev/null',os.O_RDONLY)), patch.object(namespace['signal'],'pidfd_send_signal') as sent:
            namespace['supervise'](request)
            signal_count=sent.call_count
        return json.loads((folder/'terminal.json').read_text()),calls,signal_count

    def test_supervisor_normal_sigint_exact_process_freezes_after_wait(self):
        result,calls,count=self.run_supervisor()
        self.assertEqual((result['state'],result['stopped'],count),('FROZEN',True,1))
        self.assertEqual(calls[0][calls[0].index('-i')+1],session.BRIDGE)
        self.assertEqual(result['process']['starttime'],'701')

    def test_supervisor_rejects_early_exit_rotation_drops_capacity_and_record_error(self):
        for mutation in ['early','rotation','drop','interface-drop','no-statistics','capacity','timeout','record']:
            with self.subTest(mutation=mutation):
                # 每次使用独立临时目录，保留父目录上的测试结果至结束。
                with tempfile.TemporaryDirectory(dir=ROOT) as directory:
                    self.folder=Path(directory)
                    result,_,_=self.run_supervisor(mutation)
                    self.assertEqual(result['state'],'FAILED')

    def test_supervisor_pid_reuse_never_signals_and_stop_unknown_never_marks_safe(self):
        result,_,count=self.run_supervisor('reuse')
        self.assertEqual((result['state'],result['stopped'],count),('FAILED',False,0))

    def test_supervisor_wait_timeout_keeps_unknown_stop(self):
        result,_,_=self.run_supervisor('unknown')
        self.assertEqual((result['state'],result['stopped']),('FAILED',False))

    def test_review_first_identity_failure_live_child_stays_unknown(self):
        result,_,count=self.run_supervisor('identity-live')
        self.assertEqual((result['state'],result['stopped'],count),('FAILED',False,0))

    def test_review_first_identity_failure_exited_child_can_confirm_stop(self):
        result,_,count=self.run_supervisor('identity-exited')
        self.assertEqual((result['state'],result['stopped'],count),('FAILED',True,0))


if __name__=='__main__': unittest.main()
