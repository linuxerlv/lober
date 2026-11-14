<script setup lang="ts">
/* eslint-disable vue/custom-event-name-casing -- Shared pagination events match the documented public interface. */
import { ElPagination } from 'element-plus';

defineProps<{
  currentPage: number;
  pageSize: number;
  total: number;
  disabled?: boolean;
  pageCount?: number;
  selectedCount?: number;
}>();
const emit = defineEmits<{
  'page-change': [page: number];
  'size-change': [size: number];
}>();
</script>

<template>
  <div
    class="list-pagination"
    :class="{ 'with-summary': pageCount !== undefined }"
  >
    <p
      v-if="pageCount !== undefined"
      class="text-sm text-muted-foreground"
      aria-live="polite"
    >
      本页 {{ pageCount }} 条 · 共 {{ total }} 条<span
        v-if="selectedCount !== undefined"
      >
        · 已选 {{ selectedCount }} 条</span>
    </p>
    <ElPagination
      :current-page="currentPage"
      :page-size="pageSize"
      :total="total"
      :disabled="disabled"
      :page-sizes="[10, 20, 50, 100]"
      :layout="
        pageCount === undefined
          ? 'total, sizes, prev, pager, next, jumper'
          : 'sizes, prev, pager, next, jumper'
      "
      @update:current-page="emit('page-change', $event)"
      @update:page-size="emit('size-change', $event)"
    />
  </div>
</template>

<style scoped>
.list-pagination {
  display: flex;
  flex-wrap: wrap;
  align-items: center;
  gap: 8px 16px;
  justify-content: flex-end;
  margin-top: 12px;
}
.list-pagination.with-summary {
  justify-content: space-between;
}
.with-summary :deep(.el-pagination) {
  margin-inline-start: auto;
}
.list-pagination :deep(.el-pagination) {
  flex-wrap: wrap;
  gap: 8px 0;
  justify-content: flex-end;
}
</style>
