<script lang="ts" setup>
import type { SourceDraft } from './SourceForm.vue';

import type {
  ISOCreateParams,
  ISOItem,
  ISOProgress,
  SourceProgress,
} from '#/api/console/artifact';

import { computed, onBeforeUnmount, reactive, ref, watch } from 'vue';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDialog,
  ElDrawer,
  ElEmpty,
  ElInput,
  ElMessage,
  ElMessageBox,
  ElOption,
  ElProgress,
  ElSelect,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import {
  createArtifactSetApi,
  createdISOIDOf,
  deleteArtifactSetApi,
  deprecateISOApi,
  getArtifactSetListApi,
  getISOApi,
  getISOProgressApi,
  operateISOApi,
  registerISOSourceApi,
  uploadISOChunkApi,
} from '#/api/console/artifact';
import { seriesByFamily } from '#/api/console/catalog';
import { errorMessage, problemsOf, referencesOf } from '#/api/console/problem';
import ChangeRecords from '#/components/console/ChangeRecords.vue';
import ListPagination from '#/components/console/ListPagination.vue';
import { downloadSelection } from '#/components/console/selection-export';
import { useRowSelection } from '#/composables/use-row-selection';
import ProblemList from '#/components/console/ProblemList.vue';
import { NumberedPage } from '#/composables/numbered-page';

import SourceForm from './SourceForm.vue';
import { missingChunks, verifyReselectedFile } from './upload';

defineOptions({ name: 'ArtifactSetsIndex' });

const access = useAccessStore();
const canWrite = computed(() => access.accessCodes.includes('iso:write'));
const failure = ref<unknown>();
const problems = computed(() => problemsOf(failure.value));
const references = computed(() => referencesOf(failure.value));
const labels: Record<string, string> = {
  acquiring: '获取中',
  importing: '导入中',
  ready: '可用',
  failed: '失败',
  deprecated: '已废弃',
};
const families: ISOCreateParams['family'][] = ['linux', 'windows', 'esxi'];
// Display catalog choices only; the server owns media and compatibility checks.
const series = seriesByFamily;
const filters = reactive<{
  family: '' | ISOCreateParams['family'];
  state: string;
}>({ family: '', state: '' });
const appliedFilters = reactive({ ...filters });
const page = reactive(new NumberedPage<ISOItem>());
const selection = useRowSelection(
  (row: ISOItem) => row.id,
  () => page.items,
);
const { rows: selectedRows, all, partial } = selection;
function exportSelected() {
  downloadSelection('系统镜像.csv', [
    ['名称', '编号', '版本', '状态'],
    ...selectedRows.value.map((row) => [
      row.name,
      row.id,
      row.version,
      row.state,
    ]),
  ]);
}
const [Grid, gridApi] = useVbenVxeGrid<ISOItem>({
  class: 'h-auto',
  gridOptions: {
    columns: [
      { width: 48, slots: { header: 'select-all', default: 'select-row' } },
      { field: 'name', title: '镜像名称', minWidth: 235 },
      { field: 'family', title: '系统类型', width: 100 },
      { field: 'series', title: '发行系列', width: 140 },
      { field: 'version', title: '版本', width: 110 },
      { field: 'arch', title: '架构', width: 100 },
      {
        field: 'state',
        title: '状态',
        width: 100,
        slots: { default: 'state' },
      },
      { title: '操作', width: 225, slots: { default: 'action' } },
    ],
    maxHeight: 520,
    align: 'left',
    cellConfig: { height: 48 },
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'id' },
    proxyConfig: {
      ajax: {
        query: async () => {
          try {
            const query = {
              ...(appliedFilters.family
                ? { family: appliedFilters.family }
                : {}),
              state: appliedFilters.state,
            };
            const result = await page.load((pagination) =>
              getArtifactSetListApi({
                ...query,
                ...pagination,
              }),
            );
            selection.refresh(result.items);
            return result;
          } catch (error) {
            failure.value = error;
            return { items: page.items };
          }
        },
      },
    },
    toolbarConfig: { refresh: true },
  },
});
function search() {
  selection.clear();
  failure.value = undefined;
  Object.assign(appliedFilters, filters);
  page.reset();
  void gridApi.reload();
}
function resetFilters() {
  filters.family = '';
  filters.state = '';
  search();
}
function changePage(value: number) {
  page.setPage(value);
  void gridApi.reload();
}
function changePageSize(value: number) {
  page.setPageSize(value);
  void gridApi.reload();
}
const createOpen = ref(false);
const busy = ref(false);
const isoForm = reactive<ISOCreateParams>({
  name: '',
  family: 'linux',
  series: 'rocky',
  sources: [],
});
const newSource = (role = ''): SourceDraft => ({
  source: { role, kind: 'url', addr: '', sha256: '' },
});
const main = ref<SourceDraft>(newSource());
const extra = ref<SourceDraft>(newSource('netboot'));
watch(
  () => isoForm.family,
  (family) => {
    isoForm.series = series[family]?.[0] ?? 'rocky';
    main.value = newSource();
    extra.value = newSource('netboot');
  },
);
watch(
  () => isoForm.series,
  () => {
    extra.value = newSource('netboot');
  },
);
function openCreate() {
  failure.value = undefined;
  Object.assign(isoForm, {
    name: '',
    family: 'linux',
    series: 'rocky',
    sources: [],
  });
  main.value = newSource();
  extra.value = newSource('netboot');
  createOpen.value = true;
}
const detailOpen = ref(false);
const selected = ref<ISOItem>();
const progress = ref<ISOProgress>();
const sourceOpen = ref(false);
const sourceDraft = ref<SourceDraft>(newSource());
const missingRoles = computed(() =>
  selected.value?.series === 'debian'
    ? ['', 'netboot'].filter(
        (role) =>
          !progress.value?.sources?.some((source) => source.role === role),
      )
    : [''].filter(
        (role) =>
          !progress.value?.sources?.some((source) => source.role === role),
      ),
);
let disposed = false;
let timer: ReturnType<typeof setTimeout> | undefined;
let detailGeneration = 0;
const uploadActive = reactive(new Set<string>());
const uploadStopped = new Set<string>();
const uploadMessages = reactive<Record<string, string>>({});
const uploadKey = (id: string, role: string) => `${id}:${role}`;
const identityKey = (id: string, role: string) =>
  `lober:iso-upload:${id}:${role}`;
