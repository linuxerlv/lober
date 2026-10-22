<script setup lang="ts">
/* eslint-disable vue/custom-event-name-casing -- Shared pagination events match the documented public interface. */
import { ElPagination } from 'element-plus';

defineProps<{
  currentPage: number;
  pageSize: number;
  total: number;
  disabled?: boolean;
}>();
const emit = defineEmits<{
  'page-change': [page: number];
  'size-change': [size: number];
}>();
</script>

<template>
  <div class="list-pagination">
    <ElPagination
      :current-page="currentPage"
      :page-size="pageSize"
      :total="total"
      :disabled="disabled"
      :page-sizes="[10, 20, 50, 100]"
      layout="total, sizes, prev, pager, next, jumper"
      @update:current-page="emit('page-change', $event)"
      @update:page-size="emit('size-change', $event)"
    />
  </div>
</template>

<style scoped>
.list-pagination {
  display: flex;
  justify-content: flex-end;
  margin-top: 12px;
}
.list-pagination :deep(.el-pagination) {
  flex-wrap: wrap;
  gap: 8px 0;
  justify-content: flex-end;
}
</style>
