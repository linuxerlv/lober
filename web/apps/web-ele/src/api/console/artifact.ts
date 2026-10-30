import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';

export type ISOItem = Schemas['IsoResponse'];
export type ISOCreateParams = Schemas['IsoRequest'];
export type ISOSource = Schemas['IsoSourceReq'];
export type ISOProgress = Schemas['IsoProgress'];
export type SourceProgress = Schemas['SourceProgress'];
export type ISOListParams = Paths['/console/v1/isos']['get']['query'];
export type ArtifactSetItem = ISOItem;
export type ArtifactSetCreateParams = ISOCreateParams;

const isoPath = (id: string) => `/isos/${encodeURIComponent(id)}`;
/** Source registration can fail after the ISO record was already persisted. */
export function createdISOIDOf(error: unknown): string | undefined {
  if (!error || typeof error !== 'object' || !('response' in error)) return;
  const response = error.response;
  if (!response || typeof response !== 'object' || !('headers' in response)) return;
  const headers = response.headers;
  if (!headers || typeof headers !== 'object' || !('location' in headers)) return;
  const location = headers.location;
  if (typeof location !== 'string' || !location.startsWith('/console/v1/isos/')) return;
  try { return decodeURIComponent(location.slice('/console/v1/isos/'.length)); } catch { return; }
}
export function getArtifactSetListApi(params: ISOListParams = {}) {
  return requestClient.get<Schemas['IsoPage']>('/isos', { params });
}
export function createArtifactSetApi(data: ISOCreateParams) {
  return requestClient.post<ISOItem>('/isos', data);
}
export function getISOApi(id: string) {
  return requestClient.get<ISOItem>(isoPath(id));
}
export function getISOProgressApi(id: string) {
  return requestClient.get<ISOProgress>(`${isoPath(id)}/progress`);
}
export function registerISOSourceApi(id: string, source: ISOSource) {
  return requestClient.post<null>(`${isoPath(id)}/sources`, source);
}
export function deleteArtifactSetApi(id: string, rev: number) {
  return requestClient.delete<null>(isoPath(id), { params: { rev } });
}
export function deprecateISOApi(id: string, rev: number) {
  return requestClient.post<null>(`${isoPath(id)}/deprecate`, undefined, {
    params: { rev },
  });
}
export function operateISOApi(
  id: string,
  operation: 'cancel' | 'reimport' | 'retry',
) {
  return requestClient.post<null>(`${isoPath(id)}/${operation}`);
}
export function uploadISOChunkApi(
  id: string,
  role: string,
  bytes: Blob,
  start: number,
  total: number,
) {
  return requestClient.put<null>(`${isoPath(id)}/content`, bytes, {
    headers: {
      'Content-Type': 'application/octet-stream',
      'Content-Range': `bytes ${start}-${start + bytes.size - 1}/${total}`,
    },
    params: { role },
    timeout: 15 * 60_000,
  });
}
