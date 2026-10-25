"""检查器自检；合成输入只验证拒绝条件，不是容量或装机资格。仅在 lober VM 运行。"""
import copy
import json
import os
from pathlib import Path
import tempfile
import threading
import types
import unittest
from unittest.mock import Mock, patch

import production_capacity as cap


class CapacityCheckerTests(unittest.TestCase):
    def setUp(self):
        self.profile = copy.deepcopy(cap.FORMAL_PROFILE)
        self.identity = {k: 'a' * 64 for k in ('manifest_sha256', 'profile_sha256', 'executor_sha256')}
        self.identity.update(run_id='checker-only', product_sha='b'*40, test_sha='c'*40)
        self.row = {**self.identity, 'phase': 'soak', 'mode': 'qualification',
                    'status': 'COMPLETED', 'reason': 'duration_elapsed', 'elapsed_seconds': 86400,
                    'requested_seconds': 86400, 'interrupted': False, 'sampling_errors': 0,
                    'worker_errors': 0, 'unique_sessions': 20, 'startup_seconds': 1,
                    'dataset': {'hosts': 10000, 'historical_tasks': 100000, 'violations': 0},
                    'download_baseline': {'single_verified': 1, 'parallel_verified': 4},
                    'invariants_before': {'violations': 0, 'snapshot_digest': 'd'*64},
                    'invariants_after': {'violations': 0, 'snapshot_digest': 'd'*64},
                    'owner_verified': True, 'process_identity_verified': True,
                    'artifact_size': 128*1024*1024, 'artifact_sha256': 'e'*64}
        self.samples = [self.sample(t) for t in range(0, 86401, 60)]
        self.requests = cap.request_summary([{'category': c, 'seconds': .05, 'ok': True,
                          'server_error': False, 'elapsed': i, 'session': i % 20}
                          for i, c in enumerate(cap.CATEGORIES)])
        # 仅检查器合成摘要；不写capacity_soak结果，也不冒充真实请求。
        for c in cap.READ_CATEGORIES:
            self.requests['categories'][c].update(count=500000, successes=500000)
        self.requests['count'] = 2000000
        self.downloads = [{'verified': True, 'bytes': self.row['artifact_size'], 'sha256': 'e'*64,
                           'seconds': 20, 'elapsed': 25, 'worker': i, 'kind': 'load'} for i in range(4)]
        self.downloads += [{**d, 'kind': 'parallel_baseline'} for d in self.downloads]
        self.downloads.append({**self.downloads[0], 'kind': 'single_baseline'})

    def sample(self, t):
        p = {'rss_bytes': 100000, 'fds': 20, 'connections': 10, 'alive': True,
             'identity_ok': True, 'cpu_ticks': 5, 'io': {}, 'swap_bytes': 0}
        return {'elapsed': t, 'sessions': 20, 'streams': 50, 'downloads': 4,
                'download_bytes': 1024*t, 'sample_errors': [], 'oom_kill': 0,
                'processes': {'product': copy.deepcopy(p), 'database': copy.deepcopy(p),
                              'driver': copy.deepcopy(p)}, 'root_free_bytes': 2**31,
                'data_free_bytes': 3*2**30, 'mem_available_bytes': 2**30,
                'swap_free_bytes': 2**30, 'db_locks_waiting': 0, 'queue': 0}

    def check(self):
        return cap.check_phase(self.profile, self.identity, self.row, self.samples,
                               self.requests, self.downloads)

    def test_checker_only_complete_synthetic_input(self):
        self.assertEqual(self.check(), [])

    def test_reject_short_soak(self):
        self.row['elapsed_seconds'] = 86399
        self.assertTrue(self.check())

    def test_reject_gap_order_and_missing_tail(self):
        for variant in (self.samples[:4] + self.samples[5:], list(reversed(self.samples)), self.samples[:-1]):
            with self.subTest():
                original, self.samples = self.samples, variant
                self.assertTrue(self.check())
                self.samples = original

    def test_reject_missing_client_classes(self):
        for key in ('sessions', 'streams', 'downloads'):
            with self.subTest(key=key):
                self.samples[3][key] = 0
                self.assertTrue(self.check())
                self.samples[3][key] = {'sessions': 20, 'streams': 50, 'downloads': 4}[key]

    def test_reject_missing_category_or_zero_denominator(self):
        self.requests['count'] = 0
        self.assertTrue(self.check())
        self.requests['count'] = 2000000
        self.requests['categories']['idle_observation']['successes'] = 0
        self.assertTrue(self.check())

    def test_reject_head_only_and_wrong_download_size(self):
        self.downloads = []
        self.assertTrue(self.check())
        self.downloads = [{'verified': True, 'bytes': 1, 'worker': 0, 'kind': 'load', 'seconds': 1}]
        self.assertTrue(self.check())

    def test_reject_small_dataset(self):
        self.row['dataset']['historical_tasks'] = 99999
        self.assertTrue(self.check())

    def test_reject_changed_profile_or_identity(self):
        self.profile['soak_seconds'] = 10
        self.assertTrue(self.check())
        self.profile = copy.deepcopy(cap.FORMAL_PROFILE)
        self.row['profile_sha256'] = 'f'*64
        self.assertTrue(self.check())

    def test_reject_wrong_owner_or_process_identity(self):
        self.row['owner_verified'] = False
        self.assertTrue(self.check())
        self.row['owner_verified'] = True
        self.samples[9]['processes']['product']['identity_ok'] = False
        self.assertTrue(self.check())

    def test_error_budget_strict_boundary(self):
        self.requests['server_errors'] = 2000
        self.assertTrue(self.check())
        self.requests['server_errors'] = 1999
        self.assertEqual(self.check(), [])

    def test_reject_latency_rss_fds_oom_and_low_disk(self):
        original = copy.deepcopy(self.samples)
        for mutation in ('rss', 'fds', 'oom', 'disk'):
            self.samples = copy.deepcopy(original)
            for s in self.samples[-31:]:
                if mutation == 'rss': s['processes']['product']['rss_bytes'] *= 2
                if mutation == 'fds': s['processes']['product']['fds'] += 100
                if mutation == 'oom': s['oom_kill'] = 1
                if mutation == 'disk': s['root_free_bytes'] = 10
            with self.subTest(mutation=mutation): self.assertTrue(self.check())
        self.samples = original
        for c in cap.READ_CATEGORIES: self.requests['categories'][c]['p95'] = 2
        self.assertTrue(self.check())

    def test_reject_few_calls_over_24h(self):
        for c in cap.READ_CATEGORIES: self.requests['categories'][c]['count'] = 1
        self.assertTrue(self.check())

    def test_streaming_request_and_download_aggregation(self):
        rows = ({'category': 'host_list', 'seconds': n/1000, 'ok': True, 'server_error': False}
                for n in range(1, 1001))
        stats = cap.request_summary(rows)
        self.assertEqual(stats['count'], 1000)
        self.assertEqual(stats['categories']['host_list']['p95'], .95)
        got = cap.download_summary(iter(self.downloads), self.row['artifact_size'], self.row['artifact_sha256'])
        self.assertEqual(got['verified_workers'], 4)
        self.assertEqual(got['parallel_verified'], 4)
        self.assertEqual(got['single_bytes_per_second'], self.row['artifact_size']/20)

    def test_reject_interrupted_running_probe_and_worker_loss(self):
        for key, value in (('interrupted', True), ('status', 'RUNNING'), ('mode', 'probe'),
                           ('worker_errors', 1), ('reason', 'interrupted')):
            original = self.row[key]
            self.row[key] = value
            with self.subTest(key=key): self.assertTrue(self.check())
            self.row[key] = original

    def test_snapshot_mutation_rejected(self):
        self.row['invariants_after']['snapshot_digest'] = 'e'*64
        self.assertTrue(self.check())

    def test_formal_profile_cannot_remove_or_relax_keys(self):
        for key in cap.FORMAL_PROFILE:
            p = copy.deepcopy(cap.FORMAL_PROFILE)
            del p[key]
            with self.subTest(key=key): self.assertTrue(cap.profile_problems(p))

    def test_schema_bound_to_run_and_reject_public(self):
        good = cap.schema_for('my-run')
        self.assertTrue(cap.valid_schema(good, 'my-run'))
        self.assertFalse(cap.valid_schema('public', 'my-run'))
        self.assertFalse(cap.valid_schema(good, 'other-run'))

    def test_write_once(self):
        with tempfile.TemporaryDirectory() as root:
            p = Path(root) / 'evidence.json'
            cap.write_once(p, {'a': 1})
            with self.assertRaises(FileExistsError): cap.write_once(p, {'a': 2})
            self.assertEqual(json.loads(p.read_text()), {'a': 1})

    def test_stop_refuses_process_reuse(self):
        current = cap.process_identity(os.getpid())
        current['start_ticks'] = '-1'
        self.assertFalse(cap.same_process(current))

    def test_stop_does_not_trust_finalized_marker(self):
        with tempfile.TemporaryDirectory() as root:
            d = object.__new__(cap.Driver)
            d.root = d.output_root = Path(root)
            d.identity = self.identity
            d.log = Mock()
            phase = Path(root)/'seed'
            phase.mkdir()
            owned = {**cap.process_identity(os.getpid()), 'cwd': root, 'pid': 987654}
            cap.write_once(phase/'registry.json', {**self.identity, 'processes': {'driver': owned}})
            cap.write_once(phase/'result.json', {'status': 'COMPLETED'})
            with patch.object(cap, 'same_process', side_effect=[True, False, False]), patch.object(cap.os, 'kill') as kill:
                self.assertEqual(len(d.stop()['stopped']), 1)
                kill.assert_called_once_with(987654, cap.signal.SIGTERM)
            phase = Path(root)/'soak'
            phase.mkdir()
            cap.write_once(phase/'registry.json', {**self.identity, 'processes': {'driver': {
                **cap.process_identity(os.getpid()), 'cwd': root, 'start_ticks': '-1'}}})
            with patch.object(cap.os, 'kill') as kill:
                with self.assertRaises(ValueError): d.stop()
                kill.assert_not_called()

    def test_burst_requires_double_clients_and_idle_recovery(self):
        self.row.update(phase='burst', requested_seconds=900, elapsed_seconds=900, unique_sessions=40)
        self.samples = [self.sample(t) for t in range(0, 901, 60)]
        self.row['idle_baseline'] = copy.deepcopy(self.samples[0]['processes'])
        for s in self.samples:
            s.update(sessions=40 if s['elapsed'] < 300 else 0,
                     streams=100 if s['elapsed'] < 300 else 0,
                     downloads=8 if s['elapsed'] < 300 else 0)
        self.downloads += [{**self.downloads[0], 'worker': i} for i in range(4, 8)]
        self.assertEqual(self.check(), [])
        self.samples[1]['sessions'] = 20
        self.assertTrue(self.check())
        self.samples[1]['sessions'] = 40
        self.samples[-1]['queue'] = 1
        self.assertTrue(self.check())
        self.samples[-1]['queue'] = 0
        self.samples[-1]['processes']['product']['fds'] += 9
        self.assertTrue(self.check())


