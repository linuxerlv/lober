<script setup lang="ts">
import type { EchartsUIType } from '@vben/plugins/echarts';

import { computed, onMounted, ref, watch } from 'vue';
import { useRouter } from 'vue-router';

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

defineOptions({ name: 'OverviewAnalytics' });

const router = useRouter();

const taskCount = ref(6);
const hostCount = ref(8);
const isoCount = ref(5);
const componentCount = ref(8);

const trendTab = ref<'monthly' | 'trends'>('trends');

const trendChartRef = ref<EchartsUIType>();
const radarChartRef = ref<EchartsUIType>();
const donutChartRef = ref<EchartsUIType>();
const roseChartRef = ref<EchartsUIType>();

const { renderEcharts: renderTrend } = useEcharts(trendChartRef);
const { renderEcharts: renderRadar } = useEcharts(radarChartRef);
const { renderEcharts: renderDonut } = useEcharts(donutChartRef);
const { renderEcharts: renderRose } = useEcharts(roseChartRef);

const overviewCards = computed(() => [
  {
    title: '装机任务量',
    value: taskCount.value.toLocaleString(),
    totalTitle: '累计完成装机',
    totalValue: '1,280',
    icon: SvgCardIcon,
    path: '/tasks',
  },
  {
    title: '纳管主机量',
    value: hostCount.value.toLocaleString(),
    totalTitle: '机房总物理机',
    totalValue: '320',
    icon: SvgCakeIcon,
    path: '/hosts',
  },
  {
    title: '系统镜像库',
    value: isoCount.value.toLocaleString(),
    totalTitle: '累计网络引导次数',
    totalValue: '4,620',
    icon: SvgDownloadIcon,
    path: '/artifact-sets',
  },
  {
    title: '模板与脚本',
    value: componentCount.value.toLocaleString(),
    totalTitle: '累计自动化执行',
    totalValue: '9,850',
    icon: SvgBellIcon,
    path: '/templates',
  },
]);

function drawTrendChart() {
  if (trendTab.value === 'trends') {
    void renderTrend({
      grid: {
        bottom: 0,
        containLabel: true,
        left: '1%',
        right: '1%',
        top: '4%',
      },
      tooltip: {
        axisPointer: {
          lineStyle: {
            color: '#019680',
            width: 1,
          },
        },
        trigger: 'axis',
      },
      xAxis: {
        axisTick: { show: false },
        boundaryGap: false,
        data: Array.from({ length: 18 }, (_, i) => `${i + 6}:00`),
        splitLine: {
          lineStyle: { type: 'solid', width: 1 },
          show: true,
        },
        type: 'category',
      },
      yAxis: [
        {
          axisTick: { show: false },
          max: 80,
          splitArea: { show: true },
          splitNumber: 4,
          type: 'value',
        },
      ],
      series: [
        {
          areaStyle: {},
          data: [
            1, 4, 12, 26, 42, 61, 68, 41, 22, 39, 72, 48, 29, 18, 12, 6, 3, 1,
          ],
          itemStyle: { color: '#5ab1ef' },
          name: '并发网络引导流量 (Gbps)',
          smooth: true,
          type: 'line',
        },
        {
          areaStyle: {},
          data: [
            0, 1, 3, 7, 12, 18, 24, 14, 6, 15, 25, 19, 11, 6, 4, 2, 1, 0,
          ],
          itemStyle: { color: '#019680' },
          name: '当小时装机并发数 (台)',
          smooth: true,
          type: 'line',
        },
      ],
    });
  } else {
    void renderTrend({
      grid: {
        bottom: 0,
        containLabel: true,
        left: '1%',
        right: '1%',
        top: '4%',
      },
      tooltip: {
        axisPointer: {
          lineStyle: {
            color: '#4f69fd',
            width: 1,
          },
        },
        trigger: 'axis',
      },
      xAxis: {
        data: Array.from({ length: 12 }, (_, i) => `${i + 1}月`),
        type: 'category',
      },
      yAxis: {
        max: 200,
        splitNumber: 4,
        type: 'value',
      },
      series: [
        {
          barMaxWidth: 48,
          data: [68, 52, 95, 124, 140, 118, 156, 172, 148, 165, 184, 136],
          itemStyle: {
            borderRadius: [4, 4, 0, 0],
            color: '#5ab1ef',
          },
          name: '月度交付物理机 (台)',
          type: 'bar',
        },
      ],
    });
  }
}

