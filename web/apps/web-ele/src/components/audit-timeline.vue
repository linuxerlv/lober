<script setup lang="ts">
import type { AuditRecordItem } from '#/api/console/audit';

import { ref, watch } from 'vue';

import { useAccessStore } from '@vben/stores';

import {
  ElAlert,
  ElButton,
  ElEmpty,
  ElTimeline,
  ElTimelineItem,
} from 'element-plus';

import { listAudit } from '#/api/console/audit';
import { errorMessage } from '#/api/console/problem';

const props = defineProps<{ hookId: string }>();
const access = useAccessStore();
const records = ref<AuditRecordItem[]>([]);
const next = ref('');
const failure = ref<unknown>();
const loading = ref(false);
let generation = 0;
async function load(cursor = '') {
  if (!access.accessCodes.includes('audit:read')) return;
  const run = ++generation;
  loading.value = true;
  failure.value = undefined;
  try {
    const page = await listAudit({
      target_kind: 'hook',
      target_id: props.hookId,
      cursor,
      limit: 50,
    });
    if (run !== generation) return;
    records.value = cursor
      ? [...records.value, ...(page.items ?? [])]
      : (page.items ?? []);
    next.value = page.next;
  } catch (error) {
    if (run === generation) failure.value = error;
  } finally {
    if (run === generation) loading.value = false;
  }
}
function reload() {
  records.value = [];
  next.value = '';
  void load();
}
watch(() => props.hookId, reload, { immediate: true });
defineExpose({ reload });
</script>
<template>
  <div v-if="access.accessCodes.includes('audit:read')" v-loading="loading">
    <ElAlert
      v-if="failure"
      :title="errorMessage(failure)"
      type="error"
      :closable="false"
    />
    <ElEmpty v-else-if="!records.length" description="暂无审计记录" />
    <ElTimeline v-else>
<ElTimelineItem
        v-for="record in records"
        :key="record.id"
        :timestamp="new Date(record.at * 1000).toLocaleString()"
        >
<b>{{ record.actor }} · {{ record.action }}</b>
        <p class="whitespace-pre-wrap">{{ record.detail }}</p>
</ElTimelineItem>
</ElTimeline>
    <ElButton v-if="next" @click="load(next)">加载更多</ElButton>
  </div>
</template>
