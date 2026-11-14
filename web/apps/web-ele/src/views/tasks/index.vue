<script setup lang="ts">
import type { Task } from '#/api/console/task';

import {
  computed,
  onActivated,
  onBeforeUnmount,
  onDeactivated,
  onMounted,
  reactive,
  ref,
} from 'vue';
import { RouterLink, useRoute, useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCheckbox,
  ElDatePicker,
  ElDrawer,
  ElEmpty,
  ElInput,
  ElMessageBox,
  ElOption,
  ElSelect,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { errorMessage } from '#/api/console/problem';
import { taskAPI } from '#/api/console/task';
import ListPagination from '#/components/console/ListPagination.vue';
import { downloadSelection } from '#/components/console/selection-export';
import { NumberedPage } from '#/composables/numbered-page';
import { useRowSelection } from '#/composables/use-row-selection';

import { taskStateLabels } from './task-state';

defineOptions({ name: 'TasksIndex' });
const router = useRouter();
const route = useRoute();
const access = useAccessStore();
const sn = ref('');
const state = ref('');
const dates = ref<[Date, Date] | null>(null);
const summary = ref<Task>();
const drawer = ref(false);
const appliedFilters = {
  host_sn: '',
  state: '',
  created_from: '',
  created_to: '',
};
const pagination = reactive(new NumberedPage<Task>());
const selection = useRowSelection(
  (task: Task) => task.id,
  () => pagination.items,
);
const { rows: selected, all, partial } = selection;
const cancellable = computed(() =>
  selected.value.filter(
    (task) => Array.isArray(task.canCancel) && task.canCancel.length === 0,
  ),
);
const batchBusy = ref(false);
const batchResults = ref<string[]>([]);
async function cancelSelected() {
  if (
    batchBusy.value ||
    !access.accessCodes.includes('task:cancel') ||
    !cancellable.value.length
  )
    return;
  const targets = [...cancellable.value];
  const skipped = selected.value.length - targets.length;
  batchBusy.value = true;
  try {
    try {
      await ElMessageBox.confirm(
        `将取消 ${targets.length} 个任务。${skipped ? `另有 ${skipped} 个任务当前不可取消。` : ''}`,
        '取消任务',
        {
          type: 'warning',
          confirmButtonText: '确认取消',
          cancelButtonText: '返回',
        },
      );
    } catch {
      return;
    }
    const failures: string[] = [];
    let done = 0;
    for (const task of targets) {
      try {
        if (!access.accessCodes.includes('task:cancel'))
          throw new Error('没有取消权限');
        await taskAPI.cancel(task.id, task.rev);
        selection.toggle(task, false);
        done++;
      } catch (reason) {
        failures.push(`${task.hostSN}（${task.id}）：${errorMessage(reason)}`);
      }
    }
    batchResults.value = [
      `已取消 ${done} 个，失败 ${failures.length} 个，跳过 ${skipped} 个。`,
      ...failures,
    ];
    await load();
  } finally {
    batchBusy.value = false;
  }
}
function exportSelected() {
  downloadSelection('装机任务.csv', [
    [
      '任务编号',
      '主机序列号',
      '状态',
      '系统镜像',
      '模板',
      '创建人',
      '创建时间',
    ],
    ...selected.value.map((task) => [
      task.id,
      task.hostSN,
      taskStateLabels[task.state] ?? task.state,
      task.snapshot.iso.name,
      task.snapshot.template.name,
      task.createdBy,
      new Date(task.createdAt * 1000).toLocaleString(),
    ]),
  ]);
}
const error = ref('');
function resizeTable() {
  grid.setGridOptions({ maxHeight: Math.max(230, window.innerHeight - 400) });
}
let generation = 0;
let returning = false;
function openDetail(id: string) {
  return router.push({
    name: 'TasksDetail',
    params: { id },
    query: {
      ...appliedFilters,
      page: pagination.page,
      size: pagination.pageSize,
    },
  });
}
const [Grid, grid] = useVbenVxeGrid<Task>({
  class: 'h-auto',
  gridOptions: {
    columns: [
      {
        width: 48,
        align: 'center',
        slots: { header: 'select-all', default: 'select-row' },
      },
      {
        field: 'hostSN',
        title: '主机序列号',
        minWidth: 170,
        slots: { default: 'host' },
      },
      {
        field: 'state',
        title: '状态',
        width: 115,
        slots: { default: 'state' },
      },
      { title: '安装方案', minWidth: 245, slots: { default: 'plan' } },
      { title: '批次', width: 110, slots: { default: 'batch' } },
      { field: 'createdBy', title: '创建人', width: 110 },
      {
        field: 'createdAt',
        title: '创建时间',
        width: 180,
        formatter: ({ cellValue }: { cellValue: number }) =>
          new Date(cellValue * 1000).toLocaleString(),
      },
      {
        title: '操作',
        width: 90,
        fixed: 'right',
        slots: { default: 'action' },
      },
    ],
    rowConfig: { keyField: 'id' },
    cellConfig: { height: 58 },
    pagerConfig: { enabled: false },
    align: 'left',
    maxHeight: Math.max(230, window.innerHeight - 400),
  },
});
async function load(reset = false) {
  if (reset) {
    selection.clear();
    batchResults.value = [];
    Object.assign(appliedFilters, {
      host_sn: sn.value.trim(),
      state: state.value ?? '',
      created_from: dates.value
        ? String(Math.floor(dates.value[0].getTime() / 1000))
        : '',
      created_to: dates.value
        ? String(
            Math.floor(
              new Date(
                dates.value[1].getFullYear(),
                dates.value[1].getMonth(),
                dates.value[1].getDate(),
                23,
                59,
                59,
              ).getTime() / 1000,
            ),
          )
        : '',
    });
    pagination.reset();
  }
  const run = ++generation;
  error.value = '';
  const filters = { ...appliedFilters };
  try {
    const page = await pagination.load((params) =>
      taskAPI.list({ ...filters, ...params }),
    );
    if (run === generation) {
      selection.refresh(page.items);
      grid.setGridOptions({ data: page.items });
    }
  } catch (reason) {
    if (run === generation) error.value = errorMessage(reason);
  }
}
function changePage(page: number) {
  pagination.setPage(page);
  void load();
}
function changePageSize(size: number) {
  pagination.setPageSize(size);
  void load();
}
function reset() {
  sn.value = '';
  dates.value = null;
  state.value = '';
  void load(true);
}
function openSummary(task: Task) {
  summary.value = task;
  drawer.value = true;
}
onMounted(() => {
  window.addEventListener('resize', resizeTable);
  const query = route.query;
  const string = (key: string) =>
    typeof query[key] === 'string' ? query[key] : '';
  const from = Number(string('created_from'));
  const to = Number(string('created_to'));
  sn.value = string('host_sn') || '';
  state.value = string('state') || '';
  if (from > 0 && to >= from && to < 8.64e12)
    dates.value = [new Date(from * 1000), new Date(to * 1000)];
  Object.assign(appliedFilters, {
    host_sn: sn.value,
    state: state.value,
    created_from: dates.value ? String(from) : '',
    created_to: dates.value ? String(to) : '',
  });
  const size = Number(string('size'));
  if ([10, 20, 50, 100].includes(size)) pagination.setPageSize(size);
  const page = Number(string('page'));
  if (Number.isSafeInteger(page) && page > 0) pagination.setPage(page);
  void load();
});
onDeactivated(() => {
  returning = true;
});
onActivated(() => {
  if (returning) void load();
});
onBeforeUnmount(() => {
  window.removeEventListener('resize', resizeTable);
  ++generation;
  pagination.reset();
});
</script>

<template>
  <Page title="装机任务" description="查看安装进度、排查失败并创建装机任务。">
    <template #extra>
      <ElButton
        v-if="access.accessCodes.includes('task:create')"
        type="primary"
        @click="router.push('/tasks/new')"
      >
        ＋ 创建任务
      </ElButton>
    </template>
    <section class="overflow-hidden rounded-lg border border-border bg-card">
      <form
        class="flex flex-wrap items-end gap-3 px-5 py-4"
        @submit.prevent="load(true)"
      >
        <label class="grid gap-1 text-xs">主机序列号
          <ElInput
            v-model="sn"
            aria-label="主机序列号"
            placeholder="输入完整序列号"
            clearable
            class="!w-52"
          />
        </label>
        <label class="grid gap-1 text-xs">任务状态
          <ElSelect
            v-model="state"
            aria-label="任务状态"
            placeholder="全部状态"
            clearable
            class="!w-36"
          >
            <ElOption
              v-for="(label, value) in taskStateLabels"
              :key="value"
              :label="label"
              :value="value"
            />
          </ElSelect>
        </label>
        <label class="grid gap-1 text-xs">创建日期
          <ElDatePicker
            v-model="dates"
            type="daterange"
            start-placeholder="开始日期"
            end-placeholder="结束日期"
            class="!max-w-full"
          />
        </label>
        <div class="ml-auto flex flex-wrap items-center justify-end gap-3">
          <ElButton
            type="primary"
            native-type="submit"
            :loading="pagination.loading"
            >
查询
</ElButton>
          <ElButton class="!ml-0" @click="reset">重置</ElButton>
          <ElButton
            :disabled="batchBusy || pagination.loading || !selected.length"
            @click="exportSelected"
            >
导出所选
</ElButton>
          <ElButton
            v-if="access.accessCodes.includes('task:cancel')"
            type="danger"
            plain
            :disabled="!cancellable.length"
            :loading="batchBusy"
            @click="cancelSelected"
          >
            取消任务
          </ElButton>
        </div>
      </form>
      <div
        v-if="batchResults.length"
        role="status"
        class="space-y-1 border-t border-border px-5 py-3 text-sm"
      >
        <p v-for="result in batchResults" :key="result">{{ result }}</p>
      </div>
      <ElAlert v-if="error" :title="error" type="error" :closable="false">
        <ElButton link @click="load()">重试查询</ElButton>
      </ElAlert>
      <Grid>
        <template #select-all>
          <ElCheckbox
            aria-label="选择本页任务"
            :model-value="all"
            :indeterminate="partial"
            :disabled="
              batchBusy || pagination.loading || !pagination.items.length
            "
            @change="selection.togglePage"
          />
        </template>
        <template #select-row="{ row }">
          <ElCheckbox
            :aria-label="`选择任务 ${row.id}`"
            :model-value="selection.has(row)"
            :disabled="batchBusy || pagination.loading"
            @change="selection.toggle(row, $event)"
          />
        </template>
        <template #empty>
          <ElEmpty
            :image-size="72"
            :description="
              pagination.loading
                ? '正在读取任务…'
                : error
                  ? '任务读取失败'
                  : appliedFilters.host_sn ||
                      appliedFilters.state ||
                      appliedFilters.created_from
                    ? '没有匹配的任务'
                    : '还没有装机任务'
            "
          />
        </template>
        <template #host="{ row }">
          <ElButton
            link
            type="primary"
            class="font-mono"
            @click="openSummary(row)"
            >
{{ row.hostSN }}
</ElButton>
        </template>
        <template #plan="{ row }">
          <div class="py-1">
            <div class="font-medium">{{ row.snapshot.iso.name }}</div>
            <div class="text-xs text-muted-foreground">
              {{ row.snapshot.template.name }} · v{{ row.templateVersion }}
            </div>
          </div>
        </template>
        <template #state="{ row }">
          <ElTag
            :type="
              row.state === 'failed'
                ? 'danger'
                : row.state === 'done'
                  ? 'success'
                  : row.state === 'pending'
                    ? 'warning'
                    : row.state === 'cancelled'
                      ? 'info'
                      : 'primary'
            "
          >
            {{ taskStateLabels[row.state as keyof typeof taskStateLabels] }}
          </ElTag>
        </template>
        <template #batch="{ row }">
          <RouterLink
            v-if="row.batchID"
            class="text-primary"
            :to="{ name: 'TasksBatch', params: { id: row.batchID } }"
          >
            查看批次
          </RouterLink>
          <span v-else class="text-muted-foreground">—</span>
        </template>
        <template #action="{ row }">
          <ElButton type="primary" link @click="openDetail(row.id)">
            查看详情
          </ElButton>
        </template>
      </Grid>
      <div class="px-4 pb-3">
        <ListPagination
          :current-page="pagination.page"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          :page-count="pagination.items.length"
          :selected-count="selected.length"
          :disabled="pagination.loading"
          @page-change="changePage"
          @size-change="changePageSize"
        />
      </div>
    </section>
    <ElDrawer
      v-model="drawer"
      :title="summary?.hostSN"
      size="min(460px, 100vw)"
    >
      <div v-if="summary" class="grid gap-4">
        <div>
          <ElTag>{{ taskStateLabels[summary.state] }}</ElTag>
        </div>
        <div>
          任务编号：<span class="break-all font-mono">{{ summary.id }}</span>
        </div>
        <div>系统镜像：{{ summary.snapshot.iso.name }}</div>
        <div>
          模板：{{ summary.snapshot.template.name }} · v{{
            summary.templateVersion
          }}
        </div>
        <ElAlert
          v-if="summary.state === 'pending'"
          type="warning"
          :closable="false"
          title="目标主机下次网络启动时可能开始安装。"
        />
        <div>
          <ElButton
            type="primary"
            @click="
              drawer = false;
              openDetail(summary.id);
            "
            >
查看完整详情
</ElButton>
        </div>
      </div>
    </ElDrawer>
  </Page>
</template>
