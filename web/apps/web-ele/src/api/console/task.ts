import type { Schemas } from './generated';

import { requestClient } from '#/api/request';

export type Task = Schemas['TaskItem'];
export type TaskEvent = Schemas['EventItem'];
export const taskAPI = {
  list: (params: Record<string, number | string> = {}) =>
    requestClient.get<Schemas['TaskPage']>('/tasks', { params }),
  get: (id: string) => requestClient.get<Task>(`/tasks/${encodeURIComponent(id)}`),
  events: (id: string, after = 0) =>
    requestClient.get<Schemas['EventPage']>(`/tasks/${encodeURIComponent(id)}/events`, {
      params: { after_seq: after, limit: 200 },
    }),
  cancel: (id: string) => requestClient.post<Task>(`/tasks/${encodeURIComponent(id)}/cancel`),
  retry: (id: string) => requestClient.post<Task>(`/tasks/${encodeURIComponent(id)}/retry`),
  preview: (id: string, firmware?: string) =>
    requestClient.post<Schemas['PreviewResponse']>(`/tasks/${encodeURIComponent(id)}/preview`, {
      firmware,
    }),
};

export function taskStreamURL(base: string, id: string, token: string, after: number) {
  return `${base.replace(/\/$/, '')}/tasks/${encodeURIComponent(id)}/stream?${new URLSearchParams({ access_token: token, after_seq: String(after) })}`;
}
