"""部署检查器反例；只测边界，不把这些测试计作真实部署通过。"""
import copy
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
from unittest.mock import patch
import shutil
import production_deployment as deployment

from production_deployment import (BASE, OWNER, PHASES, DeploymentError, assert_namespace,
    check_candidate, check_owner, create_private, file_map, verify_files,
    process_identity, verify_process, validate_phase)
from gate import digest


class DeploymentBoundaries(unittest.TestCase):
    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.root.chmod(0o700)
        (self.root / '.qualification-owner').write_text(OWNER)

    def test_owner_requires_exact_marker(self):
        (self.root / '.qualification-owner').write_text('another-run')
        with self.assertRaises(DeploymentError):
            check_owner(self.root, expected_root=self.root)

    def test_root_must_equal_declared_root(self):
        with self.assertRaises(DeploymentError):
            check_owner(self.root, expected_root=self.root / 'other')

    def test_owner_requires_private_mode(self):
        self.root.chmod(0o755)
        with self.assertRaises(DeploymentError):
            check_owner(self.root, expected_root=self.root)

    def test_owner_accepts_actual_owned_private_root(self):
        check_owner(self.root, expected_root=self.root)

    def test_existing_work_directory_refused(self):
        with self.assertRaises(DeploymentError):
            create_private(self.root)

    def test_symlink_work_directory_refused(self):
        link = self.root / 'link'
        link.symlink_to(self.root, target_is_directory=True)
        with self.assertRaises(DeploymentError):
            create_private(link)

    def candidate(self):
        source = self.root / 'source'
        source.mkdir()
        (source / 'one').write_bytes(b'actual-source')
        binary = self.root / 'binary'
        binary.write_bytes(b'actual-binary')
        archive = self.root / 'source.tar'
        archive.write_bytes(b'actual-archive')
        return source, {'product_sha': 'a' * 40, 'test_sha': 'b' * 40,
            'run_id': 'boundary-test', 'binary': str(binary),
            'binary_sha256': digest(binary), 'source_archive': str(archive),
            'source_archive_sha256': digest(archive), 'files': {'one': digest(source / 'one')}}

    def test_candidate_accepts_exact_file_bytes(self):
        source, manifest = self.candidate()
        check_candidate(manifest, source)

    def test_candidate_rejects_binary_changed_one_byte(self):
        source, manifest = self.candidate()
        Path(manifest['binary']).write_bytes(b'actual-binarY')
        with self.assertRaises(DeploymentError):
            check_candidate(manifest, source)

    def test_candidate_rejects_source_changed_one_byte(self):
        source, manifest = self.candidate()
        (source / 'one').write_bytes(b'actual-sourcE')
        with self.assertRaises(DeploymentError):
            check_candidate(manifest, source)

    def test_candidate_rejects_missing_identity(self):
        source, manifest = self.candidate()
        manifest.pop('test_sha')
        with self.assertRaises(DeploymentError):
            check_candidate(manifest, source)

    def test_backup_missing_file_fails(self):
        (self.root / 'data').write_bytes(b'backup bytes')
        expected = file_map(self.root)
        (self.root / 'data').unlink()
        with self.assertRaises(DeploymentError):
            verify_files(self.root, expected)

    def test_backup_same_size_mutation_fails(self):
        (self.root / 'data').write_bytes(b'backup bytes')
        expected = file_map(self.root)
        (self.root / 'data').write_bytes(b'Backup bytes')
        with self.assertRaises(DeploymentError):
            verify_files(self.root, expected)

    def test_backup_extra_file_fails(self):
        expected = file_map(self.root)
        (self.root / 'extra').write_bytes(b'not-in-backup')
        with self.assertRaises(DeploymentError):
            verify_files(self.root, expected)

    def test_backup_symlink_escape_fails(self):
        (self.root / 'link').symlink_to('/etc/passwd')
        with self.assertRaises(DeploymentError):
            file_map(self.root)

    def test_reused_pid_identity_refused(self):
        owned = process_identity(os.getpid())
        owned['starttime'] += 1
        with self.assertRaises(DeploymentError):
            verify_process(owned)

    def test_wrong_process_cwd_refused(self):
        owned = process_identity(os.getpid())
        owned['cwd'] = str(self.root)
        with self.assertRaises(DeploymentError):
            verify_process(owned)

    def test_actual_current_process_identity_matches(self):
        verify_process(process_identity(os.getpid()))

    def test_same_namespace_fails(self):
        with self.assertRaises(DeploymentError):
            assert_namespace({'net': 'net:[1]', 'mnt': 'mnt:[2]'},
                             {'net': 'net:[1]', 'mnt': 'mnt:[3]'}, offline=True)

    def test_same_mount_namespace_fails(self):
        with self.assertRaises(DeploymentError):
            assert_namespace({'net': 'net:[1]', 'mnt': 'mnt:[2]'},
                             {'net': 'net:[3]', 'mnt': 'mnt:[2]'}, offline=False)

    def test_distinct_namespace_accepts(self):
        assert_namespace({'net': 'net:[1]', 'mnt': 'mnt:[2]'},
                         {'net': 'net:[3]', 'mnt': 'mnt:[4]'}, offline=True)

    def test_same_pid_namespace_fails(self):
        with self.assertRaises(DeploymentError):
            assert_namespace({'net': 'net:[1]', 'mnt': 'mnt:[2]', 'pid': 'pid:[1]'},
                             {'net': 'net:[3]', 'mnt': 'mnt:[4]', 'pid': 'pid:[1]'}, offline=True)

    def complete(self):
        return {'phase': 'fresh', 'started_ns': 10, 'ended_ns': 50,
            'required': PHASES['fresh'], 'assertions': [{'name': name, 'started_ns': 20,
            'ended_ns': 30, 'status': 'PASS', 'evidence': 'raw.log',
            'evidence_sha256': 'a' * 64} for name in PHASES['fresh']], 'cleanup_complete': True}

    def test_complete_assertion_accepts(self):
        self.assertEqual(validate_phase(self.complete()), [])

    def test_missing_assertion_cannot_pass(self):
        data = self.complete()
        data['assertions'] = []
        self.assertTrue(validate_phase(data))

    def test_missing_end_time_cannot_pass(self):
        data = self.complete()
        data['assertions'][0].pop('ended_ns')
        self.assertTrue(validate_phase(data))

    def test_reversed_time_cannot_pass(self):
        data = self.complete()
        data['assertions'][0]['ended_ns'] = 19
        self.assertTrue(validate_phase(data))

    def test_missing_raw_evidence_cannot_pass(self):
        data = self.complete()
        data['assertions'][0].pop('evidence_sha256')
        self.assertTrue(validate_phase(data))

    def test_partial_cleanup_cannot_pass(self):
        data = self.complete()
        data['cleanup_complete'] = False
        self.assertTrue(validate_phase(data))

    def test_duplicate_assertion_cannot_pass(self):
        data = self.complete()
        data['assertions'] *= 2
        self.assertTrue(validate_phase(data))

    def test_shrinking_required_contract_cannot_pass(self):
        data = self.complete()
        data['required'] = data['required'][:1]
        data['assertions'] = data['assertions'][:1]
        self.assertTrue(validate_phase(data))

    def test_unknown_phase_cannot_pass(self):
        data = self.complete()
        data['phase'] = 'invented'
        self.assertTrue(validate_phase(data))

    def prepared(self):
        inputs = {'tools': str(self.root / 'tools'), 'tools_files': {},
                  'web_assets': {'/index.html': 'a' * 64, '/assets/app.js': 'b' * 64}}
        Path(inputs['tools']).mkdir()
        for name, basename in (('node', 'node'), ('playwright', 'playwright.js'),
                               ('ui_script', 'ui.mjs'), ('synthetic_fixture', 'synthetic.iso')):
            path = self.root / basename
            path.write_bytes(b'checker-fixture-only')
            inputs[name], inputs[name + '_sha256'] = str(path), digest(path)
        cache = self.root / 'browser-cache'
        cache.mkdir()
        executable = cache / 'chrome-headless-shell'
        executable.write_bytes(b'checker-browser-only')
        inputs.update(browser_path=str(cache), browser_files=file_map(cache),
                      browser_executable=str(executable), browser_executable_sha256=digest(executable))
        return inputs

    def test_linux_explicit_empty_tools_accepts(self):
        inputs = self.prepared()
        self.assertEqual(deployment.prepare_inputs({'deployment': inputs}), inputs)

    def test_tools_missing_field_rejected(self):
        inputs = self.prepared()
        inputs.pop('tools_files')
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def test_tools_missing_directory_rejected(self):
        inputs = self.prepared()
        Path(inputs['tools']).rmdir()
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def test_empty_tools_map_rejects_extra_file(self):
        inputs = self.prepared()
        (Path(inputs['tools']) / 'unexpected').write_bytes(b'extra')
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def test_nonempty_tools_map_missing_file_rejected(self):
        inputs = self.prepared()
        path = Path(inputs['tools']) / 'tool'
        path.write_bytes(b'tool-bytes')
        inputs['tools_files'] = file_map(inputs['tools'])
        path.unlink()
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def test_nonempty_tools_map_changed_bytes_rejected(self):
        inputs = self.prepared()
        path = Path(inputs['tools']) / 'tool'
        path.write_bytes(b'tool-bytes')
        inputs['tools_files'] = file_map(inputs['tools'])
        path.write_bytes(b'Tool-bytes')
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def test_browser_installation_missing_rejected(self):
        inputs = self.prepared()
        inputs.pop('browser_files')
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def test_browser_installation_changed_rejected(self):
        inputs = self.prepared()
        Path(inputs['browser_executable']).write_bytes(b'changed-browser-only')
        with self.assertRaises(DeploymentError):
            deployment.prepare_inputs({'deployment': inputs})

    def copied_runtime(self):
        backup = self.root / 'backup'
        runtime = backup / 'runtime'
        runtime.mkdir(parents=True)
        for name in ('artifacts/vmlinuz', 'sources/local.iso', 'staging/machine-signing.key'):
            path = runtime / name
            path.parent.mkdir(exist_ok=True)
            path.write_bytes(b'original bytes')
        (backup / 'database.dump').write_bytes(b'actual-dump-placeholder-checker-only')
        inventory = self.root / 'backup-files.json'
        inventory.write_text(json.dumps(file_map(backup)))
        copy = self.root / 'new-copy'
        shutil.copytree(runtime, copy)
        return backup, inventory, digest(inventory), copy

    def test_new_runtime_copy_accepts_complete_bytes(self):
        backup, inventory, sha, target = self.copied_runtime()
        deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_new_runtime_copy_kernel_byte_changed_rejected(self):
        backup, inventory, sha, target = self.copied_runtime()
        (target / 'artifacts/vmlinuz').write_bytes(b'Original bytes')
        with self.assertRaises(DeploymentError):
            deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_new_runtime_copy_source_missing_rejected(self):
        backup, inventory, sha, target = self.copied_runtime()
        (target / 'sources/local.iso').unlink()
        with self.assertRaises(DeploymentError):
            deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_new_runtime_copy_extra_file_rejected(self):
        backup, inventory, sha, target = self.copied_runtime()
        (target / 'extra').write_bytes(b'foreign')
        with self.assertRaises(DeploymentError):
            deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_new_runtime_copy_key_changed_rejected(self):
        backup, inventory, sha, target = self.copied_runtime()
        (target / 'staging/machine-signing.key').write_bytes(b'Original bytes')
        with self.assertRaises(DeploymentError):
            deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_restored_copy_rejects_changed_database_dump(self):
        backup, inventory, sha, target = self.copied_runtime()
        (backup / 'database.dump').write_bytes(b'changed-dump')
        with self.assertRaises(DeploymentError):
            deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_restored_copy_rejects_replaced_inventory(self):
        backup, inventory, sha, target = self.copied_runtime()
        inventory.write_text('{}')
        with self.assertRaises(DeploymentError):
            deployment.verify_restored_runtime(backup, inventory, sha, target)

    def test_independent_cleanup_continues_after_first_failure(self):
        visited = []
        class Broken:
            def stop(self):
                visited.append('broken')
                raise DeploymentError('deliberate')
        class Healthy:
            def stop(self):
                visited.append('healthy')
        complete, errors = deployment.cleanup_resources(Broken(), Healthy())
        self.assertFalse(complete)
        self.assertTrue(errors)
        self.assertEqual(visited, ['broken', 'healthy'])

    def test_browser_environment_uses_dev_home_and_frozen_cache(self):
        inputs = self.prepared()
        environment = deployment.browser_environment(inputs)
        import pwd
        self.assertEqual(environment['HOME'], pwd.getpwuid(os.getuid()).pw_dir)
        self.assertEqual(environment['PLAYWRIGHT_BROWSERS_PATH'], inputs['browser_path'])
        self.assertNotIn('LOBER_PG_DSN', environment)


