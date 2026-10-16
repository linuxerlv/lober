<script setup lang="ts">
import { ref } from 'vue';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElInput,
  ElTable,
  ElTableColumn,
} from 'element-plus';
import { operations } from '#/api/console/operations';
import { requestClient } from '#/api/request';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage, useConsoleList } from '#/composables/use-console-list';
const { can, run } = useConsoleAccess();
const actor = ref(''),
  action = ref('');
const params = () => ({
  actor: actor.value || undefined,
  action: action.value || undefined,
});
const { items, next, loading, error, load } = useConsoleList((cursor) =>
  operations.audit({ ...params(), cursor, limit: 100 }),
);
async function exportCSV() {
  try {
    await run('audit:read', async () => {
      const blob = await requestClient.get<Blob>('/audit/export', {
        params: params(),
        responseType: 'blob',
        responseReturn: 'raw',
      });
      const payload = (blob as any).data || blob;
      const url = URL.createObjectURL(payload);
      const a = document.createElement('a');
      a.href = url;
      a.download = 'lober-audit.csv';
      a.click();
      setTimeout(() => URL.revokeObjectURL(url), 0);
    });
  } catch (e) {
    error.value = errorMessage(e);
  }
}
</script>
<template>
  <main class="p-6">
    <h1 class="mb-5 text-2xl font-semibold">报表与审计</h1>
    <ElAlert
      class="mb-4"
      title="当前提供审计查询和导出；装机统计报表尚未提供。"
      type="info"
      :closable="false"
    /><ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    /><ElCard
      ><div class="mb-4 flex flex-wrap gap-3">
        <ElInput v-model="actor" class="!w-48" placeholder="操作人" /><ElInput
          v-model="action"
          class="!w-64"
          placeholder="动作，例如 role.menus.update"
        /><ElButton :loading="loading" @click="load()">查询</ElButton
        ><ElButton v-if="can('audit:read')" @click="exportCSV"
          >导出 CSV</ElButton
        >
      </div>
      <ElTable :data="items" row-key="id"
        ><ElTableColumn label="时间" width="180"
          ><template #default="{ row }">{{
            new Date(row.at * 1000).toLocaleString()
          }}</template></ElTableColumn
        ><ElTableColumn prop="actor" label="操作人" width="130" /><ElTableColumn
          prop="action"
          label="动作"
          min-width="170" /><ElTableColumn
          prop="targetKind"
          label="对象类型"
          width="100" /><ElTableColumn
          prop="targetID"
          label="对象"
          min-width="130" /><ElTableColumn
          prop="detail"
          label="详情"
          min-width="240" /></ElTable
      ><ElButton v-if="next" class="mt-4" @click="load(true)"
        >加载更多</ElButton
      ></ElCard
    >
  </main>
</template>
