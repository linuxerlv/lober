<script setup lang="ts">
import type { AuditRecordItem } from '#/api/console/audit';

import { reactive, ref } from 'vue';

import { Page } from '@vben/common-ui';

import { ElAlert, ElButton, ElInput, ElInputNumber } from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { exportAudit, listAudit } from '#/api/console/audit';
import { errorMessage, problemsOf } from '#/api/console/problem';
import ProblemList from '#/components/console/ProblemList.vue';

const filters = reactive({
  target_kind: '',
  target_id: '',
  actor: '',
  action: '',
  from: 0,
  to: 0,
});
const failure = ref<unknown>();
const downloading = ref(false);
const cursor = ref('');
const next = ref('');
const history = ref<string[]>([]);
let generation = 0;
let latestItems: AuditRecordItem[] = [];
const [Grid, grid] = useVbenVxeGrid<AuditRecordItem>({
  gridOptions: {
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
    proxyConfig: {
      ajax: {
        query: async () => {
          const run = ++generation;
          try {
            const page = await listAudit({
              ...filters,
              cursor: cursor.value,
              limit: 50,
            });
            if (run === generation) {
              next.value = page.next;
              latestItems = page.items ?? [];
            }
            return { items: latestItems };
          } catch (error) {
            if (run === generation) failure.value = error;
            return { items: latestItems };
          }
        },
      },
    },
  },
});
function search() {
  cursor.value = '';
  history.value = [];
  failure.value = undefined;
  void grid.reload();
}
function forward() {
  history.value.push(cursor.value);
  cursor.value = next.value;
  void grid.reload();
}
function back() {
  cursor.value = history.value.pop() ?? '';
  void grid.reload();
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
      /><ElButton type="primary" @click="search">查询</ElButton><ElButton :loading="downloading" @click="download">
导出全部 CSV
</ElButton>
    </div>
    <div class="mb-3 flex gap-2">
      <ElButton :disabled="!history.length" @click="back">上一页</ElButton><ElButton :disabled="!next" @click="forward">下一页</ElButton>
    </div>
    <Grid />
  </Page>
</template>
