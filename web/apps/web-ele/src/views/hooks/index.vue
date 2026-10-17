<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';

import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';

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
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import * as api from '#/api/console/hook';
import { errorMessage, problemsOf } from '#/api/console/problem';
import ProblemList from '#/components/console/ProblemList.vue';

import { authoringRights, hookPayload } from '../templates/authoring';
import ParamEditor from '../templates/ParamEditor.vue';

defineOptions({ name: 'HooksIndex' });
type Hook = Schemas['HookResponse'];
type HookForm = Omit<Hook, 'impls' | 'params'> & {
  impls: NonNullable<Hook['impls']>;
  params: NonNullable<Hook['params']>;
};
const access = useAccessStore();
const user = useUserStore();
const canWrite = computed(() => access.accessCodes.includes('hook:write'));
const selected = ref<HookForm>();
const visible = ref(false);
const busy = ref(false);
const prefix = ref('');
const state = ref('');
const next = ref('');
const cursor = ref('');
const history = ref<string[]>([]);
const errors = ref<Schemas['ProblemJSON'][]>([]);
const versions = ref<Hook[]>([]);
const reason = ref('');
let sequence = 0;
let editorToken = 0;
const rights = computed(() =>
  selected.value
    ? authoringRights(
        selected.value,
        access.accessCodes,
        user.userInfo?.username ?? '',
        'hook',
      )
    : null,
);
const editable = computed(() =>
  !!selected.value && (selected.value.id ? !!rights.value?.edit : canWrite.value),
);
const [Grid, grid] = useVbenVxeGrid<Hook>({
  gridOptions: {
    columns: [
      { field: 'name', title: '名称', minWidth: 190 },
      { field: 'version', title: '版本', width: 90 },
      { field: 'state', title: '状态', width: 110 },
      {
        field: 'builtin',
        title: '来源',
        width: 110,
        slots: { default: 'builtin' },
      },
      { field: 'author', title: '作者', width: 120 },
      {
        field: 'action',
        title: '操作',
        width: 130,
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'id' },
    toolbarConfig: { refresh: false },
  },
});
function report(error: unknown) {
  errors.value = problemsOf(error);
  ElMessage.error(errorMessage(error));
}
function currentEditor(token: number, row: Pick<Hook, 'id' | 'version'>) {
  return token === editorToken && visible.value && selected.value?.id === row.id && selected.value.version === row.version;
}
async function load(reset = false) {
  const owner = editorToken;
  if (reset) {
    cursor.value = '';
    history.value = [];
  }
  const token = ++sequence;
  try {
    const page = await api.getHookListApi({
      prefix: prefix.value,
      state: state.value || undefined,
      cursor: cursor.value,
      limit: 30,
    });
    if (token !== sequence) return;
    next.value = page.next;
    grid.setGridOptions({ data: page.items ?? [] });
  } catch (error) {
    if (token === sequence && owner === editorToken) report(error);
  }
}
function pageForward() {
  history.value.push(cursor.value);
  cursor.value = next.value;
  void load();
}
function pageBack() {
  cursor.value = history.value.pop() ?? '';
  void load();
}
function assign(row: Hook) {
  selected.value = { ...row, params: row.params ?? [], impls: row.impls ?? [] };
  errors.value = row.problems ?? [];
  reason.value = '';
}
async function open(row: Hook) {
  const token = ++editorToken;
  visible.value = true;
  busy.value = true;
  selected.value = undefined;
  try {
    const [detail, page] = await Promise.all([
      api.getHookApi(row.id, row.version),
      api.getHookVersionsApi(row.id),
    ]);
    if (token !== editorToken) return;
    assign(detail);
    versions.value = page.items ?? [];
  } catch (error) {
    if (token === editorToken) report(error);
  } finally {
    if (token === editorToken) busy.value = false;
  }
}
function create() {
  ++editorToken;
  busy.value = false;
  assign({
    id: '',
    version: 1,
    rev: 0,
    name: '',
    description: '',
    author: user.userInfo?.username ?? '',
    approver: '',
    builtin: false,
    state: 'draft',
    params: [],
    impls: [],
    problems: [],
    timeoutSec: 300,
    required: true,
  });
  versions.value = [];
  visible.value = true;
}
async function selectVersion(n: number) {
  const row = selected.value;
  if (row) await open({ ...row, version: n });
}
async function save() {
  const token = editorToken;
  const row = selected.value;
  if (!row || !editable.value) return false;
  const payload = hookPayload(row);
  const { rev: _rev, ...content } = payload;
  const saved = row.id
      ? await api.updateHookApi(row.id, row.version, payload)
      : await api.createHookApi(content);
  if (!currentEditor(token, row)) return false;
  assign(saved);
  const page = await api.getHookVersionsApi(saved.id);
  if (!currentEditor(token, saved)) return false;
  versions.value = page.items ?? [];
  await load();
  if (token !== editorToken) return false;
  ElMessage.success('草稿已保存');
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
async function validate() {
  const token = editorToken;
  if (editable.value && !(await save())) return;
  if (token !== editorToken) return;
  const row = selected.value;
  if (!row) return;
  const result = await api.validateHookApi(row.id, row.version);
  if (!currentEditor(token, row)) return;
  errors.value = result.problems ?? [];
  if (!errors.value.length) ElMessage.success('校验通过');
}
async function lifecycle(
  action: 'approve' | 'deprecate' | 'reject' | 'submit',
) {
  if (!(action === 'submit' ? rights.value?.submit : action === 'deprecate' ? rights.value?.deprecate : rights.value?.review)) return;
  const token = editorToken;
  if (action === 'reject' && !reason.value.trim()) {
    ElMessage.warning('请填写驳回理由');
    return;
  }
  if (action === 'submit' && !(await save())) return;
  if (token !== editorToken) return;
  const row = selected.value;
  if (!row) return;
  const changed = await api.hookLifecycleApi(
      row.id,
      row.version,
      row.rev,
      action,
      reason.value,
  );
  if (!currentEditor(token, row)) return;
  assign(changed);
  await load();
  if (token !== editorToken) return;
  ElMessage.success('状态已更新');
}
async function newVersion() {
  if (!rights.value?.newVersion) return;
  const token = editorToken;
  const row = selected.value;
  if (!row) return;
  const changed = await api.newHookVersionApi(row.id, row.version);
  if (!currentEditor(token, row)) return;
  assign(changed);
  const page = await api.getHookVersionsApi(row.id);
  if (!currentEditor(token, changed)) return;
  versions.value = page.items ?? [];
  await load();
}
async function remove() {
  const token = editorToken;
  const row = selected.value;
  if (!row) return;
  await ElMessageBox.confirm('删除这个草稿版本？', '删除草稿');
  if (!currentEditor(token, row) || !rights.value?.edit) return;
  await api.deleteHookApi(row.id, row.version, row.rev);
  if (!currentEditor(token, row)) return;
  visible.value = false;
  await load();
}
function addImpl() {
  selected.value?.impls.push({
    family: 'linux',
    series: [],
    kind: 'fragment',
    body: '',
    interpreter: '',
    timings: [],
  });
}
const families: Schemas['CatalogFamily'][] = ['linux', 'windows', 'esxi'];
const series: Schemas['CatalogSeries'][] = [
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
watch(visible, (open) => { if (!open) { ++editorToken; busy.value = false; errors.value = []; } }, { flush: 'sync' });
onBeforeUnmount(() => { ++editorToken; ++sequence; });
</script>
<template>
  <Page
    title="hook 管理与审批"
    description="编辑参数与各系的实现，按版本提交审批。内置 hook 只读。"
  >
    <template #extra>
<ElButton v-if="canWrite" type="primary" @click="create">
新建 hook
</ElButton>
</template>
    <div class="mb-4 flex gap-3">
      <ElInput
        v-model="prefix"
        placeholder="名称前缀"
        clearable
        @keyup.enter="load(true)"
      /><ElSelect v-model="state" clearable placeholder="所有状态">
<ElOption
          v-for="s in ['draft', 'review', 'approved', 'deprecated']"
          :key="s"
          :value="s"
          :label="s"
/>
</ElSelect><ElButton @click="load(true)">查询</ElButton>
    </div>
    <ProblemList v-if="!visible" :problems="errors" />
    <Grid>
<template #builtin="{ row }">
<ElTag>{{ row.builtin ? '内置' : '自定义' }}</ElTag>
</template><template #action="{ row }">
<ElButton link type="primary" @click="open(row)">
查看 / 编辑
</ElButton>
</template>
</Grid>
    <div class="mt-3 flex justify-end gap-2">
      <ElButton :disabled="!history.length" @click="pageBack">上一页</ElButton><ElButton :disabled="!next" @click="pageForward">下一页</ElButton>
    </div>
    <ElDialog
      v-model="visible"
      :title="
        selected?.id
          ? `${selected.name} · v${selected.version} · ${selected.state}`
          : '新建 hook'
      "
      width="90%"
      top="4vh"
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
            @update:model-value="selectVersion"
            >
<ElOption
              v-for="v in versions"
              :key="v.version"
              :value="v.version"
              :label="`v${v.version} · ${v.state}`"
/>
</ElSelect><ElButton
            v-if="rights?.newVersion"
            :disabled="busy"
            @click="run(newVersion)"
            >
基于此版本新建草稿
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
          <ElFormItem label="说明">
<ElInput v-model="selected.description" type="textarea" />
</ElFormItem>
          <ElFormItem label="默认超时秒">
<ElInputNumber v-model="selected.timeoutSec" :min="1" />
</ElFormItem>
          <ElFormItem label="失败处理">
<ElCheckbox v-model="selected.required">
失败时终止任务
</ElCheckbox>
</ElFormItem>
        </ElForm>
        <ParamEditor v-model="selected.params" :disabled="!editable || busy" />
        <h3 class="my-4 text-lg font-semibold">实现</h3>
        <ElForm
          v-for="(impl, index) in selected.impls"
          :key="index"
          :disabled="!editable || busy"
          label-width="110px"
          class="mb-4 rounded border p-4"
        >
          <div class="grid gap-3 md:grid-cols-2">
            <ElFormItem label="族">
<ElSelect v-model="impl.family">
<ElOption
                  v-for="f in families"
                  :key="f"
                  :label="f"
                  :value="f"
/>
</ElSelect>
</ElFormItem><ElFormItem label="系">
<ElSelect v-model="impl.series" multiple>
<ElOption
                  v-for="s in series"
                  :key="s"
                  :label="s"
                  :value="s"
/>
</ElSelect>
</ElFormItem><ElFormItem label="种类">
<ElSelect v-model="impl.kind">
<ElOption label="片段" value="fragment" /><ElOption
                  label="脚本"
                  value="script"
/>
</ElSelect>
</ElFormItem>
          </div>
          <template v-if="impl.kind === 'script'">
<ElFormItem label="解释器">
<ElSelect v-model="impl.interpreter">
<ElOption
                  v-for="i in ['sh', 'bash', 'cmd', 'powershell']"
                  :key="i"
                  :value="i"
                  :label="i"
/>
</ElSelect>
</ElFormItem><ElFormItem label="执行时机">
<ElSelect v-model="impl.timings" multiple>
<ElOption
                  v-for="t in [
                    'pre',
                    'post-installer',
                    'post-target',
                    'firstboot',
                  ]"
                  :key="t"
                  :value="t"
                  :label="t"
/>
</ElSelect>
</ElFormItem>
</template>
          <ElFormItem label="正文">
<ElInput
              v-model="impl.body"
              type="textarea"
              :rows="12"
              class="font-mono"
/>
</ElFormItem><ElButton
            v-if="editable"
            type="danger"
            plain
            @click="selected.impls.splice(index, 1)"
            >
删除实现
</ElButton>
        </ElForm>
        <ElButton v-if="editable" @click="addImpl">添加实现</ElButton>
        <div v-if="rights?.review" class="my-4">
          <ElInput
            v-model="reason"
            type="textarea"
            placeholder="驳回理由（驳回时必填）"
          />
        </div>
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
  </Page>
</template>
