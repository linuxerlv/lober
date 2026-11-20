<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import {
  computed,
  nextTick,
  onBeforeUnmount,
  onMounted,
  reactive,
  ref,
  watch,
} from 'vue';

import { Page } from '@vben/common-ui';
import { useAccessStore, useUserStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDialog,
  ElForm,
  ElFormItem,
  ElInput,
  ElInputNumber,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElStep,
  ElSteps,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { allSeries } from '#/api/console/catalog';
import { getHookApi } from '#/api/console/hook';
import { errorMessage, problemsOf } from '#/api/console/problem';
import * as api from '#/api/console/template';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { downloadSelection } from '#/components/console/selection-export';
import { NumberedPage } from '#/composables/numbered-page';
import { useAuthoringEditor } from '#/composables/use-authoring-editor';
import { useRowSelection } from '#/composables/use-row-selection';

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
const editorSection = ref('content');
const prefix = ref('');
const state = ref('');
const appliedFilters = { prefix: '', state: undefined as string | undefined };
const pagination = reactive(new NumberedPage<Template>());
const selection = useRowSelection(
  (row: Template) => `${row.id}/${row.version}`,
  () => pagination.items,
);
const { rows: selectedRows, all, partial } = selection;
function exportSelected() {
  downloadSelection('模板.csv', [
    ['名称', '编号', '版本', '状态'],
    ...selectedRows.value.map((row) => [
      row.name,
      row.id,
      row.version,
      row.state,
    ]),
  ]);
}
const errors = ref<Schemas['ProblemJSON'][]>([]);
const versions = ref<Template[]>([]);
const slots = ref<Schemas['SlotInfo'][]>([]);
const hookMap = ref<Record<string, Hook>>({});
const deriveName = ref('');
const sourceDiff = ref<Schemas['SourceDiffResponse']>();
const candidateDialog = ref(false);
const candidateSlot = ref('');
const candidateIndex = ref(0);
const candidatePrefix = ref('');
const candidateFilters = { prefix: '' };
const candidatePagination = reactive(
  new NumberedPage<Schemas['HookCandidateResponse']>(),
);
const candidateBusy = computed(() => candidatePagination.loading);
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
const editable = computed(
  () =>
    !!selected.value &&
    (selected.value.id ? !!rights.value?.edit : canWrite.value),
);
const staleSlots = computed(() => selected.value?.body !== savedBody.value);
const savedBody = ref('');
const unknownSlots = computed(() =>
  Object.keys(selected.value?.assembly ?? {}).filter(
    (name) => !slots.value.some((slot) => slot.name === name),
  ),
);
let listToken = 0;
let candidateToken = 0;
const [Grid, grid] = useVbenVxeGrid<Template>({
  class: 'h-auto',
  gridOptions: {
    columns: [
      { width: 48, slots: { header: 'select-all', default: 'select-row' } },
      { field: 'name', title: '模板名称', minWidth: 220 },
      { field: 'version', title: '版本', width: 80 },
      { field: 'family', title: '系统类型', width: 110 },
      {
        field: 'state',
        title: '审批状态',
        width: 120,
        slots: { default: 'state' },
      },
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
    maxHeight: 520,
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'id' },
  },
});
function report(error: unknown) {
  errors.value = problemsOf(error);
  ElMessage.error(errorMessage(error));
}
const editor = useAuthoringEditor<Template>({
  assign,
  change: api.templateLifecycleApi,
  refresh: async (token) => {
    await refreshDetails(token);
    if (token === editor.token) await load();
  },
  report,
  rights,
  save,
  selected,
  visible,
});
const { busy, currentEditor, lifecycle, reason, run } = editor;
async function load(reset = false) {
  const owner = editor.token;
  if (reset) {
    selection.clear();
    Object.assign(appliedFilters, {
      prefix: prefix.value,
      state: state.value || undefined,
    });
    pagination.reset();
  }
  const token = ++listToken;
  const filters = { ...appliedFilters };
  try {
    const page = await pagination.load((params) =>
      api.getTemplateListApi({ ...filters, ...params }),
    );
    if (token !== listToken) return;
    selection.refresh(page.items);
    grid.setGridOptions({ data: page.items });
  } catch (error) {
    if (token === listToken && owner === editor.token) report(error);
  }
}
function resetFilters() {
  prefix.value = '';
  state.value = '';
  void load(true);
}
function changePage(page: number) {
  pagination.setPage(page);
  void load();
}
function changePageSize(size: number) {
  pagination.setPageSize(size);
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
async function refreshDetails(token = editor.token) {
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
const templateSteps = ['basic', 'content', 'params', 'assembly'] as const;
const templateStepIndex = computed(() =>
  Math.max(0, templateSteps.indexOf(editorSection.value as (typeof templateSteps)[number])),
);
function setTemplateStep(idx: number) {
  const next = templateSteps[idx];
  if (next) editorSection.value = next;
}
async function open(row: Template) {
  editorSection.value = 'content';
  const token = editor.invalidate();
  candidateDialog.value = false;
  previewDialog.value = false;
  visible.value = true;
  selected.value = undefined;
  slots.value = [];
  sourceDiff.value = undefined;
  await run(async () => {
    const item = await api.getTemplateApi(row.id, row.version);
    if (token !== editor.token) return;
    assign(item);
    await refreshDetails(token);
  });
}
function create() {
  editorSection.value = 'basic';
  editor.invalidate();
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
  const token = editor.token;
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
  if (token !== editor.token) return false;
  await load();
  if (token !== editor.token) return false;
  ElMessage.success('草稿已保存，插槽已由服务端刷新');
  return true;
}
async function changeVersion(n: number) {
  const row = selected.value;
  if (!row) return;
  await open({ ...row, version: n });
}
async function newVersion() {
  if (!rights.value?.newVersion) return;
  const token = editor.token;
  const row = selected.value;
  if (!row) return;
  const changed = await api.newTemplateVersionApi(row.id, row.version);
  if (!currentEditor(token, row)) return;
  assign(changed);
  await refreshDetails(token);
  if (token !== editor.token) return;
  await load();
}
async function derive() {
  if (!rights.value?.derive) return;
  const token = editor.token;
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
  if (token !== editor.token) return;
  await load();
  if (token !== editor.token) return;
  ElMessage.success('已派生为独立草稿');
}
async function validate() {
  const token = editor.token;
  if (editable.value && !(await save())) return;
  if (token !== editor.token) return;
  const row = selected.value;
  if (!row) return;
  const result = await api.validateTemplateApi(row.id, row.version);
  if (!currentEditor(token, row)) return;
  errors.value = result.problems ?? [];
  if (!errors.value.length) ElMessage.success('校验通过');
}
async function remove() {
  const token = editor.token;
  const row = selected.value;
  if (!row) return;
  await ElMessageBox.confirm('删除这个草稿版本？', '删除草稿');
  if (!currentEditor(token, row) || !rights.value?.edit) return;
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
  const token = editor.token;
  candidateSlot.value = slot;
  candidateIndex.value = index;
  candidatePrefix.value = '';
  candidatePagination.reset();
  candidatePagination.items = [];
  candidatePagination.total = 0;
  candidateProblems.value = [];
  if (editable.value && !(await save())) return;
  await nextTick();
  if (token !== editor.token) return;
  candidateDialog.value = true;
  await candidates(true);
}
async function candidates(reset = false) {
  const owner = editor.token;
  const row = selected.value;
  if (!row) return;
  if (reset) {
    candidateFilters.prefix = candidatePrefix.value;
    candidatePagination.reset();
  }
  const token = ++candidateToken;
  const filters = {
    slot: candidateSlot.value,
    index: candidateIndex.value + 1,
    prefix: candidateFilters.prefix,
  };
  candidateProblems.value = [];
  try {
    await candidatePagination.load((params) =>
      api.getHookCandidatesApi(row.id, row.version, {
        ...filters,
        ...params,
      }),
    );
  } catch (error) {
    if (token === candidateToken && owner === editor.token) {
      candidateProblems.value = problemsOf(error);
      ElMessage.error(errorMessage(error));
    }
  }
}
function changeCandidatePage(page: number) {
  candidatePagination.setPage(page);
  void candidates();
}
function changeCandidatePageSize(size: number) {
  candidatePagination.setPageSize(size);
  void candidates();
}
function choose(candidate: Schemas['HookCandidateResponse']) {
  const row = selected.value;
  if (!row || !visible.value || !candidateDialog.value || !candidate.usable)
    return;
  const items = (row.assembly[candidateSlot.value] ??= []);
  const hook = candidate.hook;
  row.assembly[candidateSlot.value] = replaceInSlot(
    items,
    candidateIndex.value,
    {
      ...hook,
      params: hook.params ?? [],
    },
  ).map((instance) => ({ ...instance, bindings: instance.bindings ?? {} }));
  hookMap.value[`${hook.id}@${hook.version}`] = hook;
  candidateDialog.value = false;
  ElMessage.success('已更新装配，保存草稿后生效');
}
async function preview() {
  const token = editor.token;
  if (editable.value && !(await save())) return;
  if (token !== editor.token) return;
  previewResult.value = undefined;
  previewForm.value = { hostSN: '', isoID: '', entered: {} };
  previewDialog.value = true;
}
async function renderPreview() {
  const token = editor.token;
  const context = previewToken;
  const row = selected.value;
  if (!row || !previewReady.value) return;
  previewResult.value = undefined;
  errors.value = [];
  const entered = Object.fromEntries(
    previewFields.value
      .filter(
        (field) =>
          !field.locked && previewForm.value.entered?.[field.key] !== undefined,
      )
      .map((field) => [field.key, previewForm.value.entered![field.key]!]),
  );
  try {
    const result = await api.previewTemplateApi(row.id, row.version, {
      ...previewForm.value,
      entered,
    });
    if (
      token !== editor.token ||
      context !== previewToken ||
      !previewDialog.value
    )
      return;
    previewResult.value = result;
  } catch (error) {
    if (
      token === editor.token &&
      context === previewToken &&
      previewDialog.value
    )
      report(error);
  }
}
async function loadPreviewParams(token: number, owner: number) {
  const row = selected.value;
  if (!row?.id) return;
  const context = {
    hostSN: previewForm.value.hostSN,
    isoID: previewForm.value.isoID,
    series: previewForm.value.series,
    firmware: previewForm.value.firmware,
  };
  try {
    const result = await api.getPreviewParamsApi(row.id, row.version, context);
    if (
      token !== previewToken ||
      owner !== editor.token ||
      !previewDialog.value
    )
      return;
    previewFields.value = result.fields ?? [];
    previewSamples.value = result.sample ?? [];
    errors.value = result.problems ?? [];
    previewReady.value = true;
  } catch (error) {
    if (token === previewToken && owner === editor.token && previewDialog.value)
      report(error);
  } finally {
    if (token === previewToken && owner === editor.token)
      previewLoading.value = false;
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
const templateSkeletons: Record<string, string> = {
  kickstart: `#version=RHEL9
text
reboot --eject
lang zh_CN.UTF-8
keyboard us
timezone Asia/Shanghai --utc
bootloader --location=mbr --boot-drive={{ .target_disk }}
clearpart --all --initlabel --drives={{ .target_disk }}
autopart --type=lvm

# {{lober:slot packages}}
%packages
@^minimal-environment
chrony
%end

# {{lober:slot post-target exec=post-target}}
# {{lober:slot firstboot exec=firstboot}}
`,
  autoinstall: `#cloud-config
autoinstall:
  version: 1
  locale: zh_CN.UTF-8
  keyboard: { layout: us }
  identity:
    hostname: {{ .hostname }}
    username: ops
  storage:
    layout:
      name: lvm
      match:
        path: /dev/{{ .target_disk }}
# {{lober:slot post-target exec=post-target}}
`,
  esxi: `vmaccepteula
install --firstdisk=local --overwritevmfs
network --bootproto=dhcp
reboot
`,
};
function applyTemplateSkeleton(kind: 'autoinstall' | 'esxi' | 'kickstart') {
  if (!selected.value || !editable.value || busy.value) return;
  selected.value.body = templateSkeletons[kind] ?? '';
  if (kind === 'kickstart') {
    selected.value.family = 'linux';
    if (!selected.value.series.length) selected.value.series = ['rocky'];
  } else if (kind === 'autoinstall') {
    selected.value.family = 'linux';
    selected.value.series = ['ubuntu'];
  } else {
    selected.value.family = 'esxi';
    selected.value.series = ['esxi'];
  }
}
function insertTemplateSnippet(snippet: string) {
  if (!selected.value || !editable.value || busy.value) return;
  const current = selected.value.body;
  selected.value.body = current
    ? `${current.replace(/\s*$/, '')}\n${snippet}\n`
    : `${snippet}\n`;
}
const authoringStateLabels: Record<string, string> = {
  draft: '草稿',
  review: '待审批',
  approved: '已批准',
  deprecated: '已废弃',
};
const authoringStateTagType: Record<
  string,
  'danger' | 'info' | 'primary' | 'success' | 'warning'
> = {
  draft: 'info',
  review: 'warning',
  approved: 'success',
  deprecated: 'danger',
};
onMounted(() => load());
watch(
  visible,
  (open) => {
    if (!open) {
      editor.invalidate();
      candidateDialog.value = false;
      previewDialog.value = false;
      errors.value = [];
    }
  },
  { flush: 'sync' },
);
watch(
  candidateDialog,
  (open) => {
    if (!open) {
      ++candidateToken;
      candidatePagination.reset();
    }
  },
  { flush: 'sync' },
);
watch(
  () => previewForm.value.isoID,
  () => {
    previewForm.value.series = undefined;
  },
  { flush: 'sync' },
);
watch(
  () => [
    previewDialog.value,
    previewForm.value.hostSN,
    previewForm.value.isoID,
    previewForm.value.series,
    previewForm.value.firmware,
    selected.value?.id,
    selected.value?.version,
  ],
  (_context, _old, cleanup) => {
    const token = ++previewToken;
    const owner = editor.token;
    previewFields.value = [];
    previewSamples.value = [];
    previewForm.value.entered = {};
    previewResult.value = undefined;
    previewReady.value = false;
    previewLoading.value = false;
    if (!previewDialog.value || !visible.value) return;
    errors.value = [];
    previewLoading.value = true;
    const timer = setTimeout(() => {
      void loadPreviewParams(token, owner);
    }, 200);
    cleanup(() => clearTimeout(timer));
  },
  { flush: 'sync' },
);
onBeforeUnmount(() => {
  editor.invalidate();
  ++listToken;
  ++candidateToken;
  ++previewToken;
  pagination.reset();
  candidatePagination.reset();
});
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
    <section class="overflow-hidden rounded-lg border border-border bg-card">
      <div class="flex flex-wrap items-end gap-3 px-5 py-4">
        <ElInput
          v-model="prefix"
          class="!w-52"
          clearable
          placeholder="名称前缀"
          @keyup.enter="load(true)"
        /><ElSelect
          v-model="state"
          class="!w-36"
          clearable
          placeholder="所有状态"
        >
          <ElOption
            v-for="s in ['draft', 'review', 'approved', 'deprecated']"
            :key="s"
            :label="`${authoringStateLabels[s]} (${s})`"
            :value="s"
          />
        </ElSelect>
        <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
          <ElButton :loading="pagination.loading" @click="load(true)">
查询
</ElButton>
          <ElButton class="!ml-0" @click="resetFilters">重置</ElButton>
          <ElButton
            :disabled="pagination.loading || !selectedRows.length"
            @click="exportSelected"
            >
导出所选
</ElButton>
        </div>
      </div>
      <ProblemList v-if="!visible" :problems="errors" />
      <Grid>
        <template #select-all>
          <ElCheckbox
            aria-label="选择本页模板"
            :model-value="all"
            :indeterminate="partial"
            :disabled="pagination.loading || !pagination.items.length"
            @change="selection.togglePage"
          />
        </template>
        <template #select-row="{ row }">
          <ElCheckbox
            :aria-label="`选择 ${ row.name}`"
            :model-value="selection.has(row)"
            :disabled="pagination.loading"
            @change="selection.toggle(row, $event)"
          />
        </template>
        <template #state="{ row }">
          <ElTag :type="authoringStateTagType[row.state] || 'info'">
            {{ authoringStateLabels[row.state] || row.state }}
          </ElTag>
        </template>
        <template #builtin="{ row }">
          <ElTag :type="row.builtin ? 'info' : 'primary'" effect="plain">
            {{ row.builtin ? '系统内置' : '自定义' }}
          </ElTag>
        </template><template #action="{ row }">
          <ElButton type="primary" link @click="open(row)">
            查看 / 编辑
          </ElButton>
        </template>
      </Grid>
      <div class="px-4 pb-3">
        <ListPagination
          :current-page="pagination.page"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          :page-count="pagination.items.length"
          :selected-count="selectedRows.length"
          :disabled="pagination.loading"
          @page-change="changePage"
          @size-change="changePageSize"
        />
      </div>
    </section>
    <ElDialog
      v-model="visible"
      :title="
        selected?.id
          ? `${selected.name} · v${selected.version} · ${selected.state}`
          : '新建模板'
      "
      width="min(980px, 94vw)"
      top="3vh"
      :close-on-click-modal="false"
    >
      <template v-if="selected">
        <div class="mb-4 flex flex-wrap items-center gap-3">
          <ElTag v-if="selected.builtin">内置只读</ElTag><span>作者：{{ selected.author }} 审批人：{{
              selected.approver || '—'
            }}
            rev {{ selected.rev }}</span><ElSelect
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
        <!-- 4 步向导步骤条：支持点击任意步骤，也支持底部“上一步/下一步”引导 -->
        <ElSteps
          :active="templateStepIndex"
          finish-status="success"
          class="mb-4 rounded-xl border border-border bg-muted/20 p-4"
        >
          <ElStep
            title="1. 基本属性与骨架"
            description="名称、系统类型与起步模板"
            class="cursor-pointer"
            @click="setTemplateStep(0)"
          />
          <ElStep
            title="2. 模板正文与插槽"
            description="编写应答配置与插槽标记"
            class="cursor-pointer"
            @click="setTemplateStep(1)"
          />
          <ElStep
            title="3. 参数声明与取值"
            description="定义装机参数与默认绑定"
            class="cursor-pointer"
            @click="setTemplateStep(2)"
          />
          <ElStep
            title="4. 插槽脚本装配"
            description="挂载 hook 脚本并预览提交"
            class="cursor-pointer"
            @click="setTemplateStep(3)"
          />
        </ElSteps>
        <div class="max-h-[56dvh] overflow-auto px-1">
          <!-- 第 1 步：基本属性与起步骨架 -->
          <section
            v-show="editorSection === 'basic'"
            class="grid gap-4 rounded-xl border border-border bg-muted/20 p-5"
          >
            <div class="grid grid-cols-1 gap-4 sm:grid-cols-3">
              <label class="text-xs sm:col-span-3">
                <span class="mb-1.5 block font-medium text-foreground">
                  模板名称 <span class="text-red-500">*</span>
                </span>
                <ElInput
                  v-model="selected.name"
                  placeholder="例如 Rocky Linux 9 生产计算节点标准模板"
                  :disabled="!editable || busy || !!selected.id"
                />
              </label>
              <label class="text-xs">
                <span class="mb-1.5 block font-medium text-foreground">
                  操作系统类型（族）
                </span>
                <ElSelect
                  v-model="selected.family"
                  class="w-full"
                  :disabled="!editable || busy"
                >
                  <ElOption
                    v-for="f in families"
                    :key="f"
                    :value="f"
                    :label="f"
                  />
                </ElSelect>
              </label>
              <label class="text-xs sm:col-span-2">
                <span class="mb-1.5 block font-medium text-foreground">
                  适用发行系列（系，可多选）
                </span>
                <ElSelect
                  v-model="selected.series"
                  multiple
                  placeholder="选择适用的发行版"
                  class="w-full"
                  :disabled="!editable || busy"
                >
                  <ElOption
                    v-for="s in allSeries"
                    :key="s"
                    :value="s"
                    :label="s"
                  />
                </ElSelect>
              </label>
            </div>
            <div v-if="editable">
              <span class="mb-2 block text-xs font-medium text-foreground">
                选择起步模板骨架（点击后自动预填系统系列与应答正文，随后进入第 2 步微调）
              </span>
              <div class="grid grid-cols-1 gap-3 sm:grid-cols-3">
                <button
                  v-for="preset in [
                    {
                      key: 'kickstart',
                      title: 'Rocky / RHEL Kickstart 骨架',
                      desc: '预置 LVM 分区、目标盘变量与软件包/装后/首次启动三个标准插槽',
                    },
                    {
                      key: 'autoinstall',
                      title: 'Ubuntu Autoinstall 骨架',
                      desc: '预置 Cloud-Init Autoinstall 存储布局、主机名变量与装后脚本插槽',
                    },
                    {
                      key: 'esxi',
                      title: 'VMware ESXi 应答骨架',
                      desc: '预置 ESXi 自动接受协议、首盘安装与网络引导重启配置',
                    },
                  ]"
                  :key="preset.key"
                  type="button"
                  :disabled="busy"
                  class="rounded-xl border border-border bg-card p-4 text-left transition hover:border-primary/50 hover:bg-primary/5"
                  @click="
                    applyTemplateSkeleton(
                      preset.key as 'autoinstall' | 'esxi' | 'kickstart',
                    );
                    setTemplateStep(1);
                  "
                >
                  <div class="text-sm font-semibold text-foreground">
                    {{ preset.title }}
                  </div>
                  <div class="mt-1 text-xs leading-relaxed text-muted-foreground">
                    {{ preset.desc }}
                  </div>
                </button>
              </div>
            </div>
          </section>

          <!-- 第 3 步：参数声明与取值 -->
          <section v-show="editorSection === 'params'">
            <ParamEditor
              v-model="selected.params"
              :disabled="!editable || busy"
            />
            <h3 class="my-4 text-lg font-semibold">模板参数取值</h3>
            <BindingEditor
              v-model="selected.bindings"
              :params="selected.params"
              :disabled="!editable || busy"
            />
          </section>

          <!-- 第 2 步：模板正文与插槽 -->
          <section v-show="editorSection === 'content'">
            <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
              <h3 class="font-semibold">模板正文</h3>
              <div
                v-if="editable"
                class="flex flex-wrap items-center gap-1.5 text-xs"
              >
                <span class="text-muted-foreground">一键载入骨架：</span>
                <ElButton
                  size="small"
                  :disabled="busy"
                  @click="applyTemplateSkeleton('kickstart')"
                >
                  Rocky/RHEL Kickstart
                </ElButton>
                <ElButton
                  size="small"
                  :disabled="busy"
                  @click="applyTemplateSkeleton('autoinstall')"
                >
                  Ubuntu Autoinstall
                </ElButton>
                <ElButton
                  size="small"
                  :disabled="busy"
                  @click="applyTemplateSkeleton('esxi')"
                >
                  ESXi 应答骨架
                </ElButton>
              </div>
            </div>
            <div
              v-if="editable"
              class="mb-2 flex flex-wrap items-center gap-1.5 rounded-lg border border-border bg-muted/30 px-3 py-2 text-xs"
            >
              <span class="text-muted-foreground">快捷插入插槽与变量：</span>
              <ElButton
                size="small"
                plain
                :disabled="busy"
                @click="insertTemplateSnippet('# {{lober:slot packages}}')"
              >
                ＋ 软件包片段插槽
              </ElButton>
              <ElButton
                size="small"
                plain
                :disabled="busy"
                @click="insertTemplateSnippet('# {{lober:slot post-target exec=post-target}}')"
              >
                ＋ 装后脚本插槽 (post-target)
              </ElButton>
              <ElButton
                size="small"
                plain
                :disabled="busy"
                @click="insertTemplateSnippet('# {{lober:slot firstboot exec=firstboot}}')"
              >
                ＋ 首次启动插槽 (firstboot)
              </ElButton>
              <ElButton
                size="small"
                plain
                :disabled="busy"
                @click="insertTemplateSnippet('{{ .target_disk }}')"
              >
                ＋ 目标盘变量
              </ElButton>
            </div>
            <ElInput
              v-if="editable"
              v-model="selected.body"
              type="textarea"
              :rows="14"
              :disabled="busy"
              placeholder="在此编写 Kickstart / Autoinstall / Unattend 应答模板正文，或点击上方按钮一键载入标准骨架…"
              aria-label="模板正文"
              class="font-mono"
            />
            <pre
              v-else
              class="max-h-96 overflow-auto whitespace-pre-wrap rounded-lg border border-border bg-muted p-4 text-sm"
              >{{ selected.body || '暂无模板正文' }}</pre>
          </section>
          <section v-show="editorSection === 'assembly'">
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
                    <ElOption
                      label="失败处理沿用默认"
                      value="inherit"
                    /><ElOption label="失败终止任务" value="true" /><ElOption
                      label="失败继续"
                      value="false"
                    />
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
                    pickHook(
                      slot.name,
                      selected!.assembly[slot.name]?.length ?? 0,
                    ),
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
          </section>
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
        <div class="flex w-full flex-wrap items-center justify-between gap-2">
          <div class="flex gap-2">
            <ElButton @click="visible = false">关闭</ElButton>
            <ElButton
              v-if="templateStepIndex > 0"
              :disabled="busy"
              @click="setTemplateStep(templateStepIndex - 1)"
            >
              上一步
            </ElButton>
            <ElButton
              v-if="templateStepIndex < templateSteps.length - 1"
              type="primary"
              plain
              :disabled="busy"
              @click="setTemplateStep(templateStepIndex + 1)"
            >
              下一步
            </ElButton>
          </div>
          <div class="flex flex-wrap justify-end gap-2">
            <ElButton
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
          @keyup.enter="candidates(true)"
        /><ElButton :loading="candidateBusy" @click="candidates(true)">
          查询
        </ElButton>
      </div>
      <ProblemList :problems="candidateProblems" />
      <article
        v-for="candidate in candidatePagination.items"
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
      <ListPagination
        :current-page="candidatePagination.page"
        :page-size="candidatePagination.pageSize"
        :total="candidatePagination.total"
        :disabled="candidateBusy"
        @page-change="changeCandidatePage"
        @size-change="changeCandidatePageSize"
      />
      <p v-if="!candidatePagination.items.length && !candidateBusy">
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
          <ElSelect
            v-model="previewForm.series"
            clearable
            placeholder="默认使用模板首个系"
          >
            <ElOption
              v-for="series in selected?.series"
              :key="series"
              :label="series"
              :value="series"
            />
          </ElSelect>
        </ElFormItem>
      </ElForm>
      <PreviewFields
        v-model="previewForm.entered!"
        :fields="previewFields"
        :disabled="busy || previewLoading"
      />
      <ElAlert
        v-if="previewSamples.length"
        :title="`参数上下文含示例值：${previewSamples.join('、')}`"
        type="info"
        :closable="false"
        class="my-3"
      />

      <ElButton
        :loading="busy || previewLoading"
        :disabled="!previewReady"
        type="primary"
        @click="run(renderPreview)"
      >
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
