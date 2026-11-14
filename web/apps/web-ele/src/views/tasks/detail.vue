<script setup lang="ts">
import type { Schemas } from '#/api/console/generated';
import type { Task, TaskEvent } from '#/api/console/task';

import { computed, nextTick, ref, shallowRef, watch } from 'vue';
import { useRoute } from 'vue-router';

import { Page } from '@vben/common-ui';
import { useAppConfig } from '@vben/hooks';
import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDialog,
  ElMessage,
  ElMessageBox,
  ElTabPane,
  ElTabs,
  ElTag,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { errorMessage, problemsOf } from '#/api/console/problem';
import { taskAPI, taskStreamURL } from '#/api/console/task';
import ChangeRecords from '#/components/console/ChangeRecords.vue';
import ListPagination from '#/components/console/ListPagination.vue';
import ProblemList from '#/components/console/ProblemList.vue';
import { useTaskStream } from '#/composables/use-task-stream';
import { useAuthStore } from '#/store';

import { eventDetails, findFailureEvent } from './event-details';
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
const section = ref('overview');
const recentEvents = computed(() => events.value.slice(-4).reverse());
const locatedEvent = ref<TaskEvent>();
const locatedPanel = ref<HTMLElement>();
const failure = computed(() =>
  findFailureEvent(events.value, (event) => {
    const slot = task.value?.snapshot.slots?.find(
      (row) => row.seq === Number(event.fields?.slot),
    );
    return (
      slot?.instances?.[Number(event.fields?.instance) - 1]?.required === true
    );
  }),
);
const canReadAudit = computed(() => access.accessCodes.includes('audit:read'));
const sections = computed(() => [
  ['overview', '运行情况'],
  ['config', '安装配置'],
  ['events', '完整日志'],
  ...(canReadAudit.value ? [['audit', '变更记录']] : []),
]);
const fromBatch = computed(() => task.value?.batchID === route.query?.fromBatch ? task.value?.batchID : undefined);
const fromHost = computed(() => {
  const host = route.query?.fromHost;
  return typeof host === 'string' && host === task.value?.hostSN
    ? host
    : undefined;
});
async function locateFailure() {
  const event = failure.value?.event;
  section.value = 'events';
  locatedEvent.value = event;
  if (event) {
    const index = events.value.findIndex((row) => row.seq === event.seq);
    eventPage.value = Math.floor(index / eventPageSize.value) + 1;
    await nextTick();
    locatedPanel.value?.focus();
  }
}
const phase = computed(() => {
  const states = [
    'pending',
    'booting',
    'installing',
    'rebooting',
    'initializing',
    'done',
  ];
  return states.indexOf(task.value?.state ?? '');
});
const eventLabels: Record<string, string> = {
  'task.created': '任务已创建',
  'machine.boot': '主机开始引导',
  'machine.cancel': '任务已取消',
  'machine.retry': '任务已请求重试',
  'machine.installing': '开始安装',
  'machine.reboot': '主机重启',
  'machine.init': '开始初始化',
  'machine.done': '安装已完成',
  'machine.failed': '任务失败',
  'installer.syslog': '安装程序日志',
};
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
        formatter: ({ row }: { row: TaskEvent }) => eventDetails(row.fields),
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
  eventGrid.setGridOptions({
    data: events.value.slice(start, start + eventPageSize.value),
  });
});
function changeEventSize(size: number) {
  eventPageSize.value = size;
  eventPage.value = 1;
  locatedEvent.value = undefined;
}
function lastEventSeq() {
  return events.value.at(-1)?.seq ?? 0;
}
async function loadEvents() {
  const current = session;
  if (!current || streamError.value) return;
  try {
    const page = await taskAPI.events(current.id, lastEventSeq());
    if (current === session && !streamError.value)
      events.value = mergeEvents(events.value, page.items ?? []);
  } catch (reason) {
    if (current === session && !streamError.value) failed(reason);
  }
}
async function control(retry: boolean) {
  const permission = retry ? 'task:retry' : 'task:cancel';
  const current = session;
  if (
    !current ||
    !access.accessCodes.includes(permission) ||
    busy.value ||
    !task.value
  )
    return;
  if ((retry ? task.value.canRetry : task.value.canCancel)?.length) return;
  const rev = task.value.rev;
  try {
    await ElMessageBox.confirm(
      retry
        ? '重试会沿用原任务快照，目标机下次引导可能重新安装。确认重试？'
        : '确认取消该任务？',
      retry ? '重试任务' : '取消任务',
      {
        type: 'warning',
        confirmButtonText: retry ? '确认重试' : '确认取消',
        cancelButtonText: '返回',
      },
    );
  } catch {
    return;
  }
  if (
    current !== session ||
    busy.value ||
    !access.accessCodes.includes(permission) ||
    !task.value ||
    (retry ? task.value.canRetry : task.value.canCancel)?.length
  )
    return;
  busy.value = true;
  error.value = '';
  problems.value = [];
  try {
    const result = await (retry
      ? taskAPI.retry(current.id, rev)
      : taskAPI.cancel(current.id, rev));
    if (current !== session) return;
    task.value = result;
    if (retry) {
      locatedEvent.value = undefined;
      section.value = 'overview';
      ElMessage.success('已提交重试，任务状态已更新。');
    }
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
watch(
  [id, () => access.accessToken],
  ([taskId, token], _old, cleanup) => {
    task.value = undefined;
    error.value = '';
    streamError.value = '';
    problems.value = [];
    events.value = [];
    eventPage.value = 1;
    section.value = 'overview';
    locatedEvent.value = undefined;
    preview.value = undefined;
    showPreview.value = false;
    busy.value = false;
    if (!taskId || !token) return;

    const current: Session = {
      id: taskId,
      reloading: false,
      reloadPending: false,
    };
    session = current;
    void reload();
    void loadEvents();
    stream.value = useTaskStream({
      after: lastEventSeq,
      onEvent: queueEvent,
      onRejected: (status) => {
        if (session !== current) return;
        streamError.value =
          status === 403 ? '没有权限读取任务事件' : '任务不存在';
        clearInterval(poll);
      },
      onUnauthorized: () => {
        if (session === current && access.accessToken === token)
          void auth.clearSession();
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
  },
  { immediate: true, flush: 'sync' },
);
</script>

<template>
  <Page :title="task?.hostSN || '任务详情'" :description="`任务编号 ${id}`">
    <template #extra>
      <RouterLink
        v-if="fromBatch"
        class="text-primary"
        :to="{ name: 'TasksBatch', params: { id: fromBatch } }"
      >返回批次</RouterLink>
      <RouterLink
        v-else-if="fromHost"
        class="text-primary"
        :to="{ path: '/hosts', query: { host: fromHost } }"
        >返回主机详情</RouterLink
      >
      <RouterLink
        v-else
        class="text-primary"
        :to="{ path: '/tasks', query: route.query }"
        >返回任务列表</RouterLink
      >
    </template>
    <ElAlert
      v-if="streamError || error"
      :title="streamError || error"
      type="error"
      :closable="false"
      class="mb-3"
    />
    <ProblemList :problems="problems" />
    <div class="grid items-start gap-4 lg:grid-cols-[minmax(0,1fr)_280px]">
      <div class="grid min-w-0 gap-4">
        <ElCard v-if="task">
          <div class="mb-3 flex flex-wrap items-center gap-4">
            <strong>运行状态</strong>
            <ElTag
              :type="
                task.state === 'failed'
                  ? 'danger'
                  : task.state === 'done'
                    ? 'success'
                    : task.state === 'pending'
                      ? 'warning'
                      : 'info'
              "
              >{{ taskStateLabels[task.state] }}</ElTag
            >
            <ElTag :type="connected ? 'success' : 'info'">
              {{
                streamError
                  ? '日志连接已停止'
                  : connected
                    ? '实时连接已建立'
                    : '正在重连，定时补读事件'
              }}
            </ElTag>
            <ElButton @click="reload">刷新</ElButton>
            <ElButton
              v-if="
                access.accessCodes.includes('task:cancel') &&
                !task.canCancel?.length
              "
              :disabled="!!task.canCancel?.length"
              :loading="busy"
              @click="control(false)"
            >
              取消任务
            </ElButton>
            <ElButton
              v-if="
                access.accessCodes.includes('task:retry') &&
                !task.canRetry?.length
              "
              :disabled="!!task.canRetry?.length"
              :loading="busy"
              @click="control(true)"
            >
              重试任务
            </ElButton>
          </div>
          <ol
            v-if="phase >= 0"
            aria-label="装机阶段"
            class="grid grid-cols-2 gap-2 sm:grid-cols-5"
          >
            <li
              v-for="(label, index) in [
                '引导',
                '安装',
                '重启',
                '初始化',
                '完成',
              ]"
              :key="label"
              class="border-t-2 pt-2 text-xs"
              :class="
                index + 1 <= phase
                  ? 'border-primary font-semibold text-primary'
                  : 'border-border text-muted-foreground'
              "
              :aria-current="index + 1 === phase ? 'step' : undefined"
            >
              {{ label }}
            </li>
          </ol>
          <p
            v-if="task.state === 'pending'"
            class="mt-3 text-sm text-muted-foreground"
          >
            目标主机下次网络启动时可能开始安装。
          </p>
        </ElCard>
        <ElCard>
          <div
            v-if="task"
            class="mb-4 flex flex-wrap gap-2 border-b border-border pb-3"
          >
            <ElButton
              v-for="[key, label] in sections"
              :key="key"
              class="!ml-0"
              :type="section === key ? 'primary' : undefined"
              plain
              :aria-pressed="section === key"
              @click="section = key!"
              >{{ label }}</ElButton
            >
          </div>
          <ChangeRecords
            v-if="task && section === 'audit' && canReadAudit"
            target-kind="task"
            :target-id="task.id"
          />
          <div v-if="task && section === 'overview'">
            <section
              v-if="task.state === 'failed'"
              aria-label="失败原因"
              class="mb-4 rounded-lg border border-border bg-muted p-4"
            >
              <h2 class="mb-2 font-semibold">失败原因</h2>
              <p class="break-words">
                {{
                  failure?.summary || '暂无明确的失败原因，可查看完整日志排查。'
                }}
              </p>
              <div class="mt-3 flex flex-wrap items-center gap-3">
                <ElButton type="primary" plain @click="locateFailure">{{
                  failure ? '定位日志' : '查看日志'
                }}</ElButton>
                <ElButton
                  v-if="
                    access.accessCodes.includes('task:retry') &&
                    !task.canRetry?.length
                  "
                  :loading="busy"
                  @click="control(true)"
                  >重试</ElButton
                >
                <span
                  v-else-if="
                    access.accessCodes.includes('task:retry') &&
                    task.canRetry?.length
                  "
                  class="text-sm text-muted-foreground"
                  >{{ task.canRetry[0]?.message }}</span
                >
              </div>
            </section>
            <h2 class="mb-2 font-semibold">最近活动</h2>
            <p v-if="!recentEvents.length" class="text-muted-foreground">
              暂无任务事件
            </p>
            <div
              v-for="event in recentEvents"
              :key="event.seq"
              class="grid gap-1 border-b border-border py-3 sm:grid-cols-[145px_1fr]"
            >
              <time class="text-xs text-muted-foreground">{{
                new Date(event.at * 1000).toLocaleString()
              }}</time>
              <div>
                <strong>{{ eventLabels[event.kind] || event.kind }}</strong>
                <p v-if="event.fields?.reason" class="mt-1 text-sm">
                  {{ event.fields.reason }}
                </p>
              </div>
            </div>
          </div>
          <div v-show="!task || section === 'events'">
            <section
              v-if="locatedEvent"
              ref="locatedPanel"
              tabindex="-1"
              aria-label="定位到的失败日志"
              class="mb-4 rounded-lg border border-primary bg-muted p-4"
            >
              <h2 class="font-semibold">
                已定位到事件 #{{ locatedEvent.seq }}
              </h2>
              <p class="mt-1 text-sm text-muted-foreground">
                {{ eventLabels[locatedEvent.kind] || locatedEvent.kind }} ·
                {{ new Date(locatedEvent.at * 1000).toLocaleString() }}
              </p>
              <pre
                class="mt-2 max-h-52 overflow-auto whitespace-pre-wrap break-all text-sm"
                >{{
                  eventDetails(locatedEvent.fields) || '该事件没有附加日志。'
                }}</pre>
            </section>
            <EventGrid />
            <ListPagination
              :current-page="eventPage"
              :page-size="eventPageSize"
              :total="events.length"
              @page-change="eventPage = $event"
              @size-change="changeEventSize"
            />
            <ElButton :disabled="!!streamError" @click="loadEvents"
              >补读后续事件</ElButton
            >
          </div>
        </ElCard>
        <ElCard v-if="task && section === 'config'" header="创建时保存的配置">
          <ElButton :loading="busy" class="mb-3" @click="renderPreview"
            >查看下发文件</ElButton
          >
          <p class="mb-3 text-sm">
            登记属性：{{
              Object.entries(task.snapshot.host.attrs ?? {})
                .map(([key, value]) => `${key}=${value}`)
                .join('；') || '无'
            }}
          </p>
          <p
            v-if="!task.snapshot.slots?.length"
            class="text-sm text-muted-foreground"
          >
            此任务没有附加脚本。
          </p>
          <div v-for="slot in task.snapshot.slots" :key="slot.seq" class="mb-3">
            <strong>{{ slot.name }} · {{ slot.timing || '片段' }}</strong>
            <div
              v-for="(instance, index) in slot.instances"
              :key="index"
              class="ml-3 mt-2"
            >
              <span>
                {{ index + 1 }}. {{ instance.hookName }} v{{
                  instance.hookVersion
                }}
                ·
                {{ instance.required ? '必需' : '可选' }}
              </span>
              <div class="text-sm">
                {{
                  Object.entries(instance.params ?? {})
                    .map(
                      ([key, value]) =>
                        `${key}=${(value.items ?? []).join(', ')}`,
                    )
                    .join('；')
                }}
              </div>
            </div>
          </div>
        </ElCard>
      </div>
      <ElCard v-if="task" header="任务信息">
        <dl class="grid gap-3 text-sm">
          <div>
            <dt class="text-muted-foreground">系统镜像</dt>
            <dd>{{ task.snapshot.iso.name }}</dd>
          </div>
          <div>
            <dt class="text-muted-foreground">安装模板</dt>
            <dd>
              {{ task.snapshot.template.name }} · v{{ task.templateVersion }}
            </dd>
          </div>
          <div>
            <dt class="text-muted-foreground">创建人</dt>
            <dd>{{ task.createdBy }}</dd>
          </div>
          <div>
            <dt class="text-muted-foreground">创建时间</dt>
            <dd>{{ new Date(task.createdAt * 1000).toLocaleString() }}</dd>
          </div>
        </dl>
      </ElCard>
    </div>
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