class ReviewBoundaryTests(unittest.TestCase):
    """审查反例。这里只构造检查器输入，不生成任何资格结果。"""
    def test_http_error_records_only_safe_category_and_codes(self):
        summary = cap.error_summary({'code': 422, 'message': '第 7 层（状态机）尚未实现，暂不能取消或重试任务',
                                     'data': {'problems': [{'code': 'task_control_state'}]}}, 422)
        self.assertEqual(summary['category'], 'task_control_unavailable')
        self.assertEqual(summary['problem_codes'], ['task_control_state'])
        secret = 'private-message-never-logged'
        summary = cap.error_summary({'code': 500, 'message': secret, 'data': {'token': secret}}, 500)
        self.assertNotIn(secret, json.dumps(summary))

    def test_cross_second_audit_is_legal_but_old_time_and_wrong_actor_rejected(self):
        task = {'id': 't', 'state': 'cancelled', 'createdAt': 100, 'createdBy': 'owner'}
        entries = [{'targetKind': 'task', 'targetID': 't', 'actor': 'owner', 'action': action, 'at': at}
                   for action, at in [('task.create', 101), ('task.cancel', 102)]]
        self.assertEqual(cap.audit_time_problems(task, entries), [])
        for index, at in [(0, 99), (1, 100)]:
            changed = copy.deepcopy(entries); changed[index]['at'] = at
            with self.subTest(index=index): self.assertTrue(cap.audit_time_problems(task, changed))
        changed = copy.deepcopy(entries); changed[1]['actor'] = 'other'
        self.assertTrue(cap.audit_time_problems(task, changed))
    def test_stage_directory_is_authoritative(self):
        identity = {'run_id': 'checker'}
        row = {**identity, 'phase': 'soak'}
        self.assertTrue(cap.stage_bundle_problems('normal', identity, row, {}, {}, []))
        self.assertTrue(cap.stage_bundle_problems('burst', identity, row, {}, {}, []))

    def test_complete_stage_binding_and_wrong_command(self):
        binding = {'run_id': 'checker', 'phase': 'normal', 'phase_id': 'a'*32}
        inputs = {k: '/owned/'+k for k in ('manifest', 'profile', 'output', 'work-root')}
        driver = {'cwd': '/owned', 'pid': 123}
        window = {'id': 'b'*32, 'start': 110, 'end': 1910}
        row = {**binding, 'cleanup_complete': True, 'residuals': [], 'started_at': 100, 'ended_at': 1920,
               'window': window, 'inputs': inputs, 'driver_identity': driver}
        argv = ['tool', '--phase', 'normal']+[v for k, p in inputs.items() for v in ('--'+k, p)]
        command = {**binding, 'argv': argv, 'started_at': 100, 'cwd': '/owned', 'inputs': inputs}
        registry = {**binding, 'processes': {'driver': driver}}
        lifecycle = [{**binding, 'status': 'RUNNING', 'at': 100.1},
                     {**binding, 'kind': 'measurement_begin', 'at': 110, 'window': window},
                     {**binding, 'kind': 'measurement_end', 'at': 1910, 'window_id': window['id']},
                     {**binding, 'status': 'COMPLETED', 'at': 1915}]
        self.assertEqual(cap.stage_bundle_problems('normal', {'run_id': 'checker'}, row, command, registry, lifecycle), [])
        command['argv'][2] = 'soak'
        self.assertTrue(cap.stage_bundle_problems('normal', {'run_id': 'checker'}, row, command, registry, lifecycle))

    def test_all_soak_jobs_and_missing_last_import(self):
        row = {'phase': 'soak', 'window': {'id': 'w', 'start': 0, 'end': 86400}}
        requests = []
        for minute in range(1440):
            for method in ('POST', 'GET'):
                requests.append({'category': 'create_read', 'method': method, 'object_id': str(minute),
                                 'at': minute*60+1, 'window_id': 'w', 'ok': True})
        for period in range(144):
            for session in range(20): requests.append({'category': 'idle_observation', 'at': period*600,
                'window_id': 'w', 'session': session, 'seconds': 10, 'ok': True})
        imports = [{'sha256': str(i), 'size': 100} for i in range(48)]
        logs = [{'kind': 'import_verified', 'window_id': 'w', 'index': i, 'input_sha256': str(i),
                 'started_at': i*1800+1, 'at': i*1800+2, 'uploaded_bytes': 100, 'state': 'ready'} for i in range(48)]
        self.assertEqual(cap.periodic_jobs_problems(row, iter(requests), logs, imports), [])
        self.assertTrue(cap.periodic_jobs_problems(row, iter(requests), logs[:-1], imports))

    def test_stop_entry_independent_of_product_and_retryable(self):
        import argparse
        with tempfile.TemporaryDirectory() as directory:
            base = Path(directory); work = base/'work'; work.mkdir(mode=0o700); output = work/'out'; output.mkdir()
            cap.write_once(base/'.qualification-owner', {'owner': 'checker'})
            manifest = {'run_id': 'checker', 'product_sha': 'a'*40, 'test_sha': 'b'*40, 'binary_sha256': 'c'*64,
                        'binary': '/missing-product', 'capacity': {'owner_sha256': cap.sha(base/'.qualification-owner'),
                        'run_id': 'checker', 'executor_sha256': cap.sha(cap.__file__), 'profile_sha256': 'd'*64,
                        'run_identity_sha256': 'e'*64}}
            path = base/'manifest.json'; cap.write_once(path, manifest)
            args = argparse.Namespace(work_root=work, output=output, manifest=path, profile=base/'missing-profile')
            with patch.object(cap, 'BASE', base), patch.object(cap.Driver, 'stop', side_effect=[ValueError('timeout'), {'stopped': []}]):
                self.assertEqual(cap.stop_entry(args), 1)
                self.assertEqual(cap.stop_entry(args), 0)
            self.assertEqual(len(list((output/'stop-attempts').iterdir())), 2)

    def test_cleanup_uses_one_deadline_for_all_threads(self):
        d = object.__new__(cap.Driver); d.profile = {'cleanup_seconds': 30}; d.shutdown_deadline = None
        d.child_procs, d.auth_books = {}, {}; d.abort_io = Mock(); d.log = Mock()
        now = [0]; waits = []
        def join(timeout): waits.append(timeout); now[0] += timeout
        threads = [Mock(name='blocked') for _ in range(20)]
        for t in threads: t.join.side_effect = join; t.is_alive.return_value = True
        with patch.object(cap.time, 'monotonic', side_effect=lambda: now[0]): self.assertFalse(d.cleanup(threads))
        self.assertEqual(sum(waits), 30)

    def test_seed_claim_without_raw_operations_rejected(self):
        self.assertTrue(cap.seed_evidence_problems({'hosts': 10000, 'historical_tasks': 100000}, [], []))

    def test_only_one_soak_import_rejected(self):
        row = {'phase': 'soak', 'window': {'start': 10, 'end': 86410, 'id': 'w'}}
        self.assertTrue(cap.periodic_jobs_problems(row, [], [{'kind': 'import_verified', 'index': 0}],
                                                 [{'sha256': str(i)} for i in range(48)]))

    def test_reads_concentrated_or_outside_window_rejected(self):
        row = {'phase': 'normal', 'window': {'start': 100, 'end': 1900, 'id': 'w'}}
        rows = [{'category': 'host_list', 'session': 0, 'at': 50, 'window_id': 'w', 'ok': True, 'seconds': .1}]
        self.assertTrue(cap.activity_evidence_problems(row, rows))

    def test_between_stage_mutation_rejected(self):
        baseline = {'snapshot_digest': 'a', 'source_digest': 'b', 'historical_tasks': 100000}
        current = {**baseline, 'snapshot_digest': 'changed'}
        self.assertTrue(cap.invariant_chain_problems(baseline, current))

    def test_download_lane_stall_and_serial_transfers_rejected(self):
        window = {'start': 100, 'end': 220}
        rows = []
        for lane in range(4):
            rows.extend([{'kind': 'open', 'lane': lane, 'connection': str(lane), 'at': 90},
                         {'kind': 'close', 'lane': lane, 'connection': str(lane), 'at': 230},
                         {'kind': 'progress', 'lane': lane, 'connection': str(lane), 'at': 100, 'bytes': 1},
                         {'kind': 'progress', 'lane': lane, 'connection': str(lane), 'at': 160, 'bytes': 100},
                         {'kind': 'progress', 'lane': lane, 'connection': str(lane), 'at': 220, 'bytes': 200}])
        self.assertEqual(cap.lane_evidence_problems(rows, window, 4, progress=True), [])
        rows = [r for r in rows if not (r['lane'] == 3 and r['kind'] == 'progress' and r['at'] >= 160)]
        self.assertTrue(cap.lane_evidence_problems(rows, window, 4, progress=True))
        serial = [{'kind': kind, 'lane': lane, 'connection': str(lane), 'at': at}
                  for lane in range(4) for kind, at in [('open', 100+lane*30), ('close', 130+lane*30)]]
        self.assertTrue(cap.lane_evidence_problems(serial, window, 4, progress=False))

    def test_chunk_end_detected_before_tcp_close_and_cursor_resume(self):
        decoder = cap.EventDecoder(chunked=True, after=1)
        body = b'id: 2\ndata: {"seq":2,"taskID":"t"}\n\n'
        self.assertEqual(decoder.feed(hex(len(body))[2:].encode()+b'\r\n'+body+b'\r\n')[0]['seq'], 2)
        self.assertEqual(decoder.last, 2)
        with self.assertRaises(EOFError): decoder.feed(b'0\r\n\r\n')
        resumed = cap.EventDecoder(chunked=True, after=2)
        body = b'id: 4\ndata: {"seq":4,"taskID":"t"}\n\n'
        with self.assertRaises(ValueError): resumed.feed(hex(len(body))[2:].encode()+b'\r\n'+body+b'\r\n')

    def test_install_summary_and_duplicate_machine_rejected(self):
        self.assertTrue(cap.installation_problems({'status': 'PASS', 'synthetic': False,
                       'simultaneous_verified_installations': 2}, {}, Path('/nonexistent')))

    def install_fixture(self, root, serial_parallel=False):
        """仅聚合器合同夹具。冻结的是假检查器；这些JSON不是真实客户机结果。"""
        identity = {'run_id': 'CHECKER_ONLY_NO_QUALIFICATION', 'product_sha': 'a'*40, 'test_sha': 'b'*40,
                    'binary_sha256': 'c'*64, 'run_identity_sha256': 'd'*64}
        contract = {'installation_checkers': {}, 'installation_targets': {}}
        checkers = {}
        for name in ('a', 'b'):
            path = root/('mock-checker-'+name+'.py')
            path.write_text("SN='SN-"+name+"'\ndef check_qualification(value, negative=False):\n assert value.get('checker_fixture') is True\n return {'status':'PASS'}\n")
            checkers[name] = {'path': path.name, 'sha256': cap.sha(path)}
            contract['installation_checkers'][str(path)] = cap.sha(path)
            contract['installation_targets']['VM-'+name] = {'uuid': 'VM-'+name, 'sn': 'SN-'+name}
        report = {**identity, 'status': 'PASS', 'synthetic': False}
        for mode, times in [('parallel', [(1010, 1020), (1030, 1040)] if serial_parallel else [(1010, 1020), (1015, 1025)]),
                            ('serial', [(950, 960), (970, 980)])]:
            report[mode] = []
            for name, (start, end) in zip(('a', 'b'), times):
                folder = root/(mode+'-'+name); folder.mkdir()
                task_id = mode+'-task-'+name
                guest = {'smbios_serial': 'SN-'+name}
                capture = folder/'install.pcap'; capture.write_bytes(b'CHECKER ONLY - not an actual network capture')
                repeated = folder/'repeat.pcap'; repeated.write_bytes(b'CHECKER ONLY - repeated synthetic bytes')
                payloads = {'qualification': {'checker_fixture': True, 'task': {'id': task_id, 'state': 'done', 'hostSN': 'SN-'+name},
                    'events': [{'seq': 1, 'at': start, 'kind': 'machine.installing'}, {'seq': 2, 'at': end, 'kind': 'machine.ready'}],
                    'guest': guest, 'repeat': {'guest': guest}, 'raw': {'install_capture_sha256': cap.sha(capture), 'repeat_capture_sha256': cap.sha(repeated)}},
                    'timeline': {'task_id': task_id, 'target_uuid': 'VM-'+name, 'install_started_at': start, 'guest_accepted_at': end+1},
                    'target': contract['installation_targets']['VM-'+name], 'guest': guest, 'repeat_guest': guest,
                    'manifest': dict(identity)}
                files = {}
                for label, data in payloads.items():
                    path = folder/(label+'.json'); cap.write_once(path, data)
                    files[label] = {'path': str(path.relative_to(root)), 'sha256': cap.sha(path)}
                result = {**identity, 'phase': 'qualify', 'status': 'PASS', 'executor_sha256': checkers[name]['sha256'],
                          'manifest_sha256': files['manifest']['sha256'],
                          'files': {p.name: cap.sha(p) for p in folder.iterdir() if p.is_file()}}
                path = folder/'result.json'; cap.write_once(path, result)
                files['result'] = {'path': str(path.relative_to(root)), 'sha256': cap.sha(path)}
                report[mode].append({**identity, 'status': 'PASS', 'synthetic': False,
                                     'target_uuid': 'VM-'+name, 'task_id': task_id, 'files': files, 'checker': checkers[name]})
        return identity, contract, report

    def test_install_raw_contract_detects_duplicate_machine_serial_claim_and_changed_original(self):
        with tempfile.TemporaryDirectory(prefix='capacity-checker-only-') as directory:
            root = Path(directory)
            identity, contract, report = self.install_fixture(root)
            with patch.object(cap, 'BASE', root):
                self.assertEqual(cap.installation_problems(report, identity, root, contract), [])
                duplicate = copy.deepcopy(report); duplicate['parallel'][1] = duplicate['parallel'][0]
                self.assertTrue(cap.installation_problems(duplicate, identity, root, contract))
                wrong_test = {**report, 'test_sha': 'wrong'}
                self.assertTrue(cap.installation_problems(wrong_test, identity, root, contract))
                for location in ('report', 'case'):
                    wrong = copy.deepcopy(report)
                    if location == 'report': wrong['synthetic'] = True
                    else: wrong['parallel'][0]['synthetic'] = True
                    self.assertTrue(cap.installation_problems(wrong, identity, root, contract))
                reused = copy.deepcopy(report); reused['serial'][0] = reused['parallel'][0]
                self.assertTrue(cap.installation_problems(reused, identity, root, contract))
                absent_manifest = copy.deepcopy(report); del absent_manifest['parallel'][0]['files']['manifest']
                self.assertTrue(cap.installation_problems(absent_manifest, identity, root, contract))
                manifest = root/report['parallel'][0]['files']['manifest']['path']
                result_file = root/report['parallel'][0]['files']['result']['path']
                saved_manifest, saved_result = manifest.read_bytes(), result_file.read_bytes()
                changed = copy.deepcopy(report)
                actual = json.loads(saved_manifest); actual['run_identity_sha256'] = 'wrong-common-input'
                manifest.write_text(json.dumps(actual))
                changed['parallel'][0]['files']['manifest']['sha256'] = cap.sha(manifest)
                actual_result = json.loads(saved_result)
                actual_result['manifest_sha256'] = cap.sha(manifest)
                actual_result['files'][manifest.name] = cap.sha(manifest)
                result_file.write_text(json.dumps(actual_result))
                changed['parallel'][0]['files']['result']['sha256'] = cap.sha(result_file)
                self.assertTrue(cap.installation_problems(changed, identity, root, contract))
                manifest.write_bytes(saved_manifest); result_file.write_bytes(saved_result)
                original = root/report['parallel'][0]['files']['qualification']['path']
                original.write_text(original.read_text()+' ')
                self.assertTrue(cap.installation_problems(report, identity, root, contract))
        with tempfile.TemporaryDirectory(prefix='capacity-checker-only-') as directory:
            root = Path(directory); identity, contract, report = self.install_fixture(root, serial_parallel=True)
            with patch.object(cap, 'BASE', root): self.assertTrue(cap.installation_problems(report, identity, root, contract))

    def test_event_connection_index_preserves_coverage_and_cursor_rules(self):
        window = {'start': 0, 'end': 240}
        rows = [{'kind': 'open', 'lane': 0, 'connection': 'one', 'at': 0, 'after_seq': 0, 'task_id': 't'},
                {'kind': 'event_cursor', 'lane': 0, 'connection': 'one', 'at': 1, 'seq': 1, 'task_id': 't'},
                {'kind': 'open', 'lane': 0, 'connection': 'two', 'at': 119, 'after_seq': 1, 'task_id': 't'},
                {'kind': 'close', 'lane': 0, 'connection': 'one', 'at': 120},
                {'kind': 'event_cursor', 'lane': 0, 'connection': 'two', 'at': 121, 'seq': 2, 'task_id': 't'},
                {'kind': 'close', 'lane': 0, 'connection': 'two', 'at': 241}]
        self.assertEqual(cap.event_lane_problems(iter(rows), window, 1), [])
        rows[2]['after_seq'] = 0
        self.assertTrue(cap.event_lane_problems(iter(rows), window, 1))

    def test_evaluate_acquires_shared_lease_before_reading_results(self):
        d = object.__new__(cap.Driver)
        class BusyLease(Exception): pass
        d.acquire = Mock(side_effect=BusyLease)
        with self.assertRaises(BusyLease): d.evaluate()
        d.acquire.assert_called_once_with()

    def test_filesystem_budget_deduplicates_and_excludes_warmup(self):
        volumes = {'data': 'new-disk', 'wal': 'new-disk', 'logs': 'root'}
        samples = [{'relation_bytes': n, 'log_bytes': m} for n, m in [(1000000, 500), (1010000, 1000), (1020000, 1500)]]
        result = cap.plan_storage(samples, volumes, remaining_batches=10, wal_reserve=20000)
        self.assertEqual(result['new-disk'], 10000*10*3+20000)
        self.assertEqual(result['root'], 500*10*2)
        with self.assertRaises(ValueError): cap.verify_path_coverage({'data': '/owned/data', 'wal': '/owned/wal'}, ['/owned/data'])

    def test_auth_refresh_waits_for_stream_retirement(self):
        now = [100]
        issued, retired = [], []
        def login():
            token = 'private-'+str(len(issued)); issued.append(token)
            return {'token': token, 'expiresAt': now[0]+20}
        book = cap.AuthSession(login, retired.append, lambda **r: None, clock=lambda: now[0], margin=5)
        first, gen = book.acquire()
        now[0] = 116
        second = book.current()
        self.assertNotEqual(first, second)
        self.assertEqual(retired, [])
        book.release(gen)
        self.assertEqual(retired, [first])
        book.close()
        self.assertEqual(retired, [first, second])

    def test_public_and_original_8080_targets_rejected(self):
        for url in ('http://8.8.8.8:8000', 'http://127.0.0.1:8080', 'https://example.com/a'):
            with self.subTest(url=url), self.assertRaises(ValueError):
                cap.validate_url(url, ['127.0.0.1', '8.8.8.8', 'example.com'])


