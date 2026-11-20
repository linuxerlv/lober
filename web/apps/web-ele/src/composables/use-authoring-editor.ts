import type { Ref } from 'vue';

import { ref } from 'vue';

import { ElMessage } from 'element-plus';

type Action = 'approve' | 'deprecate' | 'reject' | 'submit';
type Version = { id: string; rev: number; version: number };
type Rights = { deprecate: boolean; review: boolean; submit: boolean };

const permission: Record<Action, keyof Rights> = {
  approve: 'review',
  deprecate: 'deprecate',
  reject: 'review',
  submit: 'submit',
};

interface EditorOptions<T extends Version> {
  assign: (row: T) => void;
  change: (id: string, version: number, rev: number, action: Action, reason: string) => Promise<T>;
  refresh: (token: number) => Promise<unknown>;
  report: (error: unknown) => void;
  rights: Readonly<Ref<null | Rights>>;
  save: () => Promise<boolean>;
  selected: Readonly<Ref<T | undefined>>;
  visible: Readonly<Ref<boolean>>;
}

export function useAuthoringEditor<T extends Version>(options: EditorOptions<T>) {
  const busy = ref(false);
  const reason = ref('');
  let token = 0;

  function invalidate() {
    busy.value = false;
    return ++token;
  }

  function currentEditor(owner: number, row: Pick<Version, 'id' | 'version'>) {
    const selected = options.selected.value;
    return owner === token && options.visible.value &&
      selected?.id === row.id && selected.version === row.version;
  }

  async function run(operation: () => Promise<unknown>) {
    if (busy.value) return;
    const owner = token;
    busy.value = true;
    try {
      await operation();
    } catch (error) {
      if (owner === token && error !== 'cancel' && error !== 'close') options.report(error);
    } finally {
      if (owner === token) busy.value = false;
    }
  }

  async function lifecycle(action: Action) {
    const allowed = () => options.rights.value?.[permission[action]];
    if (!allowed()) return;
    const owner = token;
    if (action === 'reject' && !reason.value.trim()) {
      ElMessage.warning('请填写驳回理由');
      return;
    }
    if (action === 'submit' && !(await options.save())) return;
    if (owner !== token || !allowed()) return;
    const row = options.selected.value;
    if (!row) return;
    const changed = await options.change(row.id, row.version, row.rev, action, reason.value);
    if (!currentEditor(owner, row)) return;
    options.assign(changed);
    await options.refresh(owner);
    if (owner === token) ElMessage.success('状态已更新');
  }

  return {
    busy,
    currentEditor,
    invalidate,
    lifecycle,
    reason,
    run,
    get token() { return token; },
  };
}
