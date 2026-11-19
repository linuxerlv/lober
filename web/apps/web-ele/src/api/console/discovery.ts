import type { HostListItem } from './host';

import { requestClient } from '#/api/request';

export interface DiscoveryComponent {
  fields: Record<string, string>;
  name: string;
}

export interface DiscoverySection {
  items: DiscoveryComponent[];
  key: string;
  reason?: string;
  status: 'absent' | 'partial' | 'read' | 'unread';
  title: string;
}

export interface DiscoveredHost {
  arch: string;
  firmware: string;
  first_seen_at: number;
  last_seen_at: number;
  mac: string;
  reason?: string;
  registered_sn?: string;
  report?: { sections: DiscoverySection[] };
  report_at?: number;
  sn?: string;
  status: 'received' | 'timed_out' | 'unsupported' | 'waiting';
  system_model?: string;
}

interface DiscoveryPage {
  items: DiscoveredHost[];
  next_cursor?: string;
}

export function getDiscoveryListApi(cursor?: string) {
  return requestClient.get<DiscoveryPage>('/discoveries', {
    params: { cursor, limit: 50 },
  });
}

export function getRegisteredDiscoveryApi(sn: string) {
  return requestClient.get<DiscoveryPage>('/discoveries', {
    params: { registered_sn: sn },
  });
}

export function getDiscoveryApi(mac: string) {
  return requestClient.get<DiscoveredHost>(
    `/discoveries/${encodeURIComponent(mac)}`,
  );
}

export function registerDiscoveryApi(mac: string, sn: string) {
  return requestClient.post<HostListItem>(
    `/discoveries/${encodeURIComponent(mac)}/register`,
    { sn },
  );
}
