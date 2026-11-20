import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';

export type TemplateItem = Schemas['TemplateResponse'];
export type TemplateUpsertParams = Schemas['TemplateRequest'];
export type TemplatePreviewParams = Schemas['PreviewRequest'];
export type TemplatePreviewResult = Schemas['PreviewResponse'];
export type TemplateListParams = Paths['/console/v1/templates']['get']['query'];
const base = (id: string) => `/templates/${encodeURIComponent(id)}`;
const version = (id: string, n: number) => `${base(id)}/versions/${n}`;
export const getTemplateListApi = (params: TemplateListParams = {}) =>
  requestClient.get<Schemas['TemplatePage']>('/templates', { params });
export const createTemplateApi = (data: Schemas['TemplateRequest']) =>
  requestClient.post<TemplateItem>('/templates', data);
export const getTemplateApi = (id: string, n: number) =>
  requestClient.get<TemplateItem>(version(id, n));
export const getTemplateVersionsApi = (id: string) =>
  requestClient.get<Schemas['TemplateVersionPage']>(`${base(id)}/versions`);
export const newTemplateVersionApi = (id: string, from: number) =>
  requestClient.post<TemplateItem>(`${base(id)}/versions`, undefined, {
    params: { from },
  });
export const updateTemplateApi = (
  id: string,
  n: number,
  data: Schemas['TemplateDraftRequest'],
) => requestClient.put<TemplateItem>(version(id, n), data);
export const deriveTemplateApi = (id: string, data: Schemas['DeriveRequest']) =>
  requestClient.post<TemplateItem>(`${base(id)}/derive`, data);
export const deleteTemplateApi = (id: string, n: number, rev: number) =>
  requestClient.delete<null>(version(id, n), { params: { rev } });
export const renameTemplateApi = (id: string, name: string) =>
  requestClient.put<null>(`${base(id)}/name`, { name });
export const validateTemplateApi = (id: string, n: number) =>
  requestClient.post<Schemas['ValidateResponse']>(`${version(id, n)}/validate`);
export const templateLifecycleApi = (
  id: string,
  n: number,
  rev: number,
  action: 'approve' | 'deprecate' | 'reject' | 'submit',
  reason?: string,
) =>
  requestClient.post<TemplateItem>(
    `${version(id, n)}/${action}`,
    action === 'reject' ? { reason } : undefined,
    { params: { rev } },
  );
export const getTemplateSlotsApi = (id: string, n: number) =>
  requestClient.get<Schemas['SlotList']>(`${version(id, n)}/slots`);
export const previewTemplateApi = (
  id: string,
  n: number,
  data: TemplatePreviewParams,
) =>
  requestClient.post<TemplatePreviewResult>(`${version(id, n)}/preview`, data);
export const getPreviewParamsApi = (
  id: string,
  n: number,
  data: TemplatePreviewParams,
) =>
  requestClient.post<Schemas['PreviewParamsResponse']>(
    `${version(id, n)}/preview-params`,
    data,
  );
export const getHookCandidatesApi = (
  id: string,
  n: number,
  params: Paths['/console/v1/templates/{id}/versions/{version}/hook-candidates']['get']['query'],
) =>
  requestClient.get<Schemas['HookCandidatePage']>(
    `${version(id, n)}/hook-candidates`,
    { params },
  );
export const getSourceDiffApi = (id: string, n: number) =>
  requestClient.get<Schemas['SourceDiffResponse']>(
    `${version(id, n)}/source-diff`,
  );
