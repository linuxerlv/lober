<script lang="ts" setup>
import type { ConsoleTaskApi } from '#/api/console/task';
/**
 * 任务详情页（SPEC §12.6）：状态机时间线、事件表、SSE 实时日志、
 * 失败详情（§3.5 的六个字段）。
 *
 * state=failed 本身无用，失败详情的六项才是运维要看的；last_signal_at
 * 区分度最高——刚收到信号说明机器活着、是脚本或 installer 逻辑错误，
 * 很久没信号说明机器层面出事，两个排查方向完全不同（§3.5）。
 *
 * SSE 用 fetch 流，不用 EventSource（§12.8，实现见
 * src/composables/use-task-stream.ts）：
 * - 断线指数退避重连，重连带 Last-Event-ID，服务端按 seq 续传，不从头重放
 * - 组件卸载必须 abort，否则列表与详情来回切会攒住一堆连接
 * - 任务进终态由服务端主动关流，前端收到终态即停止重连
 */
import type { TaskStreamEvent } from '#/composables/use-task-stream';

import {
  computed,
  onBeforeUnmount,
  onMounted,
  ref,
  shallowRef,
  watch,
} from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { useAppConfig } from '@vben/hooks';
import { useAccessStore, useUserStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDescriptions,
  ElDescriptionsItem,
  ElMessage,
  ElMessageBox,
  ElTable,
  ElTableColumn,
  ElTag,
  ElTimeline,
  ElTimelineItem,
} from 'element-plus';

import {
  cancelTaskApi,
  getTaskEventsApi,
  getTasksApi,
  retryTaskApi,
  taskStreamUrl,
} from '#/api/console/task';
import { useTaskStream } from '#/composables/use-task-stream';

defineOptions({ name: 'TaskDetail' });

const route = useRoute();
const router = useRouter();
const accessStore = useAccessStore();
const userStore = useUserStore();
const { apiURL } = useAppConfig(import.meta.env, import.meta.env.PROD);

const taskId = computed(() => String(route.params.id));

// ---------------------------------------------------------------------------
// 任务本体与事件历史
// ---------------------------------------------------------------------------

/**
 * 契约缺口（如实记录，供 contractGaps 汇报）：
 *
 * docs/contracts/http-console.md 的任务分组没有 `GET /tasks/{id}`（取单条）
 * 端点，只有 `GET /tasks?state=&sn=`（列表）与 `GET /tasks/{id}/events`。
 * src/api/console/task.ts（另一位 agent 的产出）同样没有导出任何按 id 取
 * 单条任务的函数，与「模板」分组缺 GET /templates 是同一类缺口——按 id 查
 * 详情理论上比列表更基础，任务列表页、任务详情页两处都要用到，但契约冻结
 * 时只顾了列表和事件两个端点。
 *
 * 本页面的应对：调用 getTasksApi() 拉全量任务列表后按 id 客户端过滤取出
 * 目标任务。付出的代价是每次刷新详情页都要拉一次全量任务列表（对约定 2
 * 里“百级规模”的任务表可接受），换来不用等待端点补齐就能先把页面跑起来。
 * retry/cancel 成功后用返回的 TaskItem 直接替换本地状态，不用重新拉列表。
 */
const task = shallowRef<ConsoleTaskApi.TaskItem>();
const events = ref<ConsoleTaskApi.TaskEventItem[]>([]);
const loading = ref(false);
const taskLoadError = ref<string>();

async function loadTaskAndEvents() {
  loading.value = true;
  taskLoadError.value = undefined;
  try {
    const [tasks, eventList] = await Promise.all([
      getTasksApi(),
      getTaskEventsApi(taskId.value),
    ]);
    const found = tasks.find(
      (t: ConsoleTaskApi.TaskItem) => t.id === taskId.value,
    );
    if (!found) {
      taskLoadError.value = '任务不存在或已被删除';
    }
    task.value = found;
    events.value = eventList;
  } catch (error) {
    taskLoadError.value =
      error instanceof Error ? error.message : String(error);
  } finally {
    loading.value = false;
  }
}

// ---------------------------------------------------------------------------
// SSE 实时事件流
// ---------------------------------------------------------------------------

const streamState = ref<'closed' | 'connecting' | 'error' | 'open'>(
  'connecting',
);
let stopStream: (() => void) | undefined;

/** 按 seq 去重合并 SSE 推来的事件——历史事件（events 接口）与实时事件可能重叠。 */
function appendStreamEvent(event: TaskStreamEvent) {
  const exists = events.value.some(
    (e: ConsoleTaskApi.TaskEventItem) => e.seq === event.seq,
  );
  if (!exists) {
    events.value = [
      ...events.value,
      event as ConsoleTaskApi.TaskEventItem,
    ].toSorted((a, b) => a.seq - b.seq);
  }
  if (task.value) {
    // 实时更新任务当前阶段，时间线随日志推进而不必整页刷新。
    task.value = {
      ...task.value,
      currentPhase: event.phase as ConsoleTaskApi.Phase,
    };
  }
}