function stopPolling() {
  if (timer) clearTimeout(timer);
  timer = undefined;
}
async function refreshProgress(schedule = false) {
  const id = selected.value?.id;
  if (!id) return;
  const generation = detailGeneration;
  try {
    const [item, state] = await Promise.all([
      getISOApi(id),
      getISOProgressApi(id),
    ]);
    if (
      disposed ||
      generation !== detailGeneration ||
      selected.value?.id !== id
    )
      return;
    selected.value = item;
    progress.value = state;
  } catch (error) {
    if (generation === detailGeneration && !disposed) failure.value = error;
  }
  if (
    schedule &&
    detailOpen.value &&
    !disposed &&
    generation === detailGeneration
  ) {
    stopPolling();
    timer = setTimeout(() => void refreshProgress(true), 3000);
  }
}
async function openDetails(item: ISOItem) {
  stopPolling();
  detailGeneration++;
  failure.value = undefined;
  selected.value = item;
  progress.value = undefined;
  detailOpen.value = true;
  await refreshProgress(true);
}
watch(detailOpen, (open) => {
  if (!open) {
    stopPolling();
    detailGeneration++;
  }
});
onBeforeUnmount(() => {
  disposed = true;
  detailGeneration++;
  stopPolling();
  uploadActive.forEach((key) => uploadStopped.add(key));
  page.reset();
});

function canChooseNewUploadFile(source: SourceProgress) {
  return (
    source.state === 'pending' &&
    source.fetched === 0 &&
    (source.ranges?.length ?? 0) === 0
  );
}

