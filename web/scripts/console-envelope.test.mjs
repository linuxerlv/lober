import { test } from 'node:test';
import assert from 'node:assert/strict';
import { normalizeErrorResponseInterceptor } from '../apps/web-ele/src/api/response-interceptor.ts';
test('retain actual server reason on conflict', async () => {
  const error = {
    response: {
      status: 409,
      data: { code: 1005, message: 'server revision explanation' },
    },
  };
  await assert.rejects(
    () => normalizeErrorResponseInterceptor().rejected(error),
    (result) => {
      assert.equal(result.friendlyMessage, 'server revision explanation');
      return true;
    },
  );
});
