<script lang="ts" setup>
/**
 * 磁盘策略 dry-run 页（P2-05 任务 5）。
 *
 * §8.2：dryrun 与 preview 是开发期性价比最高的两个端点，把「上电等十分钟」
 * 压到秒级——本页要做成一等公民，入口显眼（一级菜单），且支持快速迭代
 * （改参数立刻重跑，输入在重跑之间保留）。
 *
 * 两种输入方式：
 *   1. 手工贴硬件事实（LOBER_DISKS 等 facts 的模拟采集结果）
 *   2. 选一个历史任务，用它的采集数据跑（"这台机器换用另一个策略会选哪块盘"）
 *
 * 输出的 reasoning 字段是服务端 DiskPlanner 的原始说明，不能省也不能改写——
 * 运维要靠它判断"选了第二块盘"对不对，而不是靠猜。
 */
import { computed, ref } from 'vue';

import { Page } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElInput,
  ElMessage,
  ElOption,
  ElRadioButton,
  ElRadioGroup,
  ElSelect,
} from 'element-plus';

import { requestClient } from '#/api/request';

defineOptions({ name: 'DiskDryrun' });

/** 与 model.DiskPolicy 对应的常见取值（契约文档未列举全集，
 * 这里给出 SPEC 提到过的几种作为下拉选项，同时允许手工输入其它取值）。 */
const DISK_POLICY_PRESETS = [
  'first',
  'largest',
  'raid1-ssd',
  'raid1-hdd',
  'raid0',
];

interface DiskPlanResult {
  chosenDisk: string;
  reasoning: string;
}

interface TaskItem {
  id: string;
  hostSn: string;
  templateId: string;
  state: string;
}

type InputMode = 'manual' | 'task';

const inputMode = ref<InputMode>('manual');

/** 手工贴事实：facts 是自由 JSON 对象，输入区内容在重跑之间保留（不清空）。 */
const manualFactsText = ref(
  JSON.stringify(
    {
      LOBER_DISKS: [
        { name: 'sda', sizeBytes: 480_103_981_056, rotational: false },
        { name: 'sdb', sizeBytes: 480_103_981_056, rotational: false },
        { name: 'sdc', sizeBytes: 1_920_383_410_176, rotational: false },
      ],
    },
    null,
    2,
  ),
);

const diskPolicy = ref('first');
const diskPolicyCustom = ref('');
const effectivePolicy = computed(
  () => diskPolicyCustom.value.trim() || diskPolicy.value,
);

/** 任务选择输入方式：从历史任务列表选一个，用它的 hostSn 关联的采集数据跑。
 * 契约的 POST /disk-policy/dryrun 入参是 { diskPolicy, facts }，不直接接受
 * taskId——这里选中任务后仍然需要手工贴 facts（历史采集数据的展示位置），
 * 因为契约没有开放"按任务 id 取历史 facts"的端点，选任务只是帮助定位
 * "这台机器当时报的硬件是什么样"这个上下文，不是免填 facts 的捷径。 */
const taskOptions = ref<TaskItem[]>([]);
const selectedTaskId = ref('');
const taskListLoading = ref(false);

async function loadRecentTasks() {
  taskListLoading.value = true;
  try {
    taskOptions.value = await requestClient.get<TaskItem[]>('/tasks');
  } finally {
    taskListLoading.value = false;
  }
}

function switchToTaskMode() {
  inputMode.value = 'task';
  if (taskOptions.value.length === 0) {
    loadRecentTasks();
  }
}

/** 对比模式（P2-02 扩展）：当前策略与请求策略结果并排。
 * 契约的 dryrun 端点每次只接受一个 diskPolicy，"对比"在前端实现为
 * 连续两次调用、结果并排展示，不依赖服务端提供批量对比能力。 */
const compareMode = ref(false);
const comparePolicy = ref('largest');
const comparePolicyCustom = ref('');
const effectiveComparePolicy = computed(
  () => comparePolicyCustom.value.trim() || comparePolicy.value,
);

const primaryResult = ref<DiskPlanResult | null>(null);
const compareResult = ref<DiskPlanResult | null>(null);
const running = ref(false);
const lastError = ref('');

function parseFacts(): null | Record<string, any> {
  try {
    return JSON.parse(manualFactsText.value);
  } catch {
    ElMessage.error('facts 不是合法 JSON，请检查格式');
    return null;
  }
}

async function runDryrun() {
  const facts = parseFacts();
  if (!facts) return;
  running.value = true;
  lastError.value = '';
  try {
    primaryResult.value = await requestClient.post<DiskPlanResult>(
      '/disk-policy/dryrun',
      { diskPolicy: effectivePolicy.value, facts },
    );
    compareResult.value = compareMode.value
      ? await requestClient.post<DiskPlanResult>('/disk-policy/dryrun', {
          diskPolicy: effectiveComparePolicy.value,
          facts,
        })
      : null;
  } catch (error: any) {
    lastError.value =
      error?.response?.data?.message || error?.message || 'dry-run 调用失败';
  } finally {
    running.value = false;
  }
}
</script>

