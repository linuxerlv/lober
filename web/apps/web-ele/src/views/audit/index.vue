<script setup lang="ts">
import type { AuditRecordItem } from '#/api/console/audit';

import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import { ElAlert, ElButton, ElInput, ElInputNumber } from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { exportAudit, listAudit } from '#/api/console/audit';
import { errorMessage, problemsOf } from '#/api/console/problem';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { NumberedPage } from '#/composables/numbered-page';

const filters = reactive({
  target_kind: '',
  target_id: '',
  actor: '',
  action: '',
  from: 0,
  to: 0,
});
const appliedFilters = { ...filters };
const failure = ref<unknown>();
const downloading = ref(false);
const pagination = reactive(new NumberedPage<AuditRecordItem>());
let generation = 0;
const [Grid, grid] = useVbenVxeGrid<AuditRecordItem>({
  class: 'h-auto',
  gridOptions: {
    maxHeight: 520,
    columns: [
      { field: 'id', title: 'ID', width: 80 },
      {
        field: 'at',
        title: '时间',
        minWidth: 170,
        formatter: ({ cellValue }: { cellValue: number }) =>
          new Date(cellValue * 1000).toLocaleString(),
      },
      { field: 'actor', title: '操作人', width: 130 },
      { field: 'action', title: '动作', width: 150 },
      { field: 'targetKind', title: '对象类型', width: 100 },
      { field: 'targetID', title: '对象', minWidth: 180 },
      { field: 'detail', title: '详情', minWidth: 260 },
    ],
    pagerConfig: { enabled: false },
    rowConfig: { keyField: 'id' },
  },
});
async function load() {
  const run = ++generation;
  const query = { ...appliedFilters };
  failure.value = undefined;
  try {
    const page = await pagination.load((params) => listAudit({ ...query, ...params }));
    if (run === generation) grid.setGridOptions({ data: page.items });
  } catch (error) {
    if (run === generation) failure.value = error;
  }
}
function search() {
  Object.assign(appliedFilters, filters);
  pagination.reset();
  void load();
}
function changePage(page: number) {
  pagination.setPage(page);
  void load();
}
function changePageSize(size: number) {
  pagination.setPageSize(size);
  void load();
}
async function download() {
  downloading.value = true;
  failure.value = undefined;
  try {
    const blob = await exportAudit({ ...filters });
    const url = URL.createObjectURL(blob);
    const link = document.createElement('a');
    link.href = url;
    link.download = 'audit.csv';
    link.click();
    URL.revokeObjectURL(url);
  } catch (error) {
    failure.value = error;
  } finally {
    downloading.value = false;
  }
}
onMounted(() => load());
onBeforeUnmount(() => { ++generation; pagination.reset(); });
</script>
<template>
  <Page
    title="审计"
    description="按对象、操作人与时间筛选，导出全部匹配记录。时间范围使用 Unix 秒，0 表示不限制。"
  >
    <ElAlert
      v-if="failure"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
    /><ProblemList :problems="problemsOf(failure)" />
    <div class="mb-3 flex flex-wrap gap-2">
      <ElInput
        v-model="filters.target_kind"
        placeholder="对象类型"
        class="!w-32"
      /><ElInput
        v-model="filters.target_id"
        placeholder="对象 ID"
        class="!w-48"
      /><ElInput
        v-model="filters.actor"
        placeholder="操作人"
        class="!w-32"
      /><ElInput
        v-model="filters.action"
        placeholder="动作"
        class="!w-40"
      /><ElInputNumber
        v-model="filters.from"
        :min="0"
        aria-label="起始时间（Unix 秒）"
      /><ElInputNumber
        v-model="filters.to"
        :min="0"
        aria-label="结束时间（Unix 秒）"
      /><ElButton type="primary" :loading="pagination.loading" @click="search">查询</ElButton><ElButton :loading="downloading" @click="download">
导出全部 CSV
</ElButton>
    </div>
    <Grid />
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