function startStream() {
  stopStream?.();
  // apiURL 与 taskStreamUrl 的拼接方式与 requestClient 内其它调用一致
  // （baseURL + 相对路径），不额外拼 /console/v1——该前缀由部署环境的代理层
  // 负责（与 requestClient 的其它请求同一套代理规则），本页面不重复假设
  // 一个可能与之不一致的绝对路径。
  const { state, stop } = useTaskStream({
    url: `${apiURL}${taskStreamUrl(taskId.value)}`,
    authorizationHeader: () =>
      accessStore.accessToken ? `Bearer ${accessStore.accessToken}` : undefined,
    onEvent: appendStreamEvent,
    onTerminal: () => {
      // 终态已到，刷新一次事件列表兜底（防止 SSE 期间漏收的事件），
      // 不再重连——由 composable 内部保证。
      void loadTaskAndEvents();
    },
  });
  streamState.value = state.value;
  const unwatch = watch(state, (value) => {
    streamState.value = value;
  });
  stopStream = () => {
    unwatch();
    stop();
  };
}

// ---------------------------------------------------------------------------
// 失败详情（SPEC §3.5 六个字段）
// ---------------------------------------------------------------------------

const isFailed = computed(() => task.value?.state === 'failed');
const isTerminal = computed(
  () => task.value?.state === 'done' || task.value?.state === 'failed',
);

/** 距上次信号多久——区分度最高的一项，直接算差值而不是原样显示时间戳。 */
const sinceLastSignalText = computed(() => {
  const t = task.value;
  if (!t?.lastSignalAt) {
    return '从未收到信号';
  }
  const seconds = Math.max(0, Math.floor(Date.now() / 1000) - t.lastSignalAt);
  if (seconds < 60) return `${seconds} 秒前`;
  if (seconds < 3600) return `${Math.floor(seconds / 60)} 分钟前`;
  if (seconds < 86_400) return `${Math.floor(seconds / 3600)} 小时前`;
  return `${Math.floor(seconds / 86_400)} 天前`;
});

// ---------------------------------------------------------------------------
// 操作：retry / cancel
// ---------------------------------------------------------------------------

const actionLoading = ref(false);