async function uploadFile(id: string, role: string, file: File) {
  if (!canWrite.value) return;
  const key = uploadKey(id, role);
  if (uploadActive.has(key)) return;
  uploadStopped.delete(key);
  uploadActive.add(key);
  failure.value = undefined;
  try {
    const state = await getISOProgressApi(id);
    const source = (state.sources ?? []).find((item) => item.role === role);
    if (!source || source.kind !== 'upload')
      throw new Error('服务器未登记该上传来源');
    const ranges = source.ranges ?? [];
    const storageKey = identityKey(id, role);
    // 服务端重试会清空进度。只有还没开始的上传才能换用新文件。
    const expected = canChooseNewUploadFile(source)
      ? undefined
      : (localStorage.getItem(storageKey) ?? undefined);
    if (!expected && (source.fetched > 0 || ranges.length > 0))
      throw new Error(
        '此浏览器没有原上传文件的校验记录，不能安全续传；请使用原浏览器或重新登记 ISO',
      );
    uploadMessages[key] = '正在核对完整文件内容…';
    const identity = await verifyReselectedFile(
      file,
      source.totalSize,
      expected,
    );
    localStorage.setItem(storageKey, identity);
    for (const chunk of missingChunks(
      source.totalSize,
      ranges,
      undefined,
      source.state === 'fetching',
    )) {
      if (disposed || uploadStopped.has(key)) break;
      if (!canWrite.value) throw new Error('上传权限已变更，上传已停止');
      uploadMessages[key] =
        chunk.end === file.size
          ? `上传末片并校验完整文件：${chunk.start}–${chunk.end} / ${file.size} 字节`
          : `上传 ${chunk.start}–${chunk.end} / ${file.size} 字节`;
      await uploadISOChunkApi(
        id,
        role,
        file.slice(chunk.start, chunk.end),
        chunk.start,
        file.size,
      );
    }
    uploadMessages[key] = uploadStopped.has(key)
      ? '本地上传已暂停，重新选择同一文件可继续'
      : '缺失分片已上传，等待服务器校验和导入';
  } catch (error) {
    failure.value = error;
    uploadMessages[key] = errorMessage(error);
  } finally {
    uploadActive.delete(key);
    if (!disposed && selected.value?.id === id && detailOpen.value)
      await refreshProgress();
  }
}
async function createISO() {
  if (busy.value || !canWrite.value) return;
  busy.value = true;
  failure.value = undefined;
  try {
    const drafts =
      isoForm.series === 'debian' ? [main.value, extra.value] : [main.value];
    for (const draft of drafts)
      if (draft.source.kind === 'upload' && !draft.file)
        throw new Error('请为上传来源选择文件');
    const item = await createArtifactSetApi({
      ...isoForm,
      sources: drafts.map((draft) => ({ ...draft.source })),
    });
    createOpen.value = false;
    ElMessage.success('ISO 已登记');
    void gridApi.reload();
    await openDetails(item);
    for (const draft of drafts)
      if (draft.file && draft.source.kind === 'upload')
        void uploadFile(item.id, draft.source.role ?? '', draft.file);
  } catch (error) {
    failure.value = error;
    void gridApi.reload();
    const createdID = createdISOIDOf(error);
    if (createdID) {
      try {
        const item = await getISOApi(createdID);
        createOpen.value = false;
        await openDetails(item);
        failure.value = error;
      } catch {
        failure.value = error;
      }
    }
  } finally {
    busy.value = false;
  }
}
async function resumeFile(source: SourceProgress, event: Event) {
  const input = event.target as HTMLInputElement;
  const file = input.files?.[0];
  input.value = '';
  if (file && selected.value)
    await uploadFile(selected.value.id, source.role, file);
}
async function operation(
  item: ISOItem,
  action: 'cancel' | 'delete' | 'deprecate' | 'reimport' | 'retry',
) {
  if (!canWrite.value) return;
  if (['cancel', 'delete', 'deprecate'].includes(action)) {
    try {
      await ElMessageBox.confirm(
        `确认${action === 'cancel' ? '取消获取' : action === 'delete' ? '删除' : '废弃'} ISO「${item.name}」？`,
        '操作确认',
        { type: 'warning' },
      );
    } catch {
      return;
    }
  }
  if (!canWrite.value) return;
  failure.value = undefined;
  try {
    if (action === 'delete') {
      await deleteArtifactSetApi(item.id, item.rev);
      if (selected.value?.id === item.id) detailOpen.value = false;
    } else if (action === 'deprecate') await deprecateISOApi(item.id, item.rev);
    else {
      if (action === 'cancel')
        uploadActive.forEach((key) => {
          if (key.startsWith(`${item.id}:`)) uploadStopped.add(key);
        });
      await operateISOApi(item.id, action);
    }
    ElMessage.success('操作已提交');
    await gridApi.reload();
    if (detailOpen.value) await refreshProgress();
  } catch (error) {
    failure.value = error;
  }
}
function openSource() {
  sourceDraft.value = newSource(missingRoles.value[0] ?? '');
  failure.value = undefined;
  sourceOpen.value = true;
}
async function registerSource() {
  if (!canWrite.value || !selected.value || busy.value) return;
  const id = selected.value.id;
  busy.value = true;
  failure.value = undefined;
  try {
    if (sourceDraft.value.source.kind === 'upload' && !sourceDraft.value.file)
      throw new Error('请选择上传文件');
    await registerISOSourceApi(id, sourceDraft.value.source);
    sourceOpen.value = false;
    await refreshProgress();
    if (sourceDraft.value.file && sourceDraft.value.source.kind === 'upload')
      void uploadFile(
        id,
        sourceDraft.value.source.role ?? '',
        sourceDraft.value.file,
      );
  } catch (error) {
    failure.value = error;
  } finally {
    busy.value = false;
  }
}
function percent(source: SourceProgress) {
  return source.totalSize > 0
    ? Math.min(100, Math.round((source.fetched / source.totalSize) * 100))
    : 0;
}
const sourceNames: Record<SourceProgress['kind'], string> = {
  url: '远程下载',
  upload: '浏览器上传',
  path: '服务器本地文件',
};
const sourceStates: Record<string, string> = {
  ...labels,
  fetching: '获取中',
  pending: '等待获取',
  verifying: '校验中',
  fetched: '已获取',
  complete: '已获取',
  cancelled: '已取消',
};
function formatSize(bytes: number) {
  if (bytes < 1024) return `${bytes} 字节`;
  const units = ['KB', 'MB', 'GB', 'TB'];
  let value = bytes / 1024;
  let unit = 0;
  while (value >= 1024 && unit < units.length - 1) {
    value /= 1024;
    unit++;
  }
  return `${Number(value.toFixed(2))} ${units[unit]}`;
}
</script>

