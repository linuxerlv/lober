<script setup lang="ts">
import type { Task } from '#/api/console/task';

import { ref, watch } from 'vue';

import { ElButton, ElTag } from 'element-plus';

import { taskAPI } from '#/api/console/task';

import { taskStateLabels } from '../tasks/task-state';

const props = defineProps<{ sn: string }>();
const rows = ref<Task[]>([]);
const loading = ref(false);
const error = ref(false);
let request = 0;
async function load() {
  const current = ++request;
  rows.value = [];
  loading.value = true;
  error.value = false;
  try {
    const result = await taskAPI.list({
      host_sn: props.sn,
      page: 1,
      page_size: 5,
    });
    if (current === request) rows.value = (result.items ?? []).slice(0, 5);
  } catch {
    if (current === request) error.value = true;
  } finally {
    if (current === request) loading.value = false;
  }
}
watch(
  () => props.sn,
  (_sn, _old, cleanup) => {
    cleanup(() => {
      request++;
    });
    void load();
  },
  { immediate: true },
);
</script>

<template>
  <section
    class="rounded-lg border border-border p-4"
    aria-label="最近装机任务"
  >
    <div class="mb-2 flex flex-wrap items-center justify-between gap-2">
      <h3 class="font-semibold">最近装机任务</h3>
      <RouterLink
        class="text-sm text-primary"
        :to="{ path: '/tasks', query: { host_sn: sn } }"
        >
查看全部
</RouterLink>
    </div>
    <p v-if="loading" role="status" class="text-sm text-muted-foreground">
      正在读取装机历史…
    </p>
    <div v-else-if="error" role="alert" class="flex items-center gap-2 text-sm">
      读取装机历史失败。<ElButton link type="primary" @click="load">
重新读取
</ElButton>
    </div>
    <p v-else-if="!rows.length" class="text-sm text-muted-foreground">
      暂无装机记录。
    </p>
    <ul v-else class="divide-y divide-border">
      <li
        v-for="task in rows"
        :key="task.id"
        class="flex items-start justify-between gap-3 py-3"
      >
        <div class="min-w-0">
          <RouterLink
            class="break-all font-medium text-primary"
            :to="{
              name: 'TasksDetail',
              params: { id: task.id },
              query: { fromHost: sn },
            }"
            >
{{ task.id }}
</RouterLink>
          <p class="mt-1 break-words text-sm">
            {{ task.snapshot.iso.name }} · {{ task.snapshot.template.name }} v{{
              task.templateVersion
            }}
          </p>
          <time class="text-xs text-muted-foreground">{{
            new Date(task.createdAt * 1000).toLocaleString()
          }}</time>
        </div>
        <ElTag
          size="small"
          :type="
            task.state === 'failed'
              ? 'danger'
              : task.state === 'done'
                ? 'success'
                : 'info'
          "
          >
{{ taskStateLabels[task.state] ?? task.state }}
</ElTag>
      </li>
    </ul>
  </section>
</template>