class WebAssetEntries(unittest.TestCase):
    CONFIG = '/_app-config-5.8.0-d956ae99.js'
    ENTRY = '/jse/index-index-sqlNm2DI.js'

    def check_page(self, scripts, *, frozen=None, home=None, head=''):
        page = ('<!doctype html><html><head>' + head + scripts +
                '</head><body><div id="app"></div></body></html>').encode()
        paths = frozen if frozen is not None else (self.CONFIG, self.ENTRY)
        payloads = {path: b'export const checkerFixture = true;' for path in paths}
        payloads['/index.html'] = page
        assets = {path: deployment.hashlib.sha256(data).hexdigest() for path, data in payloads.items()}
        payloads['/'] = page if home is None else home
        # 只替代网络取字节；入口解析、清单绑定和全部摘要核对都用真实检查器。
        with patch.object(deployment, 'fetch', side_effect=lambda service, path: payloads[path]):
            return deployment.web_assets(object(), {'web_assets': assets})

    def test_accepts_observed_vite_module_and_app_config_shape(self):
        try:
            result = self.check_page(f'<script src="{self.CONFIG}"></script>'
                f'<script type="module" crossorigin src="{self.ENTRY}"></script>'
                '<script data-app-loading="inject-js">window.loading=true;</script>')
        except DeploymentError as exc:
            self.fail(str(exc))
        self.assertEqual(result['count'], 3)

    def test_accepts_another_frozen_bundle_directory(self):
        try:
            result = self.check_page('<script type="module" src="/bundles/main.mjs"></script>',
                                     frozen=('/bundles/main.mjs',))
        except DeploymentError as exc:
            self.fail(str(exc))
        self.assertEqual(result['count'], 2)

    def test_rejects_placeholder_with_legacy_assets_marker(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script src="/assets/loading.js"></script><p>placeholder</p>',
                            frozen=('/assets/loading.js',))

    def test_rejects_missing_scripts_even_with_legacy_marker(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<!-- <script src="/assets/not-real.js"></script> -->')

    def test_rejects_inline_module_without_frozen_external_entry(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module">/* assets/ */</script>')

    def test_rejects_unfrozen_application_entry(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module" src="/assets/unfrozen.js"></script>')

    def test_rejects_external_application_entry(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module" src="https://outside.invalid/assets/app.js"></script>')

    def test_rejects_protocol_relative_application_entry(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module" src="//outside.invalid/assets/app.js"></script>')

    def test_rejects_relative_application_entry(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module" src="assets/app.js"></script>', frozen=('/assets/app.js',))

    def test_rejects_unfrozen_config_before_valid_module(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script src="/assets/unfrozen-config.js"></script>'
                            f'<script type="module" src="{self.ENTRY}"></script>')

    def test_rejects_query_or_fragment_on_entry(self):
        for src in ('/assets/app.js?v=1', '/assets/app.js#module'):
            with self.subTest(src=src), self.assertRaises(DeploymentError):
                self.check_page(f'<script type="module" src="{src}"></script>', frozen=('/assets/app.js',))

    def test_rejects_traversal_encoded_or_backslash_entry(self):
        for src in ('/assets/../app.js', '/assets/%2e%2e/app.js', '/assets/\\app.js'):
            with self.subTest(src=src), self.assertRaises(DeploymentError):
                self.check_page(f'<script type="module" src="{src}"></script>')

    def test_rejects_base_that_changes_entry_origin(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module" src="/assets/app.js"></script>',
                            frozen=('/assets/app.js',), head='<base href="https://outside.invalid/">')

    def test_rejects_ambiguous_duplicate_source(self):
        with self.assertRaises(DeploymentError):
            self.check_page('<script type="module" src="https://outside.invalid/assets/app.js" '
                            'src="/assets/app.js"></script>', frozen=('/assets/app.js',))

    def test_rejects_home_bytes_changed_from_frozen_index(self):
        with self.assertRaises(DeploymentError):
            self.check_page(f'<script type="module" src="{self.ENTRY}"></script>',
                            home=b'changed homepage bytes')


class CleanupSourceBoundaries(unittest.TestCase):
    TREEINFO = (b'[general]\nname=Rocky Linux\nversion=9.6\narch=x86_64\n'
                b'packagedir=media/Packages\n')

    def setUp(self):
        self.tmp = tempfile.TemporaryDirectory()
        self.addCleanup(self.tmp.cleanup)
        self.root = Path(self.tmp.name)
        self.runtime = self.root / 'runtime'
        (self.runtime / 'sources').mkdir(parents=True)
        self.fixture = self.root / 'frozen.iso'
        # 检查器用字节；真实冻结ISO另在VM轻量核验，不计安装通过。
        self.fixture.write_bytes(b'\0' * 32769 + b'CD001' + self.TREEINFO + b'\0tail')
        self.frozen_sha = digest(self.fixture)

    def required(self, name):
        fn = getattr(deployment, name, None)
        self.assertTrue(callable(fn), name + ' must enforce the actual cleanup source contract')
        return fn

    def prepare(self):
        return self.required('prepare_cleanup_source')(self.fixture, self.runtime, self.frozen_sha)

    def test_cleanup_derives_only_frozen_unique_treeinfo_equal_length(self):
        before = self.fixture.read_bytes()
        record = self.prepare()
        result = Path(record['path']).read_bytes()
        self.assertEqual(result, before.replace(self.TREEINFO, self.TREEINFO.replace(b'9.6', b'9.7')))
        self.assertEqual(self.fixture.read_bytes(), before)
        self.assertEqual(record['fixture_sha256'], self.frozen_sha)
        self.assertNotEqual(record['sha256'], self.frozen_sha)
        self.assertEqual(record['size'], len(before))
        self.assertEqual(record['version'], '9.7')
        self.assertEqual(record['arch'], 'x86_64')
        self.assertEqual(digest(record['path']), record['sha256'])

    def test_cleanup_rejects_changed_frozen_fixture(self):
        self.fixture.write_bytes(self.fixture.read_bytes() + b'changed')
        with self.assertRaises(DeploymentError):
            self.prepare()

    def test_cleanup_rejects_absent_duplicate_or_incomplete_treeinfo(self):
        for replacement in (b'', self.TREEINFO * 2, self.TREEINFO.replace(b'packagedir=media/Packages', b'packagedir=other')):
            with self.subTest(replacement=replacement):
                self.fixture.write_bytes(b'\0' * 32769 + b'CD001' + replacement + b'\0')
                self.frozen_sha = digest(self.fixture)
                with self.assertRaises(DeploymentError):
                    self.prepare()

    def test_cleanup_rejects_non_iso_or_other_version(self):
        for data in (self.TREEINFO + b'\0', self.fixture.read_bytes().replace(b'9.6', b'9.8')):
            self.fixture.write_bytes(data)
            self.frozen_sha = digest(self.fixture)
            with self.assertRaises(DeploymentError):
                self.prepare()

    def test_cleanup_refuses_existing_destination(self):
        (self.runtime / 'sources/local-cleanup-original.iso').write_bytes(b'keep')
        with self.assertRaises(DeploymentError):
            self.prepare()
        self.assertEqual((self.runtime / 'sources/local-cleanup-original.iso').read_bytes(), b'keep')

    def exercise(self, mutation=None):
        self.runtime = Path(tempfile.mkdtemp(dir=self.root))
        (self.runtime / 'sources').mkdir()
        cleanup = self.prepare()
        sentinel = self.root / 'business-original.iso'
        sentinel.write_bytes(b'original business source')
        expected = {'sentinel':str(sentinel), 'sentinel_sha256':digest(sentinel),
                    'protected':{'/isos/business':{'id':'business','state':'ready'},
                                 '/tasks/old':{'id':'old','state':'pending'},
                                 '/tasks/new':{'id':'new','state':'pending'}}}
        ready = {'id':'cleanup', 'state':'ready', 'version':'9.7', 'arch':'x86_64',
                 'sha256':cleanup['sha256'], 'rev':3}
        rows = [{'iso_id':'cleanup','role':'','kind':'local','path':cleanup['path'],
                 'state':'fetched','fetched':cleanup['size']}]
        calls = []
        test = self

        class FakeService:
            def request(self, method, path, token=None, expected=200):
                calls.append((method,path,expected))
                if path in test.protected:
                    return copy.deepcopy(test.protected[path])
                if method == 'GET' and path == '/isos/cleanup' and expected == 404:
                    if mutation == 'not_deleted':
                        raise DeploymentError('still present')
                    return None
                if method == 'DELETE' and path == '/isos/cleanup?rev=3':
                    return None
                raise AssertionError((method,path,expected))

            def stop(self):
                calls.append(('stop',))

            def start(self):
                calls.append(('start',))
                if mutation == 'source_removed':
                    Path(cleanup['path']).unlink()
                elif mutation == 'source_changed':
                    data = Path(cleanup['path']).read_bytes()
                    Path(cleanup['path']).write_bytes(data[:-1]+b'X')
                elif mutation == 'source_resized':
                    Path(cleanup['path']).write_bytes(Path(cleanup['path']).read_bytes()+b'X')
                elif mutation == 'sentinel_changed':
                    sentinel.write_bytes(b'changed')
                elif mutation in ('business_changed','old_task_changed','new_task_changed'):
                    path = {'business_changed':'/isos/business','old_task_changed':'/tasks/old',
                            'new_task_changed':'/tasks/new'}[mutation]
                    test.protected[path]['state'] = 'changed'

            def login(self, username, password):
                return 'renewed-private-token'

        self.protected = copy.deepcopy(expected['protected'])
        if mutation in ('wrong_kind','wrong_path','wrong_iso','wrong_role','wrong_fetched','duplicate_source'):
            key, value = {'wrong_kind':('kind','upload'), 'wrong_path':('path',str(sentinel)),
                          'wrong_iso':('iso_id','business'), 'wrong_role':('role','extra'),
                          'wrong_fetched':('fetched',1), 'duplicate_source':('kind','local')}[mutation]
            rows[0][key] = value
            if mutation == 'duplicate_source':
                rows.append(dict(rows[0]))
        if mutation in ('not_ready','wrong_version','wrong_arch','wrong_sha'):
            key, value = {'not_ready':('state','failed'), 'wrong_version':('version','9.6'),
                          'wrong_arch':('arch','aarch64'), 'wrong_sha':('sha256','0'*64)}[mutation]
            ready[key] = value
        pg = type('PrivatePG', (), {'root':self.root})()
        with patch.object(deployment, 'import_media', return_value=ready) as importer, \
             patch.object(deployment, 'query_sources', return_value=rows):
            result = self.required('preserve_cleanup_source')(FakeService(), 'private-token', pg, cleanup,
                expected, 'private-password')
        self.assertEqual(importer.call_args.kwargs['local'], Path(cleanup['path']))
        self.assertEqual(result['local_source']['path'], cleanup['path'])
        self.assertEqual(result['deleted_iso_id'], 'cleanup')
        self.assertIn(('DELETE','/isos/cleanup?rev=3',200), calls)
        self.assertIn(('GET','/isos/cleanup',404), calls)
        self.assertLess(calls.index(('stop',)), calls.index(('start',)))

    def test_actual_ready_local_source_is_deleted_and_preserved_after_restart(self):
        self.exercise()

    def test_rejects_wrong_local_source_binding(self):
        for mutation in ('wrong_kind','wrong_path','wrong_iso','wrong_role','wrong_fetched','duplicate_source'):
            with self.subTest(mutation=mutation), self.assertRaises(DeploymentError):
                self.exercise(mutation)

    def test_rejects_not_ready_or_wrong_identified_media(self):
        for mutation in ('not_ready','wrong_version','wrong_arch','wrong_sha'):
            with self.subTest(mutation=mutation), self.assertRaises(DeploymentError):
                self.exercise(mutation)

    def test_rejects_delete_or_restart_source_loss(self):
        for mutation in ('not_deleted','source_removed','source_changed','source_resized'):
            with self.subTest(mutation=mutation), self.assertRaises((DeploymentError, FileNotFoundError)):
                self.exercise(mutation)

    def test_rejects_changed_business_iso_tasks_or_original_sentinel(self):
        for mutation in ('business_changed','old_task_changed','new_task_changed','sentinel_changed'):
            with self.subTest(mutation=mutation), self.assertRaises(DeploymentError):
                self.exercise(mutation)


if __name__ == '__main__':
    unittest.main(verbosity=2)
