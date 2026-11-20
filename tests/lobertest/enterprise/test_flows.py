"""Production-browser checks: real form submissions, no mocked responses."""
import json
import os
from pathlib import Path
import secrets
import subprocess
import unittest

from service_fixture import EnterpriseServiceFixture


class BrowserFlowTests(EnterpriseServiceFixture, unittest.TestCase):
    def test_failure_diagnostics_redact_authentication_material(self):
        result = subprocess.run([os.environ['LOBER_ENTERPRISE_NODE'], str(Path(__file__).with_name('redact-test.mjs'))],
                                capture_output=True, text=True, timeout=10)
        self.assertEqual(result.returncode, 0, 'Diagnostic redaction regression failed')
        self.assertIn('PASS:', result.stdout)

    def run_browser(self, script, count, **extra):
        evidence = self.work / script.replace('.mjs', '')
        data = dict(url=self.url, password=self.password, fixture=os.environ['LOBER_ENTERPRISE_FIXTURE'],
                    suffix=secrets.token_hex(4), failureScreenshot=str(evidence) + '.png',
                    tracePath=str(evidence) + '-trace.json', **extra)
        result = subprocess.run([os.environ['LOBER_ENTERPRISE_NODE'],
            str(Path(__file__).with_name(script)), os.environ['LOBER_ENTERPRISE_PLAYWRIGHT']],
            input=json.dumps(data), capture_output=True, text=True, timeout=300)
        self.assertEqual(result.returncode, 0, 'Browser scenario failed: ' + result.stderr[-3000:])
        outcome = json.loads(result.stdout.strip())
        self.assertEqual(outcome['status'], 'PASS')
        self.assertEqual(outcome['count'], count)
        print('Browser scenarios: ' + ', '.join(outcome['cases']), flush=True)

    def approved_hook(self, marker):
        author = self.tokens['hook-author']
        hook = self.assert_status(self.request('POST', '/hooks', author, {
            'name': 'b2_hook_' + marker.lower() + secrets.token_hex(4), 'description': 'Transport/UI test fixture',
            'timeoutSec': 60, 'required': True, 'params': [], 'impls': [
                {'family': 'linux', 'series': ['rocky'], 'kind': 'fragment', 'body': '# UI-HOOK-' + marker + '\n'}]}), 200)
        path = '/hooks/' + hook['id'] + '/versions/1'
        review = self.assert_status(self.request('POST', path + '/submit?rev=' + str(hook['rev']), author), 200)
        return self.assert_status(self.request('POST', path + '/approve?rev=' + str(review['rev']),
                                               self.tokens['hook-approver']), 200)

    def test_complete_business_forms_import_approval_and_batch(self):
        source = self.approved_template()
        template = self.assert_status(self.admin('GET', '/templates/' + source['templateID'] + '/versions/1'), 200)
        hook_a, hook_b = self.approved_hook('A'), self.approved_hook('B')
        self.run_browser('ui-flows.mjs', 4, templateName=template['name'], hookA=hook_a['name'], hookB=hook_b['name'])

    def test_open_page_role_revocation_empty_menu_and_account_switch(self):
        self.run_browser('ui-access.mjs', 3)

    def test_late_real_response_does_not_overwrite_new_resource(self):
        first = self.approved_template()
        a = self.assert_status(self.admin('GET', '/templates/' + first['templateID'] + '/versions/1'), 200)
        # Use a draft derived from the second fixture to give its body an unmistakable marker.
        author = self.tokens['hook-author']
        b = self.assert_status(self.request('POST', '/templates', author, {
            'name': 'second_' + secrets.token_hex(4), 'family': 'linux', 'series': ['rocky'],
            'body': '# SECOND-RESOURCE\n{{lober:syslog}}\n{{lober:firstboot}}\n{{lober:cleanup}}\n',
            'params': []}), 200)
        self.run_browser('ui-stale.mjs', 1, firstID=a['id'], firstName=a['name'], secondName=b['name'])
