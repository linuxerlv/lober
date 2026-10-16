<script setup lang="ts">
import type { Task } from '#/api/console/operations';
import { ref } from 'vue';
import { useRouter } from 'vue-router';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElInput,
  ElMessageBox,
  ElTable,
  ElTableColumn,
  ElTag,
} from 'element-plus';
import { operations } from '#/api/console/operations';
import AccessButton from '#/components/access-button.vue';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage, useConsoleList } from '#/composables/use-console-list';
const router = useRouter(),
  { run } = useConsoleAccess();
const sn = ref('');
const { items, next, loading, error, load } = useConsoleList((cursor) =>
  operations.tasks({ cursor, limit: 100, host_sn: sn.value || undefined }),
);
async function action(item: Task, op: 'cancel' | 'retry') {
  try {
    await ElMessageBox.confirm(
      op === 'cancel'
        ? `确认取消主机 ${item.hostSN} 的任务？`
        : `确认重试主机 ${item.hostSN} 的任务？重试可能重新执行装机操作。`,
      '确认操作',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  try {
    if (
      (await run(`task:${op}`, () => operations.taskAction(item.id, op))) !==
      undefined
    )
      await load();
  } catch (e) {
    error.value = errorMessage(e);
  }
}
</script>
<template>
  <main class="p-6">
    <header class="mb-5 flex flex-wrap items-center gap-3">
      <h1 class="mr-auto text-2xl font-semibold">任务</h1>
      <ElInput
        v-model="sn"
        class="!w-56"
        placeholder="按主机 SN 查询"
        @keyup.enter="load()"
      /><ElButton :loading="loading" @click="load()">查询</ElButton
      ><AccessButton
        permission="task:create"
        type="primary"
        @click="router.push('/tasks/new')"
        >建任务</AccessButton
      >
    </header>
    <ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    /><ElCard
      ><ElTable :data="items" row-key="id"
        ><ElTableColumn
          prop="hostSN"
          label="主机 SN"
          min-width="150"
        /><ElTableColumn label="模板" min-width="180"
          ><template #default="{ row }"
            >{{ row.snapshot.template.name }} · v{{
              row.templateVersion
            }}</template
          ></ElTableColumn
        ><ElTableColumn label="ISO" min-width="150"
          ><template #default="{ row }">{{
            row.snapshot.iso.name
          }}</template></ElTableColumn
        ><ElTableColumn label="状态" width="120"
          ><template #default="{ row }"
            ><ElTag>{{ row.state }}</ElTag></template
          ></ElTableColumn
        ><ElTableColumn prop="createdBy" label="创建人" /><ElTableColumn
          label="创建时间"
          width="170"
          ><template #default="{ row }">{{
            new Date(row.createdAt * 1000).toLocaleString()
          }}</template></ElTableColumn
        ><ElTableColumn label="操作" min-width="200"
          ><template #default="{ row }"
            ><ElButton link @click="router.push('/tasks/' + row.id)"
              >详情</ElButton
            ><AccessButton
              permission="task:cancel"
              link
              :disabled="row.canCancel.length > 0"
              :title="row.canCancel.map((p: any) => p.message).join('\n')"
              @click="action(row as Task, 'cancel')"
              >取消任务</AccessButton
            ><AccessButton
              permission="task:retry"
              link
              :disabled="row.canRetry.length > 0"
              :title="row.canRetry.map((p: any) => p.message).join('\n')"
              @click="action(row as Task, 'retry')"
              >重试</AccessButton
            ></template
          ></ElTableColumn
        ></ElTable
      ><ElButton v-if="next" class="mt-4" :loading="loading" @click="load(true)"
        >加载更多</ElButton
      ></ElCard
    >
  </main>
</template>