<template>
  <Page
    description="纯计算、不落地——贴硬件事实或选一个历史任务，秒级看策略会选哪块盘，以及为什么这么选。"
    title="磁盘策略 dry-run"
  >
    <ElAlert
      :closable="false"
      class="mb-3"
      title="dry-run 的结果不代表实际装机会这么选"
      type="warning"
    >
      实际装机用的是机器当时上报的硬件事实，而这里用的是历史数据或手工输入。
      硬盘更换、插槽变化都会让结果不同——这里只是调试辅助，不是保证。
    </ElAlert>

    <ElCard class="mb-3">
      <template #header>输入</template>

      <ElRadioGroup v-model="inputMode" class="mb-3">
        <ElRadioButton value="manual">手工贴硬件事实</ElRadioButton>
        <ElRadioButton value="task" @click="switchToTaskMode">
          参考历史任务
        </ElRadioButton>
      </ElRadioGroup>

      <div v-if="inputMode === 'task'" class="mb-3">
        <ElSelect
          v-model="selectedTaskId"
          :loading="taskListLoading"
          class="w-full"
          filterable
          placeholder="选一个历史任务作为上下文参考（仍需在下方填写 facts）"
        >
          <ElOption
            v-for="task in taskOptions"
            :key="task.id"
            :label="`${task.hostSn}（${task.state}）— ${task.id}`"
            :value="task.id"
          />
        </ElSelect>
        <div class="mt-1 text-sm text-gray-500">
          契约未提供"按任务取历史采集事实"的端点，选中任务仅用于定位上下文；
          仍需在下方手工填入该机器当时的 facts。
        </div>
      </div>

      <label class="mb-1 block text-sm">硬件事实（facts，JSON）</label>
      <ElInput
        v-model="manualFactsText"
        :rows="8"
        class="mb-3 font-mono"
        type="textarea"
      />

      <div class="mb-3 flex items-center gap-3">
        <div>
          <label class="mb-1 block text-sm">磁盘策略</label>
          <ElSelect v-model="diskPolicy" class="w-48">
            <ElOption
              v-for="opt in DISK_POLICY_PRESETS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div>
          <label class="mb-1 block text-sm">或手工输入策略值</label>
          <ElInput
            v-model="diskPolicyCustom"
            class="w-48"
            placeholder="覆盖上面的下拉选择"
          />
        </div>
      </div>

      <div class="mb-3">
        <label class="flex items-center gap-2 text-sm">
          <input v-model="compareMode" type="checkbox" />
          对比另一个策略（并排展示两个结果）
        </label>
      </div>
      <div v-if="compareMode" class="mb-3 flex items-center gap-3">
        <div>
          <label class="mb-1 block text-sm">对比策略</label>
          <ElSelect v-model="comparePolicy" class="w-48">
            <ElOption
              v-for="opt in DISK_POLICY_PRESETS"
              :key="opt"
              :label="opt"
              :value="opt"
            />
          </ElSelect>
        </div>
        <div>
          <label class="mb-1 block text-sm">或手工输入</label>
          <ElInput v-model="comparePolicyCustom" class="w-48" />
        </div>
      </div>

      <ElButton :loading="running" type="primary" @click="runDryrun">
        运行 dry-run
      </ElButton>
      <span class="ml-2 text-sm text-gray-500">
        输入内容在重跑之间保留，改参数后可直接再次点击。
      </span>
    </ElCard>

    <ElAlert v-if="lastError" class="mb-3" :title="lastError" type="error" />

    <div :class="compareMode ? 'grid grid-cols-2 gap-3' : ''">
      <ElCard v-if="primaryResult">
        <template #header>结果：{{ effectivePolicy }}</template>
        <div class="mb-2">
          <span class="font-bold">选中的主盘：</span>
          {{ primaryResult.chosenDisk }}
        </div>
        <div>
          <div class="mb-1 font-bold">为什么这么选（服务端原样返回）</div>
          <pre class="rounded bg-gray-100 p-3 text-sm whitespace-pre-wrap">{{
            primaryResult.reasoning
          }}</pre>
        </div>
      </ElCard>

      <ElCard v-if="compareMode && compareResult">
        <template #header>结果：{{ effectiveComparePolicy }}</template>
        <div class="mb-2">
          <span class="font-bold">选中的主盘：</span>
          {{ compareResult.chosenDisk }}
        </div>
        <div>
          <div class="mb-1 font-bold">为什么这么选（服务端原样返回）</div>
          <pre class="rounded bg-gray-100 p-3 text-sm whitespace-pre-wrap">{{
            compareResult.reasoning
          }}</pre>
        </div>
      </ElCard>
    </div>
  </Page>
</template>
