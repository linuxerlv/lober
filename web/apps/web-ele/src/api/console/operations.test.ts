import { expect, it } from 'vitest';
import { createOperationsApi } from './operations';
it('sends version, revision, host SN and wizard selection using the current contract', async () => {
  const requests: unknown[] = [];
  const record = async <T>(
    method: string,
    path: string,
    data?: unknown,
    config?: unknown,
  ) => {
    requests.push([method, path, data, config]);
    return { items: [], next: '' } as T;
  };
  const api = createOperationsApi({
    get: <T>(p: string, c?: unknown) => record<T>('GET', p, undefined, c),
    post: <T>(p: string, d?: unknown, c?: unknown) =>
      record<T>('POST', p, d, c),
    put: <T>(p: string, d?: unknown, c?: unknown) => record<T>('PUT', p, d, c),
    delete: <T>(p: string, c?: unknown) => record<T>('DELETE', p, undefined, c),
  });
  await api.lifecycle('hooks', { id: 'hook', version: 3, rev: 7 }, 'submit');
  await api.lifecycle(
    'templates',
    { id: 'tpl', version: 2, rev: 9 },
    'reject',
    { reason: 'revise' },
  );
  await api.newVersion('hooks', { id: 'hook', version: 3 });
  await api.deleteHost({ sn: 'SN-1', rev: 4 });
  await api.isoAction({ id: 'iso', rev: 5 }, 'deprecate');
  await api.createTasks({
    isoID: 'iso',
    templateID: 'tpl',
    templateVersion: 2,
    hosts: [{ sn: 'SN-1', entered: { hostname: 'node' } }],
  });
  expect(requests).toEqual([
    [
      'POST',
      '/hooks/hook/versions/3/submit',
      undefined,
      { params: { rev: 7 } },
    ],
    [
      'POST',
      '/templates/tpl/versions/2/reject',
      { reason: 'revise' },
      { params: { rev: 9 } },
    ],
    ['POST', '/hooks/hook/versions', undefined, { params: { from: 3 } }],
    ['DELETE', '/hosts/SN-1', undefined, { params: { rev: 4 } }],
    ['POST', '/isos/iso/deprecate', undefined, { params: { rev: 5 } }],
    [
      'POST',
      '/wizard/tasks',
      {
        isoID: 'iso',
        templateID: 'tpl',
        templateVersion: 2,
        hosts: [{ sn: 'SN-1', entered: { hostname: 'node' } }],
      },
      undefined,
    ],
  ]);
});
