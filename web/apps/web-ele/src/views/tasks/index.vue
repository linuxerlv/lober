<script lang="ts" setup>
import type { VbenFormSchema } from '@vben/common-ui';

/**
 * 任务列表页（SPEC §12.6、契约「6. 任务」端点 22）。
 *
 * 按 state 过滤、按 SN 搜索（均是服务端精确匹配，不做子串搜索——
 * docs/contracts/http-console.md「GET /tasks?state=&sn=」小节），
 * 列表内 retry / cancel。
 *
 * 两处不能马虎的 UI 语义（任务文本硬要求）：
 * 1. state=pending 的文案：建任务后即接受引导，必须标成
 *    「等待机器引导，随时可能开始装机」，不能显示成中性的「未开始」——
 *    删掉 armed 状态之后 pending 就意味着这台机器下次网络启动就会被擦盘。
 * 2. retry 按钮对不可重装的失败态（rebooting 超时 / initializing 超时或
 *    失败三种失败原因，对应 model.Task.Reinstallable() === false，SPEC
 *    §3.4）要禁用并给出原因——这类机器已经装好、盘上可能有数据，重装会
 *    擦掉。服务端会硬拒绝（CodeConflict），UI 禁用只是体验，真正的防线
 *    在状态机里，不是靠隐藏按钮（§3.4 原文）。
 *
 * 列表页统一用 useVbenVxeGrid（§12.6 末句），不单独写 el-table 加分页——
 * 沿用 src/views/hooks/index.vue 与 src/views/artifact-sets/index.vue 已
 * 确立的写法：本页自带的本地类型 + 直接用 requestClient 调用，
 * proxyConfig.ajax.query 把契约的裸数组包成 { items } 喂给 vxe-table
 * （契约约定 2：data 就是数组，不带总数，前端做客户端分页）。
 */
import { useRoute, useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElMessage,
  ElMessageBox,
  ElTag,
  ElTooltip,
} from 'element-plus';

import { useVbenVxeGrid } from '#/adapter/vxe-table';
import { requestClient } from '#/api/request';

defineOptions({ name: 'TasksIndex' });

/** 对应 model.TaskState（task.go），七个取值，终态是 done 与 failed */
type TaskState =
  | 'booting'
  | 'done'
  | 'failed'
  | 'initializing'
  | 'installing'
  | 'pending'
  | 'rebooting';

/** 对应 model.Phase（task.go） */
type Phase =
  | 'booting'
  | 'done'
  | 'initializing'
  | 'packages'
  | 'pending'
  | 'post'
  | 'pre'
  | 'rebooting';

/**
 * 对应 model.FailureReason（task.go），空串表示未失败。
 *
 * NON_REINSTALLABLE_REASONS 三项对应 Reinstallable() === false（SPEC §3.4）：
 * rebooting 超时（系统已装好但起不来）、initializing 超时或失败（系统已
 * 就绪但配置未完成）——这两类机器盘上可能已有数据，重装会擦掉。这里照抄
 * 白名单取反的结果用于禁用态展示，不在前端重新实现判定逻辑，真正拒绝
 * retry 的是服务端状态机。
 */
type FailureReason =
  | ''
  | 'attempt_exceeded'
  | 'boot_timeout'
  | 'canceled'
  | 'hook_required'
  | 'init_failed'
  | 'init_timeout'
  | 'install_phase'
  | 'precheck'
  | 'reboot_timeout';

const NON_REINSTALLABLE_REASONS: ReadonlySet<FailureReason> = new Set([
  'init_failed',
  'init_timeout',
  'reboot_timeout',
]);

const NON_REINSTALLABLE_REASON_LABEL: Record<string, string> = {
  reboot_timeout:
    '重启超时——系统已装好但起不来，机器盘上可能已有数据，重装会擦除',
  init_timeout:
    '初始化超时——系统已就绪但配置未完成，机器盘上已有数据，重装会擦除',
  init_failed:
    '初始化失败——系统已就绪但配置未完成，机器盘上已有数据，重装会擦除',
};

interface FailureDetail {
  exitCode: number;
  hookDigest: string;
  logTail: string;
  phase: Phase;
  sinceLastSig: string;
  syslogTail: string;
}

