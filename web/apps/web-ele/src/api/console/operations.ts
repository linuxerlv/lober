import type { RequestClient } from '@vben/request';
import { requestClient } from '#/api/request';
export interface Problem {
  code: string;
  message: string;
  next?: string;
}
export interface Page<T> {
  items: T[];
  next: string;
}
export interface Host {
  sn: string;
  arch: string;
  firmware: string;
  attrs: Record<string, string>;
  rev: number;
}
export interface VersionKey {
  id: string;
  version: number;
  rev: number;
}
export interface HookImpl {
  family: string;
  series: string[];
  kind: string;
  body: string;
  interpreter: string;
  timings: string[];
}
export interface Authoring extends VersionKey {
  name: string;
  state: 'draft' | 'review' | 'approved' | 'deprecated';
  author: string;
  approver: string;
  builtin: boolean;
  description?: string;
  timeoutSec?: number;
  required?: boolean;
  impls?: HookImpl[];
  family?: string;
  series?: string[];
  body?: string;
  params: unknown[];
  bindings?: Record<string, unknown>;
  assembly?: Record<string, unknown[]>;
  problems: Problem[];
}
export interface ISO {
  id: string;
  name: string;
  family: string;
  series: string;
  version: string;
  arch: string;
  state: string;
  rev: number;
  artifacts: Record<string, string>;
}
export interface Task {
  id: string;
  hostSN: string;
  isoID: string;
  templateID: string;
  templateVersion: number;
  state: string;
  createdBy: string;
  createdAt: number;
  rev: number;
  canCancel: Problem[];
  canRetry: Problem[];
  snapshot: {
    host: Host;
    iso: ISO;
    template: { name: string; body: string };
    slots: unknown[];
  };
}
export interface TaskEvent {
  taskID: string;
  seq: number;
  at: number;
  kind: string;
  fields: Record<string, string>;
}
export interface AuditEntry {
  id: number;
  at: number;
  actor: string;
  action: string;
  targetKind: string;
  targetID: string;
  detail: string;
}
export interface Preview {
  files: { kind: string; name: string; body: string; sample: boolean }[];
  problems: Problem[];
  sample: string[];
}
export interface WizardSelection {
  isoID: string;
  templateID: string;
  templateVersion: number;
}
export interface WizardField {
  key: string;
  label: string;
  type: string;
  list: boolean;
  required: boolean;
  locked: boolean;
  prefill: string;
  options: { value: string; label: string }[];
}
type Kind = 'hooks' | 'templates';
type Http = Pick<RequestClient, 'get' | 'post' | 'put' | 'delete'>;
export function createOperationsApi(client: Http) {
  const versionPath = (kind: Kind, item: { id: string; version: number }) =>
    `/${kind}/${encodeURIComponent(item.id)}/versions/${item.version}`;
  return {
    hosts: (params: Record<string, unknown> = {}) =>
      client.get<Page<Host>>('/hosts', { params }),
    createHost: (data: Omit<Host, 'rev'>) => client.post<Host>('/hosts', data),
    updateHost: (data: Host) =>
      client.put<Host>(`/hosts/${encodeURIComponent(data.sn)}`, data),
    deleteHost: (data: Pick<Host, 'sn' | 'rev'>) =>
      client.delete<null>(`/hosts/${encodeURIComponent(data.sn)}`, {
        params: { rev: data.rev },
      }),
    listAuthoring: (kind: Kind, params: Record<string, unknown> = {}) =>
      client.get<Page<Authoring>>(`/${kind}`, { params }),
    versions: (kind: Kind, item: { id: string }) =>
      client.get<{ items: Authoring[] }>(
        `/${kind}/${encodeURIComponent(item.id)}/versions`,
      ),
    createAuthoring: (kind: Kind, data: Record<string, unknown>) =>
      client.post<Authoring>(`/${kind}`, data),
    updateAuthoring: (
      kind: Kind,
      item: VersionKey,
      data: Record<string, unknown>,
    ) =>
      client.put<Authoring>(versionPath(kind, item), {
        ...data,
        rev: item.rev,
      }),
    newVersion: (kind: Kind, item: { id: string; version: number }) =>
      client.post<Authoring>(
        `/${kind}/${encodeURIComponent(item.id)}/versions`,
        undefined,
        { params: { from: item.version } },
      ),
    deleteDraft: (kind: Kind, item: VersionKey) =>
      client.delete<null>(versionPath(kind, item), {
        params: { rev: item.rev },
      }),
    lifecycle: (
      kind: Kind,
      item: VersionKey,
      action: 'submit' | 'approve' | 'reject' | 'deprecate',
      data?: unknown,
    ) =>
      client.post<Authoring>(`${versionPath(kind, item)}/${action}`, data, {
        params: { rev: item.rev },
      }),
    validate: (kind: Kind, item: VersionKey) =>
      client.post<{ ok: boolean; problems: Problem[] }>(
        `${versionPath(kind, item)}/validate`,
      ),
    preview: (item: VersionKey, data: Record<string, unknown> = {}) =>
      client.post<Preview>(`${versionPath('templates', item)}/preview`, data),
    isos: (params: Record<string, unknown> = {}) =>
      client.get<Page<ISO>>('/isos', { params }),
    createISO: (data: Record<string, unknown>) =>
      client.post<ISO>('/isos', data),
    isoProgress: (id: string) =>
      client.get<{
        id: string;
        state: string;
        sources: {
          role: string;
          state: string;
          fetched: number;
          totalSize: number;
          error: string;
        }[];
      }>(`/isos/${encodeURIComponent(id)}/progress`),
    isoAction: (
      item: Pick<ISO, 'id' | 'rev'>,
      action: 'cancel' | 'retry' | 'reimport' | 'deprecate',
    ) =>
      client.post<null>(
        `/isos/${encodeURIComponent(item.id)}/${action}`,
        undefined,
        action === 'deprecate' ? { params: { rev: item.rev } } : undefined,
      ),
    deleteISO: (item: Pick<ISO, 'id' | 'rev'>) =>
      client.delete<null>(`/isos/${encodeURIComponent(item.id)}`, {
        params: { rev: item.rev },
      }),
    registerSource: (id: string, data: unknown) =>
      client.post<null>(`/isos/${encodeURIComponent(id)}/sources`, data),
    tasks: (params: Record<string, unknown> = {}) =>
      client.get<Page<Task>>('/tasks', { params }),
    task: (id: string) => client.get<Task>(`/tasks/${encodeURIComponent(id)}`),
    taskEvents: (id: string, after_seq = 0) =>
      client.get<{ items: TaskEvent[] }>(
        `/tasks/${encodeURIComponent(id)}/events`,
        { params: { after_seq, limit: 200 } },
      ),
    taskAction: (id: string, action: 'cancel' | 'retry') =>
      client.post<Task>(`/tasks/${encodeURIComponent(id)}/${action}`),
    wizardHosts: (params: Record<string, unknown> = {}) =>
      client.get<Page<Host & { selectable: boolean; problems: Problem[] }>>(
        '/wizard/hosts',
        { params },
      ),
    wizardISOs: (hostSNs: string[]) =>
      client.post<Page<ISO & { usable: boolean; problems: Problem[] }>>(
        '/wizard/isos',
        { hostSNs },
      ),
    wizardTemplates: (isoID: string) =>
      client.post<Page<Authoring & { usable: boolean }>>('/wizard/templates', {
        isoID,
      }),
    wizardParams: (selection: WizardSelection, hostSNs: string[]) =>
      client.post<{
        hosts: { sn: string; fields: WizardField[]; problems: Problem[] }[];
        common: Problem[];
      }>('/wizard/params', { ...selection, hostSNs }),
    createTasks: (
      data: WizardSelection & {
        hosts: { sn: string; entered: Record<string, string> }[];
      },
    ) => client.post<{ tasks: Task[] }>('/wizard/tasks', data),
    audit: (params: Record<string, unknown> = {}) =>
      client.get<Page<AuditEntry>>('/audit', { params }),
  };
}
export const operations = createOperationsApi(requestClient);
