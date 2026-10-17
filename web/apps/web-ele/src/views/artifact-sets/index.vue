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
  ElDialog,
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
import { errorMessage, problemsOf, referencesOf } from '#/api/console/problem';
import ProblemList from '#/components/console/ProblemList.vue';

import { CursorPage } from '../hosts/inventory-state';
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
const series: Record<string, ISOCreateParams['series'][]> = {
  linux: [
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
  ],
  windows: ['windows-server', 'windows-desktop'],
  esxi: ['esxi'],
};
const filters = reactive<{
  family: '' | ISOCreateParams['family'];
  state: string;
}>({ family: '', state: '' });
const page = reactive(new CursorPage<ISOItem>());
const [Grid, gridApi] = useVbenVxeGrid<ISOItem>({
  gridOptions: {
    columns: [
      { field: 'name', title: '名字', minWidth: 180 },
      { field: 'id', title: 'ID', minWidth: 180 },
      { field: 'family', title: '族', width: 100 },
      { field: 'series', title: '系', width: 140 },
      { field: 'version', title: '版本', width: 110 },
      { field: 'arch', title: '架构', width: 100 },
      {
        field: 'state',
        title: '状态',
        width: 100,
        slots: { default: 'state' },
      },
      { title: '操作', width: 300, slots: { default: 'action' } },
    ],
    height: 'auto',
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'id' },
    proxyConfig: {
      ajax: {
        query: async () => {
          try {
            return await page.load((cursor) =>
              getArtifactSetListApi({
                ...(filters.family ? { family: filters.family } : {}),
                state: filters.state,
                cursor,
                limit: 50,
              }),
            );
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
  failure.value = undefined;
  page.reset();
  void gridApi.reload();
}
function nextPage() {
  page.advance();
  void gridApi.reload();
}
function previousPage() {
  page.back();
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
    const expected = localStorage.getItem(identityKey(id, role)) ?? undefined;
    if (!expected && (source.fetched > 0 || (source.ranges ?? []).length > 0))
      throw new Error(
        '此浏览器没有原上传文件的校验记录，不能安全续传；请使用原浏览器或重新登记 ISO',
      );
    uploadMessages[key] = '正在核对完整文件内容…';
    const identity = await verifyReselectedFile(
      file,
      source.totalSize,
      expected,
    );
    localStorage.setItem(identityKey(id, role), identity);
    for (const chunk of missingChunks(source.totalSize, source.ranges ?? [])) {
      if (disposed || uploadStopped.has(key)) break;
      if (!canWrite.value) throw new Error('上传权限已变更，上传已停止');
      uploadMessages[key] =
        `上传 ${chunk.start}–${chunk.end} / ${file.size} 字节`;
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
    search();
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
</script>

<template>
  <Page
    title="ISO 管理"
    description="登记远程下载、浏览器上传或服务器本地来源，查看服务端获取和导入结果。"
  >
    <template #extra>
      <ElButton v-if="canWrite" type="primary" @click="openCreate">
        新建 ISO
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
    <form class="mb-4 flex gap-3" @submit.prevent="search">
      <ElSelect
        v-model="filters.family"
        aria-label="筛选族"
        placeholder="全部族"
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
      <ElButton native-type="submit" type="primary">查询</ElButton>
    </form>
    <Grid>
      <template #state="{ row }">
        <ElTag
          :type="
            row.state === 'ready'
              ? 'success'
              : row.state === 'failed'
                ? 'danger'
                : 'info'
          "
        >
          {{ labels[row.state] || row.state }}
        </ElTag>
      </template>
      <template #action="{ row }">
        <ElButton link @click="openDetails(row)">详情 / 进度</ElButton>
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
    <div class="mt-3 flex justify-end gap-3">
      <ElButton :disabled="!page.history.length" @click="previousPage">
        上一页
      </ElButton>
      <ElButton :disabled="!page.next" @click="nextPage">下一页</ElButton>
    </div>
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
    <ElDialog
      v-model="detailOpen"
      title="ISO 详情与来源进度"
      width="860px"
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
            <strong>{{ source.role || '主 ISO' }} · {{ source.kind }}</strong>
            <span>{{ source.state }}</span>
          </div>
          <ElProgress :percentage="percent(source)" />
          <p class="my-2">
            {{ source.fetched }} / {{ source.totalSize || '未知' }} 字节 ·
            已确认 {{ (source.ranges ?? []).length }} 段
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
              选择原文件以补传缺失分片
              <input
                type="file"
                :aria-label="`${source.role || '主 ISO'}续传文件`"
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
      </template>
    </ElDialog>
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