<template>
  <Page
    title="系统镜像"
    description="查看镜像是否可用于装机，以及获取和导入进度。"
  >
    <template #extra>
      <ElButton v-if="canWrite" type="primary" @click="openCreate">
        登记镜像
      </ElButton>
    </template>
    <ElAlert
      v-if="failure && !createOpen && !detailOpen"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ProblemList
      v-if="!createOpen && !detailOpen"
      :problems="problems"
      :references="references"
    />
    <section class="overflow-hidden rounded-lg border border-border bg-card">
      <form
        class="flex flex-wrap items-end gap-3 px-5 py-4"
        @submit.prevent="search"
      >
        <label class="grid gap-1 text-xs"
          >系统类型
          <ElSelect
            v-model="filters.family"
            aria-label="筛选族"
            placeholder="全部系统"
            clearable
            class="!w-44"
          >
            <ElOption
              v-for="family in families"
              :key="family"
              :value="family"
              :label="family"
            />
          </ElSelect>
        </label>
        <label class="grid gap-1 text-xs"
          >镜像状态
          <ElSelect
            v-model="filters.state"
            aria-label="筛选状态"
            placeholder="全部状态"
            clearable
            class="!w-44"
          >
            <ElOption
              v-for="(label, state) in labels"
              :key="state"
              :value="state"
              :label="label"
            />
          </ElSelect>
        </label>
        <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
          <ElButton native-type="submit" type="primary" :loading="page.loading"
            >查询</ElButton
          >
          <ElButton class="!ml-0" @click="resetFilters">重置</ElButton>
          <ElButton
            :disabled="page.loading || !selectedRows.length"
            @click="exportSelected"
            >导出所选</ElButton
          >
        </div>
      </form>
      <Grid>
        <template #select-all>
          <ElCheckbox
            aria-label="选择本页系统镜像"
            :model-value="all"
            :indeterminate="partial"
            :disabled="page.loading || !page.items.length"
            @change="selection.togglePage"
          />
        </template>
        <template #select-row="{ row }">
          <ElCheckbox
            :aria-label="'选择 ' + row.name"
            :model-value="selection.has(row)"
            :disabled="page.loading"
            @change="selection.toggle(row, $event)"
          />
        </template>
        <template #empty>
          <ElEmpty
            :image-size="72"
            :description="
              page.loading
                ? '正在读取镜像…'
                : failure
                  ? '镜像读取失败'
                  : '没有匹配的镜像'
            "
          />
        </template>
        <template #state="{ row }">
          <ElTag
            :type="
              row.state === 'ready'
                ? 'success'
                : row.state === 'failed'
                  ? 'danger'
                  : row.state === 'deprecated'
                    ? 'info'
                    : 'warning'
            "
          >
            {{ labels[row.state] || row.state }}
          </ElTag>
        </template>
        <template #action="{ row }">
          <ElButton link type="primary" @click="openDetails(row)"
            >查看详情</ElButton
          >
          <ElButton
            v-if="canWrite && row.state === 'ready'"
            link
            @click="operation(row, 'deprecate')"
          >
            废弃
          </ElButton>
          <ElButton
            v-if="canWrite"
            link
            type="danger"
            @click="operation(row, 'delete')"
          >
            删除
          </ElButton>
        </template>
      </Grid>
      <div class="px-4 pb-3">
        <ListPagination
          :current-page="page.page"
          :page-size="page.pageSize"
          :total="page.total"
          :page-count="page.items.length"
          :selected-count="selectedRows.length"
          :disabled="page.loading"
          @page-change="changePage"
          @size-change="changePageSize"
        />
      </div>
    </section>
    <ElDialog
      v-model="createOpen"
      title="新建 ISO"
      width="760px"
      :close-on-click-modal="false"
    >
      <ElAlert
        v-if="failure"
        :title="errorMessage(failure)"
        type="error"
        :closable="false"
        class="mb-3"
      />
      <ProblemList :problems="problems" :references="references" />
      <div class="grid gap-4">
        <label>
          名字
          <ElInput
            v-model="isoForm.name"
            aria-label="ISO 名字"
            :disabled="busy"
          />
        </label>
        <div class="grid grid-cols-2 gap-4">
          <label>
            族
            <ElSelect
              v-model="isoForm.family"
              aria-label="ISO 族"
              class="w-full"
              :disabled="busy"
            >
              <ElOption
                v-for="family in families"
                :key="family"
                :value="family"
                :label="family"
              />
            </ElSelect>
          </label>
          <label>
            系
            <ElSelect
              v-model="isoForm.series"
              aria-label="ISO 系"
              class="w-full"
              :disabled="busy"
            >
              <ElOption
                v-for="item in series[isoForm.family]"
                :key="item"
                :value="item"
                :label="item"
              />
            </ElSelect>
          </label>
        </div>
        <SourceForm v-model="main" label="主 ISO" :disabled="busy" />
        <SourceForm
          v-if="isoForm.series === 'debian'"
          v-model="extra"
          label="Debian netboot（同版本、同架构）"
          :disabled="busy"
        />
      </div>
      <template #footer>
        <ElButton :disabled="busy" @click="createOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="busy" @click="createISO">
          登记并获取
        </ElButton>
      </template>
    </ElDialog>
    <ElDrawer
      v-model="detailOpen"
      :title="selected?.name || '镜像详情'"
      size="min(620px, 100vw)"
      :close-on-click-modal="false"
    >
      <template v-if="selected">
        <ElAlert
          v-if="failure && !sourceOpen"
          :title="errorMessage(failure)"
          type="error"
          :closable="false"
          class="mb-3"
        />
        <ProblemList
          v-if="!sourceOpen"
          :problems="problems"
          :references="references"
        />
        <div class="mb-4 grid grid-cols-2 gap-2">
          <span>名字：{{ selected.name }}</span>
          <span>状态：{{ labels[selected.state] || selected.state }}</span>
          <span>族 / 系：{{ selected.family }} / {{ selected.series }}</span>
          <span>
            版本 / 架构：{{ selected.version || '待识别' }} /
            {{ selected.arch || '待识别' }}
          </span>
          <span class="col-span-2 break-all">ID：{{ selected.id }}</span>
          <span class="col-span-2 break-all">
            SHA256：{{ selected.sha256 || '待校验' }}
          </span>
        </div>
        <div class="mb-4 flex flex-wrap gap-2">
          <ElButton @click="refreshProgress()">刷新进度</ElButton>
          <ElButton v-if="canWrite" @click="operation(selected, 'cancel')">
            取消获取
          </ElButton>
          <ElButton v-if="canWrite" @click="operation(selected, 'retry')">
            重试获取
          </ElButton>
          <ElButton v-if="canWrite" @click="operation(selected, 'reimport')">
            重新导入
          </ElButton>
          <ElButton v-if="canWrite && missingRoles.length" @click="openSource">
            补充缺失来源
          </ElButton>
        </div>
        <p v-if="!progress">正在读取来源状态…</p>
        <section
          v-for="source in progress?.sources"
          :key="source.role"
          class="mb-4 rounded border p-4"
        >
          <div class="mb-2 flex justify-between">
            <strong
              >{{ source.role || '主镜像' }} ·
              {{ sourceNames[source.kind] }}</strong
            >
            <span>{{ sourceStates[source.state] || source.state }}</span>
          </div>
          <ElProgress
            v-if="source.totalSize > 0"
            :percentage="percent(source)"
          />
          <p class="my-2 text-sm text-muted-foreground">
            已获取 {{ formatSize(source.fetched)
            }}<template v-if="source.totalSize > 0">
              / {{ formatSize(source.totalSize) }}</template
            >
          </p>
          <ElAlert
            v-if="source.error"
            :title="source.error"
            type="error"
            :closable="false"
            class="mb-2"
          />
          <template v-if="canWrite && source.kind === 'upload'">
            <label>
              {{
                canChooseNewUploadFile(source)
                  ? '选择文件上传'
                  : '选择原文件以补传缺失分片'
              }}
              <input
                type="file"
                :aria-label="`${source.role || '主 ISO'}${canChooseNewUploadFile(source) ? '上传文件' : '续传文件'}`"
                :disabled="
                  uploadActive.has(uploadKey(selected.id, source.role))
                "
                @change="resumeFile(source, $event)"
              />
            </label>
            <ElButton
              v-if="uploadActive.has(uploadKey(selected.id, source.role))"
              class="ml-2"
              @click="uploadStopped.add(uploadKey(selected.id, source.role))"
            >
              暂停上传
            </ElButton>
            <p class="mt-2 text-sm">
              {{ uploadMessages[uploadKey(selected.id, source.role)] }}
            </p>
          </template>
        </section>
        <section
          v-if="Object.keys(selected.artifacts ?? {}).length"
          class="mt-4"
        >
          <strong>服务端制品</strong>
          <div
            v-for="(path, role) in selected.artifacts"
            :key="role"
            class="mt-1 break-all"
          >
            {{ role }}：{{ path }}
          </div>
        </section>
        <ChangeRecords
          v-if="detailOpen && access.accessCodes.includes('audit:read')"
          class="mt-5 border-t border-border pt-4"
          target-kind="iso"
          :target-id="selected.id"
        />
      </template>
    </ElDrawer>
    <ElDialog
      v-model="sourceOpen"
      title="补充缺失来源"
      width="700px"
      :close-on-click-modal="false"
    >
      <ElAlert
        v-if="failure"
        :title="errorMessage(failure)"
        type="error"
        :closable="false"
        class="mb-3"
      />
      <ProblemList :problems="problems" :references="references" />
      <label class="mb-3 block">
        来源角色
        <ElSelect
          v-model="sourceDraft.source.role"
          aria-label="补充来源角色"
          class="w-full"
        >
          <ElOption
            v-for="role in missingRoles"
            :key="role"
            :value="role"
            :label="role || '主 ISO'"
          />
        </ElSelect>
      </label>
      <SourceForm v-model="sourceDraft" label="补充来源" :disabled="busy" />
      <template #footer>
        <ElButton :disabled="busy" @click="sourceOpen = false">取消</ElButton>
        <ElButton type="primary" :loading="busy" @click="registerSource">
          登记来源
        </ElButton>
      </template>
    </ElDialog>
  </Page>
</template>
