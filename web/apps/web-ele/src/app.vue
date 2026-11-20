<script lang="ts" setup>
import { useConsoleSessionRefresh } from '#/composables/use-console-access';
defineOptions({ name: 'App' });
useConsoleSessionRefresh();
import { useElementPlusDesignTokens } from '@vben/hooks';

import { ElConfigProvider } from 'element-plus';

import { elementLocale } from '#/locales';

useElementPlusDesignTokens();
</script>

<template>
  <ElConfigProvider :locale="elementLocale">
    <RouterView />
  </ElConfigProvider>
</template>
