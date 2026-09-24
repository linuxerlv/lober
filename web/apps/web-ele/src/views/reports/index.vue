<script lang="ts" setup>
import type { AuditRecordItem } from '#/api/console/audit';
import type {
  AuditQueryParams,
  FailureReport,
  PhaseDurationReport,
  ProvisioningReport,
  TaskProvenance,
  TimeRangeParams,
} from '#/api/console/report';

/**
 * 报表页（P4-04 任务 5）。一个页面四个标签：装机统计、失败分布、审计查询、
 * 任务溯源。
 *
 * 三条来自计划的设计决定，都写在这里免得后来者以为是随手选的：
 *
 * 1. **不引入图表库，用 CSS 画条形图。** M8 删掉仪表盘时 echarts 一并出局
 *    （SPEC §12.2：`dashboard/analytics` 是唯一用 echarts 的地方）。装机量与
 *    阶段耗时都是简单形态，不值得为它们把一个图表库重新加回依赖——那意味着
 *    气隙环境下多一个要跑「三检查」的依赖（SPEC §12.4）。
 *
 * 2. **失败分布用表格而不是图表。** 它的每一行都要能点击下钻到任务列表，
 *    而图表的点击目标不如表格清晰。不可下钻的失败分布只是一个数字。
 *
 * 3. **任务溯源在这里只是一个查询入口。** 事故排查时运维是从任务详情页开始
 *    的，所以真正的主入口挂在 `tasks/detail.vue` 上；这里这一栏是给「只记得
 *    大概时间、要先在审计里找到任务 id」那条路径用的。
 *
 * 权限：整页只给 admin（与端点 30-37 一致）。
 */
import { computed, ref } from 'vue';
import { useRoute, useRouter } from 'vue-router';

import { Page } from '@vben/common-ui';

import {
  ElAlert,
  ElButton,
  ElCard,
  ElDatePicker,
  ElEmpty,
  ElInput,
  ElMessage,
  ElTable,
  ElTableColumn,
  ElTabPane,
  ElTabs,
  ElTag,
} from 'element-plus';

import {
  exportAuditCSV,
  exportProvenanceCSV,
  exportProvisioningCSV,
  getAuditApi,
  getFailureReportApi,
  getPhaseDurationReportApi,
  getProvisioningReportApi,
  getTaskProvenanceApi,
  triggerDownload,
} from '#/api/console/report';

defineOptions({ name: 'Reports' });

const route = useRoute();
const router = useRouter();
const activeTab = ref('provisioning');

/**
 * 时间范围。两端都是 Unix 秒、**闭区间**，与后端一致。
 *
 * 用 `[Date, Date] | null` 承接 el-date-picker 的 v-model，转成 Unix 秒时
 * 除以 1000 取整——后端全部时间字段都是 Unix 秒，毫秒传过去会被当成一个
 * 几万年后的时刻，过滤结果恒为空且不报错。
 */
const dateRange = ref<[Date, Date] | null>(null);

const timeRange = computed<TimeRangeParams>(() => {
  if (!dateRange.value) {
    return {};
  }
  const [start, end] = dateRange.value;
  return {
    since: Math.floor(start.getTime() / 1000),
    until: Math.floor(end.getTime() / 1000),
  };
});

const loading = ref(false);
const errorMessage = ref('');

/** 把一次取数包起来：统一的 loading 与错误提示，避免四处重复 try/catch。 */
async function run<T>(fn: () => Promise<T>): Promise<T | undefined> {
  loading.value = true;
  errorMessage.value = '';
  try {
    return await fn();
  } catch (error: any) {
    // friendlyMessage 由 response-interceptor.ts 附上（含 409 的细分文案）。
    errorMessage.value =
      error?.friendlyMessage ?? error?.message ?? '请求失败，请重试';
    return undefined;
  } finally {
    loading.value = false;
  }
}

