"""Real PostgreSQL + HTTP event transport; does not qualify machine signals."""
import http.client
import json
import os
from pathlib import Path
import secrets
import subprocess
import unittest
import urllib.parse

from service_fixture import EnterpriseServiceFixture


class EventTests(EnterpriseServiceFixture, unittest.TestCase):
    @classmethod
    def setUpClass(cls):
        super().setUpClass()
        cls.producer = cls.work / 'event-probe'
        built = subprocess.run(['bash', str(Path(__file__).with_name('eventgen.sh')), str(cls.producer)],
                               capture_output=True, text=True, timeout=90)
        if built.returncode:
            raise RuntimeError('Event transport helper build failed: ' + built.stderr[-1200:])

    def task(self):
        if 'event_selection' not in type(self).__dict__:
            type(self).event_selection = self.selection()
        selection = type(self).event_selection
        host = self.host()
        return self.assert_status(self.admin('POST', '/wizard/tasks',
            {**selection, 'hosts': [{'sn': host['sn']}]}), 200)['tasks'][0]

    def append(self, task, start, count, prefix='probe'):
        result = subprocess.run([str(self.producer), str(self.config), task['id'], str(start), str(count), prefix],
                                capture_output=True, text=True, timeout=40)
        self.assertEqual(result.returncode, 0, 'Restricted event producer failed')
        return json.loads(result.stdout)

    def stream(self, task, token=None, after=0, last=None, query_token=False):
        connection = http.client.HTTPConnection('127.0.0.1', self.port, timeout=9)
        self.addCleanup(connection.close)
        headers = {}
        query = {'after_seq': after}
        token = token or self.tokens['viewer']
        if query_token:
            query['access_token'] = token
        else:
            headers['Authorization'] = 'Bearer ' + token
        if last is not None:
            headers['Last-Event-ID'] = str(last)
        connection.request('GET', '/console/v1/tasks/' + task['id'] + '/stream?' + urllib.parse.urlencode(query), headers=headers)
        response = connection.getresponse()
        self.assertEqual(response.status, 200)
        self.assertTrue(response.getheader('Content-Type').startswith('text/event-stream'))
        return response

    def event(self, response):
        identifier, value = None, None
        while True:
            line = response.readline()
            if not line:
                return None
            line = line.decode().strip()
            if line.startswith('id: '):
                identifier = int(line[4:])
            elif line.startswith('data: '):
                value = json.loads(line[6:])
            elif not line and value is not None:
                self.assertEqual(identifier, value['seq'])
                self.assertEqual(value['kind'], 'test.transport')
                return value

    def test_sse_large_backlog_last_event_id_resume_and_store_deduplication(self):
        task = self.task()
        original = self.assert_status(self.admin('GET', '/tasks/' + task['id']), 200)
        rows = self.append(task, 1, 205)
        self.assertEqual([item['Seq'] for item in rows], list(range(1, 206)))
        replay = self.append(task, 1, 205)
        self.assertTrue(all(item['Duplicate'] for item in replay))
        response = self.stream(task, query_token=True)
        self.assertEqual([self.event(response)['seq'] for _ in range(205)], list(range(1, 206)))
        response.close()
        resumed = self.stream(task, after=0, last=200)
        self.assertEqual([self.event(resumed)['seq'] for _ in range(5)], list(range(201, 206)))
        resumed.close()
        persisted = self.assert_status(self.admin('GET', '/tasks/' + task['id'] + '/events?limit=500'), 200)['items']
        self.assertEqual(len(persisted), 205)
        self.assertEqual(len({item['key'] for item in persisted}), 205)
        self.assertEqual(self.assert_status(self.admin('GET', '/tasks/' + task['id']), 200), original)

    def test_sse_recovers_live_events_without_notification_and_no_duplicate_delivery(self):
        task = self.task()
        response = self.stream(task)
        self.append(task, 1, 1)
        self.assertEqual(self.event(response)['seq'], 1)
        self.append(task, 1, 1)
        self.append(task, 2, 1)
        self.assertEqual(self.event(response)['seq'], 2)
        response.close()
        persisted = self.assert_status(self.admin('GET', '/tasks/' + task['id'] + '/events'), 200)['items']
        self.assertEqual([item['seq'] for item in persisted], [1, 2])

    def test_open_sse_closes_after_account_disabled_without_leaking_next_event(self):
        task = self.task()
        name = 'sse_' + secrets.token_hex(4)
        self.assert_status(self.admin('POST', '/users', {'username': name, 'role': 'viewer', 'password': self.password}), 200)
        token = self.login(name, self.password)
        response = self.stream(task, token=token)
        self.assert_status(self.admin('DELETE', '/users/' + name), 200)
        self.append(task, 1, 1)
        self.assertIsNone(self.event(response), 'Disabled subscriber received a later event')
        self.assert_status(self.request('GET', '/tasks/' + task['id'] + '/events', token), 401)

    def test_real_browser_network_disconnect_catches_up_without_duplicate_rows(self):
        task = self.task()
        data = {'url': self.url, 'password': self.password, 'producer': str(self.producer),
                'config': str(self.config), 'taskID': task['id'],
                'tracePath': str(self.work / 'ui-events-trace.json'),
                'failureScreenshot': str(self.work / 'ui-events.png')}
        result = subprocess.run([os.environ['LOBER_ENTERPRISE_NODE'], str(Path(__file__).with_name('ui-events.mjs')),
            os.environ['LOBER_ENTERPRISE_PLAYWRIGHT']], input=json.dumps(data), capture_output=True, text=True, timeout=100)
        self.assertEqual(result.returncode, 0, 'Browser SSE scenario failed: ' + result.stderr[-2500:])
        outcome = json.loads(result.stdout)
        self.assertEqual(outcome['status'], 'PASS')
        self.assertEqual(outcome['count'], 1)
        persisted = self.assert_status(self.admin('GET', '/tasks/' + task['id'] + '/events'), 200)['items']
        self.assertEqual([item['seq'] for item in persisted], [1, 2, 3, 4])
        print('Browser scenario: ' + outcome['cases'][0], flush=True)
