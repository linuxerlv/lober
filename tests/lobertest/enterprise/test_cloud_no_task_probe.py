"""只在lober VM运行人工原件；替身通过不代表真实短流程。"""
import copy
import hashlib
import importlib
import json
import os
from pathlib import Path
import socket
import tempfile
import unittest
from unittest.mock import patch

import cloud_packet_capture as parser
import cloud_capture_session as session
import test_cloud_packet_capture as packets

try:
    probe = importlib.import_module('cloud_no_task_probe')
except ModuleNotFoundError:
    probe = None

ROOT = Path('/home/dev/lober-production-qualification-20261025/machines/cloud-no-task-probe')
TARGET = {'host': 'ser866667880745', 'name': 'lober-pq25-linux01', 'uuid': '7fe71ebb-a34d-4155-8410-d1427ca2f83a',
    'sn': 'PQ25L01', 'mac': packets.CLIENT, 'cpus': 2, 'memory_mib': 4096,
    'disk_path': '/var/lib/libvirt/images/lober-pq-20261025/linux01.qcow2', 'disk_serial': 'PQ25_LINUX01_DISK', 'disk_bytes': 16 * 1024**3,
    'data_path': '/var/lib/libvirt/images/lober-pq-20261025/media01.qcow2', 'data_serial': 'PQ25_MEDIA01_DISK', 'data_bytes': 16 * 1024**3,
    'service_uuid': '895a44e1-8abd-4463-bbf7-fba8a743ed59', 'service_mac': packets.SERVICE,
    'network': 'lober-pq25', 'bridge': 'virbr-pq25', 'subnet': '192.168.201.0/24',
    'host_ip': packets.BOOT_IP, 'service_ip': packets.SERVICE_IP, 'guest_ip': packets.CLIENT_IP,
    'candidate_port': 18090, 'artifact_port': 18082}
OWNER = {'purpose': 'production-qualification-20261025', 'run_id': 'no-task-unit-fixture', 'target_uuid': TARGET['uuid']}


def canonical(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(',', ':')).encode()).hexdigest()


def xml():
    return ('<domain type="kvm"><name>' + TARGET['name'] + '</name><uuid>' + TARGET['uuid'] + '</uuid>'
        '<sysinfo type="smbios"><system><entry name="serial">PQ25L01</entry></system></sysinfo>'
        '<os><type arch="x86_64">hvm</type></os><vcpu>2</vcpu><memory unit="MiB">4096</memory><devices>'
        '<disk type="file" device="disk"><driver type="qcow2"/><source file="' + TARGET['disk_path'] + '"/>'
        '<target dev="vda" bus="virtio"/><serial>' + TARGET['disk_serial'] + '</serial><boot order="2"/></disk>'
        '<interface type="network"><mac address="' + TARGET['mac'] + '"/><source network="lober-pq25"/>'
        '<model type="e1000"/><boot order="1"/></interface></devices></domain>')


