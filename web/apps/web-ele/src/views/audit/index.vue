<script setup lang="ts">
import type { AuditRecordItem } from '#/api/console/audit';

import { onBeforeUnmount, onMounted, reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import { ElAlert, ElButton, ElInput, ElInputNumber, ElTag } from 'element-plus';

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
const targetKindLabels: Record<string, string> = {
  task: '装机任务',
  host: '主机资产',
  iso: '系统镜像',
  template: '装机模板',
  hook: '脚本组件',
  user: '账号与权限',
};
function selectQuickKind(kind: string) {
  filters.target_kind = filters.target_kind === kind ? '' : kind;
  search();
}
function selectQuickTime(hours: number) {
  if (hours <= 0) {
    filters.from = 0;
    filters.to = 0;
  } else {
    const now = Math.floor(Date.now() / 1000);
    filters.from = now - hours * 3600;
    filters.to = 0;
  }
  search();
}
const [Grid, grid] = useVbenVxeGrid<AuditRecordItem>({
  class: 'h-auto',
  gridOptions: {
    maxHeight: 520,
    columns: [
      { field: 'id', title: '编号', width: 80 },
      {
        field: 'at',
        title: '操作时间',
        minWidth: 175,
        formatter: ({ cellValue }: { cellValue: number }) =>
          new Date(cellValue * 1000).toLocaleString(),
      },
      { field: 'actor', title: '操作人', width: 130 },
      { field: 'action', title: '动作标识', width: 165 },
      {
        field: 'targetKind',
        title: '对象分类',
        width: 115,
        formatter: ({ cellValue }: { cellValue: string }) =>
          targetKindLabels[cellValue] || cellValue,
      },
      { field: 'targetID', title: '对象标识', minWidth: 180 },
      { field: 'detail', title: '操作详情', minWidth: 280 },
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
    title="审计与操作台账"
    description="追踪控制台所有关键变更（建任务、审批模板、主机采集、权限调整），支持一键筛选与导出 CSV。"
  >
    <ElAlert
      v-if="failure"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
      class="mb-3"
    /><ProblemList :problems="problemsOf(failure)" />
    <section class="overflow-hidden rounded-lg border border-border bg-card">
      <div class="flex flex-wrap items-center justify-between gap-3 border-b border-border px-5 py-3 text-xs">
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="mr-1 text-muted-foreground">按对象分类：</span>
          <ElTag
            v-for="(label, kindKey) in { '': '全部', ...targetKindLabels }"
            :key="kindKey"
            :type="filters.target_kind === kindKey ? 'primary' : 'info'"
            :effect="filters.target_kind === kindKey ? 'dark' : 'plain'"
            class="cursor-pointer"
            @click="selectQuickKind(String(kindKey))"
          >
            {{ label }}
          </ElTag>
        </div>
        <div class="flex flex-wrap items-center gap-1.5">
          <span class="mr-1 text-muted-foreground">快捷时间：</span>
          <ElTag
            v-for="item in [
              { label: '全部时间', hours: 0 },
              { label: '近 1 小时', hours: 1 },
              { label: '近 24 小时', hours: 24 },
              { label: '近 7 天', hours: 168 },
            ]"
            :key="item.label"
            type="info"
            effect="plain"
            class="cursor-pointer"
            @click="selectQuickTime(item.hours)"
          >
            {{ item.label }}
          </ElTag>
        </div>
      </div>
      <div class="flex flex-wrap items-center gap-2 px-5 py-4">
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
        />
        <div class="ml-auto flex flex-wrap gap-2">
          <ElButton type="primary" :loading="pagination.loading" @click="search">
            查询
          </ElButton>
          <ElButton :loading="downloading" @click="download">
            导出全部 CSV
          </ElButton>
        </div>
      </div>
      <Grid />
      <div class="px-4 pb-3">
        <ListPagination
          :current-page="pagination.page"
          :page-size="pagination.pageSize"
          :total="pagination.total"
          :disabled="pagination.loading"
          @page-change="changePage"
          @size-change="changePageSize"
        />
      </div>
    </section>
  </Page>
</template>
