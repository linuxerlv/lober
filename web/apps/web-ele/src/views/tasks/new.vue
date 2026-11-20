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
import { onBeforeRouteLeave, useRoute, useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElInput,
  ElMessage,
  ElMessageBox,
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
import { getHostDisksApi } from '#/api/console/discovery';
import { errorMessage, problemsOf } from '#/api/console/problem';
import { wizardAPI } from '#/api/console/wizard';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { NumberedPage } from '#/composables/numbered-page';

import {
  deviceField,
  diskInputProblem,
  diskLabel,
  diskNotice,
  isPersistentDiskPath,
  matchesTargetDisk,
  targetDiskSummary,
  useHostDiskReports,
} from './wizard-disks';
import {
  collectEditable,
  latestOnly,
  mergeHostSelection,
  useWizardSelection,
} from './wizard-state';

defineOptions({ name: 'TasksNew' });
type Host = Schemas['WizardHostItem'];
const router = useRouter();
const route = useRoute();
const access = useAccessStore();
const step = ref(0);
const busy = ref(false);
const error = ref('');
const problems = ref<Schemas['ProblemJSON'][]>([]);
const gate = latestOnly();
let validationTimer: ReturnType<typeof setTimeout> | undefined;
const hostPage = reactive(new NumberedPage<Host>());
const hosts = computed(() => hostPage.items);

const hostQuery = ref('');
const attrKey = ref('');
const attrValue = ref('');
const hostFilters = reactive({ q: '', attr_key: '', attr_value: '' });
const {
  reports: hostDiskReports,
  errors: diskReportErrors,
  loading: diskReportsLoading,
  blocked: diskReportsBlocked,
  reset: resetDiskReports,
  load: loadDiskReports,
  retry: retryDiskReports,
} = useHostDiskReports(getHostDisksApi);
const {
  selectedHosts,
  selectedISO,
  selectedTemplate,
  isoPage,
  templatePage,
  forms,
  entered,
  commonProblems,
  paramsProblems,
  activeHost,
  batchKey,
  batchValue,
  preview,
  previewOpen,
  selectHosts,
  selectISO,
  selectTemplate,
  selection,
} = useWizardSelection(() => {
  invalidate();
  resetDiskReports();
});
const isos = computed(() => isoPage.items);
const templates = computed(() => templatePage.items);
const working = computed(
  () =>
    busy.value || hostPage.loading || isoPage.loading || templatePage.loading,
);
let restoringHosts = false;
let hostRender = 0;
const restrictedHost = ref<Host>();
const restrictionOpen = ref(false);
const created = ref<Schemas['TaskItem'][]>([]);
const paramViewMode = ref<'matrix' | 'single'>('single');
const hostnamePrefix = ref('node-');

function autoNumberHostnames() {
  if (working.value) return;
  const prefix = hostnamePrefix.value.trim() || 'node-';
  let count = 0;
  forms.value.forEach((host, idx) => {
    const field = (host.fields ?? []).find(
      (f) => !f.locked && (f.type === 'hostname' || f.key.endsWith('/hostname') || f.key.endsWith('/name')),
    );
    if (field) {
      const num = String(idx + 1).padStart(2, '0');
      (entered.value[host.sn] ??= {})[field.key] = `${prefix}${num}.lober.local`;
      count++;
    }
  });
  if (count) {
    scheduleParameterValidation();
    ElMessage.success(`已为 ${count} 台主机按序号生成主机名`);
  }
}

function removeSelectedHost(sn: string) {
  if (working.value) return;
  selectHosts(selectedHosts.value.filter((h) => h.sn !== sn));
  void restoreHostChecks();
}

function jumpToStep(target: number) {
  if (working.value || step.value >= 5) return;
  if (target === 0 && step.value > 0) {
    invalidate();
    step.value = 0;
  } else if (target === 1 && step.value > 2) {
    invalidate();
    step.value = 2;
  } else if (target === 2 && step.value === 4) {
    invalidate();
    step.value = 3;
  }
}

function hostFieldValue(
  hostSN: string,
  field: Schemas['ParamFieldDTO'],
): string {
  // 空输入按服务端参数规则回落到预填值。
  return (!field.locked && entered.value[hostSN]?.[field.key]) || field.prefill;
}

function hostHasDiskMismatch(host: Schemas['WizardParamsHost']): boolean {
  const field = deviceField(host);
  return (
    !!field &&
    !matchesTargetDisk(
      host,
      hostFieldValue(host.sn, field),
      hostDiskReports.value[host.sn]?.disks,
    )
  );
}

function hostNeedsAttention(host: Schemas['WizardParamsHost']): boolean {
  return (
    Boolean(host.problems?.length) ||
    hostHasDiskMismatch(host) ||
    Boolean(diskReportErrors.value[host.sn])
  );
}

const invalidHosts = computed(
  () => forms.value.filter((host) => hostNeedsAttention(host)).length,
);

function chooseTargetDisk(
  host: Schemas['WizardParamsHost'],
  disk: Schemas['DiscoveryHostDisk'],
) {
  const field = deviceField(host);
  if (working.value || !field || field.locked) return;
  (entered.value[host.sn] ??= {})[field.key] = disk.device;
  scheduleParameterValidation();
}

function hostConfirmedDiskSummary(sn: string) {
  return targetDiskSummary(
    forms.value.find((host) => host.sn === sn),
    hostDiskReports.value[sn]?.disks,
  );
}
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
      { title: '选择限制', minWidth: 225, slots: { default: 'problems' } },
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
  clearTimeout(validationTimer);
  gate.invalidate();
  busy.value = false;
  error.value = '';
  problems.value = [];
}
function updateHosts() {
  if (restoringHosts || working.value) return;
  selectHosts(
    mergeHostSelection(
      selectedHosts.value,
      hosts.value,
      hostGrid.grid.getCheckboxRecords(),
    ),
  );
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
      step.value = selectedTemplate.value ? 2 : step.value === 2 ? 2 : 1;
    },
    fail,
  );
}
function changeISO(value: unknown) {
  selectISO(isos.value.find((iso) => iso.id === String(value)));
  void loadTemplates();
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
  const isoID = selection().isoID;
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
  selectTemplate(templates.value.find((item) => item.id === id));
}
async function evaluate(mode: 'confirm' | 'edit' | 'initial' = 'initial') {
  clearTimeout(validationTimer);
  const values = collectEditable(forms.value, entered.value);
  await gate.run(
    wizardAPI.params({
      ...selection(),
      hostSNs: selectedHosts.value.map((host) => host.sn),
      entered: values,
    }),
    (result) => {
      forms.value = result.hosts ?? [];
      commonProblems.value = result.common ?? [];
      entered.value = collectEditable(forms.value, values);
      if (mode === 'initial') {
        void loadDiskReports(forms.value, selectedHosts.value);
      }
      const mismatch = forms.value.find(hostHasDiskMismatch);
      const firstInvalid = forms.value.find(hostNeedsAttention)?.sn;
      activeHost.value =
        (mode === 'edit' ? undefined : firstInvalid) ??
        forms.value.find((host) => host.sn === activeHost.value)?.sn ??
        firstInvalid ??
        forms.value[0]?.sn ??
        '';
      if (mode !== 'edit') {
        const canConfirm =
          mode === 'confirm' &&
          !diskReportsBlocked.value &&
          paramsProblems.value.length === 0 &&
          !mismatch;
        if (mode === 'confirm' && mismatch) {
          activeHost.value = mismatch.sn;
        }
        step.value = canConfirm ? 4 : 3;
      }
    },
    fail,
  );
}
function scheduleParameterValidation() {
  if (step.value !== 3 || busy.value) return;
  // 输入一变化就废弃旧结果，停顿后再校验，避免每敲一个字就发请求。
  invalidate();
  validationTimer = setTimeout(() => void evaluate('edit'), 300);
}
function fillColumn() {
  if (busy.value) return;
  let count = 0;
  for (const host of forms.value)
    if (
      (host.fields ?? []).some(
        (field) => field.key === batchKey.value && !field.locked,
      )
    ) {
      (entered.value[host.sn] ??= {})[batchKey.value] = batchValue.value;
      count++;
    }
  if (count) {
    scheduleParameterValidation();
    ElMessage.success(`已更新 ${count} 台主机的参数`);
  }
}
async function showPreview(host: Host) {
  error.value = '';
  problems.value = [];
  busy.value = true;
  try {
    preview.value = await wizardAPI.preview(
      selection().templateID,
      selection().templateVersion,
      {
        isoID: selection().isoID,
        hostSN: host.sn,
        firmware: host.firmware || undefined,
        entered: collectEditable(forms.value, entered.value)[host.sn],
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
      if (!selection().isoID) return void ElMessage.warning('请选择 ISO');
      await loadTemplates();
    } else if (step.value === 2) {
      if (!selection().templateID) return void ElMessage.warning('请选择模板');
      await evaluate();
    } else if (step.value === 3) {
      if (diskReportsBlocked.value) {
        return void ElMessage.warning('请等硬件报告读取成功后再确认');
      }
      await evaluate('confirm');
    } else if (step.value === 4) {
      if (!access.accessCodes.includes('task:create')) return;
      if (diskReportsBlocked.value) {
        step.value = 3;
        return void ElMessage.warning('请等硬件报告读取成功后再确认');
      }
      const mismatch = forms.value.find(hostHasDiskMismatch);
      if (mismatch) {
        step.value = 3;
        activeHost.value = mismatch.sn;
        return void ElMessage.warning(
          `主机 ${mismatch.sn} 的目标盘与可安装磁盘清单不匹配，请先选择可用目标盘`,
        );
      }
      const values = collectEditable(forms.value, entered.value);
      const confirmed = new Map(forms.value.map(({ sn, targetDisk }) => [sn, targetDisk]));
      const result = await wizardAPI.submit({
        ...selection(),
        hosts: selectedHosts.value.map((host) => ({
          sn: host.sn,
          entered: values[host.sn] ?? {},
          expectedTargetDisk: confirmed.get(host.sn),
        })),
      });
      created.value = result.tasks ?? [];
      step.value = 5;
    }
  } catch (reason) {
    if (step.value === 4 && problemsOf(reason).some((p) => p.code === 'target_disk_changed')) {
      step.value = 3;
      await evaluate();
    }
    fail(reason);
  } finally {
    busy.value = false;
  }
}
function back() {
  invalidate();
  step.value = step.value === 2 ? 0 : Math.max(0, step.value - 1);
}
onMounted(async () => {
  selectedHosts.value = [];
  const requested =
    typeof route.query.host === 'string' ? route.query.host.trim() : '';
  if (requested) {
    hostQuery.value = requested;
    hostFilters.q = requested;
  }
  await loadHosts();
  if (requested) {
    const matched = hosts.value.find((row) => row.sn === requested);
    if (matched && matched.selectable) {
      selectHosts([matched]);
      void restoreHostChecks();
    } else if (matched && !matched.selectable) {
      selectedHosts.value = [];
      void restoreHostChecks();
      ElMessage.warning(
        matched.problems?.[0]?.message ||
          `主机 ${requested} 当前有选择限制，无法自动预选`,
      );
    } else {
      selectedHosts.value = [];
      void restoreHostChecks();
      ElMessage.warning(`未找到已登记的可选主机 ${requested}`);
    }
  }
});
onBeforeRouteLeave(async () => {
  if (!selectedHosts.value.length || step.value === 5) return true;
  try {
    await ElMessageBox.confirm('当前选择与填写内容会丢失。', '离开创建任务？', {
      confirmButtonText: '离开',
      cancelButtonText: '继续编辑',
      type: 'warning',
    });
    return true;
  } catch {
    return false;
  }
});
watch(step, (value) => {
  if (value === 0) void restoreHostChecks();
});
onBeforeUnmount(() => {
  clearTimeout(validationTimer);
  gate.invalidate();
  resetDiskReports();
  hostRender++;
  hostPage.reset();
  isoPage.reset();
  templatePage.reset();
});
</script>

<template>
  <Page
    title="创建装机任务"
    description="选择主机和安装方案，核对配置后提交。"
    auto-content-height
    footer-fixed
  >
    <ElSteps
      v-if="step < 5"
      :active="step === 0 ? 0 : step <= 2 ? 1 : step - 1"
      finish-status="success"
      class="mb-4 rounded-lg border border-border bg-card p-5"
    >
      <ElStep
        title="1. 选目标主机"
        description="支持跨页勾选与属性过滤"
        class="cursor-pointer"
        @click="jumpToStep(0)"
      />
      <ElStep
        title="2. 选镜像与模板"
        description="同屏选择系统镜像和装机模板"
        class="cursor-pointer"
        @click="jumpToStep(1)"
      />
      <ElStep
        title="3. 配参数与目标盘"
        description="支持单机精调与批量总表对比"
        class="cursor-pointer"
        @click="jumpToStep(2)"
      />
      <ElStep title="4. 核对并提交" description="预览下发配置后创建任务" />
    </ElSteps>
    <ElAlert
      v-if="error"
      :title="error"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ProblemList :problems="problems" />
    <div class="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
      <div class="grid min-w-0 gap-4">
        <ElCard v-if="step === 0" header="选择目标主机">
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
            <ElTag type="primary">已选 {{ selectedHosts.length }} 台</ElTag>
          </div>
          <div
            v-if="selectedHosts.length"
            class="mb-3 flex flex-wrap items-center gap-2 rounded-lg border border-primary/30 bg-primary/5 px-3 py-2 text-xs"
          >
            <span class="font-medium text-primary">已选主机清单：</span>
            <ElTag
              v-for="h in selectedHosts"
              :key="h.sn"
              closable
              size="small"
              class="font-mono"
              @close="removeSelectedHost(h.sn)"
            >
              {{ h.sn }}
            </ElTag>
          </div>
          <HostGrid>
            <template #problems="{ row }">
              <button
                v-if="!row.selectable"
                type="button"
                class="inline-flex max-w-full items-center gap-2 text-left hover:underline focus-visible:outline"
                :aria-label="`查看 ${row.sn} 的选择限制`"
                @click="
                  restrictedHost = row;
                  restrictionOpen = true;
                "
              >
                <ElTag type="warning" size="small">不可选</ElTag>
                <span class="truncate text-xs">{{
                  row.problems?.[0]?.message || '当前无法创建任务'
                }}</span>
              </button>
              <span v-else class="text-xs text-muted-foreground">可选</span>
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
        <ElCard v-if="step === 1 || step === 2" header="选择系统镜像">
          <ElRadioGroup
            :model-value="selectedISO?.id ?? ''"
            class="!grid gap-3 sm:grid-cols-2"
            @change="changeISO"
          >
            <div
              v-for="iso in isos"
              :key="iso.id"
              class="rounded-xl border p-4 transition"
              :class="[
                selectedISO?.id === iso.id
                  ? 'border-primary bg-primary/5 ring-1 ring-primary'
                  : 'border-border hover:border-primary/40',
                !iso.usable ? 'opacity-60' : '',
              ]"
            >
              <div class="flex items-start justify-between gap-2">
                <ElRadio :value="iso.id" :disabled="!iso.usable || working">
                  <span class="font-medium">{{ iso.name }}</span>
                </ElRadio>
                <ElTag size="small" :type="iso.usable ? 'success' : 'warning'">
                  {{ iso.usable ? '可用' : '不可用' }}
                </ElTag>
              </div>
              <div class="mt-2 flex flex-wrap gap-1.5 pl-6 text-xs text-muted-foreground">
                <span v-if="iso.family" class="rounded bg-muted px-2 py-0.5 uppercase">{{
                  iso.family
                }}</span>
                <span v-if="iso.series" class="rounded bg-muted px-2 py-0.5">{{
                  iso.series
                }}</span>
                <span v-if="iso.version" class="rounded bg-muted px-2 py-0.5">v{{
                  iso.version
                }}</span>
                <span v-if="iso.arch" class="rounded bg-muted px-2 py-0.5 font-mono">{{
                  iso.arch
                }}</span>
              </div>
              <ProblemList :problems="iso.problems" />
            </div>
          </ElRadioGroup>
          <p v-if="selectedISO" class="mt-3 text-sm font-medium text-primary">
            已选 ISO：{{ selectedISO.name }}
          </p>
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
        <ElCard v-if="step === 2" header="选择与镜像匹配的已审批模板">
          <ElRadioGroup
            :model-value="selectedTemplate?.id ?? ''"
            class="!grid gap-3 sm:grid-cols-2"
            @change="chooseTemplate(String($event))"
          >
            <div
              v-for="tpl in templates"
              :key="`${tpl.id}/${tpl.version}`"
              class="rounded-xl border p-4 transition"
              :class="[
                selectedTemplate?.id === tpl.id
                  ? 'border-primary bg-primary/5 ring-1 ring-primary'
                  : 'border-border hover:border-primary/40',
                !tpl.usable ? 'opacity-60' : '',
              ]"
            >
              <div class="flex items-start justify-between gap-2">
                <ElRadio :value="tpl.id" :disabled="!tpl.usable || working">
                  <span class="font-medium">{{ tpl.name }} · v{{ tpl.version }}</span>
                  <ElTag v-if="tpl.builtin" size="small" class="ml-2">内置</ElTag>
                </ElRadio>
                <ElTag size="small" :type="tpl.usable ? 'success' : 'warning'">
                  {{ tpl.usable ? '已批准' : '未就绪' }}
                </ElTag>
              </div>
              <ProblemList :problems="tpl.problems" />
            </div>
          </ElRadioGroup>
          <p v-if="selectedTemplate" class="mt-3 text-sm font-medium text-primary">
            已选模板：{{ selectedTemplate.name }} v{{
              selectedTemplate.version
            }}
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
        <ElCard v-if="step === 3" header="逐台参数与目标盘配置">
          <!-- 顶部视图切换与智能批量工具栏 -->
          <div class="mb-4 grid gap-3 rounded-xl border border-border bg-muted/40 p-4">
            <div class="flex flex-wrap items-center justify-between gap-3 border-b border-border pb-3">
              <div class="flex items-center gap-2 text-xs">
                <span class="font-semibold text-foreground">编辑视图模式：</span>
                <div class="inline-flex rounded-lg bg-card p-1 shadow-sm">
                  <button
                    type="button"
                    class="rounded-md px-3 py-1 font-medium transition"
                    :class="
                      paramViewMode === 'single'
                        ? 'bg-primary text-white'
                        : 'text-muted-foreground hover:text-foreground'
                    "
                    @click="paramViewMode = 'single'"
                  >
                    单机精调视图
                  </button>
                  <button
                    type="button"
                    class="rounded-md px-3 py-1 font-medium transition"
                    :class="
                      paramViewMode === 'matrix'
                        ? 'bg-primary text-white'
                        : 'text-muted-foreground hover:text-foreground'
                    "
                    @click="paramViewMode = 'matrix'"
                  >
                    批量总表视图（同屏对比 {{ forms.length }} 台）
                  </button>
                </div>
              </div>
              <div class="flex flex-wrap items-center gap-2">
                <span class="text-xs text-muted-foreground">主机名自动递增：</span>
                <ElInput
                  v-model="hostnamePrefix"
                  :disabled="working"
                  placeholder="前缀如 bj-node-"
                  size="small"
                  class="!w-36"
                />
                <ElButton
                  size="small"
                  :disabled="working"
                  @click="autoNumberHostnames"
                >
                  按序号生成主机名
                </ElButton>
              </div>
            </div>
            <div class="flex flex-wrap items-center gap-2">
              <span class="text-xs text-muted-foreground">统一批量赋值：</span>
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
          </div>
          <ElAlert
            v-if="invalidHosts"
            :title="`${invalidHosts} 台主机有参数需要修改`"
            type="warning"
            :closable="false"
            class="mb-4"
          />
          <div v-if="Object.keys(diskReportErrors).length" class="mb-4 grid gap-2">
            <ElAlert
              v-for="(message, sn) in diskReportErrors"
              :key="sn"
              :title="`${sn} 的硬件报告读取失败：${message}`"
              type="error"
              :closable="false"
            />
            <ElButton :disabled="working || diskReportsLoading" @click="retryDiskReports">
              重新读取硬件报告
            </ElButton>
          </div>
          <ProblemList :problems="commonProblems" />

          <!-- 模式 A：批量总表矩阵视图（所有主机同屏对比编辑） -->
          <div
            v-if="paramViewMode === 'matrix'"
            class="mb-4 overflow-x-auto rounded-xl border border-border"
          >
            <table class="w-full text-left text-sm">
              <thead class="border-b border-border bg-muted text-xs text-muted-foreground">
                <tr>
                  <th class="whitespace-nowrap p-3">主机序列号</th>
                  <th
                    v-for="col in forms[0]?.fields ?? []"
                    :key="col.key"
                    class="min-w-[180px] p-3"
                  >
                    <div>
                      {{ col.label }}
                      <span v-if="col.required" class="text-red-500">*</span>
                    </div>
                    <div class="font-mono text-[11px] font-normal opacity-75">
                      {{ col.key }}
                    </div>
                  </th>
                  <th class="whitespace-nowrap p-3">状态与预览</th>
                </tr>
              </thead>
              <tbody class="divide-y divide-border">
                <tr v-for="host in forms" :key="host.sn" class="align-top">
                  <td class="whitespace-nowrap p-3 font-mono font-semibold">
                    <div>{{ host.sn }}</div>
                    <ElTag
                      size="small"
                      class="mt-1"
                      :type="hostNeedsAttention(host) ? 'warning' : 'success'"
                    >
                      {{ hostNeedsAttention(host) ? '待修正' : '校验通过' }}
                    </ElTag>
                  </td>
                  <td
                    v-for="field in host.fields"
                    :key="field.key"
                    class="p-3"
                  >
                    <ElInput
                      v-if="field.locked"
                      :model-value="field.prefill"
                      size="small"
                      disabled
                    />
                    <ElSelect
                      v-else-if="field.options?.length && !field.list"
                      v-model="entered[host.sn]![field.key]"
                      :disabled="working"
                      size="small"
                      clearable
                      @update:model-value="scheduleParameterValidation"
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
                      v-model="entered[host.sn]![field.key]"
                      :disabled="working"
                      size="small"
                      :type="field.list ? 'textarea' : 'text'"
                      @update:model-value="scheduleParameterValidation"
                    />
                    <div
                      v-if="
                        deviceField(host)?.key === field.key &&
                        hostDiskReports[host.sn]?.disks?.length &&
                        !field.locked
                      "
                      class="mt-1.5 flex flex-wrap gap-1"
                    >
                      <button
                        v-for="disk in hostDiskReports[host.sn]!.disks"
                        :key="disk.device"
                        type="button"
                        class="rounded border px-2 py-0.5 text-[11px] transition"
                        :class="
                          hostFieldValue(host.sn, field).trim().replace(/^\/dev\//, '') === disk.device
                            ? 'border-primary bg-primary/10 font-medium text-primary'
                            : 'border-border text-muted-foreground hover:border-primary/40'
                        "
                        @click="chooseTargetDisk(host, disk)"
                      >
                        {{ diskLabel(disk) }}
                      </button>
                    </div>
                  </td>
                  <td class="whitespace-nowrap p-3">
                    <ElButton
                      size="small"
                      link
                      type="primary"
                      @click="
                        activeHost = host.sn;
                        paramViewMode = 'single';
                      "
                    >
                      单机详情
                    </ElButton>
                  </td>
                </tr>
              </tbody>
            </table>
          </div>

          <!-- 模式 B：单机精调视图 -->
          <div
            v-show="paramViewMode === 'single'"
            class="grid gap-3 md:grid-cols-[185px_minmax(0,1fr)]"
          >
            <nav
              class="flex max-h-80 flex-wrap content-start gap-2 overflow-auto md:flex-col"
              aria-label="参数主机"
            >
              <ElButton
                v-for="host in forms"
                :key="host.sn"
                class="!ml-0 !justify-start"
                :type="activeHost === host.sn ? 'primary' : undefined"
                :plain="activeHost === host.sn"
                @click="activeHost = host.sn"
              >
                {{ host.sn
                }}<span v-if="hostNeedsAttention(host)" class="ml-2 text-xs">有错误</span>
              </ElButton>
            </nav>
            <div
              v-for="host in forms"
              v-show="host.sn === activeHost"
              :key="host.sn"
              class="min-w-0 rounded border border-border p-4"
            >
              <h3 class="mb-3 font-semibold">{{ host.sn }}</h3>
              <ProblemList
                :problems="
                  (host.problems ?? []).filter(
                    (problem) =>
                      !host.fields?.some(
                        (field) => field.key === problem.subject?.path,
                      ),
                  )
                "
              />
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
                    v-model="entered[host.sn]![field.key]"
                    :disabled="working"
                    clearable
                    @update:model-value="scheduleParameterValidation"
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
                    v-model="entered[host.sn]![field.key]"
                    :disabled="working"
                    :type="field.list ? 'textarea' : 'text'"
                    :placeholder="field.list ? '每行一项' : '请输入'"
                    @update:model-value="scheduleParameterValidation"
                  />
                  <div
                    v-if="deviceField(host)?.key === field.key"
                    class="mt-2 grid gap-2 text-xs"
                  >
                    <ElAlert
                      v-if="hostHasDiskMismatch(host)"
                      :title="diskInputProblem(host, hostFieldValue(host.sn, field), hostDiskReports[host.sn]?.disks)"
                      type="warning"
                      :closable="false"
                    />
                    <div
                      v-if="hostDiskReports[host.sn]?.disks?.length && !field.locked"
                      class="flex flex-wrap items-center gap-2"
                    >
                      <span class="text-muted-foreground">可安装磁盘：</span>
                      <ElButton
                        v-for="disk in hostDiskReports[host.sn]!.disks"
                        :key="disk.device"
                        size="small"
                        :disabled="working"
                        :type="
                          hostFieldValue(host.sn, field).trim().replace(/^\/dev\//, '') === disk.device
                            ? 'primary'
                            : undefined
                        "
                        @click="chooseTargetDisk(host, disk)"
                      >
                        {{ diskLabel(disk) }}
                      </ElButton>
                    </div>
                    <p v-if="isPersistentDiskPath(host.targetDisk?.mode, hostFieldValue(host.sn, field))" class="text-muted-foreground">
                      安装程序会在目标主机解析此路径。请核对它指向的磁盘。
                    </p>
                    <p
                      v-if="diskNotice(hostDiskReports[host.sn])"
                      class="text-muted-foreground"
                    >
                      {{ diskNotice(hostDiskReports[host.sn]) }}
                    </p>
                  </div>
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
          </div>
        </ElCard>
        <ElCard v-if="step === 4" header="确认批次">
          <p class="mb-3">
            ISO：{{ selectedISO?.name }} · 模板：{{
              selectedTemplate?.name
            }}
            v{{ selectedTemplate?.version }}
          </p>
          <ElAlert
            title="创建后任务立即等待机器引导；请让目标主机从网络启动，若已停在自动发现或硬件采集界面，须先重启再从网络启动。"
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
              {{ hostConfirmedDiskSummary(host.sn) }}
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
          <p class="mb-3">
            已创建 {{ created.length }} 个任务，正在等待主机引导。
          </p>
          <ElAlert
            title="请让目标主机从网络启动；若已停在自动发现或硬件采集界面，须先重启再从网络启动以开始安装。"
            type="info"
            :closable="false"
            class="mb-3"
          />
          <div v-for="task in created" :key="task.id" class="mb-2">
            <ElButton
              link
              type="primary"
              @click="router.push(`/tasks/${encodeURIComponent(task.id)}`)"
            >
              {{ task.hostSN }} · {{ task.id }}
            </ElButton>
          </div>
          <ElButton
            v-if="created[0]?.batchID"
            type="primary"
            @click="router.push({ name: 'TasksBatch', params: { id: created[0].batchID } })"
          >
            查看批次
          </ElButton>
          <ElButton @click="router.push('/tasks')">返回任务列表</ElButton>
        </ElCard>
      </div>
      <ElCard v-if="step < 5" header="安装摘要" class="lg:sticky lg:top-4">
        <dl class="grid gap-3 text-sm">
          <div>
            <dt class="text-muted-foreground">目标主机</dt>
            <dd>已选 {{ selectedHosts.length }} 台</dd>
          </div>
          <div>
            <dt class="text-muted-foreground">系统镜像</dt>
            <dd>{{ selectedISO?.name || '未选择' }}</dd>
          </div>
          <div>
            <dt class="text-muted-foreground">安装模板</dt>
            <dd>
              {{
                selectedTemplate
                  ? `${selectedTemplate.name} · v${selectedTemplate.version}`
                  : '未选择'
              }}
            </dd>
          </div>
        </dl>
        <details v-if="selectedHosts.length" class="mt-4">
          <summary class="cursor-pointer text-sm text-primary">
            查看已选主机
          </summary>
          <ul class="mt-2 max-h-40 space-y-1 overflow-auto font-mono text-xs">
            <li v-for="host in selectedHosts" :key="host.sn">{{ host.sn }}</li>
          </ul>
        </details>
      </ElCard>
    </div>
    <template #footer>
      <div v-if="step < 5" class="flex w-full justify-between gap-3">
        <ElButton :disabled="step === 0 || working" @click="back">
          上一步
        </ElButton>
        <ElButton
          type="primary"
          :disabled="(step === 3 || step === 4) && diskReportsBlocked"
          :loading="working"
          @click="next"
        >
          {{
            step === 4 ? '创建整批任务' : step === 3 ? '校验并确认' : '下一步'
          }}
        </ElButton>
      </div>
    </template>
    <ElDialog
      v-model="restrictionOpen"
      title="主机选择限制"
      width="min(480px, 95vw)"
    >
      <strong>{{ restrictedHost?.sn }}</strong>
      <ul v-if="restrictedHost?.problems?.length" class="mt-3 grid gap-2">
        <li v-for="(problem, index) in restrictedHost.problems" :key="index">
          <p>{{ problem.message }}</p>
          <p v-if="problem.next" class="text-muted-foreground">
            {{ problem.next }}
          </p>
        </li>
      </ul>
      <p v-else class="mt-3">这台主机当前无法创建任务。</p>
      <template #footer>
<ElButton @click="restrictionOpen = false">关闭</ElButton>
</template>
    </ElDialog>
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
