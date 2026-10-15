import { describe, expect, it } from 'vitest';

import {
  normalizeErrorResponseInterceptor,
  statusCodeResponseInterceptor,
} from './response-interceptor';

describe('real console envelopes', () => {
  it('unwraps only the success data and retains pagination', () => {
    const data = { items: [], next: 'opaque cursor' };
    expect(
      statusCodeResponseInterceptor().fulfilled!({
        config: { responseReturn: 'data' },
        status: 200,
        data: { code: 0, message: '', data },
      } as never),
    ).toEqual(data);
  });
  it('keeps server reasons instead of legacy numeric-code text', async () => {
    const error = {
      response: {
        status: 409,
        data: {
          code: 1005,
          message: 'server revision explanation',
          data: { problems: [] },
        },
      },
    };
    try {
      await normalizeErrorResponseInterceptor().rejected!(error);
    } catch (result) {
      expect(result).toMatchObject({
        friendlyMessage: 'server revision explanation',
        response: error.response,
      });
    }
  });
  it('retains JSON server errors from CSV downloads', async () => {
    const error = {
      response: {
        status: 403,
        data: new Blob(
          [JSON.stringify({ code: 403, message: '导出权限不足', data: null })],
          { type: 'application/json' },
        ),
      },
    };
    await expect(
      Promise.resolve().then(() =>
        normalizeErrorResponseInterceptor().rejected!(error),
      ),
    ).rejects.toMatchObject({
      friendlyMessage: '导出权限不足',
      response: { data: { code: 403, message: '导出权限不足' } },
    });
  });
  it('rejects an unsuccessful envelope even if HTTP is 200', () => {
    expect(() =>
      statusCodeResponseInterceptor().fulfilled!({
        config: { responseReturn: 'data' },
        status: 200,
        data: { code: 422, message: 'invalid', data: null },
      } as never),
    ).toThrow();
  });
});
