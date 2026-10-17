import type { Schemas } from '#/api/console/generated';

type VersionMeta = Pick<
  Schemas['TemplateResponse'],
  'author' | 'builtin' | 'state'
>;
type HookContent = Required<Schemas['HookRequest']> & { rev: number };
type TemplateContent = Required<Schemas['TemplateRequest']> & { rev: number };

export function authoringRights(
  row: VersionMeta,
  codes: string[],
  username: string,
  kind: 'hook' | 'template',
) {
  const write = codes.includes(`${kind}:write`);
  const approve = codes.includes(`${kind}:approve`);
  const own = row.author.trim().toLowerCase() === username.trim().toLowerCase();
  return {
    edit: !row.builtin && write && row.state === 'draft',
    submit: !row.builtin && write && row.state === 'draft',
    review: !row.builtin && approve && !own && row.state === 'review',
    deprecate: !row.builtin && approve && row.state === 'approved',
    newVersion:
      !row.builtin && write && ['approved', 'deprecated'].includes(row.state),
    derive: kind === 'template' && write,
  };
}

export function replaceInstance(
  instance: Schemas['InstanceDTO'],
  hook: Pick<Schemas['HookResponse'], 'id' | 'version'> & {
    params: Pick<Schemas['ParamDeclDTO'], 'name'>[];
  },
): Schemas['InstanceDTO'] {
  const names = new Set(hook.params.map((param) => param.name));
  return {
    ...instance,
    hookID: hook.id,
    hookVersion: hook.version,
    bindings: Object.fromEntries(
      Object.entries(instance.bindings ?? {})
        .filter(([name]) => names.has(name))
        .map(([name, binding]) => [name, { ...binding }]),
    ),
  };
}

export function hookPayload(row: HookContent): Schemas['HookDraftRequest'] {
  const { name, description, timeoutSec, required, params, impls, rev } = row;
  return { name, description, timeoutSec, required, params, impls, rev };
}

export function templatePayload(
  row: TemplateContent,
): Schemas['TemplateDraftRequest'] {
  const { name, family, series, body, params, bindings, assembly, rev } = row;
  return { name, family, series, body, params, bindings, assembly, rev };
}

export function newParam(): Schemas['ParamDeclDTO'] {
  return {
    name: '',
    label: '',
    type: 'word',
    list: false,
    required: false,
    default: '',
    options: [],
    min: null,
    max: null,
    offer: '',
  };
}

export function replaceInSlot(
  instances: Schemas['InstanceDTO'][],
  index: number,
  hook: Parameters<typeof replaceInstance>[1],
): Schemas['InstanceDTO'][] {
  const previous = instances[index] ?? {
    hookID: '',
    hookVersion: 1,
    bindings: {},
    required: null,
    timeoutSec: null,
  };
  const result = [...instances];
  result.splice(
    index,
    index < instances.length ? 1 : 0,
    replaceInstance(previous, hook),
  );
  return result;
}
