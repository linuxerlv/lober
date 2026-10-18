<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';
import type { Task, TaskEvent } from '#/api/console/task';

import { onBeforeUnmount, onMounted, ref } from 'vue';
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
import ProblemList from '#/components/console/ProblemList.vue';

import { eventDetails } from './event-details';
import { taskStateLabels } from './task-state';
import { mergeEvents } from './wizard-state';

defineOptions({ name: 'TasksDetail' });
const route = useRoute();
const access = useAccessStore();
const { apiURL } = useAppConfig(import.meta.env, import.meta.env.PROD);
const id = String(route.params.id);
const task = ref<Task>();
const error = ref('');
const problems = ref<Schemas['ProblemJSON'][]>([]);
const events = ref<TaskEvent[]>([]);
const connected = ref(false);
const preview = ref<Schemas['PreviewResponse']>();
const showPreview = ref(false);
const busy = ref(false);
let source: EventSource | undefined;
let poll: ReturnType<typeof setInterval> | undefined;
let active = true;
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
  try {
    const result = await taskAPI.get(id);
    if (active) task.value = result;
  } catch (reason) {
    if (active) failed(reason);
  }
}
function append(incoming: TaskEvent[]) {
  if (!active) return;
  events.value = mergeEvents(events.value, incoming);
  eventGrid.setGridOptions({ data: events.value });
}
async function loadEvents() {
  try {
    const page = await taskAPI.events(id, events.value.at(-1)?.seq ?? 0);
    append(page.items ?? []);
  } catch (reason) {
    if (active) failed(reason);
  }
}
function connect() {
  source?.close();
  if (!access.accessToken) return;
  source = new EventSource(
    taskStreamURL(apiURL, id, access.accessToken, events.value.at(-1)?.seq ?? 0),
  );
  source.onopen = () => {
    connected.value = true;
  };
  source.onerror = () => {
    connected.value = false;
  };
  source.addEventListener('task', (message) => {
    try {
      const event: TaskEvent = JSON.parse((message as MessageEvent<string>).data);
      if (event.taskID === id && Number.isSafeInteger(event.seq)) {
        append([event]);
        void reload();
      }
    } catch {
      connected.value = false;
    }
  });
}
async function control(retry: boolean) {
  const permission=retry?'task:retry':'task:cancel';
  if (!access.accessCodes.includes(permission) || busy.value || !task.value) return;
  try {
    await ElMessageBox.confirm(
      retry ? '重试会沿用原任务快照，目标机下次引导可能重新安装。确认重试？' : '确认取消该任务？',
      retry ? '重试任务' : '取消任务',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  if (!access.accessCodes.includes(permission)) return;
  busy.value = true;
  error.value = '';
  problems.value = [];
  try {
    task.value = await (retry ? taskAPI.retry(id) : taskAPI.cancel(id));
    await loadEvents();
  } catch (reason) {
    failed(reason);
  } finally {
    busy.value = false;
  }
}
async function renderPreview() {
  busy.value = true;
  try {
    preview.value = await taskAPI.preview(id);
    showPreview.value = true;
  } catch (reason) {
    failed(reason);
  } finally {
    busy.value = false;
  }
}
onMounted(async () => {
  await reload();
  await loadEvents();
  if (!active) return;
  connect();
  poll = setInterval(() => {
    void reload();
    if (!connected.value) void loadEvents();
  }, 5000);
});
onBeforeUnmount(() => {
  active = false;
  source?.close();
  if (poll) clearInterval(poll);
});
</script>

<template>
  <Page :title="`任务 ${id}`" description="查看固化快照、服务端事件与操作限制。">
    <ElAlert v-if="error" :title="error" type="error" :closable="false" class="mb-3" />
    <ProblemList :problems="problems" />
    <ElCard v-if="task" class="mb-4">
      <div class="mb-3 flex flex-wrap items-center gap-4">
        <strong>{{ task.hostSN }}</strong>
        <ElTag>{{ taskStateLabels[task.state] }}</ElTag>
        <ElTag :type="connected ? 'success' : 'info'">
          {{ connected ? '实时连接已建立' : '正在重连，定时补读事件' }}
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
      <ElButton @click="loadEvents">补读后续事件</ElButton>
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