/** 导出：取 blob 再触发下载，失败时给提示而不是静默无反应。 */
async function doExport(fetch: () => Promise<{ blob: Blob; filename: string }>) {
  const result = await run(fetch);
  if (!result) {
    // 越过 10 万行上限时后端返回 413，提示里已经写明「缩小时间范围」，
    // 直接展示后端原文——它比任何前端兜底文案都具体。
    ElMessage.error(errorMessage.value || '导出失败');
    return;
  }
  triggerDownload(result.blob, result.filename);
  ElMessage.success(`已导出 ${result.filename}`);
}

// ── 标签一：装机统计 ──────────────────────────────────────────────

const provisioning = ref<null | ProvisioningReport>(null);
const phaseDurations = ref<null | PhaseDurationReport>(null);

async function loadProvisioning() {
  const [report, phases] = await Promise.all([
    run(() => getProvisioningReportApi(timeRange.value)),
    run(() => getPhaseDurationReportApi(timeRange.value)),
  ]);
  if (report) provisioning.value = report;
  if (phases) phaseDurations.value = phases;
}

/**
 * 条形图的宽度百分比。以本组数据的最大值为 100%，不是以 total 为分母——
 * 各状态计数差异往往很大（done 几百、failed 几个），按 total 归一化会让小
 * 计数的条几乎看不见，图就白画了。
 */
function barWidth(value: number, max: number): string {
  if (max <= 0) return '0%';
  return `${Math.max(2, Math.round((value / max) * 100))}%`;
}

const provisioningMax = computed(() =>
  Math.max(1, ...(provisioning.value?.byState ?? []).map((r) => r.count)),
);

const phaseMax = computed(() =>
  Math.max(1, ...(phaseDurations.value?.byPhase ?? []).map((r) => r.p95Sec)),
);

// ── 标签二：失败分布 ──────────────────────────────────────────────

const failures = ref<FailureReport | null>(null);

async function loadFailures() {
  const report = await run(() => getFailureReportApi(timeRange.value));
  if (report) failures.value = report;
}

/**
 * 下钻：跳到任务列表并按失败原因过滤（端点 22 的 `failureReason` 参数）。
 *
 * 这是这张表的主要用法（P4-04 任务 3）——看到「precheck 失败 12 次」之后，
 * 运维要能点进去看是哪 12 个任务、是不是同一个人建的。若失败集中在某个人
 * 建的任务上那是培训问题，集中在某个机型上那是 discovery 记录的固件模式不准。
 * **没有下钻，这两种情况看起来一样。**
 */
function drillDown(reason: string) {
  router.push({ name: 'Tasks', query: { failureReason: reason } });
}

/** 占比转成百分数文案，保留一位小数。 */
function formatShare(share: number): string {
  return `${(share * 100).toFixed(1)}%`;
}

// ── 标签三：审计查询 ──────────────────────────────────────────────

const auditFilters = ref<{ action: string; actor: string; target: string }>({
  action: '',
  actor: '',
  target: '',
});
const auditRecords = ref<AuditRecordItem[]>([]);

const auditParams = computed<AuditQueryParams>(() => ({
  ...timeRange.value,
  // 空串不传：后端把空串当「不过滤该维度」，但显式传空串会让 URL 变长且
  // 在日志里难读。三项都是精确匹配，不做子串（查 bob 不会带出 bobby）。
  actor: auditFilters.value.actor || undefined,
  action: auditFilters.value.action || undefined,
  target: auditFilters.value.target || undefined,
}));

async function loadAudit() {
  const records = await run(() => getAuditApi(auditParams.value));
  if (records) auditRecords.value = records;
}

// ── 标签四：任务溯源 ──────────────────────────────────────────────

const provenanceTaskId = ref('');
const provenance = ref<null | TaskProvenance>(null);

async function loadProvenance() {
  const id = provenanceTaskId.value.trim();
  if (!id) {
    ElMessage.warning('请输入任务 ID');
    return;
  }
  const result = await run(() => getTaskProvenanceApi(id));
  provenance.value = result ?? null;
}

/** Unix 秒转本地时间文案。0 表示「没有这个时间」，显示为空而不是 1970 年。 */
function formatTS(ts: number): string {
  if (!ts) return '';
  return new Date(ts * 1000).toLocaleString();
}

