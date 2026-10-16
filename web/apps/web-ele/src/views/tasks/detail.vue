<script setup lang="ts">
import type { Task, TaskEvent } from '#/api/console/operations';
import { onMounted, onUnmounted, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';
import { useAccessStore } from '@vben/stores';
import {
  ElAlert,
  ElButton,
  ElCard,
  ElMessageBox,
  ElTable,
  ElTableColumn,
} from 'element-plus';
import { operations } from '#/api/console/operations';
import AccessButton from '#/components/access-button.vue';
import { useConsoleAccess } from '#/composables/use-console-access';
import { errorMessage } from '#/composables/use-console-list';
const route = useRoute(),
  router = useRouter(),
  access = useAccessStore(),
  { run } = useConsoleAccess();
const item = ref<Task | null>(null),
  events = ref<TaskEvent[]>([]),
  error = ref('');
let source: EventSource | undefined;
let active = true;
const id = String(route.params.id);
async function load() {
  try {
    item.value = await operations.task(id);
    events.value = (await operations.taskEvents(id)).items;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
async function action(op: 'cancel' | 'retry') {
  try {
    await ElMessageBox.confirm(
      `确认${op === 'cancel' ? '取消' : '重试'}主机 ${item.value?.hostSN} 的任务？`,
      '确认操作',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  try {
    const result = await run(`task:${op}`, () => operations.taskAction(id, op));
    if (result) item.value = result;
  } catch (e) {
    error.value = errorMessage(e);
  }
}
onMounted(async () => {
  await load();
  if (!active || !item.value || !access.accessToken) return;
  source = new EventSource(
    `/console/v1/tasks/${encodeURIComponent(id)}/stream?access_token=${encodeURIComponent(access.accessToken)}&after_seq=${events.value.at(-1)?.seq || 0}`,
  );
  source.addEventListener('task', (message) => {
    try {
      const event = JSON.parse((message as MessageEvent).data) as TaskEvent;
      if (!events.value.some((e) => e.seq === event.seq))
        events.value.push(event);
    } catch {
      error.value = '无法读取任务事件';
    }
  });
});
onUnmounted(() => {
  active = false;
  source?.close();
});
</script>
<template>
  <main class="p-6">
    <header class="mb-5 flex items-center gap-3">
      <h1 class="mr-auto text-2xl font-semibold">任务详情</h1>
      <ElButton @click="router.push('/tasks')">返回列表</ElButton
      ><ElButton @click="load">刷新</ElButton
      ><AccessButton
        permission="task:cancel"
        :disabled="!item || item.canCancel.length > 0"
        @click="action('cancel')"
        >取消任务</AccessButton
      ><AccessButton
        permission="task:retry"
        :disabled="!item || item.canRetry.length > 0"
        @click="action('retry')"
        >重试</AccessButton
      >
    </header>
    <ElAlert
      v-if="error"
      class="mb-4"
      :title="error"
      type="error"
      :closable="false"
    /><ElCard v-if="item" class="mb-5"
      ><p>主机：{{ item.hostSN }}　状态：{{ item.state }}</p>
      <p class="mt-2">
        ISO：{{ item.snapshot.iso.name }}　模板：{{
          item.snapshot.template.name
        }}
        v{{ item.templateVersion }}
      </p>
      <p class="mt-2 text-sm text-gray-500">
        创建人：{{ item.createdBy }}　{{
          new Date(item.createdAt * 1000).toLocaleString()
        }}
      </p>
      <details class="mt-4">
        <summary>任务固化快照</summary>
        <pre class="mt-3 max-h-96 overflow-auto whitespace-pre-wrap text-xs">{{
          JSON.stringify(item.snapshot, null, 2)
        }}</pre>
      </details></ElCard
    ><ElCard
      ><h2 class="mb-3 text-lg font-medium">任务事件</h2>
      <ElTable :data="events" row-key="seq"
        ><ElTableColumn prop="seq" label="序号" width="80" /><ElTableColumn
          label="时间"
          width="180"
          ><template #default="{ row }">{{
            new Date(row.at * 1000).toLocaleString()
          }}</template></ElTableColumn
        ><ElTableColumn prop="kind" label="事件" width="180" /><ElTableColumn
          label="内容"
          ><template #default="{ row }">
            <pre class="whitespace-pre-wrap text-xs">{{
              JSON.stringify(row.fields, null, 2)
            }}</pre>
          </template></ElTableColumn
        ></ElTable
      ></ElCard
    >
  </main>
</template>