interface TaskItem {
  artifactSetId: string;
  attemptCount: number;
  createdAt: number;
  createdBy: string;
  currentPhase: Phase;
  failureDetail: FailureDetail | null;
  failureReason: FailureReason;
  formData: Record<string, unknown>;
  hookDigests: string[];
  hostMac: string;
  hostSn: string;
  hostUuid: string;
  id: string;
  lastSeq: number;
  lastSignalAt: number;
  phaseEnteredAt: number;
  resolvedParams: Record<string, unknown>;
  state: TaskState;
  stateUpdatedAt: number;
  templateId: string;
  templateVersion: number;
  version: number;
}

const STATE_TAG_TYPE: Record<
  TaskState,
  'danger' | 'info' | 'primary' | 'success' | 'warning'
> = {
  pending: 'warning',
  booting: 'info',
  initializing: 'info',
  installing: 'primary',
  rebooting: 'info',
  done: 'success',
  failed: 'danger',
};

/**
 * state 展示文案。pending 是本页唯一不能显示成中性文案的一项（任务硬要求
 * 1）——其余状态原样展示英文取值即可（服务端未定义中文文案，不擅自发明）。
 */
const STATE_LABEL: Record<TaskState, string> = {
  pending: '等待机器引导，随时可能开始装机',
  booting: 'booting',
  initializing: 'initializing',
  installing: 'installing',
  rebooting: 'rebooting',
  done: 'done',
  failed: 'failed',
};

const STATE_OPTIONS = (Object.keys(STATE_LABEL) as TaskState[]).map(
  (value) => ({
    label: STATE_LABEL[value],
    value,
  }),
);

function isNonReinstallable(reason: FailureReason): boolean {
  return NON_REINSTALLABLE_REASONS.has(reason);
}

/** retry 是否应当在 UI 上禁用——仅体验层面，真正拒绝在服务端状态机（§3.4）。 */
function retryDisabled(row: TaskItem): boolean {
  if (row.state !== 'failed') {
    // 非终态失败任务不能 retry（不是"未开始"也不是"已成功"，retry 语义上
    // 只适用于失败任务）。
    return true;
  }
  return isNonReinstallable(row.failureReason);
}

function retryDisabledReason(row: TaskItem): string {
  if (row.state !== 'failed') {
    return '仅失败任务可以重试';
  }
  return (
    NON_REINSTALLABLE_REASON_LABEL[row.failureReason] ??
    '该失败状态的机器已完成安装，重装会擦除磁盘数据'
  );
}

function cancelDisabled(row: TaskItem): boolean {
  return row.state === 'done' || row.state === 'failed';
}

/** 列表过滤表单：state 精确匹配，sn 精确匹配（契约「GET /tasks?state=&sn=」，非子串搜索）。 */
const filterFormSchema: VbenFormSchema[] = [
  {
    component: 'Select',
    componentProps: {
      clearable: true,
      options: STATE_OPTIONS,
      placeholder: '按状态过滤',
    },
    fieldName: 'state',
    label: '状态',
  },
  {
    component: 'Input',
    componentProps: {
      clearable: true,
      placeholder: '按 SN 精确匹配（非模糊搜索）',
    },
    fieldName: 'sn',
    label: 'SN',
  },
];

const route = useRoute();
const router = useRouter();

/**
 * 失败分布报表的下钻落点（P4-04 任务 3）：报表页点某个失败原因跳到这里，
 * 带 `?failureReason=precheck`，列表据此过滤。
 *
 * 从 URL 取而不是做成筛选表单的一项：它不是日常筛选维度（运维平时按状态和 SN
 * 找任务），而是报表下钻这一条路径专用的。**不接这个参数的话下钻等于没过滤**，
 * 而那正是计划里「不可下钻的失败分布只是一个数字」要避免的情形。
 */
const drillDownReason = String(route.query.failureReason ?? '');

async function queryTasks(formValues: Record<string, any>) {
  const params: Record<string, string> = {};
  if (formValues.state) params.state = formValues.state;
  if (formValues.sn) params.sn = formValues.sn;
  if (drillDownReason) params.failureReason = drillDownReason;
  const items = await requestClient.get<TaskItem[]>('/tasks', { params });
  return { items };
}