class LoadThreadBoundaryTests(unittest.TestCase):
    """只跑真实驱动器函数和内存连接；屏障控制交错，不启动服务或缩短正式档。"""
    def driver(self, directory):
        d = object.__new__(cap.Driver)
        d.profile = copy.deepcopy(cap.FORMAL_PROFILE)
        d.args = types.SimpleNamespace(phase='normal')
        d.identity = {'run_id': 'THREAD_BOUNDARY_CHECKER_ONLY'}
        d.start, d.window = 100, None
        d.media = {'url': 'http://127.0.0.1:18999/image', 'size': 4*1024*1024,
                   'sha256': cap.hashlib.sha256(b'x'*(4*1024*1024)).hexdigest()}
        d.url = 'http://127.0.0.1:18999'
        d.auth = {'seed': {}, 'sessions': [{'username': str(i)} for i in range(20)]}
        d.tokens, d.auth_books, d.child_procs = {}, {}, {}
        d.lock, d.io_lock = threading.Lock(), threading.RLock()
        d.request_condition = threading.Condition(d.lock)
        d.business_inflight, d.load_requests_closed = 0, False
        d.download_ready = cap.collections.Counter()
        d.active = cap.collections.Counter(sessions=20, streams=50, downloads=4)
        d.lane_bytes = cap.collections.Counter()
        d.stop_event, d.measurement = threading.Event(), threading.Event()
        d.sockets, d.threads, d.residuals = set(), [], []
        d.shutdown_deadline, d.cleanup_complete = None, False
        d.interrupted, d.worker_errors = False, 0
        d.log, d.requests, d.downloads, d.lanes = (self.Journal() for _ in range(4))
        d.sessions_log = self.Journal()
        d.output_root = Path(directory)
        (d.output_root/'seed').mkdir()
        dataset = dict(hosts=10000, historical_tasks=100000, snapshot_digest='s', history_digest='h', source_digest='i')
        cap.write_once(d.output_root/'seed-baseline.json', {**d.identity, 'dataset': dataset})
        cap.write_once(d.output_root/'seed/result.json', {'baseline_sha256': cap.sha(d.output_root/'seed-baseline.json')})
        d.acquire = d.configure_storage = d.verify_api_dataset = lambda *a, **k: None
        d.db_state = lambda: dataset.copy()
        d.login = lambda credential, session: 'opaque-checker-token'
        d.guard_resources = lambda row: None
        d.sample = lambda **kwargs: {'processes': {}, 'oom_kill': 0}
        d.download = Mock(return_value=True)
        d.console_loop = d.stream_loop = d.download_loop = d.import_loop = lambda *args: None
        return d

    class Journal:
        def __init__(self): self.rows, self.lock, self.before, self.after = [], threading.Lock(), None, None
        def add(self, **row):
            if self.before: self.before(row)
            with self.lock: self.rows.append({'at': cap.time.monotonic(), **row})
            if self.after: self.after(row)

    def launch(self, d):
        result = {}
        def run():
            try: result['value'] = d.load()
            except Exception as error: result['error'] = error
        t = threading.Thread(target=run, daemon=True); t.start()
        return t, result

    def settlement_case(self, outcome='healthy', interruption=None, phase='normal'):
        with tempfile.TemporaryDirectory() as directory:
            d = self.driver(directory)
            d.args.phase = phase
            cutoff = 400 if phase == 'burst' else 1900
            if phase == 'burst':
                d.auth['sessions'] += [{'username': str(i)} for i in range(20, 40)]
                d.active.update(sessions=20, streams=50, downloads=4)
                d.log.before = lambda row: now.__setitem__(0, 1000.1) if row.get('kind') == 'burst_load_stopped' else None
            d.download_ready.update({i: 1 for i in range(4)})
            if phase == 'burst': d.download_ready.update({i: 1 for i in range(4, 8)})
            now = [100.0]
            sampled, end_sample, send, reading, release, cleanup_entered, aborted = [threading.Event() for _ in range(7)]
            class Response:
                status = 500 if outcome == '500' else 200
                fp = None
                def read(self, limit):
                    reading.set()
                    if not release.wait(3): raise TimeoutError('test barrier not released')
                    if aborted.is_set(): raise OSError('connection closed by driver')
                    if outcome == 'disconnect': raise ConnectionResetError('peer closed')
                    if outcome == 'timeout': raise TimeoutError('actual request deadline')
                    return json.dumps({'code': 1 if outcome == '500' else 0, 'data': {'items': [1]}}).encode()
                def close(self): pass
            class Connection:
                def __init__(self, *a, **k): self.sock = None
                def request(self, *a, **k): pass
                def getresponse(self): return Response()
                def close(self):
                    if not release.is_set(): aborted.set(); release.set()
            original_cleanup = d.cleanup
            def cleanup(*args, **kwargs):
                cleanup_entered.set()
                return original_cleanup(*args, **kwargs)
            d.cleanup = cleanup
            def sample(**kwargs):
                if kwargs and not sampled.is_set(): sampled.set(); end_sample.wait(3)
                return {'processes': {}, 'oom_kill': 0}
            d.sample = sample
            def console(worker, quit_event):
                if worker: return
                d.measurement.wait(3); send.wait(3)
                try: d.request('GET', '/hosts', category='host_list', session=0)
                except Exception: pass
            d.console_loop = console
            if interruption == 'resource':
                def guard(row):
                    if sampled.is_set(): raise RuntimeError('resource stop threshold')
                d.guard_resources = guard
            with patch.object(cap.time, 'monotonic', side_effect=lambda: now[0]), patch.object(cap.http.client, 'HTTPConnection', Connection):
                t, result = self.launch(d)
                try:
                    self.assertTrue(sampled.wait(3))
                    now[0] = cutoff-.05; send.set()
                    self.assertTrue(reading.wait(3))
                    if interruption == 'TERM': d.on_signal(cap.signal.SIGTERM, None)
                    now[0] = cutoff+.1; end_sample.set()
                    self.assertTrue(cleanup_entered.wait(3))
                    if interruption == 'TERM_during':
                        self.assertFalse(aborted.wait(.03))
                        deadline = d.shutdown_deadline
                        now[0] = cutoff+.2; d.on_signal(cap.signal.SIGTERM, None)
                        self.assertEqual(d.shutdown_deadline, deadline)
                        self.assertTrue(aborted.wait(1))
                    elif interruption:
                        self.assertTrue(aborted.wait(1))
                    else:
                        self.assertFalse(aborted.wait(.03), 'normal end cut a request admitted at T-.05')
                    now[0] = cutoff+.45; release.set()
                    t.join(3)
                    self.assertFalse(t.is_alive())
                finally:
                    release.set(); end_sample.set(); send.set(); d.stop_event.set(); t.join(3)
            raw = [r for r in d.requests.rows if r.get('category') == 'host_list']
            self.assertEqual(len(raw), 1)
            self.assertEqual(raw[0]['at'], cutoff-.05)
            stats = cap.request_summary(r for r in raw if 100 <= r['at'] < cutoff)
            print(json.dumps({'boundary': 'T-.05/T+.1', 'outcome': outcome, 'interruption': interruption,
                              'requests': raw, 'summary': stats, 'window': d.window, 'returned': 'value' in result}))
            if interruption:
                self.assertIn('error', result)
            else:
                self.assertIn('value', result)
                self.assertEqual(d.window['end'], 1000 if phase == 'burst' else 1900)
            self.assertEqual(stats['server_errors'], int(outcome == '500' and not interruption))
            self.assertEqual(stats['other_errors'], int(outcome in ('disconnect', 'timeout') or bool(interruption)))
            self.assertEqual(stats['categories']['host_list']['successes'], int(outcome == 'healthy' and not interruption))
            if outcome == 'healthy' and not interruption: self.assertAlmostEqual(raw[0]['seconds'], .5)
            checker = CapacityCheckerTests(); checker.setUp()
            checker.requests['count'] = 100
            checker.requests['server_errors'] += stats['server_errors']
            checker.requests['other_errors'] += stats['other_errors']
            if interruption:
                checker.row.update(status='FAILED', interrupted=d.interrupted)
            self.assertEqual(bool(checker.check()), outcome != 'healthy' or bool(interruption))

    def test_normal_end_settles_window_request_without_tool_error(self): self.settlement_case()
    def test_real_500_after_window_is_still_counted(self): self.settlement_case('500')
    def test_real_disconnect_after_window_is_still_counted(self): self.settlement_case('disconnect')
    def test_real_timeout_after_window_is_still_counted(self): self.settlement_case('timeout')
    def test_term_does_not_complete_during_settlement(self): self.settlement_case(interruption='TERM')
    def test_resource_stop_does_not_complete_during_settlement(self): self.settlement_case(interruption='resource')
    def test_burst_end_settles_before_original_recovery_window(self): self.settlement_case(phase='burst')
    def test_term_during_settlement_preserves_deadline_and_does_not_complete(self): self.settlement_case(interruption='TERM_during')

    def test_fixed_cutoff_blocks_new_business_but_allows_auth_cleanup(self):
        with tempfile.TemporaryDirectory() as directory:
            d = self.driver(directory); d.window = {'start': 100, 'end': 1900}
            for phase, at in [('normal', 1900), ('burst', 400)]:
                d.args.phase = phase
                with self.subTest(phase=phase), patch.object(cap.time, 'monotonic', return_value=at), patch.object(cap.http.client, 'HTTPConnection') as connection:
                    connection.return_value.getresponse.return_value = types.SimpleNamespace(
                        status=200, fp=None, read=lambda n: b'{"code":0,"data":{}}', close=lambda: None)
                    for category in ('host_list', 'create_read', 'upload_import'):
                        with self.assertRaises(Exception): d.request('GET', '/hosts', category=category)
                    with self.assertRaises(Exception): d.request('GET', '/hosts')
                    self.assertFalse(d.measurement.is_set())
                    connection.assert_not_called()
                    d.request('POST', '/auth/logout', 'opaque', session=0)
                    connection.assert_called_once()
            self.assertTrue(all(r['path'] == '/auth/logout' for r in d.requests.rows))

    def test_unfinished_settlement_expires_at_common_deadline(self):
        with tempfile.TemporaryDirectory() as directory:
            d = self.driver(directory)
            d.business_inflight = 1
            now = [1900.1]
            waiting, finish = threading.Event(), threading.Event()
            class Condition(threading.Condition):
                def wait(self, timeout=None): waiting.set(); return super().wait(timeout)
            d.request_condition = Condition(d.lock)
            d.abort_io = Mock(side_effect=finish.set)
            d.shutdown_deadline = 1920.0  # 已有截止比本次起算30秒更早。
            result = []
            with patch.object(cap.time, 'monotonic', side_effect=lambda: now[0]):
                t = threading.Thread(target=lambda: result.append(d.cleanup(settle=True)), daemon=True)
                t.start()
                try:
                    self.assertTrue(waiting.wait(3))
                    self.assertFalse(finish.is_set())
                    now[0] = 1920.0
                    with d.request_condition: d.request_condition.notify_all()
                    t.join(3)
                    self.assertFalse(t.is_alive())
                    self.assertEqual(result, [False])
                    self.assertTrue(finish.is_set())
                    self.assertEqual(d.shutdown_deadline, 1920.0)
                    row = next(r for r in d.log.rows if r.get('kind') == 'request_settlement')
                    self.assertFalse(row['complete']); self.assertEqual(row['remaining'], 1)
                finally:
                    now[0] = 1920.0
                    with d.request_condition: d.request_condition.notify_all()
                    t.join(3)

    def readiness_case(self, pause_kind, multiplier=1, duplicate=False):
        with tempfile.TemporaryDirectory() as directory:
            d = self.driver(directory)
            if multiplier == 2:
                d.args.phase = 'burst'
                d.auth['sessions'] += [{'username': str(i)} for i in range(20, 40)]
                d.active.update(sessions=20, streams=50)
            d.active['downloads'] = 0
            paused, resume, sampled, finish = [threading.Event() for _ in range(4)]
            bodies, completed = threading.Event(), threading.Event()
            count = 4*multiplier
            now = [100.0]
            complete_barrier = threading.Barrier(count, action=lambda: now.__setitem__(0, 160))
            local = threading.local()
            class Response:
                status = 200
                def __init__(self): self.fp = types.SimpleNamespace(raw=types.SimpleNamespace(_sock=Mock())); self.left = d.media['size']
                def getheader(self, key, default): return str(d.media['size'])
                def read(self, n):
                    if self.left == d.media['size']:
                        if getattr(local, 'lane', -1) == count-1 and pause_kind == 'first': paused.set(); resume.wait(3)
                        if getattr(local, 'lane', -1) == count-1 and pause_kind == 'zero': paused.set(); return b''
                    else:
                        bodies.wait(3)
                        if self.left <= n: complete_barrier.wait(3)
                    n = min(n, self.left); self.left -= n
                    return b'x'*n
                def close(self): pass
            class Connection:
                def __init__(self, *a, **k): self.response = Response()
                def request(self, *a, **k): pass
                def getresponse(self): return self.response
                def close(self): pass
            def before(row):
                if row.get('lane') == count-1 and row.get('kind') == pause_kind and not paused.is_set():
                    paused.set(); resume.wait(3)
            d.lanes.before = before
            actual_download = cap.Driver.download.__get__(d)
            def downloads(worker, quit_event):
                local.lane = worker
                if duplicate and worker == count-1:
                    local.lane = worker-1
                    worker -= 1
                try: actual_download(worker, quit_event=quit_event)
                except Exception:
                    if not d.stop_event.is_set(): d.worker_failed('checker-download')
            d.download_loop = downloads
            d.downloads.after = lambda row: completed.set() if len(d.downloads.rows) == count else None
            def sample(**kwargs):
                if kwargs: sampled.set(); finish.wait(3)
                return {'processes': {}, 'oom_kill': 0}
            d.sample = sample
            with patch.object(cap.time, 'monotonic', side_effect=lambda: now[0]), patch.object(cap.http.client, 'HTTPConnection', Connection):
                t, result = self.launch(d)
                try:
                    if duplicate:
                        self.assertFalse(d.measurement.wait(.15), 'two connections in one lane replaced a missing lane')
                    elif pause_kind == 'zero':
                        self.assertTrue(paused.wait(3))
                        self.assertFalse(d.measurement.wait(.05))
                        self.assertTrue(d.stop_event.is_set())
                    else:
                        self.assertTrue(paused.wait(3))
                        self.assertFalse(d.measurement.wait(.05), pause_kind+' was published before complete lane evidence')
                        resume.set()
                        self.assertTrue(sampled.wait(3))
                        self.assertEqual(d.active['download_bytes'], count)
                        for lane in range(count):
                            initial = [r for r in d.lanes.rows if r.get('lane') == lane and r.get('kind') in ('open', 'progress')]
                            self.assertTrue(any(r['kind'] == 'open' for r in initial))
                            self.assertTrue(any(r['kind'] == 'progress' and r['bytes'] > 0 for r in initial))
                            self.assertTrue(all(r['at'] <= d.window['start'] for r in initial))
                        print(json.dumps({'boundary': 'lane-ready', 'pause': pause_kind, 'lanes': count,
                                          'window': d.window, 'initial': d.lanes.rows}))
                        now[0] = 150; bodies.set()
                        self.assertTrue(completed.wait(3))
                        for thread in d.threads: thread.join(.1)
                        short_window = {'start': 100, 'end': 160}
                        self.assertEqual(cap.lane_evidence_problems(d.lanes.rows, short_window, count, progress=True), [])
                        self.assertEqual(cap.download_summary(d.downloads.rows, d.media['size'], d.media['sha256'])['verified_workers'], count)
                        self.assertTrue(all(r['bytes'] == d.media['size'] and r['verified'] for r in d.downloads.rows))
                        self.assertEqual(d.active['download_bytes'], count*d.media['size'])
                        self.assertTrue(all(d.lane_bytes[lane] == d.media['size'] for lane in range(count)))
                        print(json.dumps({'boundary': 'lane-ready-full-mock-summary', 'lanes': count,
                                          'scope': 'NOT_QUALIFICATION', 'window': short_window, 'records': d.lanes.rows,
                                          'downloads': d.downloads.rows}))
                finally:
                    d.stop_event.set(); resume.set(); bodies.set(); finish.set(); t.join(3)
            self.assertFalse(t.is_alive())

    def test_last_lane_first_byte_before_open_does_not_start_window(self): self.readiness_case('open')
    def test_last_lane_open_before_progress_does_not_start_window(self): self.readiness_case('progress')
    def test_burst_eight_distinct_lanes_wait_for_initial_progress(self): self.readiness_case('progress', multiplier=2)
    def test_same_lane_two_connections_cannot_replace_missing_lane(self): self.readiness_case('none', duplicate=True)
    def test_waiting_first_byte_cannot_start_window(self): self.readiness_case('first')
    def test_zero_byte_lane_cannot_start_window(self): self.readiness_case('zero')


if __name__ == '__main__':
    unittest.main(verbosity=2)
