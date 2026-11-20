<script setup lang="ts">
import type { EchartsUIType } from '@vben/plugins/echarts';

import { computed, onMounted, ref, watch } from 'vue';

import {
  SvgBellIcon,
  SvgCakeIcon,
  SvgCardIcon,
  SvgDownloadIcon,
} from '@vben/icons';
import { EchartsUI, useEcharts } from '@vben/plugins/echarts';

import { getArtifactSetListApi } from '#/api/console/artifact';
import { getHookListApi } from '#/api/console/hook';
import { getHostListApi } from '#/api/console/host';
import { taskAPI } from '#/api/console/task';
import { getTemplateListApi } from '#/api/console/template';
import { useConsoleAccess } from '#/composables/use-console-access';
import { taskCount, taskStateLabels } from '#/views/tasks/task-state';

import { useOverviewData } from './use-overview-data';

defineOptions({ name: 'OverviewAnalytics' });

const { can, hasMenu } = useConsoleAccess();
const page = { page: 1, page_size: 1 };
const stats = useOverviewData(() => can('task:read'), () => taskAPI.statistics({ trends: true }));
const hostCount = useOverviewData(() => can('host:read'), async () => (await getHostListApi(page)).total);
const isoCount = useOverviewData(() => can('iso:read'), async () => (await getArtifactSetListApi(page)).total);
const readyCount = useOverviewData(() => can('iso:read'), async () => (await getArtifactSetListApi({ ...page, state: 'ready' })).total);
const templateCount = useOverviewData(() => can('template:read'), async () => (await getTemplateListApi(page)).total);
const hookCount = useOverviewData(() => can('hook:read'), async () => (await getHookListApi(page)).total);
const componentCount = computed(() => {
  const counts = [];
  if (can('template:read')) counts.push(templateCount.value);
  if (can('hook:read')) counts.push(hookCount.value);
  return counts.every((n): n is number => typeof n === 'number')
    ? counts.reduce((sum, n) => sum + n, 0)
    : undefined;
});

const trendTab = ref<'monthly' | 'trends'>('trends');

const trendChartRef = ref<EchartsUIType>();

const donutChartRef = ref<EchartsUIType>();
const roseChartRef = ref<EchartsUIType>();

const { renderEcharts: renderTrend } = useEcharts(trendChartRef);

const { renderEcharts: renderDonut } = useEcharts(donutChartRef);
const { renderEcharts: renderRose } = useEcharts(roseChartRef);

const overviewCards = computed(() => [
  {
    title: '装机任务量',
    visible: can('task:read'),
    value: taskCount(stats.value?.states)?.toLocaleString() ?? '—',
    totalTitle: '累计完成装机',
    totalValue: taskCount(stats.value?.states, 'done') ?? '—',
    icon: SvgCardIcon,
    path: hasMenu('tasks') ? '/tasks' : undefined,
  },
  {
    title: '纳管主机量',
    visible: can('host:read'),
    value: hostCount.value?.toLocaleString() ?? '—',
    totalTitle: '统计范围',
    totalValue: '已登记主机',
    icon: SvgCakeIcon,
    path: hasMenu('hosts') ? '/hosts' : undefined,
  },
  {
    title: '系统镜像库',
    visible: can('iso:read'),
    value: isoCount.value?.toLocaleString() ?? '—',
    totalTitle: '可用镜像',
    totalValue: readyCount.value ?? '—',
    icon: SvgDownloadIcon,
    path: hasMenu('isos') ? '/artifact-sets' : undefined,
  },
  {
    title: '模板与脚本',
    visible: can('template:read') || can('hook:read'),
    value: componentCount.value?.toLocaleString() ?? '—',
    totalTitle: can('hook:read') ? '脚本' : '模板',
    totalValue: (can('hook:read') ? hookCount.value : templateCount.value) ?? '—',
    icon: SvgBellIcon,
    path: hasMenu('templates') ? '/templates' : hasMenu('hooks') ? '/hooks' : undefined,
  },
].filter((item) => item.visible));

