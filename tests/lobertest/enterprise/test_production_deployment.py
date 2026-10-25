"""部署检查器反例；只测边界，不把这些测试计作真实部署通过。"""
import copy
import json
import os
from pathlib import Path
import tempfile
import time
import unittest
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


if __name__ == '__main__':
    unittest.main(verbosity=2)