const [Grid, gridApi] = useVbenVxeGrid<TaskItem>({
  formOptions: {
    schema: filterFormSchema,
  },
  gridOptions: {
    columns: [
      { field: 'hostSn', title: 'SN', width: 160 },
      {
        field: 'state',
        title: '状态',
        minWidth: 220,
        slots: { default: 'state' },
      },
      { field: 'currentPhase', title: '当前阶段', width: 120 },
      { field: 'templateVersion', title: '模板版本', width: 90 },
      { field: 'attemptCount', title: '尝试次数', width: 90 },
      {
        field: 'createdAt',
        title: '创建时间',
        width: 170,
        formatter: ({ cellValue }: { cellValue: number }) =>
          cellValue ? new Date(cellValue * 1000).toLocaleString() : '',
      },
      {
        field: 'action',
        title: '操作',
        width: 220,
        fixed: 'right',
        slots: { default: 'action' },
      },
    ],
    height: 'auto',
    keepSource: true,
    proxyConfig: {
      ajax: {
        query: async (_params: any, formValues: Record<string, any>) =>
          queryTasks(formValues),
      },
    },
    rowConfig: { keyField: 'id' },
    toolbarConfig: { refresh: true, search: true },
  },
});

function goToDetail(row: TaskItem) {
  router.push(`/tasks/${row.id}`);
}

function goToNew() {
  router.push('/tasks/new');
}

async function onRetry(row: TaskItem) {
  if (retryDisabled(row)) {
    // 正常情况下按钮已禁用不会走到这里，双重保险仍给出原因提示。
    ElMessage.warning(retryDisabledReason(row));
    return;
  }
  try {
    await ElMessageBox.confirm(
      '重试会重置尝试次数并签发新的装机凭据，旧凭据立即失效。',
      '确认重试',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  try {
    await requestClient.post<TaskItem>(`/tasks/${row.id}/retry`);
    ElMessage.success('已重试，等待机器重新引导');
    gridApi.reload();
  } catch (error: any) {
    const message =
      error?.response?.data?.message || error?.message || '重试失败';
    ElMessage.error(message);
  }
}

async function onCancel(row: TaskItem) {
  try {
    await ElMessageBox.confirm(
      '取消后该主机的后续引导请求将不再被响应，此任务进入终态且不可恢复。',
      '确认取消任务',
      { type: 'warning' },
    );
  } catch {
    return;
  }
  try {
    await requestClient.post<TaskItem>(`/tasks/${row.id}/cancel`);
    ElMessage.success('任务已取消');
    gridApi.reload();
  } catch (error: any) {
    const message =
      error?.response?.data?.message || error?.message || '取消失败';
    ElMessage.error(message);
  }
}
</script>

<template>
  <Page
    description="按状态过滤、按 SN 精确搜索；建任务是唯一的授权动作，入口见右上角。"
    title="任务列表"
  >
    <template #extra>
      <ElButton type="primary" @click="goToNew">建任务</ElButton>
    </template>

    <!-- 从失败分布报表下钻进来时给一条明示：列表已被 URL 上的失败原因过滤。
         不提示的话运维会以为「这个原因只有这几个任务」，而实际上是筛选条件在
         URL 里、筛选表单上看不到。 -->
    <ElAlert
      v-if="drillDownReason"
      :closable="false"
      class="mb-4"
      type="info"
    >
      已按失败原因 <strong>{{ drillDownReason }}</strong> 过滤（从失败分布报表
      下钻而来）。去掉地址栏的 <code>?failureReason=</code> 即可看全部任务。
    </ElAlert>

    <Grid>
      <template #state="{ row }">
        <ElTag :type="STATE_TAG_TYPE[row.state as TaskState]">
          {{ STATE_LABEL[row.state as TaskState] }}
        </ElTag>
      </template>
      <template #action="{ row }">
        <ElButton link size="small" @click="goToDetail(row)">详情</ElButton>
        <ElTooltip
          v-if="retryDisabled(row)"
          :content="retryDisabledReason(row)"
          placement="top"
        >
          <span>
            <ElButton link disabled size="small" type="primary">重试</ElButton>
          </span>
        </ElTooltip>
        <ElButton v-else link size="small" type="primary" @click="onRetry(row)">
          重试
        </ElButton>
        <ElButton
          :disabled="cancelDisabled(row)"
          link
          size="small"
          type="danger"
          @click="onCancel(row)"
        >
          取消
        </ElButton>
      </template>
    </Grid>
  </Page>
</template>
