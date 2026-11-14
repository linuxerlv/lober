<script setup lang="ts">
import { ElTag } from 'element-plus';

defineProps<{
  groups: {
    name: string;
    actions: { code: string; label: string; allowed: boolean }[];
  }[];
}>();
</script>

<template>
  <div class="grid gap-4 md:grid-cols-2 xl:grid-cols-3">
    <section
      v-for="group in groups"
      :key="group.name"
      class="rounded-lg border border-border"
    >
      <h2 class="border-b border-border bg-muted px-4 py-3 font-semibold">
        {{ group.name }}
      </h2>
      <ul class="divide-y divide-border px-4">
        <li
          v-for="action in group.actions"
          :key="action.code"
          class="flex items-center justify-between gap-3 py-2.5"
        >
          <span class="break-all">{{ action.label }}</span>
          <ElTag size="small" :type="action.allowed ? 'success' : 'info'">{{
            action.allowed ? '允许' : '不允许'
          }}</ElTag>
        </li>
      </ul>
    </section>
  </div>
</template>
