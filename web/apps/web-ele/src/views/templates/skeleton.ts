import type { Schemas } from '#/api/console/generated';
import type { getTemplateListApi } from '#/api/console/template';

type Template = Schemas['TemplateResponse'];

export async function loadSkeletons(read: typeof getTemplateListApi) {
  const latest = new Map<string, Template>();
  let cursor = '';
  do {
    const page = await read({
      prefix: 'lober:',
      approved: true,
      cursor,
      limit: 100,
    });
    for (const row of page.items ?? []) {
      if (
        row.builtin &&
        row.state === 'approved' &&
        row.body.trim() &&
        row.version > (latest.get(row.id)?.version ?? 0)
      ) {
        latest.set(row.id, row);
      }
    }
    cursor = page.next;
  } while (cursor);
  return [...latest.values()];
}

export function skeletonContent(source: Template, series: string) {
  if (!source.series?.includes(series)) throw new Error('骨架不支持所选系统');
  return {
    family: source.family,
    series: [series],
    body: source.body,
    params: structuredClone(source.params ?? []),
    bindings: structuredClone(source.bindings ?? {}),
    assembly: {},
  };
}
