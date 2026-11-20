<script setup lang="ts">
import { ElButton } from 'element-plus';

import { useConsoleAccess } from '#/composables/use-console-access';
const props = defineProps<{ permission: string }>();
const emit = defineEmits<{ click: [event: MouseEvent] }>();
const { can } = useConsoleAccess();
function click(event: MouseEvent) {
  if (can(props.permission)) emit('click', event);
}
</script>
<template>
  <ElButton v-if="can(permission)" @click="click"><slot></slot></ElButton>
</template>
