<script lang="ts" setup>
import { useConsoleSessionRefresh } from '#/composables/use-console-access';
useConsoleSessionRefresh();
import { useElementPlusDesignTokens } from '@vben/hooks';

import { ElConfigProvider } from 'element-plus';

import { elementLocale } from '#/locales';

defineOptions({ name: 'App' });

useElementPlusDesignTokens();
</script>

<template>
  <ElConfigProvider :locale="elementLocale">
    <RouterView />
  </ElConfigProvider>
</template>
