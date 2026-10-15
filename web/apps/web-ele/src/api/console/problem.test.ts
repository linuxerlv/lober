import { describe, expect, it } from 'vitest';

import { errorMessage, problemsOf } from './problem';
import * as diagnostics from './problem';

const problem = {
  code: 'invalid',
  subject: { kind: 'host', id: 'SN/1', path: 'params.ip', line: 3, col: 2 },
  message: '服务端原因',
  next: '服务端下一步',
};
describe('console diagnostics', () => {
  it('retains reference subjects from rejected deletes', () => {
    const references = [problem.subject];
    expect(
      diagnostics.referencesOf?.({
        response: {
          data: { code: 409, message: '仍被引用', data: { references } },
        },
      }),
    ).toEqual(references);
  });
  it('preserves common and per-host field diagnostics', () => {
    const error = {
      response: {
        data: {
          code: 422,
          message: 'summary',
          data: { common: [problem], byHost: { 'SN/2': [problem] } },
        },
      },
    };
    expect(problemsOf(error)).toEqual([problem, { ...problem, host: 'SN/2' }]);
    expect(errorMessage(error)).toContain(
      'params.ip — 服务端原因 — 服务端下一步',
    );
    expect(errorMessage(error)).toContain('SN/2');
  });
  it('reads normal problem lists and plain errors', () => {
    expect(problemsOf({ data: { problems: [problem] } })).toEqual([problem]);
    expect(errorMessage(new Error('offline'))).toBe('offline');
    expect(
      errorMessage({ response: { data: { message: '权限不足', data: null } } }),
    ).toBe('权限不足');
  });
});