async function onRetry() {
  try {
    await ElMessageBox.confirm(
      '重试会重置尝试次数并签发新的装机凭据，旧凭据立即失效。若该任务此前因超时失败且机器可能已经装好、盘上已有数据，服务端会拒绝重试。',
      '确认重试',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  actionLoading.value = true;
  try {
    task.value = await retryTaskApi(taskId.value);
    ElMessage.success('已重试，等待机器重新引导');
    startStream();
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '重试失败');
  } finally {
    actionLoading.value = false;
  }
}

async function onCancel() {
  try {
    await ElMessageBox.confirm(
      '取消后该主机的后续引导请求将不再被响应，此任务进入终态且不可恢复。',
      '确认取消任务',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  actionLoading.value = true;
  try {
    task.value = await cancelTaskApi(taskId.value);
    ElMessage.success('任务已取消');
  } catch (error) {
    ElMessage.error(error instanceof Error ? error.message : '取消失败');
  } finally {
    actionLoading.value = false;
  }
}

function backToList() {
  router.push('/tasks');
}

/**
 * 溯源视图（端点 34）只对 admin 开放。这里据此决定按钮是否渲染——渲染一个
 * 点下去必然 403 的按钮只会制造困惑，而不是"更方便发现功能"。
 */
const canViewProvenance = computed(() =>
  userStore.userRoles.includes('admin'),
);

/**
 * 跳到报表页的溯源标签并带上当前任务 id。
 *
 * 用 query 传 id 而不是新建一个 /tasks/:id/provenance 路由：溯源的四块内容与
 * 报表页的其它三个标签共用同一套时间范围控件与导出逻辑，拆成独立页面要把那些
 * 复制一遍。
 */
function goProvenance() {
  router.push({
    name: 'ReportsIndex',
    query: { tab: 'provenance', taskId: taskId.value },
  });
}

// ---------------------------------------------------------------------------
// 生命周期
// ---------------------------------------------------------------------------

onMounted(async () => {
  await loadTaskAndEvents();
  startStream();
});

watch(taskId, async () => {
  // 路由内 id 变化（例如从事件里跳到另一个任务）时，必须先停旧连接再开新
  // 连接——否则会同时存在两条指向不同任务的 SSE 连接。
  stopStream?.();
  events.value = [];
  await loadTaskAndEvents();
  startStream();
});

onBeforeUnmount(() => {
  stopStream?.();
});
</script>

<template>
  <div class="p-4">
    <div class="mb-4 flex items-center justify-between">
      <div class="flex items-center gap-2">
        <ElButton @click="backToList">返回列表</ElButton>
        <!-- 溯源直达入口（P4-04 任务 5）：事故排查时运维是从任务详情页开始
             的，所以主入口挂在这里，不只在报表页。报表页那一栏是给「只记得
             大概时间、要先在审计里找到任务 id」那条路径用的。
             只对 admin 显示——端点 34 要求 roleAdmin，对其他角色显示一个点
             下去必然 403 的按钮只会制造困惑。 -->
        <ElButton v-if="canViewProvenance" @click="goProvenance">
          查看溯源
        </ElButton>
      </div>
      <ElTag
        :type="
          streamState === 'open'
            ? 'success'
            : streamState === 'error'
              ? 'danger'
              : 'info'
        "
      >
        实时日志：{{
          isTerminal
            ? '任务已结束，连接已关闭'
            : streamState === 'open'
              ? '已连接'
              : streamState === 'connecting'
                ? '连接中'
                : streamState === 'error'
                  ? '断线重连中'
                  : '已关闭'
        }}
      </ElTag>
    </div>

    <ElAlert
      v-if="taskLoadError"
      type="error"
      :closable="false"
      title="加载任务详情失败（疑似契约缺口：契约未定义 GET /tasks/{id} 单条端点，详见本文件顶部注释）"
      :description="taskLoadError"
      class="mb-4"
    />

    <ElCard class="mb-4">
      <template #header>任务状态</template>
      <ElDescriptions :column="3" border v-loading="loading">
        <ElDescriptionsItem label="任务 id">{{ taskId }}</ElDescriptionsItem>
        <ElDescriptionsItem label="状态">
          {{
            task?.state === 'pending'
              ? '等待机器引导，随时可能开始装机'
              : task?.state
          }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="当前阶段">
          {{ task?.currentPhase }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="SN">{{ task?.hostSn }}</ElDescriptionsItem>
        <ElDescriptionsItem label="模板版本">
          {{ task?.templateVersion }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="尝试次数">
          {{ task?.attemptCount }}
        </ElDescriptionsItem>
      </ElDescriptions>

      <div class="mt-4 flex gap-2">
        <ElButton
          type="primary"
          :loading="actionLoading"
          :disabled="isTerminal"
          @click="onRetry"
        >
          重试
        </ElButton>
        <ElButton
          type="danger"
          :loading="actionLoading"
          :disabled="isTerminal"
          @click="onCancel"
        >
          取消任务
        </ElButton>
      </div>
    </ElCard>

    <!-- 失败详情：SPEC §3.5 的六个字段全部展示，last_signal_at 单独换算成
         “距今多久”而不是原样显示时间戳——这是区分度最高的一项。 -->
    <ElCard v-if="isFailed" class="mb-4">
      <template #header>
        <span class="text-red-600">失败详情</span>
      </template>
      <ElAlert
        type="error"
        :closable="false"
        :title="`失败原因：${task?.failureReason || '未分类'}`"
        class="mb-4"
      />
      <ElDescriptions :column="2" border>
        <ElDescriptionsItem label="阶段">
          {{ task?.failureDetail?.phase }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="退出码">
          {{ task?.failureDetail?.exitCode }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="hook digest">
          {{ task?.failureDetail?.hookDigest || '（非 hook 失败）' }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="距上次信号多久">
          {{ sinceLastSignalText }}
        </ElDescriptionsItem>
        <ElDescriptionsItem label="日志尾部" :span="2">
          <pre class="max-h-40 overflow-auto whitespace-pre-wrap">{{
            task?.failureDetail?.logTail
          }}</pre>
        </ElDescriptionsItem>
        <ElDescriptionsItem label="syslog 尾部" :span="2">
          <pre class="max-h-40 overflow-auto whitespace-pre-wrap">{{
            task?.failureDetail?.syslogTail
          }}</pre>
        </ElDescriptionsItem>
      </ElDescriptions>
    </ElCard>

    <ElCard class="mb-4">
      <template #header>状态机时间线</template>
      <ElTimeline>
        <ElTimelineItem
          v-for="event in events"
          :key="event.seq"
          :timestamp="new Date(event.ts * 1000).toLocaleString()"
          :type="event.level === 'error' ? 'danger' : 'primary'"
        >
          [{{ event.phase }}] {{ event.message }}
        </ElTimelineItem>
      </ElTimeline>
    </ElCard>

    <ElCard>
      <template #header>事件表 / 实时日志</template>
      <ElTable :data="events" size="small" height="360" v-loading="loading">
        <ElTableColumn prop="seq" label="序号" width="80" />
        <ElTableColumn prop="ts" label="时间" width="180">
          <template #default="{ row }">
            {{ new Date(row.ts * 1000).toLocaleString() }}
          </template>
        </ElTableColumn>
        <ElTableColumn prop="phase" label="阶段" width="120" />
        <ElTableColumn prop="level" label="级别" width="80" />
        <ElTableColumn prop="message" label="消息" />
        <ElTableColumn prop="source" label="来源" width="100" />
      </ElTable>
    </ElCard>
  </div>
</template>
