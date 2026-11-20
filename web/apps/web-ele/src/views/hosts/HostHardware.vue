<script setup lang="ts">
import type { DiscoverySection } from '#/api/console/discovery';

import { ref, watch } from 'vue';

import { ElButton } from 'element-plus';

import { getHostHardwareApi } from '#/api/console/discovery';
import { errorMessage } from '#/api/console/problem';

import HardwareSections from './HardwareSections.vue';

const props = defineProps<{ sn: string }>();
const sections = ref<DiscoverySection[]>();
const time = ref('');
const loading = ref(true);
const error = ref('');
const retry = ref(0);

watch([() => props.sn, retry], async ([sn], _, cleanup) => {
  let active = true;
  cleanup(() => { active = false; });
  loading.value = true;
  error.value = '';
  sections.value = undefined;
  time.value = '';
  try {
    const result = await getHostHardwareApi(sn);
    if (active) {
      sections.value = result.report?.sections;
      if (result.report_at) time.value = new Date(result.report_at * 1000).toLocaleString();
    }
  } catch (reason) {
    if (active) error.value = errorMessage(reason);
  } finally {
    if (active) loading.value = false;
  }
}, { immediate: true });
</script>

<template>
  <section class="rounded-lg border border-border p-4" aria-label="硬件部件清单">
    <h3 class="mb-2 font-semibold">硬件部件清单</h3>
    <p v-if="loading" role="status">正在读取部件报告…</p>
    <p v-else-if="error" role="alert">{{ error }} <ElButton link @click="retry++">重新读取</ElButton></p>
    <template v-else-if="sections">
      <p v-if="time" class="mb-2 text-sm text-muted-foreground">最近部件报告 · {{ time }}</p>
      <HardwareSections :sections="sections" />
    </template>
    <p v-else class="text-sm text-muted-foreground">暂无详细部件报告。使用新版镜像完成硬件采集后，这里会显示结果。</p>
  </section>
</template>
