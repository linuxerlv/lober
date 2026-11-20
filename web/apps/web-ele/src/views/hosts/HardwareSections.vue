<script setup lang="ts">
import type { DiscoverySection } from '#/api/console/discovery';

import { computed } from 'vue';

const props = defineProps<{ sections: DiscoverySection[] }>();
const statuses = { absent: '未安装', partial: '部分读取', read: '已读取', unread: '未读取' };
const legacy = computed(() => !props.sections.some((section) =>
  section.key === 'system' && section.items.some((item) => item.fields['采集格式'] === '2'),
));
const missing = '未读取·';
</script>

<template>
  <div class="grid gap-2">
    <p v-if="legacy" class="text-sm text-muted-foreground">
      这份报告使用旧采集格式。旧镜像没有记录的字段和缺失原因，需要使用新版镜像重新采集。
    </p>
    <details v-for="section in sections" :key="section.key" class="rounded border border-border p-3" open>
      <summary class="cursor-pointer font-medium">{{ section.title }} · {{ statuses[section.status] }}</summary>
      <p v-if="section.reason" class="mt-2 text-sm text-muted-foreground">{{ section.reason }}</p>
      <div v-for="(item, index) in section.items" :key="index" class="mt-3 border-t border-border pt-2 text-sm">
        <h4 class="mb-1 font-medium">{{ item.name }}</h4>
        <dl class="grid gap-1">
          <template v-for="(value, key) in item.fields" :key="key">
            <div v-if="key !== '采集格式'" class="grid grid-cols-[1fr_2fr] gap-3">
              <dt class="break-all text-muted-foreground">{{ key.startsWith(missing) ? key.slice(missing.length) : key }}</dt>
              <dd class="break-words">{{ key.startsWith(missing) ? `未读取（${value}）` : value || '报告未提供值' }}</dd>
            </div>
          </template>
        </dl>
      </div>
    </details>
  </div>
</template>
