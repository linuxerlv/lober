import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';

export type HookItem = Schemas['HookResponse'];
export type HookUpsertParams = Schemas['HookRequest'];
export type HookListParams = Paths['/console/v1/hooks']['get']['query'];
const base = (id: string) => `/hooks/${encodeURIComponent(id)}`;
const version = (id: string, n: number) => `${base(id)}/versions/${n}`;
export const getHookListApi = (params: HookListParams = {}) =>
  requestClient.get<Schemas['HookPage']>('/hooks', { params });
export const createHookApi = (data: Schemas['HookRequest']) =>
  requestClient.post<HookItem>('/hooks', data);
export const getHookApi = (id: string, n: number) =>
  requestClient.get<HookItem>(version(id, n));
export const getHookVersionsApi = (id: string) =>
  requestClient.get<Schemas['VersionPage']>(`${base(id)}/versions`);
export const newHookVersionApi = (id: string, from: number) =>
  requestClient.post<HookItem>(`${base(id)}/versions`, undefined, {
    params: { from },
  });
export const updateHookApi = (
  id: string,
  n: number,
  data: Schemas['HookDraftRequest'],
) => requestClient.put<HookItem>(version(id, n), data);
export const deleteHookApi = (id: string, n: number, rev: number) =>
  requestClient.delete<null>(version(id, n), { params: { rev } });
export const renameHookApi = (id: string, name: string) =>
  requestClient.put<null>(`${base(id)}/name`, { name });
export const validateHookApi = (id: string, n: number) =>
  requestClient.post<Schemas['ValidateResponse']>(`${version(id, n)}/validate`);
export const hookLifecycleApi = (
  id: string,
  n: number,
  rev: number,
  action: 'approve' | 'deprecate' | 'reject' | 'submit',
  reason?: string,
) =>
  requestClient.post<HookItem>(
    `${version(id, n)}/${action}`,
    action === 'reject' ? { reason } : undefined,
    { params: { rev } },
  );