/**
 * 标签切换时按需取数，避免一进页面就打四个请求。
 *
 * 参数类型是 el-tabs 的 `TabPaneName`（`string | number`）而不是 string：
 * 本页四个标签名都是字符串，但签名必须与组件声明一致，否则 vue-tsc 报
 * 「(name: string) => void 不能赋给 (name: TabPaneName) => any」。
 */
function onTabChange(name: number | string) {
  switch (String(name)) {
    case 'audit': {
      if (auditRecords.value.length === 0) loadAudit();
      break;
    }
    case 'failures': {
      if (!failures.value) loadFailures();
      break;
    }
    case 'provisioning': {
      if (!provisioning.value) loadProvisioning();
      break;
    }
    // 溯源需要先输入任务 id，不自动取数。
  }
}

/**
 * 接住任务详情页「查看溯源」带过来的 query（`?tab=provenance&taskId=...`）。
 *
 * 不接的话那个入口点过来会落在装机统计标签上，运维还要自己切标签再粘一次
 * 任务 id——等于那个"直达入口"并不直达。
 */
const initialTab = String(route.query.tab ?? '');
const initialTaskId = String(route.query.taskId ?? '');
if (initialTab) {
  activeTab.value = initialTab;
}
if (initialTaskId) {
  provenanceTaskId.value = initialTaskId;
}

if (activeTab.value === 'provenance' && initialTaskId) {
  loadProvenance();
} else {
  // 首次进入默认停在装机统计，直接取一次数。
  loadProvisioning();
}
</script>

