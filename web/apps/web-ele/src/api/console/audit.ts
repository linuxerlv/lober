import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';
export type AuditRecordItem = Schemas['AuditItem'];
export type AuditQuery = Paths['/console/v1/audit']['get']['query'];
export type AuditExportQuery =
  Paths['/console/v1/audit/export']['get']['query'];
export function listAudit(params?: AuditQuery) {
  return requestClient.get<Schemas['AuditPage']>('/audit', { params });
}
/** The server exports all matching pages and protects CSV cells. */
export function exportAudit(params?: AuditExportQuery) {
  return requestClient.get<Blob>('/audit/export', {
    params,
    responseType: 'blob',
    responseReturn: 'body',
  });
}
