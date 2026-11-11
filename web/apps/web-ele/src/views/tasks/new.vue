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
import { useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElInput,
  ElMessage,
  ElOption,
  ElRadio,
  ElRadioGroup,
  ElSelect,
  ElStep,
  ElSteps,
  ElTabPane,
  ElTabs,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { errorMessage, problemsOf } from '#/api/console/problem';
import { wizardAPI } from '#/api/console/wizard';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { NumberedPage } from '#/composables/numbered-page';

import {
  collectEditable,
  latestOnly,
  mergeHostSelection,
  selectionChanged,
} from './wizard-state';

defineOptions({ name: 'TasksNew' });
type Host = Schemas['WizardHostItem'];
const router = useRouter();
const access = useAccessStore();
const step = ref(0);
const busy = ref(false);
const error = ref('');
const problems = ref<Schemas['ProblemJSON'][]>([]);
const gate = latestOnly();
const hostPage = reactive(new NumberedPage<Host>());
const hosts = computed(() => hostPage.items);
const selectedHosts = ref<Host[]>([]);
const hostQuery = ref('');
const attrKey = ref('');
const attrValue = ref('');
const hostFilters = reactive({ q: '', attr_key: '', attr_value: '' });
const isoPage = reactive(new NumberedPage<Schemas['WizardISOItem']>());
const isos = computed(() => isoPage.items);
const templatePage = reactive(
  new NumberedPage<Schemas['WizardTemplateItem']>(),
);
const templates = computed(() => templatePage.items);
const working = computed(
  () =>
    busy.value || hostPage.loading || isoPage.loading || templatePage.loading,
);
const selectedISO = ref<Schemas['WizardISOItem']>();
const selectedTemplate = ref<Schemas['WizardTemplateItem']>();
let restoringHosts = false;
let hostRender = 0;
const state = ref({
  isoID: '',
  templateID: '',
  templateVersion: 0,
  entered: {} as Record<string, Record<string, string>>,
});
const forms = ref<Schemas['WizardParamsHost'][]>([]);
const paramsProblems = ref<Schemas['ProblemJSON'][]>([]);
const preview = ref<Schemas['PreviewResponse']>();
const previewOpen = ref(false);
const created = ref<Schemas['TaskItem'][]>([]);
const batchKey = ref('');
const batchValue = ref('');
const batchFields = computed(() => [
  ...new Map(
    forms.value.flatMap((host) =>
      (host.fields ?? [])
        .filter((field) => !field.locked)
        .map((field) => [field.key, field] as const),
    ),
  ).values(),
]);
const [HostGrid, hostGrid] = useVbenVxeGrid<Host>({
  gridOptions: {
    columns: [
      { type: 'checkbox', width: 45 },
      { field: 'sn', title: 'SN', minWidth: 180 },
      { field: 'arch', title: '架构', width: 110 },
      { field: 'firmware', title: '固件', width: 100 },
      {
        field: 'attrs',
        title: '登记属性',
        minWidth: 180,
        formatter: ({ row }: { row: Host }) =>
          Object.entries(row.attrs ?? {})
            .map(([key, value]) => `${key}=${value}`)
            .join('；'),
      },
      { title: '选择限制', minWidth: 200, slots: { default: 'problems' } },
    ],
    rowConfig: { keyField: 'sn' },
    checkboxConfig: {
      checkMethod: ({ row }: { row: Host }) => row.selectable && !working.value,
    },
    pagerConfig: { enabled: false },
    height: 330,
  },
  gridEvents: {
    checkboxAll: updateHosts,
    checkboxChange: updateHosts,
  },
});

function fail(reason: unknown) {
  error.value = errorMessage(reason);
  problems.value = problemsOf(reason);
}
function invalidate() {
  gate.invalidate();
  busy.value = false;
  error.value = '';
  problems.value = [];
}
function resetDownstream(kind: 'hosts' | 'iso', value = '') {
  invalidate();
  state.value = selectionChanged(state.value, kind, value);
  forms.value = [];
  paramsProblems.value = [];
  templatePage.reset();
  templatePage.items = [];
  templatePage.total = 0;
  selectedTemplate.value = undefined;
  preview.value = undefined;
  if (kind === 'hosts') {
    isoPage.reset();
    isoPage.items = [];
    isoPage.total = 0;
    selectedISO.value = undefined;
  }
}
function updateHosts() {
  if (restoringHosts || working.value) return;
  selectedHosts.value = mergeHostSelection(
    selectedHosts.value,
    hosts.value,
    hostGrid.grid.getCheckboxRecords(),
  );
  resetDownstream('hosts');
}
async function restoreHostChecks() {
  const render = ++hostRender;
  restoringHosts = true;
  try {
    await nextTick();
    if (render !== hostRender || step.value !== 0) return;
    await hostGrid.grid.clearCheckboxRow();
    const selected = new Set(selectedHosts.value.map((host) => host.sn));
    await hostGrid.grid.setCheckboxRow(
      hosts.value.filter((host) => selected.has(host.sn) && host.selectable),
      true,
    );
  } finally {
    if (render === hostRender) restoringHosts = false;
  }
}
async function loadHosts() {
  error.value = '';
  problems.value = [];
  const query = { ...hostFilters };
  await gate.run(
    hostPage.load((pagination) => wizardAPI.hosts({ ...query, ...pagination })),
    () => {
      hostGrid.setGridOptions({ data: hosts.value });
      void restoreHostChecks();
    },
    fail,
  );
}
function searchHosts() {
  Object.assign(hostFilters, {
    q: hostQuery.value,
    attr_key: attrKey.value,
    attr_value: attrValue.value,
  });
  hostPage.reset();
  void loadHosts();
}
function changeHostPage(value: number) {
  hostPage.setPage(value);
  void loadHosts();
}
function changeHostPageSize(value: number) {
  hostPage.setPageSize(value);
  void loadHosts();
}
async function loadISOs() {
  error.value = '';
  problems.value = [];
  const hostSNs = selectedHosts.value.map((host) => host.sn);
  await gate.run(
    isoPage.load((pagination) => wizardAPI.isos({ hostSNs, ...pagination })),
    () => {
      step.value = 1;
    },
    fail,
  );
}
function changeISO(value: unknown) {
  resetDownstream('iso', String(value));
  selectedISO.value = isos.value.find((iso) => iso.id === String(value));
}
function changeISOPage(value: number) {
  isoPage.setPage(value);
  void loadISOs();
}
function changeISOPageSize(value: number) {
  isoPage.setPageSize(value);
  void loadISOs();
}
async function loadTemplates() {
  error.value = '';
  problems.value = [];
  const isoID = state.value.isoID;
  await gate.run(
    templatePage.load((pagination) =>
      wizardAPI.templates({ isoID, ...pagination }),
    ),
    () => {
      step.value = 2;
    },
    fail,
  );
}
function changeTemplatePage(value: number) {
  templatePage.setPage(value);
  void loadTemplates();
}
function changeTemplatePageSize(value: number) {
  templatePage.setPageSize(value);
  void loadTemplates();
}
function chooseTemplate(id: string) {
  invalidate();
  const tpl = templates.value.find((item) => item.id === id);
  selectedTemplate.value = tpl;
  state.value.templateVersion = tpl?.version ?? 0;
  state.value.entered = {};
  forms.value = [];
  paramsProblems.value = [];
  preview.value = undefined;
}
function selection() {
  return {
    isoID: state.value.isoID,
    templateID: state.value.templateID,
    templateVersion: state.value.templateVersion,
  };
}
async function evaluate(confirm = false) {
  const entered = collectEditable(forms.value, state.value.entered);
  await gate.run(
    wizardAPI.params({
      ...selection(),
      hostSNs: selectedHosts.value.map((host) => host.sn),
      entered,
    }),
    (result) => {
      forms.value = result.hosts ?? [];
      paramsProblems.value = [
        ...(result.common ?? []),
        ...(result.hosts ?? []).flatMap((host) => host.problems ?? []),
      ];
      state.value.entered = Object.fromEntries(
        (result.hosts ?? []).map((host) => [
          host.sn,
          Object.fromEntries(
            (host.fields ?? [])
              .filter((field) => !field.locked)
              .map((field) => [
                field.key,
                entered[host.sn]?.[field.key] ?? field.prefill,
              ]),
          ),
        ]),
      );
      step.value = confirm && paramsProblems.value.length === 0 ? 4 : 3;
    },
    fail,
  );
}
function fillColumn() {
  if (busy.value) return;
  for (const host of forms.value)
    if (
      (host.fields ?? []).some(
        (field) => field.key === batchKey.value && !field.locked,
      )
    ) {
      (state.value.entered[host.sn] ??= {})[batchKey.value] = batchValue.value;
    }
}
async function showPreview(host: Host) {
  error.value = '';
  problems.value = [];
  busy.value = true;
  try {
    preview.value = await wizardAPI.preview(
      state.value.templateID,
      state.value.templateVersion,
      {
        isoID: state.value.isoID,
        hostSN: host.sn,
        firmware: host.firmware || undefined,
        entered: collectEditable(forms.value, state.value.entered)[host.sn],
      },
    );
    previewOpen.value = true;
  } catch (reason) {
    fail(reason);
  } finally {
    busy.value = false;
  }
}
async function next() {
  if (working.value) return;
  error.value = '';
  problems.value = [];
  busy.value = true;
  try {
    if (step.value === 0) {
      if (!selectedHosts.value.length)
        return void ElMessage.warning('请先选择主机');
      await loadISOs();
    } else if (step.value === 1) {
      if (!state.value.isoID) return void ElMessage.warning('请选择 ISO');
      await loadTemplates();
    } else if (step.value === 2) {
      if (!state.value.templateID) return void ElMessage.warning('请选择模板');
      await evaluate();
    } else if (step.value === 3) await evaluate(true);
    else if (step.value === 4) {
      if (!access.accessCodes.includes('task:create')) return;
      const entered = collectEditable(forms.value, state.value.entered);
      const result = await wizardAPI.submit({
        ...selection(),
        hosts: selectedHosts.value.map((host) => ({
          sn: host.sn,
          entered: entered[host.sn] ?? {},
        })),
      });
      created.value = result.tasks ?? [];
      step.value = 5;
    }
  } catch (reason) {
    fail(reason);
  } finally {
    busy.value = false;
  }
}
function back() {
  invalidate();
  step.value = Math.max(0, step.value - 1);
}
onMounted(() => loadHosts());
watch(step, (value) => {
  if (value === 0) void restoreHostChecks();
});
onBeforeUnmount(() => {
  gate.invalidate();
  hostRender++;
  hostPage.reset();
  isoPage.reset();
  templatePage.reset();
});
</script>

<template>
  <Page
    title="批量建任务"
    description="选择已登记主机、ISO 和已审批模板，服务端逐台校验后一次创建整批任务。"
  >
    <ElSteps :active="step" finish-status="success" class="mb-6">
      <ElStep title="选主机" />
      <ElStep title="选 ISO" />
      <ElStep title="选模板" />
      <ElStep title="填参数" />
      <ElStep title="确认" />
      <ElStep title="已创建" />
    </ElSteps>
    <ElAlert
      v-if="error"
      :title="error"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ProblemList :problems="problems" />
    <ElCard v-if="step === 0">
      <div class="mb-3 flex flex-wrap gap-2">
        <ElInput
          v-model="hostQuery"
          placeholder="按 SN 前缀搜索"
          class="!w-56"
          @keyup.enter="searchHosts"
        />
        <ElInput v-model="attrKey" placeholder="属性名" class="!w-36" />
        <ElInput v-model="attrValue" placeholder="属性值" class="!w-44" />
        <ElButton :loading="working" @click="searchHosts">搜索</ElButton>
        <ElTag>已选 {{ selectedHosts.length }} 台</ElTag>
      </div>
      <HostGrid>
        <template #problems="{ row }">
          <ProblemList :problems="row.problems" />
        </template>
      </HostGrid>
      <ListPagination
        :current-page="hostPage.page"
        :page-size="hostPage.pageSize"
        :total="hostPage.total"
        :disabled="working"
        @page-change="changeHostPage"
        @size-change="changeHostPageSize"
      />
    </ElCard>
    <ElCard v-if="step === 1" header="选择 ISO">
      <ElRadioGroup v-model="state.isoID" class="!block" @change="changeISO">
        <div v-for="iso in isos" :key="iso.id" class="mb-3 rounded border p-3">
          <ElRadio :value="iso.id" :disabled="!iso.usable || working">
            {{ iso.name }} · {{ iso.series }} {{ iso.version }} · {{ iso.arch }}
          </ElRadio>
          <ProblemList :problems="iso.problems" />
        </div>
      </ElRadioGroup>
      <p v-if="selectedISO" class="mt-3">已选 ISO：{{ selectedISO.name }}</p>
      <ElAlert
        v-if="!isos.length"
        title="没有可用 ISO，请先在 ISO 页面完成导入。"
        type="info"
        :closable="false"
      />
      <ListPagination
        :current-page="isoPage.page"
        :page-size="isoPage.pageSize"
        :total="isoPage.total"
        :disabled="working"
        @page-change="changeISOPage"
        @size-change="changeISOPageSize"
      />
    </ElCard>
    <ElCard v-if="step === 2" header="选择模板">
      <ElRadioGroup
        v-model="state.templateID"
        class="!block"
        @change="chooseTemplate(String($event))"
      >
        <div
          v-for="tpl in templates"
          :key="`${tpl.id}/${tpl.version}`"
          class="mb-3 rounded border p-3"
        >
          <ElRadio :value="tpl.id" :disabled="!tpl.usable || working">
            {{ tpl.name }} · v{{ tpl.version }}
            <ElTag v-if="tpl.builtin" size="small" class="ml-2">内置</ElTag>
          </ElRadio>
          <ProblemList :problems="tpl.problems" />
        </div>
      </ElRadioGroup>
      <p v-if="selectedTemplate" class="mt-3">
        已选模板：{{ selectedTemplate.name }} v{{ selectedTemplate.version }}
      </p>
      <ElAlert
        v-if="!templates.length"
        title="没有匹配该 ISO 的已审批模板。"
        type="info"
        :closable="false"
      />
      <ListPagination
        :current-page="templatePage.page"
        :page-size="templatePage.pageSize"
        :total="templatePage.total"
        :disabled="working"
        @page-change="changeTemplatePage"
        @size-change="changeTemplatePageSize"
      />
    </ElCard>
    <ElCard v-if="step === 3" header="逐台参数">
      <div class="mb-4 flex flex-wrap gap-2">
        <ElSelect
          v-model="batchKey"
          :disabled="working"
          placeholder="选择批量填写的字段"
          class="!w-64"
        >
          <ElOption
            v-for="field in batchFields"
            :key="field.key"
            :value="field.key"
            :label="`${field.label} (${field.key})`"
          />
        </ElSelect>
        <ElInput
          v-model="batchValue"
          :disabled="working"
          placeholder="整列值；列表每行一项"
          type="textarea"
          class="!w-72"
        />
        <ElButton :disabled="working || !batchKey" @click="fillColumn">
          填写整列
        </ElButton>
      </div>
      <ProblemList :problems="paramsProblems" />
      <div v-for="host in forms" :key="host.sn" class="mb-4 rounded border p-4">
        <h3 class="mb-3 font-semibold">{{ host.sn }}</h3>
        <div
          v-for="field in host.fields"
          :key="field.key"
          class="mb-3 grid gap-2 md:grid-cols-[240px_1fr]"
        >
          <label :for="`${host.sn}:${field.key}`">
            {{ field.label }}
            <span v-if="field.required" class="text-red-500">*</span>
            <small class="block text-gray-500">
              {{ field.key }} · {{ field.type }}
              <span v-if="field.locked">· 已锁定</span>
            </small>
          </label>
          <div>
            <ElInput
              v-if="field.locked"
              :id="`${host.sn}:${field.key}`"
              :model-value="field.prefill"
              disabled
            />
            <ElSelect
              v-else-if="field.options?.length && !field.list"
              :id="`${host.sn}:${field.key}`"
              v-model="state.entered[host.sn]![field.key]"
              :disabled="working"
              clearable
            >
              <ElOption
                v-for="option in field.options"
                :key="option.value"
                :label="option.label || option.value"
                :value="option.value"
              />
            </ElSelect>
            <ElInput
              v-else
              :id="`${host.sn}:${field.key}`"
              v-model="state.entered[host.sn]![field.key]"
              :disabled="working"
              :type="field.list ? 'textarea' : 'text'"
              :placeholder="field.list ? '每行一项' : '请输入'"
            />
            <ProblemList
              :problems="
                (host.problems ?? []).filter(
                  (problem) => problem.subject?.path === field.key,
                )
              "
            />
          </div>
        </div>
      </div>
    </ElCard>
    <ElCard v-if="step === 4" header="确认批次">
      <p class="mb-3">
        ISO：{{ selectedISO?.name }} · 模板：{{ selectedTemplate?.name }} v{{
          state.templateVersion
        }}
      </p>
      <ElAlert
        title="创建后任务立即等待机器引导；目标机下次网络启动可能开始安装。"
        type="warning"
        :closable="false"
        class="mb-3"
      />
      <div
        v-for="host in selectedHosts"
        :key="host.sn"
        class="mb-2 flex flex-wrap items-center gap-3 rounded border p-3"
      >
        <strong>{{ host.sn }}</strong>
        <span>
          {{ host.arch || '未登记架构' }} /
          {{ host.firmware || '未登记固件' }}
        </span>
        <span>
          {{
            Object.entries(host.attrs ?? {})
              .map(([key, value]) => `${key}=${value}`)
              .join('；')
          }}
        </span>
        <ElButton :loading="working" @click="showPreview(host)">
          预览该主机全部文件
        </ElButton>
      </div>
    </ElCard>
    <ElCard v-if="step === 5" header="任务创建成功">
      <p class="mb-3">已原子创建 {{ created.length }} 个任务。</p>
      <div v-for="task in created" :key="task.id" class="mb-2">
        <ElButton
          link
          type="primary"
          @click="router.push(`/tasks/${encodeURIComponent(task.id)}`)"
        >
          {{ task.hostSN }} · {{ task.id }}
        </ElButton>
      </div>
      <ElButton @click="router.push('/tasks')">返回任务列表</ElButton>
    </ElCard>
    <div v-if="step < 5" class="mt-5 flex justify-end gap-3">
      <ElButton :disabled="step === 0 || working" @click="back">
        上一步
      </ElButton>
      <ElButton type="primary" :loading="working" @click="next">
        {{ step === 4 ? '创建整批任务' : step === 3 ? '校验并确认' : '下一步' }}
      </ElButton>
    </div>
    <ElDialog v-model="previewOpen" title="机器下发文件预览" width="85%">
      <ElAlert
        v-if="preview?.sample?.length"
        :title="`含示例值：${preview.sample.join('、')}`"
        type="info"
        :closable="false"
      />
      <ElTabs>
        <ElTabPane
          v-for="file in preview?.files"
          :key="`${file.kind}/${file.name}/${file.slot}`"
          :label="file.name || file.kind"
        >
          <pre
            class="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-4 text-sm dark:bg-gray-900"
            >{{ file.body }}</pre>
        </ElTabPane>
      </ElTabs>
    </ElDialog>
  </Page>
</template>
