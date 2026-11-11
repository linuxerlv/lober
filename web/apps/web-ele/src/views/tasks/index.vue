<script setup lang="ts">
import type { Task } from '#/api/console/task';

import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';
import { useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAccessStore } from '@vben/stores';

import { ElAlert, ElButton, ElInput, ElOption, ElSelect, ElTag } from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { errorMessage } from '#/api/console/problem';
import { taskAPI } from '#/api/console/task';
import ListPagination from '#/components/console/ListPagination.vue';
import { NumberedPage } from '#/composables/numbered-page';

import { taskStateLabels } from './task-state';

defineOptions({ name: 'TasksIndex' });
const router = useRouter();
const access = useAccessStore();
const sn = ref('');
const state = ref('');
const appliedFilters = { host_sn: '', state: '' };
const pagination = reactive(new NumberedPage<Task>());
const error = ref('');
let generation = 0;
const [Grid, grid] = useVbenVxeGrid<Task>({
  class: 'h-auto',
  gridOptions: {
    columns: [
      { field: 'hostSN', title: '主机 SN', minWidth: 170 },
      { field: 'id', title: '任务 ID', minWidth: 200 },
      { field: 'snapshot.iso.name', title: 'ISO', minWidth: 150 },
      { field: 'snapshot.template.name', title: '模板', minWidth: 140 },
      { field: 'templateVersion', title: '版本', width: 70 },
      { field: 'state', title: '状态', minWidth: 240, slots: { default: 'state' } },
      { field: 'createdBy', title: '创建人', width: 110 },
      {
        field: 'createdAt',
        title: '创建时间',
        width: 180,
        formatter: ({ cellValue }: { cellValue: number }) =>
          new Date(cellValue * 1000).toLocaleString(),
      },
      { title: '操作', width: 90, fixed: 'right', slots: { default: 'action' } },
    ],
    rowConfig: { keyField: 'id' },
    pagerConfig: { enabled: false },
    maxHeight: 520,
  },
});
async function load(reset = false) {
  if (reset) {
    Object.assign(appliedFilters, { host_sn: sn.value, state: state.value });
    pagination.reset();
  }
  const run = ++generation;
  error.value = '';
  const filters = { ...appliedFilters };
  try {
    const page = await pagination.load((params) => taskAPI.list({ ...filters, ...params }));
    if (run === generation) grid.setGridOptions({ data: page.items });
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
onMounted(() => load());
onBeforeUnmount(() => { ++generation; pagination.reset(); });
</script>

<template>
  <Page title="任务" description="任务使用创建时固化的 ISO、模板版本和参数。">
    <div class="mb-4 flex flex-wrap gap-2">
      <ElInput v-model="sn" placeholder="主机 SN（精确匹配）" class="!w-64" @keyup.enter="load(true)" />
      <ElSelect v-model="state" placeholder="全部状态" clearable class="!w-64">
        <ElOption
          v-for="(label, value) in taskStateLabels"
          :key="value"
          :label="label"
          :value="value"
        />
      </ElSelect>
      <ElButton :loading="pagination.loading" @click="load(true)">查询</ElButton>
      <ElButton
        v-if="access.accessCodes.includes('task:create')"
        type="primary"
        @click="router.push('/tasks/new')"
      >
        批量建任务
      </ElButton>
    </div>
    <ElAlert v-if="error" :title="error" type="error" :closable="false" class="mb-3" />
    <Grid>
      <template #state="{ row }">
        <ElTag
          :type="row.state === 'failed' ? 'danger' : row.state === 'done' ? 'success' : 'info'"
        >
          {{ taskStateLabels[row.state as keyof typeof taskStateLabels] }}
        </ElTag>
      </template>
      <template #action="{ row }">
        <ElButton type="primary" link @click="router.push(`/tasks/${encodeURIComponent(row.id)}`)">
          详情
        </ElButton>
      </template>
    </Grid>
    <ListPagination
      :current-page="pagination.page"
      :page-size="pagination.pageSize"
      :total="pagination.total"
      :disabled="pagination.loading"
      @page-change="changePage"
      @size-change="changePageSize"
    />
  </Page>
</template>