function drawCharts() {
  const data = stats.value;
  if (!data) return;
  const monthly = trendTab.value === 'monthly';
  const today = new Date();
  const keys = Array.from({ length: monthly ? 12 : 14 }, (_, index) => {
    const d = new Date(today);
    if (monthly) {
      d.setUTCDate(1);
      d.setUTCMonth(d.getUTCMonth() - 11 + index);
    } else d.setUTCDate(d.getUTCDate() - 13 + index);
    return d.toISOString().slice(0, monthly ? 7 : 10);
  });
  const counts = monthly ? data.months : data.days;
  void renderTrend({
    tooltip: { trigger: 'axis' },
    xAxis: { type: 'category', data: keys },
    yAxis: { type: 'value', minInterval: 1 },
    series: [{ name: '新建任务', type: monthly ? 'bar' : 'line', data: keys.map((key) => counts?.[key] ?? 0) }],
  });
  if (!taskCount(data.states)) return;
  void renderDonut({
    tooltip: { trigger: 'item' },
    series: [{ type: 'pie', radius: ['35%', '65%'], data: Object.entries(data.images ?? {}).map(([name, value]) => ({ name, value })) }],
  });
  void renderRose({
    tooltip: { trigger: 'item' },
    series: [{ type: 'pie', radius: '65%', data: Object.entries(taskStateLabels).map(([state, name]) => ({ name, value: data.states?.[state] ?? 0 })).filter((row) => row.value > 0) }],
  });
}

watch([stats, trendTab], drawCharts, { flush: 'post' });
onMounted(drawCharts);
</script>

<template>
  <div class="p-5">
    <!-- 第一行：4 张 Vben 原生风格核心指标卡 -->
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
      <component
        :is="item.path ? 'RouterLink' : 'div'"
        v-for="item in overviewCards"
        :key="item.title"
        :to="item.path"
        class="rounded-xl border border-border bg-card p-5"
        :class="{ 'transition hover:border-primary/50 hover:shadow-md': item.path }"
      >
        <div class="flex items-center justify-between">
          <h3 class="text-base font-semibold text-foreground">
            {{ item.title }}
          </h3>
        </div>
        <div class="mt-5 flex items-center justify-between">
          <span class="text-2xl font-semibold tabular-nums text-foreground">
            {{ item.value }}
          </span>
          <component :is="item.icon" class="size-8 shrink-0" />
        </div>
        <div
          class="mt-5 flex items-center justify-between text-xs text-muted-foreground"
        >
          <span>{{ item.totalTitle }}</span>
          <span class="tabular-nums">{{ item.totalValue }}</span>
        </div>
      </component>
    </div>

    <!-- 第二行：装机趋势 / 月装机量 大图表卡片 -->
    <div v-if="can('task:read')" class="mt-4 rounded-xl border border-border bg-card p-5">
      <div class="mb-4 inline-flex rounded-lg bg-muted p-1 text-xs">
        <button
          type="button"
          class="rounded-md px-3 py-1.5 font-medium transition"
          :class="
            trendTab === 'trends'
              ? 'bg-card text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          "
          @click="trendTab = 'trends'"
        >
          近 14 天新建任务
        </button>
        <button
          type="button"
          class="rounded-md px-3 py-1.5 font-medium transition"
          :class="
            trendTab === 'monthly'
              ? 'bg-card text-foreground shadow-sm'
              : 'text-muted-foreground hover:text-foreground'
          "
          @click="trendTab = 'monthly'"
        >
          近 12 个月新建任务
        </button>
      </div>
      <p class="text-xs text-muted-foreground">按世界标准时间统计。网络引导流量尚未采集。</p>
      <p v-if="!stats" class="py-5">暂无统计数据</p>
      <EchartsUI v-if="stats" ref="trendChartRef" height="320px" />
    </div>

    <!-- 第三行：底部三联图表卡片（雷达图 / 环形图 / 玫瑰图） -->
    <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
      <div class="rounded-xl border border-border bg-card p-5">
        <h3 class="mb-3 text-base font-semibold text-foreground">
          集群装机容量
        </h3>
        <p class="text-sm text-muted-foreground">尚未采集机房的规划容量。</p>
      </div>
      <div v-if="can('task:read')" class="rounded-xl border border-border bg-card p-5">
        <h3 class="mb-3 text-base font-semibold text-foreground">
          装机任务使用的镜像
        </h3>
        <p v-if="!taskCount(stats?.states)" class="text-sm text-muted-foreground">暂无数据</p>
        <EchartsUI v-if="stats && taskCount(stats.states)" ref="donutChartRef" height="280px" />
      </div>
      <div v-if="can('task:read')" class="rounded-xl border border-border bg-card p-5">
        <h3 class="mb-3 text-base font-semibold text-foreground">
          装机阶段分布
        </h3>
        <p v-if="!taskCount(stats?.states)" class="text-sm text-muted-foreground">暂无数据</p>
        <EchartsUI v-if="stats && taskCount(stats.states)" ref="roseChartRef" height="280px" />
      </div>
    </div>
  </div>
</template>
