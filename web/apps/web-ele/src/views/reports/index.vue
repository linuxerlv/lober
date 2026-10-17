<script setup lang="ts">
import { ElAlert } from 'element-plus';

import AuditPage from '#/views/audit/index.vue';
</script>
<template>
  <div class="p-6">
    <ElAlert title="当前提供审计查询和导出；装机统计报表尚未提供。" type="info" :closable="false" />
    <AuditPage />
  </div>
</template>
