<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';
import type { Task, TaskEvent } from '#/api/console/task';

import { computed, ref, shallowRef, watch } from 'vue';
import { useRoute } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAppConfig } from '@vben/hooks';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElMessageBox,
  ElTabPane,
  ElTabs,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { errorMessage, problemsOf } from '#/api/console/problem';
import { taskAPI, taskStreamURL } from '#/api/console/task';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { useTaskStream } from '#/composables/use-task-stream';
import { useAuthStore } from '#/store';

import { eventDetails } from './event-details';
import { taskStateLabels } from './task-state';
import { mergeEvents } from './wizard-state';

defineOptions({ name: 'TasksDetail' });
const route = useRoute();
const access = useAccessStore();
const auth = useAuthStore();
const { apiURL } = useAppConfig(import.meta.env, import.meta.env.PROD);
const id = computed(() => String(route.params.id ?? ''));
const task = ref<Task>();
const error = ref('');
const streamError = ref('');
const problems = ref<Schemas['ProblemJSON'][]>([]);
const events = ref<TaskEvent[]>([]);
const eventPage = ref(1);
const eventPageSize = ref(20);
const stream = shallowRef<ReturnType<typeof useTaskStream>>();
const connected = computed(() => stream.value?.state.value === 'open');
const preview = ref<Schemas['PreviewResponse']>();
const showPreview = ref(false);
const busy = ref(false);
type Session = { id: string; reloading: boolean; reloadPending: boolean };
let session: Session | undefined;
let eventFlush: ReturnType<typeof setTimeout> | undefined;
const pendingEvents = new Map<number, TaskEvent>();
const [EventGrid, eventGrid] = useVbenVxeGrid<TaskEvent>({
  gridOptions: {
    columns: [
      { field: 'seq', title: '序号', width: 80 },
      {
        field: 'at',
        title: '时间',
        width: 180,
        formatter: ({ cellValue }: { cellValue: number }) =>
          new Date(cellValue * 1000).toLocaleString(),
      },
      { field: 'kind', title: '事件', minWidth: 140 },
      {
        title: '详情',
        minWidth: 300,
        formatter: ({ row }: { row: TaskEvent }) =>
          eventDetails(row.fields),
      },
    ],
    rowConfig: { keyField: 'seq' },
    pagerConfig: { enabled: false },
    height: 330,
  },
});
function failed(reason: unknown) {
  error.value = errorMessage(reason);
  problems.value = problemsOf(reason);
}
async function reload() {
  const current = session;
  if (!current) return;
  if (current.reloading) {
    current.reloadPending = true;
    return;
  }
  current.reloading = true;
  try {
    do {
      current.reloadPending = false;
      try {
        const result = await taskAPI.get(current.id);
        if (current === session) task.value = result;
      } catch (reason) {
        if (current === session) failed(reason);
      }
    } while (current === session && current.reloadPending);
  } finally {
    current.reloading = false;
  }
}
function queueEvent(event: TaskEvent) {
  pendingEvents.set(event.seq, event);
  if (eventFlush !== undefined) return;
  // 新消息不推迟本批处理，持续到达时也会定时展示。
  eventFlush = setTimeout(() => {
    eventFlush = undefined;
    events.value = mergeEvents(events.value, [...pendingEvents.values()]);
    pendingEvents.clear();
    void reload();
  }, 100);
}
watch([events, eventPage, eventPageSize], () => {
  const start = (eventPage.value - 1) * eventPageSize.value;
  eventGrid.setGridOptions({ data: events.value.slice(start, start + eventPageSize.value) });
});
function changeEventSize(size: number) {
  eventPageSize.value = size;
  eventPage.value = 1;
}
function lastEventSeq() {
  return events.value.at(-1)?.seq ?? 0;
}
async function loadEvents() {
  const current = session;
  if (!current || streamError.value) return;
  try {
    const page = await taskAPI.events(current.id, lastEventSeq());
    if (current === session && !streamError.value) events.value = mergeEvents(events.value, page.items ?? []);
  } catch (reason) {
    if (current === session && !streamError.value) failed(reason);
  }
}
async function control(retry: boolean) {
  const permission=retry?'task:retry':'task:cancel';
  const current = session;
  if (!current || !access.accessCodes.includes(permission) || busy.value || !task.value) return;
  try {
    await ElMessageBox.confirm(
      retry ? '重试会沿用原任务快照，目标机下次引导可能重新安装。确认重试？' : '确认取消该任务？',
      retry ? '重试任务' : '取消任务',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  if (current !== session || busy.value || !access.accessCodes.includes(permission)) return;
  busy.value = true;
  error.value = '';
  problems.value = [];
  try {
    const result = await (retry ? taskAPI.retry(current.id) : taskAPI.cancel(current.id));
    if (current !== session) return;
    task.value = result;
    await loadEvents();
  } catch (reason) {
    if (current === session) failed(reason);
  } finally {
    if (current === session) busy.value = false;
  }
}
async function renderPreview() {
  const current = session;
  if (!current || busy.value) return;
  busy.value = true;
  try {
    const result = await taskAPI.preview(current.id);
    if (current !== session) return;
    preview.value = result;
    showPreview.value = true;
  } catch (reason) {
    if (current === session) failed(reason);
  } finally {
    if (current === session) busy.value = false;
  }
}
watch([id, () => access.accessToken], ([taskId, token], _old, cleanup) => {
  task.value = undefined;
  error.value = '';
  streamError.value = '';
  problems.value = [];
  events.value = [];
  eventPage.value = 1;
  preview.value = undefined;
  showPreview.value = false;
  busy.value = false;
  if (!taskId || !token) return;

  const current: Session = { id: taskId, reloading: false, reloadPending: false };
  session = current;
  void reload();
  void loadEvents();
  stream.value = useTaskStream({
    after: lastEventSeq,
    onEvent: queueEvent,
    onRejected: (status) => {
      if (session !== current) return;
      streamError.value = status === 403 ? '没有权限读取任务事件' : '任务不存在';
      clearInterval(poll);
    },
    onUnauthorized: () => {
      if (session === current && access.accessToken === token) void auth.clearSession();
    },
    taskId,
    token,
    url: taskStreamURL(apiURL, taskId),
  });
  const poll = setInterval(() => {
    void reload();
    if (!connected.value) void loadEvents();
  }, 5000);
  cleanup(() => {
    session = undefined;
    stream.value?.stop();
    stream.value = undefined;
    clearInterval(poll);
    clearTimeout(eventFlush);
    eventFlush = undefined;
    pendingEvents.clear();
  });
}, { immediate: true, flush: 'sync' });
</script>

<template>
  <Page :title="`任务 ${id}`" description="查看固化快照、服务端事件与操作限制。">
    <ElAlert v-if="streamError || error" :title="streamError || error" type="error" :closable="false" class="mb-3" />
    <ProblemList :problems="problems" />
    <ElCard v-if="task" class="mb-4">
      <div class="mb-3 flex flex-wrap items-center gap-4">
        <strong>{{ task.hostSN }}</strong>
        <ElTag>{{ taskStateLabels[task.state] }}</ElTag>
        <ElTag :type="connected ? 'success' : 'info'">
          {{ streamError ? '日志连接已停止' : connected ? '实时连接已建立' : '正在重连，定时补读事件' }}
        </ElTag>
        <ElButton @click="reload">刷新</ElButton>
        <ElButton :loading="busy" @click="renderPreview">查看下发文件</ElButton>
        <ElButton
          v-if="access.accessCodes.includes('task:cancel')"
          :disabled="!!task.canCancel?.length"
          :loading="busy"
          @click="control(false)"
        >
          取消任务
        </ElButton>
        <ElButton
          v-if="access.accessCodes.includes('task:retry')"
          :disabled="!!task.canRetry?.length"
          :loading="busy"
          @click="control(true)"
        >
          重试任务
        </ElButton>
      </div>
      <ProblemList :problems="task.canCancel ?? []" />
      <ProblemList :problems="task.canRetry ?? []" />
      <div class="grid gap-3 md:grid-cols-2">
        <div>ISO：{{ task.snapshot.iso.name }} · {{ task.snapshot.iso.version }}</div>
        <div>模板：{{ task.snapshot.template.name }} · v{{ task.templateVersion }}</div>
        <div>创建人：{{ task.createdBy }}</div>
        <div>创建时间：{{ new Date(task.createdAt * 1000).toLocaleString() }}</div>
        <div>
          登记属性：{{
            Object.entries(task.snapshot.host.attrs ?? {})
              .map(([key, value]) => `${key}=${value}`)
              .join('；')
          }}
        </div>
      </div>
    </ElCard>
    <ElCard header="任务事件">
      <EventGrid />
      <ListPagination
        :current-page="eventPage"
        :page-size="eventPageSize"
        :total="events.length"
        @page-change="eventPage = $event"
        @size-change="changeEventSize"
      />
      <ElButton :disabled="!!streamError" @click="loadEvents">补读后续事件</ElButton>
    </ElCard>
    <ElCard v-if="task" class="mt-4" header="固化参数与 hook">
      <div v-for="slot in task.snapshot.slots" :key="slot.seq" class="mb-3">
        <strong>{{ slot.name }} · {{ slot.timing || '片段' }}</strong>
        <div v-for="(instance, index) in slot.instances" :key="index" class="ml-3 mt-2">
          <span>
            {{ index + 1 }}. {{ instance.hookName }} v{{ instance.hookVersion }} ·
            {{ instance.required ? '必需' : '可选' }}
          </span>
          <div class="text-sm">
            {{
              Object.entries(instance.params ?? {})
                .map(([key, value]) => `${key}=${(value.items ?? []).join(', ')}`)
                .join('；')
            }}
          </div>
        </div>
      </div>
    </ElCard>
    <ElDialog v-model="showPreview" title="固化快照渲染预览" width="85%">
      <ElAlert
        v-if="preview?.sample?.length"
        :title="`含示例值：${preview.sample.join('、')}`"
        type="info"
        :closable="false"
      />
      <ElTabs>
        <ElTabPane
          v-for="file in preview?.files"
          :key="`${file.kind}/${file.name}/${file.slot}`"
          :label="file.name || file.kind"
        >
          <pre
            class="max-h-[60vh] overflow-auto whitespace-pre-wrap rounded bg-gray-100 p-4 text-sm dark:bg-gray-900"
            >{{ file.body }}</pre>
        </ElTabPane>
      </ElTabs>
    </ElDialog>
  </Page>
</template>
