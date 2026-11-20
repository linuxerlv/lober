import type { Schemas } from './generated';

export type Problem = Schemas['ProblemJSON'] & { host?: string };
export type ReferenceSubject = Schemas['SubjectJSON'];
function object(value: unknown): Record<string, unknown> {
  return value !== null && typeof value === 'object'
    ? (value as Record<string, unknown>)
    : {};
}
function envelope(error: unknown) {
  const source = object(error);
  return object(object(source.response).data ?? source);
}
/** Keep every server reason and its host and field location. */
export function problemsOf(error: unknown): Problem[] {
  const body = envelope(error);
  const detail = object(body.data ?? body);
  const read = (value: unknown): Problem[] =>
    Array.isArray(value)
      ? value.filter((p) => typeof object(p).message === 'string')
      : [];
  return [
    ...read(detail.problems),
    ...read(detail.common),
    ...Object.entries(object(detail.byHost)).flatMap(([host, value]) =>
      read(value).map((p) => ({ ...p, host })),
    ),
  ];
}
export function errorMessage(error: unknown): string {
  const messages = problemsOf(error).map((p) =>
    [p.host, p.subject?.path, p.message, p.next].filter(Boolean).join(' — '),
  );
  if (messages.length) return messages.join('\n');
  const body = envelope(error);
  return typeof body.message === 'string' && body.message
    ? body.message
    : '请求失败';
}

export function referencesOf(error: unknown): ReferenceSubject[] {
  const body = envelope(error);
  const references = object(body.data ?? body).references;
  return Array.isArray(references) ? references : [];
}