class ProbeChecks(unittest.TestCase):
    def setUp(self):
        self.assertEqual((socket.gethostname(), os.geteuid()), ('lober', 1000))
        ROOT.mkdir(mode=0o700, parents=True, exist_ok=True)
        self.temp = tempfile.TemporaryDirectory(dir=ROOT); self.addCleanup(self.temp.cleanup)
        self.folder = Path(self.temp.name); self.sequence = 0
        self.contract, self.receipt = self.build()

    def raw(self, data):
        self.sequence += 1
        path = self.folder / ('original-' + str(self.sequence))
        path.write_bytes(data); path.chmod(0o600)
        return {'path': str(path), 'bytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}

    def record(self, value):
        return self.raw(json.dumps(value, sort_keys=True).encode())

    def context(self, contract):
        return {'owner': OWNER, 'target_sha256': canonical(TARGET), 'bindings': contract['bindings']}

    def build(self):
        data = packets.pcap(packets.frames(packets.http_frames(req=packets.no_task_request(), res=packets.response(body=packets.LOCAL_BOOT))))
        pcap = self.raw(data)
        packet = packets.no_task_contract(data)
        for key in ('pcap_sha256', 'pcap_size', 'capture_start_ns', 'capture_end_ns'):
            packet.pop(key)
        capture = {'schema': 'lober-cloud-capture-session-v1', 'host': TARGET['host'], 'bridge': TARGET['bridge'], 'network': TARGET['network'],
            'targets': {TARGET['uuid']: {'packet_mode': 'NO_TASK', 'target_sha256': canonical(TARGET), 'packet_contract': packet,
                'boot_chain': {'firmware_sha256': 'a' * 64, 'dhcp_config_sha256': 'b' * 64, 'user_class_sha256': packets.digest(b''),
                               'boot_file': 'pq25.ipxe', 'required_tftp': ['pq25.ipxe']}}},
            'tftp_port_range': [40000, 40010], 'max_bytes': 64 * 1024**2, 'max_seconds': 60, 'tcp_reset_policy': 'no-rst'}
        resource = {'schema': 'lober-cloud-target-resource-v1', 'owner': OWNER, 'target': TARGET,
                    'review': {'approved': True, 'root_reviewer': 'fixture-root', 'independent_reviewer': 'fixture-reviewer'},
                    'executes_mutation': False, 'failed_disk_delete_or_reuse_allowed': False}
        runtime = self.record({'fixture_only': 'runtime'}); instance = self.record({'fixture_only': 'instance'})
        start_source = self.raw(b'# artificial start source; never executed\n')
        stop_source = self.raw(b'# artificial stop source; never executed\n')
        contract = {'schema': 'lober-cloud-no-task-probe-v1', 'scope': 'NO_TASK', 'owner': OWNER, 'target': TARGET,
            'bindings': {'product_sha': 'a' * 40, 'test_sha': 'b' * 40, 'binary_sha256': 'c' * 64,
                         'runtime_record_sha256': runtime['sha256'], 'instance_record_sha256': instance['sha256'],
                         'target_contract_sha256': canonical(resource)},
            'checkers': {'probe_sha256': packets.digest(Path(probe.__file__).read_bytes()) if probe else '0' * 64,
                         'parser_sha256': packets.digest(Path(parser.__file__).read_bytes()),
                         'capture_session_sha256': packets.digest(Path(session.__file__).read_bytes())},
            'capture_contract': capture, 'task_policy': 'TOTAL_TASKS_ZERO', 'max_seconds': 120,
            'operations': {'start_source_sha256': start_source['sha256'], 'stop_source_sha256': stop_source['sha256']}}
        context = self.context(contract)
        def api(start, finish):
            return {'schema': 'lober-no-task-api-v1', 'context': context, 'started_ns': start, 'finished_ns': finish,
                    'requests': [{'method': 'GET', 'path': path, 'status': 200,
                                  'body': self.record({'code': 0, 'data': {'items': [], 'next': '', 'total': 0}, 'message': ''})}
                                 for path in ('/console/v1/tasks?page=1&page_size=100', '/console/v1/tasks?host_sn=PQ25L01&page=1&page_size=100')]}
        def disk(at):
            return {'schema': 'lober-no-task-disk-v1', 'context': context, 'at_ns': at, 'domain_state': 'shut off',
                    'source_identity': {'device': 1, 'inode': 2}, 'owner': self.record(OWNER), 'domain_xml': self.raw(xml().encode()),
                    'info': self.record({'filename': TARGET['disk_path'], 'format': 'qcow2', 'virtual-size': TARGET['disk_bytes']}),
                    'map': self.record([{'start': 0, 'length': TARGET['disk_bytes'], 'zero': True, 'data': False, 'depth': 0}])}
        def action(kind, start, finish, source):
            facts = {'owner': OWNER, 'target': TARGET}
            if kind == 'start':
                facts.update(started=True, state='running', network_boot_first=True)
            else:
                facts.update(client_stopped=True, state='shut off', failed_disks_preserved=True, qemu_gone=True)
            return {'schema': 'lober-no-task-action-v1', 'context': context, 'action': kind, 'started_ns': start, 'finished_ns': finish,
                    'command': {'source': source, 'stdout': self.record(facts), 'stderr': self.raw(b''), 'exit': 0, 'timed_out': False}}
        capture_receipt = {'scope': 'CAPTURE_LIFECYCLE_ONLY', 'owner': OWNER, 'target_uuid': TARGET['uuid'],
            'target_sha256': canonical(TARGET), 'contract_sha256': canonical(capture), 'path': pcap['path'], 'sha256': pcap['sha256'],
            'size': pcap['bytes'], 'capture_start_ns': 500_000_000, 'capture_end_ns': 1_300_000_000, 'packet_contract': packet}
        capture_anchor = {'contract_sha256': canonical(capture), 'target_sha256': canonical(TARGET), 'receipt_sha256': canonical(capture_receipt)}
        records = {'target_contract': self.record(resource), 'runtime_record': runtime, 'instance_record': instance,
            'api_before': self.record(api(100, 200)), 'api_after': self.record(api(1_500_000_000, 1_600_000_000)),
            'disk_before': self.record(disk(300)), 'disk_after': self.record(disk(1_400_000_000)),
            'client_start': self.record(action('start', 600_000_000, 650_000_000, start_source)),
            'client_stop': self.record(action('stop', 1_100_000_000, 1_200_000_000, stop_source)),
            'capture_receipt': self.record(capture_receipt), 'capture_anchor': self.record(capture_anchor)}
        receipt = {'schema': 'lober-cloud-no-task-probe-receipt-v1', 'owner': OWNER, 'contract_sha256': canonical(contract),
            'status': 'EXPECTED_FAILURE', 'failure_kind': 'no_active_task_local_boot_on_blank_disk', 'synthetic': False,
            'root_verified': False, 'no_task_created': True, 'client_stopped': True, 'failed_disks_preserved': True, 'records': records}
        return contract, receipt

    def replace(self, name, change):
        value = json.loads(Path(self.receipt['records'][name]['path']).read_text())
        change(value)
        self.receipt['records'][name] = self.record(value)

    def analyze_capture(self, receipt, frozen, target, *, anchor):
        self.assertEqual((frozen, target), (self.contract['capture_contract'], TARGET))
        self.assertEqual(anchor['receipt_sha256'], canonical(receipt))
        packet = dict(receipt['packet_contract'], pcap_sha256=receipt['sha256'], pcap_size=receipt['size'],
                      capture_start_ns=receipt['capture_start_ns'], capture_end_ns=receipt['capture_end_ns'])
        return parser.analyze_no_task_capture(receipt['path'], packet)

    def verify(self, anchor=None):
        self.assertIsNotNone(probe, '缺少无任务短流程校验器')
        anchor = anchor or {'contract_sha256': canonical(self.contract), 'receipt_sha256': canonical(self.receipt)}
        with patch.object(session, 'analyze_session', side_effect=self.analyze_capture) as checker:
            value = probe.verify_no_task_probe(self.receipt, self.contract, anchor=anchor)
            self.assertEqual(checker.call_count, 1)
            return value

    def reject(self):
        self.assertIsNotNone(probe, '缺少无任务短流程校验器')
        with self.assertRaises(ValueError):
            self.verify()

    def test_prepare_is_explicit_proposal_and_never_sets_task_gate(self):
        self.assertIsNotNone(probe, '缺少无任务短流程准备器')
        value = probe.prepare_no_task_probe(self.contract)
        self.assertEqual(value['status'], 'PROPOSAL_ONLY')
        self.assertIs(value['executes_mutation'], False)
        self.assertIs(value['short_failure_evidence_verified'], False)

    def test_full_fixture_checks_still_require_independent_root_review(self):
        value = self.verify()
        self.assertEqual(value['scope'], 'NO_TASK_PROBE_EVIDENCE_ONLY')
        self.assertEqual(value['status'], 'ROOT_REVIEW_REQUIRED')
        self.assertIs(value['short_failure_evidence_verified'], False)
        self.assertIs(value['root_verified'], False)
        self.assertNotIn(packets.NO_TASK_TARGET, str(value))

    def test_equivalent_repeated_boots_are_all_counted_without_opening_task_gate(self):
        data = packets.pcap(packets.frames(packets.http_frames(req=packets.no_task_request() * 2,
                                                              res=packets.response(body=packets.LOCAL_BOOT) * 2)))
        pcap = self.raw(data)
        capture = json.loads(Path(self.receipt['records']['capture_receipt']['path']).read_text())
        capture.update(path=pcap['path'], size=pcap['bytes'], sha256=pcap['sha256'])
        self.receipt['records']['capture_receipt'] = self.record(capture)
        self.receipt['records']['capture_anchor'] = self.record({'contract_sha256': canonical(self.contract['capture_contract']),
            'target_sha256': canonical(TARGET), 'receipt_sha256': canonical(capture)})
        value = self.verify()
        self.assertEqual(value['boot_exchange_count'], 2)
        self.assertIs(value['short_failure_evidence_verified'], False)

    def test_synthetic_unknown_stop_or_premature_root_flags_cannot_pass(self):
        for key, value in (('synthetic', True), ('client_stopped', False), ('root_verified', True), ('no_task_created', False)):
            saved = copy.deepcopy(self.receipt)
            with self.subTest(key=key):
                self.receipt[key] = value; self.reject()
            self.receipt = saved

    def test_independent_anchor_rejects_changed_receipt_or_contract(self):
        frozen = {'contract_sha256': canonical(self.contract), 'receipt_sha256': canonical(self.receipt)}
        self.receipt['status'] = 'PASS'
        self.assertIsNotNone(probe, '缺少无任务短流程校验器')
        with self.assertRaises(ValueError):
            self.verify(anchor=frozen)

    def test_no_task_api_requires_global_and_exact_sn_empty_complete_originals(self):
        for payload in ({'items': [{'id': 'created-then-cancelled'}], 'next': '', 'total': 1},
                        {'items': [], 'next': 'more', 'total': 0}, {'items': [], 'next': '', 'total': 1}):
            saved = copy.deepcopy(self.receipt)
            self.replace('api_after', lambda v: v['requests'][0].update(body=self.record({'code': 0, 'data': payload, 'message': ''})))
            self.reject(); self.receipt = saved

    def test_wrong_api_context_and_forged_http_status_are_rejected(self):
        self.replace('api_before', lambda v: v['context'].update(target_sha256='0' * 64)); self.reject()

    def test_disk_map_must_cover_all_virtual_bytes_and_remain_zero(self):
        for extents in ([{'start': 0, 'length': TARGET['disk_bytes'] - 1, 'zero': True, 'data': False, 'depth': 0}],
                        [{'start': 0, 'length': TARGET['disk_bytes'], 'zero': False, 'data': True, 'depth': 0}]):
            saved = copy.deepcopy(self.receipt)
            self.replace('disk_after', lambda v: v.update(map=self.record(extents)))
            self.reject(); self.receipt = saved

    def test_disk_backing_or_swapped_source_or_serial_is_rejected(self):
        for change in (lambda v: v.update(source_identity={'device': 1, 'inode': 99}),
                       lambda v: v.update(info=self.record({'format': 'qcow2', 'filename': TARGET['disk_path'], 'virtual-size': TARGET['disk_bytes'], 'backing-filename': '/old'})),
                       lambda v: v.update(domain_xml=self.raw(xml().replace(TARGET['disk_serial'], 'WRONG').encode()))):
            saved = copy.deepcopy(self.receipt); self.replace('disk_after', change); self.reject(); self.receipt = saved

    def test_network_boot_and_bios_are_verified_from_xml(self):
        self.replace('disk_before', lambda v: v.update(domain_xml=self.raw(xml().replace('<boot order="1"/>', '<boot order="3"/>').encode())))
        self.reject()

    def test_stop_raw_stdout_and_source_hash_are_bound(self):
        self.replace('client_stop', lambda v: v['command'].update(stdout=self.record({'owner': OWNER, 'target': TARGET, 'client_stopped': True,
                       'state': 'shut off', 'failed_disks_preserved': True, 'qemu_gone': False})))
        self.reject()

    def test_timeout_or_nonzero_start_exit_cannot_be_expected_failure(self):
        for change in (lambda v: v['command'].update(timed_out=True), lambda v: v['command'].update(exit=1)):
            saved = copy.deepcopy(self.receipt); self.replace('client_start', change); self.reject(); self.receipt = saved

    def test_disk_after_cannot_be_observed_before_stop(self):
        self.replace('disk_after', lambda v: v.update(at_ns=1_050_000_000)); self.reject()

    def set_capture_end(self, end):
        capture = json.loads(Path(self.receipt['records']['capture_receipt']['path']).read_text())
        capture['capture_end_ns'] = end
        self.receipt['records']['capture_receipt'] = self.record(capture)
        self.receipt['records']['capture_anchor'] = self.record({'contract_sha256': canonical(self.contract['capture_contract']),
            'target_sha256': canonical(TARGET), 'receipt_sha256': canonical(capture)})

    def test_capture_cannot_end_before_or_during_client_stop(self):
        # 两个截止时间都晚于全部人工包，拒绝必须来自停机覆盖而非坏摘要/截包。
        for end in (1_050_000_000, 1_150_000_000):
            saved = copy.deepcopy(self.receipt)
            with self.subTest(capture_end=end):
                self.set_capture_end(end)
                self.reject()
            self.receipt = saved

    def test_capture_can_end_exactly_when_stop_finishes_or_later(self):
        for end in (1_200_000_000, 1_300_000_000):
            saved = copy.deepcopy(self.receipt)
            with self.subTest(capture_end=end):
                self.set_capture_end(end)
                value = self.verify()
                self.assertEqual(value['status'], 'ROOT_REVIEW_REQUIRED')
                self.assertIs(value['short_failure_evidence_verified'], False)
            self.receipt = saved

    def test_frozen_original_changes_are_rejected(self):
        ref = self.receipt['records']['api_before']; Path(ref['path']).write_bytes(b'{}'); self.reject()

    def test_checker_hash_and_target_approval_are_not_self_selected(self):
        self.contract['checkers']['parser_sha256'] = '0' * 64
        self.receipt['contract_sha256'] = canonical(self.contract); self.reject()

    def test_nested_raw_map_cannot_change_after_it_was_read(self):
        self.assertIsNotNone(probe, '缺少无任务短流程校验器')
        disk = json.loads(Path(self.receipt['records']['disk_after']['path']).read_text())
        victim = disk['map']['path']; original = probe._bound; changed = False
        def racing(ref, **kwargs):
            nonlocal changed
            data = original(ref, **kwargs)
            if ref['path'] == victim and not changed:
                changed = True; Path(victim).write_bytes(b'[]')
            return data
        with patch.object(probe, '_bound', side_effect=racing):
            self.reject()

    def test_xml_source_cannot_hide_an_extra_device_or_bridge(self):
        for raw in (xml().replace('file="' + TARGET['disk_path'] + '"', 'file="' + TARGET['disk_path'] + '" dev="/dev/evil"'),
                    xml().replace('network="lober-pq25"', 'network="lober-pq25" bridge="virbr0"')):
            saved = copy.deepcopy(self.receipt)
            self.replace('disk_after', lambda v: v.update(domain_xml=self.raw(raw.encode())))
            self.reject(); self.receipt = saved

    def test_session_dispatch_result_still_must_match_exact_frozen_boot_body(self):
        self.assertIsNotNone(probe, '缺少无任务短流程校验器')
        def wrong(*args, **kwargs):
            value = self.analyze_capture(*args, **kwargs)
            value['http'][0]['response_body_sha256'] = '0' * 64
            return value
        anchor = {'contract_sha256': canonical(self.contract), 'receipt_sha256': canonical(self.receipt)}
        with patch.object(session, 'analyze_session', side_effect=wrong), self.assertRaises(ValueError):
            probe.verify_no_task_probe(self.receipt, self.contract, anchor=anchor)

    def test_wrong_api_status_and_cursor_mode_cannot_replace_total_zero(self):
        for change in (lambda v: v['requests'][0].update(status=500),
                       lambda v: v['requests'][0].update(path='/console/v1/tasks?limit=100')):
            saved = copy.deepcopy(self.receipt); self.replace('api_before', change); self.reject(); self.receipt = saved

    def test_action_source_or_error_output_cannot_change(self):
        for change in (lambda v: v['command'].update(source=self.raw(b'different stop source')),
                       lambda v: v['command'].update(stderr=self.raw(b'failed'))):
            saved = copy.deepcopy(self.receipt); self.replace('client_stop', change); self.reject(); self.receipt = saved

    def test_unapproved_target_or_wrong_runtime_original_is_rejected(self):
        for change in (lambda v: v['review'].update(approved=False), lambda v: v['review'].update(independent_reviewer='fixture-root')):
            saved = copy.deepcopy(self.receipt)
            self.replace('target_contract', change); self.reject(); self.receipt = saved
        self.receipt['records']['runtime_record'] = self.record({'other_instance': True}); self.reject()

    def test_symlink_or_public_nested_original_is_rejected(self):
        disk = json.loads(Path(self.receipt['records']['disk_after']['path']).read_text())
        path = Path(disk['map']['path']); path.chmod(0o644); self.reject(); path.chmod(0o600)
        alias = path.parent / 'alias'; alias.symlink_to(path)
        self.replace('disk_after', lambda v: v['map'].update(path=str(alias))); self.reject()


if __name__ == '__main__':
    unittest.main()
