<script setup lang="ts">
import { useRouter } from 'vue-router';

import { ElAlert, ElButton, ElCard } from 'element-plus';

import { useConsoleAccess } from '#/composables/use-console-access';
const router = useRouter();
  const { hasMenu } = useConsoleAccess();
</script>
<template>
  <main class="p-6">
    <h1 class="mb-5 text-2xl font-semibold">工具</h1>
    <ElCard>
<ElAlert
        title="当前服务尚未提供独立磁盘策略试算。可以在模板页面使用版本预览查看生成配置与诊断。"
        type="info"
        :closable="false"
      /><ElButton
        v-if="hasMenu('templates')"
        class="mt-4"
        @click="router.push('/templates')"
        >
打开模板预览
</ElButton><ElButton class="mt-4" disabled>磁盘策略试算暂不可用</ElButton>
</ElCard>
  </main>
</template>
