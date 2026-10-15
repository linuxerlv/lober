<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';
import { useAccessStore, useUserStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { getHookApi } from '#/api/console/hook';
import { errorMessage, problemsOf } from '#/api/console/problem';
import * as api from '#/api/console/template';
import ProblemList from '#/components/console/ProblemList.vue';

import { authoringRights, replaceInSlot, templatePayload } from './authoring';
import BindingEditor from './BindingEditor.vue';
import ParamEditor from './ParamEditor.vue';
import PreviewFields from './PreviewFields.vue';

defineOptions({ name: 'TemplatesIndex' });
type Template = Schemas['TemplateResponse'];
type Hook = Schemas['HookResponse'];
type Instance = Schemas['InstanceDTO'] & {
  bindings: NonNullable<Schemas['InstanceDTO']['bindings']>;
};
type TemplateForm = Omit<
  Template,
  'assembly' | 'bindings' | 'params' | 'series'
> & {
  assembly: Record<string, Instance[]>;
  bindings: NonNullable<Template['bindings']>;
  params: NonNullable<Template['params']>;
  series: NonNullable<Template['series']>;
};
const access = useAccessStore();
const user = useUserStore();
const canWrite = computed(() => access.accessCodes.includes('template:write'));
const selected = ref<TemplateForm>();
const visible = ref(false);
const busy = ref(false);
const prefix = ref('');
const state = ref('');
const cursor = ref('');
const next = ref('');
const history = ref<string[]>([]);
const errors = ref<Schemas['ProblemJSON'][]>([]);
const versions = ref<Template[]>([]);
const slots = ref<Schemas['SlotInfo'][]>([]);
const hookMap = ref<Record<string, Hook>>({});
const reason = ref('');
const deriveName = ref('');
const sourceDiff = ref<Schemas['SourceDiffResponse']>();
const candidateDialog = ref(false);
const candidateSlot = ref('');
const candidateIndex = ref(0);
const candidatePrefix = ref('');
const candidateNext = ref('');
const candidateRows = ref<Schemas['HookCandidateResponse'][]>([]);
const candidateBusy = ref(false);
const candidateProblems = ref<Schemas['ProblemJSON'][]>([]);
const previewDialog = ref(false);
const previewResult = ref<Schemas['PreviewResponse']>();
const previewFields = ref<Schemas['ParamFieldDTO'][]>([]);
const previewSamples = ref<string[]>([]);
const previewLoading = ref(false);
const previewReady = ref(false);
let previewToken = 0;
const previewForm = ref<Schemas['PreviewRequest']>({
  hostSN: '',
  isoID: '',
  entered: {},
});
const rights = computed(() =>
  selected.value
    ? authoringRights(
        selected.value,
        access.accessCodes,
        user.userInfo?.username ?? '',
        'template',
      )
    : null,
);
const editable = computed(() =>
  !!selected.value && (selected.value.id ? !!rights.value?.edit : canWrite.value),
);
const staleSlots = computed(() => selected.value?.body !== savedBody.value);
const savedBody = ref('');
const unknownSlots = computed(() =>
  Object.keys(selected.value?.assembly ?? {}).filter(
    (name) => !slots.value.some((slot) => slot.name === name),
  ),
);
let listToken = 0;
let editorToken = 0;
let candidateToken = 0;
const [Grid, grid] = useVbenVxeGrid<Template>({
  gridOptions: {
    columns: [
      { field: 'name', title: '名称', minWidth: 200 },
      { field: 'version', title: '版本', width: 80 },
      { field: 'family', title: '族', width: 110 },
      { field: 'state', title: '状态', width: 110 },
      {
        field: 'builtin',
        title: '来源',
        width: 110,
        slots: { default: 'builtin' },
      },
      { field: 'author', title: '作者', width: 110 },
      {
        field: 'action',
        title: '操作',
        width: 140,
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'id' },
  },
});
function report(error: unknown) {
  errors.value = problemsOf(error);
  ElMessage.error(errorMessage(error));
}
function currentEditor(token: number, row: Pick<Template, 'id' | 'version'>) {
  return token === editorToken && visible.value && selected.value?.id === row.id && selected.value.version === row.version;
}
async function load(reset = false) {
  const owner = editorToken;
  if (reset) {
    cursor.value = '';
    history.value = [];
  }
  const token = ++listToken;
  try {
    const page = await api.getTemplateListApi({
      prefix: prefix.value,
      state: state.value || undefined,
      cursor: cursor.value,
      limit: 30,
    });
    if (token !== listToken) return;
    next.value = page.next;
    grid.setGridOptions({ data: page.items ?? [] });
  } catch (error) {
    if (token === listToken && owner === editorToken) report(error);
  }
}
function forward() {
  history.value.push(cursor.value);
  cursor.value = next.value;
  void load();
}
function back() {
  cursor.value = history.value.pop() ?? '';
  void load();
}
function normalize(row: Template): TemplateForm {
  return {
    ...row,
    series: row.series ?? [],
    params: row.params ?? [],
    bindings: row.bindings ?? {},
    assembly: Object.fromEntries(
      Object.entries(row.assembly ?? {}).map(([name, items]) => [
        name,
        (items ?? []).map((item) => ({
          ...item,
          bindings: item.bindings ?? {},
        })),
      ]),
    ),
  };
}
function assign(row: Template) {
  selected.value = normalize(row);
  savedBody.value = row.body;
  errors.value = row.problems ?? [];
  reason.value = '';
}
function hookKey(instance: Schemas['InstanceDTO']) {
  return `${instance.hookID}@${instance.hookVersion}`;
}
function hookOf(instance: Schemas['InstanceDTO']) {
  return hookMap.value[hookKey(instance)];
}
async function refreshDetails(token = editorToken) {
  const row = selected.value;
  if (!row?.id) return;
  const [v, s, d] = await Promise.all([
    api.getTemplateVersionsApi(row.id),
    api.getTemplateSlotsApi(row.id, row.version),
    api.getSourceDiffApi(row.id, row.version),
  ]);
  if (!currentEditor(token, row)) return;
  versions.value = v.items ?? [];
  slots.value = s.slots ?? [];
  sourceDiff.value = d;
  errors.value = [...(row.problems ?? []), ...(s.problems ?? [])];
  for (const slot of slots.value) selected.value!.assembly[slot.name] ??= [];
  const refs = Object.values(selected.value!.assembly).flat();
  const hooks = await Promise.all(
    refs.map(async (instance) => {
      try {
        return await getHookApi(instance.hookID, instance.hookVersion);
      } catch {
        return undefined;
      }
    }),
  );
  if (!currentEditor(token, row)) return;
  hookMap.value = Object.fromEntries(
    hooks.filter((h): h is Hook => !!h).map((h) => [`${h.id}@${h.version}`, h]),
  );
}
async function open(row: Template) {
  const token = ++editorToken;
  candidateDialog.value = false;
  previewDialog.value = false;
  visible.value = true;
  busy.value = true;
  selected.value = undefined;
  slots.value = [];
  sourceDiff.value = undefined;
  try {
    const item = await api.getTemplateApi(row.id, row.version);
    if (token !== editorToken) return;
    assign(item);
    await refreshDetails(token);
  } catch (error) {
    if (token === editorToken) report(error);
  } finally {
    if (token === editorToken) busy.value = false;
  }
}
function create() {
  ++editorToken;
  busy.value = false;
  candidateDialog.value = false;
  previewDialog.value = false;
  assign({
    id: '',
    version: 1,
    rev: 0,
    name: '',
    family: 'linux',
    series: [],
    body: '',
    params: [],
    bindings: {},
    assembly: {},
    author: user.userInfo?.username ?? '',
    approver: '',
    builtin: false,
    state: 'draft',
    derivedFrom: null,
    problems: [],
  });
  slots.value = [];
  versions.value = [];
  sourceDiff.value = undefined;
  visible.value = true;
}
async function save() {
  const token = editorToken;
  const row = selected.value;
  if (!row || !editable.value) return false;
  const data = templatePayload(row);
  const { rev: _rev, ...content } = data;
  const saved = row.id
      ? await api.updateTemplateApi(row.id, row.version, data)
      : await api.createTemplateApi(content);
  if (!currentEditor(token, row)) return false;
  assign(saved);
  await refreshDetails(token);
  if (token !== editorToken) return false;
  await load();
  if (token !== editorToken) return false;
  ElMessage.success('草稿已保存，插槽已由服务端刷新');
  return true;
}
async function run(action: () => Promise<unknown>) {
  const token = editorToken;
  busy.value = true;
  try {
    await action();
  } catch (error) {
    if (token === editorToken && error !== 'cancel' && error !== 'close') report(error);
  } finally {
    if (token === editorToken) busy.value = false;
  }
}
async function changeVersion(n: number) {
  const row = selected.value;
  if (!row) return;
  await open({ ...row, version: n });
}
async function newVersion() {
  const token = editorToken;
  const row = selected.value;
  if (!row) return;
  const changed = await api.newTemplateVersionApi(row.id, row.version);
  if (!currentEditor(token, row)) return;
  assign(changed);
  await refreshDetails(token);
  if (token !== editorToken) return;
  await load();
}
async function derive() {
  const token = editorToken;
  const row = selected.value;
  if (!row || !deriveName.value.trim()) return;
  const changed = await api.deriveTemplateApi(row.id, {
      from: row.version,
      name: deriveName.value.trim(),
    });
  if (!currentEditor(token, row)) return;
  assign(changed);
  deriveName.value = '';
  await refreshDetails(token);
  if (token !== editorToken) return;
  await load();
  if (token !== editorToken) return;
  ElMessage.success('已派生为独立草稿');
}
async function validate() {
  const token = editorToken;
  if (editable.value && !(await save())) return;
  if (token !== editorToken) return;
  const row = selected.value;
  if (!row) return;
  const result = await api.validateTemplateApi(row.id, row.version);
  if (!currentEditor(token, row)) return;
  errors.value = result.problems ?? [];
  if (!errors.value.length) ElMessage.success('校验通过');
}
async function lifecycle(
  action: 'approve' | 'deprecate' | 'reject' | 'submit',
) {
  const token = editorToken;
  if (action === 'reject' && !reason.value.trim()) {
    ElMessage.warning('请填写驳回理由');
    return;
  }
  if (action === 'submit' && !(await save())) return;
  if (token !== editorToken) return;
  const row = selected.value;
  if (!row) return;
  const changed = await api.templateLifecycleApi(
      row.id,
      row.version,
      row.rev,
      action,
      reason.value,
  );
  if (!currentEditor(token, row)) return;
  assign(changed);
  await refreshDetails(token);
  if (token !== editorToken) return;
  await load();
  if (token !== editorToken) return;
  ElMessage.success('状态已更新');
}
async function remove() {
  const token = editorToken;
  const row = selected.value;
  if (!row) return;
  await ElMessageBox.confirm('删除这个草稿版本？', '删除草稿');
  if (!currentEditor(token, row)) return;
  await api.deleteTemplateApi(row.id, row.version, row.rev);
  if (!currentEditor(token, row)) return;
  visible.value = false;
  await load();
}
function move(name: string, index: number, delta: number) {
  const items = selected.value?.assembly[name];
  if (!items) return;
  const target = index + delta;
  if (target < 0 || target >= items.length) return;
  [items[index], items[target]] = [items[target]!, items[index]!];
}
async function pickHook(slot: string, index: number) {
  const token = editorToken;
  candidateSlot.value = slot;
  candidateIndex.value = index;
  candidatePrefix.value = '';
  candidateRows.value = [];
  candidateNext.value = '';
  candidateProblems.value = [];
  if (editable.value && !(await save())) return;
  await nextTick();
  if (token !== editorToken) return;
  candidateDialog.value = true;
  await candidates();
}
async function candidates(cursorValue = '') {
  const owner = editorToken;
  const row = selected.value;
  if (!row) return;
  const token = ++candidateToken;
  candidateBusy.value = true;
  try {
    const page = await api.getHookCandidatesApi(row.id, row.version, {
      slot: candidateSlot.value,
      index: candidateIndex.value + 1,
      prefix: candidatePrefix.value,
      cursor: cursorValue,
      limit: 30,
    });
    if (token !== candidateToken || owner !== editorToken) return;
    candidateRows.value = cursorValue
      ? [...candidateRows.value, ...(page.items ?? [])]
      : (page.items ?? []);
    candidateNext.value = page.next;
  } catch (error) {
    if (token === candidateToken && owner === editorToken) {
      candidateProblems.value = problemsOf(error);
      ElMessage.error(errorMessage(error));
    }
  } finally {
    if (token === candidateToken && owner === editorToken) candidateBusy.value = false;
  }
}
function choose(candidate: Schemas['HookCandidateResponse']) {
  const row = selected.value;
  if (!row || !visible.value || !candidateDialog.value || !candidate.usable) return;
  const items = (row.assembly[candidateSlot.value] ??= []);
  const hook = candidate.hook;
  row.assembly[candidateSlot.value] = replaceInSlot(items, candidateIndex.value, {
    ...hook, params: hook.params ?? [],
  }).map((instance) => ({ ...instance, bindings: instance.bindings ?? {} }));
  hookMap.value[`${hook.id}@${hook.version}`] = hook;
  candidateDialog.value = false;
  ElMessage.success('已更新装配，保存草稿后生效');
}
async function preview() {
  const token = editorToken;
  if (editable.value && !(await save())) return;
  if (token !== editorToken) return;
  previewResult.value = undefined;
  previewForm.value = { hostSN: '', isoID: '', entered: {} };
  previewDialog.value = true;
}
async function renderPreview() {
  const token = editorToken;
  const context = previewToken;
  const row = selected.value;
  if (!row || !previewReady.value) return;
  previewResult.value = undefined;
  errors.value = [];
  const entered = Object.fromEntries(previewFields.value.filter((field) => !field.locked && previewForm.value.entered?.[field.key] !== undefined).map((field) => [field.key, previewForm.value.entered![field.key]!]));
  try {
    const result = await api.previewTemplateApi(row.id, row.version, { ...previewForm.value, entered });
    if (token !== editorToken || context !== previewToken || !previewDialog.value) return;
    previewResult.value = result;
  } catch (error) {
    if (token === editorToken && context === previewToken && previewDialog.value) report(error);
  }
}
async function loadPreviewParams(token: number, owner: number) {
  const row = selected.value;
  if (!row?.id) return;
  const context = { hostSN: previewForm.value.hostSN, isoID: previewForm.value.isoID, series: previewForm.value.series, firmware: previewForm.value.firmware };
  try {
    const result = await api.getPreviewParamsApi(row.id, row.version, context);
    if (token !== previewToken || owner !== editorToken || !previewDialog.value) return;
    previewFields.value = result.fields ?? [];
    previewSamples.value = result.sample ?? [];
    errors.value = result.problems ?? [];
    previewReady.value = true;
  } catch (error) {
    if (token === previewToken && owner === editorToken && previewDialog.value) report(error);
  } finally {
    if (token === previewToken && owner === editorToken) previewLoading.value = false;
  }
}
function comparison(row: null | Template) {
  return row
    ? JSON.stringify(
        {
          family: row.family,
          series: row.series,
          body: row.body,
          params: row.params,
          bindings: row.bindings,
          assembly: row.assembly,
        },
        null,
        2,
      )
    : '';
}
const families: Schemas['CatalogFamily'][] = ['linux', 'windows', 'esxi'];
const allSeries: Schemas['CatalogSeries'][] = [
  'rhel',
  'rocky',
  'almalinux',
  'openeuler',
  'opencloudos',
  'debian',
  'ubuntu',
  'sles15',
  'leap15',
  'sles16',
  'leap16',
  'windows-server',
  'windows-desktop',
  'esxi',
];
onMounted(() => load());
watch(visible, (open) => { if (!open) { ++editorToken; busy.value = false; candidateDialog.value = false; previewDialog.value = false; errors.value = []; } }, { flush: 'sync' });
watch(candidateDialog, (open) => { if (!open) { ++candidateToken; candidateBusy.value = false; } }, { flush: 'sync' });
watch(() => previewForm.value.isoID, () => { previewForm.value.series = undefined; }, { flush: 'sync' });
watch(() => [previewDialog.value, previewForm.value.hostSN, previewForm.value.isoID, previewForm.value.series, previewForm.value.firmware, selected.value?.id, selected.value?.version], (_context, _old, cleanup) => {
  const token = ++previewToken;
  const owner = editorToken;
  previewFields.value = []; previewSamples.value = []; previewForm.value.entered = {};
  previewResult.value = undefined; previewReady.value = false; previewLoading.value = false;
  if (!previewDialog.value || !visible.value) return;
  errors.value = []; previewLoading.value = true;
  const timer = setTimeout(() => { void loadPreviewParams(token, owner); }, 200);
  cleanup(() => clearTimeout(timer));
}, { flush: 'sync' });
onBeforeUnmount(() => { ++editorToken; ++listToken; ++candidateToken; ++previewToken; });
</script>
<template>
  <Page
    title="模板管理与审批"
    description="编辑正文，使用服务端插槽装配 hook，预览完整文件后提交审批。内置模板可派生。"
  >
    <template #extra>
<ElButton v-if="canWrite" type="primary" @click="create">
新建模板
</ElButton>
</template>
    <div class="mb-4 flex gap-3">
      <ElInput
        v-model="prefix"
        clearable
        placeholder="名称前缀"
        @keyup.enter="load(true)"
      /><ElSelect v-model="state" clearable placeholder="所有状态">
<ElOption
          v-for="s in ['draft', 'review', 'approved', 'deprecated']"
          :key="s"
          :label="s"
          :value="s"
/>
</ElSelect><ElButton @click="load(true)">查询</ElButton>
    </div>
    <ProblemList v-if="!visible" :problems="errors" />
    <Grid>
<template #builtin="{ row }">
<ElTag>{{ row.builtin ? '内置' : '自定义' }}</ElTag>
</template><template #action="{ row }">
<ElButton type="primary" link @click="open(row)">
查看 / 编辑
</ElButton>
</template>
</Grid>
    <div class="mt-3 flex justify-end gap-2">
      <ElButton :disabled="!history.length" @click="back">上一页</ElButton><ElButton :disabled="!next" @click="forward">下一页</ElButton>
    </div>
    <ElDialog
      v-model="visible"
      :title="
        selected?.id
          ? `${selected.name} · v${selected.version} · ${selected.state}`
          : '新建模板'
      "
      width="92%"
      top="3vh"
      :close-on-click-modal="false"
    >
      <template v-if="selected">
        <div class="mb-4 flex flex-wrap items-center gap-3">
          <ElTag v-if="selected.builtin">内置只读</ElTag><span>作者：{{ selected.author }} 审批人：{{
              selected.approver || '—'
            }} rev {{ selected.rev }}</span><ElSelect
            v-if="versions.length"
            :model-value="selected.version"
            :disabled="busy"
            class="!w-64"
            @update:model-value="changeVersion"
            >
<ElOption
              v-for="v in versions"
              :key="v.version"
              :label="`v${v.version} · ${v.state}`"
              :value="v.version"
/>
</ElSelect><ElButton
            v-if="rights?.newVersion"
            :disabled="busy"
            @click="run(newVersion)"
            >
基于此版本新建草稿
</ElButton>
        </div>
        <div
          v-if="rights?.derive && selected.id"
          class="mb-4 flex gap-3 rounded border p-3"
        >
          <ElInput
            v-model="deriveName"
            placeholder="派生模板名称（独立副本）"
          /><ElButton
            :disabled="busy || !deriveName.trim()"
            @click="run(derive)"
            >
派生模板
</ElButton>
        </div>
        <ElAlert
          v-if="
            selected.author.trim().toLowerCase() ===
              (user.userInfo?.username ?? '').trim().toLowerCase() &&
            selected.state === 'review'
          "
          title="审批人不能是作者，请由其他审批人审批。"
          type="info"
          :closable="false"
        />
        <ProblemList :problems="errors" />
        <ElForm :disabled="!editable || busy" label-width="100px">
          <ElFormItem label="名称">
<ElInput v-model="selected.name" :disabled="!!selected.id" />
</ElFormItem>
          <ElFormItem label="族">
<ElSelect v-model="selected.family">
<ElOption
                v-for="f in families"
                :key="f"
                :value="f"
                :label="f"
/>
</ElSelect>
</ElFormItem>
          <ElFormItem label="系">
<ElSelect v-model="selected.series" multiple>
<ElOption
                v-for="s in allSeries"
                :key="s"
                :value="s"
                :label="s"
/>
</ElSelect>
</ElFormItem>
          <ElFormItem label="模板正文">
<ElInput
              v-model="selected.body"
              type="textarea"
              :rows="18"
              class="font-mono"
          />
</ElFormItem>
        </ElForm>
        <ParamEditor v-model="selected.params" :disabled="!editable || busy" />
        <h3 class="my-4 text-lg font-semibold">模板参数取值</h3>
        <BindingEditor
          v-model="selected.bindings"
          :params="selected.params"
          :disabled="!editable || busy"
        />
        <h3 class="my-4 text-lg font-semibold">插槽与 hook 装配</h3>
        <ElAlert
          v-if="!selected.id || staleSlots"
          title="保存草稿以刷新服务端插槽。装配和预览使用已保存的版本。"
          type="info"
          :closable="false"
          class="mb-3"
        />
        <ElButton
          v-if="editable"
          :disabled="busy"
          class="mb-3"
          @click="run(save)"
          >
保存并刷新插槽
</ElButton>
        <section
          v-for="slot in slots"
          :key="slot.name"
          class="mb-4 rounded border p-4"
        >
          <h4 class="mb-3 font-semibold">
            {{ slot.name }} ·
            {{ slot.exec ? `执行 / ${slot.timing}` : '片段' }} ·
            {{ slot.line }}:{{ slot.col }}
          </h4>
          <div
            v-for="(instance, index) in selected.assembly[slot.name]"
            :key="`${index}-${hookKey(instance)}`"
            class="mb-3 rounded border p-3"
          >
            <div class="mb-3 flex flex-wrap items-center gap-2">
              <strong>#{{ index + 1 }}
                {{ hookOf(instance)?.name || instance.hookID }} v{{
                  instance.hookVersion
                }}</strong><ElButton
                v-if="editable"
                :disabled="busy || staleSlots"
                @click="run(() => pickHook(slot.name, index))"
                >
替换 hook（保留同名绑定）
</ElButton><ElButton
                v-if="editable"
                :disabled="busy || index === 0"
                @click="move(slot.name, index, -1)"
                >
上移
</ElButton><ElButton
                v-if="editable"
                :disabled="
                  busy ||
                  index === (selected.assembly[slot.name]?.length ?? 0) - 1
                "
                @click="move(slot.name, index, 1)"
                >
下移
</ElButton><ElButton
                v-if="editable"
                :disabled="busy"
                type="danger"
                plain
                @click="selected.assembly[slot.name]?.splice(index, 1)"
                >
移除
</ElButton>
            </div>
            <div
              v-if="slot.exec"
              class="mb-3 flex flex-wrap items-center gap-3"
            >
              <span>超时秒（空为默认）</span><ElInputNumber
                :model-value="instance.timeoutSec ?? undefined"
                :disabled="!editable || busy"
                :min="1"
                @update:model-value="instance.timeoutSec = $event ?? null"
              /><ElSelect
                :model-value="
                  instance.required === null
                    ? 'inherit'
                    : String(instance.required)
                "
                :disabled="!editable || busy"
                class="!w-52"
                @update:model-value="
                  instance.required =
                    $event === 'inherit' ? null : $event === 'true'
                "
                >
<ElOption label="失败处理沿用默认" value="inherit" /><ElOption
                  label="失败终止任务"
                  value="true"
/><ElOption label="失败继续" value="false" />
</ElSelect>
            </div>
            <BindingEditor
              v-model="instance.bindings"
              :params="hookOf(instance)?.params ?? []"
              :disabled="!editable || busy"
            />
          </div>
          <ElButton
            v-if="editable"
            :disabled="busy || staleSlots"
            @click="
              run(() =>
                pickHook(slot.name, selected!.assembly[slot.name]?.length ?? 0),
              )
            "
            >
添加 hook
</ElButton>
          <p
            v-if="!selected.assembly[slot.name]?.length"
            class="text-sm text-gray-500"
          >
            插槽为空
          </p>
        </section>
        <div
          v-for="name in unknownSlots"
          :key="name"
          class="my-3 rounded border border-orange-300 p-3"
        >
          装配中的 {{ name }} 不在服务端插槽列表中。<ElButton
            v-if="editable"
            type="danger"
            link
            @click="delete selected.assembly[name]"
            >
移除该插槽的装配
</ElButton>
        </div>
        <details v-if="sourceDiff?.original" class="my-4 rounded border p-3">
          <summary class="cursor-pointer font-semibold">
            派生来源：{{ sourceDiff.original.name }} v{{
              sourceDiff.original.version
            }}
            ·
            {{
              sourceDiff.changed
                ? `已有 v${sourceDiff.current?.version}，展开查看差异`
                : '暂无新版本'
            }}
          </summary>
          <p class="my-2">
            来源更新不会自动修改当前草稿。左侧为派生时版本，右侧为最新来源版本。
          </p>
          <div class="grid gap-3 lg:grid-cols-2">
            <pre
              class="max-h-96 overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-3 dark:bg-gray-800"
              >{{ comparison(sourceDiff.original) }}</pre>
            <pre
              class="max-h-96 overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-3 dark:bg-gray-800"
              >{{ comparison(sourceDiff.current) }}</pre>
          </div>
        </details>
        <ElInput
          v-if="rights?.review"
          v-model="reason"
          class="my-4"
          type="textarea"
          placeholder="驳回理由（驳回时必填）"
        />
      </template>
      <template #footer>
<div class="flex flex-wrap justify-end gap-2">
          <ElButton @click="visible = false">关闭</ElButton><ElButton
            v-if="editable"
            :loading="busy"
            type="primary"
            @click="run(save)"
            >
保存草稿
</ElButton><ElButton
            v-if="selected?.id && canWrite"
            :disabled="busy"
            @click="run(validate)"
            >
校验
</ElButton><ElButton v-if="selected?.id" :disabled="busy" @click="run(preview)">
预览全部文件
</ElButton><ElButton
            v-if="rights?.submit"
            :disabled="busy"
            @click="run(() => lifecycle('submit'))"
            >
提交审批
</ElButton><ElButton
            v-if="rights?.review"
            :disabled="busy"
            type="success"
            @click="run(() => lifecycle('approve'))"
            >
批准
</ElButton><ElButton
            v-if="rights?.review"
            :disabled="busy || !reason.trim()"
            type="danger"
            @click="run(() => lifecycle('reject'))"
            >
驳回
</ElButton><ElButton
            v-if="rights?.deprecate"
            :disabled="busy"
            type="danger"
            @click="run(() => lifecycle('deprecate'))"
            >
废弃
</ElButton><ElButton
            v-if="rights?.edit && selected?.id"
            :disabled="busy"
            type="danger"
            plain
            @click="run(remove)"
            >
删除草稿
</ElButton>
        </div>
</template>
    </ElDialog>
    <ElDialog
      v-model="candidateDialog"
      :title="`选择 hook · ${candidateSlot} #${candidateIndex + 1}`"
      width="85%"
      top="5vh"
      append-to-body
    >
      <div class="mb-3 flex gap-3">
        <ElInput
          v-model="candidatePrefix"
          placeholder="hook 名称前缀"
          @keyup.enter="candidates()"
        /><ElButton :loading="candidateBusy" @click="candidates()">
查询
</ElButton>
      </div>
      <ProblemList :problems="candidateProblems" />
      <article
        v-for="candidate in candidateRows"
        :key="`${candidate.hook.id}@${candidate.hook.version}`"
        class="mb-3 rounded border p-3"
        :class="{ 'opacity-60': !candidate.usable }"
      >
        <div class="flex items-center justify-between">
          <span>{{ candidate.hook.name }} v{{ candidate.hook.version }} ·
            {{ candidate.hook.state }}</span><ElButton
            :disabled="!candidate.usable || candidateBusy"
            type="primary"
            @click="choose(candidate)"
            >
选用
</ElButton>
        </div>
        <p>{{ candidate.hook.description }}</p>
        <ProblemList :problems="candidate.problems ?? []" />
      </article>
      <ElButton
        v-if="candidateNext"
        :loading="candidateBusy"
        @click="candidates(candidateNext)"
        >
加载更多
</ElButton>
      <p v-if="!candidateRows.length && !candidateBusy">
        没有候选 hook，可先到 hook 管理页新建。
      </p>
    </ElDialog>
    <ElDialog
      v-model="previewDialog"
      title="预览全部文件"
      width="90%"
      top="4vh"
      append-to-body
    >
      <ElForm label-width="150px">
<ElFormItem label="主机 SN（可选）">
<ElInput
            v-model="previewForm.hostSN"
            placeholder="留空使用示例主机"
/>
</ElFormItem><ElFormItem label="ISO ID（可选）">
<ElInput
            v-model="previewForm.isoID"
            placeholder="留空使用示例 ISO，ESXi 需真实 ISO"
/>
</ElFormItem><ElFormItem label="固件">
<ElSelect v-model="previewForm.firmware" clearable>
<ElOption label="UEFI" value="uefi" /><ElOption
              label="BIOS"
              value="bios"
/>
</ElSelect>
</ElFormItem>
        <ElFormItem v-if="!previewForm.isoID" label="示例 ISO 系">
          <ElSelect v-model="previewForm.series" clearable placeholder="默认使用模板首个系">
            <ElOption v-for="series in selected?.series" :key="series" :label="series" :value="series" />
          </ElSelect>
        </ElFormItem>
</ElForm>
      <PreviewFields v-model="previewForm.entered!" :fields="previewFields" :disabled="busy || previewLoading" />
      <ElAlert v-if="previewSamples.length" :title="`参数上下文含示例值：${ previewSamples.join('、')}`" type="info" :closable="false" class="my-3" />

      <ElButton :loading="busy || previewLoading" :disabled="!previewReady" type="primary" @click="run(renderPreview)">
生成预览
</ElButton><ProblemList :problems="previewResult?.problems ?? errors" />
      <ElAlert
        v-if="previewResult?.sample?.length"
        class="my-3"
        :title="`含示例值：${previewResult.sample.join('、')}`"
        type="warning"
        :closable="false"
      />
      <article
        v-for="file in previewResult?.files"
        :key="`${file.kind}:${file.name}:${file.slot ?? ''}`"
        class="my-4 rounded border p-3"
      >
        <h3 class="font-semibold">
          {{ file.name }}
          <ElTag v-if="file.sample" type="warning">含示例值</ElTag>
        </h3>
        <p class="text-sm">{{ file.kind }} {{ file.slot }} {{ file.digest }}</p>
        <pre
          class="mt-2 max-h-[32rem] overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-3 dark:bg-gray-800"
          >{{ file.body }}</pre>
      </article>
    </ElDialog>
  </Page>
</template>
