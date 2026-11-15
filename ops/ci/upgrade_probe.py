"""在独立数据库上演练上一候选升级与备份恢复。"""
import hashlib
import json
import os
from pathlib import Path
import shutil
import subprocess
import unittest

from db_env import postgres_env
from service_fixture import EnterpriseServiceFixture, ROLES
from upgrade_evidence import BASELINE_IMAGE, BASELINE_SHA, PROOF_PREFIX


def fingerprint(value):
    return hashlib.sha256(json.dumps(value, sort_keys=True, separators=(",", ":")).encode()).hexdigest()


def frozen_binary(sha):
    path = Path('/var/lib/lober-ci-test') / sha
    manifest = json.loads((path / 'candidate.json').read_text())
    binary = path / 'lober'
    if manifest['product_sha'] != sha or manifest['test_sha'] != sha or Path(manifest['binary']) != binary:
        raise ValueError('候选身份不一致')
    with binary.open('rb') as stream:
        digest = hashlib.file_digest(stream, 'sha256').hexdigest()
    if digest != manifest['binary_sha256']:
        raise ValueError('候选程序校验不符')
    print(f'candidate {sha} sha256={digest}', flush=True)
    return binary


class UpgradeProbe(EnterpriseServiceFixture, unittest.TestCase):
    def start_version(self, version):
        if self.image:
            self.__class__.image = version
            self.__class__.container = None
        else:
            self.__class__.binary = version
        self.start()
        self.sign_in()

    def observe(self, phase, snapshot, files):
        artifacts = {str(path): hashlib.sha256((self.work / path).read_bytes()).hexdigest()
                     for path in files}
        self.assertTrue(artifacts, "No real imported artifacts observed")
        return {"phase": phase, "container_id": self.container,
                "image_id": (self.work / "runtime-image.txt").read_text().strip(),
                "snapshot_sha256": fingerprint(snapshot),
                "artifacts_sha256": fingerprint(artifacts), "artifact_count": len(artifacts)}

    def stop_service(self):
        self.proc.terminate()
        self.proc.wait(timeout=25)

    def sign_in(self):
        for role in ROLES:
            username = 'admin' if role == 'admin' else 'enterprise_' + role.replace('-', '_')
            self.tokens[role] = self.login(username, self.password)

    def create_task(self, host, selection):
        data = self.assert_status(self.admin('POST', '/wizard/tasks', {
            **selection, 'hosts': [{'sn': host['sn'], 'entered': {'tpl/hostname': 'upgrade-node'}}]}), 200)
        return data['tasks'][0]['id']

    def snapshot(self, host, selection, task):
        self.assert_status(self.request('POST', '/hosts', self.tokens['viewer'], {'sn': 'FORBIDDEN'}), 403)
        task_data = self.assert_status(self.admin('GET', '/tasks/' + task), 200)
        return {
            'host': self.assert_status(self.admin('GET', '/hosts/' + host['sn']), 200),
            'task': {key: task_data[key] for key in ('state', 'snapshot')},
            'iso': self.assert_status(self.admin('GET', '/isos/' + selection['isoID']), 200)['sha256'],
            'template': self.assert_status(self.admin('GET', '/templates/' + selection['templateID'] + '/versions/1'), 200)['state'],
        }

    def pg(self, argv):
        result = subprocess.run(argv, env=postgres_env(self.base_dsn), capture_output=True, timeout=60)
        self.assertEqual(result.returncode, 0, f'{argv[0]} failed')

    def test_upgrade_and_restore_previous_candidate(self):
        previous = self.image or self.binary
        selection = self.selection()
        host = self.host()
        task = self.create_task(host, selection)
        expected = self.snapshot(host, selection, task)
        self.stop_service()
        dump = self.work / 'upgrade-backup.sql'
        self.pg(['pg_dump', '--no-owner', '--no-privileges', '--schema=' + self.schema, '--file=' + str(dump)])
        backup = self.work / 'upgrade-files'
        directories = tuple(name for name in ('staging', 'artifacts', 'smb') if (self.work / name).is_dir())
        for name in directories:
            shutil.copytree(self.work / name, backup / name, symlinks=True)
        files = {path.relative_to(backup): hashlib.sha256(path.read_bytes()).hexdigest()
                 for path in backup.rglob('*') if path.is_file()}
        before = self.observe("before", expected, files) if self.image else None

        candidate = os.environ["LOBER_UPGRADE_NEXT_IMAGE"] if self.image else frozen_binary(os.environ["LOBER_UPGRADE_NEXT_SHA"])
        self.start_version(candidate)
        upgraded = self.snapshot(host, selection, task)
        self.assertEqual(upgraded, expected)
        for path, digest in files.items():
            self.assertEqual(hashlib.sha256((self.work / path).read_bytes()).hexdigest(), digest)
        new_host = self.host()
        new_task = self.create_task(new_host, selection)
        self.assertNotEqual(new_task, task)
        upgraded_proof = self.observe("upgraded", upgraded, files) if self.image else None
        print('升级后旧数据、权限和制品完整，新任务创建成功', flush=True)

        self.stop_service()
        self.db('DROP SCHEMA ' + self.schema + ' CASCADE')
        self.pg(['psql', '-X', '-v', 'ON_ERROR_STOP=1', '-f', str(dump)])
        for name in directories:
            shutil.rmtree(self.work / name)
            shutil.copytree(backup / name, self.work / name, symlinks=True)
        self.start_version(previous)
        restored = self.snapshot(host, selection, task)
        self.assertEqual(restored, expected)
        self.assert_status(self.admin('GET', '/hosts/' + new_host['sn']), 404)
        gone = self.admin('GET', '/tasks/' + new_task)
        self.assert_status(gone, 404)
        if self.image:
            print(PROOF_PREFIX + json.dumps({"baseline_sha": BASELINE_SHA,
                  "probe_sha256": hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
                  "new_task_sha256": fingerprint(new_task), "rolled_back_status": gone[0],
                  "stages": [before, upgraded_proof, self.observe("restored", restored, files)]}), flush=True)
        print('已恢复升级前数据库及制品，旧程序重新运行', flush=True)


if __name__ == '__main__':
    if os.environ.get("LOBER_UPGRADE_NEXT_IMAGE"):
        os.environ["LOBER_ENTERPRISE_IMAGE"] = BASELINE_IMAGE
        revision = subprocess.check_output(["docker", "image", "inspect", "--format",
            '{{index .Config.Labels "org.opencontainers.image.revision"}}', BASELINE_IMAGE],
            text=True, timeout=30).strip()
        if revision != BASELINE_SHA or os.environ["LOBER_UPGRADE_NEXT_IMAGE"] == BASELINE_IMAGE:
            raise SystemExit("升级起点或目标镜像身份不符")
    else:
        os.environ['LOBER_ENTERPRISE_BINARY'] = str(frozen_binary(os.environ['LOBER_UPGRADE_PREVIOUS_SHA']))
    unittest.main(verbosity=2)