<template>
  <Page
    description="装机统计、失败分布、审计查询与任务溯源。全部只对管理员可见。"
    title="报表与审计"
  >
    <ElAlert
      v-if="errorMessage"
      :closable="false"
      :title="errorMessage"
      class="mb-4"
      type="error"
    />

    <ElCard class="mb-4" shadow="never">
      <div class="flex flex-wrap items-center gap-3">
        <span class="text-sm">时间范围（闭区间）</span>
        <ElDatePicker
          v-model="dateRange"
          end-placeholder="结束时间"
          range-separator="至"
          start-placeholder="开始时间"
          type="datetimerange"
        />
        <ElButton
          :loading="loading"
          type="primary"
          @click="onTabChange(activeTab)"
        >
          重新取数
        </ElButton>
        <span class="text-xs text-gray-500">
          不选时间范围即统计全部历史数据（受服务端默认 1000 条上限约束）
        </span>
      </div>
    </ElCard>

    <ElTabs v-model="activeTab" @tab-change="onTabChange">
      <!-- 标签一：装机统计 -->
      <ElTabPane label="装机统计" name="provisioning">
        <ElCard shadow="never">
          <template #header>
            <div class="flex items-center justify-between">
              <span>装机量（按状态）</span>
              <div class="flex items-center gap-3">
                <span class="text-sm text-gray-500">
                  共 {{ provisioning?.total ?? 0 }} 个任务
                </span>
                <ElButton
                  :loading="loading"
                  size="small"
                  @click="doExport(() => exportProvisioningCSV(timeRange))"
                >
                  导出 CSV
                </ElButton>
              </div>
            </div>
          </template>

          <ElEmpty
            v-if="!provisioning?.byState?.length"
            description="该时间范围内没有任务"
          />
          <!-- CSS 条形图，不引图表库（见文件头注释第 1 条） -->
          <div v-else class="space-y-2">
            <div
              v-for="row in provisioning.byState"
              :key="row.state"
              class="flex items-center gap-3"
            >
              <span class="w-28 shrink-0 text-sm">{{ row.state }}</span>
              <div class="h-5 flex-1 rounded bg-gray-100 dark:bg-gray-800">
                <div
                  :style="{ width: barWidth(row.count, provisioningMax) }"
                  class="h-5 rounded bg-blue-500"
                ></div>
              </div>
              <span class="w-16 shrink-0 text-right text-sm">
                {{ row.count }}
              </span>
            </div>
          </div>
        </ElCard>

        <ElCard class="mt-4" shadow="never">
          <template #header>
            <span>阶段耗时（中位数 / P95 / 最大值，单位秒）</span>
          </template>
          <ElEmpty
            v-if="!phaseDurations?.byPhase?.length"
            description="该时间范围内没有可计算耗时的完整阶段序列"
          />
          <div v-else class="space-y-3">
            <div
              v-for="row in phaseDurations.byPhase"
              :key="row.phase"
              class="flex items-center gap-3"
            >
              <span class="w-28 shrink-0 text-sm">{{ row.phase }}</span>
              <div
                class="relative h-5 flex-1 rounded bg-gray-100 dark:bg-gray-800"
              >
                <!-- P95 在下、中位数在上：一眼看出长尾有多长 -->
                <div
                  :style="{ width: barWidth(row.p95Sec, phaseMax) }"
                  class="absolute h-5 rounded bg-amber-200"
                ></div>
                <div
                  :style="{ width: barWidth(row.medianSec, phaseMax) }"
                  class="absolute h-5 rounded bg-amber-500"
                ></div>
              </div>
              <span class="w-44 shrink-0 text-right text-xs text-gray-600">
                中位 {{ row.medianSec }} / P95 {{ row.p95Sec }} / 最大
                {{ row.maxSec }}
              </span>
              <!-- 样本数必须显示：3 个样本与 300 个样本的中位数不该看起来一样权威 -->
              <ElTag size="small" type="info">
                {{ row.sampleCount }} 个样本
              </ElTag>
            </div>
            <p class="text-xs text-gray-500">
              统计口径是中位数与高分位，不用平均值——装机耗时是长尾分布，少数
              机器因重试或慢盘远超常规，平均值会被那些异常值拉高到没有参考价值。
              中途失败、阶段序列不完整的任务被跳过而非算成 0，因此各阶段的样本
              数可能不同。
            </p>
          </div>
        </ElCard>
      </ElTabPane>

      <!-- 标签二：失败分布。用表格而不是图表，因为每一行都要能点击下钻 -->
      <ElTabPane label="失败分布" name="failures">
        <ElCard shadow="never">
          <template #header>
            <div class="flex items-center justify-between">
              <span>失败分布（按原因）</span>
              <span class="text-sm text-gray-500">
                共 {{ failures?.totalFailed ?? 0 }} 个失败任务
              </span>
            </div>
          </template>

          <ElEmpty
            v-if="!failures?.byReason?.length"
            description="该时间范围内没有失败任务"
          />
          <ElTable v-else :data="failures.byReason" stripe>
            <ElTableColumn label="失败原因" prop="reason" min-width="180" />
            <ElTableColumn label="次数" prop="count" width="90" />
            <ElTableColumn label="占比" width="100">
              <template #default="{ row }">{{ formatShare(row.share) }}</template>
            </ElTableColumn>
            <ElTableColumn label="可否重装" width="110">
              <template #default="{ row }">
                <!-- 取服务端给的 reinstallable，前端不自己维护白名单副本：
                     副本漂移的方向若是「把不可重装误标成可重装」，代价是运维
                     照着报表点重试、擦掉一台已经装好的机器（SPEC §3.4） -->
                <ElTag
                  :type="row.reinstallable ? 'success' : 'danger'"
                  size="small"
                >
                  {{ row.reinstallable ? '可重装' : '不可重装' }}
                </ElTag>
              </template>
            </ElTableColumn>
            <ElTableColumn label="操作" width="120">
              <template #default="{ row }">
                <ElButton link type="primary" @click="drillDown(row.reason)">
                  查看任务
                </ElButton>
              </template>
            </ElTableColumn>
          </ElTable>
          <p v-if="failures?.byReason?.length" class="mt-3 text-xs text-gray-500">
            占比的分母是本次统计到的失败任务总数，不是全部任务数——这张表回答
            「失败里哪一类最多」；总体失败率看装机统计标签的 failed / 总数。
            点「查看任务」下钻到该原因的任务列表：失败集中在某个人建的任务上是
            培训问题，集中在某个机型上是 discovery 记录的固件模式不准，
            没有下钻这两种情况看起来一样。
          </p>
        </ElCard>
      </ElTabPane>

      <!-- 标签三：审计查询 -->
      <ElTabPane label="审计查询" name="audit">
        <ElCard shadow="never">
          <template #header>
            <div class="flex flex-wrap items-center gap-2">
              <ElInput
                v-model="auditFilters.actor"
                class="!w-40"
                clearable
                placeholder="操作人"
              />
              <ElInput
                v-model="auditFilters.action"
                class="!w-48"
                clearable
                placeholder="动作，如 task.create"
              />
              <ElInput
                v-model="auditFilters.target"
                class="!w-48"
                clearable
                placeholder="目标资源 id"
              />
              <ElButton :loading="loading" type="primary" @click="loadAudit">
                查询
              </ElButton>
              <ElButton
                :loading="loading"
                @click="doExport(() => exportAuditCSV(auditParams))"
              >
                导出 CSV
              </ElButton>
            </div>
          </template>

          <ElEmpty
            v-if="auditRecords.length === 0"
            description="没有匹配的审计记录"
          />
          <ElTable v-else :data="auditRecords" stripe>
            <ElTableColumn label="时间" width="180">
              <template #default="{ row }">{{ formatTS(row.ts) }}</template>
            </ElTableColumn>
            <ElTableColumn label="操作人" prop="actor" width="140" />
            <ElTableColumn label="动作" prop="action" width="160" />
            <ElTableColumn label="目标" prop="target" min-width="180" />
            <ElTableColumn label="详情" prop="detail" min-width="240" />
          </ElTable>
          <p class="mt-3 text-xs text-gray-500">
            三个条件都是精确匹配、不做子串（查 bob 不会带出 bobby 的记录），
            与时间范围按「与」相交。审计记录不会被自动清理——保留期是合规要求
            而非技术决定，误删审计记录不可恢复。
          </p>
        </ElCard>
      </ElTabPane>

      <!-- 标签四：任务溯源。主入口在任务详情页，这里是「先查审计再溯源」那条路径 -->
      <ElTabPane label="任务溯源" name="provenance">
        <ElCard shadow="never">
          <template #header>
            <div class="flex flex-wrap items-center gap-2">
              <ElInput
                v-model="provenanceTaskId"
                class="!w-72"
                clearable
                placeholder="任务 ID"
                @keyup.enter="loadProvenance"
              />
              <ElButton
                :loading="loading"
                type="primary"
                @click="loadProvenance"
              >
                查询
              </ElButton>
              <ElButton
                v-if="provenance"
                :loading="loading"
                @click="
                  doExport(() => exportProvenanceCSV(provenance!.taskId))
                "
              >
                导出 CSV
              </ElButton>
              <span class="text-xs text-gray-500">
                事故排查通常从任务详情页进入，那里有直达入口
              </span>
            </div>
          </template>

          <ElEmpty v-if="!provenance" description="输入任务 ID 查询溯源信息" />
          <div v-else class="space-y-4">
            <ElCard shadow="never">
              <template #header><span>创建信息</span></template>
              <div class="grid grid-cols-2 gap-2 text-sm">
                <div>任务 ID：{{ provenance.taskId }}</div>
                <div>当前状态：{{ provenance.state }}</div>
                <div>创建人：{{ provenance.createdBy }}</div>
                <div>创建时间：{{ formatTS(provenance.createdAt) }}</div>
                <div>客户端 IP：{{ provenance.createdFromIp || '（未记录）' }}</div>
                <div>主机 SN：{{ provenance.hostSn }}</div>
                <div>主机 MAC：{{ provenance.hostMac }}</div>
                <div v-if="provenance.failureReason">
                  失败原因：{{ provenance.failureReason }}
                </div>
              </div>
            </ElCard>

            <ElCard shadow="never">
              <template #header><span>pin 的三项</span></template>
              <div class="space-y-2 text-sm">
                <div>
                  模板：
                  <template v-if="provenance.template.missing">
                    <ElTag size="small" type="warning">已删除</ElTag>
                    v{{ provenance.template.version }}
                    <span class="text-xs text-gray-500">
                      {{ provenance.template.hint }}
                    </span>
                  </template>
                  <template v-else>
                    {{ provenance.template.name }}（v{{
                      provenance.template.version
                    }}）
                  </template>
                </div>
                <div>
                  制品集：
                  <template v-if="provenance.artifactSet.missing">
                    <ElTag size="small" type="warning">已删除</ElTag>
                    <span class="text-xs text-gray-500">
                      {{ provenance.artifactSet.hint }}
                    </span>
                  </template>
                  <template v-else>
                    {{ provenance.artifactSet.name }}（{{
                      provenance.artifactSet.osFamily
                    }}
                    {{ provenance.artifactSet.osVersion }}）
                  </template>
                </div>
                <div v-if="provenance.hooks.length">
                  <div class="mb-1">hook（作者与审批人是追责链的关键一环）：</div>
                  <ElTable :data="provenance.hooks" size="small" stripe>
                    <ElTableColumn label="名称" min-width="160">
                      <template #default="{ row }">
                        <ElTag v-if="row.missing" size="small" type="warning">
                          已删除
                        </ElTag>
                        <span v-else>{{ row.name }}</span>
                      </template>
                    </ElTableColumn>
                    <ElTableColumn label="阶段" prop="stage" width="120" />
                    <ElTableColumn label="作者" prop="author" width="140" />
                    <ElTableColumn label="审批人" prop="approver" width="140" />
                    <ElTableColumn label="digest" prop="digest" min-width="220" />
                  </ElTable>
                </div>
              </div>
            </ElCard>

            <ElCard v-if="provenance.failureDetail" shadow="never">
              <template #header><span>失败详情（SPEC §3.5 六项）</span></template>
              <div class="space-y-1 text-sm">
                <div>阶段：{{ provenance.failureDetail.phase }}</div>
                <div>退出码：{{ provenance.failureDetail.exitCode }}</div>
                <div v-if="provenance.failureDetail.hookDigest">
                  失败的 hook：{{ provenance.failureDetail.hookDigest }}
                </div>
                <div>距上次信号：{{ provenance.failureDetail.sinceLastSig }}</div>
                <pre
                  v-if="provenance.failureDetail.logTail"
                  class="max-h-60 overflow-auto rounded bg-gray-50 p-2 text-xs dark:bg-gray-900"
                  >{{ provenance.failureDetail.logTail }}</pre>
              </div>
            </ElCard>

            <ElCard v-if="provenance.operations.length" shadow="never">
              <template #header><span>后续操作</span></template>
              <ElTable :data="provenance.operations" size="small" stripe>
                <ElTableColumn label="时间" width="180">
                  <template #default="{ row }">{{ formatTS(row.ts) }}</template>
                </ElTableColumn>
                <ElTableColumn label="操作人" prop="actor" width="140" />
                <ElTableColumn label="动作" prop="action" width="160" />
                <ElTableColumn label="详情" prop="detail" min-width="200" />
              </ElTable>
            </ElCard>

            <ElCard shadow="never">
              <template #header><span>事件时间线</span></template>
              <ElEmpty
                v-if="provenance.timeline.length === 0"
                description="没有事件记录"
              />
              <ElTable v-else :data="provenance.timeline" size="small" stripe>
                <ElTableColumn label="时间" width="180">
                  <template #default="{ row }">{{ formatTS(row.ts) }}</template>
                </ElTableColumn>
                <ElTableColumn label="阶段" prop="phase" width="120" />
                <ElTableColumn label="级别" prop="level" width="90" />
                <ElTableColumn label="来源" prop="source" width="110" />
                <ElTableColumn label="消息" prop="message" min-width="240" />
              </ElTable>
            </ElCard>
          </div>
        </ElCard>
      </ElTabPane>
    </ElTabs>
  </Page>
</template>