function drawBottomCharts() {
  void renderRadar({
    legend: {
      bottom: 0,
      data: ['已交付节点', '规划容量'],
    },
    radar: {
      indicator: [
        { name: '生产计算', max: 100 },
        { name: '容器集群', max: 100 },
        { name: '虚拟化', max: 100 },
        { name: '分布式存储', max: 100 },
        { name: 'ARM 算力', max: 100 },
        { name: '边缘节点', max: 100 },
      ],
      radius: '60%',
      splitNumber: 5,
    },
    series: [
      {
        areaStyle: {
          opacity: 0.7,
          shadowBlur: 0,
          shadowColor: 'rgba(0,0,0,.2)',
          shadowOffsetX: 0,
          shadowOffsetY: 10,
        },
        data: [
          {
            itemStyle: { color: '#b6a2de' },
            name: '已交付节点',
            value: [88, 76, 92, 68, 74, 62],
          },
          {
            itemStyle: { color: '#5ab1ef' },
            name: '规划容量',
            value: [95, 90, 95, 85, 88, 80],
          },
        ],
        symbolSize: 0,
        type: 'radar',
      },
    ],
    tooltip: {},
  });

  void renderDonut({
    legend: {
      bottom: '2%',
      left: 'center',
    },
    series: [
      {
        animationDelay() {
          return Math.random() * 100;
        },
        avoidLabelOverlap: false,
        color: ['#5ab1ef', '#b6a2de', '#67e0e3', '#2ec7c9', '#ffb980'],
        data: [
          { name: 'Rocky Linux 9', value: 540 },
          { name: 'Ubuntu 24.04', value: 360 },
          { name: 'VMware ESXi 8', value: 210 },
          { name: 'Debian 12 ARM', value: 110 },
          { name: 'Windows 2022', value: 60 },
        ],
        emphasis: {
          label: {
            fontSize: '12',
            fontWeight: 'bold',
            show: true,
          },
        },
        itemStyle: {
          borderRadius: 10,
          borderWidth: 2,
        },
        label: {
          position: 'center',
          show: false,
        },
        labelLine: {
          show: false,
        },
        name: '系统镜像占比',
        radius: ['40%', '65%'],
        type: 'pie',
      },
    ],
    tooltip: {
      trigger: 'item',
    },
  });

  void renderRose({
    series: [
      {
        animationDelay() {
          return Math.random() * 400;
        },
        animationType: 'scale',
        center: ['50%', '50%'],
        color: ['#5ab1ef', '#2ec7c9', '#67e0e3', '#b6a2de', '#f87171'],
        data: [
          { name: '安装完成', value: 480 },
          { name: '正在安装', value: 310 },
          { name: '初始化脚本', value: 240 },
          { name: '等待引导', value: 185 },
          { name: '异常排障', value: 95 },
        ].sort((a, b) => a.value - b.value),
        name: '装机阶段分布',
        radius: '75%',
        roseType: 'radius',
        type: 'pie',
      },
    ],
    tooltip: {
      trigger: 'item',
    },
  });
}

watch(trendTab, () => {
  drawTrendChart();
});

onMounted(async () => {
  drawTrendChart();
  drawBottomCharts();

  const [tasksRes, hostsRes, isosRes, tplRes, hooksRes] =
    await Promise.allSettled([
      taskAPI.list({ page: 1, page_size: 20 }),
      getHostListApi({ page: 1, page_size: 20 }),
      getArtifactSetListApi({ page: 1, page_size: 20 }),
      getTemplateListApi({ page: 1, page_size: 20 }),
      getHookListApi({ page: 1, page_size: 20 }),
    ]);

  if (tasksRes.status === 'fulfilled')
    taskCount.value = tasksRes.value.total ?? 0;
  if (hostsRes.status === 'fulfilled')
    hostCount.value = hostsRes.value.total ?? 0;
  if (isosRes.status === 'fulfilled')
    isoCount.value = isosRes.value.total ?? 0;
  if (tplRes.status === 'fulfilled' && hooksRes.status === 'fulfilled') {
    componentCount.value =
      (tplRes.value.total ?? 0) + (hooksRes.value.total ?? 0);
  }
});
</script>

<template>
  <div class="p-5">
    <!-- 第一行：4 张 Vben 原生风格核心指标卡 -->
    <div class="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-4">
      <div
        v-for="item in overviewCards"
        :key="item.title"
        class="cursor-pointer rounded-xl border border-border bg-card p-5 transition hover:border-primary/50 hover:shadow-md"
        @click="router.push(item.path)"
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
      </div>
    </div>

    <!-- 第二行：装机趋势 / 月装机量 大图表卡片 -->
    <div class="mt-4 rounded-xl border border-border bg-card p-5">
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
          装机趋势
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
          月装机量
        </button>
      </div>
      <EchartsUI ref="trendChartRef" height="320px" />
    </div>

    <!-- 第三行：底部三联图表卡片（雷达图 / 环形图 / 玫瑰图） -->
    <div class="mt-4 grid grid-cols-1 gap-4 lg:grid-cols-3">
      <div class="rounded-xl border border-border bg-card p-5">
        <h3 class="mb-3 text-base font-semibold text-foreground">
          集群装机容量
        </h3>
        <EchartsUI ref="radarChartRef" height="280px" />
      </div>
      <div class="rounded-xl border border-border bg-card p-5">
        <h3 class="mb-3 text-base font-semibold text-foreground">
          系统镜像占比
        </h3>
        <EchartsUI ref="donutChartRef" height="280px" />
      </div>
      <div class="rounded-xl border border-border bg-card p-5">
        <h3 class="mb-3 text-base font-semibold text-foreground">
          装机阶段分布
        </h3>
        <EchartsUI ref="roseChartRef" height="280px" />
      </div>
    </div>
  </div>
</template>
