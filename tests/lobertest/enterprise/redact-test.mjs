import assert from 'node:assert/strict';
import { redact } from './redact.mjs';

const secret = 'fixture-secret-never-publish';
for (const diagnostic of [
  'Authorization: Bearer ' + secret,
  'authorization: bearer ' + secret,
  'GET /stream?access_token=' + secret + '&after_seq=1',
  'request body password=' + secret,
]) {
  // Fail with a fixed message; the regression itself must not print secrets.
  assert.ok(!redact(diagnostic, [secret]).includes(secret), 'Authentication material escaped diagnostic redaction');
}
assert.equal(redact('GET /hosts returned 403'), 'GET /hosts returned 403');
console.log('PASS: bearer, query token and known authentication values redacted');
