import type { Paths, Schemas } from './generated';

import { requestClient } from '#/api/request';

export type HostListItem = Schemas['HostResponse'];
export type HostCreateParams = Schemas['HostRequest'];
export type HostListParams = Paths['/console/v1/hosts']['get']['query'];
export type HostPage = Schemas['HostPage'];
export type CSVPlan = Schemas['CsvPlan'];
export type Arch = HostListItem['arch'];
export type Firmware = HostListItem['firmware'];

export function getHostListApi(params: HostListParams = {}) {
  return requestClient.get<HostPage>('/hosts', { params });
}
export function getHostApi(sn: string) {
  return requestClient.get<HostListItem>(`/hosts/${encodeURIComponent(sn)}`);
}
export function createHostApi(data: HostCreateParams) {
  return requestClient.post<HostListItem>('/hosts', data);
}
export function updateHostApi(sn: string, data: HostCreateParams) {
  return requestClient.put<HostListItem>(
    `/hosts/${encodeURIComponent(sn)}`,
    data,
  );
}
export function deleteHostApi(sn: string, rev: number) {
  return requestClient.delete<null>(`/hosts/${encodeURIComponent(sn)}`, {
    params: { rev },
  });
}
export function previewHostCSVApi(bytes: Uint8Array) {
  return requestClient.post<CSVPlan>('/hosts/import', bytes, {
    headers: { 'Content-Type': 'text/csv' },
    params: { dryRun: true },
  });
}
export function commitHostCSVApi(bytes: Uint8Array) {
  return requestClient.post<HostPage>('/hosts/import', bytes, {
    headers: { 'Content-Type': 'text/csv' },
    params: { dryRun: false },
  });
}
