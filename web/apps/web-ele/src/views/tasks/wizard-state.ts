import type { Schemas } from '#/api/console/generated';

export function collectEditable(
  forms: Pick<Schemas['WizardParamsHost'], 'fields' | 'sn'>[],
  values: Record<string, Record<string, string>>,
): Record<string, Record<string, string>> {
  return Object.fromEntries(
    forms.map((host) => [
      host.sn,
      Object.fromEntries(
        (host.fields ?? [])
          .filter((field) => !field.locked)
          .map((field) => [field.key, values[host.sn]?.[field.key] ?? field.prefill]),
      ),
    ]),
  );
}

interface Selection {
  entered: Record<string, Record<string, string>>;
  isoID: string;
  templateID: string;
  templateVersion: number;
}

export function selectionChanged(state: Selection, kind: 'hosts' | 'iso', value = ''): Selection {
  return {
    ...state,
    isoID: kind === 'iso' ? value : '',
    templateID: '',
    templateVersion: 0,
    entered: {},
  };
}

export function latestOnly() {
  let revision = 0;
  return {
    invalidate() {
      revision += 1;
    },
    async run<T>(request: Promise<T>, apply: (result: T) => void, fail?: (error: unknown) => void) {
      const ticket = ++revision;
      try {
        const result = await request;
        if (ticket === revision) apply(result);
      } catch (error) {
        if (ticket === revision) {
          if (fail) fail(error);
          else throw error;
        }
      }
    },
  };
}

export function mergeEvents<T extends Pick<Schemas['EventItem'], 'seq'>>(
  current: T[],
  incoming: T[],
): T[] {
  const events = new Map(current.map((event) => [event.seq, event]));
  for (const event of incoming) events.set(event.seq, event);
  return [...events.values()].sort((a, b) => a.seq - b.seq);
}
