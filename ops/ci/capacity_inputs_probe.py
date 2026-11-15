"""让固定候选逐个接收容量测试样本，保存各阶段的输入清单。"""
import hashlib
import json
import os
from pathlib import Path
import time
import unittest

from service_fixture import EnterpriseServiceFixture


class CapacityInputs(EnterpriseServiceFixture, unittest.TestCase):
    def test_candidate_imports_all_fifty_inputs(self):
        directory = Path(os.environ['LOBER_CAPACITY_INPUTS']).resolve(strict=True)
        inputs = sorted(directory.glob('sample-*.iso'))
        self.assertEqual(len(inputs), 50)
        entries, digests, versions = [], set(), set()
        for index, path in enumerate(inputs):
            data = path.read_bytes()
            digest = hashlib.sha256(data).hexdigest()
            self.assertNotIn(digest, digests)
            digests.add(digest)
            item = self.assert_status(self.admin('POST', '/isos', {
                'name': f'capacity-input-{index:02d}', 'family': 'linux', 'series': 'rocky',
                'sources': [{'kind': 'upload', 'size': len(data), 'sha256': digest}],
            }), 200)
            endpoint = '/isos/' + item['id']
            self.assert_status(self.admin('PUT', endpoint + '/content', raw=data, headers={
                'Content-Type': 'application/octet-stream', 'Content-Range': f'bytes 0-{len(data)-1}/{len(data)}',
            }), 200)
            deadline = time.monotonic() + 12
            while time.monotonic() < deadline:
                item = self.assert_status(self.admin('GET', endpoint), 200)
                if item['state'] == 'ready':
                    break
                self.assertNotEqual(item['state'], 'failed', f'导入失败 {path.name}')
                time.sleep(.1)
            else:
                self.fail(f'导入超时 {path.name}')
            self.assertEqual(item['sha256'], digest)
            self.assertEqual(item['version'], f'9.{60+index}')
            self.assertNotIn(item['version'], versions)
            self.assertTrue(item['artifacts'])
            versions.add(item['version'])
            entries.append({'path': str(path), 'size': len(data), 'sha256': digest,
                            'family': 'linux', 'series': 'rocky', 'synthetic': True,
                            'projected_artifact_bytes': 4 * 1024 * 1024})
            print(f'{path.name} ready {item["version"]} {digest}', flush=True)
        with (directory / 'imports.json').open('x', encoding='utf-8') as stream:
            json.dump({'normal': entries[:1], 'burst': entries[1:2], 'soak': entries[2:]}, stream, indent=2)


if __name__ == '__main__':
    unittest.main(verbosity=2)
